import { useCallback, useEffect, useMemo, useState } from 'react'
import { deleteModel, deletePrice, discoverModels, getCredentials, getModels, getPricingCatalog, saveModel, savePrice } from '../api/client'
import type { CatalogPrice, Credential, ModelDefinition, ModelRoute, Price, ProviderModel } from '../api/contracts'
import { Badge, Empty, ErrorBanner, Field } from '../components/Management'
import { Modal } from '../components/Modal'
import { ModelUsageModal } from '../components/ModelUsageModal'
import { PageLoading } from '../components/PageState'
import { SearchableSelect, TruncatedText } from '../components/SearchableSelect'
import { useSession } from '../context/SessionContext'
import { formatPriceRate } from '../lib/pricing'

const emptyPrice: Price = { input_per_m: 0, output_per_m: 0, cached_input_per_m: 0, cache_write_per_m: 0 }
type Tab = 'catalog' | 'blends'
interface ConnectedModel { credential: Credential; model: ProviderModel; price?: CatalogPrice }
const catalogPageSize = 50
const statusOptions = [{ value: 'enabled', label: 'Enabled' }, { value: 'disabled', label: 'Disabled' }]

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
  if (!price) return <div className="price-strip free-price"><span>In $0.0000</span><span>Out $0.0000</span><span>Read $0.0000</span><span>Write $0.0000</span><small><b>Free</b> · no catalog price</small></div>
  return <div className="price-strip"><span>In ${formatPriceRate(price.input_per_m)}</span><span>Out ${formatPriceRate(price.output_per_m)}</span><span>Read ${formatPriceRate(price.cached_input_per_m)}</span><span>Write ${formatPriceRate(price.cache_write_per_m)}</span><small>{source}</small></div>
}

