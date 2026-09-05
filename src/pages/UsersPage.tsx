import { useCallback, useEffect, useState } from 'react'
import { createUser, getUsers, setUserStatus } from '../api/client'
import type { User } from '../api/contracts'
import { Badge, Empty, ErrorBanner, Field } from '../components/Management'
import { Modal } from '../components/Modal'
import { SearchableSelect } from '../components/SearchableSelect'
import { PageLoading } from '../components/PageState'
import { SecretModal } from '../components/SecretModal'
import { formatDateTime } from '../lib/format'

type UserRole = 'org_manager' | 'user'

export function UsersPage() {
  const [users, setUsers] = useState<User[]>([]); const [loading, setLoading] = useState(true); const [error, setError] = useState(''); const [creating, setCreating] = useState(false); const [secret, setSecret] = useState('')
  const load = useCallback(async () => { setLoading(true); try { setUsers((await getUsers()).data) } catch (reason) { setError((reason as Error).message) } finally { setLoading(false) } }, [])
  useEffect(() => { void load() }, [load])
  const toggle = async (user: User) => { try { await setUserStatus(user.id, user.status === 'active' ? 'disabled' : 'active'); await load() } catch (reason) { setError((reason as Error).message) } }
  return <><header className="page-header"><div><span className="eyebrow">Manage / Identity</span><h1>Users</h1><p>Create a unique email identity. GoRouter generates its single initial API key automatically.</p></div><button className="button" onClick={() => setCreating(true)}>Create user</button></header><ErrorBanner message={error} />{loading ? <PageLoading /> : users.length === 0 ? <Empty title="No users" detail="Create the first managed user." /> : <section className="panel table-panel"><div className="table-scroll"><table className="management-table"><thead><tr><th>Username</th><th>Role</th><th>Status</th><th>Created</th><th>Updated</th><th /></tr></thead><tbody>{users.map((user) => <tr key={user.id}><td><strong>{user.username}</strong><small>{user.id}</small></td><td><Badge>{(user.role ?? 'user').replace('_', ' ')}</Badge></td><td><Badge tone={user.status === 'active' ? 'good' : ''}>{user.status}</Badge></td><td>{formatDateTime(user.created_at)}</td><td>{formatDateTime(user.updated_at)}</td><td><button onClick={() => void toggle(user)}>{user.status === 'active' ? 'Disable' : 'Enable'}</button></td></tr>)}</tbody></table></div></section>}{creating && <CreateUserModal onClose={() => setCreating(false)} onCreated={(plaintext) => { setCreating(false); setSecret(plaintext); void load() }} />}{secret && <SecretModal secret={secret} title="User API key" onClose={() => setSecret('')} />}</>
}

function CreateUserModal({ onClose, onCreated }: { onClose: () => void; onCreated: (secret: string) => void }) {
  const [username, setUsername] = useState(''); const [role, setRole] = useState<UserRole>('user'); const [error, setError] = useState(''); const [busy, setBusy] = useState(false)
  const submit = async () => { setBusy(true); try { const response = await createUser(username, role); onCreated(response.initial_key?.plaintext ?? '') } catch (reason) { setError((reason as Error).message) } finally { setBusy(false) } }
  return <Modal title="Create user" onClose={onClose}><ErrorBanner message={error} /><div className="form-grid"><Field label="Email username"><input type="email" value={username} onChange={(event) => setUsername(event.target.value)} placeholder="person@example.com" /></Field><Field label="Role"><SearchableSelect value={role} onChange={(value) => setRole(value as UserRole)} options={[{ value: 'user', label: 'User', meta: 'Cannot create organizations' }, { value: 'org_manager', label: 'Organization manager', meta: 'Can create organizations and manage their scope' }]} /></Field></div><p className="safe-note"><strong>Automatic access</strong><span>The generated key receives management scopes and the virtual <code>auto</code> model. Copy it once after creation.</span></p><div className="dialog-actions"><button className="button" disabled={busy || !username.trim()} onClick={() => void submit()}>Create user and key</button></div></Modal>
}
