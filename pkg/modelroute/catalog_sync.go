package modelroute

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kimnt93/gorouter/pkg/chat"
	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/provider"
)

type CatalogDiscoverer func(providerID string) credential.ModelDiscoverer
type OrganizationNameResolver func(ctx context.Context, id string) (string, error)
type RefreshLocker interface {
	WithLock(ctx context.Context, key string, fn func() error) (bool, error)
}

type catalogCredential struct {
	definition entities.Credential
	provider   string
	orgName    string
	successful bool
	discovered map[string]credential.ProviderModel
}

// CatalogSync makes durable, callable routes match successful provider
// discovery. A discovery failure preserves the last known-good routes.
type CatalogSync struct {
	Credentials      *credential.Service
	Models           *Service
	Discoverer       CatalogDiscoverer
	OrganizationName OrganizationNameResolver
	Locker           RefreshLocker

	mu   sync.Mutex
	wake chan struct{}
}

func (s *CatalogSync) Trigger() {
	if s == nil || s.wake == nil {
		return
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *CatalogSync) Refresh(ctx context.Context) error {
	if s == nil || s.Credentials == nil || s.Models == nil || s.Discoverer == nil {
		return fmt.Errorf("model catalog sync is unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Locker != nil {
		_, err := s.Locker.WithLock(ctx, "model-catalog", func() error { return s.refreshLocked(ctx) })
		return err
	}
	return s.refreshLocked(ctx)
}

func (s *CatalogSync) refreshLocked(ctx context.Context) error {
	connections, err := s.Credentials.List(ctx)
	if err != nil {
		return err
	}
	models, err := s.Models.List(ctx)
	if err != nil {
		return err
	}
	byName := make(map[string]entities.ModelDef, len(models))
	original := make(map[string]entities.ModelDef, len(models))
	for _, model := range models {
		byName[model.Name] = model
		original[model.Name] = model
	}

	states := make(map[string]*catalogCredential, len(connections))
	failures := 0
	for _, connection := range connections {
		state := &catalogCredential{definition: connection, provider: connection.Provider}
		states[connection.ID] = state
		if connection.Status != "" && connection.Status != entities.StatusActive {
			continue
		}
		runtime, runtimeErr := s.Credentials.Runtime(ctx, connection.ID)
		if runtimeErr != nil {
			failures++
			continue
		}
		state.provider = runtime.Provider
		if connection.OwnerTenantID != nil {
			if s.OrganizationName == nil {
				failures++
				continue
			}
			state.orgName, err = s.OrganizationName(ctx, *connection.OwnerTenantID)
			if err != nil {
				failures++
				continue
			}
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
		state.successful = true
		state.discovered = make(map[string]credential.ProviderModel, len(discovered))
		for _, item := range discovered {
			item.ID = strings.TrimSpace(item.ID)
			if item.ID == "" {
				continue
			}
			name := provider.PublicModelID(runtime.Provider, item.ID)
			if state.orgName != "" {
				name = provider.OrganizationModelID(state.orgName, runtime.Provider, item.ID)
			}
			state.discovered[name] = item
			model, exists := byName[name]
			if !exists {
				model = entities.ModelDef{Name: name, Strategy: "priority", UpstreamModel: item.ID, Enabled: true, Routes: []entities.ModelRoute{}}
			}
			hasRoute := false
			nextPriority := 0
			for _, route := range model.Routes {
				if route.CredentialID == connection.ID && (route.UpstreamModel == "" || route.UpstreamModel == model.UpstreamModel) {
					hasRoute = true
				}
				if route.Priority <= nextPriority {
					nextPriority = route.Priority - 1
				}
			}
			if !hasRoute {
				model.Routes = append(model.Routes, entities.ModelRoute{CredentialID: connection.ID, UpstreamModel: model.UpstreamModel, Priority: nextPriority, Weight: 1, Enabled: true})
			}
			model.Metadata = credential.MetadataSnapshot(runtime.Provider, connection.ID, item, time.Now().UTC())
			byName[name] = model
		}
	}

	// Every successfully discovered provider gets a durable static auto alias.
	providerAutos := map[string]entities.ModelDef{}
	for _, state := range states {
		if !state.successful || len(state.discovered) == 0 {
			continue
		}
		autoName := provider.PublicModelID(state.provider, "auto")
		if state.orgName != "" {
			autoName = provider.OrganizationModelID(state.orgName, state.provider, "auto")
		}
		auto := providerAutos[autoName]
		if auto.Name == "" {
			auto = entities.ModelDef{Name: autoName, Strategy: chat.StrategyRoundRobin, UpstreamModel: "auto", Enabled: true, Routes: []entities.ModelRoute{}, Metadata: &entities.ModelMetadata{Provider: state.provider}}
		}
		for _, item := range state.discovered {
			auto.Routes = append(auto.Routes, entities.ModelRoute{CredentialID: state.definition.ID, UpstreamModel: item.ID, Priority: 0, Weight: 1, Enabled: true})
		}
		providerAutos[autoName] = auto
	}
	for name, auto := range providerAutos {
		byName[name] = auto
	}

	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		model := byName[name]
		routes := make([]entities.ModelRoute, 0, len(model.Routes))
		for _, route := range model.Routes {
			state, exists := states[route.CredentialID]
			if !exists {
				continue
			}
			canonical := provider.PublicModelID(state.provider, model.UpstreamModel)
			if state.orgName != "" {
				canonical = provider.OrganizationModelID(state.orgName, state.provider, model.UpstreamModel)
			}
			managedRoute := name == canonical && model.UpstreamModel != "auto"
			if managedRoute && state.definition.Status != "" && state.definition.Status != entities.StatusActive {
				continue
			}
			if managedRoute && state.successful {
				if _, reported := state.discovered[name]; !reported {
					continue
				}
			}
			routes = append(routes, route)
		}
		model.Routes = routes
		byName[name] = model
	}

	for _, name := range names {
		model := byName[name]
		if len(model.Routes) == 0 && isProviderModel(model) {
			if err := s.Models.Delete(ctx, name); err != nil && !errors.Is(err, entities.ErrNotFound) {
				failures++
			}
			continue
		}
		if before, exists := original[name]; exists && reflect.DeepEqual(before, model) {
			continue
		}
		if err := s.Models.Upsert(ctx, model); err != nil {
			failures++
		}
	}
	if failures != 0 {
		return fmt.Errorf("model catalog refresh had %d failed operation(s)", failures)
	}
	return nil
}

func isProviderModel(model entities.ModelDef) bool {
	if model.Metadata == nil || model.Metadata.Provider == "" {
		return false
	}
	public := provider.PublicModelID(model.Metadata.Provider, model.UpstreamModel)
	return model.Name == public || strings.HasSuffix(model.Name, "/"+public)
}

func (s *CatalogSync) Start(ctx context.Context, interval time.Duration, report func(error)) {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	s.wake = make(chan struct{}, 1)
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
			case <-s.wake:
				run()
			}
		}
	}()
}
