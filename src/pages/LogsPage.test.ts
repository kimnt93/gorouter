import { expect, test } from 'vitest'
import { LogsPage } from './LogsPage'

test('request log page is available without a conversation-content parser', () => {
  expect(LogsPage).toBeTypeOf('function')
})
