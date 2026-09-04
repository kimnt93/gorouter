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

The published image starts with local SQLite and no required environment
variables:

```bash
docker run -d \
  --name gorouter \
  --restart unless-stopped \
  -p 8090:8090 \
  -v gorouter_data:/var/lib/gorouter \
  ghcr.io/kimnt93/gorouter:latest
```

The built-in master key is `secret`; override it with `-e MASTER_KEY=...` before
exposing GoRouter outside a private local machine. The named volume persists
`/var/lib/gorouter/gorouter.db`.

Docker Compose also needs no `.env` file. Choose exactly one profile:

```bash
# Local SQLite; no Redis or external database
docker compose -f docker-compose.local.yml up -d --build

# PostgreSQL
docker compose -f docker-compose.postgres.yml up -d --build

# ClickHouse
docker compose -f docker-compose.clickhouse.yml up -d --build
```

All variables are optional for the default local runtime. Copy
[`.env.example`](.env.example) only when overrides are needed, and replace the
development secret/password defaults for any shared or exposed deployment.

Verify with `curl -fsS http://127.0.0.1:8090/healthz`.
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

| Variable | Required | Default | Description |
|---|---:|---|---|
| `ROUTER_PORT` | No; Compose only | `8090` | Published host port. The GoRouter process does not read it. |
| `MASTER_KEY` | No | `secret` | Administrative login and root material. Override with a long random value outside private local use. |
| `DB_BACKEND` | No | `local` | `postgresql`, `clickhouse`, or `local`. |
| `DB_CONNECTION_URL` | No | `gorouter.db` in local mode | Plain SQLite path for local mode. A matching DSN is needed after explicitly selecting a distributed backend. |
| `REDIS_HOST`, `REDIS_PORT`, `REDIS_USER`, `REDIS_PASSWORD` | No | Compose: `redis`, `6379`, `gorouter`, `secret` | Shared coordination connection; ignored by local mode. |
| `LISTEN` | No | `:8090` | Address and container port used by the GoRouter process. |
| `APP_ENV` | No | `development` | Runtime safety mode; Compose profiles use `production`. |
| `OTEL_SERVICE_NAME` | No | `gorouter` | Service identity in logs and traces. |
| `DEVELOPMENT_ENVIRONMENT` | No | `local` | Deployment label in logs and traces. |
| `LOG_LEVEL` | No | `info` | Minimum structured-log level. Set `debug` to emit successful request logs. |
| `LOG_TIME_FORMAT` | No | `rfc3339` | Also accepts `rfc3339nano`. |
| `OTEL_ENABLED` | No | `false` | Enables request tracing and OTLP export. |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | No | `grpc` | OTLP transport: `grpc` or `http`. |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | No | Empty | Collector endpoint; must be set only when OTEL is enabled. |
| `CACHE_ENABLED`, `CACHE_TTL`, `CACHE_SCOPE` | No | `true`, `24h`, `key` | Deterministic response-cache controls. |
| `REQUEST_TIMEOUT`, `REQUEST_LIMIT_MB` | No | `5m`, `20` | Request duration and body-size limits. |
| `ROUTE_RETRIES` | No | `2` | Retry budget for providers without quota-aware account routing. |
| `USAGE_WRITE_CONCURRENCY`, `USAGE_WRITE_QUEUE_SIZE` | No | `4`, `100000` | Asynchronous usage writer controls. |
| `ENABLE_STORE_COMPLLETIONS` | No | `false` | Opt in to bounded, encrypted request/completion capture for the Logs detail popup. Applies only to future requests. |

Examples:

```env
DB_BACKEND=postgresql
DB_CONNECTION_URL=postgres://gorouter:password@postgres:5432/gorouter?sslmode=disable

DB_BACKEND=clickhouse
DB_CONNECTION_URL=clickhouse://gorouter:password@clickhouse:9000/gorouter

DB_BACKEND=local
DB_CONNECTION_URL=/var/lib/gorouter/gorouter.db
```

See [.env.example](.env.example) for Redis, OAuth, cache, pricing, and model-catalog options.
