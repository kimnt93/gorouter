import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, expect, test, vi } from 'vitest'
import { LogsPage } from './LogsPage'

const api = vi.hoisted(() => ({
  getRecent: vi.fn(), getUsageDetail: vi.fn(), getUsers: vi.fn(), getAPIKeys: vi.fn(), getOrganizations: vi.fn(), getModels: vi.fn(), getMembers: vi.fn(),
}))
vi.mock('../api/client', () => api)

beforeEach(() => {
  api.getUsers.mockResolvedValue({ object: 'list', data: [] })
  api.getAPIKeys.mockResolvedValue({ object: 'list', data: [] })
  api.getOrganizations.mockResolvedValue({ object: 'list', data: [] })
  api.getModels.mockResolvedValue([])
  api.getMembers.mockResolvedValue({ object: 'list', data: [] })
  api.getUsageDetail.mockResolvedValue({ id: 'usage-1', ts: '2026-09-01T00:00:00Z', tenant_id: '', api_key_id: 'key-1', credential_id: 'cred-1', provider: 'codex', model: 'cx/gpt-5.6-luna', upstream_model: 'gpt-5.6-luna', prompt_tokens: 10, completion_tokens: 4, cache_read_tokens: 2, cache_write_tokens: 1, cost_usd: 0.01, priced: true, cache_hit: false, status_code: 200, duration_ms: 123, actor_type: 'user', user_id: 'user-1', username: 'person@example.test', organization_id: '', request_body: '{"prompt":"hello"}', response_body: '{"answer":"world"}', content_available: true, content_truncated: false })
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
  expect(screen.getByText(/hello/)).toBeInTheDocument()
  expect(api.getUsageDetail).toHaveBeenCalledWith('usage-1', '')
  expect(screen.getByText('cred-1')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Close details' }))
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
})