export function ModelsPage() {
  const { isMaster, isMasterView: sessionMasterView } = useSession()
  const isMasterView = sessionMasterView ?? isMaster
  const [tab, setTab] = useState<Tab>('catalog')
  const [models, setModels] = useState<ModelDefinition[]>([])
  const [credentials, setCredentials] = useState<Credential[]>([])
  const [catalog, setCatalog] = useState<CatalogPrice[]>([])
  const [connected, setConnected] = useState<ConnectedModel[]>([])
  const [discoveryErrors, setDiscoveryErrors] = useState<string[]>([])
  const [search, setSearch] = useState('')
  const [catalogPage, setCatalogPage] = useState(1)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [editing, setEditing] = useState<ModelDefinition | null>(null)
  const [draft, setDraft] = useState<ModelDefinition | null>(null)
  const [usage, setUsage] = useState<{ model: string; price: Price } | null>(null)

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
  const catalogPages = Math.max(1, Math.ceil(visibleConnected.length / catalogPageSize))
  const pagedConnected = useMemo(() => visibleConnected.slice((catalogPage - 1) * catalogPageSize, catalogPage * catalogPageSize), [catalogPage, visibleConnected])
  useEffect(() => { setCatalogPage(1) }, [search])
  useEffect(() => { if (catalogPage > catalogPages) setCatalogPage(catalogPages) }, [catalogPage, catalogPages])
  const closeModal = () => { setEditing(null); setDraft(null) }
  const remove = async (name: string) => { if (!window.confirm(`Delete model blend ${name}?`)) return; try { await deleteModel(name); await load() } catch (reason) { setError((reason as Error).message) } }
  const addConnected = (item: ConnectedModel) => setDraft({ name: item.model.public_id, upstream_model: item.model.id, strategy: 'priority', enabled: true, routes: [{ credential_id: item.credential.id, priority: 0, weight: 1, enabled: true }] })

  return <>
    <header className="page-header"><div><span className="eyebrow">Manage / Models</span><h1>Models</h1><p>Browse every model exposed by connected providers, then create stable public model blends with stacked routes.</p></div>{isMasterView && <button className="button" onClick={() => setDraft({ name: '', upstream_model: '', strategy: 'priority', enabled: true, routes: [] })}>Create blend</button>}</header>
    <ErrorBanner message={error} />
    <div className="page-tabs" role="tablist"><button className={tab === 'catalog' ? 'active' : ''} onClick={() => setTab('catalog')}>Available models <span>{connected.length}</span></button><button className={tab === 'blends' ? 'active' : ''} onClick={() => setTab('blends')}>Model blends <span>{models.length}</span></button></div>
    {loading ? <PageLoading /> : tab === 'catalog' ? <>
      <div className="catalog-toolbar"><input aria-label="Search models" placeholder="Search model or provider" value={search} onChange={(event) => setSearch(event.target.value)} /><span>{visibleConnected.length} connected models</span></div>
      {discoveryErrors.length > 0 && <div className="banner error-banner">Some provider catalogs could not load: {discoveryErrors.join(' · ')}</div>}
      {visibleConnected.length === 0 ? <Empty title="No connected models" detail="Connect a provider with model discovery, or create a blend manually." /> : <><div className="available-model-grid">{pagedConnected.map((item) => { const callable = models.find((model) => model.enabled && model.name === item.model.public_id); return <article className="available-model-card" key={`${item.credential.id}-${item.model.id}`}><div className="model-card-title"><div><strong><TruncatedText>{item.model.public_id}</TruncatedText></strong><small title={item.model.name || item.model.id}>{item.model.name || item.model.id}</small></div><Badge>{item.credential.provider}</Badge></div><p title={item.credential.name}>{item.credential.name}{item.model.context_length ? ` · ${item.model.context_length.toLocaleString()} context` : ''}</p><PriceStrip price={item.price?.price} source={item.price ? `catalog · ${item.price.model}` : 'free'} /><div className="card-actions model-card-actions">{callable && <button onClick={() => setUsage({ model: callable.name, price: callable.price ?? item.price?.price ?? emptyPrice })}>View usage</button>}{isMasterView && <button onClick={() => addConnected(item)}>Add to blends</button>}</div></article>})}</div>{catalogPages > 1 && <nav className="catalog-pagination" aria-label="Model catalog pages"><button disabled={catalogPage === 1} onClick={() => setCatalogPage((page) => page - 1)}>Previous</button><span>Page {catalogPage} of {catalogPages} · {visibleConnected.length} models</span><button disabled={catalogPage === catalogPages} onClick={() => setCatalogPage((page) => page + 1)}>Next</button></nav>}</>}
    </> : models.length === 0 ? <Empty title="No model blends configured" detail="Add a connected model or define a blend manually." /> : <div className="model-card-grid">{models.map((model) => { const inherited = findCatalogPrice(catalog, model.name, model.upstream_model); const effectivePrice = model.price ?? inherited?.price ?? emptyPrice; return <article className="model-card" key={model.name}><div className="model-card-title"><div><strong><TruncatedText>{model.name}</TruncatedText></strong><small title={model.upstream_model}>original · {model.upstream_model}</small></div><div><Badge tone={model.enabled ? 'good' : ''}>{model.enabled ? 'enabled' : 'disabled'}</Badge><Badge>{model.strategy}</Badge></div></div><div className="route-list">{model.routes.map((route) => { const connection = credentials.find((credential) => credential.id === route.credential_id)?.name || route.credential_id; return <div key={route.credential_id}><span title={connection}>{connection}</span><small>P{route.priority} · W{route.weight} · {route.enabled ? 'active' : 'off'}</small></div> })}</div><PriceStrip price={effectivePrice} source={model.price ? 'manual override' : inherited ? `original · ${inherited.model}` : 'free'} /><div className="card-actions"><button onClick={() => setUsage({ model: model.name, price: effectivePrice })}>View usage</button>{isMasterView && <><button onClick={() => setEditing(model)}>Edit blend</button><button className="danger-text" onClick={() => void remove(model.name)}>Delete</button></>}</div></article>})}</div>}
    {(draft || editing) && <ModelModal existing={editing} initial={draft} credentials={credentials} connected={connected} inherited={findCatalogPrice(catalog, (editing ?? draft)?.name ?? '', (editing ?? draft)?.upstream_model ?? '')} onClose={closeModal} onSaved={() => { closeModal(); void load() }} />}
    {usage && <ModelUsageModal model={usage.model} price={usage.price} onClose={() => setUsage(null)} />}
  </>
}

