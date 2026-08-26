import { render, screen } from '@testing-library/react'
import { expect, test } from 'vitest'
import { TokenBreakdown } from './TokenBreakdown'

test('renders tokens in input output cache-read cache-write order', () => {
  render(<TokenBreakdown input={10} output={20} cacheRead={30} cacheWrite={40} />)
  expect(screen.getByLabelText(/Tokens: input, output, cache read, cache write/)).toHaveTextContent('[I10/O20/CR30/CW40]')
})
