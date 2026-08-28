import type { GroupBy, UsageFilters } from '../api/contracts'

const day = 86_400_000

export function groupByForUsageRange(filters: Pick<UsageFilters, 'range' | 'since' | 'until'>): GroupBy {
  switch (filters.range) {
    case '1d':
    case '7d':
      return 'hour'
    case '30d':
    case '90d':
    case 'ytd':
      return 'day'
    case 'all':
      return 'week'
    case 'custom': {
      const since = Date.parse(filters.since)
      const until = Date.parse(filters.until)
      if (!Number.isFinite(since) || !Number.isFinite(until) || until <= since) return 'day'
      const duration = until - since
      if (duration <= 7 * day) return 'hour'
      if (duration <= 366 * day) return 'day'
      return 'week'
    }
  }
}

export function withAutomaticGroupBy(filters: UsageFilters): UsageFilters {
  return { ...filters, groupBy: groupByForUsageRange(filters) }
}
