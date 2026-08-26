import { useCallback, useEffect, useMemo, useState } from 'react'
import { deleteModel, deletePrice, discoverModels, getCredentials, getModels, getPricingCatalog, saveModel, savePrice } from '../api/client'
import type { CatalogPrice, Credential, ModelDefinition, ModelRoute, Price, ProviderModel } from '../api/contracts'
import { Badge, Empty, ErrorBanner, Field } from '../components/Management'
import { Modal } from '../components/Modal'
import { PageLoading } from '../components/PageState'
import { useSession } from '../context/SessionContext'

const emptyPrice: Price = { input_per_m: 0, output_per_m: 0, cached_input_per_m: 0, cache_write_per_m: 0 }
type Tab = 'catalog' | 'blends'
interface ConnectedModel { credential: Credential; model: ProviderModel; price?: CatalogPrice }

function priceCandidates(publicID: string, upstream: string): string[] {
  const value = (upstream || publicID).toLowerCase()
  const base = value.includes('/') ? value.slice(value.indexOf('/') + 1) : value
  const prefix = base.startsWith('gpt-') || /^o[134]/.test(base) ? 'openai' : base.startsWith('deepseek-') ? 'deepseek' : base.startsWith('claude-') ? 'anthropic' : base.startsWith('gemini-') ? 'google' : base.startsWith('grok-') ? 'x-ai' : base.startsWith('qwen') ? 'qwen' : ''
  return [publicID.toLowerCase(), value, base, prefix ? `${prefix}/${base}` : '']
}

function findCatalogPrice(catalog: CatalogPrice[], publicID: string, upstream: string): CatalogPrice | undefined {
  const candidates = new Set(priceCandidates(publicID, upstream))
  return catalog.find((item) => candidates.has(item.model.toLowerCase()))
}

function PriceStrip({ price, source }: { price?: Price; source: string }) {
  if (!price) return <div className="price-strip unpriced"><span>Unpriced</span><small>No matching original-model price</small></div>
  return <div className="price-strip"><span>In ${price.input_per_m.toFixed(3)}</span><span>Out ${price.output_per_m.toFixed(3)}</span><span>Read ${price.cached_input_per_m.toFixed(3)}</span><span>Write ${price.cache_write_per_m.toFixed(3)}</span><small>{source}</small></div>
}

