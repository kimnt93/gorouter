import { useMemo, useRef, useState, type FocusEvent, type PointerEvent, type ReactNode, type RefObject } from 'react'
import type { GroupBy, UsageActivityBucket } from '../api/contracts'
import { formatDateBucket, formatInteger, formatUSD } from '../lib/format'
import { TruncatedText } from './SearchableSelect'

type Metric = 'requests' | 'tokens' | 'cost'

interface Segment { key: string; label: string; value: number; color: string }
interface Bucket { start: string; segments: Segment[]; total: number }

const colors = ['#a99af4', '#78aaf7', '#62c996', '#e2ad61', '#e47c9f', '#69c6cf', '#c9a4ff', '#8fc16a']

interface TooltipState<T> { item: T; x: number; y: number }

function tooltipPosition(x: number, y: number) {
  const viewportWidth = document.documentElement.clientWidth || window.innerWidth
  return { x: Math.max(152, Math.min(viewportWidth - 152, x)), y }
}

function useChartTooltip<T>() {
  const [tooltip, setTooltip] = useState<TooltipState<T> | null>(null)
  const tooltipRef = useRef<HTMLDivElement>(null)
  const activate = (item: T, event: PointerEvent<HTMLElement>) => {
    const position = tooltipPosition(event.clientX, event.clientY)
    setTooltip({ item, ...position })
  }
  const move = (event: PointerEvent<HTMLElement>) => {
    const element = tooltipRef.current
    if (!element) return
    const position = tooltipPosition(event.clientX, event.clientY)
    element.style.left = `${position.x}px`
    element.style.top = `${position.y}px`
  }
  const focus = (item: T, event: FocusEvent<HTMLElement>) => {
    const bounds = event.currentTarget.getBoundingClientRect()
    const position = tooltipPosition(bounds.left + bounds.width / 2, bounds.top + Math.min(56, bounds.height / 2))
    setTooltip({ item, ...position })
  }
  return { tooltip, tooltipRef, activate, move, focus, hide: () => setTooltip(null) }
}

