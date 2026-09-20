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
3. Open **Models** to refresh/import the account's live catalog.
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
- Chat re-reads the live mapping before selecting a variant. This is deliberately
  stricter than caching an inferred model-to-UID mapping across accounts.
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
