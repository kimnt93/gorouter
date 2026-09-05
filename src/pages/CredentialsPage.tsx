import { useCallback, useEffect, useState } from 'react'
import { deleteCredential, getCredentials, updateCredential } from '../api/client'
import type { Credential } from '../api/contracts'
import { Badge, Empty, ErrorBanner } from '../components/Management'
import { PageLoading } from '../components/PageState'
import { formatDateTime } from '../lib/format'

export function CredentialsPage() {
  const [items, setItems] = useState<Credential[]>([]); const [loading, setLoading] = useState(true); const [error, setError] = useState('')
  const load = useCallback(async () => { setLoading(true); try { setItems(await getCredentials()) } catch (reason) { setError((reason as Error).message) } finally { setLoading(false) } }, [])
  useEffect(() => { void load() }, [load])
  const update = async (item: Credential) => { try { await updateCredential(item.id, { name: item.name, base_url: item.base_url, status: item.status === 'active' ? 'disabled' : 'active', api_key: '', oauth_access: '', oauth_refresh: '' }); await load() } catch (reason) { setError((reason as Error).message) } }
  const remove = async (item: Credential) => { if (!window.confirm(`Delete ${item.name}?`)) return; try { await deleteCredential(item.id); await load() } catch (reason) { setError((reason as Error).message) } }
  return <><header className="page-header"><div><span className="eyebrow">Manage / Connections</span><h1>Connection inventory</h1><p>Safe connection metadata only. Add providers through the guided connection page.</p></div><a className="button" href="/dashboard/providers">Connect provider</a></header><ErrorBanner message={error} />{loading ? <PageLoading /> : items.length === 0 ? <Empty title="No provider connections" detail="Connect an API key or OAuth subscription to get started." /> : <section className="panel table-panel"><div className="table-scroll"><table className="management-table"><thead><tr><th>Name</th><th>Provider</th><th>Kind</th><th>Endpoint</th><th>Created</th><th>Status</th><th /></tr></thead><tbody>{items.map((item) => <tr key={item.id}><td><strong>{item.name}</strong><small>{item.key_preview || 'encrypted'}</small></td><td>{item.provider}</td><td>{item.kind}</td><td className="wrap-cell">{item.base_url}</td><td>{formatDateTime(item.created_at)}</td><td><Badge tone={item.status === 'active' ? 'good' : ''}>{item.status}</Badge></td><td><div className="compact-actions"><button onClick={() => void update(item)}>{item.status === 'active' ? 'Disable' : 'Enable'}</button><button className="danger-text" onClick={() => void remove(item)}>Delete</button></div></td></tr>)}</tbody></table></div></section>}</>
}
