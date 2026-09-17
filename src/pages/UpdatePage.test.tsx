import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, expect, test, vi } from 'vitest'
import { UpdatePage } from './UpdatePage'
const api = vi.hoisted(() => ({ checkForUpdates: vi.fn() }))
vi.mock('../api/client', () => api)
vi.mock('../context/SessionContext', () => ({ useSession: () => ({ isMaster: true }) }))
beforeEach(() => { vi.clearAllMocks() })
test('only checks on click and displays a pinned image without performing an update', async () => {
  api.checkForUpdates.mockResolvedValue({ installed: 'v0.2.1', latest: 'v0.2.2', image: 'ghcr.io/kimnt93/gorouter:v0.2.2', release_url: 'https://github.com/kimnt93/gorouter/releases/tag/v0.2.2', update_available: true, checked_at: '2026-09-17T00:00:00Z' })
  render(<UpdatePage />)
  expect(api.checkForUpdates).not.toHaveBeenCalled()
  fireEvent.click(screen.getByRole('button', { name: 'Check for updates' }))
  expect(await screen.findByRole('status')).toHaveTextContent('A newer release is available')
  expect(screen.getByText(/docker pull ghcr.io\/kimnt93\/gorouter:v0.2.2/)).toBeInTheDocument()
  expect(screen.getByRole('link', { name: 'View release notes ↗' })).toHaveAttribute('rel', 'noopener noreferrer')
})
