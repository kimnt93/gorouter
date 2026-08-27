# Agent integrations

GoRouter can be used by agents that support OpenAI-compatible Chat
Completions, OpenAI Responses, or Anthropic Messages. This guide uses a local
gateway at `http://127.0.0.1:8090` and `cx/gpt-5.6-luna` as an example. Replace
the model with an exact ID visible to your GoRouter API key.

## Compatibility

| Client | Status | GoRouter endpoint |
|---|---|---|
| Codex CLI | Supported | `POST /v1/responses` |
| OpenCode | Supported | `POST /v1/chat/completions` |
| OpenClaw | Supported | `POST /v1/chat/completions` |
| DeepSeek Harness (`dsh`) | Supported | `POST /v1/chat/completions` |
| Hermes Agent | Supported | `POST /v1/chat/completions` |
| Other OpenAI-compatible agents | Usually supported | `POST /v1/chat/completions` |
| Claude Code | Supported | `POST /v1/messages` |

## Gateway URL, API key, and model

Create an API key from **Dashboard → API keys** at
`http://127.0.0.1:8090/dashboard/keys`. Copy the plaintext key when it is
created; GoRouter does not display the full key again.

An API key is scoped to its owner and allowed models. Requests use that key's
quota and cost allocation. Do not use `MASTER_KEY` in agent configuration.

Set reusable shell variables:

```bash
export GOROUTER_BASE_URL="http://127.0.0.1:8090"
export GOROUTER_API_KEY="replace-with-a-gorouter-api-key"
export GOROUTER_MODEL="cx/gpt-5.6-luna"
```

If the agent runs in a container or on another computer, replace `127.0.0.1`
with a hostname or address that can reach GoRouter.

All protected runtime requests authenticate with a bearer token:

```http
Authorization: Bearer replace-with-a-gorouter-api-key
```

List the models visible to the key before configuring a client:

```bash
curl "$GOROUTER_BASE_URL/v1/models" \
  -H "Authorization: Bearer $GOROUTER_API_KEY"
```

Model visibility depends on the key's user or organization context and model
allowlist. Copy the returned `id` exactly, including provider and organization
prefixes such as `cx/gpt-5.6-luna` or
`acme/cx/gpt-5.6-luna`.

## Runtime endpoints

| Method | URL | Purpose |
|---|---|---|
| `GET` | `http://127.0.0.1:8090/v1/models` | Models visible to the supplied key. |
| `POST` | `http://127.0.0.1:8090/v1/chat/completions` | OpenAI-compatible chat, tools, and streaming. |
| `POST` | `http://127.0.0.1:8090/v1/responses` | OpenAI Responses compatibility used by Codex CLI. |
| `POST` | `http://127.0.0.1:8090/v1/messages` | Anthropic Messages compatibility used by Claude Code. |
| `GET` | `http://127.0.0.1:8090/healthz` | Service health. |

The base URL used by OpenAI-compatible client libraries is normally
`http://127.0.0.1:8090/v1`. Do not append `/chat/completions`, `/responses`, or `/messages`
when a client asks for a *base URL*; the client adds the endpoint path.

Test Chat Completions directly:

```bash
curl "$GOROUTER_BASE_URL/v1/chat/completions" \
  -H "Authorization: Bearer $GOROUTER_API_KEY" \
  -H "Content-Type: application/json" \
  -d "{
    \"model\": \"$GOROUTER_MODEL\",
    \"messages\": [{\"role\": \"user\", \"content\": \"Reply with: connected\"}],
    \"stream\": false
  }"
```

## Codex CLI

Codex custom model providers use the Responses API. Add this to the user-level
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
base_url = "http://127.0.0.1:8090/v1"
env_key = "GOROUTER_API_KEY"
wire_api = "responses"
```

Export the key, then run a bounded connection test:

```bash
export GOROUTER_API_KEY="replace-with-a-gorouter-api-key"
codex exec --ephemeral \
  "Reply with exactly: codex gateway test" \
  --skip-git-repo-check
```

This selects medium reasoning effort and requests detailed reasoning summaries.
`hide_agent_reasoning = false` keeps those reasoning events visible. It does
not expose private chain-of-thought. The `model` must be a GoRouter model ID
rather than only the upstream model name.

`approval_policy = "never"` together with
`sandbox_mode = "danger-full-access"` is Codex's always-allow, full-access
configuration. It gives Codex unsandboxed filesystem and network access and
allows commands without approval prompts. Use it only on a machine and in
repositories you trust.

See the official [Codex configuration
sample](https://learn.chatgpt.com/docs/config-file/config-sample) and [Codex
configuration reference](https://developers.openai.com/codex/config-reference/)
for other settings.

## OpenCode

Export `GOROUTER_API_KEY`, then add a provider to `opencode.json`:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "provider": {
    "gorouter": {
      "npm": "@ai-sdk/openai-compatible",
      "name": "GoRouter",
      "options": {
        "baseURL": "http://127.0.0.1:8090/v1",
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

The outer `gorouter/` segment is OpenCode's provider name. The remaining
`cx/gpt-5.6-luna` is the full model ID sent to GoRouter. See the [OpenCode
provider documentation](https://opencode.ai/docs/providers/) for configuration
locations and precedence.

## OpenClaw

Add a custom OpenAI-compatible provider to the OpenClaw configuration. The
following is JSON5, matching OpenClaw's documented configuration format:

```json5
{
  agents: {
    defaults: {
      model: { primary: "gorouter/cx/gpt-5.6-luna" },
      models: {
        "gorouter/cx/gpt-5.6-luna": { alias: "GoRouter GPT" },
      },
    },
  },
  models: {
    mode: "merge",
    providers: {
      gorouter: {
        baseUrl: "http://127.0.0.1:8090/v1",
        apiKey: "${GOROUTER_API_KEY}",
        api: "openai-completions",
        timeoutSeconds: 1800,
        models: [
          {
            id: "cx/gpt-5.6-luna",
            name: "GPT through GoRouter",
          },
        ],
      },
    },
  },
}
```

Then verify the registered model:

```bash
openclaw models list
openclaw models set gorouter/cx/gpt-5.6-luna
```

See OpenClaw's [custom provider
documentation](https://docs.openclaw.ai/concepts/model-providers#providers-via-modelsproviders-custombase-url)
for optional model capability and context-window declarations.

## DeepSeek Harness

DeepSeek Harness is currently a developer preview, so its configuration may
change. In the Web UI, open **Settings → Models → Add a custom provider** and
enter:

- Provider ID: `gorouter`
- Display name: `GoRouter`
- Base URL: `http://127.0.0.1:8090/v1`
- API protocol: `OpenAI Completions`
- API-key environment variable: `GOROUTER_API_KEY`
- Model ID: `cx/gpt-5.6-luna`

