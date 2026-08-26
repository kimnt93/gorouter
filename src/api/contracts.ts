export interface UsageEvent {
  id: string
  ts: string
  tenant_id: string
  api_key_id: string
  credential_id: string
  model: string
  upstream_model: string
  prompt_tokens: number
  completion_tokens: number
  cache_read_tokens: number
  cache_write_tokens: number
  cost_usd: number
  priced: boolean
  cache_hit: boolean
  status_code: number
  duration_ms: number
  actor_type: string
  user_id: string
  username: string
  organization_id: string
}

export interface UsageRecentResponse {
  object: 'list'
  data: UsageEvent[]
  next_cursor?: string
}

export interface UsageActivityBucket {
  start: string
  requests: number
  prompt_tokens: number
  completion_tokens: number
  cache_read_tokens: number
  cache_write_tokens: number
  cost_usd: number
}

export interface UsageActivityResponse {
  group_by: GroupBy
  data: UsageActivityBucket[]
}

export interface User {
  id: string
  username: string
  status: string
  created_at: string
  updated_at: string
}

export interface APIKey {
  id: string
  tenant_id: string
  name: string
  key_prefix: string
  models: string[]
  scopes: string[]
  quota_usd: number | null
  quota_period: string
  rpm: number | null
  owner_type: string
  owner_user_id?: string
  owner_organization_id?: string
  context_organization_id?: string
  enabled: boolean
  created_at: string
}

export interface CreatedAPIKey extends APIKey { plaintext: string }

export interface Session {
  ok: boolean
  role: string
  principal_type: string
  user_id?: string
  username?: string
  organization_id?: string
  membership_role?: string
  scopes: string[]
}

export interface Organization { id: string; name: string; status: string; created_at: string; updated_at: string }
export interface Membership { organization_id: string; user_id: string; role: 'member' | 'admin'; created_at: string }
export interface AuditEvent { id: string; ts: string; actor_type: string; actor_id: string; actor_label: string; organization_id: string; action: string; target_type: string; target_id: string; safe_metadata: Record<string, string> }

export interface ProviderDefinition {
  id: string; name: string; description: string; auth: 'api_key' | 'oauth'; protocol: string
  default_base_url: string; model_prefix: string; custom_base_url: boolean; oauth_supported: boolean; oauth_refresh_required: boolean
}

export interface Credential {
  id: string; name: string; provider: string; kind: string; base_url: string; status: string
  key_preview?: string; owner_tenant_id: string | null; created_at: string
}

export interface ConnectivityResult { ok: boolean; status?: number; latency_ms: number }
export interface ProviderModel { id: string; public_id: string; name?: string; owned_by?: string; context_length?: number; default?: boolean }
export interface ProviderModelsResponse { object: 'list'; provider: string; default_model?: string; data: ProviderModel[] }
export interface OAuthStartResponse { flow_id: string; flow_type: string; authorize_url: string; verification_uri?: string; verification_uri_complete?: string; user_code?: string; interval?: number; expires_in?: number; instructions: string }
export interface OAuthCompleteResponse { id?: string; provider?: string; name?: string; status?: string }

export interface ModelRoute { credential_id: string; priority: number; weight: number; enabled: boolean }
export interface Price { input_per_m: number; output_per_m: number; cached_input_per_m: number; cache_write_per_m: number }
export interface ModelDefinition { name: string; strategy: string; upstream_model: string; enabled: boolean; routes: ModelRoute[]; price?: Price }

export interface UserCreateResponse { user: User; initial_key?: CreatedAPIKey }
export interface AuditFilters { organizationId: string; actorId: string; action: string; targetType: string; targetId: string; since: string; until: string }

export interface ListResponse<T> {
  object: 'list'
  data: T[]
  next_cursor?: string
}

export interface RouterCacheStats {
  hits?: number
  misses?: number
  entries?: number
  [key: string]: number | undefined
}

export type RangePreset = '1d' | '7d' | '30d' | '90d' | 'ytd' | 'all' | 'custom'
export type GroupBy = 'hour' | 'day' | 'week'

export interface UsageFilters {
  range: RangePreset
  groupBy: GroupBy
  userId: string
  apiKeyId: string
  since: string
  until: string
  organizationId?: string
  model?: string
  status?: string
}
