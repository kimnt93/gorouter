package modelroute

import (
	"context"
	"errors"
	"testing"

	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
)

type syncCredentialRepo struct {
	credentials  []entities.Credential
	runtimeCalls *int
	runtime      *entities.CredentialRuntime
}

func (syncCredentialRepo) Create(context.Context, entities.CredentialInput, entities.SecretBox) (*entities.Credential, error) {
	return nil, nil
}
func (r syncCredentialRepo) List(context.Context) ([]entities.Credential, error) {
	if r.credentials != nil {
		return r.credentials, nil
	}
	return []entities.Credential{{ID: "cred", Status: entities.StatusActive}}, nil
}
func (syncCredentialRepo) Update(context.Context, entities.SecretBox, string, entities.CredentialUpdate) (*entities.Credential, error) {
	return nil, nil
}
func (syncCredentialRepo) Delete(context.Context, string) error { return nil }

func (r syncCredentialRepo) Runtime(context.Context, entities.SecretBox, string) (*entities.CredentialRuntime, error) {
	if r.runtimeCalls != nil {
		*r.runtimeCalls++
	}
	if r.runtime != nil {
		return r.runtime, nil
	}
	return &entities.CredentialRuntime{ID: "cred", Provider: "openai"}, nil
}

func TestCatalogSyncSkipsDisabledCredentials(t *testing.T) {
	runtimeCalls := 0
	credentialRepo := syncCredentialRepo{credentials: []entities.Credential{{ID: "cred", Status: entities.StatusDisabled}}, runtimeCalls: &runtimeCalls}
	repo := &syncModelRepo{models: []entities.ModelDef{{Name: "openai/existing", UpstreamModel: "existing", Enabled: true, Routes: []entities.ModelRoute{{CredentialID: "cred", Weight: 1, Enabled: true}}}}}
	sync := &CatalogSync{Credentials: credential.NewService(credentialRepo, nil), Models: NewService(repo), Discoverer: func(string) credential.ModelDiscoverer { return syncDiscoverer{} }}
	if err := sync.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if runtimeCalls != 0 || len(repo.upserts) != 0 {
		t.Fatalf("runtime calls=%d upserts=%+v", runtimeCalls, repo.upserts)
	}
}
func (syncCredentialRepo) UpdateOAuthTokens(context.Context, entities.SecretBox, string, string, string) error {
	return nil
}
func (syncCredentialRepo) RoutesForModel(context.Context, string) ([]entities.RouteCandidate, error) {
	return nil, nil
}

type syncModelRepo struct {
	models  []entities.ModelDef
	upserts []entities.ModelDef
	deletes []string
}

func (r *syncModelRepo) Upsert(_ context.Context, model entities.ModelDef) error {
	r.upserts = append(r.upserts, model)
	for index := range r.models {
		if r.models[index].Name == model.Name {
			r.models[index] = model
			return nil
		}
	}
	r.models = append(r.models, model)
	return nil
}
func (r *syncModelRepo) Delete(_ context.Context, name string) error {
	r.deletes = append(r.deletes, name)
	return nil
}
func (r *syncModelRepo) List(context.Context) ([]entities.ModelDef, error) {
	return append([]entities.ModelDef(nil), r.models...), nil
}
func (*syncModelRepo) SetPrice(context.Context, string, entities.Price) error        { return nil }
func (*syncModelRepo) DeletePrice(context.Context, string) error                     { return nil }
func (*syncModelRepo) ListPrices(context.Context) (map[string]entities.Price, error) { return nil, nil }

type syncDiscoverer struct{}

func (syncDiscoverer) DiscoverModels(context.Context, *entities.CredentialRuntime) ([]credential.ProviderModel, error) {
	return []credential.ProviderModel{{ID: "existing", Name: "Updated", InputModalities: []string{"text", "image"}}, {ID: "new-model", Name: "Not imported"}}, nil
}

func TestCatalogSyncRefreshesExistingRoutesAndImportsNewModels(t *testing.T) {
	repo := &syncModelRepo{models: []entities.ModelDef{{Name: "openai/existing", UpstreamModel: "existing", Enabled: true, Routes: []entities.ModelRoute{{CredentialID: "cred", Weight: 1, Enabled: true}}}}}
	sync := &CatalogSync{Credentials: credential.NewService(syncCredentialRepo{}, nil), Models: NewService(repo), Discoverer: func(string) credential.ModelDiscoverer { return syncDiscoverer{} }}
	if err := sync.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(repo.upserts) != 3 || repo.upserts[0].Name != "openai/auto" || len(repo.upserts[0].Routes) != 2 || repo.upserts[1].Name != "openai/existing" || repo.upserts[1].Metadata == nil || repo.upserts[1].Metadata.DisplayName != "Updated" || repo.upserts[2].Name != "openai/new-model" {
		t.Fatalf("upserts = %+v", repo.upserts)
	}
}

