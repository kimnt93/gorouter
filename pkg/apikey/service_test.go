package apikey

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/kimnt93/gorouter/pkg/entities"
)

type repositoryStub struct {
	created      CreateInput
	listedTenant string
	patched      struct{ tenant, id string }
	deleted      struct{ tenant, id string }
}

func (r *repositoryStub) CreateWithQuota(_ context.Context, tenantID, name string, models, scopes []string, quota *float64, period string, rpm *int) (*entities.ApiKey, error) {
	r.created = CreateInput{TenantID: tenantID, Name: name, Models: models, Scopes: scopes, QuotaUSD: quota, QuotaPeriod: period, RPM: rpm}
	return &entities.ApiKey{ID: "key_1", TenantID: tenantID, Name: name, Models: models, Scopes: scopes, QuotaUSD: quota, QuotaPeriod: period}, nil
}

func (*repositoryStub) PatchQuota(context.Context, string, *bool, *[]string, *[]string, **float64, *string, **int) error {
	return nil
}

func (r *repositoryStub) PatchQuotaForTenant(_ context.Context, tenantID, id string, _ *bool, _ *[]string, _ *[]string, _ **float64, _ *string, _ **int) error {
	r.patched.tenant, r.patched.id = tenantID, id
	return nil
}

func (r *repositoryStub) Create(_ context.Context, tenantID, name string, models, scopes []string, quota *float64, rpm *int) (*entities.ApiKey, error) {
	r.created = CreateInput{TenantID: tenantID, Name: name, Models: models, Scopes: scopes, MonthlyQuotaUSD: quota, RPM: rpm}
	return &entities.ApiKey{ID: "key_1", TenantID: tenantID, Name: name, Models: models, Scopes: scopes}, nil
}
func (*repositoryStub) GetBySecret(context.Context, string) (*entities.ApiKey, error) {
	return nil, entities.ErrNotFound
}
func (*repositoryStub) GetByID(context.Context, string) (*entities.ApiKey, error) {
	return nil, entities.ErrNotFound
}
func (*repositoryStub) GetByIDForTenant(context.Context, string, string) (*entities.ApiKey, error) {
	return nil, entities.ErrNotFound
}
func (*repositoryStub) List(context.Context) ([]entities.ApiKey, error) { return nil, nil }
func (r *repositoryStub) ListByTenant(_ context.Context, tenantID string) ([]entities.ApiKey, error) {
	r.listedTenant = tenantID
	return nil, nil
}
func (*repositoryStub) Patch(context.Context, string, *bool, *[]string, *[]string, **float64, **int) error {
	return nil
}
func (r *repositoryStub) PatchForTenant(_ context.Context, tenantID, id string, _ *bool, _ *[]string, _ *[]string, _ **float64, _ **int) error {
	r.patched.tenant, r.patched.id = tenantID, id
	return nil
}
func (*repositoryStub) Delete(context.Context, string) error { return nil }
func (r *repositoryStub) DeleteForTenant(_ context.Context, tenantID, id string) error {
	r.deleted.tenant, r.deleted.id = tenantID, id
	return nil
}

func newTestService(repo Repository) *Service {
	return NewService(repo, func(s string) string { return s }, func() string { return "secret" })
}

