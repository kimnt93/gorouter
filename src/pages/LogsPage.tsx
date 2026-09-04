import { useEffect, useState } from 'react'
import { getRecent, getUsageDetail } from '../api/client'
import type { ConversationEntry, UsageDetail, UsageEvent } from '../api/contracts'
import { Modal } from '../components/Modal'
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
  const [selected, setSelected] = useState<UsageEvent | null>(null)
  useEffect(() => {
    let live = true
    setLoading(true); setError('')
    void getRecent(filterState.filters).then((response) => { if (live) { setEvents(response.data); setCursor(response.next_cursor ?? '') } }).catch((reason: Error) => { if (live) setError(reason.message) }).finally(() => { if (live) setLoading(false) })
    return () => { live = false }
  }, [filterState.filters, version])
  const loadMore = () => void getRecent(filterState.filters, cursor).then((response) => { setEvents((current) => [...current, ...response.data]); setCursor(response.next_cursor ?? '') }).catch((reason: Error) => setError(reason.message))

  return <>
    <header className="page-header"><div><span className="eyebrow">Operations / Logs</span><h1>Request logs</h1><p>Inspect safe request metadata, provider routing, token accounting, cost, and latency. Prompt and completion content is never stored.</p></div><span className="live-indicator"><i /> Live data</span></header>
    <RangeSelector {...filterState} onChange={filterState.setFilters} showRange={false} />
    <div className="secondary-filters"><label className="select-field"><span>Model</span><SearchableSelect value={filterState.filters.model ?? ''} onChange={(value) => filterState.setFilters({ ...filterState.filters, model: value })} searchPlaceholder="Search models" options={[{ value: '', label: 'All models' }, ...filterState.models.map((model) => ({ value: model.name, label: model.name, meta: model.upstream_model }))]} /></label><label className="select-field small"><span>Status</span><SearchableSelect value={filterState.filters.status ?? ''} onChange={(value) => filterState.setFilters({ ...filterState.filters, status: value })} options={['', '200', '400', '401', '403', '429', '500', '502', '503'].map((status) => ({ value: status, label: status || 'All statuses' }))} /></label></div>
    {loading ? <PageLoading /> : error ? <PageError message={error} retry={() => setVersion((value) => value + 1)} /> : <section className="panel table-panel"><div className="table-toolbar"><span>{formatInteger(events.length)} loaded requests</span><span className="token-order">tokens = [in / out / cache read / cache write]</span></div><div className="table-scroll"><table className="logs-table"><thead><tr><th>Status</th><th>Model</th><th>Provider</th><th>User</th><th>Tokens [I/O/CR/CW]</th><th>Cost</th><th>Latency</th><th>Time</th><th aria-label="Actions" /></tr></thead><tbody>{events.map((event) => { const actor = event.username || event.actor_type || 'legacy'; return <tr key={event.id} tabIndex={0} role="button" aria-label={`View request ${event.id} details`} onClick={() => setSelected(event)} onKeyDown={(keyEvent) => { if (keyEvent.key === 'Enter' || keyEvent.key === ' ') { keyEvent.preventDefault(); setSelected(event) } }}><td><span className={event.status_code >= 200 && event.status_code < 400 ? 'status success' : 'status failure'}>{event.status_code}</span></td><td><strong><TruncatedText>{event.model}</TruncatedText></strong><small title={event.upstream_model || 'direct'}>{event.upstream_model || 'direct'}</small></td><td><strong><TruncatedText>{event.provider || 'unknown'}</TruncatedText></strong><small title={event.credential_id || 'router cache'}>{event.credential_id ? `credential · ${event.credential_id.slice(0, 10)}` : 'router cache'}</small></td><td><TruncatedText>{actor}</TruncatedText><small title={event.organization_id || 'personal'}>{event.organization_id ? `org · ${event.organization_id.slice(0, 10)}` : 'personal'}</small></td><td><TokenBreakdown compact input={event.prompt_tokens} output={event.completion_tokens} cacheRead={event.cache_read_tokens} cacheWrite={event.cache_write_tokens} /></td><td><CostLabel event={event} /></td><td>{formatInteger(event.duration_ms)} ms</td><td><time dateTime={event.ts} title={formatDateTime(event.ts)}>{relativeTime(event.ts)}</time></td><td><button className="log-detail-button" onClick={(clickEvent) => { clickEvent.stopPropagation(); setSelected(event) }}>Details</button></td></tr> })}</tbody></table></div>{events.length === 0 && <div className="empty-state"><strong>No matching requests</strong><span>Try changing the filters.</span></div>}{cursor && <div className="load-more"><button className="button secondary" onClick={loadMore}>Load older requests</button></div>}</section>}
    {selected && <LogDetailModal event={selected} onClose={() => setSelected(null)} />}
  </>
}

