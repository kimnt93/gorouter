package handlers

import (
	"encoding/json"

	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/provider"
)

type OKResponse struct {
	OK bool `json:"ok"`
}
type HealthResponse struct {
	OK bool `json:"ok"`
}
type ProviderListResponse struct {
	Data []provider.Definition `json:"data"`
}
type LoginRequest struct {
	Key string `json:"key"`
}
type LoginResponse struct {
	OK             bool     `json:"ok"`
	Role           string   `json:"role"`
	PrincipalType  string   `json:"principal_type"`
	UserID         string   `json:"user_id,omitempty"`
	Username       string   `json:"username,omitempty"`
	OrganizationID string   `json:"organization_id,omitempty"`
	MembershipRole string   `json:"membership_role,omitempty"`
	Scopes         []string `json:"scopes"`
}
type TenantCreateRequest struct {
	Name string `json:"name"`
}
type CredentialCreateRequest struct {
	Name                string `json:"name"`
	Provider            string `json:"provider"`
	Kind                string `json:"kind"`
	BaseURL             string `json:"base_url"`
	APIKey              string `json:"api_key"`
	OAuthAccess         string `json:"oauth_access"`
	OAuthRefresh        string `json:"oauth_refresh"`
	OwnerType           string `json:"owner_type,omitempty"`
	OwnerUserID         string `json:"owner_user_id,omitempty"`
	OwnerOrganizationID string `json:"owner_organization_id,omitempty"`
}
type CredentialUpdateRequest struct {
	Name         string `json:"name"`
	BaseURL      string `json:"base_url"`
	Status       string `json:"status"`
	APIKey       string `json:"api_key"`
	OAuthAccess  string `json:"oauth_access"`
	OAuthRefresh string `json:"oauth_refresh"`
}
type APIKeyCreateRequest struct {
	TenantID              string   `json:"tenant_id"`
	Name                  string   `json:"name"`
	Models                []string `json:"models"`
	Scopes                []string `json:"scopes"`
	QuotaUSD              *float64 `json:"quota_usd"`
	QuotaPeriod           string   `json:"quota_period"`
	RPM                   *int     `json:"rpm"`
	OwnerType             string   `json:"owner_type"`
	OwnerUserID           string   `json:"owner_user_id"`
	OwnerOrganizationID   string   `json:"owner_organization_id"`
	ContextOrganizationID string   `json:"context_organization_id"`
}
type APIKeyPatchRequest struct {
	Enabled     *bool     `json:"enabled"`
	Models      *[]string `json:"models"`
	Scopes      *[]string `json:"scopes"`
	QuotaUSD    **float64 `json:"quota_usd"`
	QuotaPeriod *string   `json:"quota_period"`
	RPM         **int     `json:"rpm"`
}
type CreatedAPIKeyResponse struct {
	ID                    string   `json:"id"`
	TenantID              string   `json:"tenant_id"`
	Name                  string   `json:"name"`
	KeyPrefix             string   `json:"key_prefix"`
	Models                []string `json:"models"`
	Scopes                []string `json:"scopes"`
	QuotaUSD              *float64 `json:"quota_usd"`
	QuotaPeriod           string   `json:"quota_period"`
	RPM                   *int     `json:"rpm"`
	Enabled               bool     `json:"enabled"`
	Plaintext             string   `json:"plaintext"`
	OwnerType             string   `json:"owner_type"`
	OwnerUserID           string   `json:"owner_user_id,omitempty"`
	OwnerOrganizationID   string   `json:"owner_organization_id,omitempty"`
	ContextOrganizationID string   `json:"context_organization_id,omitempty"`
}
type APIKeyListResponse struct {
	Object     string            `json:"object"`
	Data       []entities.ApiKey `json:"data"`
	NextCursor string            `json:"next_cursor,omitempty"`
}
type APIKeyModelOption struct {
	ID            string         `json:"id"`
	UpstreamModel string         `json:"upstream_model"`
	Price         entities.Price `json:"price"`
	Free          bool           `json:"free"`
}
type APIKeyModelOptionsResponse struct {
	Object string              `json:"object"`
	Data   []APIKeyModelOption `json:"data"`
}
type PricingCatalogResponse struct {
	Data   []entities.CatalogPrice `json:"data"`
	Total  int                     `json:"total"`
	Offset int                     `json:"offset"`
	Limit  int                     `json:"limit"`
}
type PricingEstimateResponse struct {
	Model          string                  `json:"model"`
	UpstreamModel  string                  `json:"upstream_model,omitempty"`
	Price          *entities.Price         `json:"price,omitempty"`
	CacheSupported bool                    `json:"cache_supported"`
	Estimates      entities.PriceEstimates `json:"estimates"`
}
type PriceListResponse struct {
	Prices map[string]entities.Price `json:"prices"`
}
type UsageRecentResponse struct {
	Object     string                 `json:"object"`
	Data       []entities.RecentEvent `json:"data"`
	NextCursor string                 `json:"next_cursor,omitempty"`
}
type UsageActivityResponse struct {
	GroupBy string                         `json:"group_by"`
	Data    []entities.UsageActivityBucket `json:"data"`
	Summary *entities.UsageSummary         `json:"summary"`
}
type UserListResponse struct {
	Object     string          `json:"object"`
	Data       []entities.User `json:"data"`
	NextCursor string          `json:"next_cursor,omitempty"`
}
type OrganizationListResponse struct {
	Object     string                  `json:"object"`
	Data       []entities.Organization `json:"data"`
	NextCursor string                  `json:"next_cursor,omitempty"`
}
type MembershipListResponse struct {
	Object     string                `json:"object"`
	Data       []entities.Membership `json:"data"`
	NextCursor string                `json:"next_cursor,omitempty"`
}
type AuditListResponse struct {
	Object     string                `json:"object"`
	Data       []entities.AuditEvent `json:"data"`
	NextCursor string                `json:"next_cursor,omitempty"`
}
type InitialKeyRequest struct {
	Name   string   `json:"name"`
	Models []string `json:"models"`
	Scopes []string `json:"scopes"`
}
type UserCreateRequest struct {
	Username           string            `json:"username"`
	GenerateInitialKey bool              `json:"generate_initial_key"`
	InitialKey         InitialKeyRequest `json:"initial_key"`
}
type UserCreateResponse struct {
	User       *entities.User         `json:"user"`
	InitialKey *CreatedAPIKeyResponse `json:"initial_key,omitempty"`
}
type UserDetailResponse struct {
	User        *entities.User         `json:"user"`
	Memberships []entities.Membership  `json:"memberships"`
	Keys        []entities.ApiKey      `json:"keys"`
	Usage       *entities.UsageSummary `json:"usage"`
	Recent      []entities.RecentEvent `json:"recent"`
}
type UserStatusRequest struct {
	Status string `json:"status"`
}
type OrganizationCreateRequest struct {
	Name string `json:"name"`
}
type OrganizationUpdateRequest struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}
type OrganizationDetailResponse struct {
	Organization *entities.Organization `json:"organization"`
	Membership   *entities.Membership   `json:"membership,omitempty"`
}
type MembershipCreateRequest struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
}
type MembershipUpdateRequest struct {
	Role string `json:"role"`
}
type ImportModelsRequest struct {
	Models []string `json:"models"`
}
type ImportModelsResponse struct {
	OK       bool     `json:"ok"`
	Imported []string `json:"imported"`
}
type CredentialChatTestRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}
type ProviderModelResponse struct {
	ID                 string            `json:"id"`
	Object             string            `json:"object"`
	PublicID           string            `json:"public_id"`
	Default            bool              `json:"default,omitempty"`
	Created            int64             `json:"created,omitempty"`
	OwnedBy            string            `json:"owned_by,omitempty"`
	Permission         []json.RawMessage `json:"permission"`
	Root               string            `json:"root,omitempty"`
	Parent             *string           `json:"parent"`
	APIFormat          string            `json:"api_format,omitempty"`
	ContextLength      int64             `json:"context_length,omitempty"`
	MaxOutputTokens    int64             `json:"max_output_tokens,omitempty"`
	SupportedEndpoints []string          `json:"supported_endpoints,omitempty"`
	Capabilities       json.RawMessage   `json:"capabilities,omitempty"`
	InputModalities    []string          `json:"input_modalities,omitempty"`
	OutputModalities   []string          `json:"output_modalities,omitempty"`
	MaxInputTokens     int64             `json:"max_input_tokens,omitempty"`
	Name               string            `json:"name,omitempty"`
}
type ProviderModelsResponse struct {
	Object       string                  `json:"object"`
	Provider     string                  `json:"provider"`
	DefaultModel string                  `json:"default_model,omitempty"`
	Data         []ProviderModelResponse `json:"data"`
}
type OAuthPendingResponse struct {
	Status string `json:"status"`
}
type OAuthStartResponse struct {
	FlowID                  string `json:"flow_id"`
	FlowType                string `json:"flow_type"`
	AuthorizeURL            string `json:"authorize_url"`
	VerificationURI         string `json:"verification_uri,omitempty"`
	VerificationURIComplete string `json:"verification_uri_complete,omitempty"`
	UserCode                string `json:"user_code,omitempty"`
	Interval                int    `json:"interval,omitempty"`
	ExpiresIn               int    `json:"expires_in,omitempty"`
	Instructions            string `json:"instructions"`
}
type OAuthCompleteRequest struct {
	FlowID              string `json:"flow_id"`
	Callback            string `json:"callback"`
	Name                string `json:"name"`
	OwnerType           string `json:"owner_type,omitempty"`
	OwnerOrganizationID string `json:"owner_organization_id,omitempty"`
}
type OAuthCompleteResponse struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Name     string `json:"name"`
}
