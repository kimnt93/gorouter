import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, expect, test, vi } from 'vitest'
import { KeysPage } from './KeysPage'

const api = vi.hoisted(() => ({
  getAPIKeys: vi.fn(), getOrganizations: vi.fn(), getUsers: vi.fn(), getMembers: vi.fn(), getAPIKeyModels: vi.fn(),
  createAPIKey: vi.fn(), patchAPIKey: vi.fn(), revealAPIKey: vi.fn(), rotateAPIKey: vi.fn(), deleteAPIKey: vi.fn(),
}))
vi.mock('../api/client', () => api)
vi.mock('../context/SessionContext', () => ({ useSession: () => ({ session: { role: 'master' } }) }))

beforeEach(() => {
  api.getAPIKeys.mockResolvedValue({ object: 'list', data: [] })
  api.getOrganizations.mockResolvedValue({ object: 'list', data: [{ id: 'org-1', name: 'Acme', status: 'active', created_at: '', updated_at: '' }] })
  api.getUsers.mockResolvedValue({ object: 'list', data: [{ id: 'user-1', username: 'member@example.com', status: 'active', created_at: '', updated_at: '' }] })
  api.getMembers.mockResolvedValue({ object: 'list', data: [{ organization_id: 'org-1', user_id: 'user-1', role: 'member', created_at: '' }] })
  api.getAPIKeyModels.mockResolvedValue({ object: 'list', data: [{ id: 'cx/gpt-5.6-luna', upstream_model: 'gpt-5.6-luna', free: false, price: { input_per_m: 0.2, output_per_m: 1.2, cached_input_per_m: 0.02, cache_write_per_m: 0.25 } }] })
  api.createAPIKey.mockResolvedValue({ plaintext: 'secret-once' })
})

test('creates a chat-only key for a selected organization member and priced model', async () => {
  render(<KeysPage />)
  fireEvent.click(await screen.findByRole('button', { name: 'Create API key' }))

  expect(await screen.findByText('cx/gpt-5.6-luna')).toBeInTheDocument()
  expect(screen.queryByLabelText('Requests/minute')).not.toBeInTheDocument()
  expect(screen.getByText('$0.2000 input · $1.2000 output · $0.0200 cache read · $0.2500 cache write / 1M')).toBeInTheDocument()
  fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Luna key' } })
  fireEvent.click(screen.getByRole('checkbox'))
  fireEvent.click(screen.getByRole('button', { name: 'Create key' }))

  await waitFor(() => expect(api.createAPIKey).toHaveBeenCalledWith(expect.objectContaining({
    name: 'Luna key', owner_type: 'user', owner_user_id: 'user-1', context_organization_id: 'org-1', scopes: ['chat'], models: ['cx/gpt-5.6-luna'],
  })))
  expect(api.createAPIKey.mock.calls[0][0]).not.toHaveProperty('rpm')
  expect(await screen.findByText('secret-once')).toBeInTheDocument()
})


test('reveals an API key in a copyable popup', async () => {
  api.getAPIKeys.mockResolvedValue({ object: 'list', data: [{ id: 'key-1', name: 'Member key', key_prefix: 'nr-preview', models: ['cx/model'], scopes: ['chat'], quota_usd: null, quota_period: 'none', owner_type: 'user', owner_user_id: 'user-1', context_organization_id: 'org-1', enabled: true, created_at: '2026-01-01T00:00:00Z' }] })
  api.revealAPIKey.mockResolvedValue({ plaintext: 'nr-synthetic-secret' })
  render(<KeysPage />)
  fireEvent.click(await screen.findByRole('button', { name: 'View Member key API key' }))
  expect(await screen.findByText('nr-synthetic-secret')).toBeInTheDocument()
  expect(screen.getAllByRole('button', { name: 'Copy secret' }).length).toBeGreaterThan(0)
  expect(api.revealAPIKey).toHaveBeenCalledWith('key-1')
})
