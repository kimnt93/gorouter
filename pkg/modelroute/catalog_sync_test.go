package modelroute

import (
	"context"
	"testing"

	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
)

type syncCredentialRepo struct {
	credentials  []entities.Credential
	runtimeCalls *int
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
}

func (r *syncModelRepo) Upsert(_ context.Context, model entities.ModelDef) error {
	r.upserts = append(r.upserts, model)
	return nil
}
func (*syncModelRepo) Delete(context.Context, string) error { return nil }
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

func TestCatalogSyncRefreshesExistingRoutesWithoutImportingNewModels(t *testing.T) {
	repo := &syncModelRepo{models: []entities.ModelDef{{Name: "openai/existing", UpstreamModel: "existing", Enabled: true, Routes: []entities.ModelRoute{{CredentialID: "cred", Weight: 1, Enabled: true}}}}}
	sync := &CatalogSync{Credentials: credential.NewService(syncCredentialRepo{}, nil), Models: NewService(repo), Discoverer: func(string) credential.ModelDiscoverer { return syncDiscoverer{} }}
	if err := sync.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(repo.upserts) != 1 || repo.upserts[0].Name != "openai/existing" || repo.upserts[0].Metadata == nil || repo.upserts[0].Metadata.DisplayName != "Updated" {
		t.Fatalf("upserts = %+v", repo.upserts)
	}
}
