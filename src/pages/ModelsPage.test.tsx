import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import { ModelsPage } from './ModelsPage'

const api = vi.hoisted(() => ({
  getModels: vi.fn(), getCredentials: vi.fn(), getPricingCatalog: vi.fn(), discoverModels: vi.fn(),
  deleteModel: vi.fn(), deletePrice: vi.fn(), saveModel: vi.fn(), savePrice: vi.fn(),
}))
vi.mock('../api/client', () => api)
vi.mock('../context/SessionContext', () => ({ useSession: () => ({ isMaster: true }) }))
afterEach(cleanup)

beforeEach(() => {
  api.getModels.mockResolvedValue([])
  api.getCredentials.mockResolvedValue([{
    id: 'codex-1', name: 'OpenAI Codex', provider: 'codex', kind: 'oauth', base_url: '', status: 'active', owner_tenant_id: null, created_at: '',
  }])
  api.getPricingCatalog.mockResolvedValue([{
    model: 'openai/gpt-5.6-luna', name: 'GPT 5.6 Luna', provider: 'openai', cache_supported: true, source: 'catalog', updated_at: '',
    price: { input_per_m: 0.19999999999999998, output_per_m: 1.2, cached_input_per_m: 0.02, cache_write_per_m: 0.25 },
  }])
  api.discoverModels.mockResolvedValue({
    object: 'list', provider: 'codex', data: [{ id: 'gpt-5.6-luna', public_id: 'cx/gpt-5.6-luna', name: 'GPT 5.6 Luna' }],
  })
})

test('shows copyable curl usage for a configured blend', async () => {
  api.getModels.mockResolvedValue([{ name: 'cx/gpt-5.6-luna', upstream_model: 'gpt-5.6-luna', strategy: 'priority', enabled: true, routes: [{ credential_id: 'codex-1', priority: 0, weight: 1, enabled: true }] }])
  render(<ModelsPage />)

  await screen.findByText('cx/gpt-5.6-luna')
  fireEvent.click(screen.getByRole('button', { name: /Model blends/ }))
  fireEvent.click(screen.getByRole('button', { name: 'View usage' }))

  expect(screen.getAllByText(/GOROUTER_API_KEY/).length).toBeGreaterThan(0)
  expect(screen.getByText(/chat\/completions/)).toBeInTheDocument()
  expect(screen.getByText(/"model":"cx\/gpt-5.6-luna"/)).toBeInTheDocument()
})

test('lists connected public model with inherited original-model cost and prefills a blend', async () => {
  render(<ModelsPage />)

  expect(await screen.findByText('cx/gpt-5.6-luna')).toBeInTheDocument()
  expect(screen.getByText('In $0.2000')).toBeInTheDocument()
  expect(screen.getByText('Out $1.2000')).toBeInTheDocument()
  expect(screen.getByText('Read $0.0200')).toBeInTheDocument()
  expect(screen.getByText('Write $0.2500')).toBeInTheDocument()
  expect(screen.getByText('catalog · openai/gpt-5.6-luna')).toBeInTheDocument()

  fireEvent.click(screen.getByRole('button', { name: 'Add to blends' }))
  expect(screen.getByRole('dialog')).toBeInTheDocument()
  expect(screen.getByLabelText('Public model name')).toHaveValue('cx/gpt-5.6-luna')
  expect(screen.getByLabelText('Original upstream model')).toHaveValue('gpt-5.6-luna')
  expect(screen.getByText(/Uses openai\/gpt-5.6-luna: \$0.2000\/M input/)).toBeInTheDocument()
})

test('offers and saves prompt-cache affinity routing for a blend', async () => {
  api.saveModel.mockResolvedValue({ ok: true })
  render(<ModelsPage />)
  await screen.findByText('cx/gpt-5.6-luna')
  fireEvent.click(screen.getByRole('button', { name: 'Add to blends' }))
  fireEvent.click(screen.getByRole('button', { name: 'Strategy' }))
  fireEvent.click(screen.getByRole('option', { name: /Prompt-cache affinity/ }))
  fireEvent.click(screen.getByRole('button', { name: 'Save blend' }))
  expect(api.saveModel).toHaveBeenCalledWith(expect.objectContaining({ strategy: 'cache_affinity' }))
})
