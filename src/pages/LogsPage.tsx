import { useEffect, useState } from 'react'
import { getRecent } from '../api/client'
import type { UsageEvent } from '../api/contracts'
import { Modal } from '../components/Modal'
import { PageError, PageLoading } from '../components/PageState'
import { RangeSelector } from '../components/RangeSelector'
import { TokenBreakdown } from '../components/TokenBreakdown'
import { useUsageFilters } from '../hooks/useUsageFilters'
import { formatDateTime, formatInteger, formatUSD, relativeTime } from '../lib/format'

export function LogsPage() {
  const filterState = useUsageFilters()
  const [events, setEvents] = useState<UsageEvent[]>([])
  const [cursor, setCursor] = useState('')
  const [selected, setSelected] = useState<UsageEvent | null>(null)
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
    <header className="page-header"><div><span className="eyebrow">Operations / Logs</span><h1>Request logs</h1><p>Safe request metadata only. Prompts, completions, credentials, and secrets are never displayed.</p></div><span className="live-indicator"><i /> Live data</span></header>
    <RangeSelector {...filterState} onChange={filterState.setFilters} />
    <div className="secondary-filters"><label className="select-field"><span>Model</span><select value={filterState.filters.model ?? ''} onChange={(event) => filterState.setFilters({ ...filterState.filters, model: event.target.value })}><option value="">All models</option>{filterState.models.map((model) => <option value={model.name} key={model.name}>{model.name}</option>)}</select></label><label className="select-field small"><span>Status</span><select value={filterState.filters.status ?? ''} onChange={(event) => filterState.setFilters({ ...filterState.filters, status: event.target.value })}><option value="">All statuses</option><option value="200">200</option><option value="400">400</option><option value="401">401</option><option value="403">403</option><option value="429">429</option><option value="500">500</option><option value="502">502</option><option value="503">503</option></select></label></div>
    {loading ? <PageLoading /> : error ? <PageError message={error} retry={() => setVersion((value) => value + 1)} /> : <section className="panel table-panel"><div className="table-toolbar"><span>{formatInteger(events.length)} recent requests</span><span className="token-order">tokens = [in / out / cache read / cache write]</span></div><div className="table-scroll"><table className="logs-table"><thead><tr><th>Status</th><th>Model</th><th>User</th><th>Tokens [I/O/CR/CW]</th><th>Cost</th><th>Latency</th><th>Time</th></tr></thead><tbody>{events.map((event) => <tr key={event.id} tabIndex={0} onClick={() => setSelected(event)} onKeyDown={(key) => { if (key.key === 'Enter' || key.key === ' ') setSelected(event) }}><td><span className={event.status_code >= 200 && event.status_code < 400 ? 'status success' : 'status failure'}>{event.status_code}</span></td><td><strong>{event.model}</strong><small>{event.upstream_model || 'direct'}</small></td><td>{event.username || event.actor_type || 'legacy'}<small>{event.organization_id ? `org · ${event.organization_id.slice(0, 10)}` : 'personal'}</small></td><td><TokenBreakdown compact input={event.prompt_tokens} output={event.completion_tokens} cacheRead={event.cache_read_tokens} cacheWrite={event.cache_write_tokens} /></td><td>{event.priced ? formatUSD(event.cost_usd) : 'Unpriced'}</td><td>{formatInteger(event.duration_ms)} ms</td><td><time dateTime={event.ts} title={formatDateTime(event.ts)}>{relativeTime(event.ts)}</time></td></tr>)}</tbody></table></div>{events.length === 0 && <div className="empty-state"><strong>No matching requests</strong><span>Try changing your range or filters.</span></div>}{cursor && <div className="load-more"><button className="button secondary" onClick={loadMore}>Load older requests</button></div>}</section>}
    {selected && <UsageDetail event={selected} onClose={() => setSelected(null)} />}
  </>
}

function UsageDetail({ event, onClose }: { event: UsageEvent; onClose: () => void }) {
  const fields = [
    ['Request ID', event.id], ['Timestamp', formatDateTime(event.ts)], ['Status', String(event.status_code)], ['Duration', `${formatInteger(event.duration_ms)} ms`],
    ['Model', event.model], ['Upstream model', event.upstream_model || '—'], ['Actor', event.username || event.actor_type || 'legacy'], ['Organization', event.organization_id || 'Personal'],
    ['API key ID', event.api_key_id], ['Credential ID', event.credential_id], ['Pricing', event.priced ? formatUSD(event.cost_usd) : 'Unpriced'], ['Provider cache hit', event.cache_hit ? 'Yes' : 'No'],
  ]
  return <Modal title={event.id} onClose={onClose}><TokenBreakdown input={event.prompt_tokens} output={event.completion_tokens} cacheRead={event.cache_read_tokens} cacheWrite={event.cache_write_tokens} /><dl className="detail-grid">{fields.map(([label, value]) => <div key={label}><dt>{label}</dt><dd>{value}</dd></div>)}</dl><div className="safe-note"><strong>Privacy boundary</strong><span>This view intentionally excludes prompts, completions, secret material, cookies, and raw provider errors.</span></div></Modal>
}
