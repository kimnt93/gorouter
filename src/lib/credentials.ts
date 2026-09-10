export function maskSecretPreview(value = ''): string {
  if (!value) return 'encrypted API key'
  if (/^[•*]+$/.test(value)) return value
  const separator = value.includes('…') ? '…' : value.includes('...') ? '...' : ''
  if (separator) {
    const [prefix, suffix = ''] = value.split(separator, 2)
    return `${prefix}${'*'.repeat(6)}${suffix}`
  }
  if (value.length <= 8) return '••••••'
  return `${value.slice(0, 6)}${'*'.repeat(6)}${value.slice(-4)}`
}
