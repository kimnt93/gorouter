import { chmod, writeFile } from 'node:fs/promises'

const baseURL = (process.env.SEED_BASE_URL ?? 'http://127.0.0.1:8090').replace(/\/$/, '')
const masterKey = process.env.MASTER_KEY
const backend = process.env.SEED_BACKEND ?? 'postgresql'
const accessFile = process.env.SEED_ACCESS_FILE ?? ''
if (!masterKey) throw new Error('MASTER_KEY is required')

const providerInputs = [
  { id: 'groq', label: 'Groq', keys: splitKeys(process.env.SMOKE_GROQ_KEYS), preferred: ['llama-3.1-8b-instant', 'openai/gpt-oss-20b'] },
  { id: 'opencode-zen', label: 'OpenCode Zen', keys: splitKeys(process.env.SMOKE_OPENCODE_ZEN_KEYS), preferred: ['deepseek-v4-flash', 'kimi-k2.5-free'] },
  { id: 'openrouter', label: 'OpenRouter', keys: splitKeys(process.env.SMOKE_OPENROUTER_KEYS), preferred: ['openrouter/free', 'meta-llama/llama-3.3-70b-instruct:free'] },
  { id: 'gemini', label: 'Gemini', keys: splitKeys(process.env.SMOKE_GEMINI_KEYS), preferred: ['gemini-2.5-flash', 'gemini-2.5-flash-lite'] },
].filter((provider) => provider.keys.length > 0)
if (providerInputs.length === 0) throw new Error('at least one SMOKE_*_KEYS variable is required')

const people = [
  'ada.lovelace@example.com', 'alan.turing@example.com', 'grace.hopper@example.com', 'claude.shannon@example.com', 'margaret.hamilton@example.com',
  'joan.clarke@example.com', 'john.von.neumann@example.com', 'katherine.johnson@example.com', 'donald.knuth@example.com', 'barbara.liskov@example.com',
  'edsger.dijkstra@example.com', 'frances.allen@example.com', 'george.boole@example.com', 'hedy.lamarr@example.com', 'tim.berners.lee@example.com',
  'radia.perlman@example.com', 'dennis.ritchie@example.com', 'ken.thompson@example.com', 'linus.torvalds@example.com', 'james.gosling@example.com',
  'guido.van.rossum@example.com', 'brendan.eich@example.com', 'bjarne.stroustrup@example.com', 'john.mccarthy@example.com', 'marvin.minsky@example.com',
  'allen.newell@example.com', 'herbert.simon@example.com', 'norbert.wiener@example.com', 'alonzo.church@example.com', 'kurt.godel@example.com',
  'emmy.noether@example.com', 'mary.jackson@example.com', 'dorothy.vaughan@example.com', 'annie.easley@example.com', 'evelyn.boyd.granville@example.com',
  'maryam.mirzakhani@example.com', 'sophie.germain@example.com', 'florence.nightingale@example.com', 'cecilia.payne@example.com', 'chien.shiung.wu@example.com',
  'lise.meitner@example.com', 'marie.curie@example.com', 'niels.bohr@example.com', 'richard.feynman@example.com', 'subrahmanyan.chandrasekhar@example.com',
  'srinivasa.ramanujan@example.com', 'david.hilbert@example.com', 'bernhard.riemann@example.com', 'carl.gauss@example.com', 'leonhard.euler@example.com',
]
const organizationNames = ['Microsoft', 'VN Fin', 'Bletchley Park', 'Bell Laboratories', 'NASA', 'CERN', 'MIT AI Laboratory', 'Apollo Guidance', 'Royal Society', 'Institute for Advanced Study']
const multiOrganizationCounts = new Map([[0, 4], [1, 3], [2, 2], [3, 4], [4, 3], [5, 2], [6, 4], [7, 3]])

const report = { backend, base_url: baseURL, users: [], organizations: [], memberships: [], credentials: [], models: [], api_keys: [], provider_checks: [], gateway_checks: [], authorization_checks: [], prompt_profiles: {} }
const access = { backend, base_url: baseURL, admin_keys: [], sample_user_keys: [], organization_keys: [], personal_keys: [] }

