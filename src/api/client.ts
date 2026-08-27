import type { APIKey, APIKeyModelOption, AuditEvent, AuditFilters, CatalogPrice, ConnectivityResult, CreatedAPIKey, Credential, ListResponse, Membership, ModelDefinition, OAuthCompleteResponse, OAuthStartResponse, Organization, PricingCatalogResponse, ProviderDefinition, ProviderModelsResponse, ProviderQuotaSnapshot, RouterCacheStats, Session, UsageActivityResponse, UsageDetail, UsageFilters, UsageRecentResponse, User, UserCreateResponse } from './contracts'

export class APIError extends Error {
  constructor(readonly status: number, message: string) {
    super(message)
  }
}

export async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const method = (init.method ?? 'GET').toUpperCase()
  const pageParams = new URLSearchParams(window.location.search)
  const viewOrganization = pageParams.get('organization_id') ?? ''
  const viewUser = pageParams.get('view_user_id') ?? ''
  const viewSensitive = ['/admin/api-keys', '/admin/credentials', '/admin/models', '/admin/audit/', '/admin/organizations', '/admin/usage/']
  if (method === 'GET' && !path.includes('view_catalog=1') && (viewOrganization || viewUser) && viewSensitive.some((prefix) => path.startsWith(prefix))) {
    const url = new URL(path, window.location.origin)
    if (!url.searchParams.has('organization_id')) url.searchParams.set('organization_id', viewOrganization)
    if (viewUser && !url.searchParams.has('view_user_id')) url.searchParams.set('view_user_id', viewUser)
    path = `${url.pathname}${url.search}`
  }
  const headers = new Headers(init.headers)
  headers.set('Accept', 'application/json')
  if (init.body && !headers.has('Content-Type')) headers.set('Content-Type', 'application/json')
  const response = await fetch(path, { ...init, credentials: 'include', headers })
  if (!response.ok) {
    let message = `Request failed (${response.status})`
    try {
      const body = (await response.json()) as { error?: { message?: string }; message?: string }
      message = body.error?.message ?? body.message ?? message
    } catch {
      // The status remains useful when a proxy returns a non-JSON error page.
    }
    throw new APIError(response.status, message)
  }
  return response.json() as Promise<T>
}

export async function requestStream(path: string, body: object, onText: (text: string) => void, signal?: AbortSignal): Promise<void> {
  const response = await fetch(path, { method: 'POST', credentials: 'include', signal, headers: { Accept: 'text/event-stream', 'Content-Type': 'application/json' }, body: JSON.stringify(body) })
  if (!response.ok || !response.body) throw new APIError(response.status, `Request failed (${response.status})`)
  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  while (true) {
    const { done, value } = await reader.read()
    if (done) break
    buffer += decoder.decode(value, { stream: true })
    const events = buffer.split('\n\n')
    buffer = events.pop() ?? ''
    for (const event of events) {
      const data = event.split('\n').filter((line) => line.startsWith('data:')).map((line) => line.slice(5).trim()).join('')
      if (!data || data === '[DONE]') continue
      try {
        const parsed = JSON.parse(data) as { choices?: Array<{ delta?: { content?: string } }> }
        const text = parsed.choices?.[0]?.delta?.content
        if (text) onText(text)
      } catch { onText(data) }
    }
  }
}

function applyFilters(params: URLSearchParams, filters: UsageFilters): void {
  params.set('range', filters.range)
  params.set('group_by', filters.groupBy)
  if (filters.userIds.length) params.set('user_id', filters.userIds.join(','))
  if (filters.apiKeyIds.length) params.set('api_key_id', filters.apiKeyIds.join(','))
  if (filters.organizationIds.length) params.set('organization_id', filters.organizationIds.join(','))
  if (filters.range === 'custom') {
    if (filters.since) params.set('since', new Date(filters.since).toISOString())
    if (filters.until) params.set('until', new Date(filters.until).toISOString())
  }
}

export function getActivity(filters: UsageFilters): Promise<UsageActivityResponse> {
  const params = new URLSearchParams()
  applyFilters(params, filters)
  return request(`/admin/usage/activity?${params}`)
}

