# GoRouter agent client contracts

Assume `GOROUTER_URL=http://127.0.0.1:8090`,
`GOROUTER_API_KEY` is exported, and `cx/gpt-5.6-luna` is visible to the key.

## Endpoints

| Client family | Base URL | Protocol |
|---|---|---|
| Codex CLI | `$GOROUTER_URL/v1` | OpenAI Responses: `POST /v1/responses` |
| Claude Code | `$GOROUTER_URL` | Anthropic Messages: `POST /v1/messages` |
| OpenCode, OpenClaw, DeepSeek Harness, Hermes | `$GOROUTER_URL/v1` | OpenAI Chat Completions |

Do not append an endpoint path when a client asks for a base URL. A duplicated
`/v1/v1` is a client configuration error.

## Codex CLI

Merge into `~/.codex/config.toml`:

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

This displays agent reasoning summaries, not private chain-of-thought. The
approval and sandbox settings are full access and should be used only in a
trusted environment.

## Claude Code

Claude Code uses Anthropic Messages rather than OpenAI compatibility:

```bash
export ANTHROPIC_BASE_URL="http://127.0.0.1:8090"
export ANTHROPIC_AUTH_TOKEN="$GOROUTER_API_KEY"
export ANTHROPIC_MODEL="cx/gpt-5.6-luna"
export ANTHROPIC_DEFAULT_SONNET_MODEL="$ANTHROPIC_MODEL"
export ANTHROPIC_DEFAULT_OPUS_MODEL="$ANTHROPIC_MODEL"
export ANTHROPIC_DEFAULT_HAIKU_MODEL="$ANTHROPIC_MODEL"
```

Use `ANTHROPIC_AUTH_TOKEN`, which sends bearer authentication. Do not set a
different `ANTHROPIC_API_KEY` in the same process.

## OpenCode

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
      "models": {"cx/gpt-5.6-luna": {"name": "GoRouter GPT"}}
    }
  },
  "model": "gorouter/cx/gpt-5.6-luna"
}
```

## Hermes Agent

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

## OpenClaw

Use a custom provider with base URL `$GOROUTER_URL/v1`, API
`openai-completions`, key `${GOROUTER_API_KEY}`, and public model ID
`cx/gpt-5.6-luna`.

## DeepSeek Harness

Use provider ID `gorouter`, API `openai-completions`, base URL
`$GOROUTER_URL/v1`, key environment `GOROUTER_API_KEY`, and the exact public
model ID. DeepSeek Harness is a developer preview; verify its installed schema
before replacing an existing settings file.

## Generic clients

Most OpenAI-compatible SDKs accept:

```bash
export OPENAI_BASE_URL="$GOROUTER_URL/v1"
export OPENAI_API_KEY="$GOROUTER_API_KEY"
```

Expose these generic aliases only to the intended process so the gateway key
is not accidentally sent to another OpenAI-compatible host.

