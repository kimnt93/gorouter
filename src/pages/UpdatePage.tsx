import { useState } from 'react'
import { checkForUpdates } from '../api/client'
import type { UpdateStatus } from '../api/contracts'
import { ErrorBanner } from '../components/Management'
import { useSession } from '../context/SessionContext'

export function UpdatePage() {
  const { isMaster } = useSession()
  const [status, setStatus] = useState<UpdateStatus | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const check = async () => {
    setBusy(true); setError('')
    try { setStatus(await checkForUpdates()) } catch (reason) { setError((reason as Error).message) } finally { setBusy(false) }
  }
  if (!isMaster) return <section className="panel"><h1>Updates</h1><p>Only the master administrator can check releases.</p></section>
  return <>
    <header className="page-header"><div><span className="eyebrow">System / Updates</span><h1>Updates</h1><p>Check the latest GitHub release on demand. GHCR publication may take a few minutes after a release.</p></div></header>
    <section className="panel update-panel">
      <div className="update-panel-actions"><button className="button" type="button" disabled={busy} onClick={() => void check()}>{busy ? 'Checking…' : 'Check for updates'}</button>{status && <span role="status">{status.update_available ? 'A newer release is available (check GHCR availability before pulling)' : status.installed === 'unknown' ? 'Installed version is unknown' : 'No newer release found'}</span>}</div>
      <ErrorBanner message={error} />
      {status && <>
        <dl className="detail-grid"><div><dt>Installed</dt><dd>{status.installed}</dd></div><div><dt>Latest release</dt><dd>{status.latest}</dd></div><div><dt>Image</dt><dd>{status.image}</dd></div><div><dt>Checked</dt><dd>{new Date(status.checked_at).toLocaleString()}</dd></div></dl>
        <p><a href={status.release_url} target="_blank" rel="noopener noreferrer">View release notes ↗</a></p>
        <h2>Apply the update on your Docker host</h2>
        <p>For safety, the dashboard cannot replace its running container or access the Docker socket. Keep your database and volume; pull the version shown above, then set the <code>gorouter</code> service image to the shown tag in your existing Compose file (keep its environment, volumes and ports), and recreate it:</p>
        <pre className="update-command">{`docker pull ${status.image}\n# Set services.gorouter.image to ${status.image} in your existing Compose file\ndocker compose -f YOUR_COMPOSE_FILE up -d --no-deps --no-build gorouter`}</pre>
        <p>If you use <code>docker run</code>, recreate the container with your original ports, environment, secrets and volume, changing only the image tag. Back up the database first. Do not remove volumes.</p>
      </>}
    </section>
  </>
}
