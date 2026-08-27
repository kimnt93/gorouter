import { useCallback, useEffect, useMemo, useState } from 'react'
import { completeOAuth, createCredential, deleteCredential, discoverModels, getCredentialQuota, getCredentials, getOrganizations, getProviders, importModels, refreshCredentialQuota, requestStream, startOAuth, testCredential, updateCredential } from '../api/client'
import type { Credential, OAuthStartResponse, Organization, ProviderDefinition, ProviderModel, ProviderQuotaSnapshot, Session } from '../api/contracts'
import { Badge, Empty, ErrorBanner, Field, SuccessBanner } from '../components/Management'
import { Modal } from '../components/Modal'
import { PageLoading } from '../components/PageState'
import { useSession } from '../context/SessionContext'

export function ProvidersPage() {
  const { isMaster, session } = useSession()
  const [providers, setProviders] = useState<ProviderDefinition[]>([])
  const [credentials, setCredentials] = useState<Credential[]>([])
  const [organizations, setOrganizations] = useState<Organization[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [connect, setConnect] = useState<ProviderDefinition | null>(null)
  const [modelsFor, setModelsFor] = useState<Credential | null>(null)
  const [chatFor, setChatFor] = useState<Credential | null>(null)
  const load = useCallback(async () => {
    setLoading(true); setError('')
    try {
      const [providerResponse, credentialResponse, organizationResponse] = await Promise.all([getProviders(), getCredentials(), getOrganizations()])
      setProviders(providerResponse.data); setCredentials(credentialResponse); setOrganizations(organizationResponse.data)
    } catch (reason) { setError((reason as Error).message) } finally { setLoading(false) }
  }, [])
  useEffect(() => { void load() }, [load])
  const grouped = useMemo(() => ({ oauth: providers.filter((provider) => provider.auth === 'oauth'), api: providers.filter((provider) => provider.auth === 'api_key') }), [providers])
  if (loading) return <PageLoading />
  return <>
    <header className="page-header"><div><span className="eyebrow">Manage / Providers</span><h1>Provider connections</h1><p>Connect subscriptions and API keys, check health, discover models, and run bounded streaming tests.</p></div><a className="button secondary" href="/dashboard/credentials">Connection inventory</a></header>
    <ErrorBanner message={error} />
    <ProviderSection title="OAuth subscriptions" detail="Guided browser and device authorization flows." providers={grouped.oauth} credentials={credentials} onConnect={setConnect} onModels={setModelsFor} onChat={setChatFor} onRefresh={load} />
    <ProviderSection title="API-key providers" detail="Preset and custom OpenAI-compatible endpoints." providers={grouped.api} credentials={credentials} onConnect={setConnect} onModels={setModelsFor} onChat={setChatFor} onRefresh={load} />
    {connect && <ConnectProviderModal provider={connect} session={session} organizations={organizations} onClose={() => setConnect(null)} onConnected={() => { setConnect(null); void load() }} />}
    {modelsFor && <ModelsModal credential={modelsFor} canImport={isMaster || Boolean(session?.scopes.includes('models:manage'))} onClose={() => setModelsFor(null)} />}
    {chatFor && <ChatTestModal credential={chatFor} onClose={() => setChatFor(null)} />}
  </>
}

interface SectionProps { title: string; detail: string; providers: ProviderDefinition[]; credentials: Credential[]; onConnect: (provider: ProviderDefinition) => void; onModels: (credential: Credential) => void; onChat: (credential: Credential) => void; onRefresh: () => Promise<void> }
function ProviderSection({ title, detail, providers, credentials, onConnect, onModels, onChat, onRefresh }: SectionProps) {
  return <section className="provider-section-react"><div className="section-heading"><div><h2>{title}</h2><p>{detail}</p></div><Badge>{providers.length}</Badge></div><div className="provider-grid-react">{providers.map((provider) => {
    const accounts = credentials.filter((credential) => credential.provider === provider.id)
    return <article className="provider-card-react" key={provider.id}><div className="provider-card-head"><span className="provider-monogram">{provider.name.slice(0, 2)}</span><div><h3>{provider.name}</h3><p>{provider.description}</p></div><Badge tone={accounts.length ? 'good' : ''}>{accounts.length ? `${accounts.length} connected` : provider.auth}</Badge></div>
      {accounts.map((credential) => <ConnectionRow credential={credential} quotaSupported={provider.quota_supported} onModels={() => onModels(credential)} onChat={() => onChat(credential)} onRefresh={onRefresh} key={credential.id} />)}
      <button className="button connect-button" onClick={() => onConnect(provider)}>Connect {provider.name}</button>
    </article>
  })}</div></section>
}

function ConnectionRow({ credential, quotaSupported, onModels, onChat, onRefresh }: { credential: Credential; quotaSupported: boolean; onModels: () => void; onChat: () => void; onRefresh: () => Promise<void> }) {
  const [result, setResult] = useState('')
  const [busy, setBusy] = useState(false)
  const [quota, setQuota] = useState<ProviderQuotaSnapshot | null>(null)
  const [quotaBusy, setQuotaBusy] = useState(false)
  useEffect(() => {
    if (!quotaSupported) return
    let active = true
    void getCredentialQuota(credential.id).then((value) => { if (active) setQuota(value) }).catch((reason: Error) => { if (active) setResult(reason.message) })
    return () => { active = false }
  }, [credential.id, quotaSupported])
  const run = async (action: () => Promise<void>) => { setBusy(true); setResult(''); try { await action() } catch (reason) { setResult((reason as Error).message) } finally { setBusy(false) } }
  const reloadQuota = async () => { setQuotaBusy(true); setResult(''); try { setQuota(await refreshCredentialQuota(credential.id)) } catch (reason) { setResult((reason as Error).message) } finally { setQuotaBusy(false) } }
  const displayName = maskEmail(credential.name)
  return <div className={`connection-row ${quota?.in_use ? 'in-use' : ''}`}><div className="connection-name"><i className={credential.status === 'active' ? 'connection-dot active' : 'connection-dot'} /><span><strong>{displayName}{quota?.in_use && <em className="in-use-label">In use</em>}</strong><small>{credential.key_preview || credential.kind} · {credential.base_url}</small></span></div>
    {quotaSupported && <QuotaPanel quota={quota} accountFallback={displayName} busy={quotaBusy} onReload={() => void reloadQuota()} />}
    <div className="compact-actions">
    <button disabled={busy} onClick={() => void run(async () => { const response = await testCredential(credential.id); setResult(response.ok ? `Healthy · ${response.status ?? 'OK'} · ${response.latency_ms} ms` : 'Health check failed') })}>Test</button>
    <button onClick={onModels}>Models</button><button onClick={onChat}>Chat</button>
    <button disabled={busy} onClick={() => void run(async () => { await updateCredential(credential.id, { name: credential.name, base_url: credential.base_url, status: credential.status === 'active' ? 'disabled' : 'active', api_key: '', oauth_access: '', oauth_refresh: '' }); await onRefresh() })}>{credential.status === 'active' ? 'Disable' : 'Enable'}</button>
    <button className="danger-text" disabled={busy} onClick={() => { if (window.confirm(`Delete ${credential.name}?`)) void run(async () => { await deleteCredential(credential.id); await onRefresh() }) }}>Delete</button>
  </div>{result && <small className="inline-result">{result}</small>}</div>
}

function QuotaPanel({ quota, accountFallback, busy, onReload }: { quota: ProviderQuotaSnapshot | null; accountFallback: string; busy: boolean; onReload: () => void }) {
  const account = accountFallback.includes('@') ? accountFallback : quota?.account || accountFallback || 'Connected account'
  const loaded = Boolean(quota?.fetched_at || quota?.windows.length)
  return <div className={`provider-quota ${loaded && quota && !quota.available ? 'exhausted' : ''}`}><div className="provider-quota-head"><div><strong>{account}</strong>{quota?.plan && <small>{quota.plan}</small>}</div><div className="provider-quota-actions">{quota && <span className={loaded ? quota.available ? 'available' : 'exhausted' : 'not-loaded'}>{loaded ? quota.available ? 'Available' : 'Exhausted' : 'Not loaded'}</span>}<button disabled={busy} onClick={onReload}>{busy ? 'Reloading…' : 'Reload'}</button></div></div>
    {quota?.windows.map((window) => { const remaining = Math.max(0, Math.min(100, window.remaining_percent)); const tone = remaining <= 10 ? 'critical' : remaining <= 30 ? 'warning' : 'good'; return <div className="quota-window" key={window.name}><div><span>{window.name}</span><strong>{remaining.toFixed(1)}% remaining</strong></div><div className="quota-track"><i className={tone} style={{ width: `${remaining}%` }} /></div>{window.reset_at && <small>Resets {relativeTime(window.reset_at)}</small>}</div> })}
    {quota?.message && <small className="quota-message">{quota.message}</small>}
    {quota?.fetched_at && <small className="quota-fetched">Updated {relativeTime(quota.fetched_at)}</small>}
  </div>
}

function maskEmail(value: string): string {
  const at = value.indexOf('@')
  if (at < 1) return value
  const visible = Math.min(2, at)
  return `${value.slice(0, visible)}${'*'.repeat(Math.max(4, at - visible))}${value.slice(at)}`
}

function relativeTime(value: string): string {
  const timestamp = new Date(value).getTime()
  if (!Number.isFinite(timestamp)) return value
  const seconds = Math.round((timestamp - Date.now()) / 1000)
  const absolute = Math.abs(seconds)
  const formatter = new Intl.RelativeTimeFormat(undefined, { numeric: 'auto' })
  if (absolute < 60) return formatter.format(seconds, 'second')
  if (absolute < 3600) return formatter.format(Math.round(seconds / 60), 'minute')
  if (absolute < 86_400) return formatter.format(Math.round(seconds / 3600), 'hour')
  return formatter.format(Math.round(seconds / 86_400), 'day')
}

function ConnectProviderModal({ provider, session, organizations, onClose, onConnected }: { provider: ProviderDefinition; session: Session | null; organizations: Organization[]; onClose: () => void; onConnected: () => void }) {
  const [name, setName] = useState(`${provider.name} account`)
  const [baseURL, setBaseURL] = useState(provider.default_base_url)
  const [apiKey, setAPIKey] = useState('')
  const [flow, setFlow] = useState<OAuthStartResponse | null>(null)
  const [callback, setCallback] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [status, setStatus] = useState('')
  const canChooseOrganization = session?.role === 'master' || session?.principal_type === 'organization' || session?.membership_role === 'admin'
  const defaultOwner = session?.principal_type === 'organization' ? 'organization' : 'personal'
  const [owner, setOwner] = useState(defaultOwner)
  const [ownerOrganizationID, setOwnerOrganizationID] = useState(session?.organization_id ?? '')
  const ownership = owner === 'organization' ? { owner_type: 'organization', owner_organization_id: ownerOrganizationID } : session?.role === 'master' ? {} : { owner_type: 'user' }
  const submitAPIKey = async () => { setBusy(true); setError(''); try { await createCredential({ name, provider: provider.id, kind: 'api_key', base_url: baseURL, api_key: apiKey, oauth_access: '', oauth_refresh: '', ...ownership }); setAPIKey(''); onConnected() } catch (reason) { setError((reason as Error).message) } finally { setBusy(false) } }
  const beginOAuth = async () => { setBusy(true); setError(''); try { setFlow(await startOAuth(provider.id)) } catch (reason) { setError((reason as Error).message) } finally { setBusy(false) } }
  const finishOAuth = async () => { if (!flow) return; setBusy(true); setError(''); try { const response = await completeOAuth(provider.id, { flow_id: flow.flow_id, callback, name, ...ownership }); if (response.status === 'authorization_pending') setStatus('Authorization is still pending. Finish sign-in, then check again.'); else onConnected() } catch (reason) { setError((reason as Error).message) } finally { setBusy(false) } }
  return <Modal title={`Connect ${provider.name}`} onClose={onClose}><ErrorBanner message={error} /><SuccessBanner message={status} /><div className="form-grid"><Field label="Connection name"><input value={name} onChange={(event) => setName(event.target.value)} /></Field></div>
    {canChooseOrganization && <div className="form-grid"><Field label="Connection owner"><select value={owner} onChange={(event) => setOwner(event.target.value)}><option value="personal">{session?.role === 'master' ? 'Shared globally' : 'Personal'}</option><option value="organization">Organization</option></select></Field>{owner === 'organization' && <Field label="Owner organization"><select value={ownerOrganizationID} disabled={session?.role !== 'master'} onChange={(event) => setOwnerOrganizationID(event.target.value)}><option value="">Choose organization</option>{organizations.map((organization) => <option value={organization.id} key={organization.id}>{organization.name}</option>)}</select></Field>}</div>}
    {provider.auth === 'api_key' ? <><Field label="Base URL"><input type="url" value={baseURL} disabled={!provider.custom_base_url && Boolean(provider.default_base_url)} onChange={(event) => setBaseURL(event.target.value)} /></Field><Field label="API key"><input type="password" autoComplete="new-password" value={apiKey} onChange={(event) => setAPIKey(event.target.value)} /></Field><div className="dialog-actions"><button className="button" disabled={busy || !name.trim() || !apiKey.trim() || owner === 'organization' && !ownerOrganizationID} onClick={() => void submitAPIKey()}>Save encrypted connection</button></div></> : !flow ? <div className="oauth-panel"><p>{provider.description}</p><button className="button" disabled={busy || !provider.oauth_supported || owner === 'organization' && !ownerOrganizationID} onClick={() => void beginOAuth()}>Start authorization</button></div> : <div className="oauth-panel"><p>{flow.instructions}</p>{flow.user_code && <div className="device-code"><span>Device code</span><strong>{flow.user_code}</strong></div>}{(flow.verification_uri_complete || flow.verification_uri || flow.authorize_url) && <a className="button secondary" target="_blank" rel="noopener noreferrer" href={flow.verification_uri_complete || flow.verification_uri || flow.authorize_url}>Open authorization page ↗</a>}{flow.flow_type !== 'device' && <Field label="Callback URL or code"><textarea rows={4} value={callback} onChange={(event) => setCallback(event.target.value)} /></Field>}<div className="dialog-actions"><button className="button" disabled={busy} onClick={() => void finishOAuth()}>{flow.flow_type === 'device' ? 'Check authorization' : 'Complete connection'}</button></div></div>}
  </Modal>
}

function ModelsModal({ credential, canImport, onClose }: { credential: Credential; canImport: boolean; onClose: () => void }) {
  const [models, setModels] = useState<ProviderModel[]>([]); const [selected, setSelected] = useState<string[]>([]); const [loading, setLoading] = useState(true); const [error, setError] = useState(''); const [message, setMessage] = useState('')
  useEffect(() => { void discoverModels(credential.id).then((response) => setModels(response.data)).catch((reason: Error) => setError(reason.message)).finally(() => setLoading(false)) }, [credential.id])
  const toggle = (id: string) => setSelected((current) => current.includes(id) ? current.filter((item) => item !== id) : [...current, id])
  return <Modal title={`${credential.name} models`} onClose={onClose}>{loading ? <PageLoading /> : <><ErrorBanner message={error} /><SuccessBanner message={message} /><div className="model-picker-react">{models.map((model) => <label key={model.id}><input type="checkbox" checked={selected.includes(model.id)} onChange={() => toggle(model.id)} /><span><strong>{model.public_id}</strong><small>{model.owned_by || model.id}{model.context_length ? ` · ${model.context_length.toLocaleString()} context` : ''}</small></span></label>)}</div>{models.length === 0 && <Empty title="No models returned" detail="Check the connection and provider permissions." />}{canImport && models.length > 0 && <div className="dialog-actions"><button className="button secondary" onClick={() => setSelected(models.map((model) => model.id))}>Select all</button><button className="button" disabled={!selected.length} onClick={() => void importModels(credential.id, selected).then((response) => setMessage(`Imported ${response.imported.length} model routes`)).catch((reason: Error) => setError(reason.message))}>Import selected</button></div>}</>}</Modal>
}

function ChatTestModal({ credential, onClose }: { credential: Credential; onClose: () => void }) {
  const [models, setModels] = useState<ProviderModel[]>([]); const [model, setModel] = useState(''); const [prompt, setPrompt] = useState('Reply with exactly: connection healthy'); const [output, setOutput] = useState(''); const [error, setError] = useState(''); const [busy, setBusy] = useState(false)
  useEffect(() => { void discoverModels(credential.id).then((response) => { setModels(response.data); setModel(response.default_model || response.data[0]?.id || '') }).catch((reason: Error) => setError(reason.message)) }, [credential.id])
  const send = async () => { setBusy(true); setOutput(''); setError(''); try { await requestStream(`/admin/credentials/${encodeURIComponent(credential.id)}/chat-tests`, { model, prompt }, (text) => setOutput((current) => current + text)) } catch (reason) { setError((reason as Error).message) } finally { setBusy(false) } }
  return <Modal title={`Test ${credential.name}`} onClose={onClose}><ErrorBanner message={error} /><Field label="Model"><select value={model} onChange={(event) => setModel(event.target.value)}>{models.map((item) => <option value={item.id} key={item.id}>{item.public_id}</option>)}</select></Field><Field label="Prompt"><textarea rows={4} value={prompt} onChange={(event) => setPrompt(event.target.value)} /></Field><div className="dialog-actions"><button className="button" disabled={busy || !model || !prompt.trim()} onClick={() => void send()}>{busy ? 'Streaming…' : 'Send test'}</button></div><pre className="chat-output-react">{output || 'Response output will appear here.'}</pre></Modal>
}
