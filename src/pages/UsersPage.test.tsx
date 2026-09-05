import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import { UsersPage } from './UsersPage'

const api = vi.hoisted(() => ({ createUser: vi.fn(), deleteUser: vi.fn(), getUsers: vi.fn(), setUserStatus: vi.fn() }))
vi.mock('../api/client', () => api)
afterEach(cleanup)
beforeEach(() => { vi.clearAllMocks(); api.getUsers.mockResolvedValue({ object: 'list', data: [{ id: 'user-1', username: 'person@example.test', status: 'active', created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z' }] }); api.deleteUser.mockResolvedValue({ ok: true }); vi.spyOn(window, 'confirm').mockReturnValue(true) })

test('confirms and deletes a user with cascading access warning', async () => {
  render(<UsersPage />)
  fireEvent.click(await screen.findByRole('button', { name: 'Delete' }))
  expect(window.confirm).toHaveBeenCalledWith(expect.stringContaining('API keys, provider connections, memberships'))
  await waitFor(() => expect(api.deleteUser).toHaveBeenCalledWith('user-1'))
  expect(api.getUsers).toHaveBeenCalledTimes(2)
})
