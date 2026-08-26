export const formatInteger = (value: number): string => new Intl.NumberFormat('en-US', { notation: Math.abs(value) >= 100_000 ? 'compact' : 'standard', maximumFractionDigits: 1 }).format(value)
export const formatUSD = (value: number): string => new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD', minimumFractionDigits: value < 1 ? 4 : 2, maximumFractionDigits: value < 1 ? 4 : 2 }).format(value)
export const formatDateTime = (value: string): string => new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'medium' }).format(new Date(value))
export const formatDateBucket = (value: string): string => new Intl.DateTimeFormat(undefined, { month: 'short', day: '2-digit', hour: '2-digit' }).format(new Date(value))
export const relativeTime = (value: string): string => {
  const delta = Date.now() - new Date(value).getTime()
  if (delta < 60_000) return `${Math.max(0, Math.floor(delta / 1000))}s ago`
  if (delta < 3_600_000) return `${Math.floor(delta / 60_000)}m ago`
  if (delta < 86_400_000) return `${Math.floor(delta / 3_600_000)}h ago`
  return formatDateTime(value)
}