export function getRecent(filters: UsageFilters, cursor = ''): Promise<UsageRecentResponse> {
  const params = new URLSearchParams({ limit: '100' })
  if (filters.userIds.length) params.set('user_id', filters.userIds.join(','))
  if (filters.apiKeyIds.length) params.set('api_key_id', filters.apiKeyIds.join(','))
  if (filters.organizationIds.length) params.set('organization_id', filters.organizationIds.join(','))
  if (filters.model) params.set('model', filters.model)
  if (filters.status) params.set('status', filters.status)
  if (cursor) params.set('cursor', cursor)
  const now = new Date()
  const durations: Partial<Record<UsageFilters['range'], number>> = { '1d': 1, '7d': 7, '30d': 30, '90d': 90 }
  const days = durations[filters.range]
  if (days) params.set('since', new Date(now.getTime() - days * 86_400_000).toISOString())
  if (filters.range === 'ytd') params.set('since', new Date(Date.UTC(now.getUTCFullYear(), 0, 1)).toISOString())
  if (filters.range === 'custom') {
    if (filters.since) params.set('since', new Date(filters.since).toISOString())
    if (filters.until) params.set('until', new Date(filters.until).toISOString())
  }
  return request(`/admin/usage/recent?${params}`)
}

export const getUsageDetail = (id: string, organizationId = ''): Promise<UsageDetail> => {
  const params = organizationId ? `?organization_id=${encodeURIComponent(organizationId)}` : ''
  return request(`/admin/usage/events/${encodeURIComponent(id)}${params}`)
}

const normalizeList = <T>(response: ListResponse<T>): ListResponse<T> => ({ ...response, data: response.data ?? [] })
export const getUsers = (email = '', organizationId = ''): Promise<ListResponse<User>> => {
  const params = new URLSearchParams({ limit: '500' })
  if (email.trim()) params.set('q', email.trim())
  if (organizationId) params.set('organization_id', organizationId)
  return request<ListResponse<User>>(`/admin/users?${params}`).then(normalizeList)
}
export const getAPIKeys = (): Promise<ListResponse<APIKey>> => request<ListResponse<APIKey>>('/admin/api-keys?limit=500').then(normalizeList)
export const getAPIKeyModels = (organizationId = ''): Promise<ListResponse<APIKeyModelOption>> => {
  const query = organizationId ? `?organization_id=${encodeURIComponent(organizationId)}` : ''
  return request<ListResponse<APIKeyModelOption>>(`/admin/api-keys/models${query}`).then(normalizeList)
}
export const getRouterCacheStats = (): Promise<RouterCacheStats> => request('/admin/cache/stats')
export const getSession = (): Promise<Session> => request('/admin/session')
export const getOrganizations = (viewCatalog = false): Promise<ListResponse<Organization>> => request<ListResponse<Organization>>(`/admin/organizations?limit=500${viewCatalog ? '&view_catalog=1' : ''}`).then(normalizeList)
export const createOrganization = (name: string): Promise<Organization> => request('/admin/organizations', { method: 'POST', body: JSON.stringify({ name }) })
export const updateOrganization = (id: string, name: string, status: string): Promise<Organization> => request(`/admin/organizations/${encodeURIComponent(id)}`, { method: 'PATCH', body: JSON.stringify({ name, status }) })
export const getMembers = (id: string): Promise<ListResponse<Membership>> => request(`/admin/organizations/${encodeURIComponent(id)}/members`)
export const addMember = (id: string, userId: string, role: string): Promise<Membership> => request(`/admin/organizations/${encodeURIComponent(id)}/members`, { method: 'POST', body: JSON.stringify({ user_id: userId, role }) })
export const updateMember = (id: string, userId: string, role: string): Promise<{ ok: boolean }> => request(`/admin/organizations/${encodeURIComponent(id)}/members/${encodeURIComponent(userId)}`, { method: 'PATCH', body: JSON.stringify({ role }) })
export const deleteMember = (id: string, userId: string): Promise<{ ok: boolean }> => request(`/admin/organizations/${encodeURIComponent(id)}/members/${encodeURIComponent(userId)}`, { method: 'DELETE' })

export const createUser = (username: string, generateInitialKey: boolean): Promise<UserCreateResponse> => request('/admin/users', { method: 'POST', body: JSON.stringify({ username, generate_initial_key: generateInitialKey, initial_key: { name: 'Initial login key', models: [], scopes: ['usage:read'] } }) })
export const setUserStatus = (id: string, status: string): Promise<{ ok: boolean }> => request(`/admin/users/${encodeURIComponent(id)}`, { method: 'PATCH', body: JSON.stringify({ status }) })

export const createAPIKey = (body: object): Promise<CreatedAPIKey> => request('/admin/api-keys', { method: 'POST', body: JSON.stringify(body) })
export const patchAPIKey = (id: string, body: object): Promise<{ ok: boolean }> => request(`/admin/api-keys/${encodeURIComponent(id)}`, { method: 'PATCH', body: JSON.stringify(body) })
export const rotateAPIKey = (id: string): Promise<CreatedAPIKey> => request(`/admin/api-keys/${encodeURIComponent(id)}/rotate`, { method: 'POST' })
export const deleteAPIKey = (id: string): Promise<{ ok: boolean }> => request(`/admin/api-keys/${encodeURIComponent(id)}`, { method: 'DELETE' })

