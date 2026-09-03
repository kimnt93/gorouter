# gorouter

`gorouter` is a multi-user LLM gateway with OpenAI-compatible chat APIs,
provider credential routing, scoped API keys, quotas, usage tracking, pricing,
and prompt caching.

The web dashboard includes activity analysis, request logs, provider cache-token
metrics, users, organizations, API keys, models, credentials, and audit events.

## Providers

Connect API-key or OAuth accounts from `/dashboard/providers`. Antigravity uses
Google OAuth and exposes the account's live Cloud Code model catalog.

| Authentication | Providers |
|---|---|
| API key | OpenAI, Anthropic, Gemini, Groq, OpenRouter, OpenCode Zen, OpenCode Go, xAI, DeepSeek, Moonshot, Qwen, OpenAI-compatible |
| OAuth | Claude Code, OpenAI Codex, GitHub Copilot, Cursor, Grok Build, xAI, Kimi Code, Cline, ClinePass, Kilo Code, Kiro, Amazon Q, Google Antigravity |

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

Fully local (no database or Redis service):

```bash
docker compose --env-file .env -f docker-compose.local.yml up -d --build
```

Or run the binary directly:

```bash
MASTER_KEY="$(openssl rand -base64 33)" DB_BACKEND=local go run ./cmd/gorouter
```

Local mode persists application data in `data/gorouter.db` by default and uses
process memory for cache, quota/RPM, OAuth flow, and routing coordination. It is
intended for one GoRouter process, not multiple replicas. Set `SQLITE_PATH` to
choose another database file.

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

## API endpoints

Set the gateway URL, a GoRouter API key, and a model that the key is allowed to
use:

```bash
export GOROUTER_BASE_URL=http://localhost:8090
export GOROUTER_API_KEY='replace-with-a-gorouter-key'
export GOROUTER_MODEL='cx/gpt-5.6-luna'
```

The public runtime endpoints are:

| Method | Endpoint | Description |
|---|---|---|
| `POST` | `/v1/chat/completions` | OpenAI-compatible chat completions, including streaming. |
| `POST` | `/v1/responses` | OpenAI Responses compatibility for clients such as Codex CLI. |
| `POST` | `/v1/messages` | Anthropic Messages compatibility for clients such as Claude Code. |
| `GET` | `/v1/models` | Models visible to and allowed for the supplied API key. |
| `GET` | `/healthz` | Service health check. |

Dashboard management APIs live under `/admin`. See the interactive API
documentation at `/docs` for their request and response contracts.

List the models available to a key:

```bash
curl "$GOROUTER_BASE_URL/v1/models" \
  -H "Authorization: Bearer $GOROUTER_API_KEY"
```

Create a chat completion:

```bash
curl "$GOROUTER_BASE_URL/v1/chat/completions" \
  -H "Authorization: Bearer $GOROUTER_API_KEY" \
  -H "Content-Type: application/json" \
  -d "{
    \"model\": \"$GOROUTER_MODEL\",
    \"messages\": [{\"role\": \"user\", \"content\": \"Hello\"}],
    \"stream\": false
  }"
```

## Coding clients

See the [agent integration guide](docs/integrations.md) for API-key setup,
endpoint details, and complete configurations for Codex CLI, OpenCode,
Pi, OpenClaw, DeepSeek Harness, Hermes Agent, Claude Code, and other
OpenAI-compatible clients.

| Client | Status | Required protocol |
|---|---|---|
| OpenCode | Supported | OpenAI-compatible `/v1/chat/completions` |
| Pi | Supported | OpenAI-compatible `/v1/chat/completions` |
| Codex CLI | Supported | OpenAI `/v1/responses` |
| OpenClaw | Supported | OpenAI-compatible `/v1/chat/completions` |
| DeepSeek Harness | Supported | OpenAI-compatible `/v1/chat/completions` |
| Hermes Agent | Supported | OpenAI-compatible `/v1/chat/completions` |
| Claude Code | Supported | Anthropic `/v1/messages` |

### OpenCode

