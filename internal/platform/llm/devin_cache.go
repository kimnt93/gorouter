package llm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/kimnt93/gorouter/pkg/entities"
)

// Only safe metadata is cached. ACP authenticates with the current repository
// credential and confirms its selected UID on EVERY chat, including warm hits.
type DevinCatalogCache interface {
	GetSnapshot(context.Context, string) ([]byte, bool, error)
	SetSnapshot(context.Context, string, []byte, time.Duration) error
	DeleteSnapshot(context.Context, string) error
}
type DevinCatalogLocker interface {
	WithLock(context.Context, string, func() error) (bool, error)
}
type devinCatalogSnapshot struct {
	FetchedAt time.Time    `json:"fetched_at"`
	Models    []devinModel `json:"models"`
}

func devinCatalogKey(cr *entities.CredentialRuntime) string {
	// Key rotation cannot read a preceding revision even if an old fetch completes
	// after mutation. Identical keys on different owned connections stay separate.
	b, _ := json.Marshal([]string{"devin-catalog-v1", cr.ID, cr.APIKey})
	digest := sha256.Sum256(b)
	return "devin:" + hex.EncodeToString(digest[:])
}
func (a *DevinCLIAdapter) invalidateCatalog(ctx context.Context, cr *entities.CredentialRuntime) {
	if a.CatalogCache != nil {
		_ = a.CatalogCache.DeleteSnapshot(ctx, devinCatalogKey(cr))
	}
}
func (a *DevinCLIAdapter) catalog(ctx context.Context, cr *entities.CredentialRuntime, force bool) ([]devinModel, error) {
	if err := validateDevinKey(cr.APIKey); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	key := devinCatalogKey(cr)
	minimum := time.Time{}
	if force {
		minimum = time.Now()
	}
	get := func() ([]devinModel, bool) {
		if a.CatalogCache == nil {
			return nil, false
		}
		b, ok, err := a.CatalogCache.GetSnapshot(ctx, key)
		if err != nil || !ok || len(b) > 4<<20 {
			return nil, false
		}
		var s devinCatalogSnapshot
		if json.Unmarshal(b, &s) != nil || len(s.Models) == 0 || s.FetchedAt.Before(minimum) {
			return nil, false
		}
		return s.Models, true
	}
	if models, ok := get(); ok {
		return models, nil
	}
	// Join metadata work only, never ACP sessions, histories or subprocess homes.
	// DoChan lets queued callers honor cancellation independently of the leader.
	// A refresh may join an already running fresh fetch.
	result := a.catalogGroup.DoChan(key, func() (any, error) {
		if models, ok := get(); ok {
			return models, nil
		}
		var models []devinModel
		fetch := func() error {
			if cached, ok := get(); ok {
				models = cached
				return nil
			}
			work, err := a.workspace(ctx, cr.APIKey)
			if err != nil {
				return err
			}
			defer work.Close()
			models, err = work.catalog()
			if err != nil {
				a.invalidateCatalog(ctx, cr)
				return err
			}
			if a.CatalogCache != nil {
				data, _ := json.Marshal(devinCatalogSnapshot{FetchedAt: time.Now().UTC(), Models: models})
				ttl := a.CatalogTTL
				if ttl <= 0 {
					ttl = 5 * time.Minute
				}
				_ = a.CatalogCache.SetSnapshot(ctx, key, data, ttl)
			}
			return nil
		}
		if a.CatalogLocker == nil || a.CatalogCache == nil {
			err := fetch()
			return models, err
		}
		for {
			acquired, err := a.CatalogLocker.WithLock(ctx, key, fetch)
			// Metadata is an optimization, not authorization. Redis outage falls back
			// to bounded fresh discovery rather than trusting process-local stale data.
			if err != nil && !acquired {
				err := fetch()
				return models, err
			}
			if acquired {
				return models, err
			}
			if cached, ok := get(); ok {
				return cached, nil
			}
			select {
			case <-ctx.Done():
				return nil, devinFailure(504, "Devin catalog refresh wait timed out or was canceled")
			case <-time.After(50 * time.Millisecond):
			}
		}
	})
	select {
	case <-ctx.Done():
		return nil, devinFailure(504, "Devin model discovery timed out or was canceled")
	case r := <-result:
		if r.Err != nil {
			return nil, r.Err
		}
		models, _ := r.Val.([]devinModel)
		return models, nil
	}
}
