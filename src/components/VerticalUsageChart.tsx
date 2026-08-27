import { useMemo } from 'react'
import type { UsageActivityBucket } from '../api/contracts'
import { formatDateBucket, formatInteger, formatUSD } from '../lib/format'
import { TruncatedText } from './SearchableSelect'

type Metric = 'requests' | 'tokens' | 'cost' | 'cache'

interface Segment { key: string; label: string; value: number; color: string }
interface Bucket { start: string; segments: Segment[]; total: number }

const colors = ['#a99af4', '#78aaf7', '#62c996', '#e2ad61', '#e47c9f', '#69c6cf', '#c9a4ff', '#8fc16a']

function aggregate(data: UsageActivityBucket[], metric: Metric): Bucket[] {
  const grouped = new Map<string, UsageActivityBucket[]>()
  for (const row of data) grouped.set(row.start, [...(grouped.get(row.start) ?? []), row])
  const userKeys = [...new Set(data.map((row) => row.user_id || row.username || 'legacy'))].sort()
  const userColors = new Map(userKeys.map((key, index) => [key, colors[index % colors.length]]))
  return [...grouped.entries()].sort(([a], [b]) => a.localeCompare(b)).map(([start, rows]) => {
    let segments: Segment[]
    if (metric === 'requests') {
      const users = new Map<string, { label: string; value: number }>()
      for (const row of rows) {
        const key = row.user_id || row.username || 'legacy'
        const current = users.get(key)
        users.set(key, { label: row.username || row.user_id || 'Legacy / unknown', value: (current?.value ?? 0) + row.requests })
      }
      segments = [...users.entries()].sort(([a], [b]) => a.localeCompare(b)).map(([key, item]) => ({ key, label: item.label, value: item.value, color: userColors.get(key) ?? colors[0] }))
    } else {
      const sum = (pick: (row: UsageActivityBucket) => number) => rows.reduce((total, row) => total + pick(row), 0)
      segments = metric === 'tokens' ? [
        { key: 'input', label: 'Input', value: sum((row) => row.prompt_tokens), color: colors[0] },
        { key: 'output', label: 'Output', value: sum((row) => row.completion_tokens), color: colors[1] },
        { key: 'cache-read', label: 'Cache read', value: sum((row) => row.cache_read_tokens), color: colors[2] },
        { key: 'cache-write', label: 'Cache write', value: sum((row) => row.cache_write_tokens), color: colors[3] },
      ] : metric === 'cost' ? [
        { key: 'input', label: 'Input', value: sum((row) => row.input_cost_usd), color: colors[0] },
        { key: 'output', label: 'Output', value: sum((row) => row.output_cost_usd), color: colors[1] },
        { key: 'cache-read', label: 'Cache read', value: sum((row) => row.cache_read_cost_usd), color: colors[2] },
        { key: 'cache-write', label: 'Cache write', value: sum((row) => row.cache_write_cost_usd), color: colors[3] },
      ] : [
        { key: 'cache-read', label: 'Cache read', value: sum((row) => row.cache_read_tokens), color: colors[2] },
        { key: 'cache-write', label: 'Cache write', value: sum((row) => row.cache_write_tokens), color: colors[3] },
      ]
      if (metric === 'cost') {
        const recordedTotal = sum((row) => row.cost_usd)
        const componentTotal = segments.reduce((total, segment) => total + segment.value, 0)
        const legacy = Math.max(0, recordedTotal - componentTotal)
        if (legacy > 0.0000001) segments.push({ key: 'legacy-cost', label: 'Unattributed (legacy)', value: legacy, color: '#8b8b8b' })
      }
    }
    return { start, segments, total: segments.reduce((sum, segment) => sum + segment.value, 0) }
  })
}

export function VerticalUsageChart({ data, metric }: { data: UsageActivityBucket[]; metric: Metric }) {
  const buckets = useMemo(() => aggregate(data, metric), [data, metric])
  const max = Math.max(1, ...buckets.map((bucket) => bucket.total))
  const legend = useMemo(() => {
    const entries = new Map<string, Segment>()
    for (const bucket of buckets) for (const segment of bucket.segments) if (!entries.has(segment.key)) entries.set(segment.key, segment)
    return [...entries.values()]
  }, [buckets])
  if (buckets.length === 0) return <div className="empty-state"><strong>No activity in this range</strong><span>Try a wider range or clear the selected identity values.</span></div>
  const display = (value: number) => metric === 'cost' ? formatUSD(value) : formatInteger(value)
  return <div className="vertical-chart-wrap">
    <div className="vertical-chart" aria-label={`${metric} activity trend`}>
      {buckets.map((bucket) => {
        const height = metric === 'requests' ? (bucket.total > 0 ? 100 : 0) : bucket.total / max * 100
        return <div className="vertical-column" key={bucket.start} tabIndex={0}>
          <div className="vertical-value">{metric === 'requests' ? `${formatInteger(bucket.total)}` : display(bucket.total)}</div>
          <div className="vertical-track"><div className="vertical-stack" style={{ height: `${height}%` }}>
            {bucket.segments.filter((segment) => segment.value > 0).map((segment) => <i key={segment.key} style={{ background: segment.color, height: `${bucket.total ? segment.value / bucket.total * 100 : 0}%` }} />)}
          </div></div>
          <time dateTime={bucket.start}>{formatDateBucket(bucket.start)}</time>
          <div className="chart-tooltip"><strong>{formatDateBucket(bucket.start)}</strong>{bucket.segments.map((segment) => <span key={segment.key}><i style={{ background: segment.color }} /><TruncatedText>{segment.label}</TruncatedText><b>{display(segment.value)}</b></span>)}<span className="tooltip-total">Total<b>{display(bucket.total)}{metric === 'requests' ? ' · 100%' : ''}</b></span></div>
        </div>
      })}
    </div>
    <div className="chart-legend">{legend.map((segment) => <span key={segment.key}><i style={{ background: segment.color }} /><TruncatedText>{segment.label}</TruncatedText></span>)}</div>
  </div>
}
