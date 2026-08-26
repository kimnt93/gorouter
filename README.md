# gorouter

A small, fast multi-tenant LLM gateway in Go. Connect provider credentials (API keys or
OAuth), define models routed across those credentials (priority failover or round robin),
issue per-tenant API keys with model allowlists and weekly USD limits, and get cost
tracking plus a multi-tenant prompt cache — all exposed through an OpenAI-compatible API.

Built for the "only important features" case: no agents, combos, or CLI translation.
Reference points were CLIProxyAPI (credential handling), new-api (tenancy/billing shape)
and OmniRoute (feature set), implemented fresh in Go with Fiber v3, HTMX, Tailwind, `pgx`, and the ClickHouse Go client.

Implementation references:

- OmniRoute: <https://github.com/diegosouzapw/OmniRoute>. Use it to compare routing, fallback, pricing, usage, and caching behavior.
- Fiber clean architecture: follow <https://docs.gofiber.io/recipes/clean-architecture/> for package boundaries.

Defined API and domain data must use typed Go structs with JSON tags. Avoid `map[string]any`
for response models when a struct can represent the response shape.

## Features

- **Multi-tenant**: tenants → API keys → model allowlists (fail-closed: a key with no models can call nothing).
- **Scoped login**: the setup master key has every permission; API keys can log into the console with explicit scopes such as `usage:read`, `keys:manage`, `credentials:manage`, `models:manage`, and `cache:purge`.
- **Provider dashboard**: expandable cards at `/ui/providers`, separated into OAuth
  subscriptions and API-key providers. Connect multiple accounts, test health, discover and
  import models, or run a direct streaming chat test without navigating to another page.
- **OAuth subscriptions**: guided copy/paste PKCE flows for Claude Code and OpenAI Codex.
  Flows are session-bound, single-use, and expire after 10 minutes. Access, refresh, ID,
  account, and provider metadata are encrypted before they touch the selected store; refresh-token
  rotation is persisted.
- **API-key providers**: presets for OpenAI, Anthropic, Gemini, Groq, OpenRouter,
  OpenCode Zen, xAI, DeepSeek, Moonshot, Qwen, and custom OpenAI-compatible endpoints.
- **Provider model IDs**: discovered models are imported with a provider prefix. Claude Code
  uses `cc/<model>` and Codex uses `cx/<model>`.
- **Routing**: per-model strategy — `priority` (failover, higher priority first) or
  `round_robin`. Automatic retry across candidates on 429/5xx/transport errors with a
  simple health cooldown (3 consecutive failures → 60s cooldown).
- **Quota & cost**: each derived key selects imported models and can use a weekly or no-limit
  USD quota. Weeks start Sunday by default and are configurable. Estimates are reserved in Redis before a
  request and settled against actual usage afterward. Prices are per-model
  (input/output/cache-read/cache-write per 1M tokens).
- **OpenRouter pricing**: a background job fetches the public catalog immediately and hourly,
  stores one compact row per canonical model, and atomically refreshes an in-memory resolver.
  Cached and non-cached estimates are derived from the stored rates.
- **Prompt cache** (multi-tenant): exact-match cache of deterministic requests
  (temperature 0 / top_p 1 / no tools), scoped per API key by default (`CACHE_SCOPE=key|tenant|global`).
  Cache hits cost $0 and are tagged in usage logs. Streaming responses are cached as
  assembled text and replayed as SSE.
- **Usage log**: every request recorded (tokens incl. provider cache read/write, cost,
  latency, status) through a bounded, configurable pool of batched async writers.
- **API-token cache**: Redis caches hashed token lookups with a sliding 10-minute TTL;
  storage is the fallback and mutations invalidate the corresponding cache entries.
- **Master-key admin API + embedded UI** (login with master key or scoped API key).
- **Distributed cache**: Redis uses structured `REDIS_*` settings; local memory cache is the development fallback.

## Quick start

```bash
cp .env.example .env          # edit MASTER_KEY and database/Redis passwords
docker compose up -d --build  # router + PostgreSQL + Redis
```

Container profiles:

```bash
cp .env.example .env
make compose-postgres
# or, with ClickHouse as the complete primary store:
make compose-clickhouse
```

The two profiles are standalone alternatives. PostgreSQL stores relational configuration and
usage events; ClickHouse stores configuration in versioned records and usage in a MergeTree.
The ClickHouse profile does not start or require PostgreSQL.

