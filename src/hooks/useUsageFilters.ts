import { useEffect, useState } from 'react'
import { getAPIKeys, getMembers, getModels, getOrganizations, getUsers } from '../api/client'
import type { APIKey, ModelDefinition, Organization, UsageFilters, User } from '../api/contracts'
import { withAutomaticGroupBy } from '../lib/usageResolution'

const now = new Date()
const sevenDaysAgo = new Date(now.getTime() - 7 * 86_400_000)
const localInput = (date: Date) => new Date(date.getTime() - date.getTimezoneOffset() * 60_000).toISOString().slice(0, 16)

export const defaultFilters: UsageFilters = { range: '7d', groupBy: 'day', filterType: 'user', userIds: [], apiKeyIds: [], organizationIds: [], since: localInput(sevenDaysAgo), until: localInput(now) }

export function useUsageFilters() {
  const [filters, setFiltersState] = useState<UsageFilters>(() => {
    const organizationID = new URLSearchParams(window.location.search).get('organization_id') ?? ''
    return { ...defaultFilters, filterType: organizationID ? 'organization' : 'user', organizationIds: organizationID ? [organizationID] : [] }
  })
  const setFilters = (next: UsageFilters) => setFiltersState(withAutomaticGroupBy(next))
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