function splitKeys(value = '') { return value.split(',').map((item) => item.trim()).filter(Boolean) }
function safeErrorBody(value) {
  if (!value) return ''
  try { const parsed = JSON.parse(value); return String(parsed?.error?.message ?? parsed?.error ?? parsed?.message ?? '').slice(0, 240) }
  catch { return value.replace(/(?:gsk_|sk-|AQ\.)[A-Za-z0-9_.-]+/g, '[redacted]').slice(0, 240) }
}
async function request(path, { method = 'GET', body, key = masterKey, accept = 'application/json', timeout = 120000, allowFailure = false } = {}) {
  const controller = new AbortController(); const timer = setTimeout(() => controller.abort(), timeout)
  try {
    const response = await fetch(`${baseURL}${path}`, { method, signal: controller.signal, headers: { Accept: accept, Authorization: `Bearer ${key}`, ...(body === undefined ? {} : { 'Content-Type': 'application/json' }) }, body: body === undefined ? undefined : JSON.stringify(body) })
    const text = await response.text()
    if (!response.ok && !allowFailure) throw new Error(`${method} ${path}: HTTP ${response.status} ${safeErrorBody(text)}`)
    let data = text
    if ((response.headers.get('content-type') ?? '').includes('json') && text) { try { data = JSON.parse(text) } catch { /* retain text */ } }
    return { ok: response.ok, status: response.status, data, text }
  } finally { clearTimeout(timer) }
}
function modelID(item) { return String(item?.id ?? item?.root ?? '').trim() }
function chooseModels(models, preferred) {
  const selected = []
  for (const wanted of preferred) {
    const match = models.find((model) => model === wanted) ?? models.find((model) => model.endsWith(`/${wanted}`))
    if (match && !selected.includes(match)) selected.push(match)
  }
  for (const model of [...models].sort()) { if (selected.length >= 2) break; if (!selected.includes(model)) selected.push(model) }
  return selected.slice(0, 2)
}
function buildPrompt(profile, tokenTarget) {
  const prefix = `Repository simulation ${profile}. Analyze authorization boundaries, provider ownership, cache accounting, and concurrent request safety. `
  return (prefix + 'Use precise operational language and return one short sentence. '.repeat(Math.ceil(tokenTarget * 4 / 58))).slice(0, tokenTarget * 4)
}
const prompts = { short: buildPrompt('short', 100), medium: buildPrompt('medium', 2000), long: buildPrompt('long', 10000) }
report.prompt_profiles = Object.fromEntries(Object.entries(prompts).map(([name, value]) => [name, { estimated_tokens: Math.round(value.length / 4), characters: value.length }]))

await request('/healthz')
const users = []
for (const username of people) {
  const response = await request('/admin/users', { method: 'POST', body: { username } })
  users.push(response.data.user); report.users.push({ id: response.data.user.id, username: response.data.user.username })
}
const organizations = []
for (const name of organizationNames) {
  const response = await request('/admin/organizations', { method: 'POST', body: { name } })
  organizations.push(response.data); report.organizations.push({ id: response.data.id, name: response.data.name })
}

const membershipsByUser = new Map()
for (let userIndex = 0; userIndex < users.length; userIndex += 1) {
  const organizationIndexes = [userIndex % organizations.length]
  const wantedCount = multiOrganizationCounts.get(userIndex) ?? 1
  for (let offset = 1; organizationIndexes.length < wantedCount; offset += 1) organizationIndexes.push((userIndex + offset * 3) % organizations.length)
  const uniqueIndexes = [...new Set(organizationIndexes)]
  membershipsByUser.set(userIndex, uniqueIndexes)
  for (const organizationIndex of uniqueIndexes) {
    const role = userIndex === organizationIndex ? 'admin' : 'member'
    const response = await request(`/admin/organizations/${organizations[organizationIndex].id}/members`, { method: 'POST', body: { user_id: users[userIndex].id, role } })
    report.memberships.push({ organization_id: organizations[organizationIndex].id, user_id: users[userIndex].id, role: response.data.role })
  }
}
const multiUsers = [...membershipsByUser.entries()].filter(([, memberships]) => memberships.length > 1)
if (multiUsers.length !== 8 || multiUsers.some(([, memberships]) => memberships.length < 2 || memberships.length > 4)) throw new Error('multi-organization fixture invariant failed')
if ([...membershipsByUser.entries()].some(([index, memberships]) => index >= 8 && memberships.length !== 1)) throw new Error('single-organization fixture invariant failed')

