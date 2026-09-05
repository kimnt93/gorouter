import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { completeOAuth, createCredential, deleteCredential, discoverModels, getCredentialQuota, getCredentials, getProviders, importModels, refreshCredentialQuota, getCodexResetCredits, redeemCodexResetCredit, requestStream, startOAuth, testCredential, updateCredential } from '../api/client'
import type { CodexResetCredit, Credential, OAuthStartResponse, ProviderDefinition, ProviderModel, ProviderQuotaSnapshot, Session } from '../api/contracts'
import { Badge, Empty, ErrorBanner, Field, SuccessBanner } from '../components/Management'
import { Modal } from '../components/Modal'
import { PageLoading } from '../components/PageState'
import { SearchableSelect, TruncatedText } from '../components/SearchableSelect'
import { useSession } from '../context/SessionContext'
import { createIdempotencyKey } from '../lib/idempotency'

export function ProvidersPage() {
  const { viewOrganizationID } = useSession()
  const [providers, setProviders] = useState<ProviderDefinition[]>([])
  const [credentials, setCredentials] = useState<Credential[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [connect, setConnect] = useState<ProviderDefinition | null>(null)
  const [modelsFor, setModelsFor] = useState<Credential | null>(null)
  const [chatFor, setChatFor] = useState<Credential | null>(null)
  const [chatAllFor, setChatAllFor] = useState<{ provider: ProviderDefinition; accounts: Credential[] } | null>(null)
  const load = useCallback(async () => {
    setLoading(true); setError('')
    try {
      const [providerResponse, credentialResponse] = await Promise.all([getProviders(), getCredentials()])
      setProviders(providerResponse.data); setCredentials(credentialResponse)
    } catch (reason) { setError((reason as Error).message) } finally { setLoading(false) }
  }, [])
  useEffect(() => { void load() }, [load])
  const grouped = useMemo(() => ({ oauth: providers.filter((provider) => provider.auth === 'oauth'), api: providers.filter((provider) => provider.auth === 'api_key') }), [providers])
  if (loading) return <PageLoading />
  return <>
    <header className="page-header"><div><span className="eyebrow">Manage / Providers</span><h1>Provider connections</h1><p>Connect subscriptions and API keys, check health, discover models, and run bounded streaming tests.</p></div><a className="button secondary" href={viewOrganizationID ? `/dashboard/credentials?organization_id=${encodeURIComponent(viewOrganizationID)}` : '/dashboard/credentials'}>Connection inventory</a></header>
    <ErrorBanner message={error} />
    <ProviderSection title="OAuth subscriptions" detail="Guided browser and device authorization flows." providers={grouped.oauth} credentials={credentials} onConnect={setConnect} onModels={setModelsFor} onChat={setChatFor} onChatAll={(provider, accounts) => setChatAllFor({ provider, accounts })} onRefresh={load} />
    <ProviderSection title="API-key providers" detail="Preset and custom OpenAI-compatible endpoints." providers={grouped.api} credentials={credentials} onConnect={setConnect} onModels={setModelsFor} onChat={setChatFor} onChatAll={(provider, accounts) => setChatAllFor({ provider, accounts })} onRefresh={load} />
    {connect && <ConnectProviderModal provider={connect} onClose={() => setConnect(null)} onConnected={() => { setConnect(null); void load() }} />}
    {modelsFor && <ModelsModal credential={modelsFor} canImport onClose={() => setModelsFor(null)} />}
    {chatFor && <ChatTestModal credential={chatFor} onClose={() => setChatFor(null)} />}
    {chatAllFor && <ChatAllProviderModal provider={chatAllFor.provider} accounts={chatAllFor.accounts} onClose={() => setChatAllFor(null)} />}
  </>
}

interface SectionProps { title: string; detail: string; providers: ProviderDefinition[]; credentials: Credential[]; onConnect: (provider: ProviderDefinition) => void; onModels: (credential: Credential) => void; onChat: (credential: Credential) => void; onChatAll: (provider: ProviderDefinition, accounts: Credential[]) => void; onRefresh: () => Promise<void> }
function ProviderSection({ title, detail, providers, credentials, onConnect, onModels, onChat, onChatAll, onRefresh }: SectionProps) {
  return <section className="provider-section-react"><div className="section-heading"><div><h2>{title}</h2><p>{detail}</p></div><Badge>{providers.length}</Badge></div><div className="provider-grid-react">{providers.map((provider) => {
    const accounts = credentials.filter((credential) => credential.provider === provider.id)
    return <article className={`provider-card-react ${accounts.length ? 'has-accounts' : ''}`} key={provider.id}><div className="provider-card-head"><span className="provider-monogram">{provider.name.slice(0, 2)}</span><div><h3><TruncatedText>{provider.name}</TruncatedText></h3><p title={provider.description}>{provider.description}</p></div><Badge tone={accounts.length ? 'good' : ''}>{accounts.length ? `${accounts.length} connected` : provider.auth}</Badge></div>
      {accounts.length > 0 && <><div className="provider-bulk-actions"><button className="button secondary" disabled={!accounts.some((account) => account.status === 'active')} onClick={() => onChatAll(provider, accounts.filter((account) => account.status === 'active'))}>Chat all accounts</button><small>One bounded test per active connection</small></div><div className="connection-grid">{accounts.map((credential) => <ConnectionRow credential={credential} quotaSupported={provider.quota_supported} onModels={() => onModels(credential)} onChat={() => onChat(credential)} onRefresh={onRefresh} key={credential.id} />)}</div></>}
      <button className="button connect-button" onClick={() => onConnect(provider)}>Connect {provider.name}</button>
    </article>
  })}</div></section>
}

function ConnectionRow({ credential, quotaSupported, onModels, onChat, onRefresh }: { credential: Credential; quotaSupported: boolean; onModels: () => void; onChat: () => void; onRefresh: () => Promise<void> }) {
  const [result, setResult] = useState('')
  const [busy, setBusy] = useState(false)
  const [quota, setQuota] = useState<ProviderQuotaSnapshot | null>(null)
  const [quotaBusy, setQuotaBusy] = useState(false)
  const [resetCredits, setResetCredits] = useState<CodexResetCredit[] | null>(null)
  const resetRequestIDs = useRef<Record<string, string>>({})
  useEffect(() => {
    if (!quotaSupported) return
    let active = true
    void getCredentialQuota(credential.id).then((value) => { if (active) setQuota(value) }).catch((reason: Error) => { if (active) setResult(reason.message) })
    return () => { active = false }
  }, [credential.id, quotaSupported])
  const run = async (action: () => Promise<void>) => { setBusy(true); setResult(''); try { await action() } catch (reason) { setResult((reason as Error).message) } finally { setBusy(false) } }
  const reloadQuota = async () => { setQuotaBusy(true); setResult(''); try { setQuota(await refreshCredentialQuota(credential.id)) } catch (reason) { setResult((reason as Error).message) } finally { setQuotaBusy(false) } }
  const loadResetCredits = async () => { setQuotaBusy(true); setResult(''); try { setResetCredits((await getCodexResetCredits(credential.id)).credits) } catch (reason) { setResult((reason as Error).message) } finally { setQuotaBusy(false) } }
  const redeem = async (credit: CodexResetCredit) => {
    if (!window.confirm(`Redeem this banked reset for ${credential.name}? It immediately resets eligible Codex usage windows and permanently consumes this credit.`)) return
    setQuotaBusy(true); setResult('')
    const requestID = resetRequestIDs.current[credit.selection_token] ?? (resetRequestIDs.current[credit.selection_token] = createIdempotencyKey())
    try {
      const response = await redeemCodexResetCredit(credential.id, credit.selection_token, requestID)
      delete resetRequestIDs.current[credit.selection_token]
      setQuota(response.quota); setResetCredits((await getCodexResetCredits(credential.id)).credits); setResult('Codex reset credit redeemed')
    } catch (reason) { setResult((reason as Error).message) } finally { setQuotaBusy(false) }
  }
  const displayName = maskEmail(credential.name)
  return <div className={`connection-row ${quota?.in_use ? 'in-use' : ''}`}><div className="connection-name"><i className={credential.status === 'active' ? 'connection-dot active' : 'connection-dot'} /><span><strong>{displayName}{quota?.in_use && <em className="in-use-label">In use</em>}</strong><small>{credential.key_preview || credential.kind} · {credential.base_url}</small></span></div>
    {quotaSupported && <QuotaPanel quota={quota} accountFallback={displayName} busy={quotaBusy} onReload={() => void reloadQuota()} />}
    {credential.provider === 'codex' && credential.kind === 'oauth' && <div className="reset-credit-panel"><button disabled={quotaBusy} onClick={() => void loadResetCredits()}>{quotaBusy ? 'Loading resets…' : 'Reset credits'}</button></div>}
    <div className="compact-actions">
    <button disabled={busy} onClick={() => void run(async () => { const response = await testCredential(credential.id); setResult(response.ok ? `Healthy · ${response.status ?? 'OK'} · ${response.latency_ms} ms` : 'Health check failed') })}>Test</button>
    <button onClick={onModels}>Models</button><button onClick={onChat}>Chat</button>
    <button disabled={busy} onClick={() => void run(async () => { await updateCredential(credential.id, { name: credential.name, base_url: credential.base_url, status: credential.status === 'active' ? 'disabled' : 'active', api_key: '', oauth_access: '', oauth_refresh: '' }); await onRefresh() })}>{credential.status === 'active' ? 'Disable' : 'Enable'}</button>
    <button className="danger-text" disabled={busy} onClick={() => { if (window.confirm(`Delete ${credential.name}?`)) void run(async () => { await deleteCredential(credential.id); await onRefresh() }) }}>Delete</button>
  </div>{result && <small className="inline-result">{result}</small>}{resetCredits !== null && <ResetCreditsModal credential={credential} credits={resetCredits} busy={quotaBusy} onReload={() => void loadResetCredits()} onRedeem={(credit) => void redeem(credit)} onClose={() => setResetCredits(null)} />}</div>
}

function ResetCreditsModal({ credential, credits, busy, onReload, onRedeem, onClose }: { credential: Credential; credits: CodexResetCredit[]; busy: boolean; onReload: () => void; onRedeem: (credit: CodexResetCredit) => void; onClose: () => void }) {
  return <Modal title={`${credential.name} reset credits`} onClose={onClose} className="reset-credit-modal"><div className="safe-note"><strong>Permanent action</strong><span>Redeeming immediately resets an eligible Codex usage window and permanently consumes the selected credit.</span></div><div className="reset-credit-toolbar"><span>{credits.length} available</span><button disabled={busy} onClick={onReload}>{busy ? 'Reloading…' : 'Reload credits'}</button></div>{credits.length === 0 ? <Empty title="No banked reset credits" detail="This Codex account currently has no redeemable reset credit." /> : <div className="reset-credit-list">{credits.map((credit, index) => <article key={credit.selection_token}><div><strong>{credit.title || `Reset credit ${index + 1}`}</strong><small>{credit.description || credit.reset_type || 'Codex usage reset'}</small>{credit.expires_at && <small>Expires {relativeTime(credit.expires_at)}</small>}</div><button className="button secondary" disabled={busy} onClick={() => onRedeem(credit)}>{busy ? 'Redeeming…' : 'Redeem'}</button></article>)}</div>}</Modal>
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

function ConnectProviderModal({ provider, onClose, onConnected }: { provider: ProviderDefinition; onClose: () => void; onConnected: () => void }) {
  const [name, setName] = useState(`${provider.name} account`)
  const [baseURL, setBaseURL] = useState(provider.default_base_url)
  const [apiKey, setAPIKey] = useState('')
  const [flow, setFlow] = useState<OAuthStartResponse | null>(null)
  const [callback, setCallback] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [status, setStatus] = useState('')
  const submitAPIKey = async () => { setBusy(true); setError(''); try { await createCredential({ name, provider: provider.id, kind: 'api_key', base_url: baseURL, api_key: apiKey, oauth_access: '', oauth_refresh: '' }); setAPIKey(''); onConnected() } catch (reason) { setError((reason as Error).message) } finally { setBusy(false) } }
  const beginOAuth = async () => { setBusy(true); setError(''); try { setFlow(await startOAuth(provider.id)) } catch (reason) { setError((reason as Error).message) } finally { setBusy(false) } }
  const finishOAuth = async () => { if (!flow) return; setBusy(true); setError(''); try { const response = await completeOAuth(provider.id, { flow_id: flow.flow_id, callback, name }); if (response.status === 'authorization_pending') setStatus('Authorization is still pending. Finish sign-in, then check again.'); else onConnected() } catch (reason) { setError((reason as Error).message) } finally { setBusy(false) } }
  return <Modal title={`Connect ${provider.name}`} onClose={onClose}><ErrorBanner message={error} /><SuccessBanner message={status} /><div className="form-grid"><Field label="Connection name"><input value={name} onChange={(event) => setName(event.target.value)} /></Field></div>
    {provider.auth === 'api_key' ? <><Field label="Base URL"><input type="url" value={baseURL} disabled={!provider.custom_base_url && Boolean(provider.default_base_url)} onChange={(event) => setBaseURL(event.target.value)} /></Field><Field label="API key"><input type="password" autoComplete="new-password" value={apiKey} onChange={(event) => setAPIKey(event.target.value)} /></Field><div className="dialog-actions"><button className="button" disabled={busy || !name.trim() || !apiKey.trim()} onClick={() => void submitAPIKey()}>Save encrypted connection</button></div></> : !flow ? <div className="oauth-panel"><p>{provider.description}</p><button className="button" disabled={busy || !provider.oauth_supported} onClick={() => void beginOAuth()}>Start authorization</button></div> : <div className="oauth-panel"><p>{flow.instructions}</p>{flow.user_code && <div className="device-code"><span>Device code</span><strong>{flow.user_code}</strong></div>}{(flow.verification_uri_complete || flow.verification_uri || flow.authorize_url) && <a className="button secondary" target="_blank" rel="noopener noreferrer" href={flow.verification_uri_complete || flow.verification_uri || flow.authorize_url}>Open authorization page ↗</a>}{flow.flow_type !== 'device' && <Field label="Callback URL or code"><textarea rows={4} value={callback} onChange={(event) => setCallback(event.target.value)} /></Field>}<div className="dialog-actions"><button className="button" disabled={busy} onClick={() => void finishOAuth()}>{flow.flow_type === 'device' ? 'Check authorization' : 'Complete connection'}</button></div></div>}
  </Modal>
}

function ModelsModal({ credential, canImport, onClose }: { credential: Credential; canImport: boolean; onClose: () => void }) {
  const [models, setModels] = useState<ProviderModel[]>([]); const [selected, setSelected] = useState<string[]>([]); const [query, setQuery] = useState(''); const [loading, setLoading] = useState(true); const [error, setError] = useState(''); const [message, setMessage] = useState('')
  useEffect(() => { void discoverModels(credential.id).then((response) => setModels(response.data)).catch((reason: Error) => setError(reason.message)).finally(() => setLoading(false)) }, [credential.id])
  const toggle = (id: string) => setSelected((current) => current.includes(id) ? current.filter((item) => item !== id) : [...current, id])
  const visibleModels = models.filter((model) => `${model.public_id} ${model.id} ${model.owned_by ?? ''}`.toLowerCase().includes(query.trim().toLowerCase()))
  return <Modal title={`${credential.name} models`} onClose={onClose}>{loading ? <PageLoading /> : <><ErrorBanner message={error} /><SuccessBanner message={message} /><div className="select-search model-list-search"><span>⌕</span><input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Search models" /></div><div className="model-picker-react">{visibleModels.map((model) => { const number = models.findIndex((candidate) => candidate.id === model.id) + 1; return <label key={model.id} title={model.public_id}><b>{String(number).padStart(2, '0')}</b><input type="checkbox" checked={selected.includes(model.id)} onChange={() => toggle(model.id)} /><span><strong>{model.public_id}</strong><small>{model.owned_by || model.id}{model.context_length ? ` · ${model.context_length.toLocaleString()} context` : ''}</small></span></label> })}</div><div className="select-count">{visibleModels.length} of {models.length} models</div>{models.length === 0 && <Empty title="No models returned" detail="Check the connection and provider permissions." />}{canImport && models.length > 0 && <div className="dialog-actions"><button className="button secondary" onClick={() => setSelected(models.map((model) => model.id))}>Select all</button><button className="button" disabled={!selected.length} onClick={() => void importModels(credential.id, selected).then((response) => setMessage(`Imported ${response.imported.length} model routes`)).catch((reason: Error) => setError(reason.message))}>Import selected</button></div>}</>}</Modal>
}

type BulkChatResult = { credential: Credential; model: string; status: 'pending' | 'running' | 'passed' | 'failed'; output: string; error: string }

function ChatAllProviderModal({ provider, accounts, onClose }: { provider: ProviderDefinition; accounts: Credential[]; onClose: () => void }) {
  const defaultPrompt = 'Reply with exactly: connection healthy'
  const [prompt, setPrompt] = useState(defaultPrompt)
  const [results, setResults] = useState<BulkChatResult[]>(() => accounts.map((credential) => ({ credential, model: '', status: 'pending', output: '', error: '' })))
  const [busy, setBusy] = useState(false)
  const controllers = useRef<AbortController[]>([])
  useEffect(() => () => controllers.current.forEach((controller) => controller.abort()), [])
  const update = (id: string, change: Partial<BulkChatResult>) => setResults((current) => current.map((item) => item.credential.id === id ? { ...item, ...change } : item))
  const runOne = async (credential: Credential) => {
    update(credential.id, { status: 'running', output: '', error: '' })
    try {
      const discovered = await discoverModels(credential.id)
      const model = discovered.default_model || discovered.data[0]?.id || ''
      if (!model) throw new Error('No provider model available')
      update(credential.id, { model })
      const controller = new AbortController(); controllers.current.push(controller)
      let output = ''
      await requestStream(`/admin/credentials/${encodeURIComponent(credential.id)}/chat-tests`, { model, prompt }, (text) => { output += text; update(credential.id, { output }) }, controller.signal)
      update(credential.id, { status: 'passed', output })
    } catch (reason) {
      const message = reason instanceof Error && reason.name === 'AbortError' ? 'Cancelled' : (reason as Error).message
      update(credential.id, { status: 'failed', error: message })
    }
  }
  const runAll = async () => {
    setBusy(true); controllers.current.forEach((controller) => controller.abort()); controllers.current = []
    setResults(accounts.map((credential) => ({ credential, model: '', status: 'pending', output: '', error: '' })))
    let next = 0
    const worker = async () => { while (next < accounts.length) { const credential = accounts[next++]; await runOne(credential) } }
    await Promise.all(Array.from({ length: Math.min(3, accounts.length) }, worker))
    setBusy(false)
  }
  const passed = results.filter((result) => result.status === 'passed').length
  const finished = results.filter((result) => result.status === 'passed' || result.status === 'failed').length
  return <Modal title={`Chat all · ${provider.name}`} onClose={onClose} className="chat-all-modal"><div className="safe-note"><strong>Bounded test</strong><span>Sends one streaming request with at most 128 output tokens to each active account. Up to three accounts run concurrently.</span></div><Field label="Prompt"><textarea rows={3} value={prompt} disabled={busy} onChange={(event) => setPrompt(event.target.value)} /></Field><div className="bulk-chat-toolbar"><span>{busy ? `${finished}/${accounts.length} finished` : finished ? `${passed}/${accounts.length} passed` : `${accounts.length} active accounts`}</span><button className="button" disabled={busy || !accounts.length || !prompt.trim()} onClick={() => void runAll()}>{busy ? 'Testing all…' : finished ? 'Run all again' : 'Run all accounts'}</button></div><div className="bulk-chat-results">{results.map((result, index) => <article key={result.credential.id} className={`bulk-chat-result ${result.status}`}><div><b>{String(index + 1).padStart(2, '0')}</b><span><strong>{maskEmail(result.credential.name)}</strong><small>{result.model || result.credential.id}</small></span><Badge tone={result.status === 'passed' ? 'good' : ''}>{result.status}</Badge></div>{result.error && <p className="bulk-chat-error">{result.error}</p>}{result.output && <pre>{result.output}</pre>}</article>)}</div></Modal>
}

function ChatTestModal({ credential, onClose }: { credential: Credential; onClose: () => void }) {
  const [models, setModels] = useState<ProviderModel[]>([]); const [model, setModel] = useState(''); const [prompt, setPrompt] = useState('Reply with exactly: connection healthy'); const [output, setOutput] = useState(''); const [error, setError] = useState(''); const [busy, setBusy] = useState(false)
  useEffect(() => { void discoverModels(credential.id).then((response) => { setModels(response.data); setModel(response.default_model || response.data[0]?.id || '') }).catch((reason: Error) => setError(reason.message)) }, [credential.id])
  const send = async () => { setBusy(true); setOutput(''); setError(''); try { await requestStream(`/admin/credentials/${encodeURIComponent(credential.id)}/chat-tests`, { model, prompt }, (text) => setOutput((current) => current + text)) } catch (reason) { setError((reason as Error).message) } finally { setBusy(false) } }
  return <Modal title={`Test ${credential.name}`} onClose={onClose}><ErrorBanner message={error} /><Field label="Model"><SearchableSelect value={model} onChange={setModel} searchPlaceholder="Search models" options={models.map((item) => ({ value: item.id, label: item.public_id, meta: item.id }))} /></Field><Field label="Prompt"><textarea rows={4} value={prompt} onChange={(event) => setPrompt(event.target.value)} /></Field><div className="dialog-actions"><button className="button" disabled={busy || !model || !prompt.trim()} onClick={() => void send()}>{busy ? 'Streaming…' : 'Send test'}</button></div><pre className="chat-output-react">{output || 'Response output will appear here.'}</pre></Modal>
}
