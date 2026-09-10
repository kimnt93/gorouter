import { describe, expect, it } from 'vitest'
import { maskSecretPreview } from './credentials'

describe('maskSecretPreview', () => {
  it('shows persisted API-key edges while replacing the hidden middle', () => {
    expect(maskSecretPreview('ghp_fsf…o28fk')).toBe('ghp_fsf******o28fk')
    expect(maskSecretPreview('gsk_NX...j9Nn0')).toBe('gsk_NX******j9Nn0')
  })

  it('never expands missing or short previews', () => {
    expect(maskSecretPreview()).toBe('encrypted API key')
    expect(maskSecretPreview('…')).toBe('******')
    expect(maskSecretPreview('••••••')).toBe('••••••')
  })
})
