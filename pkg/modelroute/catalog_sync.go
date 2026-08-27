package modelroute

import (
	"context"
	"fmt"
	"time"

	"github.com/kimnt93/gorouter/pkg/credential"
)

type CatalogDiscoverer func(providerID string) credential.ModelDiscoverer

// CatalogSync periodically refreshes metadata for already configured routes.
// It never imports newly discovered models or changes route ownership.
type CatalogSync struct {
	Credentials *credential.Service
	Models      *Service
	Discoverer  CatalogDiscoverer
}

func (s *CatalogSync) Refresh(ctx context.Context) error {
	if s == nil || s.Credentials == nil || s.Models == nil || s.Discoverer == nil {
		return fmt.Errorf("model catalog sync is unavailable")
	}
	credentials, err := s.Credentials.List(ctx)
	if err != nil {
		return err
	}
	models, err := s.Models.List(ctx)
	if err != nil {
		return err
	}
	failures := 0
	for _, connection := range credentials {
		runtime, runtimeErr := s.Credentials.Runtime(ctx, connection.ID)
		if runtimeErr != nil {
			failures++
			continue
		}
		discoverer := s.Discoverer(runtime.Provider)
		if discoverer == nil {
			continue
		}
		discovered, discoverErr := s.Credentials.DiscoverModels(ctx, runtime.ID, discoverer)
		if discoverErr != nil {
			failures++
			continue
		}
		byID := make(map[string]credential.ProviderModel, len(discovered))
		for _, item := range discovered {
			byID[item.ID] = item
		}
		now := time.Now()
		for index := range models {
			model := &models[index]
			if model.Metadata != nil && model.Metadata.SourceCredentialID != "" && model.Metadata.SourceCredentialID != runtime.ID {
				continue
			}
			usesCredential := false
			for _, route := range model.Routes {
				if route.CredentialID == runtime.ID {
					usesCredential = true
					break
				}
			}
			if !usesCredential {
				continue
			}
			reported, ok := byID[model.UpstreamModel]
			if !ok {
				continue
			}
			model.Metadata = credential.MetadataSnapshot(runtime.Provider, runtime.ID, reported, now)
			if upsertErr := s.Models.Upsert(ctx, *model); upsertErr != nil {
				failures++
			}
		}
	}
	if failures != 0 {
		return fmt.Errorf("model catalog refresh had %d failed operation(s)", failures)
	}
	return nil
}

func (s *CatalogSync) Start(ctx context.Context, interval time.Duration, report func(error)) {
	go func() {
		run := func() {
			if err := s.Refresh(ctx); err != nil && report != nil {
				report(err)
			}
		}
		run()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				run()
			}
		}
	}()
}
