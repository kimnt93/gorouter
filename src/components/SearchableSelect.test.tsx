import { fireEvent, render, screen } from '@testing-library/react'
import { expect, test, vi } from 'vitest'
import { SearchableSelect } from './SearchableSelect'

test('searches numbered options and returns the selected value', () => {
  const onChange = vi.fn()
  render(<SearchableSelect value="" onChange={onChange} options={[{ value: 'one', label: 'First organization' }, { value: 'two', label: 'Second organization', meta: '12 members' }]} />)
  fireEvent.click(screen.getByRole('button', { name: /select an option/i }))
  expect(screen.getByText('01')).toBeInTheDocument()
  expect(screen.getByText('02')).toBeInTheDocument()
  fireEvent.change(screen.getByRole('textbox', { name: 'Search options' }), { target: { value: 'second' } })
  expect(screen.queryByText('First organization')).not.toBeInTheDocument()
  fireEvent.click(screen.getByRole('option', { name: /second organization/i }))
  expect(onChange).toHaveBeenCalledWith('two')
})
