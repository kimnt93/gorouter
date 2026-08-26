import type { GroupBy, RangePreset, UsageFilters, User, APIKey, Organization } from '../api/contracts'

const ranges: Array<{ value: RangePreset; label: string }> = [
  { value: '1d', label: '1D' }, { value: '7d', label: '7D' }, { value: '30d', label: '30D' },
  { value: '90d', label: '90D' }, { value: 'ytd', label: 'YTD' }, { value: 'all', label: 'All' }, { value: 'custom', label: 'Custom' },
]

interface Props {
  filters: UsageFilters
  onChange: (filters: UsageFilters) => void
  users: User[]
  apiKeys: APIKey[]
  organizations?: Organization[]
}

export function RangeSelector({ filters, onChange, users, apiKeys, organizations = [] }: Props) {
  const set = <K extends keyof UsageFilters>(key: K, value: UsageFilters[K]) => onChange({ ...filters, [key]: value })
  return <div className="filter-panel">
    <div className="filter-row">
      <div className="segmented" aria-label="Date range">{ranges.map((range) => <button key={range.value} className={filters.range === range.value ? 'selected' : ''} onClick={() => set('range', range.value)}>{range.label}</button>)}</div>
      <label className="select-field"><span>User</span><select value={filters.userId} onChange={(event) => set('userId', event.target.value)}><option value="">All visible users</option>{users.map((user) => <option key={user.id} value={user.id}>{user.username}</option>)}</select></label>
      <label className="select-field"><span>API key</span><select value={filters.apiKeyId} onChange={(event) => set('apiKeyId', event.target.value)}><option value="">All visible keys</option>{apiKeys.map((key) => <option key={key.id} value={key.id}>{key.name} · {key.key_prefix}</option>)}</select></label>
      {organizations.length > 0 && <label className="select-field"><span>Organization</span><select value={filters.organizationId ?? ''} onChange={(event) => set('organizationId', event.target.value)}><option value="">All visible</option>{organizations.map((organization) => <option value={organization.id} key={organization.id}>{organization.name}</option>)}</select></label>}
      <label className="select-field small"><span>Group by</span><select value={filters.groupBy} onChange={(event) => set('groupBy', event.target.value as GroupBy)}><option value="hour">Hour</option><option value="day">Day</option><option value="week">Week</option></select></label>
    </div>
    {filters.range === 'custom' && <div className="custom-range"><label><span>From</span><input type="datetime-local" value={filters.since} onChange={(event) => set('since', event.target.value)} /></label><label><span>To</span><input type="datetime-local" value={filters.until} onChange={(event) => set('until', event.target.value)} /></label></div>}
  </div>
}
