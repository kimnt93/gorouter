# GoRouter

## Providers

GoRouter is a small multi-user LLM gateway with OpenAI-compatible APIs,
ownership-aware routing, quotas, usage attribution, pricing, and caching.
Connect accounts from `/dashboard/providers`.

| Authentication | Providers |
|---|---|
| API key | OpenAI, Anthropic, Gemini, Groq, OpenRouter, OpenCode Zen, OpenCode Go, xAI, DeepSeek, Moonshot, Qwen, OpenAI-compatible |
| OAuth | Claude Code, OpenAI Codex, GitHub Copilot, Cursor, Grok Build, xAI, Kimi Code, Cline, ClinePass, Kilo Code, Kiro, Amazon Q, Google Antigravity |

## Performance snapshot

Same-host proxy-overhead test with a 5 ms OpenAI-compatible mock; 40 requests
per target/mode/concurrency cell. See the [full dated report](docs/benchmarks/2026-09-03-local-proxy-comparison.md).

| Target | Success | Non-stream p99 @ c32 | Stream p99 @ c32 | Throughput @ c32 | Median memory | Image |
|---|---:|---:|---:|---:|---:|---:|
| GoRouter local | 240/240 | 8.015 ms | 8.863 ms | 1.69K req/s | 30.48 MiB | 28.36 MB |
| CLIProxyAPI | 240/240 | 8.363 ms | 9.597 ms | 1.65–1.66K req/s | 22.58 MiB | 75.05 MB |
| OmniRoute tested config | 55/240 | 100% timeout at c32 | 100% timeout at c32 | Not comparable | 744.20 MiB | 1.79 GB |

The OmniRoute result applies only to the tested configuration. Read the report
for methodology and limitations before quoting comparisons.

## Start with Docker

Create `.env`, generate secrets, and choose one complete backend:

```bash
cp .env.example .env
chmod 600 .env
```

Set `MASTER_KEY`, `DB_BACKEND`, `DB_CONNECTION_URL`, and Redis settings for a
distributed backend. Then start exactly one profile:

```bash
# PostgreSQL
# DB_CONNECTION_URL=postgres://user:password@postgres:5432/database?sslmode=disable
docker compose --env-file .env -f docker-compose.postgres.yml up -d --build

# ClickHouse
# DB_CONNECTION_URL=clickhouse://user:password@clickhouse:9000/database
docker compose --env-file .env -f docker-compose.clickhouse.yml up -d --build

# Local SQLite; no Redis or external database
# DB_CONNECTION_URL=file:///var/lib/gorouter/gorouter.db
docker compose --env-file .env -f docker-compose.local.yml up -d --build
```

Verify with `curl -fsS http://127.0.0.1:${ROUTER_PORT:-8090}/healthz`.
Local mode is single-process; PostgreSQL and ClickHouse modes use Redis for
shared production coordination. See [INSTALL.md](INSTALL.md) for safe upgrades.

## Coding clients

Detailed configuration and API examples are in the [integration guide](docs/integrations.md).

| Client | Status | Protocol |
|---|---|---|
| OpenCode | Supported | OpenAI-compatible `/v1/chat/completions` |
| Pi | Supported | OpenAI-compatible `/v1/chat/completions` |
| Codex CLI | Supported | OpenAI `/v1/responses` |
| OpenClaw | Supported | OpenAI-compatible `/v1/chat/completions` |
| DeepSeek Harness | Supported | OpenAI-compatible `/v1/chat/completions` |
| Hermes Agent | Supported | OpenAI-compatible `/v1/chat/completions` |
| Claude Code | Supported | Anthropic `/v1/messages` |

## Environment configuration

| Variable | Required | Description |
|---|---:|---|
| `MASTER_KEY` | Yes | Administrative login and root material; use a long random value. |
| `DB_BACKEND` | Yes | `postgresql`, `clickhouse`, or `local`. |
| `DB_CONNECTION_URL` | Yes | PostgreSQL DSN, ClickHouse DSN, or SQLite `file://` URL/path matching `DB_BACKEND`. |
| `ROUTER_PORT` | Compose only | Published HTTP port; default `8090` (`CLICKHOUSE_ROUTER_PORT` defaults to `18091`). |
| `REDIS_HOST`, `REDIS_PORT`, `REDIS_USER`, `REDIS_PASSWORD` | Distributed modes | Shared coordination connection; ignored by local mode. |
| `APP_ENV` | No | `development` by default; Compose profiles use `production`. |
| `CACHE_ENABLED`, `CACHE_TTL`, `CACHE_SCOPE` | No | Deterministic response-cache controls. |
| `REQUEST_TIMEOUT`, `REQUEST_LIMIT_MB` | No | Request duration and body-size limits. |
| `ROUTE_RETRIES` | No | Retry budget for providers without quota-aware account routing. |
| `USAGE_WRITE_CONCURRENCY`, `USAGE_WRITE_QUEUE_SIZE` | No | Asynchronous usage writer controls. |

Examples:

```env
DB_BACKEND=postgresql
DB_CONNECTION_URL=postgres://gorouter:password@postgres:5432/gorouter?sslmode=disable

DB_BACKEND=clickhouse
DB_CONNECTION_URL=clickhouse://gorouter:password@clickhouse:9000/gorouter

DB_BACKEND=local
DB_CONNECTION_URL=file:///var/lib/gorouter/gorouter.db
```

See [.env.example](.env.example) for Redis, OAuth, cache, pricing, and model-catalog options.