type catalogDiscovererFunc func(context.Context, *entities.CredentialRuntime) ([]credential.ProviderModel, error)

func (f catalogDiscovererFunc) DiscoverModels(ctx context.Context, runtime *entities.CredentialRuntime) ([]credential.ProviderModel, error) {
	return f(ctx, runtime)
}

func TestCatalogSyncImportsCanonicalProviderName(t *testing.T) {
	credentials := []entities.Credential{{ID: "cred", Provider: "opencode-zen", Status: entities.StatusActive}}
	repo := &syncModelRepo{}
	sync := &CatalogSync{
		Credentials: credential.NewService(syncCredentialRepo{credentials: credentials, runtime: &entities.CredentialRuntime{ID: "cred", Provider: "opencode-zen"}}, nil),
		Models:      NewService(repo),
		Discoverer: func(string) credential.ModelDiscoverer {
			return catalogDiscovererFunc(func(context.Context, *entities.CredentialRuntime) ([]credential.ProviderModel, error) {
				return []credential.ProviderModel{{ID: "gpt-5.6-luna"}}, nil
			})
		},
	}
	if err := sync.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(repo.upserts) != 2 || repo.upserts[0].Name != "ocz/auto" || repo.upserts[1].Name != "ocz/gpt-5.6-luna" || len(repo.upserts[1].Routes) != 1 {
		t.Fatalf("upserts=%+v", repo.upserts)
	}
}

func TestCatalogSyncPrunesOnlyAfterSuccessfulDiscovery(t *testing.T) {
	existing := entities.ModelDef{Name: "ocz/removed", UpstreamModel: "removed", Enabled: true, Routes: []entities.ModelRoute{{CredentialID: "cred", Weight: 1, Enabled: true}}, Metadata: &entities.ModelMetadata{Provider: "opencode-zen", SourceCredentialID: "cred"}}
	credentials := []entities.Credential{{ID: "cred", Provider: "opencode-zen", Status: entities.StatusActive}}
	credentialRepo := syncCredentialRepo{credentials: credentials, runtime: &entities.CredentialRuntime{ID: "cred", Provider: "opencode-zen"}}

	failingRepo := &syncModelRepo{models: []entities.ModelDef{existing}}
	failing := &CatalogSync{Credentials: credential.NewService(credentialRepo, nil), Models: NewService(failingRepo), Discoverer: func(string) credential.ModelDiscoverer {
		return catalogDiscovererFunc(func(context.Context, *entities.CredentialRuntime) ([]credential.ProviderModel, error) {
			return nil, errors.New("upstream unavailable")
		})
	}}
	if err := failing.Refresh(context.Background()); err == nil {
		t.Fatal("expected discovery failure")
	}
	if len(failingRepo.deletes) != 0 || len(failingRepo.upserts) != 0 {
		t.Fatalf("failed discovery changed catalog: deletes=%v upserts=%+v", failingRepo.deletes, failingRepo.upserts)
	}

	successRepo := &syncModelRepo{models: []entities.ModelDef{existing}}
	success := &CatalogSync{Credentials: credential.NewService(credentialRepo, nil), Models: NewService(successRepo), Discoverer: func(string) credential.ModelDiscoverer {
		return catalogDiscovererFunc(func(context.Context, *entities.CredentialRuntime) ([]credential.ProviderModel, error) {
			return []credential.ProviderModel{}, nil
		})
	}}
	if err := success.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(successRepo.deletes) != 1 || successRepo.deletes[0] != "ocz/removed" {
		t.Fatalf("deletes=%v", successRepo.deletes)
	}
}

type skipRefreshLocker struct{ calls int }

func (l *skipRefreshLocker) WithLock(context.Context, string, func() error) (bool, error) {
	l.calls++
	return false, nil
}

func TestCatalogSyncSkipsWhenAnotherReplicaOwnsRefresh(t *testing.T) {
	locker := &skipRefreshLocker{}
	repo := &syncModelRepo{}
	sync := &CatalogSync{Credentials: credential.NewService(syncCredentialRepo{}, nil), Models: NewService(repo), Discoverer: func(string) credential.ModelDiscoverer { return syncDiscoverer{} }, Locker: locker}
	if err := sync.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if locker.calls != 1 || len(repo.models) != 0 {
		t.Fatalf("locker calls=%d models=%+v", locker.calls, repo.models)
	}
}
