import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import { ProvidersPage } from './ProvidersPage'

const api = vi.hoisted(() => ({
  getProviders: vi.fn(), getCredentials: vi.fn(), getOrganizations: vi.fn(), createCredential: vi.fn(), deleteCredential: vi.fn(), discoverModels: vi.fn(), importModels: vi.fn(), requestStream: vi.fn(), startOAuth: vi.fn(), completeOAuth: vi.fn(), testCredential: vi.fn(), updateCredential: vi.fn(), getCredentialQuota: vi.fn(), refreshCredentialQuota: vi.fn(), getCodexResetCredits: vi.fn(), redeemCodexResetCredit: vi.fn(),
}))
vi.mock('../api/client', () => api)

afterEach(cleanup)

beforeEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
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
  expect(screen.queryByLabelText('Connection owner')).not.toBeInTheDocument()
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

test('redeems a reset credit without crypto.randomUUID and reuses the request ID after failure', async () => {
  vi.stubGlobal('crypto', { getRandomValues: (bytes: Uint8Array) => { bytes.fill(7); return bytes } })
  vi.spyOn(window, 'confirm').mockReturnValue(true)
  api.getProviders.mockResolvedValue({ data: [{ id: 'codex', name: 'OpenAI Codex', description: 'Codex subscription', auth: 'oauth', protocol: 'codex', default_base_url: '', model_prefix: 'cx', custom_base_url: false, oauth_supported: true, oauth_refresh_required: true, quota_supported: true }] })
  api.getCredentials.mockResolvedValue([{ id: 'cred-1', name: 'Account 1', provider: 'codex', kind: 'oauth', base_url: 'https://chatgpt.com/backend-api', status: 'active', owner_tenant_id: null, created_at: '' }])
  api.getCredentialQuota.mockResolvedValue({ credential_id: 'cred-1', provider: 'codex', account: 'account', available: true, windows: [] })
  api.getCodexResetCredits.mockResolvedValue({ available_count: 1, credits: [{ selection_token: 'credit-1', title: 'Weekly reset', description: 'Reset weekly usage' }] })
  api.redeemCodexResetCredit.mockRejectedValueOnce(new Error('temporary failure')).mockResolvedValueOnce({ quota: { credential_id: 'cred-1', provider: 'codex', available: true, windows: [] } })

  render(<ProvidersPage />)
  const resetButtons = await screen.findAllByRole('button', { name: 'Reset credits' })
  fireEvent.click(resetButtons[resetButtons.length - 1])
  const redeemButtons = await screen.findAllByRole('button', { name: 'Redeem' })
  fireEvent.click(redeemButtons[redeemButtons.length - 1])
  expect(await screen.findByText('temporary failure')).toBeInTheDocument()
  const firstRequestID = api.redeemCodexResetCredit.mock.calls[0][2]
  expect(firstRequestID).toMatch(/^[0-9a-f-]{36}$/)

  const retryButtons = screen.getAllByRole('button', { name: 'Redeem' })
  fireEvent.click(retryButtons[retryButtons.length - 1])
  await waitFor(() => expect(api.redeemCodexResetCredit).toHaveBeenCalledTimes(2))
  expect(api.redeemCodexResetCredit).toHaveBeenLastCalledWith('cred-1', 'credit-1', firstRequestID)
  expect(await screen.findByText('Codex reset credit redeemed')).toBeInTheDocument()
})

