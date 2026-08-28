import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'
import * as api from '../api/client'
import { useUsageFilters, usageFiltersFromURL, usageFiltersURL } from './useUsageFilters'

const now = new Date('2026-08-28T14:30:00Z')

afterEach(() => vi.restoreAllMocks())

test('initializes the default 7D range with hourly source buckets', () => {
  const filters = usageFiltersFromURL('', now)
  expect(filters.range).toBe('7d')
  expect(filters.groupBy).toBe('hour')
})

test('restores the selected range and context from the URL', () => {
  const filters = usageFiltersFromURL('?organization_id=org-1&range=1d', now)
  expect(filters).toMatchObject({ range: '1d', groupBy: 'hour', filterType: 'organization', organizationIds: ['org-1'] })
})

test('writes range changes without dropping existing dashboard context', () => {
  const filters = usageFiltersFromURL('?range=7d', now)
  expect(usageFiltersURL({ ...filters, range: '90d', groupBy: 'day' }, 'https://router.test/dashboard/cache?organization_id=org-1&view_user_id=user-1&range=7d')).toBe('/dashboard/cache?organization_id=org-1&view_user_id=user-1&range=90d')
})

test('round trips custom boundaries and removes them for presets', () => {
  const filters = { ...usageFiltersFromURL('?range=custom&since=2026-08-01T00%3A00&until=2026-08-03T00%3A00', now), range: 'custom' as const }
  const custom = usageFiltersURL(filters, 'https://router.test/dashboard/analysis?range=7d')
  expect(custom).toContain('range=custom')
  expect(custom).toContain('since=2026-08-01T00%3A00')
  expect(custom).toContain('until=2026-08-03T00%3A00')
  expect(usageFiltersURL({ ...filters, range: '1d' }, `https://router.test${custom}`)).toBe('/dashboard/analysis?range=1d')
})

test('ignores unknown range values', () => {
  expect(usageFiltersFromURL('?range=invalid', now)).toMatchObject({ range: '7d', groupBy: 'hour' })
})


test('persists a clicked range and restores it after remount', async () => {
	window.history.replaceState({}, '', '/dashboard/cache?organization_id=org-1&range=1d')
	vi.spyOn(api, 'getUsers').mockResolvedValue({ object: 'list', data: [] })
	vi.spyOn(api, 'getMembers').mockResolvedValue({ object: 'list', data: [] })
	vi.spyOn(api, 'getAPIKeys').mockResolvedValue({ object: 'list', data: [] })
	vi.spyOn(api, 'getOrganizations').mockResolvedValue({ object: 'list', data: [] })
	vi.spyOn(api, 'getModels').mockResolvedValue([])

	const first = renderHook(() => useUsageFilters())
	await waitFor(() => expect(api.getModels).toHaveBeenCalled())
	expect(first.result.current.filters.range).toBe('1d')
	act(() => first.result.current.setFilters({ ...first.result.current.filters, range: '7d' }))
	expect(window.location.search).toContain('organization_id=org-1')
	expect(window.location.search).toContain('range=7d')
	expect(first.result.current.filters.groupBy).toBe('hour')
	first.unmount()

	const restored = renderHook(() => useUsageFilters())
	expect(restored.result.current.filters).toMatchObject({ range: '7d', groupBy: 'hour' })
})
