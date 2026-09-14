import { beforeEach, expect, test, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { OrganizationModelsModal } from './OrganizationModelsModal'
import * as api from '../api/client'
vi.mock('../api/client', () => ({ getOrganizationModels: vi.fn(), getOrganizationGrants: vi.fn(), publishOrganizationModel: vi.fn(), assignOrganizationModel: vi.fn(), getMembers: vi.fn() }))
beforeEach(() => {
 vi.mocked(api.getOrganizationModels).mockResolvedValue([{ organization_id: 'org', name: 'xno/g/default', kind: 'group', targets: ['xno/lite'], enabled: true, weekly_limit_usd: 5, created_at: '', updated_at: '' }])
 vi.mocked(api.getOrganizationGrants).mockResolvedValue([{ organization_id: 'org', model: 'xno/g/default', user_id: 'u', enabled: true, weekly_limit_usd: 2, updated_at: '' }])
 vi.mocked(api.getMembers).mockResolvedValue({ object: 'list', data: [] })
})
test('shows shared and per-user limits and revokes assignment without creating a key', async () => {
 render(<OrganizationModelsModal organization={{ id: 'org', name: 'xno', status: 'active', created_at: '', updated_at: '' }} onClose={() => {}} />)
 expect(await screen.findByText('xno/lite · $5/week')).toBeInTheDocument()
 expect(screen.getByText('u · $2/week')).toBeInTheDocument()
 fireEvent.click(screen.getByRole('button', { name: 'Revoke' }))
 await waitFor(() => expect(api.assignOrganizationModel).toHaveBeenCalledWith('org', expect.objectContaining({ user_id: 'u', model: 'xno/g/default', enabled: false, weekly_limit_usd: 2 })))
})
