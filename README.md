# gorouter

A small, fast multi-tenant LLM gateway in Go. Connect provider credentials (API keys or
OAuth), define models routed across those credentials (priority failover or round robin),
issue per-tenant API keys with model allowlists and monthly USD quotas, and get cost
tracking plus a multi-tenant prompt cache — all exposed through an OpenAI-compatible API.

Built for the "only important features" case: no agents, combos, or CLI translation.
Reference points were CLIProxyAPI (credential handling), new-api (tenancy/billing shape)
and OmniRoute (feature set), implemented fresh in Go with Fiber v3, HTMX, Tailwind, and `pgx`.

Implementation references:

- OmniRoute: <https://github.com/diegosouzapw/OmniRoute>. Use it to compare routing, fallback, pricing, usage, and caching behavior.
- Fiber clean architecture: follow <https://docs.gofiber.io/recipes/clean-architecture/> for package boundaries.

Defined API and domain data must use typed Go structs with JSON tags. Avoid `map[string]any`
for response models when a struct can represent the response shape.

## Features

- **Multi-tenant**: tenants → API keys → model allowlists (fail-closed: a key with no models can call nothing).
- **Scoped login**: the setup master key has every permission; API keys can log into the console with explicit scopes such as `usage:read`, `keys:manage`, `credentials:manage`, `models:manage`, and `cache:purge`.
- **Credentials**: `api_key` or `oauth` kind; providers `openai-compatible` and `anthropic`.
  Secrets are sealed with AES-256-GCM before they touch Postgres.
  Anthropic OAuth credentials auto-refresh on 401 and persist rotated tokens.
- **Routing**: per-model strategy — `priority` (failover, higher priority first) or
  `round_robin`. Automatic retry across candidates on 429/5xx/transport errors with a
  simple health cooldown (3 consecutive failures → 60s cooldown).
- **Quota & cost**: monthly USD quota per key enforced pre-flight (estimate reserved,
  actuals settled after each request). Prices are per-model (input/output/cache-read/cache-write per 1M tokens).
- **Prompt cache** (multi-tenant): exact-match cache of deterministic requests
  (temperature 0 / top_p 1 / no tools), scoped per API key by default (`CACHE_SCOPE=key|tenant|global`).
  Cache hits cost $0 and are tagged in usage logs. Streaming responses are cached as
  assembled text and replayed as SSE.
- **Usage log**: every request recorded (tokens incl. provider cache read/write, cost,
  latency, status) via batched async inserts.
- **Master-key admin API + embedded UI** (login with master key or scoped API key).
- **Distributed cache**: Redis is used when `REDIS_URL` is configured; local memory cache is the development fallback.

## Quick start

```bash
docker compose --env-file .env -f docker-compose.postgres.yml up -d --build
cp .env.example .env          # edit MASTER_KEY / ENCRYPTION_KEY
make run                      # builds and starts on :8090
```

Container profiles:

```bash
cp .env.example .env
make compose-postgres
# or, for ClickHouse usage analytics:
make compose-clickhouse
```

The ClickHouse profile still includes PostgreSQL for transactional configuration. ClickHouse is not a drop-in replacement for PostgreSQL authorization/configuration queries; it is the analytics usage store profile and is initialized with `platform/database/clickhouse/001_usage_events.sql`.

Open http://localhost:8090/ and sign in with your master key.

Then point any OpenAI SDK at it:

```bash
curl http://localhost:8090/v1/chat/completions \
  -H "Authorization: Bearer nr-..." \
  -d '{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}'
```

## Configuration (env)

| Variable | Default | Notes |
|---|---|---|
| `DATABASE_URL` | – (required) | `postgres://user:pass@host:port/db` |
| `REDIS_URL` | empty | Redis URL for multi-node prompt caching; memory fallback when empty |
| `MASTER_KEY` | required during setup | admin API + UI login; has every scope |
| `ENCRYPTION_KEY` | required during setup | 32-byte base64 or any passphrase. Stored credentials cannot be decrypted without it. |
| `LISTEN` | `:8090` | |
| `CACHE_ENABLED` | `true` | |
| `CACHE_TTL` | `24h` | |
| `CACHE_SCOPE` | `key` | `key`, `tenant`, or `global` |
| `REQUEST_LIMIT_MB` | `20` | max request body |

## PostgreSQL vs ClickHouse

They are different engines, not "same query, different schema". This project uses
PostgreSQL for everything in v1 (relational config + usage events with batched inserts),
which is correct up to roughly thousands of requests/second. The usage-event insert path
is isolated in `pkg/usage` and the repository interface; at higher volume add a ClickHouse
sink behind the same interface and keep Postgres for configuration only.

## Layout

```
cmd/gorouter       server entrypoint
cmd/mock-gorouter    fake OpenAI/Anthropic upstream for local testing
pkg/config            env parsing
pkg/seal              AES-GCM secret sealing
repositories/postgres pgx repositories + migrations
platform/llm          OpenAI types, Anthropic translation (incl. SSE), upstream adapters
pkg/chat               priority / round-robin selection + health cooldown + cache port
platform/promptcache  memory and Redis prompt-cache implementations
api/handlers           Fiber delivery controllers and scoped admin API
api/views               embedded html/template + HTMX console
internal/integration  legacy integration tests against real Postgres
```

## Testing

```bash
docker compose up -d
TEST_DATABASE_URL="postgres://gorouter:change-me-postgres-password@127.0.0.1:54329/gorouter" make test
```

Covers: unit (crypto, cost, routing, cache isolation, Anthropic translation/SSE) and
integration (passthrough + cost math, SSE usage extraction, round-robin distribution,
priority failover, quota reserve/settle, cross-tenant cache isolation, OAuth refresh,
sealed-at-rest verification).

## Security notes

- Provider secrets never leave the server unencrypted; API responses only include previews.
- API keys are stored as SHA-256 hashes; plaintext shown once at creation.
- Empty model allowlist = deny all (fail-closed).
- Run behind TLS (e.g. Traefik/Caddy) in production; the server itself is plain HTTP.