function CostLabel({ event }: { event: Pick<UsageEvent, 'cost_usd' | 'priced'> }) {
  const free = !event.priced || event.cost_usd === 0
  return <span className={free ? 'cost-label free' : 'cost-label'}>{formatUSD(free ? 0 : event.cost_usd)}{free && <em>Free</em>}</span>
}


function LogDetailModal({ event, onClose }: { event: UsageEvent; onClose: () => void }) {
  const [content, setContent] = useState<UsageDetail | null>(null)
  const [contentError, setContentError] = useState('')
  useEffect(() => {
    let live = true
    const organization = new URLSearchParams(window.location.search).get('organization_id') ?? ''
    void getUsageDetail(event.id, organization).then((detail) => { if (live) setContent(detail) }).catch((reason: Error) => { if (live) setContentError(reason.message) })
    return () => { live = false }
  }, [event.id])
  const actor = event.username || event.actor_type || 'legacy'
  const detail = [
    ['Status', String(event.status_code)], ['Time', formatDateTime(event.ts)],
    ['Model', event.model || 'unknown'], ['Upstream model', event.upstream_model || 'direct'],
    ['Provider', event.provider || 'unknown'], ['Credential', event.credential_id || 'router cache'],
    ['Actor', actor], ['Actor type', event.actor_type || 'legacy'],
    ['User ID', event.user_id || '—'], ['Organization ID', event.organization_id || 'personal'],
    ['API key ID', event.api_key_id || 'master/direct'], ['Duration', `${formatInteger(event.duration_ms)} ms`],
    ['Cache hit', event.cache_hit ? 'yes' : 'no'], ['Cost', !event.priced || event.cost_usd === 0 ? '$0.000000 · Free' : formatUSD(event.cost_usd)],
  ]
  return <Modal title={`Request ${event.id}`} onClose={onClose} className="usage-detail-modal">
    <div className="safe-note"><strong>Conversation capture</strong><span>{content?.content_available ? 'Request and response content was captured for this request.' : contentError ? `Content could not be loaded: ${contentError}` : content ? 'No content is available. Enable ENABLE_STORE_COMPLLETIONS=true to capture future requests.' : 'Loading request content…'}</span></div>
    <dl className="detail-grid usage-detail-grid">{detail.map(([label, value]) => <div key={label}><dt>{label}</dt><dd>{value}</dd></div>)}</dl>
    <section className="conversation-section"><div className="conversation-heading"><div><span className="eyebrow">Token accounting</span><h3>[input / output / cache read / cache write]</h3></div></div><TokenBreakdown input={event.prompt_tokens} output={event.completion_tokens} cacheRead={event.cache_read_tokens} cacheWrite={event.cache_write_tokens} /></section>
    {content?.content_available && <section className="conversation-section"><div className="conversation-heading"><div><span className="eyebrow">Conversation</span><h3>Messages, reasoning, and tool activity</h3></div>{content.content_truncated && <span className="truncated-badge">truncated</span>}</div><div className="conversation-list">{content.conversation?.map((entry, index) => <ConversationItem entry={entry} key={`${entry.type}-${entry.tool_call_id || index}`} />)}</div></section>}
  </Modal>
}

function ConversationItem({ entry }: { entry: ConversationEntry }) {
  const label = entry.type === 'reasoning' ? 'Reasoning' : entry.type === 'tool_call' ? `Tool call${entry.name ? ` · ${entry.name}` : ''}` : entry.type === 'tool_result' ? `Tool result${entry.name ? ` · ${entry.name}` : ''}` : entry.role
  return <article className={`conversation-message role-${entry.role} trace-${entry.type}`}><span>{label}</span>{entry.tool_call_id && <small>{entry.tool_call_id}</small>}<pre>{formatContent(entry.content || '')}</pre></article>
}

function formatContent(value: string): string {
  try { return JSON.stringify(JSON.parse(value), null, 2) } catch { return value || 'No content' }
}