The process selects exactly one primary store at startup from `DB_BACKEND`; it never writes
configuration or usage to both databases. All persisted identifiers and timestamps are created
in the Go backend and passed explicitly to SQL inserts—database ID/time defaults are disabled.

Open <http://localhost:8090/> and sign in with your master key.

Then point any OpenAI SDK at it:

```bash
curl http://localhost:8090/v1/chat/completions \
  -H "Authorization: Bearer nr-..." \
  -d '{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}'
```

## Configuration (env)

| Variable | Default | Notes |
|---|---|---|
| `MASTER_KEY` | required during setup | Arbitrary random string with no required prefix; generate with `openssl rand -base64 33`. Used for admin login and internal key derivation. |
| `DB_BACKEND` | `postgresql` | Complete primary backend: `postgresql` or `clickhouse`. |
| `DB_HOST` | `127.0.0.1` | Database host; Compose uses the internal service name. |
| `DB_PORT` | `5432` | Database port. |
| `DB_USER` | required | Database user. |
| `DB_PASSWORD` | required | Database password. |
| `DB_NAME` | required | Database name. |
| `DB_SSLMODE` | `disable` | PostgreSQL TLS mode. Use `require` or a verification mode for a TLS-enabled remote database. |
| `CLICKHOUSE_HOST` | `127.0.0.1` | ClickHouse native-protocol host. |
| `CLICKHOUSE_PORT` | `9000` | ClickHouse native-protocol port. |
| `CLICKHOUSE_USER` | required in ClickHouse mode | ClickHouse user. |
| `CLICKHOUSE_PASSWORD` | required in ClickHouse mode | ClickHouse password. |
| `CLICKHOUSE_DB` | required in ClickHouse mode | ClickHouse database. |
| `CLICKHOUSE_TLS` | `false` | Enables the secure native protocol. |
| `REDIS_HOST` | empty | Redis host; Compose uses the internal service name. Empty enables the development memory fallback. |
| `REDIS_PORT` | `6379` | Redis port. |
| `REDIS_USER` | empty | Redis ACL user. |
| `REDIS_PASSWORD` | empty | Redis ACL password. |
| `LISTEN` | `:8090` | |
| `APP_ENV` | `development` | Production disables implicit memory-cache fallback. |
| `REQUEST_TIMEOUT` | `5m` | Fiber request/read timeout. |
| `CACHE_ENABLED` | `true` | |
| `CACHE_TTL` | `24h` | |
| `CACHE_SCOPE` | `key` | `key`, `tenant`, or `global` |
| `CACHE_MAX_ENTRY_BYTES` | `1048576` | Maximum cached response size. |
| `CACHE_MAX_TOTAL_BYTES` | `268435456` | Memory fallback capacity. |
| `CACHE_MEMORY_FALLBACK` | environment-dependent | Allowed only for local development. |
| `REDIS_OUTAGE_POLICY` | `strict` | `strict` fails quota/RPM closed; `open` is an explicit bypass policy. |
| `API_TOKEN_CACHE_TTL` | `10m` | Sliding Redis TTL for API-token lookups. |
| `WEEK_START` | `sunday` | Weekly-limit boundary; weekday name, three-letter name, or `0..6`. |
| `USAGE_WRITE_CONCURRENCY` | `4` | Concurrent durable usage batch writers. |
| `USAGE_WRITE_QUEUE_SIZE` | `100000` | Bounded in-memory usage-event queue. |
| `REQUEST_LIMIT_MB` | `20` | max request body |
| `ANTHROPIC_OAUTH_CLIENT_ID` | built-in client ID | OAuth refresh client ID. |
| `ANTHROPIC_OAUTH_TOKEN_URL` | Anthropic token endpoint | Optional compatible/test override. |
| `CODEX_OAUTH_CLIENT_ID` | built-in client ID | Codex browser OAuth and refresh client ID. |
| `CODEX_OAUTH_TOKEN_URL` | OpenAI token endpoint | Optional compatible/test override. |
| `OPENROUTER_CATALOG_ENABLED` | `true` | Enables startup and periodic OpenRouter catalog synchronization. |
| `OPENROUTER_CATALOG_URL` | OpenRouter frontend catalog | Optional HTTP(S) catalog override. |
| `OPENROUTER_SYNC_INTERVAL` | `1h` | Positive synchronization interval. |
| `OPENROUTER_HTTP_TIMEOUT` | `30s` | Positive catalog request timeout. |
| `ROUTER_PORT` | `8090` | Public application port, bound on all host interfaces. |

