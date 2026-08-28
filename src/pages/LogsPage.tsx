import { useEffect, useState } from 'react'
import { getRecent } from '../api/client'
import type { UsageEvent } from '../api/contracts'
import { PageError, PageLoading } from '../components/PageState'
import { RangeSelector } from '../components/RangeSelector'
import { TokenBreakdown } from '../components/TokenBreakdown'
import { SearchableSelect, TruncatedText } from '../components/SearchableSelect'
import { useUsageFilters } from '../hooks/useUsageFilters'
import { formatDateTime, formatInteger, formatUSD, relativeTime } from '../lib/format'

export function LogsPage() {
  const filterState = useUsageFilters()
  const [events, setEvents] = useState<UsageEvent[]>([])
  const [cursor, setCursor] = useState('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [version, setVersion] = useState(0)
  useEffect(() => {
    let live = true
    setLoading(true); setError('')
    void getRecent(filterState.filters).then((response) => { if (live) { setEvents(response.data); setCursor(response.next_cursor ?? '') } }).catch((reason: Error) => { if (live) setError(reason.message) }).finally(() => { if (live) setLoading(false) })
    return () => { live = false }
  }, [filterState.filters, version])
  const loadMore = () => void getRecent(filterState.filters, cursor).then((response) => { setEvents((current) => [...current, ...response.data]); setCursor(response.next_cursor ?? '') }).catch((reason: Error) => setError(reason.message))

  return <>
    <header className="page-header"><div><span className="eyebrow">Operations / Logs</span><h1>Request logs</h1><p>Inspect safe request metadata, provider routing, token accounting, cost, and latency. Prompt and completion content is never stored.</p></div><span className="live-indicator"><i /> Live data</span></header>
    <RangeSelector {...filterState} onChange={filterState.setFilters} />
    <div className="secondary-filters"><label className="select-field"><span>Model</span><SearchableSelect value={filterState.filters.model ?? ''} onChange={(value) => filterState.setFilters({ ...filterState.filters, model: value })} searchPlaceholder="Search models" options={[{ value: '', label: 'All models' }, ...filterState.models.map((model) => ({ value: model.name, label: model.name, meta: model.upstream_model }))]} /></label><label className="select-field small"><span>Status</span><SearchableSelect value={filterState.filters.status ?? ''} onChange={(value) => filterState.setFilters({ ...filterState.filters, status: value })} options={['', '200', '400', '401', '403', '429', '500', '502', '503'].map((status) => ({ value: status, label: status || 'All statuses' }))} /></label></div>
    {loading ? <PageLoading /> : error ? <PageError message={error} retry={() => setVersion((value) => value + 1)} /> : <section className="panel table-panel"><div className="table-toolbar"><span>{formatInteger(events.length)} loaded requests</span><span className="token-order">tokens = [in / out / cache read / cache write]</span></div><div className="table-scroll"><table className="logs-table"><thead><tr><th>Status</th><th>Model</th><th>Provider</th><th>User</th><th>Tokens [I/O/CR/CW]</th><th>Cost</th><th>Latency</th><th>Time</th></tr></thead><tbody>{events.map((event) => { const actor = event.username || event.actor_type || 'legacy'; return <tr key={event.id}><td><span className={event.status_code >= 200 && event.status_code < 400 ? 'status success' : 'status failure'}>{event.status_code}</span></td><td><strong><TruncatedText>{event.model}</TruncatedText></strong><small title={event.upstream_model || 'direct'}>{event.upstream_model || 'direct'}</small></td><td><strong><TruncatedText>{event.provider || 'unknown'}</TruncatedText></strong><small title={event.credential_id || 'router cache'}>{event.credential_id ? `credential · ${event.credential_id.slice(0, 10)}` : 'router cache'}</small></td><td><TruncatedText>{actor}</TruncatedText><small title={event.organization_id || 'personal'}>{event.organization_id ? `org · ${event.organization_id.slice(0, 10)}` : 'personal'}</small></td><td><TokenBreakdown compact input={event.prompt_tokens} output={event.completion_tokens} cacheRead={event.cache_read_tokens} cacheWrite={event.cache_write_tokens} /></td><td><CostLabel event={event} /></td><td>{formatInteger(event.duration_ms)} ms</td><td><time dateTime={event.ts} title={formatDateTime(event.ts)}>{relativeTime(event.ts)}</time></td></tr> })}</tbody></table></div>{events.length === 0 && <div className="empty-state"><strong>No matching requests</strong><span>Try changing your range or filters.</span></div>}{cursor && <div className="load-more"><button className="button secondary" onClick={loadMore}>Load older requests</button></div>}</section>}
  </>
}

function CostLabel({ event }: { event: Pick<UsageEvent, 'cost_usd' | 'priced'> }) {
  const free = !event.priced || event.cost_usd === 0
  return <span className={free ? 'cost-label free' : 'cost-label'}>{formatUSD(free ? 0 : event.cost_usd)}{free && <em>Free</em>}</span>
}