test('runs one bounded chat test for every active account in a provider', async () => {
  api.getProviders.mockResolvedValue({ data: [{ id: 'codex', name: 'OpenAI Codex', description: 'Codex subscription', auth: 'oauth', protocol: 'codex', default_base_url: '', model_prefix: 'cx', custom_base_url: false, oauth_supported: true, oauth_refresh_required: true, quota_supported: false }] })
  api.getCredentials.mockResolvedValue([
    { id: 'cred-1', name: 'Account 1', provider: 'codex', kind: 'oauth', base_url: '', status: 'active', owner_tenant_id: null, created_at: '' },
    { id: 'cred-2', name: 'Account 2', provider: 'codex', kind: 'oauth', base_url: '', status: 'active', owner_tenant_id: null, created_at: '' },
    { id: 'cred-off', name: 'Disabled', provider: 'codex', kind: 'oauth', base_url: '', status: 'disabled', owner_tenant_id: null, created_at: '' },
  ])
  api.discoverModels.mockImplementation((id: string) => Promise.resolve({ object: 'list', data: [{ id: `${id}-model`, public_id: `cx/${id}-model`, object: 'model', permission: [], parent: null }], default_model: `${id}-model` }))
  api.requestStream.mockImplementation((_path: string, _body: object, onText: (text: string) => void) => { onText('connection healthy'); return Promise.resolve() })

  render(<ProvidersPage />)
  fireEvent.click(await screen.findByRole('button', { name: 'Chat all accounts' }))
  expect(screen.getByRole('dialog', { name: 'Chat all · OpenAI Codex' })).toBeInTheDocument()
  expect(screen.getByText('2 active accounts')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Run all accounts' }))

  await waitFor(() => expect(api.requestStream).toHaveBeenCalledTimes(2))
  expect(api.discoverModels).toHaveBeenCalledTimes(2)
  expect(api.requestStream.mock.calls.map((call: unknown[]) => call[0])).toEqual(expect.arrayContaining([
    '/admin/credentials/cred-1/chat-tests', '/admin/credentials/cred-2/chat-tests',
  ]))
  expect(screen.getByText('2/2 passed')).toBeInTheDocument()
  expect(api.requestStream.mock.calls.some((call: unknown[]) => String(call[0]).includes('cred-off'))).toBe(false)
})

test('reloads quota for every active account from the provider card', async () => {
  api.getProviders.mockResolvedValue({ data: [{ id: 'codex', name: 'OpenAI Codex', description: 'Codex subscription', auth: 'oauth', protocol: 'codex', default_base_url: '', model_prefix: 'cx', custom_base_url: false, oauth_supported: true, oauth_refresh_required: true, quota_supported: true }] })
  api.getCredentials.mockResolvedValue([
    { id: 'cred-1', name: 'Account 1', provider: 'codex', kind: 'oauth', base_url: '', status: 'active', owner_tenant_id: null, created_at: '' },
    { id: 'cred-2', name: 'Account 2', provider: 'codex', kind: 'oauth', base_url: '', status: 'active', owner_tenant_id: null, created_at: '' },
    { id: 'cred-off', name: 'Disabled', provider: 'codex', kind: 'oauth', base_url: '', status: 'disabled', owner_tenant_id: null, created_at: '' },
  ])
  api.getCredentialQuota.mockImplementation((id: string) => Promise.resolve({ credential_id: id, provider: 'codex', account: id, available: true, windows: [] }))
  api.refreshCredentialQuota.mockImplementation((id: string) => id === 'cred-2' ? Promise.reject(new Error('quota failed')) : Promise.resolve({ credential_id: id, provider: 'codex', account: id, available: true, windows: [] }))

  render(<ProvidersPage />)
  fireEvent.click(await screen.findByRole('button', { name: 'Reload all accounts' }))
  await waitFor(() => expect(api.refreshCredentialQuota).toHaveBeenCalledTimes(2))
  expect(api.refreshCredentialQuota).toHaveBeenCalledWith('cred-1')
  expect(api.refreshCredentialQuota).toHaveBeenCalledWith('cred-2')
  expect(api.refreshCredentialQuota).not.toHaveBeenCalledWith('cred-off')
  expect(await screen.findByText('1/2 accounts reloaded')).toBeInTheDocument()
})


test('tests every active provider account from the bulk shortcut', async () => {
  api.getProviders.mockResolvedValue({ data: [{ id: 'codex', name: 'OpenAI Codex', description: '', auth: 'oauth', protocol: 'codex', default_base_url: '', model_prefix: 'cx', custom_base_url: false, oauth_supported: true, oauth_refresh_required: true, quota_supported: true }] })
  api.getCredentials.mockResolvedValue([{ id: 'cred-1', name: 'One', provider: 'codex', kind: 'oauth', base_url: '', status: 'active', owner_tenant_id: null, created_at: '' }, { id: 'cred-2', name: 'Two', provider: 'codex', kind: 'oauth', base_url: '', status: 'active', owner_tenant_id: null, created_at: '' }, { id: 'off', name: 'Off', provider: 'codex', kind: 'oauth', base_url: '', status: 'disabled', owner_tenant_id: null, created_at: '' }])
  api.getCredentialQuota.mockResolvedValue({ credential_id: 'cred-1', provider: 'codex', available: true, windows: [] })
  api.testCredential.mockResolvedValue({ ok: true, status: 200, latency_ms: 10 })
  render(<ProvidersPage />)
  fireEvent.click(await screen.findByRole('button', { name: 'Test all accounts' }))
  expect(screen.getByRole('dialog', { name: 'Test all · OpenAI Codex' })).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Start all tests' }))
  await waitFor(() => expect(api.testCredential).toHaveBeenCalledTimes(2))
  expect(api.testCredential).not.toHaveBeenCalledWith('off')
  expect(await screen.findByText('2/2 healthy')).toBeInTheDocument()
})
