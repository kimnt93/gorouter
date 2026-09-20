# Devin: live models and separate reasoning levels

GoRouter now has **one connectable Devin provider**, `devin-cli` (dashboard name
**Devin**, public prefix `dv`). The standard all-provider Docker image includes
the checksum-pinned official CLI, currently **3000.10.31**. No sidecar, host login,
shared HOME, or provider-specific image is needed.

## Connect and discover

1. Choose **Providers → Devin**.
2. Add a `cog_…` **personal access token** or a legacy `apk_user_…` key with CLI
   access. A Cloud service-user key or old Desktop/Windsurf key is not equivalent
   to a human CLI subscription. Invalid/revoked/unauthorized keys cannot be fixed
   by inventing a static model catalog.
3. Open **Models** to read/import the cached account catalog. Use **Refresh from provider** when a live reload is needed.
4. Choose a model name, then pass reasoning separately:

```json
{
  "model": "dv/gpt-5.6-luna",
  "reasoning": { "effort": "low" },
  "messages": [{ "role": "user", "content": "Hello" }]
}
```

`reasoning_effort` is also accepted by the Devin adapter for chat-completions.
Conflicting values return 400. Supported levels are returned in
`supported_reasoning_levels` by credential discovery
and `/v1/models` after import/sync; `default_reasoning_level` gives the default.
Unsupported model/effort pairs are rejected before inference. No low/medium/high
fallback is fabricated for models that do not report reasoning variants.

## What changed and why it works

The earlier implementation incorrectly assumed the summarizer's ACP
`configOptions` would contain the account model list. The **summarizer is a
special-purpose internal agent**: it omitted the model selector and accepted
session creation even when a nonexistent `--model` was supplied. Labeling its
unspecified model as Adaptive was not a verified model-selection contract.

The replacement:

- Creates a private `0700` temporary HOME/XDG directory and writes the encrypted
  repository credential into a short-lived `0600` `credentials.toml`, in the
  format the official CLI expects. `models list` does not use the environment
  variable alone; a browser login is **not** required for this API-token path.
- Runs `devin models list --format json`, consuming typed `families` and
  `variants`. This is live provider metadata, not OmniRoute's static snapshot.
- Publishes **one ID per family slug** and separates the provider-reported
  reasoning labels. Native opaque variant UIDs stay internal; GoRouter never
  constructs them from a family name or guesses that dots and hyphens match.
- Excludes fast/priority variants and the separate Fast family. Fusion lead /
  sidekick combinations are omitted because they are not a single model with
  a reasoning level. For duplicate context-window variants at the same effort,
  the smaller reported context is selected deterministically.
- Starts normal ACP with the exact selected UID using `--model`, then requires
  the model selector to confirm it before sending a prompt. There is no silent
  Adaptive or summarizer fallback.

Families and reasoning metadata can change without a GoRouter code release as
long as the installed CLI continues to support the wire format. A CLI protocol
change may still require a reviewed runtime update. Listing proves catalog
availability, **not unlimited quota or guaranteed inference entitlement**.

## Safety, distributed operation and limitations

- Tools are disabled (`disabled_tools: ["*"]`) and denied
  (`permissions.deny: ["*"]`); subagents, imported config and CLI auto-updates are
  disabled. No MCP servers, filesystem/terminal client capabilities, hooks or
  host configuration are supplied. Agent-originated client requests are denied;
  an unexpected tool update fails the turn rather than executing it in GoRouter.
- This relies on the trusted, pinned CLI honoring its configuration; a private
  HOME is **not an OS sandbox**. Run the standard non-root container. GoRouter
  does not implement autonomous coding workflows through this provider.
- Four local subprocess slots, bounded output/diagnostics, request deadlines,
  Unix process-group cancellation and directory cleanup remain enforced.
- Each call/account/node gets isolated files, removed on normal completion,
  errors and cancellation. No mutable login files or process-local entitlement
  caches are shared. The durable encrypted credential is authoritative; Redis
  caches only credential-scoped discovery metadata. The discovery cache schema
  is versioned to avoid reusing earlier placeholder catalogs.
- Discovery and chat share a bounded provider-reported mapping cache. Redis is
  used in distributed deployments; explicit local mode uses bounded memory.
  Cache identity includes the credential ID plus a digest of the current key,
  so key rotation cannot reuse an earlier credential revision. Only non-secret
  model metadata/UIDs are stored, never a token, CLI HOME, prompt or session.
  `MODEL_CATALOG_CACHE_TTL` controls its lifetime (default one hour). A cache
  outage falls back to bounded fresh discovery, not a stale local copy.
