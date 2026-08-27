package modelroute

import (
	"context"
	"testing"

	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
)

type syncCredentialRepo struct{}

func (syncCredentialRepo) Create(context.Context, entities.CredentialInput, entities.SecretBox) (*entities.Credential, error) {
	return nil, nil
}
func (syncCredentialRepo) List(context.Context) ([]entities.Credential, error) {
	return []entities.Credential{{ID: "cred"}}, nil
}
func (syncCredentialRepo) Update(context.Context, entities.SecretBox, string, entities.CredentialUpdate) (*entities.Credential, error) {
	return nil, nil
}
func (syncCredentialRepo) Delete(context.Context, string) error { return nil }
func (syncCredentialRepo) Runtime(context.Context, entities.SecretBox, string) (*entities.CredentialRuntime, error) {
	return &entities.CredentialRuntime{ID: "cred", Provider: "openai"}, nil
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
