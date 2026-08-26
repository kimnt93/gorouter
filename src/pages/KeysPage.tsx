import { useCallback, useEffect, useMemo, useState } from 'react'
import { createAPIKey, deleteAPIKey, getAPIKeyModels, getAPIKeys, getMembers, getOrganizations, getUsers, patchAPIKey, rotateAPIKey } from '../api/client'
import type { APIKey, APIKeyModelOption, Membership, Organization, Session, User } from '../api/contracts'
import { Badge, Empty, ErrorBanner, Field } from '../components/Management'
import { Modal } from '../components/Modal'
import { PageLoading } from '../components/PageState'
import { SecretModal } from '../components/SecretModal'
import { useSession } from '../context/SessionContext'
import { formatDateTime } from '../lib/format'
import { priceSummary } from '../lib/pricing'

export function KeysPage() {
  const [keys, setKeys] = useState<APIKey[]>([])
  const [organizations, setOrganizations] = useState<Organization[]>([])
  const [users, setUsers] = useState<User[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [creating, setCreating] = useState(false)
  const [editing, setEditing] = useState<APIKey | null>(null)
  const [secret, setSecret] = useState('')
  const { session } = useSession()
  const load = useCallback(async () => {
    setLoading(true); setError('')
    try {
      const [keyResponse, organizationResponse, userResponse] = await Promise.all([
        getAPIKeys(), getOrganizations().catch(() => ({ object: 'list' as const, data: [] })), getUsers().catch(() => ({ object: 'list' as const, data: [] })),
      ])
      setKeys(keyResponse.data); setOrganizations(organizationResponse.data); setUsers(userResponse.data)
    } catch (reason) { setError((reason as Error).message) } finally { setLoading(false) }
  }, [])
  useEffect(() => { void load() }, [load])
  const userNames = useMemo(() => new Map(users.map((user) => [user.id, user.username])), [users])
  const organizationNames = useMemo(() => new Map(organizations.map((organization) => [organization.id, organization.name])), [organizations])
  const toggle = async (key: APIKey) => { try { await patchAPIKey(key.id, { enabled: !key.enabled }); await load() } catch (reason) { setError((reason as Error).message) } }
  const rotate = async (key: APIKey) => { if (!window.confirm(`Rotate ${key.name}? The old secret will stop working immediately.`)) return; try { const response = await rotateAPIKey(key.id); setSecret(response.plaintext); await load() } catch (reason) { setError((reason as Error).message) } }
  const remove = async (key: APIKey) => { if (!window.confirm(`Delete ${key.name}?`)) return; try { await deleteAPIKey(key.id); await load() } catch (reason) { setError((reason as Error).message) } }
  return <>
    <header className="page-header"><div><span className="eyebrow">Manage / Access</span><h1>API keys</h1><p>Create chat-only keys for a member of an organization, with explicit model and spending limits.</p></div><button className="button" onClick={() => setCreating(true)}>Create API key</button></header>
    <ErrorBanner message={error} />
    {loading ? <PageLoading /> : keys.length === 0 ? <Empty title="No visible API keys" detail="Create a key for an organization member." /> : <section className="panel table-panel"><div className="table-scroll"><table className="management-table"><thead><tr><th>Name</th><th>User</th><th>Organization</th><th>Allowed models</th><th>Limits</th><th>Status</th><th /></tr></thead><tbody>{keys.map((key) => <tr key={key.id}><td><strong>{key.name}</strong><small>{key.key_prefix} · {formatDateTime(key.created_at)}</small></td><td>{userNames.get(key.owner_user_id ?? '') ?? key.owner_user_id ?? '—'}</td><td>{organizationNames.get(key.context_organization_id ?? '') ?? key.context_organization_id ?? '—'}</td><td className="wrap-cell">{key.models.join(', ') || 'none'}</td><td>{key.quota_usd == null ? 'No spending limit' : `$${key.quota_usd}/${key.quota_period}`}<small>{key.rpm ? `${key.rpm} RPM` : 'No RPM limit'}</small></td><td><Badge tone={key.enabled ? 'good' : ''}>{key.enabled ? 'enabled' : 'disabled'}</Badge></td><td><div className="compact-actions"><button onClick={() => setEditing(key)}>Edit</button><button onClick={() => void toggle(key)}>{key.enabled ? 'Disable' : 'Enable'}</button><button onClick={() => void rotate(key)}>Rotate</button><button className="danger-text" onClick={() => void remove(key)}>Delete</button></div></td></tr>)}</tbody></table></div></section>}
    {(creating || editing) && <KeyModal existing={editing} organizations={organizations} users={users} session={session} onClose={() => { setCreating(false); setEditing(null) }} onSaved={(plaintext) => { setCreating(false); setEditing(null); if (plaintext) setSecret(plaintext); void load() }} />}
    {secret && <SecretModal secret={secret} title="One-time API key" onClose={() => setSecret('')} />}
  </>
}

function KeyModal({ existing, organizations, users, session, onClose, onSaved }: { existing: APIKey | null; organizations: Organization[]; users: User[]; session: Session | null; onClose: () => void; onSaved: (secret: string) => void }) {
  const [name, setName] = useState(existing?.name ?? '')
  const [organizationID, setOrganizationID] = useState(existing?.context_organization_id || session?.organization_id || organizations[0]?.id || '')
  const [ownerUserID, setOwnerUserID] = useState(existing?.owner_user_id ?? '')
  const [memberships, setMemberships] = useState<Membership[]>([])
  const [models, setModels] = useState<APIKeyModelOption[]>([])
  const [selectedModels, setSelectedModels] = useState(existing?.models ?? [])
  const [quota, setQuota] = useState(existing?.quota_usd?.toString() ?? '')
  const [quotaPeriod, setQuotaPeriod] = useState(existing?.quota_period === 'week' ? 'week' : 'none')
  const [rpm, setRPM] = useState(existing?.rpm?.toString() ?? '')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [loadingOptions, setLoadingOptions] = useState(true)
  useEffect(() => {
    if (!organizationID) { setMemberships([]); setModels([]); setLoadingOptions(false); return }
    setLoadingOptions(true); setError('')
    const canListMembers = session?.role === 'master' || session?.principal_type === 'organization' || session?.membership_role === 'admin'
    const memberRequest = canListMembers ? getMembers(organizationID) : Promise.resolve({ object: 'list' as const, data: session?.user_id ? [{ organization_id: organizationID, user_id: session.user_id, role: 'member' as const, created_at: '' }] : [] })
    void Promise.all([memberRequest, getAPIKeyModels(organizationID)]).then(([memberResponse, modelResponse]) => {
      setMemberships(memberResponse.data); setModels(modelResponse.data)
      if (!existing) setOwnerUserID((current) => memberResponse.data.some((membership) => membership.user_id === current) ? current : memberResponse.data[0]?.user_id ?? '')
      setSelectedModels((current) => current.filter((model) => modelResponse.data.some((option) => option.id === model)))
    }).catch((reason) => setError((reason as Error).message)).finally(() => setLoadingOptions(false))
  }, [organizationID, existing, session])
  const userNames = useMemo(() => new Map([...users.map((user) => [user.id, user.username] as const), ...(session?.user_id && session.username ? [[session.user_id, session.username] as const] : [])]), [users, session])
  const toggleModel = (model: string) => setSelectedModels((current) => current.includes(model) ? current.filter((item) => item !== model) : [...current, model])
  const submit = async () => {
    setBusy(true); setError('')
    try {
      const quotaValue = quotaPeriod === 'none' || !quota ? null : Number(quota)
      const rpmValue = rpm ? Number(rpm) : null
      if (existing) {
        await patchAPIKey(existing.id, { models: selectedModels, scopes: ['chat'], quota_usd: quotaValue, quota_period: quotaPeriod, rpm: rpmValue }); onSaved('')
      } else {
        const response = await createAPIKey({ name, models: selectedModels, scopes: ['chat'], quota_usd: quotaValue, quota_period: quotaPeriod, rpm: rpmValue, owner_type: 'user', owner_user_id: ownerUserID, context_organization_id: organizationID })
        onSaved(response.plaintext)
      }
    } catch (reason) { setError((reason as Error).message) } finally { setBusy(false) }
  }
  return <Modal title={existing ? `Edit ${existing.name}` : 'Create API key'} onClose={onClose} className="key-modal">
    <ErrorBanner message={error} />
    <div className="safe-note"><strong>Chat access only</strong><span>The key can list its allowed models and prices, and send chat requests. Dashboard and management scopes are not granted.</span></div>
    <div className="form-grid">
      <Field label="Name"><input value={name} disabled={Boolean(existing)} placeholder="Development key" onChange={(event) => setName(event.target.value)} /></Field>
      <Field label="Organization"><select value={organizationID} disabled={Boolean(existing)} onChange={(event) => setOrganizationID(event.target.value)}><option value="">Choose organization</option>{organizations.map((organization) => <option value={organization.id} key={organization.id}>{organization.name}</option>)}</select></Field>
      <Field label="User in organization"><select value={ownerUserID} disabled={Boolean(existing) || loadingOptions} onChange={(event) => setOwnerUserID(event.target.value)}><option value="">Choose member</option>{memberships.map((membership) => <option value={membership.user_id} key={membership.user_id}>{userNames.get(membership.user_id) ?? membership.user_id} · {membership.role}</option>)}</select></Field>
      <Field label="Requests/minute"><input type="number" min="1" placeholder="No limit" value={rpm} onChange={(event) => setRPM(event.target.value)} /></Field>
      <Field label="Spending period"><select value={quotaPeriod} onChange={(event) => setQuotaPeriod(event.target.value)}><option value="none">No spending limit</option><option value="week">Weekly</option></select></Field>
      <Field label="Spending limit (USD)"><input type="number" min="0" step="0.0001" value={quota} disabled={quotaPeriod === 'none'} onChange={(event) => setQuota(event.target.value)} /></Field>
    </div>
    <div className="form-section-heading"><h3 className="form-section-title">Allowed models</h3><span className="model-selection-count">{selectedModels.length} selected</span></div>
    {loadingOptions ? <div className="model-options-state">Loading accessible models…</div> : models.length === 0 ? <div className="model-options-state">No callable models are available to this organization.</div> : <div className="key-model-options">{models.map((model) => <label key={model.id}><input type="checkbox" checked={selectedModels.includes(model.id)} onChange={() => toggleModel(model.id)} /><span><strong>{model.id}</strong><small>{model.free ? 'Free' : priceSummary(model.price)}</small></span></label>)}</div>}
    <div className="dialog-actions"><button className="button" disabled={busy || loadingOptions || (!existing && (!name.trim() || !organizationID || !ownerUserID)) || selectedModels.length === 0} onClick={() => void submit()}>{existing ? 'Save key limits' : 'Create key'}</button></div>
  </Modal>
}
