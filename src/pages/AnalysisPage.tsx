import { useMemo } from 'react'
import { VerticalUsageChart } from '../components/VerticalUsageChart'
import { PageError, PageLoading } from '../components/PageState'
import { RangeSelector } from '../components/RangeSelector'
import { StatCard } from '../components/StatCard'
import { useActivity } from '../hooks/useActivity'
import { useUsageFilters } from '../hooks/useUsageFilters'
import { formatInteger, formatUSD } from '../lib/format'

export function AnalysisPage() {
  const filterState = useUsageFilters()
  const activity = useActivity(filterState.filters)
  const total = useMemo(() => activity.summary ? ({ requests: activity.summary.requests, tokens: activity.summary.prompt_tokens + activity.summary.completion_tokens + activity.summary.cache_read_tokens + activity.summary.cache_write_tokens, cache: activity.summary.cache_read_tokens, cost: activity.summary.cost_usd }) : ({ requests: 0, tokens: 0, cache: 0, cost: 0 }), [activity.summary])
  return <>
    <header className="page-header"><div><span className="eyebrow">Operations / Analysis</span><h1>Activity trend</h1><p>Explore request and token volume across the users and API keys you can see.</p></div><a className="button secondary" href="/dashboard/logs">View request logs</a></header>
    <RangeSelector {...filterState} onChange={filterState.setFilters} />
    {activity.loading ? <PageLoading /> : activity.error ? <PageError message={activity.error} retry={activity.retry} /> : <>
      <section className="stat-grid"><StatCard label="Requests" value={formatInteger(total.requests)} detail="completed gateway requests" accent="purple" /><StatCard label="Tokens" value={formatInteger(total.tokens)} detail="input + output + provider cache" accent="blue" /><StatCard label="Cache reads" value={formatInteger(total.cache)} detail="tokens reported by providers" accent="green" /><StatCard label="Estimated cost" value={formatUSD(total.cost)} detail="priced request total" accent="amber" /></section>
      <section className="panel"><div className="panel-header"><div><span className="eyebrow">Throughput · input, output, cache read, and cache write</span><h2>Tokens by {filterState.filters.groupBy}</h2></div></div><VerticalUsageChart data={activity.data} metric="tokens" /></section>
      <section className="panel"><div className="panel-header"><div><span className="eyebrow">Estimated spend</span><h2>Cost activity</h2></div></div><VerticalUsageChart data={activity.data} metric="cost" /></section>
      <ModelBreakdown summary={activity.summary?.by_model ?? {}} />
    </>}
  </>
}

function ModelBreakdown({ summary }: { summary: NonNullable<ReturnType<typeof useActivity>['summary']>['by_model'] }) {
  const rows = Object.entries(summary).map(([model, usage]) => ({ model, usage, total: usage.in_tokens + usage.out_tokens + usage.cache_read_tokens + usage.cache_write_tokens })).sort((a, b) => b.total - a.total)
  const allTokens = rows.reduce((sum, row) => sum + row.total, 0)
  return <section className="panel model-breakdown"><div className="panel-header"><div><span className="eyebrow">Selected interval</span><h2>Model breakdown</h2></div></div><div className="table-scroll"><table><thead><tr><th>Model</th><th>Requests</th><th>Input</th><th>Output</th><th>Cache read</th><th>Cache write</th><th>Total</th><th>Cost</th><th>Breakdown</th></tr></thead><tbody>{rows.map(({ model, usage, total }) => { const share = allTokens ? total / allTokens * 100 : 0; return <tr key={model}><td><strong>{model}</strong></td><td>{formatInteger(usage.requests)}</td><td>{formatInteger(usage.in_tokens)}</td><td>{formatInteger(usage.out_tokens)}</td><td>{formatInteger(usage.cache_read_tokens)}</td><td>{formatInteger(usage.cache_write_tokens)}</td><td><strong>{formatInteger(total)}</strong></td><td>{formatUSD(usage.cost_usd)}</td><td><div className="share-cell"><span><i style={{ width: `${Math.min(100, share)}%` }} /></span><strong>{share.toFixed(1)}%</strong></div></td></tr> })}</tbody></table></div>{rows.length === 0 && <div className="empty-state"><strong>No model activity</strong><span>No matching model usage was recorded in this range.</span></div>}</section>
}
