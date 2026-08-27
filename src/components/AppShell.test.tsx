import { render, screen } from '@testing-library/react'
import { beforeEach, expect, test, vi } from 'vitest'
import { AppShell } from './AppShell'

const api = vi.hoisted(() => ({ getOrganizations: vi.fn(), getUsers: vi.fn() }))
vi.mock('../api/client', () => api)
vi.mock('../context/SessionContext', () => ({ useSession: () => ({
  session: { role: 'master', scopes: [] }, isMaster: true, isMasterView: false, has: () => true,
}) }))

beforeEach(() => {
  window.history.replaceState({}, '', '/dashboard/organizations?view_user_id=user-1&organization_id=org-1')
  api.getOrganizations.mockResolvedValue({ object: 'list', data: [{ id: 'org-1', name: 'Microsoft', status: 'active', created_at: '', updated_at: '', member_count: 2 }] })
  api.getUsers.mockResolvedValue({ object: 'list', data: [{
    id: 'user-1', username: 'ada@example.com', status: 'active', created_at: '', updated_at: '',
    memberships: [{ organization_id: 'org-1', user_id: 'user-1', role: 'admin', created_at: '' }],
  }] })
})

test('shows the selected user, organization, and colored organization role', async () => {
  render(<AppShell><div>Page content</div></AppShell>)
  expect((await screen.findAllByText('ada@example.com')).length).toBeGreaterThan(0)
  expect(screen.getByText('Microsoft')).toBeInTheDocument()
  expect(screen.getByText('admin')).toHaveClass('role-chip', 'admin')
  expect(screen.getByRole('button', { name: 'Return to Master view' })).toBeInTheDocument()
})
