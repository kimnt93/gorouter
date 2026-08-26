# gorouter

`gorouter` is a multi-user LLM gateway with OpenAI-compatible chat APIs,
provider credential routing, scoped API keys, quotas, usage tracking, pricing,
and prompt caching.

The web dashboard includes activity analysis, request logs, provider cache-token
metrics, users, organizations, API keys, models, credentials, and audit events.

## Start with Docker

Create the environment file and replace every placeholder secret:

```bash
cp .env.example .env
openssl rand -base64 33
```

Set the generated value as `MASTER_KEY`, then start one database mode.

PostgreSQL:

```bash
docker compose --env-file .env -f docker-compose.postgres.yml up -d --build
```

ClickHouse:

```bash
docker compose --env-file .env -f docker-compose.clickhouse.yml up -d --build
```

Open <http://localhost:8090/> and sign in with `MASTER_KEY` or an enabled,
appropriately scoped API key.

Useful pages:

- `/dashboard/analysis` — filterable usage activity
- `/dashboard/logs` — request logs and token details
- `/dashboard/cache` — provider cache-read/cache-write metrics
- `/dashboard/providers` — API-key and OAuth provider connections
- `/dashboard/credentials` — connection inventory
- `/dashboard/models` — model routes and pricing
- `/dashboard/users` — users and initial login keys
- `/dashboard/organizations` — organizations and memberships
- `/dashboard/keys` — API-key policies and rotation
- `/dashboard/audit` — administrative audit events
- `/docs` — API documentation

## Environment configuration

Required settings:

| Variable | Description |
|---|---|
| `MASTER_KEY` | Administrative login and root key material. Use a long random value. |
| `ROUTER_PORT` | Public Compose port; defaults to `8090`. |
| `DB_BACKEND` | Primary storage mode: `postgresql` or `clickhouse`. |
| `DB_USER`, `DB_PASSWORD`, `DB_NAME` | PostgreSQL connection settings. |
| `CLICKHOUSE_USER`, `CLICKHOUSE_PASSWORD`, `CLICKHOUSE_DB` | ClickHouse connection settings. |
| `REDIS_USER`, `REDIS_PASSWORD` | Redis authentication settings. |

Common optional settings:

| Variable | Default | Description |
|---|---:|---|
| `LISTEN` | `:8090` | Application listen address. |
| `APP_ENV` | `development` | Runtime environment. Redis is required for every other value. |
| `CACHE_ENABLED` | `true` | Enables deterministic response caching. |
| `CACHE_TTL` | `24h` | Response-cache lifetime. |
| `CACHE_SCOPE` | `key` | Cache isolation: `key`, `tenant`, or `global`. |
| `REDIS_OUTAGE_POLICY` | `strict` | Quota/RPM behavior during Redis outages. |
| `REQUEST_TIMEOUT` | `5m` | Maximum request duration. |
| `REQUEST_LIMIT_MB` | `20` | Maximum request body size. |
| `USAGE_WRITE_CONCURRENCY` | `4` | Usage-event writer count. |
| `USAGE_WRITE_QUEUE_SIZE` | `100000` | Usage-event queue capacity. |

See [.env.example](.env.example) for every provider, OAuth, pricing-catalog,
database, Redis, cache, and runtime option.

PostgreSQL and ClickHouse are standalone alternatives. The application uses
exactly one durable backend at runtime and does not mirror data between them.
Redis coordinates distributed cache, quota, OAuth, routing-health, and pricing
invalidation state. The in-memory fallback is development-only.