func TestCreateValidatesAndNormalizes(t *testing.T) {
	repo := &repositoryStub{}
	svc := newTestService(repo)
	got, err := svc.Create(context.Background(), CreateInput{
		TenantID: " tenant_1 ", Name: " test key ",
		Models: []string{" gpt-test ", "gpt-test"},
		Scopes: []string{" chat ", "chat", "usage:read"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.TenantID != "tenant_1" || got.Name != "test key" {
		t.Fatalf("unexpected key: %+v", got)
	}
	if !reflect.DeepEqual(repo.created.Models, []string{"gpt-test"}) || !reflect.DeepEqual(repo.created.Scopes, []string{"chat", "usage:read"}) {
		t.Fatalf("input not normalized: %+v", repo.created)
	}
}

func TestCreateAllowsEmptyModelAllowlist(t *testing.T) {
	_, err := newTestService(&repositoryStub{}).Create(context.Background(), CreateInput{TenantID: "tenant_1", Name: "denied", Scopes: []string{}})
	if err != nil {
		t.Fatalf("empty allowlist should be valid and deny all models: %v", err)
	}
}

func TestCreateNormalizesQuotaPeriods(t *testing.T) {
	quota := 2.5
	tests := []struct {
		name       string
		in         CreateInput
		wantQuota  *float64
		wantPeriod string
	}{
		{"no limit", CreateInput{TenantID: "t", Name: "k", QuotaUSD: &quota, QuotaPeriod: "none"}, nil, entities.QuotaPeriodNone},
		{"weekly", CreateInput{TenantID: "t", Name: "k", QuotaUSD: &quota, QuotaPeriod: " WEEK "}, &quota, entities.QuotaPeriodWeek},
		{"generic default", CreateInput{TenantID: "t", Name: "k", QuotaUSD: &quota}, &quota, entities.QuotaPeriodWeek},
		{"legacy value becomes weekly", CreateInput{TenantID: "t", Name: "k", MonthlyQuotaUSD: &quota}, &quota, entities.QuotaPeriodWeek},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &repositoryStub{}
			if _, err := newTestService(repo).Create(context.Background(), tt.in); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(repo.created.QuotaUSD, tt.wantQuota) || repo.created.QuotaPeriod != tt.wantPeriod {
				t.Fatalf("quota = %v/%q, want %v/%q", repo.created.QuotaUSD, repo.created.QuotaPeriod, tt.wantQuota, tt.wantPeriod)
			}
		})
	}
}

func TestCreateRejectsInvalidInput(t *testing.T) {
	quota := math.NaN()
	rpm := 0
	tests := []struct {
		name string
		in   CreateInput
		want error
	}{
		{"tenant", CreateInput{Name: "x"}, ErrTenantRequired},
		{"name", CreateInput{TenantID: "t"}, ErrNameRequired},
		{"model", CreateInput{TenantID: "t", Name: "x", Models: []string{" "}}, ErrInvalidModel},
		{"scope", CreateInput{TenantID: "t", Name: "x", Scopes: []string{"admin"}}, ErrInvalidScope},
		{"quota", CreateInput{TenantID: "t", Name: "x", MonthlyQuotaUSD: &quota}, ErrInvalidQuota},
		{"period", CreateInput{TenantID: "t", Name: "x", QuotaPeriod: "year"}, ErrInvalidPeriod},
		{"missing quota", CreateInput{TenantID: "t", Name: "x", QuotaPeriod: "week"}, ErrQuotaRequired},
		{"daily rejected", CreateInput{TenantID: "t", Name: "x", QuotaUSD: new(float64), QuotaPeriod: "day"}, ErrInvalidPeriod},
		{"rpm", CreateInput{TenantID: "t", Name: "x", RPM: &rpm}, ErrInvalidRPM},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newTestService(&repositoryStub{}).Create(context.Background(), tt.in)
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestTenantScopedOperationsDelegateTenant(t *testing.T) {
	repo := &repositoryStub{}
	svc := newTestService(repo)
	if _, err := svc.ListByTenant(context.Background(), " tenant_1 "); err != nil {
		t.Fatal(err)
	}
	models := []string{}
	if err := svc.PatchForTenant(context.Background(), "tenant_1", "key_1", nil, &models, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteForTenant(context.Background(), "tenant_1", "key_1"); err != nil {
		t.Fatal(err)
	}
	if repo.listedTenant != "tenant_1" || repo.patched.tenant != "tenant_1" || repo.deleted.tenant != "tenant_1" {
		t.Fatalf("tenant was not propagated: %+v", repo)
	}
}