const sources = []
for (const provider of providerInputs) for (let index = 0; index < provider.keys.length; index += 1) sources.push({ provider, key: provider.keys[index], index })
const preferredSourceOrder = ['openrouter', 'opencode-zen', 'groq', 'gemini', 'opencode-zen', 'openrouter', 'groq', 'gemini', 'opencode-zen', 'openrouter']
const organizationModels = new Map()
for (let organizationIndex = 0; organizationIndex < organizations.length; organizationIndex += 1) {
  const candidates = sources.filter((source) => source.provider.id === preferredSourceOrder[organizationIndex])
  const source = candidates[organizationIndex % candidates.length] ?? sources[organizationIndex % sources.length]
  const organization = organizations[organizationIndex]
  const created = await request('/admin/credentials', { method: 'POST', body: { name: `${organization.name} ${source.provider.label}`, provider: source.provider.id, kind: 'api_key', api_key: source.key, owner_type: 'organization', owner_organization_id: organization.id } })
  const credential = created.data
  report.credentials.push({ id: credential.id, name: credential.name, provider: credential.provider, owner_type: 'organization', owner_organization_id: organization.id, key_preview: credential.key_preview })
  const connectivity = await request(`/admin/credentials/${credential.id}/test`, { method: 'POST', allowFailure: true })
  const discoveredResponse = await request(`/admin/credentials/${credential.id}/models`, { allowFailure: true })
  const discovered = discoveredResponse.ok && Array.isArray(discoveredResponse.data?.data) ? discoveredResponse.data.data.map(modelID).filter(Boolean) : []
  const selected = chooseModels(discovered, source.provider.preferred)
  const check = { provider: source.provider.id, credential_id: credential.id, organization: organization.name, connectivity_status: connectivity.status, models_status: discoveredResponse.status, discovered_models: discovered.length, selected_models: selected }
  if (selected.length > 0) {
    const imported = await request(`/admin/credentials/${credential.id}/models/import`, { method: 'POST', body: { models: selected } })
    organizationModels.set(organization.id, imported.data.imported ?? []); report.models.push(...(imported.data.imported ?? []))
    const chat = await request(`/admin/credentials/${credential.id}/chat-tests`, { method: 'POST', body: { model: selected[0], prompt: 'Reply with exactly: connection healthy' }, accept: 'text/event-stream', allowFailure: true })
    check.chat_status = chat.status; if (!chat.ok) check.chat_error = safeErrorBody(chat.text)
  } else check.chat_error = 'no models discovered'
  report.provider_checks.push(check)
}

const personalUserIndex = 17
const personalSource = sources.find((source) => source.provider.id === 'opencode-zen') ?? sources[0]
const personalCreated = await request('/admin/credentials', { method: 'POST', body: { name: `${people[personalUserIndex]} personal`, provider: personalSource.provider.id, kind: 'api_key', api_key: personalSource.key, owner_type: 'user', owner_user_id: users[personalUserIndex].id } })
const personalCredential = personalCreated.data
report.credentials.push({ id: personalCredential.id, name: personalCredential.name, provider: personalCredential.provider, owner_type: 'user', owner_user_id: users[personalUserIndex].id, key_preview: personalCredential.key_preview })
const personalDiscovery = await request(`/admin/credentials/${personalCredential.id}/models`)
const personalSelected = chooseModels(personalDiscovery.data.data.map(modelID).filter(Boolean), personalSource.provider.preferred)
const personalImport = await request(`/admin/credentials/${personalCredential.id}/models/import`, { method: 'POST', body: { models: personalSelected } })
const personalModels = personalImport.data.imported ?? []; report.models.push(...personalModels)

const managementScopes = ['chat', 'usage:read', 'keys:manage', 'credentials:manage', 'models:manage', 'members:manage']
const adminKeys = []
for (let index = 0; index < organizations.length; index += 1) {
  const organization = organizations[index]; const models = organizationModels.get(organization.id) ?? []
  if (models.length === 0) throw new Error(`no imported models for ${organization.name}`)
  const response = await request('/admin/api-keys', { method: 'POST', body: { name: `${organization.name} administrator`, owner_type: 'user', owner_user_id: users[index].id, context_organization_id: organization.id, models, scopes: managementScopes, quota_usd: 25, quota_period: 'week', rpm: 120 } })
  adminKeys.push(response.data); report.api_keys.push({ id: response.data.id, name: response.data.name, owner_type: 'user', owner_user_id: users[index].id, organization_id: organization.id, models, role: 'admin' })
  access.admin_keys.push({ username: people[index], organization: organization.name, plaintext: response.data.plaintext })
}