The equivalent `$DSH_HOME/settings.yaml` provider entry is:

```yaml
llm-pi-ai:
  providers:
    gorouter:
      apiKeyEnv: GOROUTER_API_KEY
      api: openai-completions
      baseURL: http://127.0.0.1:8090/v1
      models:
        - id: cx/gpt-5.6-luna
```

After saving, select `gorouter/cx/gpt-5.6-luna` in the model picker. See the
[DeepSeek Harness custom-provider
guide](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/user/guide/providers.md)
for the current UI and configuration contract.

## Hermes Agent

For the provider-map format in the question, replace the old OmniRoute URL and
key variable with GoRouter values:

```yaml
custom:
  name: GoRouter
  api: http://127.0.0.1:8090/v1
  api_mode: chat_completions
  default_model: cx/gpt-5.6-luna
  model: cx/gpt-5.6-luna
  key_env: GOROUTER_API_KEY
  request_timeout_seconds: 1800
  models:
    cx/gpt-5.6-luna: {}
```

Current Hermes Agent versions can define the route in `~/.hermes/config.yaml`
as a named provider:

```yaml
model:
  default: cx/gpt-5.6-luna
  provider: gorouter

providers:
  gorouter:
    name: GoRouter
    base_url: http://127.0.0.1:8090/v1
    api_mode: chat_completions
    model: cx/gpt-5.6-luna
    key_env: GOROUTER_API_KEY
    request_timeout_seconds: 1800
    models:
      cx/gpt-5.6-luna: {}
```

Export `GOROUTER_API_KEY` in the environment that starts Hermes. Keep
`api_mode: chat_completions`; `codex_responses` is only needed by clients whose
runtime specifically uses the Responses protocol.

## Other OpenAI-compatible agents

When a client supports a custom OpenAI endpoint, configure these three values:

```text
Base URL: http://127.0.0.1:8090/v1
API key:  value of GOROUTER_API_KEY
Model:    cx/gpt-5.6-luna
```

Common environment-variable conventions are:

```bash
export OPENAI_BASE_URL="http://127.0.0.1:8090/v1"
export OPENAI_API_KEY="$GOROUTER_API_KEY"
export OPENAI_MODEL="$GOROUTER_MODEL"
```

Only expose these generic `OPENAI_*` aliases to the agent process that needs
them. Keeping `GOROUTER_API_KEY` as the stored secret avoids accidentally using
the gateway key with an unrelated OpenAI client.

## Claude Code

Claude Code uses Anthropic Messages, including tools and SSE streaming. Point
it at the GoRouter host without appending `/v1`:

```bash
export ANTHROPIC_BASE_URL="http://127.0.0.1:8090"
export ANTHROPIC_AUTH_TOKEN="$GOROUTER_API_KEY"
export ANTHROPIC_MODEL="$GOROUTER_MODEL"
export ANTHROPIC_DEFAULT_SONNET_MODEL="$GOROUTER_MODEL"
export ANTHROPIC_DEFAULT_OPUS_MODEL="$GOROUTER_MODEL"
export ANTHROPIC_DEFAULT_HAIKU_MODEL="$GOROUTER_MODEL"
```

`ANTHROPIC_AUTH_TOKEN` sends the GoRouter key as bearer authentication. Avoid
setting a different `ANTHROPIC_API_KEY` in the same process.

See the [Claude Code LLM gateway
documentation](https://code.claude.com/docs/en/llm-gateway) for the upstream
protocol requirements.

## Troubleshooting

- `401 Unauthorized`: the API key is missing, invalid, disabled, or was sent
  without the `Bearer` authorization scheme.
- `403 Forbidden`: the key does not have the required capability or is not
  allowed to use the selected model or context.
- `404 unknown model`: copy the exact model ID from `/v1/models`; confirm that
  the model route is enabled and visible to this key.
- Upstream rejected the request: the selected provider connection or upstream
  model rejected the translated request. Test another visible model to
  distinguish a client connection problem from an upstream model problem.
- A client calls `/v1/v1/...`: its base URL already appends `/v1`; configure
  `http://127.0.0.1:8090` instead of a base URL ending in `/v1` for that client.
- A remote or containerized agent cannot connect: `127.0.0.1` points to the
  agent itself. Use the GoRouter host's reachable address and port.
