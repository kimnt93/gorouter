import type { ReactNode } from 'react'

const links = [
  { href: '/dashboard/analysis', label: 'Analysis' },
  { href: '/dashboard/logs', label: 'Logs' },
  { href: '/dashboard/cache', label: 'Cache' },
]

const manageLinks = [
  { href: '/ui/users', label: 'Users' },
  { href: '/ui/organizations', label: 'Organizations' },
  { href: '/ui/keys', label: 'API keys' },
  { href: '/ui/audit', label: 'Audit' },
]

export function AppShell({ children }: { children: ReactNode }) {
  const path = window.location.pathname
  return (
    <div className="app-shell">
      <aside className="sidebar">
        <a className="brand" href="/dashboard/analysis" aria-label="gorouter dashboard">
          <span className="brand-mark">g</span>
          <span>gorouter</span>
        </a>
        <nav aria-label="Dashboard">
          <p className="nav-heading">Observe</p>
          {links.map((link) => (
            <a className={path === link.href || (path === '/' && link.href.endsWith('analysis')) ? 'nav-link active' : 'nav-link'} href={link.href} key={link.href}>
              <span className="nav-dot" />{link.label}
            </a>
          ))}
          <p className="nav-heading">Manage</p>
          {manageLinks.map((link) => <a className="nav-link" href={link.href} key={link.href}><span className="nav-dot" />{link.label}</a>)}
        </nav>
        <form action="/logout" method="post" className="logout-form"><button className="nav-link logout" type="submit">Sign out</button></form>
      </aside>
      <main className="main-content">{children}</main>
    </div>
  )
}
