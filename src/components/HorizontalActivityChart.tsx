import type { UsageActivityBucket } from '../api/contracts'
import { formatDateBucket, formatInteger } from '../lib/format'

type Metric = 'requests' | 'tokens' | 'cache'

export function HorizontalActivityChart({ data, metric = 'requests' }: { data: UsageActivityBucket[]; metric?: Metric }) {
  const value = (bucket: UsageActivityBucket) => metric === 'requests' ? bucket.requests : metric === 'cache' ? bucket.cache_read_tokens + bucket.cache_write_tokens : bucket.prompt_tokens + bucket.completion_tokens + bucket.cache_read_tokens + bucket.cache_write_tokens
  const max = Math.max(1, ...data.map(value))
  if (data.length === 0) return <div className="empty-state"><strong>No activity in this range</strong><span>Try a wider range or clear a user/API-key filter.</span></div>
  return <div className="horizontal-chart" aria-label={`${metric} activity trend`}>{data.map((bucket) => {
    const amount = value(bucket)
    return <div className="chart-row" key={bucket.start}><time dateTime={bucket.start}>{formatDateBucket(bucket.start)}</time><div className="bar-track"><div className={`bar-fill metric-${metric}`} style={{ width: `${Math.max(amount > 0 ? 2 : 0, amount / max * 100)}%` }} /></div><strong>{formatInteger(amount)}</strong></div>
  })}</div>
}
