import { useMemo } from 'react'
import { HorizontalActivityChart } from '../components/HorizontalActivityChart'
import { PageError, PageLoading } from '../components/PageState'
import { RangeSelector } from '../components/RangeSelector'
import { StatCard } from '../components/StatCard'
import { useActivity } from '../hooks/useActivity'
import { useUsageFilters } from '../hooks/useUsageFilters'
import { formatInteger, formatUSD } from '../lib/format'

export function AnalysisPage() {
  const filterState = useUsageFilters()
  const activity = useActivity(filterState.filters)
  const total = useMemo(() => activity.data.reduce((sum, bucket) => ({ requests: sum.requests + bucket.requests, tokens: sum.tokens + bucket.prompt_tokens + bucket.completion_tokens + bucket.cache_read_tokens + bucket.cache_write_tokens, cache: sum.cache + bucket.cache_read_tokens, cost: sum.cost + bucket.cost_usd }), { requests: 0, tokens: 0, cache: 0, cost: 0 }), [activity.data])
  return <>
    <header className="page-header"><div><span className="eyebrow">Operations / Analysis</span><h1>Activity trend</h1><p>Explore request and token volume across the users and API keys you can see.</p></div><a className="button secondary" href="/dashboard/logs">View request logs</a></header>
    <RangeSelector {...filterState} onChange={filterState.setFilters} />
    {activity.loading ? <PageLoading /> : activity.error ? <PageError message={activity.error} retry={activity.retry} /> : <>
      <section className="stat-grid"><StatCard label="Requests" value={formatInteger(total.requests)} detail="completed gateway requests" accent="purple" /><StatCard label="Tokens" value={formatInteger(total.tokens)} detail="input + output + provider cache" accent="blue" /><StatCard label="Cache reads" value={formatInteger(total.cache)} detail="tokens reported by providers" accent="green" /><StatCard label="Estimated cost" value={formatUSD(total.cost)} detail="priced request total" accent="amber" /></section>
      <section className="panel"><div className="panel-header"><div><span className="eyebrow">Selected interval</span><h2>Requests by {filterState.filters.groupBy}</h2></div><span className="legend"><i className="legend-purple" /> requests</span></div><HorizontalActivityChart data={activity.data} /></section>
      <section className="panel"><div className="panel-header"><div><span className="eyebrow">Throughput</span><h2>Total token activity</h2></div><span className="legend"><i className="legend-blue" /> tokens</span></div><HorizontalActivityChart data={activity.data} metric="tokens" /></section>
    </>}
  </>
}
