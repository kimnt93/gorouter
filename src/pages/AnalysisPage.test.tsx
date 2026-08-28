import { fireEvent, render, screen } from '@testing-library/react'
import { expect, test } from 'vitest'
import { HealthTable } from './AnalysisPage'

test('shows provider model and connection health without mixing client and provider failures', () => {
  render(<HealthTable health={[
    { dimension: 'provider', id: 'openai', requests: 100, successes: 98, client_errors: 1, provider_errors: 1, success_rate: .98, average_ms: 220, p95_ms: 640, cache_read_rate: .96 },
    { dimension: 'model', id: 'cx/gpt-5.6', requests: 50, successes: 50, client_errors: 0, provider_errors: 0, success_rate: 1, average_ms: 180, p95_ms: 400, cache_read_rate: .97 },
    { dimension: 'credential', id: 'cred-safe-id', requests: 25, successes: 24, client_errors: 0, provider_errors: 1, success_rate: .96, average_ms: 300, p95_ms: 900, cache_read_rate: .80 },
  ]} />)
  expect(screen.getByText('openai').closest('tr')).toHaveTextContent('98.0%')
  expect(screen.getByText('openai').closest('tr')).toHaveTextContent('96.0%')
  expect(screen.getByText('cx/gpt-5.6')).toBeInTheDocument()
  expect(screen.getByText('cred-safe-id')).toBeInTheDocument()
  expect(screen.getAllByText('Client 4xx')).toHaveLength(3)
  expect(screen.getAllByText('Provider 5xx')).toHaveLength(3)
})
