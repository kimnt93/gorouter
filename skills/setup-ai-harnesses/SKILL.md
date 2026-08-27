---
name: setup-ai-harnesses
description: Configure AI coding harnesses and agents to use GoRouter through Responses, Anthropic Messages, or OpenAI Chat Completions. Use for Codex, Claude Code, OpenCode, OpenClaw, DeepSeek Harness, Hermes Agent, and generic clients.
---

# Set up AI harnesses with GoRouter

First obtain a scoped GoRouter API key and copy an exact permitted model ID
from `GET /v1/models`. Never substitute the upstream model name for the public
GoRouter model ID.

Read `references/clients.md` for the selected client and protocol. Use
`scripts/render_config.py` to generate a mergeable snippet without exposing the
key itself.

```bash
python3 scripts/render_config.py codex \
  --base-url http://127.0.0.1:8090 \
  --model cx/gpt-5.6-luna
```

The script prints to stdout by default. Use `--output` only for a new file; it
refuses to overwrite an existing file unless `--force` is explicitly supplied.
Merge generated snippets with existing client configuration instead of
discarding unrelated settings.

## Verify

1. Export `GOROUTER_API_KEY` only into the agent process environment.
2. Confirm `GET <base>/v1/models` succeeds with the key.
3. Run a single bounded prompt with a visible model.
4. For Claude Code, confirm the client calls `/v1/messages`; for Codex, confirm
   `/v1/responses`; other examples use `/v1/chat/completions`.

Do not grant full filesystem/network access unless the user explicitly wants
that trust level. The Codex template defaults to the requested always-allow,
full-access profile; warn before deploying it onto an untrusted machine or
repository.

