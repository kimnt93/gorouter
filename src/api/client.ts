import type { APIKey, ListResponse, RouterCacheStats, UsageActivityResponse, UsageFilters, UsageRecentResponse, User } from './contracts'

export class APIError extends Error {
  constructor(readonly status: number, message: string) {
    super(message)
  }
}

async function request<T>(path: string): Promise<T> {
  const response = await fetch(path, { credentials: 'include', headers: { Accept: 'application/json' } })
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

function applyFilters(params: URLSearchParams, filters: UsageFilters): void {
  params.set('range', filters.range)
  params.set('group_by', filters.groupBy)
  if (filters.userId) params.set('user_id', filters.userId)
  if (filters.apiKeyId) params.set('api_key_id', filters.apiKeyId)
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
  if (filters.userId) params.set('user_id', filters.userId)
  if (filters.apiKeyId) params.set('api_key_id', filters.apiKeyId)
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

export const getUsers = (): Promise<ListResponse<User>> => request('/admin/users?limit=500')
export const getAPIKeys = (): Promise<ListResponse<APIKey>> => request('/admin/api-keys?limit=500')
export const getRouterCacheStats = (): Promise<RouterCacheStats> => request('/admin/cache/stats')