function ModelModal({ existing, initial, credentials, connected, inherited, onClose, onSaved }: { existing: ModelDefinition | null; initial: ModelDefinition | null; credentials: Credential[]; connected: ConnectedModel[]; inherited?: CatalogPrice; onClose: () => void; onSaved: () => void }) {
  const source = existing ?? initial
  const [name, setName] = useState(source?.name ?? '')
  const [upstream, setUpstream] = useState(source?.upstream_model ?? '')
  const [strategy, setStrategy] = useState(source?.strategy ?? 'priority')
  const [enabled, setEnabled] = useState(source?.enabled ?? true)
  const [routes, setRoutes] = useState<ModelRoute[]>(source?.routes.length ? source.routes.map((route) => ({ ...route, upstream_model: route.upstream_model || source.upstream_model })) : [{ credential_id: credentials[0]?.id ?? '', upstream_model: source?.upstream_model ?? '', priority: 0, weight: 1, enabled: true }])
  const [overridePrice, setOverridePrice] = useState(Boolean(existing?.price))
  const [price, setPrice] = useState<Price>(existing?.price ?? inherited?.price ?? emptyPrice)
  const [error, setError] = useState(''); const [busy, setBusy] = useState(false)
  const submit = async () => { setBusy(true); setError(''); try { await saveModel({ name, upstream_model: upstream || name, strategy, enabled, routes: routes.filter((route) => route.credential_id && route.upstream_model?.trim()) }); if (overridePrice) await savePrice(name, price); else if (existing?.price) await deletePrice(name); onSaved() } catch (reason) { setError((reason as Error).message) } finally { setBusy(false) } }
  const updateRoute = (index: number, patch: Partial<ModelRoute>) => setRoutes((current) => current.map((route, routeIndex) => routeIndex === index ? { ...route, ...patch } : route))
  // Descending priorities preserve the order accounts were stacked: the
  // first route stays active until quota exhaustion, then the next is used.
  const addRoute = () => setRoutes((current) => [...current, { credential_id: credentials[0]?.id ?? '', upstream_model: '', priority: -current.length, weight: 1, enabled: true }])
  const setRate = (key: keyof Price, value: string) => setPrice((current) => ({ ...current, [key]: Number(value) || 0 }))
  return <Modal title={existing ? `Edit ${existing.name}` : 'Create model blend'} onClose={onClose}><ErrorBanner message={error} /><div className="form-grid"><Field label="Public model name"><input value={name} disabled={Boolean(existing)} onChange={(event) => setName(event.target.value)} /></Field><Field label="Original upstream model"><input value={upstream} onChange={(event) => setUpstream(event.target.value)} /></Field><Field label="Strategy"><SearchableSelect value={strategy} onChange={setStrategy} options={[{ value: 'priority', label: 'Priority fallback' }, { value: 'round_robin', label: 'Weighted round robin' }, { value: 'cache_affinity', label: 'Prompt-cache affinity', meta: 'Stable reusable prefixes stay on one connection' }]} /></Field><Field label="Status"><SearchableSelect value={enabled ? 'enabled' : 'disabled'} onChange={(value) => setEnabled(value === 'enabled')} options={statusOptions} /></Field></div><div className="form-section-heading"><h3 className="form-section-title">Stacked provider routes</h3><button type="button" className="button secondary small" onClick={addRoute}>Add route</button></div><div className="route-editor">{routes.map((route, index) => <div className="route-editor-row" key={`${route.credential_id}-${index}`}><Field label="Connection"><SearchableSelect value={route.credential_id} onChange={(value) => updateRoute(index, { credential_id: value })} placeholder="Select connection" searchPlaceholder="Search connections" options={credentials.map((credential) => ({ value: credential.id, label: credential.name, meta: `${credential.provider} · ${credential.id}` }))} /></Field><Field label="Model"><SearchableSelect value={route.upstream_model ?? ""} onChange={(value) => updateRoute(index, { upstream_model: value })} placeholder="Select model" searchPlaceholder="Search models" options={connected.filter((item) => item.credential.id === route.credential_id).map((item) => ({ value: item.model.id, label: item.model.public_id, meta: item.model.id }))} /></Field><Field label="Priority"><input type="number" value={route.priority} onChange={(event) => updateRoute(index, { priority: Number(event.target.value) })} /></Field><Field label="Weight"><input type="number" min="1" value={route.weight} onChange={(event) => updateRoute(index, { weight: Math.max(1, Number(event.target.value) || 1) })} /></Field><Field label="Status"><SearchableSelect value={route.enabled ? 'enabled' : 'disabled'} onChange={(value) => updateRoute(index, { enabled: value === 'enabled' })} options={statusOptions} /></Field><button type="button" className="danger-text route-remove" onClick={() => setRoutes((current) => current.filter((_, routeIndex) => routeIndex !== index))}>Remove</button></div>)}</div><label className="check-row price-override"><input type="checkbox" checked={overridePrice} onChange={(event) => setOverridePrice(event.target.checked)} /><span>Override original-model catalog pricing</span></label>{!overridePrice && <div className="safe-note"><strong>Automatic cost</strong><span>{inherited ? `Uses ${inherited.model}: $${formatPriceRate(inherited.price.input_per_m)}/M input, $${formatPriceRate(inherited.price.output_per_m)}/M output, $${formatPriceRate(inherited.price.cached_input_per_m)}/M cache read, $${formatPriceRate(inherited.price.cache_write_per_m)}/M cache write.` : 'No matching catalog price. This model is tracked as Free at $0.'}</span></div>}{overridePrice && <div className="form-grid four"><Field label="Input / 1M"><input type="number" min="0" step="any" value={price.input_per_m} onChange={(event) => setRate('input_per_m', event.target.value)} /></Field><Field label="Output / 1M"><input type="number" min="0" step="any" value={price.output_per_m} onChange={(event) => setRate('output_per_m', event.target.value)} /></Field><Field label="Cache read / 1M"><input type="number" min="0" step="any" value={price.cached_input_per_m} onChange={(event) => setRate('cached_input_per_m', event.target.value)} /></Field><Field label="Cache write / 1M"><input type="number" min="0" step="any" value={price.cache_write_per_m} onChange={(event) => setRate('cache_write_per_m', event.target.value)} /></Field></div>}<div className="dialog-actions"><button className="button" disabled={busy || !name.trim() || routes.every((route) => !route.credential_id || !route.upstream_model?.trim())} onClick={() => void submit()}>Save blend</button></div></Modal>
}
