const baseURL = (process.env.SEED_BASE_URL ?? 'http://127.0.0.1:8090').replace(/\/$/, '')
const masterKey = process.env.MASTER_KEY
const backend = process.env.SEED_BACKEND ?? 'postgresql'

if (!masterKey) throw new Error('MASTER_KEY is required')

const providerInputs = [
  { id: 'groq', label: 'Groq', keys: splitKeys(process.env.SMOKE_GROQ_KEYS), preferred: ['llama-3.1-8b-instant', 'openai/gpt-oss-20b', 'llama-3.3-70b-versatile'] },
  { id: 'opencode-zen', label: 'OpenCode Zen', keys: splitKeys(process.env.SMOKE_OPENCODE_ZEN_KEYS), preferred: ['deepseek-v4-flash', 'kimi-k2.5-free', 'muse-spark-1.2'] },
  { id: 'openrouter', label: 'OpenRouter', keys: splitKeys(process.env.SMOKE_OPENROUTER_KEYS), preferred: ['openrouter/free', 'meta-llama/llama-3.3-70b-instruct:free', 'deepseek/deepseek-chat-v3-0324:free'] },
  { id: 'gemini', label: 'Gemini', keys: splitKeys(process.env.SMOKE_GEMINI_KEYS), preferred: ['gemini-2.5-flash', 'gemini-2.5-flash-lite', 'gemini-3-flash-preview'] },
].filter((provider) => provider.keys.length > 0)

if (providerInputs.length === 0) throw new Error('at least one SMOKE_*_KEYS variable is required')

const people = [
  'ada.lovelace@example.com',
  'alan.turing@example.com',
  'grace.hopper@example.com',
  'claude.shannon@example.com',
  'margaret.hamilton@example.com',
  'joan.clarke@example.com',
]

const organizationPlans = [
  { name: 'Analytical Engine Society', admins: [0], members: [2] },
  { name: 'Bletchley Park', admins: [1], members: [5] },
  { name: 'Bell Laboratories', admins: [3], members: [4] },
]

const report = {
  backend,
  base_url: baseURL,
  users: [],
  organizations: [],
  credentials: [],
  models: [],
  api_keys: [],
  provider_checks: [],
  gateway_checks: [],
}

function splitKeys(value = '') {
  return value.split(',').map((item) => item.trim()).filter(Boolean)
}

function safeErrorBody(value) {
  if (!value) return ''
  try {
    const parsed = JSON.parse(value)
    return String(parsed?.error?.message ?? parsed?.message ?? '').slice(0, 240)
  } catch {
    return value.replace(/(?:gsk_|sk-|AQ\.)[A-Za-z0-9_.-]+/g, '[redacted]').slice(0, 240)
  }
}

async function request(path, { method = 'GET', body, key = masterKey, accept = 'application/json', timeout = 60000, allowFailure = false } = {}) {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), timeout)
  try {
    const response = await fetch(`${baseURL}${path}`, {
      method,
      signal: controller.signal,
      headers: {
        Accept: accept,
        Authorization: `Bearer ${key}`,
        ...(body === undefined ? {} : { 'Content-Type': 'application/json' }),
      },
      body: body === undefined ? undefined : JSON.stringify(body),
    })
    const text = await response.text()
    if (!response.ok && !allowFailure) throw new Error(`${method} ${path}: HTTP ${response.status} ${safeErrorBody(text)}`)
    let data = text
    if ((response.headers.get('content-type') ?? '').includes('json') && text) {
      try { data = JSON.parse(text) } catch { /* retain text */ }
    }
    return { ok: response.ok, status: response.status, data, text }
  } finally {
    clearTimeout(timer)
  }
}

function modelID(item) {
  return String(item?.id ?? item?.root ?? '').trim()
}

function chooseModels(modelLists, preferred) {
  if (modelLists.length === 0) return []
  const common = modelLists.slice(1).reduce((result, current) => {
    const values = new Set(current)
    return result.filter((model) => values.has(model))
  }, [...modelLists[0]])
  const selected = []
  for (const wanted of preferred) {
    const exact = common.find((model) => model === wanted)
    const suffix = common.find((model) => model.endsWith(`/${wanted}`))
    const match = exact ?? suffix
    if (match && !selected.includes(match)) selected.push(match)
  }
  for (const model of [...common].sort()) {
    if (selected.length >= 2) break
    if (!selected.includes(model)) selected.push(model)
  }
  return selected.slice(0, 2)
}