Set `GOROUTER_API_KEY`, then add a GoRouter provider to `opencode.json`. Replace
the example model with one returned by `/v1/models`:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "provider": {
    "gorouter": {
      "npm": "@ai-sdk/openai-compatible",
      "name": "GoRouter",
      "options": {
        "baseURL": "http://localhost:8090/v1",
        "apiKey": "{env:GOROUTER_API_KEY}"
      },
      "models": {
        "cx/gpt-5.6-luna": {
          "name": "GPT through GoRouter"
        }
      }
    }
  },
  "model": "gorouter/cx/gpt-5.6-luna"
}
```

See the [OpenCode provider documentation](https://opencode.ai/docs/providers/)
for configuration precedence and additional provider options.

### Pi

Pi uses `~/.pi/agent/models.json` for custom OpenAI-compatible providers. Add a
GoRouter provider and keep the API key in the process environment:

```json
{
  "providers": {
    "gorouter": {
      "baseUrl": "http://localhost:8090/v1",
      "api": "openai-completions",
      "apiKey": "$GOROUTER_API_KEY",
      "models": [
        {
          "id": "cx/gpt-5.6-luna",
          "name": "GPT through GoRouter",
          "contextWindow": 128000,
          "maxTokens": 16384
        }
      ]
    }
  }
}
```

Then run `pi --model gorouter/cx/gpt-5.6-luna`. Replace the example ID with an
exact model returned by GoRouter's `/v1/models` endpoint. See the
[Pi custom-model documentation](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/models.md)
for model metadata and configuration precedence.

### Codex CLI

Current Codex custom providers use the Responses API. Choose an exact model ID
returned by GoRouter's `/v1/models` endpoint, then add this to
`~/.codex/config.toml`:

```toml
model_provider = "gorouter"
model = "cx/gpt-5.6-luna"
model_reasoning_effort = "medium"
model_reasoning_summary = "detailed"
hide_agent_reasoning = false
approval_policy = "never"
sandbox_mode = "danger-full-access"

[model_providers.gorouter]
name = "GoRouter"
base_url = "http://localhost:8090/v1"
env_key = "GOROUTER_API_KEY"
wire_api = "responses"
```

Export the key and run Codex:

```bash
export GOROUTER_API_KEY='replace-with-a-gorouter-key'
codex exec --ephemeral \
  "Reply with exactly: codex gateway test" \
  --skip-git-repo-check
```

This configuration gives Codex unsandboxed filesystem and network access and
allows commands without approval prompts. Use it only on a machine and in
repositories you trust.

See the [Codex configuration reference](https://developers.openai.com/codex/config-reference/)
for the current custom-provider contract.

### Claude Code

Claude Code connects to LLM gateways through the Anthropic Messages protocol.
Point it at the GoRouter host without appending `/v1`:

```bash
export ANTHROPIC_BASE_URL=http://localhost:8090
export ANTHROPIC_AUTH_TOKEN="$GOROUTER_API_KEY"
export ANTHROPIC_MODEL="$GOROUTER_MODEL"
```

Reusable one-time migration and client-setup skills are available under
[`skills/`](skills/). They include dry-run-first scripts for OmniRoute and
9Router migrations and mergeable configuration examples for supported agent
harnesses.

See the [Claude Code gateway documentation](https://code.claude.com/docs/en/llm-gateway)
for the current gateway requirements.

## Environment configuration

Required settings:

| Variable | Description |
|---|---|
| `MASTER_KEY` | Administrative login and root key material. Use a long random value. |
| `ROUTER_PORT` | Public Compose port; defaults to `8090`. |
| `DB_BACKEND` | Primary runtime mode: `postgresql`, `clickhouse`, or `local`. |
| `DB_USER`, `DB_PASSWORD`, `DB_NAME` | PostgreSQL connection settings (PostgreSQL mode only). |
| `SQLITE_PATH` | SQLite file path for local mode; defaults to `data/gorouter.db`. |
| `CLICKHOUSE_USER`, `CLICKHOUSE_PASSWORD`, `CLICKHOUSE_DB` | ClickHouse connection settings. |
| `REDIS_USER`, `REDIS_PASSWORD` | Redis authentication settings (PostgreSQL/ClickHouse modes only). |

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

PostgreSQL, ClickHouse, and local SQLite are standalone alternatives. The
application uses
exactly one durable backend at runtime and does not mirror data between them.
PostgreSQL and ClickHouse use Redis for distributed coordination. Local mode is
explicitly
single-process and uses in-memory cache, quota, OAuth, and routing state; that ephemeral
coordination state resets when the process restarts.
