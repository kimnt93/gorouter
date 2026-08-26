import { formatInteger } from '../lib/format'

interface Props { input: number; output: number; cacheRead: number; cacheWrite: number; compact?: boolean }

export function TokenBreakdown({ input, output, cacheRead, cacheWrite, compact = false }: Props) {
  const values = [
    { label: 'Input', short: 'I', value: input, className: 'token-input' },
    { label: 'Output', short: 'O', value: output, className: 'token-output' },
    { label: 'Cache read', short: 'CR', value: cacheRead, className: 'token-read' },
    { label: 'Cache write', short: 'CW', value: cacheWrite, className: 'token-write' },
  ]
  return <div className={compact ? 'token-breakdown compact' : 'token-breakdown'} aria-label="Tokens: input, output, cache read, cache write">
    <span className="bracket">[</span>{values.map((item, index) => <span className={`token-value ${item.className}`} title={item.label} key={item.short}>{!compact && <small>{item.short}</small>}{formatInteger(item.value)}{index < values.length - 1 && <i>/</i>}</span>)}<span className="bracket">]</span>
  </div>
}