const userKeys = []
for (let index = 0; index < users.length; index += 1) {
  const organization = organizations[index % organizations.length]; const models = organizationModels.get(organization.id) ?? []
  const response = await request('/admin/api-keys', { method: 'POST', body: { name: `${people[index].split('@')[0].replaceAll('.', ' ')} workspace`, owner_type: 'user', owner_user_id: users[index].id, context_organization_id: organization.id, models: models.slice(0, 2), scopes: ['chat', 'usage:read'], quota_usd: 5, quota_period: 'week', rpm: 60 } })
  userKeys.push(response.data); report.api_keys.push({ id: response.data.id, name: response.data.name, owner_type: 'user', owner_user_id: users[index].id, organization_id: organization.id, models: models.slice(0, 2), role: 'member' })
  if (index < 5 || index === personalUserIndex) access.sample_user_keys.push({ username: people[index], organization: organization.name, plaintext: response.data.plaintext })
}

const organizationKeys = []
for (let index = 0; index < organizations.length; index += 1) {
  const organization = organizations[index]; const models = organizationModels.get(organization.id) ?? []
  const response = await request('/admin/api-keys', { method: 'POST', body: { name: `${organization.name} shared workload`, owner_type: 'organization', owner_organization_id: organization.id, context_organization_id: organization.id, models, scopes: ['chat', 'usage:read'], quota_usd: 20, quota_period: 'week', rpm: 120 } })
  organizationKeys.push(response.data); report.api_keys.push({ id: response.data.id, name: response.data.name, owner_type: 'organization', organization_id: organization.id, models })
  access.organization_keys.push({ organization: organization.name, plaintext: response.data.plaintext })
}

const personalKeyResponse = await request('/admin/api-keys', { method: 'POST', body: { name: `${people[personalUserIndex]} private project`, owner_type: 'user', owner_user_id: users[personalUserIndex].id, context_organization_id: '', models: personalModels, scopes: ['chat', 'usage:read', 'credentials:manage', 'models:manage'], quota_usd: 5, quota_period: 'week', rpm: 60 } })
const personalKey = personalKeyResponse.data
report.api_keys.push({ id: personalKey.id, name: personalKey.name, owner_type: 'user', owner_user_id: users[personalUserIndex].id, organization_id: '', models: personalModels, role: 'personal' })
access.personal_keys.push({ username: people[personalUserIndex], plaintext: personalKey.plaintext })

const delegatedUserIndex = 10
const delegated = await request('/admin/api-keys', { method: 'POST', key: adminKeys[0].plaintext, body: { name: 'Microsoft delegated member key', owner_type: 'user', owner_user_id: users[delegatedUserIndex].id, context_organization_id: organizations[0].id, models: (organizationModels.get(organizations[0].id) ?? []).slice(0, 1), scopes: ['chat'], quota_usd: 1, quota_period: 'week', rpm: 20 } })
report.api_keys.push({ id: delegated.data.id, name: delegated.data.name, owner_type: 'user', owner_user_id: users[delegatedUserIndex].id, organization_id: organizations[0].id, delegated_by_org_admin: true })

async function gateway(key, model, profile, stream) {
  const response = await request('/v1/chat/completions', { method: 'POST', key, body: { model, messages: [{ role: 'user', content: prompts[profile] }], max_tokens: 32, stream, reasoning: { effort: 'low' } }, accept: stream ? 'text/event-stream' : 'application/json', timeout: 180000, allowFailure: true })
  report.gateway_checks.push({ profile, model, stream, status: response.status, ...(response.ok ? {} : { error: safeErrorBody(response.text) }) })
  return response
}
for (let index = 0; index < userKeys.length; index += 1) {
  const models = report.api_keys.find((item) => item.id === userKeys[index].id)?.models ?? []
  await gateway(userKeys[index].plaintext, models[0], 'short', false)
}
for (let index = 0; index < organizationKeys.length; index += 1) {
  const models = organizationModels.get(organizations[index].id) ?? []
  await gateway(organizationKeys[index].plaintext, models[0], 'medium', false)
  await gateway(organizationKeys[index].plaintext, models[0], 'long', index % 2 === 0)
}
for (const profile of ['short', 'medium', 'long']) await gateway(personalKey.plaintext, personalModels[0], profile, profile === 'long')

