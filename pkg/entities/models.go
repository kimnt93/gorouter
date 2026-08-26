package entities

import (
	"errors"
	"time"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
)

const (
	ProviderOpenAICompatible = "openai-compatible"
	ProviderAnthropic        = "anthropic"

	KindAPIKey = "api_key"
	KindOAuth  = "oauth"
)

type Tenant struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type Credential struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Provider      string    `json:"provider"`
	Kind          string    `json:"kind"`
	BaseURL       string    `json:"base_url"`
	Status        string    `json:"status"`
	KeyPreview    string    `json:"key_preview,omitempty"`
	OwnerTenantID *string   `json:"owner_tenant_id"`
	CreatedAt     time.Time `json:"created_at"`

	apiKeySealed []byte
	oauthSealed  []byte
}

func (c *Credential) SetSecrets(apiKeySealed, oauthSealed []byte) {
	c.apiKeySealed = apiKeySealed
	c.oauthSealed = oauthSealed
}

func (c *Credential) APIKeySealed() []byte { return c.apiKeySealed }
func (c *Credential) OAuthSealed() []byte  { return c.oauthSealed }
func (c *Credential) HasAPIKey() bool      { return len(c.apiKeySealed) > 0 }
func (c *Credential) HasOAuthBlob() bool   { return len(c.oauthSealed) > 0 }

type CredentialInput struct {
	Name         string
	Provider     string
	Kind         string
	BaseURL      string
	APIKey       string
	OAuthAccess  string
	OAuthRefresh string
	OAuthIDToken string
	OAuthAccount string
	OAuthMeta    OAuthMetadata
	OwnerTenant  *string
}

type OAuthMetadata struct {
	AccountID      string `json:"account_id,omitempty"`
	OrganizationID string `json:"organization_id,omitempty"`
	DeviceID       string `json:"device_id,omitempty"`
	ClientID       string `json:"client_id,omitempty"`
	ClientSecret   string `json:"client_secret,omitempty"`
	Region         string `json:"region,omitempty"`
	AuthMethod     string `json:"auth_method,omitempty"`
	MachineID      string `json:"machine_id,omitempty"`
	ProjectID      string `json:"project_id,omitempty"`
	ProfileARN     string `json:"profile_arn,omitempty"`
	CopilotToken   string `json:"copilot_token,omitempty"`
	TokenExpiresAt string `json:"token_expires_at,omitempty"`
	Login          string `json:"login,omitempty"`
	Email          string `json:"email,omitempty"`
	PrincipalType  string `json:"principal_type,omitempty"`
	PrincipalID    string `json:"principal_id,omitempty"`
	TeamID         string `json:"team_id,omitempty"`
}

// CredentialUpdate replaces safe credential metadata and optionally rotates
// the secret for the credential's existing kind. Provider and kind are
// intentionally immutable so a partial update cannot reinterpret ciphertext.
type CredentialUpdate struct {
	Name         string
	BaseURL      string
	Status       string
	APIKey       string
	OAuthAccess  string
	OAuthRefresh string
	OwnerTenant  *string
}

type ApiKey struct {
	ID                    string    `json:"id"`
	TenantID              string    `json:"tenant_id"`
	TenantName            string    `json:"tenant_name,omitempty"`
	Name                  string    `json:"name"`
	SecretHash            string    `json:"-"`
	SecretPrefix          string    `json:"key_prefix"`
	Models                []string  `json:"models"`
	Scopes                []string  `json:"scopes"`
	QuotaUSD              *float64  `json:"quota_usd"`
	QuotaPeriod           string    `json:"quota_period"`
	RPM                   *int      `json:"rpm"`
	Enabled               bool      `json:"enabled"`
	CreatedAt             time.Time `json:"created_at"`
	OwnerType             string    `json:"owner_type"`
	OwnerUserID           string    `json:"owner_user_id,omitempty"`
	OwnerOrganizationID   string    `json:"owner_organization_id,omitempty"`
	ContextOrganizationID string    `json:"context_organization_id,omitempty"`

	Plaintext string `json:"-"`
}

