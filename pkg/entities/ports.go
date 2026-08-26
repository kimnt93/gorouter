package entities

import (
	"context"
	"io"
	"time"
)

type SecretBox interface {
	Seal(plaintext []byte) ([]byte, error)
	Open(sealed []byte) ([]byte, error)
}

type UpstreamResult struct {
	StatusCode int
	Header     map[string][]string
	Body       io.ReadCloser
}

type CredentialRuntime struct {
	ID           string
	Provider     string
	Kind         string
	BaseURL      string
	APIKey       string
	OAuthAccess  string
	OAuthRefreh  string
	OAuthIDToken string
	OAuthAccount string
	OAuthMeta    OAuthMetadata
}

type Upstream interface {
	Send(ctx context.Context, cr *CredentialRuntime, upstreamModel string, rawBody []byte) (*UpstreamResult, error)
}

type TenantRepository interface {
	List(ctx context.Context) ([]Tenant, error)
	Create(ctx context.Context, name string) (*Tenant, error)
	EnsureDefault(ctx context.Context) error
}

type CredentialRepository interface {
	Create(ctx context.Context, in CredentialInput, box SecretBox) (*Credential, error)
	List(ctx context.Context) ([]Credential, error)
	Update(ctx context.Context, box SecretBox, id string, in CredentialUpdate) (*Credential, error)
	Delete(ctx context.Context, id string) error
	Runtime(ctx context.Context, box SecretBox, id string) (*CredentialRuntime, error)
	UpdateOAuthTokens(ctx context.Context, box SecretBox, id, access, refresh string) error
	RoutesForModel(ctx context.Context, model string) ([]RouteCandidate, error)
}

type RouteCandidate struct {
	CredentialID string
	Priority     int
	Weight       int
	OwnerTenant  *string
}

type ApiKeyRepository interface {
	Create(ctx context.Context, tenantID, name string, models, scopes []string, quota *float64, rpm *int) (*ApiKey, error)
	GetBySecret(ctx context.Context, secretHash string) (*ApiKey, error)
	List(ctx context.Context) ([]ApiKey, error)
	Patch(ctx context.Context, id string, enabled *bool, models *[]string, scopes *[]string, quota **float64, rpm **int) error
	Delete(ctx context.Context, id string) error
}

type ModelRepository interface {
	Upsert(ctx context.Context, m ModelDef) error
	Delete(ctx context.Context, name string) error
	List(ctx context.Context) ([]ModelDef, error)
	SetPrice(ctx context.Context, model string, p Price) error
	DeletePrice(ctx context.Context, model string) error
	ListPrices(ctx context.Context) (map[string]Price, error)
}

// CatalogPriceRepository is separate from ModelRepository because imported
// prices are fallback data; manually configured model prices take priority.
type CatalogPriceRepository interface {
	ReplaceCatalogPrices(ctx context.Context, source string, prices []CatalogPrice) error
	ListCatalogPrices(ctx context.Context) ([]CatalogPrice, error)
}

type UsageRepository interface {
	MonthSpendForKey(ctx context.Context, apiKeyID string) (float64, error)
	SpendForKeySince(ctx context.Context, apiKeyID string, since time.Time) (float64, error)
	Summary(ctx context.Context, since time.Time) (*UsageSummary, error)
	Recent(ctx context.Context, limit int) ([]RecentEvent, error)
	SummaryForTenant(ctx context.Context, tenantID string, since time.Time) (*UsageSummary, error)
	RecentForTenant(ctx context.Context, tenantID string, limit int) ([]RecentEvent, error)
}