If PostgreSQL reports `password authentication failed` after `DB_PASSWORD` was changed,
the existing `postgres_data` volume still contains the password used when it was first
initialized. Either restore that password in `.env`, or, if the local database can be
discarded, recreate the stack and its volumes:

```bash
docker compose --env-file .env -f docker-compose.postgres.yml down -v
make compose-postgres
```

The `down -v` command permanently removes the local PostgreSQL and Redis data volumes.

## PostgreSQL vs ClickHouse

They are complete, mutually exclusive primary-store modes behind the same repository
interfaces. PostgreSQL uses relational tables and transactions. ClickHouse uses versioned
`ReplacingMergeTree` configuration records, tombstones for deletion, and a partitioned
`MergeTree` for usage. Choose one with `DB_BACKEND`; neither mode depends on the other.

## Layout

```
cmd/gorouter       server entrypoint
cmd/mock-gorouter    fake OpenAI/Anthropic upstream for local testing
pkg/config            env parsing
pkg/seal              AES-GCM secret sealing
repositories/postgres pgx repositories + migrations
repositories/clickhouse ClickHouse repositories for configuration and usage
platform/llm          OpenAI types, Anthropic translation (incl. SSE), upstream adapters
pkg/chat               priority / round-robin selection + health cooldown + cache port
platform/promptcache  memory and Redis prompt-cache implementations
api/handlers           Fiber delivery controllers and scoped admin API
api/views               embedded html/template + HTMX console
api/routes             Fiber app integration tests against PostgreSQL and Redis
ui                     Tailwind source; compiled CSS is embedded under api/views/static
```

## Testing

```bash
go test ./...
```

Integration tests additionally accept `TEST_DATABASE_URL` and `TEST_REDIS_URL` pointing
to dedicated, externally reachable test services. The Compose databases intentionally do
not publish host ports.

Covers: unit (crypto, cost, routing, cache isolation, Anthropic translation/SSE) and
integration (passthrough + cost math, SSE usage extraction, round-robin distribution,
priority failover, quota reserve/settle, cross-tenant cache isolation, OAuth refresh,
sealed-at-rest verification).

The administration API supports tenant, credential, API-key, model/route, price, usage,
cache, and credential-connectivity operations under `/admin`. Credential secrets are
accepted only on create/update and never returned. The embedded HTMX console exposes the
same scope-aware management flows without placing management secrets in JavaScript.

Provider connection endpoints:

| Endpoint | Purpose |
|---|---|
| `GET /admin/providers` | Safe static provider catalog. |
| `POST /admin/oauth/:provider/start` | Start a Claude Code or Codex browser flow. |
| `POST /admin/oauth/:provider/complete` | Exchange a pasted callback and create the encrypted credential. |
| `POST /admin/credentials/:id/test` | Probe credential health. |
| `GET /admin/credentials/:id/models` | Discover live upstream models. |
| `POST /admin/credentials/:id/models/import` | Import selected models and merge the credential route. |
| `POST /admin/credentials/:id/chat-tests` | Stream a direct provider chat test as SSE. |
| `GET /admin/pricing/catalog` | Search/paginate synchronized catalog prices. |
| `GET /admin/pricing/estimate` | Derive cached and non-cached estimates for token counts. |

The Codex gateway accepts reasoning separately from the model name:

```json
{
  "model": "cx/gpt-5.5",
  "reasoning": {"effort": "high", "summary": "auto"},
  "messages": [{"role": "user", "content": "hello"}]
}
```

Do not add reasoning suffixes to `cx/` model IDs. The Codex adapter translates ordinary
OpenAI function declarations, tool-call history/results, named tool choice, streamed argument
events, and non-streaming function-call output. Provider-native web/search/computer/custom/MCP
tool items, image/file input translation, and reasoning-summary events are outside the current
compatibility contract. A real subscription OAuth smoke test remains deferred.

## Security notes

- Provider secrets never leave the server unencrypted; API responses only include previews.
- API keys are stored as SHA-256 hashes; plaintext shown once at creation.
- Empty model allowlist = deny all (fail-closed).
- Run behind TLS (e.g. Traefik/Caddy) in production; the server itself is plain HTTP.
