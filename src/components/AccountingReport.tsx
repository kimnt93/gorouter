import { useEffect, useState } from 'react'
import { getAccountingCapabilities, getUsageReport } from '../api/client'
import type { UsageFilters, UsageReport, UsageReportDimension } from '../api/contracts'
import { PageError, PageLoading } from './PageState'
import { SearchableSelect, TruncatedText } from './SearchableSelect'
import { StatCard } from './StatCard'
import { ReportUsageChart } from './VerticalUsageChart'
import { formatInteger, formatUSD } from '../lib/format'

export function AccountingReport({ filters }: { filters: UsageFilters }) {
  const [report, setReport] = useState<UsageReport | null>(null)
  const [dimension, setDimension] = useState<UsageReportDimension>('agent')
  const [agents, setAgents] = useState<string[]>([])
  const [agentInput, setAgentInput] = useState('')
  const [search, setSearch] = useState('')
  const [conversationInput, setConversationInput] = useState('')
  const [conversation, setConversation] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [version, setVersion] = useState(0)
  useEffect(() => {
    const controller = new AbortController()
    setLoading(true); setError(''); setReport(null)
    void (async () => {
      const caps = await getAccountingCapabilities(controller.signal)
      if (caps.capabilities.usage_report !== 'gorouter-usage-report-v1') throw new Error('This Router does not support the accounting report contract.')
      const result = await getUsageReport(filters, dimension, agents, conversation, controller.signal)
      if (!controller.signal.aborted) setReport(result)
    })().catch((reason: Error) => { if (!controller.signal.aborted) setError(reason.message) }).finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
  }, [filters, dimension, agents, conversation, version])
  const toggle = (id: string) => setAgents((previous) => previous.includes(id) ? previous.filter((value) => value !== id) : [...previous, id])
  const options = [...new Set([...agents, ...(dimension === 'agent' ? report?.groups.map((group) => group.id).filter(Boolean) ?? [] : [])])].sort().filter((id) => id.toLowerCase().includes(search.toLowerCase()))
  return <>
    <section className="panel accounting-report-controls">
      <div className="identity-filter-row"><label className="select-field"><span>Group and stack by</span><SearchableSelect value={dimension} onChange={(value) => setDimension(value as UsageReportDimension)} options={['agent', 'model', 'user'].map((value) => ({ value, label: value[0].toUpperCase() + value.slice(1) }))} /></label>
        <details className="multi-picker"><summary>{agents.length ? `${agents.length} agents selected` : 'All authorized agents'}</summary><div className="multi-picker-menu"><input aria-label="Search agents" placeholder="Search observed agents" value={search} onChange={(event) => setSearch(event.target.value)} />{options.map((id) => <label key={id}><input type="checkbox" aria-label={id} checked={agents.includes(id)} onChange={() => toggle(id)} /><TruncatedText>{id}</TruncatedText></label>)}{options.length === 0 && <small>No observed agents. Enter a stable ID below.</small>}</div></details>
      </div>
      <form className="identity-filter-row" onSubmit={(event) => { event.preventDefault(); const next = agentInput.split(',').map((id) => id.trim()).filter(Boolean); if (next.some((id) => !/^[A-Za-z0-9_.:/-]{1,128}$/.test(id)) || new Set([...agents, ...next]).size > 100) { setError('Use at most 100 valid agent IDs.'); return }; setAgents([...new Set([...agents, ...next])].sort()); setAgentInput(''); setConversation(conversationInput.trim()) }}>
        <label><span>Agent IDs (comma separated)</span><input value={agentInput} onChange={(event) => setAgentInput(event.target.value)} /></label>
        <label><span>Conversation ID</span><input value={conversationInput} maxLength={128} onChange={(event) => setConversationInput(event.target.value)} /></label>
        <button type="submit">Apply tracking filters</button><button type="button" onClick={() => { setAgents([]); setConversation(''); setConversationInput(''); setAgentInput('') }}>Clear</button>
      </form>
      <div className="filter-chips">{agents.map((id) => <button key={id} onClick={() => toggle(id)} aria-label={`Remove ${id}`}><TruncatedText>{id}</TruncatedText><span aria-hidden="true">×</span></button>)}</div>
      <p>IDs are scoped to the authenticated user. Use a bounded range (not All). Historical or deleted agents remain queryable by ID.</p>
    </section>
    {loading ? <PageLoading /> : error ? <PageError message={error} retry={() => setVersion((value) => value + 1)} /> : report && <>
      <section className="panel" role="status"><strong>Stored usage · coverage {report.coverage.state}</strong><p>As of {report.as_of} · {report.scope.kind} · accounting time · UTC · week starts {report.range.week_starts_on}. {formatInteger(report.coverage.measurement_unknown_requests)} requests have unknown measurement. In-flight or unrecorded work is not included; zero is not proof of free usage.</p></section>
      <section className="stat-grid"><StatCard label="Stored requests" value={formatInteger(report.totals.requests)} detail="not messages or sessions" accent="purple" /><StatCard label="Incurred tokens" value={formatInteger(report.totals.total_tokens)} detail="four disjoint token components" accent="blue" /><StatCard label="Recorded cost" value={formatUSD(report.totals.cost_usd)} detail="Router-priced · not an invoice" accent="amber" /><StatCard label="Response cache hits" value={formatInteger(report.totals.response_cache_hits)} detail="replayed tokens excluded" accent="green" /></section>
      <section className="panel"><h2>Tokens by {dimension}</h2><ReportUsageChart report={report} metric="tokens" /></section>
      <section className="panel"><h2>Recorded cost by {dimension}</h2><ReportUsageChart report={report} metric="cost" /></section>
      <section className="panel"><h2>Grouped usage</h2><div className="table-scroll"><table><thead><tr><th>#</th><th>{dimension}</th><th>Requests</th><th>Total tokens</th><th>Recorded cost</th><th>Unknown measurement</th></tr></thead><tbody>{report.groups.map((group, index) => <tr key={group.id}><td>{index + 1}</td><td><TruncatedText>{group.unattributed ? 'Unattributed' : group.id}</TruncatedText></td><td>{formatInteger(group.totals.requests)}</td><td>{formatInteger(group.totals.total_tokens)}</td><td>{formatUSD(group.totals.cost_usd)}</td><td>{formatInteger(group.totals.measurement_unknown_requests)}</td></tr>)}</tbody></table></div>{report.groups.length === 0 && <p>No stored records match these filters. Accounting coverage remains unknown.</p>}</section>
    </>}
  </>
}
