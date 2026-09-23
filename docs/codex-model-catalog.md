# Codex model catalog refresh

GoRouter obtains subscription model availability from the authenticated Codex
backend (`/codex/models`), not from a hardcoded list. The backend applies account
entitlements and a Codex CLI `client_version` compatibility gate.

## Delayed automatic updates

- `MODEL_CATALOG_SYNC_ENABLED=true` enables durable route synchronization.
- `MODEL_CATALOG_SYNC_INTERVAL` controls the delay (default **15 minutes**).
- Every scheduler cycle performs an upstream refresh. Redis's distributed refresh
  lock permits one replica to do the cycle while all replicas read the same
  durable model routes.
- Credential-list requests may use the credential-scoped discovery cache for up
  to `MODEL_CATALOG_CACHE_TTL` (default one hour). This no longer prevents the
  scheduler from refreshing upstream.
- Discovery failures preserve the previous last-known-good catalog. Models are
  added and removed only after successful discovery.

The compatibility fingerprint starts at Codex CLI **0.156.0**. GoRouter checks
the latest stable official `openai/codex` GitHub release at startup and at most once every twelve
hours, shares that safe version metadata through Redis in distributed mode, and
never moves below its compiled baseline. Network, malformed, draft, prerelease,
or unofficial responses retain the last safe version. A newer version changes
only catalog compatibility headers; the account's authenticated upstream response
remains authoritative for which models GoRouter imports.

This allows compatible new Codex models to appear after the normal update delay
without a GoRouter release. A future Codex wire-protocol change can still require
a reviewed adapter update.

## GPT-6 incident review — 2026-09-23

Production was configured correctly (`15m` sync, `1h` cache), but GoRouter sent
`client_version=0.153.4`. The official GPT-6 Sol metadata requires at least
`0.155.0`; the current stable Codex release is `0.156.0`. A forced refresh from
the valid account therefore still returned GPT-5.6 Luna/Sol/Terra, GPT-5.5, and
GPT-6 Astra only. `/v1/models` could not publish models absent from that account
response.

Official OpenAI documentation confirms the GPT-6 family contains Astra, Sol and
Luna: <https://developers.openai.com/api/docs/guides/latest-model/gpt-6-astra.md>.
The current official Codex source catalog contains `gpt-6-sol` and `gpt-6-luna`
with the `0.155.0` minimum compatibility version.


## CLI and compatibility update policy

Every 12 hours (and once at process startup), GoRouter checks only official
vendor metadata:

| Integration | Automatic action |
|---|---|
| Codex subscription | Refresh stable official Codex client compatibility version via `openai/codex` release metadata; share through Redis. |
| Claude Code subscription | Refresh the published `@anthropic-ai/claude-code` compatibility version via npm metadata; share through Redis. |
| Devin | Fetch the official `static.devin.ai` manifest per replica, validate exact HTTPS artifact URL and SHA-256, stage a versioned binary, run `--version`, and atomically select it for new requests. |
| Other adapters | No blind update. Their version headers describe a protocol/client implementation and require a reviewed adapter change. |

The standard image's pinned Devin binary remains the fallback. Updated binaries
are stored in the `provider_runtimes` volume for PostgreSQL/ClickHouse profiles;
local mode already persists `/var/lib/gorouter`. Downloads are size-bounded,
regular-file-only, non-world-writable, checksum-verified, and retained only for
the active version. Running requests retain the executable they started with;
new requests use the activated version. If any check/download/smoke test fails,
GoRouter logs a safe warning and continues with the last verified binary.

This auto-update mechanism never installs a new GoRouter binary, changes schemas,
or treats a newer CLI version as account authorization. Provider model discovery
and ownership policy remain authoritative.
