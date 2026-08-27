import type { GroupBy, RangePreset, UsageFilters, User, APIKey, Organization } from '../api/contracts'
import { useState } from 'react'
import { SearchableSelect, TruncatedText } from './SearchableSelect'

const ranges: Array<{ value: RangePreset; label: string }> = [
  { value: '1d', label: '1D' }, { value: '7d', label: '7D' }, { value: '30d', label: '30D' },
  { value: '90d', label: '90D' }, { value: 'ytd', label: 'YTD' }, { value: 'all', label: 'All' }, { value: 'custom', label: 'Custom' },
]

interface Props { filters: UsageFilters; onChange: (filters: UsageFilters) => void; users: User[]; apiKeys: APIKey[]; organizations?: Organization[] }

export function RangeSelector({ filters, onChange, users, apiKeys, organizations = [] }: Props) {
  const [query, setQuery] = useState('')
  const set = <K extends keyof UsageFilters>(key: K, value: UsageFilters[K]) => onChange({ ...filters, [key]: value })
  const options = filters.filterType === 'user'
    ? users.map((user) => ({ id: user.id, label: user.username }))
    : filters.filterType === 'api_key'
      ? apiKeys.map((key) => ({ id: key.id, label: `${key.name} · ${key.key_prefix}` }))
      : organizations.map((organization) => ({ id: organization.id, label: organization.name }))
  const selected = filters.filterType === 'user' ? filters.userIds : filters.filterType === 'api_key' ? filters.apiKeyIds : filters.organizationIds
  const selectedKey: 'userIds' | 'apiKeyIds' | 'organizationIds' = filters.filterType === 'user' ? 'userIds' : filters.filterType === 'api_key' ? 'apiKeyIds' : 'organizationIds'
  const toggle = (id: string) => set(selectedKey, selected.includes(id) ? selected.filter((value) => value !== id) : [...selected, id])
  const setType = (value: UsageFilters['filterType']) => onChange({ ...filters, filterType: value, userIds: [], apiKeyIds: [], organizationIds: [] })
  const filteredOptions = options.filter((option) => option.label.toLowerCase().includes(query.trim().toLowerCase()))
  return <div className="filter-panel usage-filter-panel">
    <div className="filter-block"><span className="filter-label">Range</span><div className="segmented" aria-label="Date range">{ranges.map((range) => <button key={range.value} className={filters.range === range.value ? 'selected' : ''} onClick={() => set('range', range.value)}>{range.label}</button>)}</div></div>
    <div className="identity-filter-row"><label className="select-field filter-type"><span>Filter by</span><SearchableSelect value={filters.filterType} onChange={(value) => setType(value as UsageFilters['filterType'])} options={[{ value: 'user', label: 'User' }, { value: 'api_key', label: 'API key' }, { value: 'organization', label: 'Organization' }]} /></label><details className="multi-picker"><summary>{selected.length ? `${selected.length} selected` : `All ${filters.filterType.replace('_', ' ')}s`}</summary><div className="multi-picker-menu"><div className="select-search"><span>⌕</span><input value={query} onChange={(event) => setQuery(event.target.value)} placeholder={`Search ${filters.filterType.replace('_', ' ')}s`} /></div>{filteredOptions.length === 0 ? <small>No visible values</small> : filteredOptions.map((option) => <label key={option.id} title={option.label}><b>{String(options.findIndex((candidate) => candidate.id === option.id) + 1).padStart(2, '0')}</b><input aria-label={option.label} type="checkbox" checked={selected.includes(option.id)} onChange={() => toggle(option.id)} /><TruncatedText>{option.label}</TruncatedText></label>)}<div className="select-count">{filteredOptions.length} of {options.length} options</div></div></details><div className="filter-chips">{selected.map((id) => { const label = options.find((option) => option.id === id)?.label ?? id; return <button key={id} title={label} onClick={() => toggle(id)}><TruncatedText>{label}</TruncatedText><span>×</span></button> })}</div></div>
    <div className="filter-block resolution-block"><span className="filter-label">Resolution</span><div className="segmented resolution" aria-label="Resolution">{(['hour', 'day', 'week'] as GroupBy[]).map((group) => <button key={group} className={filters.groupBy === group ? 'selected' : ''} onClick={() => set('groupBy', group)}>{group[0].toUpperCase() + group.slice(1)}</button>)}</div></div>
    {filters.range === 'custom' && <div className="custom-range"><label><span>From</span><input type="datetime-local" value={filters.since} onChange={(event) => set('since', event.target.value)} /></label><label><span>To</span><input type="datetime-local" value={filters.until} onChange={(event) => set('until', event.target.value)} /></label></div>}
  </div>
}
