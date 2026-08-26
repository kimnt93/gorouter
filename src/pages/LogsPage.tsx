import { useEffect, useMemo, useState } from 'react'
import { getRecent, getUsageDetail } from '../api/client'
import type { UsageDetail as UsageDetailData, UsageEvent } from '../api/contracts'
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
    <header className="page-header"><div><span className="eyebrow">Operations / Logs</span><h1>Request logs</h1><p>Inspect request metadata, token accounting, cost, and the captured conversation for each visible request.</p></div><span className="live-indicator"><i /> Live data</span></header>
    <RangeSelector {...filterState} onChange={filterState.setFilters} />
    <div className="secondary-filters"><label className="select-field"><span>Model</span><select value={filterState.filters.model ?? ''} onChange={(event) => filterState.setFilters({ ...filterState.filters, model: event.target.value })}><option value="">All models</option>{filterState.models.map((model) => <option value={model.name} key={model.name}>{model.name}</option>)}</select></label><label className="select-field small"><span>Status</span><select value={filterState.filters.status ?? ''} onChange={(event) => filterState.setFilters({ ...filterState.filters, status: event.target.value })}><option value="">All statuses</option><option value="200">200</option><option value="400">400</option><option value="401">401</option><option value="403">403</option><option value="429">429</option><option value="500">500</option><option value="502">502</option><option value="503">503</option></select></label></div>
    {loading ? <PageLoading /> : error ? <PageError message={error} retry={() => setVersion((value) => value + 1)} /> : <section className="panel table-panel"><div className="table-toolbar"><span>{formatInteger(events.length)} recent requests</span><span className="token-order">tokens = [in / out / cache read / cache write]</span></div><div className="table-scroll"><table className="logs-table"><thead><tr><th>Status</th><th>Model</th><th>User</th><th>Tokens [I/O/CR/CW]</th><th>Cost</th><th>Latency</th><th>Time</th></tr></thead><tbody>{events.map((event) => <tr key={event.id} tabIndex={0} onClick={() => setSelected(event)} onKeyDown={(key) => { if (key.key === 'Enter' || key.key === ' ') setSelected(event) }}><td><span className={event.status_code >= 200 && event.status_code < 400 ? 'status success' : 'status failure'}>{event.status_code}</span></td><td><strong>{event.model}</strong><small>{event.upstream_model || 'direct'}</small></td><td>{event.username || event.actor_type || 'legacy'}<small>{event.organization_id ? `org · ${event.organization_id.slice(0, 10)}` : 'personal'}</small></td><td><TokenBreakdown compact input={event.prompt_tokens} output={event.completion_tokens} cacheRead={event.cache_read_tokens} cacheWrite={event.cache_write_tokens} /></td><td><CostLabel event={event} /></td><td>{formatInteger(event.duration_ms)} ms</td><td><time dateTime={event.ts} title={formatDateTime(event.ts)}>{relativeTime(event.ts)}</time></td></tr>)}</tbody></table></div>{events.length === 0 && <div className="empty-state"><strong>No matching requests</strong><span>Try changing your range or filters.</span></div>}{cursor && <div className="load-more"><button className="button secondary" onClick={loadMore}>Load older requests</button></div>}</section>}
    {selected && <UsageDetail event={selected} onClose={() => setSelected(null)} />}
  </>
}

function CostLabel({ event }: { event: Pick<UsageEvent, 'cost_usd' | 'priced'> }) {
  const free = !event.priced || event.cost_usd === 0
  return <span className={free ? 'cost-label free' : 'cost-label'}>{formatUSD(free ? 0 : event.cost_usd)}{free && <em>Free</em>}</span>
}

interface ConversationMessage { role: string; content: string }

function contentText(value: unknown): string {
  if (typeof value === 'string') return value
  if (Array.isArray(value)) return value.map((part) => {
    if (typeof part === 'string') return part
    if (part && typeof part === 'object') {
      const item = part as Record<string, unknown>
      return contentText(item.text ?? item.content ?? item)
    }
    return String(part ?? '')
  }).filter(Boolean).join('\n')
  if (value == null) return ''
  try { return JSON.stringify(value, null, 2) } catch { return String(value) }
}

