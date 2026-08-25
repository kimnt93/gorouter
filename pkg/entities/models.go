package entities

import (
	"errors"
	"time"
)

var ErrNotFound = errors.New("not found")

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
	OwnerTenant  *string
}

type ApiKey struct {
	ID              string    `json:"id"`
	TenantID        string    `json:"tenant_id"`
	TenantName      string    `json:"tenant_name,omitempty"`
	Name            string    `json:"name"`
	SecretHash      string    `json:"-"`
	SecretPrefix    string    `json:"key_prefix"`
	Models          []string  `json:"models"`
	Scopes          []string  `json:"scopes"`
	MonthlyQuotaUSD *float64  `json:"monthly_quota_usd"`
	RPM             *int      `json:"rpm"`
	Enabled         bool      `json:"enabled"`
	CreatedAt       time.Time `json:"created_at"`

	Plaintext string `json:"-"`
}

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
}

type RecentEvent struct {
	TS           time.Time `json:"ts"`
	TenantID     string    `json:"tenant_id"`
	KeyID        string    `json:"api_key_id"`
	CredentialID string    `json:"credential_id"`
	Model        string    `json:"model"`
	CostUSD      float64   `json:"cost_usd"`
	Priced       bool      `json:"priced"`
	CacheHit     bool      `json:"cache_hit"`
	StatusCode   int       `json:"status_code"`
	DurationMS   int64     `json:"duration_ms"`
	Error        string    `json:"error"`
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