export function ModelsPage() {
  const { isMaster } = useSession()
  const [tab, setTab] = useState<Tab>('catalog')
  const [models, setModels] = useState<ModelDefinition[]>([])
  const [credentials, setCredentials] = useState<Credential[]>([])
  const [catalog, setCatalog] = useState<CatalogPrice[]>([])
  const [connected, setConnected] = useState<ConnectedModel[]>([])
  const [discoveryErrors, setDiscoveryErrors] = useState<string[]>([])
  const [search, setSearch] = useState('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [editing, setEditing] = useState<ModelDefinition | null>(null)
  const [draft, setDraft] = useState<ModelDefinition | null>(null)

  const load = useCallback(async () => {
    setLoading(true); setError('')
    try {
      const [modelData, credentialData, catalogData] = await Promise.all([getModels(), getCredentials(), getPricingCatalog()])
      setModels(modelData); setCredentials(credentialData); setCatalog(catalogData)
      const active = credentialData.filter((credential) => credential.status === 'active')
      const discoveries = await Promise.allSettled(active.map(async (credential) => ({ credential, response: await discoverModels(credential.id) })))
      const available: ConnectedModel[] = []
      const failures: string[] = []
      discoveries.forEach((result, index) => {
        if (result.status === 'rejected') failures.push(`${active[index].name}: ${(result.reason as Error).message}`)
        else result.value.response.data.forEach((model) => available.push({ credential: result.value.credential, model, price: findCatalogPrice(catalogData, model.public_id, model.id) }))
      })
      available.sort((a, b) => a.model.public_id.localeCompare(b.model.public_id) || a.credential.name.localeCompare(b.credential.name))
      setConnected(available); setDiscoveryErrors(failures)
    } catch (reason) { setError((reason as Error).message) } finally { setLoading(false) }
  }, [])
  useEffect(() => { void load() }, [load])

  const visibleConnected = useMemo(() => {
    const query = search.trim().toLowerCase()
    return query ? connected.filter((item) => `${item.model.public_id} ${item.model.name ?? ''} ${item.credential.name} ${item.credential.provider}`.toLowerCase().includes(query)) : connected
  }, [connected, search])
  const closeModal = () => { setEditing(null); setDraft(null) }
  const remove = async (name: string) => { if (!window.confirm(`Delete model blend ${name}?`)) return; try { await deleteModel(name); await load() } catch (reason) { setError((reason as Error).message) } }
  const addConnected = (item: ConnectedModel) => setDraft({ name: item.model.public_id, upstream_model: item.model.id, strategy: 'priority', enabled: true, routes: [{ credential_id: item.credential.id, priority: 0, weight: 1, enabled: true }] })

  return <>
    <header className="page-header"><div><span className="eyebrow">Manage / Models</span><h1>Models</h1><p>Browse every model exposed by connected providers, then create stable public model blends with stacked routes.</p></div>{isMaster && <button className="button" onClick={() => setDraft({ name: '', upstream_model: '', strategy: 'priority', enabled: true, routes: [] })}>Create blend</button>}</header>
    <ErrorBanner message={error} />
    <div className="page-tabs" role="tablist"><button className={tab === 'catalog' ? 'active' : ''} onClick={() => setTab('catalog')}>Available models <span>{connected.length}</span></button><button className={tab === 'blends' ? 'active' : ''} onClick={() => setTab('blends')}>Model blends <span>{models.length}</span></button></div>
    {loading ? <PageLoading /> : tab === 'catalog' ? <>
      <div className="catalog-toolbar"><input aria-label="Search models" placeholder="Search model or provider" value={search} onChange={(event) => setSearch(event.target.value)} /><span>{visibleConnected.length} connected models</span></div>
      {discoveryErrors.length > 0 && <div className="banner error-banner">Some provider catalogs could not load: {discoveryErrors.join(' · ')}</div>}
      {visibleConnected.length === 0 ? <Empty title="No connected models" detail="Connect a provider with model discovery, or create a blend manually." /> : <div className="available-model-grid">{visibleConnected.map((item) => <article className="available-model-card" key={`${item.credential.id}-${item.model.id}`}><div className="model-card-title"><div><strong>{item.model.public_id}</strong><small>{item.model.name || item.model.id}</small></div><Badge>{item.credential.provider}</Badge></div><p>{item.credential.name}{item.model.context_length ? ` · ${item.model.context_length.toLocaleString()} context` : ''}</p><PriceStrip price={item.price?.price} source={item.price ? `catalog · ${item.price.model}` : 'unpriced'} />{isMaster && <button className="button secondary connect-button" onClick={() => addConnected(item)}>Add to blends</button>}</article>)}</div>}
    </> : models.length === 0 ? <Empty title="No model blends configured" detail="Add a connected model or define a blend manually." /> : <div className="model-card-grid">{models.map((model) => { const inherited = findCatalogPrice(catalog, model.name, model.upstream_model); return <article className="model-card" key={model.name}><div className="model-card-title"><div><strong>{model.name}</strong><small>original · {model.upstream_model}</small></div><div><Badge tone={model.enabled ? 'good' : ''}>{model.enabled ? 'enabled' : 'disabled'}</Badge><Badge>{model.strategy}</Badge></div></div><div className="route-list">{model.routes.map((route) => <div key={route.credential_id}><span>{credentials.find((credential) => credential.id === route.credential_id)?.name || route.credential_id}</span><small>P{route.priority} · W{route.weight} · {route.enabled ? 'active' : 'off'}</small></div>)}</div><PriceStrip price={model.price ?? inherited?.price} source={model.price ? 'manual override' : inherited ? `original · ${inherited.model}` : 'unpriced'} />{isMaster && <div className="card-actions"><button onClick={() => setEditing(model)}>Edit blend</button><button className="danger-text" onClick={() => void remove(model.name)}>Delete</button></div>}</article>})}</div>}
    {(draft || editing) && <ModelModal existing={editing} initial={draft} credentials={credentials} inherited={findCatalogPrice(catalog, (editing ?? draft)?.name ?? '', (editing ?? draft)?.upstream_model ?? '')} onClose={closeModal} onSaved={() => { closeModal(); void load() }} />}
  </>
}

function ModelModal({ existing, initial, credentials, inherited, onClose, onSaved }: { existing: ModelDefinition | null; initial: ModelDefinition | null; credentials: Credential[]; inherited?: CatalogPrice; onClose: () => void; onSaved: () => void }) {
  const source = existing ?? initial
  const [name, setName] = useState(source?.name ?? '')
  const [upstream, setUpstream] = useState(source?.upstream_model ?? '')
  const [strategy, setStrategy] = useState(source?.strategy ?? 'priority')
  const [enabled, setEnabled] = useState(source?.enabled ?? true)
  const [routes, setRoutes] = useState<ModelRoute[]>(source?.routes.length ? source.routes : [{ credential_id: credentials[0]?.id ?? '', priority: 0, weight: 1, enabled: true }])
  const [overridePrice, setOverridePrice] = useState(Boolean(existing?.price))
  const [price, setPrice] = useState<Price>(existing?.price ?? inherited?.price ?? emptyPrice)
  const [error, setError] = useState(''); const [busy, setBusy] = useState(false)
  const submit = async () => { setBusy(true); setError(''); try { await saveModel({ name, upstream_model: upstream || name, strategy, enabled, routes: routes.filter((route) => route.credential_id) }); if (overridePrice) await savePrice(name, price); else if (existing?.price) await deletePrice(name); onSaved() } catch (reason) { setError((reason as Error).message) } finally { setBusy(false) } }
  const updateRoute = (index: number, patch: Partial<ModelRoute>) => setRoutes((current) => current.map((route, routeIndex) => routeIndex === index ? { ...route, ...patch } : route))
  const addRoute = () => setRoutes((current) => [...current, { credential_id: credentials.find((credential) => !current.some((route) => route.credential_id === credential.id))?.id ?? '', priority: 0, weight: 1, enabled: true }])
  const setRate = (key: keyof Price, value: string) => setPrice((current) => ({ ...current, [key]: Number(value) || 0 }))
  return <Modal title={existing ? `Edit ${existing.name}` : 'Create model blend'} onClose={onClose}><ErrorBanner message={error} /><div className="form-grid"><Field label="Public model name"><input value={name} disabled={Boolean(existing)} onChange={(event) => setName(event.target.value)} /></Field><Field label="Original upstream model"><input value={upstream} onChange={(event) => setUpstream(event.target.value)} /></Field><Field label="Strategy"><select value={strategy} onChange={(event) => setStrategy(event.target.value)}><option value="priority">Priority fallback</option><option value="round_robin">Weighted round robin</option></select></Field><Field label="Status"><select value={enabled ? 'enabled' : 'disabled'} onChange={(event) => setEnabled(event.target.value === 'enabled')}><option value="enabled">Enabled</option><option value="disabled">Disabled</option></select></Field></div><div className="form-section-heading"><h3 className="form-section-title">Stacked provider routes</h3><button type="button" className="button secondary small" onClick={addRoute}>Add route</button></div><div className="route-editor">{routes.map((route, index) => <div className="route-editor-row" key={`${route.credential_id}-${index}`}><Field label="Connection"><select value={route.credential_id} onChange={(event) => updateRoute(index, { credential_id: event.target.value })}><option value="">Select connection</option>{credentials.map((credential) => <option value={credential.id} key={credential.id}>{credential.name} · {credential.provider}</option>)}</select></Field><Field label="Priority"><input type="number" value={route.priority} onChange={(event) => updateRoute(index, { priority: Number(event.target.value) })} /></Field><Field label="Weight"><input type="number" min="1" value={route.weight} onChange={(event) => updateRoute(index, { weight: Math.max(1, Number(event.target.value) || 1) })} /></Field><Field label="Status"><select value={route.enabled ? 'enabled' : 'disabled'} onChange={(event) => updateRoute(index, { enabled: event.target.value === 'enabled' })}><option value="enabled">Enabled</option><option value="disabled">Disabled</option></select></Field><button type="button" className="danger-text route-remove" onClick={() => setRoutes((current) => current.filter((_, routeIndex) => routeIndex !== index))}>Remove</button></div>)}</div><label className="check-row price-override"><input type="checkbox" checked={overridePrice} onChange={(event) => setOverridePrice(event.target.checked)} /><span>Override original-model catalog pricing</span></label>{!overridePrice && <div className="safe-note"><strong>Automatic cost</strong><span>{inherited ? `Uses ${inherited.model}: $${inherited.price.input_per_m}/M input, $${inherited.price.output_per_m}/M output, $${inherited.price.cached_input_per_m}/M cache read, $${inherited.price.cache_write_per_m}/M cache write.` : 'No matching catalog price. Usage remains explicitly unpriced until a catalog or manual rate is available.'}</span></div>}{overridePrice && <div className="form-grid four"><Field label="Input / 1M"><input type="number" min="0" step="any" value={price.input_per_m} onChange={(event) => setRate('input_per_m', event.target.value)} /></Field><Field label="Output / 1M"><input type="number" min="0" step="any" value={price.output_per_m} onChange={(event) => setRate('output_per_m', event.target.value)} /></Field><Field label="Cache read / 1M"><input type="number" min="0" step="any" value={price.cached_input_per_m} onChange={(event) => setRate('cached_input_per_m', event.target.value)} /></Field><Field label="Cache write / 1M"><input type="number" min="0" step="any" value={price.cache_write_per_m} onChange={(event) => setRate('cache_write_per_m', event.target.value)} /></Field></div>}<div className="dialog-actions"><button className="button" disabled={busy || !name.trim() || routes.every((route) => !route.credential_id)} onClick={() => void submit()}>Save blend</button></div></Modal>
}
