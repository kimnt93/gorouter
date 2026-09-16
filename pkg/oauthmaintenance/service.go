// Package oauthmaintenance coordinates periodic and request-time OAuth token refresh.
package oauthmaintenance

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/kimnt93/gorouter/pkg/entities"
)

// Store supplies the authoritative encrypted credential state; token material
// must never be cached by this service.
type Store interface {
	List(context.Context) ([]entities.Credential, error)
	Runtime(context.Context, string) (*entities.CredentialRuntime, error)
}

type Locker interface {
	WithLock(context.Context, string, func() error) (bool, error)
}
type Refresh func(context.Context, *entities.CredentialRuntime) error

type Service struct {
	Store      Store
	Locker     Locker // required for multi-replica deployments
	Refreshers map[string]Refresh
	mu         sync.Mutex // serializes request and timer refresh in single-process mode
}

var ErrBusy = errors.New("oauth refresh in progress")

// Refresh serializes the entire read/exchange/persist sequence with request-time
// refresh. A stale request runtime is replaced by the latest durable tokens.
func (s *Service) Refresh(ctx context.Context, cr *entities.CredentialRuntime, force bool) error {
	if s == nil || s.Store == nil || cr == nil || cr.ID == "" {
		return errors.New("oauth refresh unavailable")
	}
	refresh := s.Refreshers[cr.Provider]
	if refresh == nil {
		return errors.New("oauth refresh unsupported")
	}
	// Bound provider I/O and persistence below the distributed lease lifetime.
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	run := func() error {
		latest, err := s.Store.Runtime(ctx, cr.ID)
		if err != nil {
			return err
		}
		if latest.Kind != entities.KindOAuth || latest.Provider != cr.Provider {
			return errors.New("oauth credential changed")
		}
		if latest.OAuthRefreh == "" {
			return errors.New("oauth refresh token unavailable")
		}
		// Another request/replica may already have rotated the credential. Reuse it.
		if latest.OAuthAccess != cr.OAuthAccess || latest.OAuthRefreh != cr.OAuthRefreh {
			*cr = *latest
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if !force && !needsRefresh(latest.OAuthMeta.TokenExpiresAt) {
			return nil
		}
		if err := refresh(ctx, latest); err != nil {
			return err
		}
		*cr = *latest
		return nil
	}
	if s.Locker != nil {
		// The Redis lock skips on contention. Wait briefly and reload rather than
		// exchanging the same single-use refresh token on another replica.
		for {
			acquired, err := s.Locker.WithLock(ctx, "oauth:"+cr.ID, run)
			if err != nil {
				return err
			} // Redis outage fails closed
			if acquired {
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(200 * time.Millisecond):
			}
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return run()
}

func needsRefresh(expires string) bool {
	if expires == "" {
		return true
	} // provider omitted expiry: refresh on timer
	expiry, err := time.Parse(time.RFC3339, expires)
	return err != nil || time.Until(expiry) < 15*time.Minute
}

// Start checks idle active OAuth connections periodically. Each exchange is
// bounded below the distributed lock TTL, and errors contain no token material.
func (s *Service) Start(ctx context.Context, interval time.Duration, report func(error)) {
	if interval <= 0 {
		interval = 30 * time.Minute
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			listCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			credentials, err := s.Store.List(listCtx)
			cancel()
			if err != nil {
				if report != nil {
					report(fmt.Errorf("list oauth credentials: %w", err))
				}
				continue
			}
			for _, c := range credentials {
				if ctx.Err() != nil {
					return
				}
				if c.Kind != entities.KindOAuth || (c.Status != "" && c.Status != entities.StatusActive) || s.Refreshers[c.Provider] == nil {
					continue
				}
				opCtx, stop := context.WithTimeout(ctx, 45*time.Second)
				cr, err := s.Store.Runtime(opCtx, c.ID)
				if err == nil && cr.OAuthRefreh != "" {
					err = s.Refresh(opCtx, cr, false)
				}
				stop()
				if err != nil && report != nil {
					report(fmt.Errorf("refresh oauth credential %s: %w", c.ID, err))
				}
			}
		}
	}()
}
