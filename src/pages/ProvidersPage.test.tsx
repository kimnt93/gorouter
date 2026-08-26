import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, expect, test, vi } from 'vitest'
import { ProvidersPage } from './ProvidersPage'

const api = vi.hoisted(() => ({
  getProviders: vi.fn(), getCredentials: vi.fn(), getOrganizations: vi.fn(), createCredential: vi.fn(), deleteCredential: vi.fn(), discoverModels: vi.fn(), importModels: vi.fn(), requestStream: vi.fn(), startOAuth: vi.fn(), completeOAuth: vi.fn(), testCredential: vi.fn(), updateCredential: vi.fn(),
}))
vi.mock('../api/client', () => api)

beforeEach(() => {
  api.getProviders.mockResolvedValue({ data: [{ id: 'custom', name: 'Custom provider', description: 'Compatible endpoint', auth: 'api_key', protocol: 'openai', default_base_url: '', model_prefix: 'custom', custom_base_url: true, oauth_supported: false, oauth_refresh_required: false }] })
  api.getCredentials.mockResolvedValue([])
  api.getOrganizations.mockResolvedValue({ object: 'list', data: [] })
})

test('exposes the provider API-key connection workflow', async () => {
  render(<ProvidersPage />)
  const connect = await screen.findByRole('button', { name: 'Connect Custom provider' })
  fireEvent.click(connect)
  expect(screen.getByRole('dialog')).toBeInTheDocument()
  expect(screen.getByLabelText('API key')).toHaveAttribute('type', 'password')
  expect(screen.getByLabelText('Base URL')).toBeEnabled()
  await waitFor(() => expect(api.getCredentials).toHaveBeenCalledOnce())
})
