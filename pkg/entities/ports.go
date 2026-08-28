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
	CredentialID  string
	UpstreamModel string
	Priority      int
	Weight        int
	OwnerTenant   *string
	OwnerUserID   string
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
	SpendForKeySince(ctx context.Context, apiKeyID string, since time.Time) (float64, error)
	Summary(ctx context.Context, since time.Time) (*UsageSummary, error)
	Recent(ctx context.Context, limit int) ([]RecentEvent, error)
	SummaryForTenant(ctx context.Context, tenantID string, since time.Time) (*UsageSummary, error)
	RecentForTenant(ctx context.Context, tenantID string, limit int) ([]RecentEvent, error)
}

type PageQuery struct {
	Cursor string
	Limit  int
	Query  string
	Status string
}

type UserRepository interface {
	CreateUser(ctx context.Context, user User) error
	UserByID(ctx context.Context, id string) (*User, error)
	UserByNormalizedUsername(ctx context.Context, normalized string) (*User, error)
	ListUsers(ctx context.Context, query PageQuery) ([]User, string, error)
	UpdateUserStatus(ctx context.Context, id, status string, updatedAt time.Time) error
}

type OrganizationRepository interface {
	CreateOrganization(ctx context.Context, organization Organization) error
	OrganizationByID(ctx context.Context, id string) (*Organization, error)
	OrganizationByNormalizedName(ctx context.Context, normalized string) (*Organization, error)
	ListOrganizations(ctx context.Context, query PageQuery) ([]Organization, string, error)
	UpdateOrganization(ctx context.Context, organization Organization) error
}

type MembershipRepository interface {
	PutMembership(ctx context.Context, membership Membership) error
	Membership(ctx context.Context, organizationID, userID string) (*Membership, error)
	ListMemberships(ctx context.Context, organizationID string) ([]Membership, error)
	ListMembershipsForUser(ctx context.Context, userID string) ([]Membership, error)
	CountActiveOrganizationAdmins(ctx context.Context, organizationID string) (int, error)
	DeleteMembership(ctx context.Context, organizationID, userID string) error
}

type UsageVisibility struct {
	PrincipalType    string
	UserID           string
	OrganizationID   string
	OrganizationWide bool
}

type UsageQuery struct {
	Visibility     UsageVisibility
	Cursor         string
	Limit          int
	Since          *time.Time
	Until          *time.Time
	OrganizationID string
	UserID         string
	Model          string
	APIKeyID       string
	StatusCode     *int
}

type UsagePage struct {
	Data       []RecentEvent `json:"data"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

type PrincipalUsageRepository interface {
	QueryUsage(ctx context.Context, query UsageQuery) (*UsagePage, error)
	SummaryUsage(ctx context.Context, query UsageQuery) (*UsageSummary, error)
}

type UsageActivityRepository interface {
	ActivityUsage(ctx context.Context, query UsageQuery, groupBy string) ([]UsageActivityBucket, error)
}

type UsageHealthRepository interface {
	HealthUsage(ctx context.Context, query UsageQuery) ([]UsageHealthMetric, error)
}

type AuditQuery struct {
	Visibility     UsageVisibility
	OrganizationID string
	Cursor         string
	Limit          int
	Since          *time.Time
	Until          *time.Time
	ActorID        string
	Action         string
	TargetType     string
	TargetID       string
}

type AuditPage struct {
	Data       []AuditEvent `json:"data"`
	NextCursor string       `json:"next_cursor,omitempty"`
}

type AuditRepository interface {
	AppendAudit(ctx context.Context, event AuditEvent) error
	QueryAudit(ctx context.Context, query AuditQuery) (*AuditPage, error)
}