- `GET /admin/credentials/{id}/models` uses cached metadata by default;
  `?refresh=true` forces an upstream reload. Import, metadata refresh and health
  checks revalidate upstream. Concurrent cold loads coalesce locally and use a
  Redis refresh lock across replicas. Auth/permission failures invalidate the
  mapping. Every chat still authenticates ACP and confirms the selected UID;
  a cached catalog is not an authorization or quota grant.
- Chat no longer runs a separate `models list` process on a warm cache hit. It
  still starts a fresh private ACP process, so it does not have the persistent
  IDE's warm connection/startup behavior. No cross-request conversation pooling
  or saved writable CLI state is introduced.
- Text chat and incremental text/reasoning streaming are supported. Client tool
  schemas, tool history, images, reasoning summaries and multiple choices are
  rejected. Inference sampling/output defaults remain CLI-owned; this is not
  full OpenAI parameter passthrough. Usage preserves optional ACP token totals
  and cache components, otherwise uses GoRouter's existing estimator.
- Authentication, permission, quota, invalid selection, missing runtime and
  discovery timeout errors are sanitized and distinguished rather than all
  becoming an unexplained 502. Raw CLI diagnostics and credentials are never
  returned to clients or stored in application logs.

## Retired Cloud and Desktop connections

The `devin` Cloud and `devin-desktop` Connect-protobuf implementations have been
removed. They are not offered when creating connections and cannot run provider
requests. Existing credential IDs, encrypted secrets, namespaces and usage
history remain intact; no key is silently reinterpreted or moved between owners.
A dashboard notice directs operators to reconnect through **Devin** and remove
old connections from **Connection inventory** when ready.

Catalog synchronization removes routes backed by retired connections; request
handling also rejects them when synchronization is disabled. Existing
`devin-cli` credentials retain their IDs and the `dv` namespace. Sync replaces
obsolete managed variant routes with family routes after successful discovery;
custom aliases referencing removed routes may need to be updated manually.

No storage schema changes are needed: SQLite, PostgreSQL and ClickHouse retain
historical provider IDs, and the shared service rejects new retired connections.

## Verification (2026-09-20)

- Official CLI, isolated credential file: **48 live families** received;
  **46 standard families / 155 reasoning or default variants** after filtering.
- Normal ACP confirmed an exact model UID; summarizer did not expose a selector.
- One bounded request through the Go adapter using `gpt-5.6-luna` + `low`:
  **HTTP 200**, complete text response, 5 reported completion tokens. No request
  or response content, token or raw catalog is checked into the repository.
- Synthetic tests cover future model families, opaque UIDs, reasoning mapping,
  speed exclusion, malformed/oversized catalogs, authentication, denied host
  requests, cancellation/cleanup, concurrency, SSE errors and token components.

References: [CLI models](https://docs.devin.ai/cli/models),
[ACP setup](https://docs.devin.ai/cli/acp/zed),
[CLI configuration](https://docs.devin.ai/cli/reference/configuration),
[permissions](https://docs.devin.ai/cli/reference/permissions),
[authentication](https://docs.devin.ai/api-reference/authentication),
[OmniRoute catalog comparison](https://github.com/diegosouzapw/OmniRoute/blob/7a921299/open-sse/config/providers/registry/devin/catalog.ts).

## Legacy keys and measured latency (2026-09-20)

Both tested legacy `apk_user_` and `apk_` service keys returned 200 from the
read-only Cloud v1 session-list endpoint but were rejected by official CLI
model discovery. They are valid for that legacy Cloud API, **not proven usable
for CLI inference**. Cloud v3 `/self` returned 403 for those legacy keys.

GoRouter now rejects `apk_` service keys with an explicit Cloud-versus-CLI
message. A rejected `apk_user_` key returns actionable 401 guidance rather than
calling it universally invalid. Use a current `cog_` **personal access token**
with CLI access. Do not assume a `cog_` service-user token has human model access.
No Cloud agent session is started as a fallback for an LLM request.

A bounded local test with the same official CLI and `swe-2` / `medium`:

| Path | Samples | Observed latency |
|---|---:|---:|
| Cold catalog through Go adapter | 1 | 2,601.56 ms |
| Warm catalog through Go adapter (memory) | 5 | 0.863–1.352 ms; median 0.999 ms |
| Cold chat, time to first text / completion | 1 | 12,693 / 12,702 ms |
| Warm-catalog chat, time to first text / completion | 1 | 9,979 / 9,986 ms |

These are diagnostic samples, not a general speed guarantee. Redis, HTTP and
DB overhead are excluded from adapter catalog timings. Warm chat still incurs
ACP startup/authentication, provider latency and generation. See the
[latency report](devin-latency-report.md) for controls and reproduction.
