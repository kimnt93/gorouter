import { expect, test, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { PersonalModelAliasesModal } from './PersonalModelAliasesModal'
import * as api from '../api/client'
vi.mock('../api/client', () => ({ getPersonalAliases: vi.fn(async () => []), publishPersonalAlias: vi.fn(), assignPersonalAlias: vi.fn(), setAssignedModelLimit: vi.fn() }))
test('submits an optional personal cap with zero meaning unlimited', async () => {
 render(<PersonalModelAliasesModal onClose={() => {}} />)
 fireEvent.change(screen.getByLabelText('Assigned model ID'), { target: { value: 'org/xno/model' } })
 fireEvent.click(screen.getByRole('button', { name: 'Save my cap' }))
 await waitFor(() => expect(api.setAssignedModelLimit).toHaveBeenCalledWith({ model: 'org/xno/model', weekly_limit_usd: 0 }))
 expect(await screen.findByRole('status')).toHaveTextContent('Saved')
})
