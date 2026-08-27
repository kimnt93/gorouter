import { useEffect, useRef, useState, type ReactNode } from 'react'
import { useSession } from '../context/SessionContext'
import { getOrganizations } from '../api/client'
import type { Organization } from '../api/contracts'
import { SearchableSelect } from './SearchableSelect'

const links = [
  { href: '/dashboard/analysis', label: 'Analysis' },
  { href: '/dashboard/logs', label: 'Logs' },
  { href: '/dashboard/cache', label: 'Cache' },
]

export function AppShell({ children }: { children: ReactNode }) {
  const path = window.location.pathname
  const { session, isMaster, isMasterView, has } = useSession()
  const navRef = useRef<HTMLElement>(null)
  const [organizations, setOrganizations] = useState<Organization[]>([])
  const queryContext = new URLSearchParams(window.location.search).get('organization_id') ?? session?.organization_id ?? ''
  useEffect(() => { void getOrganizations().then((response) => setOrganizations(response.data)).catch(() => setOrganizations([])) }, [])
  useEffect(() => { navRef.current?.querySelector('.nav-link.active')?.scrollIntoView({ block: 'nearest', inline: 'center' }) }, [path, session])
  const hrefFor = (href: string) => queryContext ? `${href}?organization_id=${encodeURIComponent(queryContext)}` : href
  const changeContext = (organizationID: string) => { const url = new URL(window.location.href); if (organizationID) url.searchParams.set('organization_id', organizationID); else url.searchParams.delete('organization_id'); if (organizationID && url.pathname.endsWith('/users')) url.pathname = '/dashboard/organizations'; window.location.assign(url.toString()) }
  const adminOrganizations = isMaster ? organizations : organizations.filter((organization) => organization.membership_role === 'admin' || organization.id === session?.organization_id && session.membership_role === 'admin')
  const selectedOrganization = organizations.find((organization) => organization.id === queryContext)
  const viewingAsAdmin = Boolean(queryContext && selectedOrganization && (isMaster || selectedOrganization.membership_role === 'admin' || session?.organization_id === queryContext && session?.membership_role === 'admin'))
  const observeLinks = has('usage:read') ? links : []
  const manageLinks = [
    { href: '/dashboard/providers', label: 'Providers', show: has('credentials:manage') },
    { href: '/dashboard/credentials', label: 'Connections', show: has('credentials:manage') },
    { href: '/dashboard/models', label: 'Models & routes', show: has('models:manage') },
    { href: '/dashboard/users', label: 'Users', show: isMasterView },
    { href: '/dashboard/organizations', label: 'Organizations', show: true },
    { href: '/dashboard/keys', label: 'API keys', show: has('keys:manage') },
    { href: '/dashboard/audit', label: 'Audit', show: has('usage:read') },
  ].filter((link) => link.show)
  return (
    <div className="app-shell">
      <aside className="sidebar">
        <a className="brand" href="/dashboard/analysis" aria-label="gorouter dashboard">
          <span className="brand-mark">g</span>
          <span>gorouter</span>
        </a>
        <nav aria-label="Dashboard" ref={navRef}>
          <p className="nav-heading">Observe</p>
          {observeLinks.map((link) => (
            <a className={path === link.href || (path === '/' && link.href.endsWith('analysis')) ? 'nav-link active' : 'nav-link'} href={hrefFor(link.href)} key={link.href}>
              <span className="nav-dot" />{link.label}
            </a>
          ))}
          <p className="nav-heading">Manage</p>
          {manageLinks.map((link) => <a className={path === link.href ? 'nav-link active' : 'nav-link'} href={hrefFor(link.href)} key={link.href}><span className="nav-dot" />{link.label}</a>)}
        </nav>
        {(isMaster || adminOrganizations.length > 0) && <label className="context-switcher"><span>View as</span><SearchableSelect value={queryContext} onChange={changeContext} searchPlaceholder="Search organizations" options={[...(isMaster || !session?.organization_id ? [{ value: '', label: isMaster ? 'Master view' : 'Personal view', meta: isMaster ? 'All organizations and global data' : 'Only your personal data' }] : []), ...adminOrganizations.map((organization) => ({ value: organization.id, label: organization.name, meta: `Organization admin · ${organization.member_count ?? 0} members` }))]} /></label>}
        <div className="session-summary"><strong>{session?.username || session?.principal_type || 'session'}</strong><span>{session?.organization_id ? `org · ${session.organization_id.slice(0, 10)}` : session?.role}</span></div>
        <form action="/logout" method="post" className="logout-form"><button className="nav-link logout" type="submit">Sign out</button></form>
      </aside>
      <main className="main-content">{viewingAsAdmin && selectedOrganization && <div className="view-as-notice"><span>Viewing as organization admin: <strong title={selectedOrganization.name}>{selectedOrganization.name}</strong>. Lists and usage are limited to this organization.</span>{isMaster && <button onClick={() => changeContext('')}>Return to Master view</button>}</div>}{children}</main>
    </div>
  )
}
