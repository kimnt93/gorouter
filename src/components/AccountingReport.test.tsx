import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import { AccountingReport } from './AccountingReport'
import { defaultFilters } from '../hooks/useUsageFilters'
import type { UsageReport, UsageReportTotals } from '../api/contracts'
const api = vi.hoisted(() => ({ getAccountingCapabilities: vi.fn(), getUsageReport: vi.fn() }))
vi.mock('../api/client', () => api)
const totals: UsageReportTotals = { requests: 1, input_tokens: 100, output_tokens: 20, cache_read_tokens: 40, cache_write_tokens: 0, total_tokens: 160, cost_usd: .02, input_cost_usd: .01, output_cost_usd: .005, cache_read_cost_usd: .005, cache_write_cost_usd: 0, response_cache_hits: 0, measurement_unknown_requests: 1, unattributed_requests: 0 }
const report: UsageReport = { capability_version: 'gorouter-usage-report-v1', scope: { kind: 'personal', user_id: 'u' }, range: { from: '2026-09-01T00:00:00Z', to: '2026-09-02T00:00:00Z', timezone: 'UTC', time_basis: 'accounting', week_starts_on: 'sunday' }, as_of: '2026-09-02T00:00:00Z', freshness: { state: 'stored_only' }, coverage: { state: 'unknown', unattributed_requests: 0, measurement_unknown_requests: 1 }, totals, groups: [{ dimension: 'agent', id: 'alpha', unattributed: false, totals }], series: [{ start: '2026-09-01T00:00:00Z', end: '2026-09-02T00:00:00Z', group_id: 'alpha', unattributed: false, totals }], truncated: false }
afterEach(cleanup)
beforeEach(() => { vi.clearAllMocks(); api.getAccountingCapabilities.mockResolvedValue({ capabilities: { usage_report: 'gorouter-usage-report-v1' } }); api.getUsageReport.mockResolvedValue(report) })
test('shows stored cost with explicit unknown coverage, groups and searchable agent selection', async () => {
  render(<AccountingReport filters={defaultFilters} />)
  expect(await screen.findByText('Stored usage · coverage unknown')).toBeInTheDocument()
  expect(screen.getByText(/zero is not proof of free usage/)).toBeInTheDocument()
  fireEvent.click(screen.getByText('All authorized agents'))
  fireEvent.change(screen.getByLabelText('Search agents'), { target: { value: 'alp' } })
  fireEvent.click(screen.getByRole('checkbox', { name: 'alpha' }))
  await waitFor(() => expect(api.getUsageReport).toHaveBeenLastCalledWith(defaultFilters, 'agent', ['alpha'], '', expect.any(AbortSignal)))
  expect(screen.getByRole('button', { name: 'Remove alpha' })).toBeInTheDocument()
})
test('does not invent zero totals on dependency errors', async () => {
  api.getUsageReport.mockRejectedValue(new Error('Accounting storage unavailable'))
  render(<AccountingReport filters={defaultFilters} />)
  expect(await screen.findByText('Accounting storage unavailable')).toBeInTheDocument()
  expect(screen.queryByText('Recorded cost')).not.toBeInTheDocument()
})
test('gates unsupported contracts and aborts unmounted requests', async () => {
  api.getAccountingCapabilities.mockResolvedValue({ capabilities: { usage_report: '' } })
  const view = render(<AccountingReport filters={defaultFilters} />)
  expect(await screen.findByText(/does not support/)).toBeInTheDocument()
  expect(api.getUsageReport).not.toHaveBeenCalled()
  const signal: AbortSignal = api.getAccountingCapabilities.mock.calls[0][0]
  view.unmount()
  expect(signal.aborted).toBe(true)
})
test('applies conversation and stable agent IDs without per-agent calls', async () => {
  render(<AccountingReport filters={defaultFilters} />)
  await screen.findByText('Stored usage · coverage unknown')
  fireEvent.change(screen.getByLabelText('Agent IDs (comma separated)'), { target: { value: 'beta,alpha' } })
  fireEvent.change(screen.getByLabelText('Conversation ID'), { target: { value: 'conversation-1' } })
  fireEvent.click(screen.getByRole('button', { name: 'Apply tracking filters' }))
  await waitFor(() => expect(api.getUsageReport).toHaveBeenLastCalledWith(defaultFilters, 'agent', ['alpha', 'beta'], 'conversation-1', expect.any(AbortSignal)))
})
