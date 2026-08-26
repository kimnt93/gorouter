import { AppShell } from './components/AppShell'
import { AnalysisPage } from './pages/AnalysisPage'
import { CachePage } from './pages/CachePage'
import { LogsPage } from './pages/LogsPage'
import './styles/app.css'

export default function App() {
  const path = window.location.pathname
  const page = path.endsWith('/logs') ? <LogsPage /> : path.endsWith('/cache') ? <CachePage /> : <AnalysisPage />
  return <AppShell>{page}</AppShell>
}
