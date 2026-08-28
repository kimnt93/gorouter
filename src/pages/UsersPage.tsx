import { useCallback, useEffect, useState } from 'react'
import { createUser, getAPIKeyModels, getUsers, setUserStatus } from '../api/client'
import type { APIKeyModelOption, User } from '../api/contracts'
import { Badge, Empty, ErrorBanner, Field } from '../components/Management'
import { Modal } from '../components/Modal'
import { PageLoading } from '../components/PageState'
import { SecretModal } from '../components/SecretModal'
import { formatDateTime } from '../lib/format'

export function UsersPage() {
  const [users, setUsers] = useState<User[]>([]); const [loading, setLoading] = useState(true); const [error, setError] = useState(''); const [creating, setCreating] = useState(false); const [secret, setSecret] = useState('')
  const load = useCallback(async () => { setLoading(true); try { setUsers((await getUsers()).data) } catch (reason) { setError((reason as Error).message) } finally { setLoading(false) } }, [])
  useEffect(() => { void load() }, [load])
  const toggle = async (user: User) => { try { await setUserStatus(user.id, user.status === 'active' ? 'disabled' : 'active'); await load() } catch (reason) { setError((reason as Error).message) } }
  return <><header className="page-header"><div><span className="eyebrow">Manage / Identity</span><h1>Users</h1><p>Create login identities and disable access without exposing authentication secrets.</p></div><button className="button" onClick={() => setCreating(true)}>Create user</button></header><ErrorBanner message={error} />{loading ? <PageLoading /> : users.length === 0 ? <Empty title="No users" detail="Create the first user and optionally issue an initial login key." /> : <section className="panel table-panel"><div className="table-scroll"><table className="management-table"><thead><tr><th>Username</th><th>Status</th><th>Created</th><th>Updated</th><th /></tr></thead><tbody>{users.map((user) => <tr key={user.id}><td><strong>{user.username}</strong><small>{user.id}</small></td><td><Badge tone={user.status === 'active' ? 'good' : ''}>{user.status}</Badge></td><td>{formatDateTime(user.created_at)}</td><td>{formatDateTime(user.updated_at)}</td><td><button onClick={() => void toggle(user)}>{user.status === 'active' ? 'Disable' : 'Enable'}</button></td></tr>)}</tbody></table></div></section>}{creating && <CreateUserModal onClose={() => setCreating(false)} onCreated={(plaintext) => { setCreating(false); if (plaintext) setSecret(plaintext); void load() }} />}{secret && <SecretModal secret={secret} title="Initial user API key" onClose={() => setSecret('')} />}</>
}

function CreateUserModal({ onClose, onCreated }: { onClose: () => void; onCreated: (secret: string) => void }) {
  const [username, setUsername] = useState(''); const [initial, setInitial] = useState(true); const [models, setModels] = useState<APIKeyModelOption[]>([]); const [selectedModels, setSelectedModels] = useState<string[]>([]); const [error, setError] = useState(''); const [busy, setBusy] = useState(false)
  useEffect(() => { void getAPIKeyModels().then((response) => setModels(response.data)).catch(() => setModels([])) }, [])
  const submit = async () => { setBusy(true); try { const response = await createUser(username, initial, selectedModels); onCreated(response.initial_key?.plaintext ?? '') } catch (reason) { setError((reason as Error).message) } finally { setBusy(false) } }
  return <Modal title="Create user" onClose={onClose}><ErrorBanner message={error} /><Field label="Email username"><input type="email" value={username} onChange={(event) => setUsername(event.target.value)} placeholder="person@example.com" /></Field><label className="check-row"><input type="checkbox" checked={initial} onChange={(event) => setInitial(event.target.checked)} /><span>Generate an initial chat API key</span></label>{initial && <><h3 className="form-section-title">Assigned models or blends</h3><div className="key-model-options">{models.map((model) => <label key={model.id}><input type="checkbox" checked={selectedModels.includes(model.id)} onChange={() => setSelectedModels((current) => current.includes(model.id) ? current.filter((item) => item !== model.id) : [...current, model.id])} /><span><strong>{model.id}</strong></span></label>)}</div></>}<div className="dialog-actions"><button className="button" disabled={busy || !username.trim() || initial && selectedModels.length === 0} onClick={() => void submit()}>Create user</button></div></Modal>
}