await request('/healthz')

const users = []
for (const username of people) {
  const response = await request('/admin/users', { method: 'POST', body: { username } })
  users.push(response.data.user)
  report.users.push({ id: response.data.user.id, username: response.data.user.username })
}

const organizations = []
const organizationByUser = new Map()
for (const plan of organizationPlans) {
  const response = await request('/admin/organizations', { method: 'POST', body: { name: plan.name } })
  const organization = response.data
  organizations.push(organization)
  report.organizations.push({ id: organization.id, name: organization.name })
  for (const index of plan.admins) {
    await request(`/admin/organizations/${organization.id}/members`, { method: 'POST', body: { user_id: users[index].id, role: 'admin' } })
    organizationByUser.set(index, organization)
  }
  for (const index of plan.members) {
    await request(`/admin/organizations/${organization.id}/members`, { method: 'POST', body: { user_id: users[index].id, role: 'member' } })
    organizationByUser.set(index, organization)
  }
}

const credentialsByProvider = new Map()
for (const provider of providerInputs) {
  const credentials = []
  for (let index = 0; index < provider.keys.length; index += 1) {
    const response = await request('/admin/credentials', {
      method: 'POST',
      body: { name: `${provider.label} ${index + 1}`, provider: provider.id, kind: 'api_key', api_key: provider.keys[index] },
    })
    credentials.push(response.data)
    report.credentials.push({ id: response.data.id, name: response.data.name, provider: response.data.provider, key_preview: response.data.key_preview })
  }
  credentialsByProvider.set(provider.id, credentials)
}

const importedModels = []
for (const provider of providerInputs) {
  const credentials = credentialsByProvider.get(provider.id) ?? []
  const discovered = []
  for (const credential of credentials) {
    const connectivity = await request(`/admin/credentials/${credential.id}/test`, { method: 'POST', allowFailure: true })
    const modelsResponse = await request(`/admin/credentials/${credential.id}/models`, { allowFailure: true })
    const models = modelsResponse.ok && Array.isArray(modelsResponse.data?.data) ? modelsResponse.data.data.map(modelID).filter(Boolean) : []
    discovered.push(models)
    report.provider_checks.push({ provider: provider.id, credential_id: credential.id, connectivity_status: connectivity.status, models_status: modelsResponse.status, discovered_models: models.length })
  }
  const selected = chooseModels(discovered, provider.preferred)
  if (selected.length === 0) continue
  for (const credential of credentials) {
    const imported = await request(`/admin/credentials/${credential.id}/models/import`, { method: 'POST', body: { models: selected } })
    for (const model of imported.data.imported ?? []) if (!importedModels.includes(model)) importedModels.push(model)
    const chat = await request(`/admin/credentials/${credential.id}/chat-tests`, {
      method: 'POST',
      body: { model: selected[0], prompt: 'Reply with exactly: connection healthy' },
      accept: 'text/event-stream',
      timeout: 90000,
      allowFailure: true,
    })
    const check = report.provider_checks.find((item) => item.credential_id === credential.id)
    check.selected_models = selected
    check.chat_status = chat.status
    if (!chat.ok) check.chat_error = safeErrorBody(chat.text)
  }
}

if (importedModels.length === 0) throw new Error('no callable provider models could be discovered and imported')
report.models = importedModels

const userKeys = []
for (let index = 0; index < users.length; index += 1) {
  const organization = organizationByUser.get(index)
  if (!organization) throw new Error(`no organization assigned to ${people[index]}`)
  const assigned = [importedModels[index % importedModels.length], importedModels[(index + 1) % importedModels.length]].filter((value, position, values) => values.indexOf(value) === position)
  const quota = 2
  const rpm = 30
  const response = await request('/admin/api-keys', {
    method: 'POST',
    body: {
      name: `${people[index].split('@')[0].replaceAll('.', ' ')} chat`,
      owner_type: 'user',
      owner_user_id: users[index].id,
      context_organization_id: organization.id,
      models: assigned,
      scopes: ['chat'],
      quota_usd: quota,
      quota_period: 'week',
      rpm,
    },
  })
  userKeys.push(response.data)
  report.api_keys.push({ id: response.data.id, name: response.data.name, key_prefix: response.data.key_prefix, owner_type: 'user', owner_user_id: users[index].id, organization_id: organization.id, models: assigned, quota_usd: quota, rpm })
}

