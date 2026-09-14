import { useEffect, useState } from 'react'
import { getPersonalAliases, publishPersonalAlias, assignPersonalAlias, setAssignedModelLimit } from '../api/client'
import type { OrganizationModel } from '../api/contracts'
import { Modal } from './Modal'
import { ErrorBanner, Field } from './Management'

export function PersonalModelAliasesModal({ onClose }: { onClose: () => void }) {
  const [aliases, setAliases] = useState<OrganizationModel[]>([])
  const [name, setName] = useState('')
  const [source, setSource] = useState('')
  const [model, setModel] = useState('')
  const [user, setUser] = useState('')
  const [limit, setLimit] = useState('0')
  const [selfModel, setSelfModel] = useState('')
  const [selfLimit, setSelfLimit] = useState('0')
  const [error, setError] = useState('')
  const [message, setMessage] = useState('')
  const [busy, setBusy] = useState(false)
  const invalid = (value: string) => !Number.isFinite(Number(value)) || Number(value) < 0
  const load = () => getPersonalAliases().then(setAliases)
  useEffect(() => { void load().catch((e: Error) => setError(e.message)) }, [])
  const run = async (action: () => Promise<unknown>) => { setBusy(true); setError(''); setMessage(''); try { await action(); await load(); setMessage('Saved') } catch (e) { setError((e as Error).message) } finally { setBusy(false) } }
  return <Modal title="Personal aliases and model limits" onClose={onClose}>
    <p>Each source has one unique alias under your username. The original ID still works, but the alias replaces it in model listings. Zero means unlimited; your personal cap cannot remove an assigner's limit.</p>
    <ErrorBanner message={error} />{message && <p role="status">{message}</p>}
    <h3>Rename a personal model</h3>
    <div className="form-grid"><Field label="Source model ID"><input value={source} onChange={e => setSource(e.target.value)} placeholder="cx/gpt-5.5" /></Field><Field label="Alias name"><input value={name} onChange={e => setName(e.target.value)} placeholder="gpt-5.5" /></Field></div>
    <button className="button" disabled={busy || !name.trim() || !source.trim()} onClick={() => void run(() => publishPersonalAlias({ name, kind: 'alias', targets: [source], enabled: true, weekly_limit_usd: 0 }))}>Save alias</button>
    {aliases.map(a => <div className="safe-note" key={a.name}><strong>{a.name}</strong><span>{a.targets[0]} · {a.enabled ? 'Enabled' : 'Disabled'}</span><button disabled={busy} onClick={() => void run(() => publishPersonalAlias({ ...a, name: a.name.split('/').at(-1) || '', enabled: !a.enabled, weekly_limit_usd: 0 }))}>{a.enabled ? 'Disable' : 'Enable'}</button></div>)}
    <h3>Share your alias with another user</h3>
    <div className="form-grid"><Field label="Your full alias ID"><input value={model} onChange={e => setModel(e.target.value)} /></Field><Field label="Recipient user ID"><input value={user} onChange={e => setUser(e.target.value)} /></Field><Field label="Recipient weekly USD limit"><input type="number" min="0" step="any" value={limit} onChange={e => setLimit(e.target.value)} /></Field></div>
    <div className="compact-actions"><button disabled={busy || !model || !user || invalid(limit)} onClick={() => void run(() => assignPersonalAlias({ model, user_id: user, enabled: true, weekly_limit_usd: Number(limit) }))}>Assign model</button><button disabled={busy || !model || !user} onClick={() => void run(() => assignPersonalAlias({ model, user_id: user, enabled: false, weekly_limit_usd: 0 }))}>Revoke model</button></div>
    <h3>Set an additional cap on a model assigned to you</h3>
    <div className="form-grid"><Field label="Assigned model ID"><input value={selfModel} onChange={e => setSelfModel(e.target.value)} placeholder="org/xno/gpt-5.6-luna" /></Field><Field label="My weekly USD limit"><input type="number" min="0" step="any" value={selfLimit} onChange={e => setSelfLimit(e.target.value)} /></Field></div>
    <button className="button" disabled={busy || !selfModel || invalid(selfLimit)} onClick={() => void run(() => setAssignedModelLimit({ model: selfModel, weekly_limit_usd: Number(selfLimit) }))}>Save my cap</button>
  </Modal>
}