export const getProviders = (): Promise<{ data: ProviderDefinition[] }> => request('/admin/providers')
export const getCredentials = (): Promise<Credential[]> => request<Credential[]>('/admin/credentials').then((items) => items ?? [])
export const createCredential = (body: object): Promise<Credential> => request('/admin/credentials', { method: 'POST', body: JSON.stringify(body) })
export const updateCredential = (id: string, body: object): Promise<Credential> => request(`/admin/credentials/${encodeURIComponent(id)}`, { method: 'PUT', body: JSON.stringify(body) })
export const deleteCredential = (id: string): Promise<{ ok: boolean }> => request(`/admin/credentials/${encodeURIComponent(id)}`, { method: 'DELETE' })
export const testCredential = (id: string): Promise<ConnectivityResult> => request(`/admin/credentials/${encodeURIComponent(id)}/test`, { method: 'POST' })
export const getCredentialQuota = (id: string): Promise<ProviderQuotaSnapshot> => request(`/admin/credentials/${encodeURIComponent(id)}/quota`, { cache: 'no-store' })
export const refreshCredentialQuota = (id: string): Promise<ProviderQuotaSnapshot> => request(`/admin/credentials/${encodeURIComponent(id)}/quota`, { method: 'POST' })
export const discoverModels = (id: string): Promise<ProviderModelsResponse> => request(`/admin/credentials/${encodeURIComponent(id)}/models`)
export const importModels = (id: string, models: string[]): Promise<{ ok: boolean; imported: string[] }> => request(`/admin/credentials/${encodeURIComponent(id)}/models/import`, { method: 'POST', body: JSON.stringify({ models }) })
export const startOAuth = (provider: string): Promise<OAuthStartResponse> => request(`/admin/oauth/${encodeURIComponent(provider)}/start`, { method: 'POST' })
export const completeOAuth = (provider: string, body: object): Promise<OAuthCompleteResponse> => request(`/admin/oauth/${encodeURIComponent(provider)}/complete`, { method: 'POST', body: JSON.stringify(body) })

export const getModels = (): Promise<ModelDefinition[]> => request<ModelDefinition[]>('/admin/models').then((items) => items ?? [])
export const saveModel = (model: ModelDefinition): Promise<{ ok: boolean }> => request(`/admin/models/${encodeURIComponent(model.name)}`, { method: 'PUT', body: JSON.stringify(model) })
export const deleteModel = (name: string): Promise<{ ok: boolean }> => request(`/admin/models/${encodeURIComponent(name)}`, { method: 'DELETE' })
export const savePrice = (name: string, price: object): Promise<{ ok: boolean }> => request(`/admin/prices/${encodeURIComponent(name)}`, { method: 'PUT', body: JSON.stringify(price) })
export const deletePrice = (name: string): Promise<{ ok: boolean }> => request(`/admin/prices/${encodeURIComponent(name)}`, { method: 'DELETE' })
export const getPricingCatalog = async (): Promise<CatalogPrice[]> => {
  const data: CatalogPrice[] = []
  for (let offset = 0; ; offset += 500) {
    const page = await request<PricingCatalogResponse>(`/admin/pricing/catalog?limit=500&offset=${offset}`)
    data.push(...page.data)
    if (data.length >= page.total || page.data.length === 0) return data
  }
}

export const getAuditEvents = (filters: AuditFilters, cursor = ''): Promise<ListResponse<AuditEvent>> => {
  const params = new URLSearchParams({ limit: '100' })
  if (cursor) params.set('cursor', cursor)
  if (filters.organizationId) params.set('organization_id', filters.organizationId)
  if (filters.actorId) params.set('actor_id', filters.actorId)
  if (filters.action) params.set('action', filters.action)
  if (filters.targetType) params.set('target_type', filters.targetType)
  if (filters.targetId) params.set('target_id', filters.targetId)
  if (filters.since) params.set('since', new Date(filters.since).toISOString())
  if (filters.until) params.set('until', new Date(filters.until).toISOString())
  return request<ListResponse<AuditEvent>>(`/admin/audit/events?${params}`).then(normalizeList)
}
export const flushRouterCache = (): Promise<{ ok: boolean }> => request('/admin/cache/flush', { method: 'POST' })
