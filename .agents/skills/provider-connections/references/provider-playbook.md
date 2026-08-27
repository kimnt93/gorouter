# Provider implementation playbook

`pkg/provider/catalog.go` is the canonical source for safe static metadata. Do
not duplicate provider names, prefixes, protocols, or default endpoints in the
React UI.

## Current catalog

| Authentication | Provider IDs and public prefixes |
|---|---|
| OAuth/subscription | `claude` → `cc`, `codex` → `cx`, `github-copilot` → `ghc`, `cursor` → `cursor`, `grok-build` → `gb`, `xai-oauth` → `xai`, `kimi-code` → `kimi`, `cline` → `cline`, `clinepass` → `clinepass`, `kilo-code` → `kilo`, `kiro` → `kiro`, `amazon-q` → `amazonq`, `antigravity` → `ag` |
| API key | `openai`, `anthropic`, `gemini`, `groq`, `openrouter`, `opencode-zen` → `ocz`, `opencode-go` → `ocg`, `xai`, `deepseek`, `moonshot`, `qwen`, `openai-compatible` → `custom` |

Check the catalog itself for current URLs, protocol, OAuth refresh, custom-base-
URL, and quota flags; this table is an orientation aid.

## Add or update a provider

1. **Catalog:** add or modify one `provider.Definition`; test lookup,
   base-URL resolution, slug-safe public ID, and prefix uniqueness.
2. **Wire adapter:** implement `entities.Upstream` plus the optional
   `credential.ConnectivityProber` and `credential.ModelDiscoverer` interfaces
   under `internal/platform/llm`. Reuse `OpenAIAdapter` or
   `AnthropicAdapter` only if the provider is genuinely wire compatible.
3. **Protocol translation:** isolate special headers, paths, body fields, token
   refresh, SSE conversion, tool calls, and error draining in the adapter.
   Determine cache behavior from the provider's current contract: automatic
   prefix caching, explicit `cache_control`, `prompt_cache_key`, or no cache
   support. Do not broadcast an unsupported cache field to every
   OpenAI-compatible endpoint.
4. **OAuth:** for OAuth/device providers, add the driver in `pkg/oauth`, bind
   cryptographic state to the initiating session, use PKCE where applicable,
   store flows in Redis in production, and persist rotated tokens encrypted.
5. **Quota:** if the provider exposes account quota, add a safe fetch/normalize
   implementation and persist snapshots. Dashboard reads the snapshot and
   contacts the provider only on explicit Reload.
6. **Registration:** wire the adapter/prober/upstream/OAuth config in
   `cmd/gorouter/main.go`. Add the right entry to the `providerProbes` and
   derived upstream maps.
7. **HTTP and UI:** ensure `/admin/providers`, connection create, health,
   discovery/import, direct chat test, quota, enable/disable, and delete
   contracts represent the feature. The React UI renders catalog metadata; it
   must never receive stored secrets.
8. **Tests:** use `httptest.Server` to assert URL, headers, typed body,
   refresh-on-401, response parsing, discovery, stream fragments, safe errors,
   cancellation, and provider cache-token aliases. Add route/browser coverage
   when the connection workflow changes.

## Model import

- Treat the provider's bounded discovery response as the current model and
  capability source; do not freeze a provider's model list in Go or React.
- Resolve the same provider-specific-or-protocol discoverer for manual import
  and scheduled catalog refresh so the two paths cannot drift.
- Decode documented snake_case/camelCase aliases into one typed metadata
  snapshot, deduplicate by stable upstream ID/slug, skip inactive credentials,
  and stamp refresh times in UTC.
- Refresh existing model metadata without replacing route ownership,
  priorities, pricing overrides, or public namespaces. A temporary discovery
  failure must not erase the last safe catalog snapshot.
- `provider.PublicModelID(providerID, upstream)` creates the stable personal or
  global ID.
- `provider.OrganizationModelID(orgName, providerID, upstream)` creates the
  organization route ID using a normalized slug.
- The credential's server-derived ownership selects which helper is legal.
- A model blend/route stack retains one public model with multiple ordered
  routes. Priority/weight and provider account health decide the selected
  original route.
- Price resolution may check the public model and upstream model, but recorded
  cost uses the resolved original model rates. Do not invent a price for an
  alias; missing pricing is explicit Free/zero.

## Connection ownership

- User API-key/OAuth connections are personal. Derive `owner_user_id` from the
  authenticated principal for self-service flows.
- Organization-owned connections are visible/routable only in that organization
  and require its object policy.
- Organization admins must not list, inspect, test, or infer a member's personal
  provider connection.
- Global/master connections are an explicit operational compatibility path,
  not the default for user-created credentials.