const (
	QuotaPeriodNone = "none"
	QuotaPeriodWeek = "week"
)

type ModelRoute struct {
	CredentialID string `json:"credential_id"`
	Priority     int    `json:"priority"`
	Weight       int    `json:"weight"`
	Enabled      bool   `json:"enabled"`
}

type ModelDef struct {
	Name          string       `json:"name"`
	Strategy      string       `json:"strategy"`
	UpstreamModel string       `json:"upstream_model"`
	Enabled       bool         `json:"enabled"`
	Routes        []ModelRoute `json:"routes"`
	Price         *Price       `json:"price,omitempty"`
}

type UsageEvent struct {
	ID               string    `json:"id"`
	TS               time.Time `json:"ts"`
	TenantID         string    `json:"tenant_id"`
	ApiKeyID         string    `json:"api_key_id"`
	CredentialID     string    `json:"credential_id"`
	Model            string    `json:"model"`
	UpstreamModel    string    `json:"upstream_model"`
	PromptTokens     int64     `json:"prompt_tokens"`
	CompletionTokens int64     `json:"completion_tokens"`
	CacheReadTokens  int64     `json:"cache_read_tokens"`
	CacheWriteTokens int64     `json:"cache_write_tokens"`
	CostUSD          float64   `json:"cost_usd"`
	Priced           bool      `json:"priced"`
	CacheHit         bool      `json:"cache_hit"`
	StatusCode       int       `json:"status_code"`
	DurationMS       int64     `json:"duration_ms"`
	Error            string    `json:"error"`
	ActorType        string    `json:"actor_type"`
	UserID           string    `json:"user_id"`
	Username         string    `json:"username"`
	OrganizationID   string    `json:"organization_id"`
}

type RecentEvent struct {
	ID               string    `json:"id"`
	TS               time.Time `json:"ts"`
	TenantID         string    `json:"tenant_id"`
	KeyID            string    `json:"api_key_id"`
	CredentialID     string    `json:"credential_id"`
	Model            string    `json:"model"`
	UpstreamModel    string    `json:"upstream_model"`
	PromptTokens     int64     `json:"prompt_tokens"`
	CompletionTokens int64     `json:"completion_tokens"`
	CacheReadTokens  int64     `json:"cache_read_tokens"`
	CacheWriteTokens int64     `json:"cache_write_tokens"`
	CostUSD          float64   `json:"cost_usd"`
	Priced           bool      `json:"priced"`
	CacheHit         bool      `json:"cache_hit"`
	StatusCode       int       `json:"status_code"`
	DurationMS       int64     `json:"duration_ms"`
	Error            string    `json:"error"`
	ActorType        string    `json:"actor_type"`
	UserID           string    `json:"user_id"`
	Username         string    `json:"username"`
	OrganizationID   string    `json:"organization_id"`
}

type UsageSummary struct {
	Requests     int64             `json:"requests"`
	CacheHits    int64             `json:"cache_hits"`
	CostUSD      float64           `json:"cost_usd"`
	PromptTok    int64             `json:"prompt_tokens"`
	CompletionTo int64             `json:"completion_tokens"`
	CacheReadTok int64             `json:"cache_read_tokens"`
	Unpriced     int64             `json:"unpriced_requests"`
	ByModel      map[string]ModelU `json:"by_model"`
	ByKey        map[string]KeyU   `json:"by_key"`
}

type ModelU struct {
	Requests int64   `json:"requests"`
	CostUSD  float64 `json:"cost_usd"`
	InTok    int64   `json:"in_tokens"`
	OutTok   int64   `json:"out_tokens"`
}

type KeyU struct {
	Requests int64   `json:"requests"`
	CostUSD  float64 `json:"cost_usd"`
}
