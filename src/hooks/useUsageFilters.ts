import { useEffect, useState } from 'react'
import { getAPIKeys, getModels, getOrganizations, getUsers } from '../api/client'
import type { APIKey, ModelDefinition, Organization, UsageFilters, User } from '../api/contracts'

const now = new Date()
const sevenDaysAgo = new Date(now.getTime() - 7 * 86_400_000)
const localInput = (date: Date) => new Date(date.getTime() - date.getTimezoneOffset() * 60_000).toISOString().slice(0, 16)

export const defaultFilters: UsageFilters = { range: '7d', groupBy: 'day', filterType: 'user', userIds: [], apiKeyIds: [], organizationIds: [], since: localInput(sevenDaysAgo), until: localInput(now) }

export function useUsageFilters() {
  const [filters, setFilters] = useState<UsageFilters>(() => {
    const organizationID = new URLSearchParams(window.location.search).get('organization_id') ?? ''
    return { ...defaultFilters, filterType: organizationID ? 'organization' : 'user', organizationIds: organizationID ? [organizationID] : [] }
  })
  const [users, setUsers] = useState<User[]>([])
  const [apiKeys, setAPIKeys] = useState<APIKey[]>([])
  const [organizations, setOrganizations] = useState<Organization[]>([])
  const [models, setModels] = useState<ModelDefinition[]>([])
  useEffect(() => {
    void getUsers().then((response) => setUsers(response.data)).catch(() => setUsers([]))
    void getAPIKeys().then((response) => setAPIKeys(response.data)).catch(() => setAPIKeys([]))
    void getOrganizations().then((response) => setOrganizations(response.data)).catch(() => setOrganizations([]))
    void getModels().then(setModels).catch(() => setModels([]))
  }, [])
  return { filters, setFilters, users, apiKeys, organizations, models }
}
