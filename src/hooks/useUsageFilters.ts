import { useEffect, useState } from 'react'
import { getAPIKeys, getMembers, getModels, getOrganizations, getUsers } from '../api/client'
import type { APIKey, ModelDefinition, Organization, RangePreset, UsageFilters, User } from '../api/contracts'
import { withAutomaticGroupBy } from '../lib/usageResolution'

const day = 86_400_000
const rangePresets = new Set<RangePreset>(['1d', '7d', '30d', '90d', 'ytd', 'all', 'custom'])
const localInput = (date: Date) => new Date(date.getTime() - date.getTimezoneOffset() * 60_000).toISOString().slice(0, 16)

function baseFilters(now = new Date()): UsageFilters {
  return withAutomaticGroupBy({ range: '7d', groupBy: 'hour', filterType: 'user', userIds: [], apiKeyIds: [], organizationIds: [], since: localInput(new Date(now.getTime() - 7 * day)), until: localInput(now) })
}

export const defaultFilters: UsageFilters = baseFilters()

export function usageFiltersFromURL(search: string, now = new Date()): UsageFilters {
  const params = new URLSearchParams(search)
  const defaults = baseFilters(now)
  const requestedRange = params.get('range') as RangePreset | null
  const range = requestedRange && rangePresets.has(requestedRange) ? requestedRange : defaults.range
  const organizationID = params.get('organization_id') ?? ''
  const since = range === 'custom' ? params.get('since') || defaults.since : defaults.since
  const until = range === 'custom' ? params.get('until') || defaults.until : defaults.until
  return withAutomaticGroupBy({ ...defaults, range, since, until, filterType: organizationID ? 'organization' : 'user', organizationIds: organizationID ? [organizationID] : [] })
}

export function usageFiltersURL(filters: UsageFilters, current: string): string {
  const url = new URL(current)
  url.searchParams.set('range', filters.range)
  if (filters.range === 'custom') {
    if (filters.since) url.searchParams.set('since', filters.since); else url.searchParams.delete('since')
    if (filters.until) url.searchParams.set('until', filters.until); else url.searchParams.delete('until')
  } else {
    url.searchParams.delete('since')
    url.searchParams.delete('until')
  }
  return `${url.pathname}${url.search}${url.hash}`
}

export function useUsageFilters() {
  const [filters, setFiltersState] = useState<UsageFilters>(() => usageFiltersFromURL(window.location.search))
  const setFilters = (next: UsageFilters) => {
    const normalized = withAutomaticGroupBy(next)
    window.history.replaceState(window.history.state, '', usageFiltersURL(normalized, window.location.href))
    setFiltersState(normalized)
  }
  const [users, setUsers] = useState<User[]>([])
  const [apiKeys, setAPIKeys] = useState<APIKey[]>([])
  const [organizations, setOrganizations] = useState<Organization[]>([])
  const [models, setModels] = useState<ModelDefinition[]>([])
  useEffect(() => {
    const organizationID = new URLSearchParams(window.location.search).get('organization_id') ?? ''
    if (organizationID) {
      void Promise.all([getUsers().catch(() => ({ object: 'list' as const, data: [] })), getMembers(organizationID).catch(() => ({ object: 'list' as const, data: [] }))]).then(([userResponse, memberResponse]) => {
        const byID = new Map(userResponse.data.map((user) => [user.id, user]))
        setUsers(memberResponse.data.map((membership) => byID.get(membership.user_id) ?? { id: membership.user_id, username: membership.username ?? membership.user_id, status: 'active', created_at: membership.created_at, updated_at: membership.created_at }))
      })
    } else void getUsers().then((response) => setUsers(response.data)).catch(() => setUsers([]))
    void getAPIKeys().then((response) => setAPIKeys(response.data)).catch(() => setAPIKeys([]))
    void getOrganizations().then((response) => setOrganizations(organizationID ? response.data.filter((organization) => organization.id === organizationID) : response.data)).catch(() => setOrganizations([]))
    void getModels().then(setModels).catch(() => setModels([]))
  }, [])
  return { filters, setFilters, users, apiKeys, organizations, models }
}
