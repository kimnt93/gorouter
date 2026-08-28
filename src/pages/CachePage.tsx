import { useEffect, useMemo, useState } from 'react'
import { flushRouterCache, getRouterCacheStats } from '../api/client'
import type { RouterCacheStats } from '../api/contracts'
import { CacheEfficiencyChart } from '../components/VerticalUsageChart'
import { PageError, PageLoading } from '../components/PageState'
import { RangeSelector } from '../components/RangeSelector'
import { StatCard } from '../components/StatCard'
import { useActivity } from '../hooks/useActivity'
import { useUsageFilters } from '../hooks/useUsageFilters'
import { formatInteger } from '../lib/format'
import { useSession } from '../context/SessionContext'

export function CachePage() {
  const filterState = useUsageFilters()
  const activity = useActivity(filterState.filters)
  const [routerStats, setRouterStats] = useState<RouterCacheStats>({})
  const [cacheMessage, setCacheMessage] = useState('')
  const { has } = useSession()
  useEffect(() => { void getRouterCacheStats().then(setRouterStats).catch(() => setRouterStats({})) }, [])
  const totals = useMemo(() => activity.data.reduce((sum, bucket) => ({ input: sum.input + bucket.prompt_tokens, read: sum.read + bucket.cache_read_tokens, write: sum.write + bucket.cache_write_tokens, requests: sum.requests + bucket.requests }), { input: 0, read: 0, write: 0, requests: 0 }), [activity.data])
  const denominator = totals.input + totals.read
  const rate = Math.min(100, denominator > 0 ? totals.read / denominator * 100 : 0)
  return <>
    <header className="page-header"><div><span className="eyebrow">Operations / Provider cache</span><h1>Prompt cache</h1><p>Cache read and write tokens reported by upstream providers—not gorouter response-cache guesses.</p></div><span className="provider-badge">Provider reported</span></header>
    <RangeSelector {...filterState} onChange={filterState.setFilters} />
    {activity.loading ? <PageLoading /> : activity.error ? <PageError message={activity.error} retry={activity.retry} /> : <>
      <section className="stat-grid"><StatCard label="Cache read" value={formatInteger(totals.read)} detail="reused provider-side tokens" accent="green" /><StatCard label="Cache write" value={formatInteger(totals.write)} detail="tokens written to provider cache" accent="amber" /><StatCard label="Read share" value={`${rate.toFixed(1)}%`} detail={`${formatInteger(totals.read)} / ${formatInteger(denominator)} context tokens`} accent="purple" /><StatCard label="Requests" value={formatInteger(totals.requests)} detail="requests in selected range" accent="blue" /></section>
      <section className="panel"><div className="panel-header"><div><span className="eyebrow">Provider-side cache · volume and efficiency</span><h2>Cache-read share by {filterState.filters.groupBy}</h2><p>Bar height compares total usage tokens; the green split and label show cache-read share of input context. Hover for exact totals.</p></div></div><CacheEfficiencyChart data={activity.data} groupBy={filterState.filters.groupBy} range={filterState.filters} /></section>
      <section className="panel router-cache"><div><span className="eyebrow">Separate subsystem</span><h2>gorouter response cache</h2><p>These operational counters belong to the router’s response cache and are intentionally kept separate from provider prompt-cache tokens.</p>{has('cache:purge') && <button className="button danger-button" onClick={() => { if (window.confirm('Flush all gorouter response-cache entries?')) void flushRouterCache().then(() => { setRouterStats({}); setCacheMessage('Response cache flushed') }) }}>Flush response cache</button>}{cacheMessage && <small className="inline-result">{cacheMessage}</small>}</div><dl>{Object.entries(routerStats).filter((entry): entry is [string, number] => typeof entry[1] === 'number').map(([name, value]) => <div key={name}><dt>{name.replaceAll('_', ' ')}</dt><dd>{formatInteger(value)}</dd></div>)}</dl></section>
    </>}
  </>
}
