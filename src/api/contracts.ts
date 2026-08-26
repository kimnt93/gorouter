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
}

export interface APIKey {
  id: string
  name: string
  key_prefix: string
  owner_type: string
  owner_user_id?: string
  owner_organization_id?: string
  enabled: boolean
}

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
}
