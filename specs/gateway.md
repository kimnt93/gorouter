# Gateway API Specification

## Endpoints

```text
GET  /healthz
GET  /v1/models
POST /v1/chat/completions
```

The API uses OpenAI-compatible JSON and Bearer API-key authentication.

For Codex subscription routes, public model IDs use `cx/<model>`. Optional reasoning controls are supplied separately:

```json
"reasoning": {"effort": "high", "summary": "auto"}
```

Reasoning settings must not be encoded into the model name.

## Model List

Return only enabled models included in the caller's API-key allowlist. Never expose unauthorized models.

## Chat Lifecycle

1. Authenticate API key.
2. Require `chat` scope.
3. Parse and validate request.
4. Require model and messages.
5. Check model allowlist.
6. Resolve model and routes.
7. Look up prompt cache.
8. Check quota and reserve estimated cost.
9. Select an eligible credential.
10. Call the provider adapter.
11. Translate response if required.
12. Stream or return response.
13. Settle actual usage and cost.
14. Record usage asynchronously.
15. Store deterministic response in cache.

## Streaming

Support valid SSE responses. Set:

```text
Content-Type: text/event-stream
Cache-Control: no-cache
X-Accel-Buffering: no
```

Provider stream formats must be converted to OpenAI chunks where necessary. The final usage must be recorded even when the provider omits usage, using a clearly documented estimate.

## Headers

Allowed response observability headers:

- `X-Cache: hit|miss|off|bypass`
- `X-Upstream-Credential` may expose only a safe internal identifier if product policy permits; never expose secrets.

## Errors

Use OpenAI-style errors with message, type, and code. Do not leak database errors, tokens, request bodies, or provider credentials.