export function parseConversation(requestBody: string, responseBody: string): ConversationMessage[] {
  const messages: ConversationMessage[] = []
  try {
    const request = JSON.parse(requestBody) as { messages?: Array<Record<string, unknown>> }
    for (const message of request.messages ?? []) {
      const content = contentText(message.content)
      const tools = message.tool_calls ? `\n${contentText(message.tool_calls)}` : ''
      messages.push({ role: String(message.role ?? 'user'), content: `${content}${tools}`.trim() })
    }
  } catch {
    if (requestBody.trim()) messages.push({ role: 'request', content: requestBody })
  }
  try {
    const response = JSON.parse(responseBody) as { choices?: Array<{ message?: Record<string, unknown> }> }
    for (const choice of response.choices ?? []) {
      if (!choice.message) continue
      const content = contentText(choice.message.content)
      const reasoning = contentText(choice.message.reasoning_content)
      const tools = contentText(choice.message.tool_calls)
      messages.push({ role: String(choice.message.role ?? 'assistant'), content: [reasoning && `Reasoning\n${reasoning}`, content, tools].filter(Boolean).join('\n\n') })
    }
  } catch {
    if (responseBody.trim()) messages.push({ role: 'response', content: responseBody })
  }
  return messages
}

function UsageDetail({ event, onClose }: { event: UsageEvent; onClose: () => void }) {
  const [detail, setDetail] = useState<UsageDetailData | null>(null)
  const [error, setError] = useState('')
  useEffect(() => {
    let live = true
    void getUsageDetail(event.id, event.organization_id).then((value) => { if (live) setDetail(value) }).catch((reason: Error) => { if (live) setError(reason.message) })
    return () => { live = false }
  }, [event.id])
  const messages = useMemo(() => detail ? parseConversation(detail.request_body, detail.response_body) : [], [detail])
  const started = new Date(new Date(event.ts).getTime() - event.duration_ms).toISOString()
  const fields = [
    ['Started at', formatDateTime(started)], ['Ended at', formatDateTime(event.ts)], ['Duration', `${(event.duration_ms / 1000).toFixed(1)}s`], ['Status', String(event.status_code)],
    ['Model', event.upstream_model || event.model], ['Requested model', event.model], ['Actor', event.username || event.actor_type || 'legacy'], ['Organization', event.organization_id || 'Personal'],
    ['API key ID', event.api_key_id || '—'], ['Credential ID', event.credential_id || '—'], ['Cost', event.cost_usd === 0 || !event.priced ? '$0.0000 · Free' : formatUSD(event.cost_usd)], ['Cache source', event.cache_hit ? 'Gateway response cache' : event.cache_read_tokens > 0 ? 'Upstream provider' : 'None'],
  ]
  return <Modal className="usage-detail-modal" title={event.id} onClose={onClose}><TokenBreakdown input={event.prompt_tokens} output={event.completion_tokens} cacheRead={event.cache_read_tokens} cacheWrite={event.cache_write_tokens} /><dl className="detail-grid usage-detail-grid">{fields.map(([label, value]) => <div key={label}><dt>{label}</dt><dd>{value}</dd></div>)}</dl><section className="conversation-section"><div className="conversation-heading"><div><span className="eyebrow">Conversation context</span><h3>{messages.length} messages</h3></div>{detail?.content_truncated && <span className="truncated-badge">Stored content truncated at 8 MiB</span>}</div>{error ? <div className="banner error-banner">{error}</div> : !detail ? <PageLoading /> : messages.length === 0 ? <div className="empty-state"><strong>No captured conversation</strong><span>Conversation capture applies to requests made after this update.</span></div> : <div className="conversation-list">{messages.map((message, index) => <article className={`conversation-message role-${message.role}`} key={`${message.role}-${index}`}><span>{message.role}</span><pre>{message.content || '—'}</pre></article>)}</div>}</section><div className="safe-note"><strong>Access boundary</strong><span>Conversation content follows the same user and organization visibility policy as usage logs. Credentials, authorization headers, cookies, and provider secrets are never captured.</span></div></Modal>
}