function CursorTooltip<T>({ state, tooltipRef, children }: { state: TooltipState<T>; tooltipRef: RefObject<HTMLDivElement | null>; children: (item: T) => ReactNode }) {
  return <div ref={tooltipRef} className="chart-tooltip chart-cursor-tooltip" role="tooltip" style={{ left: state.x, top: state.y }}>{children(state.item)}</div>
}

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
      ] : [
        { key: 'input', label: 'Input', value: sum((row) => row.input_cost_usd), color: colors[0] },
        { key: 'output', label: 'Output', value: sum((row) => row.output_cost_usd), color: colors[1] },
        { key: 'cache-read', label: 'Cache read', value: sum((row) => row.cache_read_cost_usd), color: colors[2] },
        { key: 'cache-write', label: 'Cache write', value: sum((row) => row.cache_write_cost_usd), color: colors[3] },
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

interface CacheBucket {
  start: string
  input: number
  output: number
  read: number
  write: number
  context: number
  usage: number
  rate: number
}

function aggregateCache(data: UsageActivityBucket[]): CacheBucket[] {
  const grouped = new Map<string, UsageActivityBucket[]>()
  for (const row of data) grouped.set(row.start, [...(grouped.get(row.start) ?? []), row])
  return [...grouped.entries()].sort(([a], [b]) => a.localeCompare(b)).map(([start, rows]) => {
    const sum = (pick: (row: UsageActivityBucket) => number) => rows.reduce((total, row) => total + pick(row), 0)
    const input = sum((row) => row.prompt_tokens)
    const output = sum((row) => row.completion_tokens)
    const read = sum((row) => row.cache_read_tokens)
    const write = sum((row) => row.cache_write_tokens)
    const context = input + read
    return { start, input, output, read, write, context, usage: context + output + write, rate: context ? read / context * 100 : 0 }
  })
}

export function CacheEfficiencyChart({ data, groupBy }: { data: UsageActivityBucket[]; groupBy: GroupBy }) {
  const buckets = useMemo(() => aggregateCache(data), [data])
  const maxUsage = Math.max(1, ...buckets.map((bucket) => bucket.usage))
  const hover = useChartTooltip<CacheBucket>()
  if (buckets.length === 0) return <div className="empty-state"><strong>No cache activity in this range</strong><span>Try a wider range or clear the selected identity values.</span></div>
  return <div className="vertical-chart-wrap cache-efficiency-wrap">
    <div className="vertical-chart cache-efficiency-chart" aria-label="cache read share by time bucket" onPointerLeave={hover.hide}>
      {buckets.map((bucket) => <div className="vertical-column cache-efficiency-column" key={bucket.start} tabIndex={0} aria-label={`${formatDateBucket(bucket.start, groupBy)} bucket`} onPointerEnter={(event) => hover.activate(bucket, event)} onPointerMove={hover.move} onFocus={(event) => hover.focus(bucket, event)} onBlur={hover.hide}>
        <div className="vertical-value cache-rate-value">{bucket.rate.toFixed(1)}%</div>
        <div className="vertical-track"><div className="vertical-stack cache-efficiency-stack" style={{ height: `${bucket.usage / maxUsage * 100}%` }}>
          {bucket.input > 0 && <i className="cache-uncached-segment" style={{ height: `${100 - bucket.rate}%` }} />}
          {bucket.read > 0 && <i className="cache-read-segment" style={{ height: `${bucket.rate}%` }} />}
        </div></div>
        <time dateTime={bucket.start}>{formatDateBucket(bucket.start, groupBy)}</time>
      </div>)}
    </div>
    {hover.tooltip && <CursorTooltip state={hover.tooltip} tooltipRef={hover.tooltipRef}>{(bucket) => <><strong>{formatDateBucket(bucket.start, groupBy)}</strong>
          <span><i className="cache-read-swatch" /><span>Cache read</span><b>{formatInteger(bucket.read)} · {bucket.rate.toFixed(1)}%</b></span>
          <span><i className="cache-uncached-swatch" /><span>Uncached input</span><b>{formatInteger(bucket.input)} · {(100 - bucket.rate).toFixed(1)}%</b></span>
          <span><i className="cache-write-swatch" /><span>Cache write</span><b>{formatInteger(bucket.write)}</b></span>
          <span><i className="output-swatch" /><span>Output</span><b>{formatInteger(bucket.output)}</b></span>
          <span className="tooltip-total">Total usage tokens<b>{formatInteger(bucket.usage)}</b></span>
        </>}</CursorTooltip>}
    <div className="chart-legend"><span><i className="cache-read-swatch" />Cache read</span><span><i className="cache-uncached-swatch" />Uncached input</span><span className="legend-note">Bar height compares total usage tokens; its split and label show cache-read share of input context. Hover for exact totals.</span></div>
  </div>
}

export function VerticalUsageChart({ data, metric, groupBy }: { data: UsageActivityBucket[]; metric: Metric; groupBy: GroupBy }) {
  const buckets = useMemo(() => aggregate(data, metric), [data, metric])
  const max = Math.max(1, ...buckets.map((bucket) => bucket.total))
  const hover = useChartTooltip<Bucket>()
  const legend = useMemo(() => {
    const entries = new Map<string, Segment>()
    for (const bucket of buckets) for (const segment of bucket.segments) if (!entries.has(segment.key)) entries.set(segment.key, segment)
    return [...entries.values()]
  }, [buckets])
  if (buckets.length === 0) return <div className="empty-state"><strong>No activity in this range</strong><span>Try a wider range or clear the selected identity values.</span></div>
  const display = (value: number) => metric === 'cost' ? formatUSD(value) : formatInteger(value)
  return <div className="vertical-chart-wrap">
    <div className="vertical-chart" aria-label={`${metric} activity trend`} onPointerLeave={hover.hide}>
      {buckets.map((bucket) => {
        const height = metric === 'requests' ? (bucket.total > 0 ? 100 : 0) : bucket.total / max * 100
        return <div className="vertical-column" key={bucket.start} tabIndex={0} aria-label={`${formatDateBucket(bucket.start, groupBy)} bucket`} onPointerEnter={(event) => hover.activate(bucket, event)} onPointerMove={hover.move} onFocus={(event) => hover.focus(bucket, event)} onBlur={hover.hide}>
          <div className="vertical-value">{metric === 'requests' ? `${formatInteger(bucket.total)}` : display(bucket.total)}</div>
          <div className="vertical-track"><div className="vertical-stack" style={{ height: `${height}%` }}>
            {bucket.segments.filter((segment) => segment.value > 0).map((segment) => <i key={segment.key} style={{ background: segment.color, height: `${bucket.total ? segment.value / bucket.total * 100 : 0}%` }} />)}
          </div></div>
          <time dateTime={bucket.start}>{formatDateBucket(bucket.start, groupBy)}</time>
        </div>
      })}
    </div>
    {hover.tooltip && <CursorTooltip state={hover.tooltip} tooltipRef={hover.tooltipRef}>{(bucket) => <><strong>{formatDateBucket(bucket.start, groupBy)}</strong>{bucket.segments.map((segment) => <span key={segment.key}><i style={{ background: segment.color }} /><TruncatedText>{segment.label}</TruncatedText><b>{display(segment.value)}</b></span>)}<span className="tooltip-total">Total<b>{display(bucket.total)}{metric === 'requests' ? ' · 100%' : ''}</b></span></>}</CursorTooltip>}
    <div className="chart-legend">{legend.map((segment) => <span key={segment.key}><i style={{ background: segment.color }} /><TruncatedText>{segment.label}</TruncatedText></span>)}</div>
  </div>
}
