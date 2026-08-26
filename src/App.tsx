import { AppShell } from './components/AppShell'
import { AnalysisPage } from './pages/AnalysisPage'
import { CachePage } from './pages/CachePage'
import { LogsPage } from './pages/LogsPage'
import { ProvidersPage } from './pages/ProvidersPage'
import { CredentialsPage } from './pages/CredentialsPage'
import { ModelsPage } from './pages/ModelsPage'
import { UsersPage } from './pages/UsersPage'
import { OrganizationsPage } from './pages/OrganizationsPage'
import { KeysPage } from './pages/KeysPage'
import { AuditPage } from './pages/AuditPage'
import './styles/app.css'

export default function App() {
  const path = window.location.pathname
  const page = path.endsWith('/logs') ? <LogsPage />
    : path.endsWith('/cache') ? <CachePage />
      : path.endsWith('/providers') ? <ProvidersPage />
        : path.endsWith('/credentials') ? <CredentialsPage />
          : path.endsWith('/models') ? <ModelsPage />
            : path.endsWith('/users') ? <UsersPage />
              : path.endsWith('/organizations') ? <OrganizationsPage />
                : path.endsWith('/keys') ? <KeysPage />
                  : path.endsWith('/audit') ? <AuditPage />
                    : <AnalysisPage />
  return <AppShell>{page}</AppShell>
}
