import { useEffect, useState } from 'react'
import { getActivity } from '../api/client'
import type { UsageActivityBucket, UsageFilters, UsageHealthMetric, UsageSummary } from '../api/contracts'

export function useActivity(filters: UsageFilters) {
  const [data, setData] = useState<UsageActivityBucket[]>([])
  const [summary, setSummary] = useState<UsageSummary | null>(null)
  const [health, setHealth] = useState<UsageHealthMetric[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [version, setVersion] = useState(0)
  useEffect(() => {
    if (filters.range === 'custom' && (!filters.since || !filters.until)) return
    let live = true
    setLoading(true)
    setError('')
    void getActivity(filters).then((response) => { if (live) { setData(response.data); setSummary(response.summary); setHealth(response.health ?? []) } }).catch((reason: Error) => { if (live) setError(reason.message) }).finally(() => { if (live) setLoading(false) })
    return () => { live = false }
  }, [filters, version])
  return { data, summary, health, loading, error, retry: () => setVersion((value) => value + 1) }
}
