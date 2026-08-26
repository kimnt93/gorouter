import { useEffect, useState } from 'react'
import { getAPIKeys, getUsers } from '../api/client'
import type { APIKey, UsageFilters, User } from '../api/contracts'

const now = new Date()
const sevenDaysAgo = new Date(now.getTime() - 7 * 86_400_000)
const localInput = (date: Date) => new Date(date.getTime() - date.getTimezoneOffset() * 60_000).toISOString().slice(0, 16)

export const defaultFilters: UsageFilters = { range: '7d', groupBy: 'day', userId: '', apiKeyId: '', since: localInput(sevenDaysAgo), until: localInput(now) }

export function useUsageFilters() {
  const [filters, setFilters] = useState(defaultFilters)
  const [users, setUsers] = useState<User[]>([])
  const [apiKeys, setAPIKeys] = useState<APIKey[]>([])
  useEffect(() => {
    void getUsers().then((response) => setUsers(response.data)).catch(() => setUsers([]))
    void getAPIKeys().then((response) => setAPIKeys(response.data)).catch(() => setAPIKeys([]))
  }, [])
  return { filters, setFilters, users, apiKeys }
}
