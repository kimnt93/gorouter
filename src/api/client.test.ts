import { afterEach, expect, test, vi } from 'vitest'
import type { UsageFilters } from './contracts'
import { getRecent, getActivity } from './client'

afterEach(() => vi.restoreAllMocks())

test('loads recent logs from newest without applying a date range', async () => {
  const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ object: 'list', data: [] }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
  const filters: UsageFilters = { range: 'custom', groupBy: 'day', filterType: 'user', userIds: ['user-1'], apiKeyIds: [], organizationIds: [], since: '2020-01-01T00:00', until: '2020-01-02T00:00', model: 'cx/model', status: '200' }

  await getRecent(filters)

  const url = String(fetchMock.mock.calls[0][0])
  expect(url).toContain('/admin/usage/recent?')
  expect(url).toContain('user_id=user-1')
  expect(url).toContain('model=cx%2Fmodel')
  expect(url).toContain('status=200')
  expect(url).not.toContain('since=')
  expect(url).not.toContain('until=')
  expect(url).not.toContain('range=')
})

test('encodes tracking multi-selections consistently for events and aggregates', async () => {
  const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation(async () => new Response('{}', { status: 200 }))
  const filters: UsageFilters = { range: 'all', groupBy: 'day', filterType: 'user', userIds: [], apiKeyIds: [], organizationIds: [], since: '', until: '', agentIds: ['a', 'b'], traceIds: ['trace-a', 'trace-b'], requestIds: ['req'], parentRunIds: ['parent'], conversationIds: ['session'], runIds: ['run'] }
  await getRecent(filters)
  await getActivity(filters)
  for (const call of fetchMock.mock.calls) {
    const params = new URL(String(call[0]), 'http://localhost').searchParams
    expect(params.get('agent_id')).toBe('a,b')
    expect(params.get('trace_id')).toBe('trace-a,trace-b')
    expect(params.get('logical_request_id')).toBe('req')
    expect(params.get('parent_run_id')).toBe('parent')
    expect(params.get('conversation_id')).toBe('session')
    expect(params.get('run_id')).toBe('run')
    expect(params.has('workspace_id')).toBe(false)
  }
})
