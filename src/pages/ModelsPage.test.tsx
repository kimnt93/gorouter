import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, expect, test, vi } from 'vitest'
import { ModelsPage } from './ModelsPage'

const api = vi.hoisted(() => ({
  getModels: vi.fn(), getCredentials: vi.fn(), getPricingCatalog: vi.fn(), discoverModels: vi.fn(),
  deleteModel: vi.fn(), deletePrice: vi.fn(), saveModel: vi.fn(), savePrice: vi.fn(),
}))
vi.mock('../api/client', () => api)
vi.mock('../context/SessionContext', () => ({ useSession: () => ({ isMaster: true }) }))

beforeEach(() => {
  api.getModels.mockResolvedValue([])
  api.getCredentials.mockResolvedValue([{
    id: 'codex-1', name: 'OpenAI Codex', provider: 'codex', kind: 'oauth', base_url: '', status: 'active', owner_tenant_id: null, created_at: '',
  }])
  api.getPricingCatalog.mockResolvedValue([{
    model: 'openai/gpt-5.6-luna', name: 'GPT 5.6 Luna', provider: 'openai', cache_supported: true, source: 'catalog', updated_at: '',
    price: { input_per_m: 0.2, output_per_m: 1.2, cached_input_per_m: 0.02, cache_write_per_m: 0.25 },
  }])
  api.discoverModels.mockResolvedValue({
    object: 'list', provider: 'codex', data: [{ id: 'gpt-5.6-luna', public_id: 'cx/gpt-5.6-luna', name: 'GPT 5.6 Luna' }],
  })
})

test('lists connected public model with inherited original-model cost and prefills a blend', async () => {
  render(<ModelsPage />)

  expect(await screen.findByText('cx/gpt-5.6-luna')).toBeInTheDocument()
  expect(screen.getByText('In $0.200')).toBeInTheDocument()
  expect(screen.getByText('Out $1.200')).toBeInTheDocument()
  expect(screen.getByText('Read $0.020')).toBeInTheDocument()
  expect(screen.getByText('Write $0.250')).toBeInTheDocument()
  expect(screen.getByText('catalog · openai/gpt-5.6-luna')).toBeInTheDocument()

  fireEvent.click(screen.getByRole('button', { name: 'Add to blends' }))
  expect(screen.getByRole('dialog')).toBeInTheDocument()
  expect(screen.getByLabelText('Public model name')).toHaveValue('cx/gpt-5.6-luna')
  expect(screen.getByLabelText('Original upstream model')).toHaveValue('gpt-5.6-luna')
  expect(screen.getByText(/Uses openai\/gpt-5.6-luna/)).toBeInTheDocument()
})
