import { expect, test } from 'vitest'
import { createIdempotencyKey } from './idempotency'

test('creates a UUID when randomUUID is unavailable', () => {
  const cryptoAPI = {
    getRandomValues<T extends Exclude<BufferSource, ArrayBuffer>>(array: T): T {
      new Uint8Array(array.buffer, array.byteOffset, array.byteLength).forEach((_, index, bytes) => { bytes[index] = index })
      return array
    },
  }

  expect(createIdempotencyKey(cryptoAPI)).toBe('00010203-0405-4607-8809-0a0b0c0d0e0f')
})

test('creates a bounded fallback identifier without browser crypto', () => {
  const key = createIdempotencyKey(null)
  expect(key).toMatch(/^reset-[a-z0-9]+-[a-z0-9]+-[a-z0-9]+$/)
  expect(key.length).toBeLessThan(128)
})
