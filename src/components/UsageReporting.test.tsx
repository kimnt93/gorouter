import { fireEvent, render, screen, within } from '@testing-library/react'
import { expect, test, vi } from 'vitest'
import type { UsageActivityBucket, UsageFilters } from '../api/contracts'
import { RangeSelector } from './RangeSelector'
import { CacheEfficiencyChart, VerticalUsageChart } from './VerticalUsageChart'
import { groupByForUsageRange } from '../lib/usageResolution'

test('renders normalized request bars with stable user breakdown tooltips', () => {
  const rows: UsageActivityBucket[] = [
    { start: '2026-08-25T00:00:00Z', requests: 3, prompt_tokens: 10, completion_tokens: 2, cache_read_tokens: 4, cache_write_tokens: 1, cost_usd: 1, input_cost_usd: .2, output_cost_usd: .6, cache_read_cost_usd: .1, cache_write_cost_usd: .1, user_id: 'user-a', username: 'Alice' },
    { start: '2026-08-25T00:00:00Z', requests: 1, prompt_tokens: 5, completion_tokens: 1, cache_read_tokens: 0, cache_write_tokens: 0, cost_usd: .5, input_cost_usd: .1, output_cost_usd: .4, cache_read_cost_usd: 0, cache_write_cost_usd: 0, user_id: 'user-b', username: 'Bob' },
  ]
  render(<VerticalUsageChart data={rows} metric="requests" groupBy="hour" />)
  expect(screen.getByLabelText('requests activity trend')).toHaveTextContent('Alice3')
  expect(screen.getByLabelText('requests activity trend')).toHaveTextContent('Bob1')
  expect(screen.getByText('Total').parentElement).toHaveTextContent('4 · 100%')
})

test('shows all four token and cost components in chart tooltips', () => {
  const row: UsageActivityBucket = { start: '2026-08-25T00:00:00Z', requests: 1, prompt_tokens: 10, completion_tokens: 2, cache_read_tokens: 4, cache_write_tokens: 1, cost_usd: 1, input_cost_usd: .2, output_cost_usd: .6, cache_read_cost_usd: .1, cache_write_cost_usd: .1, user_id: 'user-a', username: 'Alice' }
  const { rerender } = render(<VerticalUsageChart data={[row]} metric="tokens" groupBy="day" />)
  for (const label of ['Input', 'Output', 'Cache read', 'Cache write']) expect(screen.getAllByText(label).length).toBeGreaterThan(0)
  rerender(<VerticalUsageChart data={[row]} metric="cost" groupBy="day" />)
  for (const label of ['Input', 'Output', 'Cache read', 'Cache write']) expect(screen.getAllByText(label).length).toBeGreaterThan(0)
})

test('shows cache-read percentage and total usage for each bucket', () => {
  const row: UsageActivityBucket = { start: '2026-08-25T00:00:00Z', requests: 1, prompt_tokens: 10, completion_tokens: 5, cache_read_tokens: 90, cache_write_tokens: 2, cost_usd: 1, input_cost_usd: .2, output_cost_usd: .6, cache_read_cost_usd: .1, cache_write_cost_usd: .1, user_id: 'user-a', username: 'Alice' }
  render(<CacheEfficiencyChart data={[row]} groupBy="hour" />)
  const chart = within(screen.getByLabelText('cache read share by time bucket'))
  expect(chart.getByText('90.0%')).toBeInTheDocument()
  expect(chart.getByText('Total usage tokens').parentElement).toHaveTextContent('107')
  expect(chart.getByText('Cache read').parentElement).toHaveTextContent('90 · 90.0%')
  expect(chart.getByText('Uncached input').parentElement).toHaveTextContent('10 · 10.0%')
})

test('supports a filter dimension and multiple values without a manual resolution filter', () => {
  const filters: UsageFilters = { range: '7d', groupBy: 'day', filterType: 'user', userIds: [], apiKeyIds: [], organizationIds: [], since: '', until: '' }
  const onChange = vi.fn()
  render(<RangeSelector filters={filters} onChange={onChange} users={[{ id: 'a', username: 'Alice', status: 'active', created_at: '', updated_at: '' }, { id: 'b', username: 'Bob', status: 'active', created_at: '', updated_at: '' }]} apiKeys={[]} organizations={[]} />)
  expect(screen.queryByLabelText('Resolution')).not.toBeInTheDocument()
  fireEvent.click(screen.getByText('All users'))
  fireEvent.click(screen.getByLabelText('Alice'))
  expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ userIds: ['a'] }))
})

test('selects usage resolution from preset and custom ranges', () => {
  const filters: UsageFilters = { range: '7d', groupBy: 'hour', filterType: 'user', userIds: [], apiKeyIds: [], organizationIds: [], since: '2026-08-01T00:00', until: '2026-08-08T00:00' }
  const expected = { '1d': 'hour', '7d': 'hour', '30d': 'day', '90d': 'day', ytd: 'week', all: 'week' } as const
  for (const [range, groupBy] of Object.entries(expected)) expect(groupByForUsageRange({ ...filters, range: range as UsageFilters['range'] })).toBe(groupBy)
  expect(groupByForUsageRange({ ...filters, range: 'custom', until: '2026-08-08T00:00' })).toBe('hour')
  expect(groupByForUsageRange({ ...filters, range: 'custom', until: '2026-10-30T00:00' })).toBe('day')
  expect(groupByForUsageRange({ ...filters, range: 'custom', until: '2026-10-31T00:01' })).toBe('week')
})