const ownMembers = await request(`/admin/organizations/${organizations[0].id}/members`, { key: adminKeys[0].plaintext })
const foreignMembers = await request(`/admin/organizations/${organizations[1].id}/members`, { key: adminKeys[0].plaintext, allowFailure: true })
const adminCredentials = await request('/admin/credentials', { key: adminKeys[0].plaintext })
const personalCredentials = await request('/admin/credentials', { key: personalKey.plaintext })
const userRecent = await request('/admin/usage/recent?limit=100', { key: userKeys[personalUserIndex].plaintext })
const orgRecent = await request(`/admin/usage/recent?limit=500&organization_id=${organizations[0].id}`, { key: adminKeys[0].plaintext })
const userModelList = await request('/v1/models', { key: userKeys[0].plaintext })
const expectedModels = report.api_keys.find((item) => item.id === userKeys[0].id)?.models ?? []
const listedModelIDs = userModelList.data.data?.map((item) => item.id) ?? []
const checks = [
  ['org admin lists all own members', ownMembers.ok && ownMembers.data.data.length === report.memberships.filter((item) => item.organization_id === organizations[0].id).length],
  ['org admin cannot list foreign members', foreignMembers.status === 403 || foreignMembers.status === 404],
  ['org admin cannot see member personal credential', !adminCredentials.data.some((item) => item.id === personalCredential.id)],
  ['personal user sees own credential', personalCredentials.data.some((item) => item.id === personalCredential.id)],
  ['user usage contains only own actor', userRecent.data.data.every((item) => item.user_id === users[personalUserIndex].id)],
  ['org usage contains only organization context', orgRecent.data.data.every((item) => item.organization_id === organizations[0].id)],
  ['API key lists exact allowed models', listedModelIDs.length === expectedModels.length && expectedModels.every((model) => listedModelIDs.includes(model))],
  ['organization model namespace', [...organizationModels.values()].flat().every((model) => model.split('/').length >= 3)],
  ['personal model namespace', personalModels.every((model) => model.split('/').length === 2)],
]
for (const [name, ok] of checks) { report.authorization_checks.push({ name, ok }); if (!ok) throw new Error(`authorization check failed: ${name}`) }

const listedUsers = await request('/admin/users?limit=100')
const listedOrganizations = await request('/admin/organizations?limit=100')
const listedKeys = await request('/admin/api-keys?limit=500')
const listedCredentials = await request('/admin/credentials')
const listedModels = await request('/admin/models')
async function waitForUsageCount(expected, timeout = 30000) {
  const deadline = Date.now() + timeout
  let summary
  do {
    summary = await request('/admin/usage/summary')
    if ((summary.data.requests ?? 0) >= expected) return summary
    await new Promise((resolve) => setTimeout(resolve, 250))
  } while (Date.now() < deadline)
  return summary
}
const summary = await waitForUsageCount(report.gateway_checks.length)
const seededOrganizationNames = new Set(organizationNames)
const counts = {
  users: listedUsers.data.data?.length ?? 0,
  seeded_organizations: (listedOrganizations.data.data ?? []).filter((item) => seededOrganizationNames.has(item.name)).length,
  memberships: report.memberships.length,
  multi_organization_users: multiUsers.length,
  api_keys: listedKeys.data.data?.length ?? 0,
  credentials: Array.isArray(listedCredentials.data) ? listedCredentials.data.length : 0,
  models: Array.isArray(listedModels.data) ? listedModels.data.length : 0,
  usage_requests: summary.data.requests ?? 0,
}
if (counts.users !== 50 || counts.seeded_organizations !== 10 || counts.multi_organization_users >= 10) throw new Error(`fixture count mismatch: ${JSON.stringify(counts)}`)
if (counts.api_keys !== report.api_keys.length) throw new Error(`API key count mismatch: ${counts.api_keys} != ${report.api_keys.length}`)
if (counts.usage_requests < report.gateway_checks.length) throw new Error(`usage count mismatch: ${counts.usage_requests} < ${report.gateway_checks.length}`)
report.counts = counts; report.ok = true
if (accessFile) { await writeFile(accessFile, `${JSON.stringify(access, null, 2)}\n`, { mode: 0o600 }); await chmod(accessFile, 0o600) }
process.stdout.write(`${JSON.stringify(report, null, 2)}\n`)
