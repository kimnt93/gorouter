import { expect, test } from 'vitest'
import { parseConversation } from './LogsPage'

test('parses request and assistant messages for log conversation detail', () => {
  const messages = parseConversation(
    JSON.stringify({ messages: [{ role: 'system', content: 'Be concise' }, { role: 'user', content: [{ type: 'text', text: 'Hello' }] }] }),
    JSON.stringify({ choices: [{ message: { role: 'assistant', content: 'Hi', reasoning_content: 'Short reasoning' } }] }),
  )
  expect(messages).toEqual([
    { role: 'system', content: 'Be concise' },
    { role: 'user', content: 'Hello' },
    { role: 'assistant', content: 'Reasoning\nShort reasoning\n\nHi' },
  ])
})
