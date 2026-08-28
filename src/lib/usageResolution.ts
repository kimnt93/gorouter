import type { GroupBy, UsageFilters } from '../api/contracts'

const day = 86_400_000

export function groupByForUsageRange(filters: Pick<UsageFilters, 'range' | 'since' | 'until'>): GroupBy {
  switch (filters.range) {
    case '1d':
      return 'hour'
    case '7d':
    case '30d':
    case '90d':
      return 'day'
    case 'ytd':
    case 'all':
      return 'week'
    case 'custom': {
      const since = Date.parse(filters.since)
      const until = Date.parse(filters.until)
      if (!Number.isFinite(since) || !Number.isFinite(until) || until <= since) return 'day'
      const duration = until - since
      if (duration <= 2 * day) return 'hour'
      if (duration <= 90 * day) return 'day'
      return 'week'
    }
  }
}

export function withAutomaticGroupBy(filters: UsageFilters): UsageFilters {
  return { ...filters, groupBy: groupByForUsageRange(filters) }
}
