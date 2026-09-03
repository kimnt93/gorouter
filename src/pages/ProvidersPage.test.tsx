import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, expect, test, vi } from 'vitest'
import { ProvidersPage } from './ProvidersPage'

const api = vi.hoisted(() => ({
  getProviders: vi.fn(), getCredentials: vi.fn(), getOrganizations: vi.fn(), createCredential: vi.fn(), deleteCredential: vi.fn(), discoverModels: vi.fn(), importModels: vi.fn(), requestStream: vi.fn(), startOAuth: vi.fn(), completeOAuth: vi.fn(), testCredential: vi.fn(), updateCredential: vi.fn(), getCredentialQuota: vi.fn(), refreshCredentialQuota: vi.fn(), getCodexResetCredits: vi.fn(), redeemCodexResetCredit: vi.fn(),
}))
vi.mock('../api/client', () => api)

beforeEach(() => {
  vi.clearAllMocks()
  api.getProviders.mockResolvedValue({ data: [{ id: 'custom', name: 'Custom provider', description: 'Compatible endpoint', auth: 'api_key', protocol: 'openai', default_base_url: '', model_prefix: 'custom', custom_base_url: true, oauth_supported: false, oauth_refresh_required: false, quota_supported: false }] })
  api.getCredentials.mockResolvedValue([])
  api.getOrganizations.mockResolvedValue({ object: 'list', data: [] })
})

test('reads cached quota and contacts the provider only after Reload', async () => {
  api.getProviders.mockResolvedValue({ data: [{ id: 'codex', name: 'OpenAI Codex', description: 'Codex subscription', auth: 'oauth', protocol: 'codex', default_base_url: '', model_prefix: 'cx', custom_base_url: false, oauth_supported: true, oauth_refresh_required: true, quota_supported: true }] })
  api.getCredentials.mockResolvedValue([{ id: 'cred-1', name: 'person@example.test', provider: 'codex', kind: 'oauth', base_url: 'https://chatgpt.com/backend-api', status: 'active', owner_tenant_id: null, created_at: '' }])
  api.getCredentialQuota.mockResolvedValue({ credential_id: 'cred-1', provider: 'codex', account: 'pe****@example.test', available: true, in_use: true, windows: [], message: 'Click reload to fetch quota' })
  api.refreshCredentialQuota.mockResolvedValue({ credential_id: 'cred-1', provider: 'codex', account: 'pe****@example.test', available: true, fetched_at: '2026-08-26T00:00:00Z', windows: [{ name: 'Session (5h)', used_percent: 25, remaining_percent: 75, reset_at: '2026-08-26T05:00:00Z' }] })

  render(<ProvidersPage />)
  expect(await screen.findByText('Click reload to fetch quota')).toBeInTheDocument()
  expect(screen.getAllByText('pe****@example.test').length).toBeGreaterThan(0)
  expect(screen.getByText('In use')).toBeInTheDocument()
  expect(screen.queryByText(/^Updated /)).not.toBeInTheDocument()
  expect(api.getCredentialQuota).toHaveBeenCalledWith('cred-1')
  expect(api.refreshCredentialQuota).not.toHaveBeenCalled()

  fireEvent.click(screen.getByRole('button', { name: 'Reload' }))
  expect(await screen.findByText('75.0% remaining')).toBeInTheDocument()
  expect(screen.getByText('Available')).toBeInTheDocument()
  expect(api.refreshCredentialQuota).toHaveBeenCalledWith('cred-1')
})

test('exposes the provider API-key connection workflow', async () => {
  render(<ProvidersPage />)
  const connect = await screen.findByRole('button', { name: 'Connect Custom provider' })
  fireEvent.click(connect)
  expect(screen.getByRole('dialog')).toBeInTheDocument()
  expect(screen.getByLabelText('API key')).toHaveAttribute('type', 'password')
  expect(screen.getByLabelText('Base URL')).toBeEnabled()
  expect(screen.queryByLabelText('Owner organization')).not.toBeInTheDocument()
  await waitFor(() => expect(api.getCredentials).toHaveBeenCalledOnce())
})


test('lays out connected accounts in a compact grid and opens reset credits modal', async () => {
  api.getProviders.mockResolvedValue({ data: [{ id: 'codex', name: 'OpenAI Codex', description: 'Codex subscription', auth: 'oauth', protocol: 'codex', default_base_url: '', model_prefix: 'cx', custom_base_url: false, oauth_supported: true, oauth_refresh_required: true, quota_supported: true }] })
  api.getCredentials.mockResolvedValue(Array.from({ length: 4 }, (_, index) => ({ id: `cred-${index + 1}`, name: `Account ${index + 1}`, provider: 'codex', kind: 'oauth', base_url: 'https://chatgpt.com/backend-api', status: 'active', owner_tenant_id: null, created_at: '' })))
  api.getCredentialQuota.mockResolvedValue({ credential_id: 'cred-1', provider: 'codex', account: 'account', available: true, windows: [] })
  api.getCodexResetCredits.mockResolvedValue({ available_count: 1, credits: [{ selection_token: 'credit-1', title: 'Weekly reset', description: 'Reset weekly usage' }] })
  render(<ProvidersPage />)
  const providerCard = (await screen.findByText('4 connected')).closest('.provider-card-react')
  expect(providerCard).toHaveClass('has-accounts')
  expect(providerCard?.querySelector('.connection-grid')?.children).toHaveLength(4)
  fireEvent.click(screen.getAllByRole('button', { name: 'Reset credits' })[0])
  const creditTitle = await screen.findByText('Weekly reset')
  expect(creditTitle.closest('.reset-credit-modal')).toBeInTheDocument()
  expect(creditTitle.closest('.reset-credit-modal')?.querySelector('button.button')).toHaveTextContent('Redeem')
})
