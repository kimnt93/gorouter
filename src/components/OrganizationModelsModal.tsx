import { useCallback, useEffect, useState } from 'react'
import { getOrganizationModels, getOrganizationGrants, publishOrganizationModel, assignOrganizationModel, getMembers } from '../api/client'
import type { Organization, OrganizationModel, OrganizationModelGrant, Membership } from '../api/contracts'
import { Modal } from './Modal'
import { ErrorBanner, Field } from './Management'
import { SearchableSelect } from './SearchableSelect'

export function OrganizationModelsModal({ organization, onClose }: { organization: Organization; onClose: () => void }) {
  const [models, setModels] = useState<OrganizationModel[]>([])
  const [grants, setGrants] = useState<OrganizationModelGrant[]>([])
  const [members, setMembers] = useState<Membership[]>([])
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [loading, setLoading] = useState(true)
  const [name, setName] = useState('')
  const [kind, setKind] = useState('alias')
  const [targets, setTargets] = useState('')
  const [limit, setLimit] = useState('')
  const [model, setModel] = useState('')
  const [user, setUser] = useState('')
  const [userLimit, setUserLimit] = useState('')
  const load = useCallback(async () => {
    const [m, g, users] = await Promise.all([getOrganizationModels(organization.id), getOrganizationGrants(organization.id), getMembers(organization.id)])
    setModels(m); setGrants(g); setMembers(users.data)
  }, [organization.id])
  useEffect(() => { void load().catch((e: Error) => setError(e.message)).finally(() => setLoading(false)) }, [load])
  const run = async (action: () => Promise<unknown>) => { setBusy(true); setError(''); try { await action(); await load() } catch (e) { setError((e as Error).message) } finally { setBusy(false) } }
  const invalidLimit = (v: string) => v !== '' && (!Number.isFinite(Number(v)) || Number(v) < 0)
  return <Modal title={`${organization.name} models and limits`} onClose={onClose}>
    <p>Aliases rename existing models. Groups try members in order. Limits are weekly USD: blank means unlimited, zero blocks new calls. Members use their existing user key.</p>
    <ErrorBanner message={error} />
    {loading ? <p role="status">Loading organization models…</p> : <>
      <h3>Publish an alias or group</h3>
      <div className="form-grid">
        <Field label="Kind"><SearchableSelect value={kind} onChange={setKind} options={[{ value: 'alias', label: 'Alias' }, { value: 'group', label: 'Group (/g/)' }]} /></Field>
        <Field label="Name (without organization prefix)"><input value={name} onChange={e => setName(e.target.value)} /></Field>
        <Field label="Weekly USD limit"><input type="number" min="0" step="any" value={limit} onChange={e => setLimit(e.target.value)} placeholder="Unlimited" /></Field>
      </div>
      <Field label="Target model IDs (one per line, in fallback order)"><textarea value={targets} rows={3} onChange={e => setTargets(e.target.value)} /></Field>
      <button className="button" disabled={busy || !name.trim() || !targets.trim() || invalidLimit(limit)} onClick={() => void run(() => publishOrganizationModel(organization.id, { name, kind: kind as 'alias' | 'group', targets: targets.split('\n').map(x => x.trim()).filter(Boolean), enabled: true, weekly_limit_usd: limit === '' ? null : Number(limit) }))}>{busy ? 'Saving…' : 'Save model'}</button>
      {models.map(item => <div className="safe-note" key={item.name}><strong>{item.name}</strong><span>{item.targets.join(' → ')} · {item.weekly_limit_usd === null ? 'Unlimited' : `$${item.weekly_limit_usd}/week`}</span><div className="compact-actions"><button disabled={busy} onClick={() => { setName(item.name.split('/').at(-1) || ''); setKind(item.kind); setTargets(item.targets.join('\n')); setLimit(item.weekly_limit_usd === null ? '' : String(item.weekly_limit_usd)) }}>Edit</button><button disabled={busy} onClick={() => void run(() => publishOrganizationModel(organization.id, { ...item, name: item.name.split('/').at(-1) || '', enabled: !item.enabled }))}>{item.enabled ? 'Disable' : 'Enable'}</button></div></div>)}
      <h3>Assign to a member</h3>
      <div className="form-grid">
        <Field label="Model or group"><SearchableSelect value={model} onChange={setModel} options={models.map(m => ({ value: m.name, label: m.name }))} /></Field>
        <Field label="User"><SearchableSelect value={user} onChange={setUser} options={members.map(m => ({ value: m.user_id, label: m.username || m.user_id }))} /></Field>
        <Field label="User weekly USD limit"><input type="number" min="0" step="any" value={userLimit} onChange={e => setUserLimit(e.target.value)} placeholder="Unlimited" /></Field>
      </div>
      <button className="button" disabled={busy || !model || !user || invalidLimit(userLimit)} onClick={() => void run(() => assignOrganizationModel(organization.id, { model, user_id: user, enabled: true, weekly_limit_usd: userLimit === '' ? null : Number(userLimit) }))}>Save assignment</button>
      {grants.map(g => <div className="safe-note" key={`${g.model}:${g.user_id}`}><strong>{g.model}</strong><span>{g.user_id} · {g.weekly_limit_usd === null ? 'Unlimited' : `$${g.weekly_limit_usd}/week`}</span><div className="compact-actions"><button disabled={busy} onClick={() => { setModel(g.model); setUser(g.user_id); setUserLimit(g.weekly_limit_usd === null ? '' : String(g.weekly_limit_usd)) }}>Edit limit</button><button disabled={busy} onClick={() => void run(() => assignOrganizationModel(organization.id, { ...g, enabled: !g.enabled }))}>{g.enabled ? 'Revoke' : 'Enable'}</button></div></div>)}
    </>}
  </Modal>
}
