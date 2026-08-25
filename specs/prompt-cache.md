# Prompt Cache Specification

## Backend

Redis is the production cache and must be shared by all router nodes. A memory implementation may be used when `REDIS_URL` is absent for local development only.

## Cache Key

Hash the following with SHA-256:

- Scope identity
- Client-facing model
- Canonical request body

Never use raw prompt text as the Redis key.

## Scope

Supported scopes:

- `key`: default; isolated per API key
- `tenant`: shared only within one tenant
- `global`: explicitly enabled, shared globally

Cross-tenant leakage is a release-blocking security defect.

## Store Gate

Cache only deterministic requests by default:

- Temperature absent or zero
- Top-p absent or one
- `n` absent or one
- Frequency/presence penalties absent or zero
- No tools or tool calls

Cache lookup may happen before provider execution. A hit must not incur provider cost.

## Entry

Store response body, content type, status, stream-replay information, token metadata, and TTL. Enforce maximum entry size and TTL.

Streaming responses may be buffered into a replayable OpenAI response or reconstructed into valid SSE chunks. The first implementation may support text-only stream replay.

## Observability

Return `X-Cache`. Record cache hits as usage events with `cache_hit=true` and provider cost zero. Redis hit/miss/store counters should be atomic.

## Privacy

Do not store full prompts or completions outside cache entries unless explicitly configured. Cache scopes must be intentionally selected; default must remain per-key.
