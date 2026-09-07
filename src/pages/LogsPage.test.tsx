import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import { LogsPage } from './LogsPage'

const api = vi.hoisted(() => ({
  getRecent: vi.fn(), getUsageDetail: vi.fn(), getUsers: vi.fn(), getAPIKeys: vi.fn(), getOrganizations: vi.fn(), getModels: vi.fn(), getMembers: vi.fn(),
}))
vi.mock('../api/client', () => api)

afterEach(cleanup)

beforeEach(() => {
  vi.clearAllMocks()
  api.getUsers.mockResolvedValue({ object: 'list', data: [] })
  api.getAPIKeys.mockResolvedValue({ object: 'list', data: [] })
  api.getOrganizations.mockResolvedValue({ object: 'list', data: [] })
  api.getModels.mockResolvedValue([])
  api.getMembers.mockResolvedValue({ object: 'list', data: [] })
  api.getUsageDetail.mockResolvedValue({ id: 'usage-1', ts: '2026-09-01T00:00:00Z', tenant_id: '', api_key_id: 'key-1', credential_id: 'cred-1', provider: 'codex', model: 'cx/gpt-5.6-luna', upstream_model: 'gpt-5.6-luna', prompt_tokens: 10, completion_tokens: 4, cache_read_tokens: 2, cache_write_tokens: 1, cost_usd: 0.01, priced: true, cache_hit: false, status_code: 200, duration_ms: 123, actor_type: 'user', user_id: 'user-1', username: 'person@example.test', organization_id: '', conversation: [{ role: 'user', type: 'text', content: 'hello' }, { role: 'assistant', type: 'reasoning', content: 'considering' }, { role: 'assistant', type: 'tool_call', name: 'lookup', tool_call_id: 'call-1', content: '{"q":1}' }, { role: 'assistant', type: 'text', content: 'world' }], content_available: true, content_truncated: false })
  api.getRecent.mockResolvedValue({ object: 'list', data: [{
    id: 'usage-1', ts: '2026-09-01T00:00:00Z', tenant_id: '', api_key_id: 'key-1', credential_id: 'cred-1', provider: 'codex', model: 'cx/gpt-5.6-luna', upstream_model: 'gpt-5.6-luna', prompt_tokens: 10, completion_tokens: 4, cache_read_tokens: 2, cache_write_tokens: 1, cost_usd: 0.01, priced: true, cache_hit: false, status_code: 200, duration_ms: 123, actor_type: 'user', user_id: 'user-1', username: 'person@example.test', organization_id: '',
  }] })
})

test('opens request details from a clicked log row and closes the popup', async () => {
  render(<LogsPage />)
  const row = await screen.findByRole('button', { name: 'View request usage-1 details' })
  expect(screen.getByRole('button', { name: 'Details' })).toBeInTheDocument()
  fireEvent.click(row)
  expect(screen.getByRole('dialog', { name: 'Request usage-1' })).toBeInTheDocument()
  expect(await screen.findByText('Conversation')).toBeInTheDocument()
  expect(screen.getByText('hello')).toBeInTheDocument()
  expect(screen.getByText('Reasoning')).toBeInTheDocument()
  expect(screen.getByText('Tool call · lookup')).toBeInTheDocument()
  expect(api.getUsageDetail).toHaveBeenCalledWith('usage-1', '')
  expect(screen.getByText('cred-1')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Close details' }))
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
})


test('reloads the newest log page and replaces stale rows', async () => {
  const event = (id: string, model: string) => ({ id, ts: '2026-09-01T00:00:00Z', tenant_id: '', api_key_id: '', credential_id: '', provider: 'codex', model, upstream_model: model, prompt_tokens: 0, completion_tokens: 0, cache_read_tokens: 0, cache_write_tokens: 0, cost_usd: 0, priced: true, cache_hit: false, status_code: 200, duration_ms: 1, actor_type: 'master', user_id: '', username: 'master', organization_id: '' })
  api.getRecent.mockResolvedValueOnce({ object: 'list', data: [event('usage-old', 'cx/old')] }).mockResolvedValueOnce({ object: 'list', data: [event('usage-new', 'cx/new')] })
  render(<LogsPage />)
  expect((await screen.findAllByText('cx/old')).length).toBeGreaterThan(0)
  fireEvent.click(screen.getByRole('button', { name: 'Reload logs' }))
  expect((await screen.findAllByText('cx/new')).length).toBeGreaterThan(0)
  await waitFor(() => expect(api.getRecent).toHaveBeenCalledTimes(2))
  expect(screen.queryAllByText('cx/old')).toHaveLength(0)
})
