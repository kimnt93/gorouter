#!/usr/bin/env python3
"""Render a GoRouter client configuration snippet without embedding an API key."""

from __future__ import annotations

import argparse
import json
from pathlib import Path


CLIENTS = ("codex", "claude-code", "opencode", "openclaw", "deepseek-harness", "hermes", "generic")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("client", choices=CLIENTS)
    parser.add_argument("--base-url", default="http://127.0.0.1:8090")
    parser.add_argument("--model", default="cx/gpt-5.6-luna")
    parser.add_argument("--key-env", default="GOROUTER_API_KEY")
    parser.add_argument("--output", type=Path)
    parser.add_argument("--force", action="store_true")
    return parser.parse_args()


def render(ns: argparse.Namespace) -> str:
    root = ns.base_url.rstrip("/")
    if root.endswith("/v1"):
        root = root[:-3].rstrip("/")
    api = root + "/v1"
    env = ns.key_env
    model = ns.model
    if ns.client == "codex":
        return f'''model_provider = "gorouter"
model = "{model}"
model_reasoning_effort = "medium"
model_reasoning_summary = "detailed"
hide_agent_reasoning = false
approval_policy = "never"
sandbox_mode = "danger-full-access"

[model_providers.gorouter]
name = "GoRouter"
base_url = "{api}"
env_key = "{env}"
wire_api = "responses"
'''
    if ns.client == "claude-code":
        return f'''export ANTHROPIC_BASE_URL="{root}"
export ANTHROPIC_AUTH_TOKEN="${{{env}}}"
export ANTHROPIC_MODEL="{model}"
export ANTHROPIC_DEFAULT_SONNET_MODEL="$ANTHROPIC_MODEL"
export ANTHROPIC_DEFAULT_OPUS_MODEL="$ANTHROPIC_MODEL"
export ANTHROPIC_DEFAULT_HAIKU_MODEL="$ANTHROPIC_MODEL"
'''
    if ns.client == "opencode":
        value = {"$schema": "https://opencode.ai/config.json", "provider": {"gorouter": {"npm": "@ai-sdk/openai-compatible", "name": "GoRouter", "options": {"baseURL": api, "apiKey": "{env:" + env + "}"}, "models": {model: {"name": "GoRouter model"}}}}, "model": "gorouter/" + model}
        return json.dumps(value, indent=2) + "\n"
    if ns.client == "openclaw":
        return f'''{{
  agents: {{ defaults: {{ model: {{ primary: "gorouter/{model}" }} }} }},
  models: {{ mode: "merge", providers: {{ gorouter: {{
    baseUrl: "{api}", apiKey: "${{{env}}}", api: "openai-completions",
    timeoutSeconds: 1800,
    models: [{{ id: "{model}", name: "GoRouter model" }}],
  }} }} }},
}}
'''
    if ns.client == "deepseek-harness":
        return f'''llm-pi-ai:
  providers:
    gorouter:
      apiKeyEnv: {env}
      api: openai-completions
      baseUrl: {api}
      models:
        - id: {model}
'''
    if ns.client == "hermes":
        return f'''custom:
  name: GoRouter
  api: {api}
  api_mode: chat_completions
  default_model: {model}
  model: {model}
  key_env: {env}
  request_timeout_seconds: 1800
  models:
    {model}: {{}}
'''
    return f'''export OPENAI_BASE_URL="{api}"
export OPENAI_API_KEY="${{{env}}}"
export OPENAI_MODEL="{model}"
'''


def main() -> int:
    ns = parse_args()
    content = render(ns)
    if ns.output:
        if ns.output.exists() and not ns.force:
            raise SystemExit(f"refusing to overwrite {ns.output}; pass --force explicitly")
        ns.output.parent.mkdir(parents=True, exist_ok=True)
        ns.output.write_text(content, encoding="utf-8")
    else:
        print(content, end="")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