const organizationKeys = []
for (const organization of organizations) {
  const quota = 5
  const rpm = 60
  const response = await request('/admin/api-keys', {
    method: 'POST',
    body: {
      name: `${organization.name} shared chat`,
      owner_type: 'organization',
      owner_organization_id: organization.id,
      context_organization_id: organization.id,
      models: importedModels,
      scopes: ['chat'],
      quota_usd: quota,
      quota_period: 'week',
      rpm,
    },
  })
  organizationKeys.push(response.data)
  report.api_keys.push({ id: response.data.id, name: response.data.name, key_prefix: response.data.key_prefix, owner_type: 'organization', organization_id: organization.id, models: importedModels, quota_usd: quota, rpm })
}

for (let index = 0; index < userKeys.length; index += 1) {
  const key = userKeys[index]
  const models = await request('/v1/models', { key: key.plaintext })
  const allowed = Array.isArray(models.data?.data) ? models.data.data.map((item) => item.id) : []
  const expected = report.api_keys.find((item) => item.id === key.id)?.models ?? []
  if (allowed.length !== expected.length || expected.some((model) => !allowed.includes(model))) throw new Error(`allowed model mismatch for ${key.id}`)
  const model = expected[0]
  const chat = await request('/v1/chat/completions', {
    method: 'POST',
    key: key.plaintext,
    body: { model, messages: [{ role: 'user', content: `Reply with exactly: ${people[index].split('@')[0]}` }], max_tokens: 24, stream: false },
    timeout: 90000,
    allowFailure: true,
  })
  report.gateway_checks.push({ key_id: key.id, model, stream: false, status: chat.status, ...(chat.ok ? {} : { error: safeErrorBody(chat.text) }) })
}

if (organizationKeys.length > 0) {
  for (let index = 0; index < importedModels.length; index += 1) {
    const key = organizationKeys[index % organizationKeys.length]
    const model = importedModels[index]
    const chat = await request('/v1/chat/completions', {
      method: 'POST',
      key: key.plaintext,
      body: { model, messages: [{ role: 'user', content: `Reply with exactly: ${model} healthy` }], max_tokens: 24, stream: false },
      timeout: 90000,
      allowFailure: true,
    })
    report.gateway_checks.push({ key_id: key.id, model, stream: false, status: chat.status, ...(chat.ok ? {} : { error: safeErrorBody(chat.text) }) })
  }
  const key = organizationKeys[0]
  const model = importedModels.find((item) => item.startsWith('ocz/')) ?? importedModels[0]
  const stream = await request('/v1/chat/completions', {
    method: 'POST',
    key: key.plaintext,
    body: { model, messages: [{ role: 'user', content: 'Reply with exactly: streaming healthy' }], max_tokens: 24, stream: true },
    accept: 'text/event-stream',
    timeout: 90000,
    allowFailure: true,
  })
  report.gateway_checks.push({ key_id: key.id, model, stream: true, status: stream.status, ...(stream.ok ? {} : { error: safeErrorBody(stream.text) }) })
}

const listedUsers = await request('/admin/users?limit=100')
const listedOrganizations = await request('/admin/organizations?limit=100')
const listedKeys = await request('/admin/api-keys?limit=100')
const listedCredentials = await request('/admin/credentials')
const listedModels = await request('/admin/models')
const summary = await request('/admin/usage/summary')

const counts = {
  users: listedUsers.data.data?.length ?? 0,
  organizations: listedOrganizations.data.data?.length ?? 0,
  api_keys: listedKeys.data.data?.length ?? 0,
  credentials: Array.isArray(listedCredentials.data) ? listedCredentials.data.length : 0,
  models: Array.isArray(listedModels.data) ? listedModels.data.length : 0,
  usage_requests: summary.data.requests ?? 0,
}
if (counts.users !== people.length) throw new Error(`expected ${people.length} users, found ${counts.users}`)
if (counts.organizations < organizationPlans.length) throw new Error(`expected at least ${organizationPlans.length} organizations, found ${counts.organizations}`)
if (counts.api_keys !== userKeys.length + organizationKeys.length) throw new Error(`API key count mismatch: ${counts.api_keys}`)
report.counts = counts
report.ok = true

process.stdout.write(`${JSON.stringify(report, null, 2)}\n`)
