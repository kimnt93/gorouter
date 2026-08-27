import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, expect, test, vi } from 'vitest'
import { OrganizationsPage } from './OrganizationsPage'

const api = vi.hoisted(() => ({
  getOrganizations: vi.fn(), getUsers: vi.fn(), getMembers: vi.fn(), addMember: vi.fn(),
  createOrganization: vi.fn(), deleteMember: vi.fn(), updateMember: vi.fn(), updateOrganization: vi.fn(),
}))
vi.mock('../api/client', () => api)
vi.mock('../context/SessionContext', () => ({ useSession: () => ({ isMasterView: true, viewOrganizationID: '', viewUserID: '', has: () => true }) }))

beforeEach(() => {
  api.getOrganizations.mockResolvedValue({ object: 'list', data: [
    { id: 'org-1', name: 'Microsoft', status: 'active', created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z', member_count: 1 },
    { id: 'org-2', name: 'NASA', status: 'active', created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z', member_count: 2 },
  ] })
  api.getUsers.mockImplementation((email = '') => Promise.resolve({ object: 'list', data: email ? [{
    id: 'user-2', username: 'grace@example.com', status: 'active', created_at: '', updated_at: '',
    memberships: [{ organization_id: 'org-2', user_id: 'user-2', role: 'admin', created_at: '' }],
  }] : [] }))
  api.getMembers.mockResolvedValue({ object: 'list', data: [{ organization_id: 'org-1', user_id: 'user-1', username: 'member@example.com', role: 'member', created_at: '' }] })
  api.addMember.mockResolvedValue({})
})

test('finds an exact email, shows joined organizations, and defaults to member', async () => {
  render(<OrganizationsPage />)
  fireEvent.click((await screen.findAllByRole('button', { name: 'Manage members' }))[0])
  fireEvent.change(screen.getByLabelText('User email'), { target: { value: 'grace@example.com' } })

  expect(await screen.findByText('grace@example.com')).toBeInTheDocument()
  expect(screen.getByText('NASA (admin)')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Add member' }))
  await waitFor(() => expect(api.addMember).toHaveBeenCalledWith('org-1', 'user-2', 'member'))
})

test('shows user not found for an unknown exact email', async () => {
  api.getUsers.mockResolvedValue({ object: 'list', data: [] })
  render(<OrganizationsPage />)
  fireEvent.click((await screen.findAllByRole('button', { name: 'Manage members' }))[0])
  fireEvent.change(screen.getByLabelText('User email'), { target: { value: 'missing@example.com' } })
  expect(await screen.findByText('User not found')).toBeInTheDocument()
})
