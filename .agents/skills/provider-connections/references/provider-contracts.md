# Provider runtime contracts

## Credential lifecycle

- Persist provider kind, endpoint, safe preview/status/ownership, and encrypted
  API-key or OAuth material.
- `pkg/seal` uses authenticated encryption; never persist plaintext or reuse a
  nonce.
- Convert persisted credentials to `entities.CredentialRuntime` only inside the
  credential service boundary.
- Token refresh updates the encrypted OAuth blob without losing account,
  organization, device, ID-token, or session metadata.

## Request/response rules

- Use typed structs in `internal/platform/llm/types.go` and provider-local types
  for stable shapes.
- Preserve `reasoning` as a distinct request object. Never append effort or
  summary settings to a `cx/<model>` ID.
- Set provider-specific headers only inside adapters. Never forward incoming
  browser/gateway authorization headers upstream unchanged.
- Bound model-list and error-body reads. Drain/close response bodies correctly
  so connection reuse survives concurrent load.
- Retry/fail over only eligible network failures and 408, 429, 500, 502, 503,
  or 504. Ordinary 4xx responses should not consume the next credential.

## Streaming and tools

- Translate provider frames incrementally into valid OpenAI SSE chunks.
- Preserve streamed tool/function name, ID, and argument deltas in order.
- Support assistant tool-call history and function outputs where the provider
  contract promises them.
- Treat a clean terminal event, EOF, malformed frame, timeout, and client
  cancellation separately for health and usage status.
- Never log stream content.

## Token and cache accounting

Capture independently:

```text
input tokens
output tokens
provider cache-read tokens
provider cache-write tokens
input cost
output cost
cache-read cost
cache-write cost
```

If a provider omits usage, use the bounded documented estimator. Do not label
GoRouter Redis response-cache hits as provider cache reads. A router response-
cache hit has zero provider cost and its own `cache_hit` flag.

### Provider prompt-cache behavior

- Preserve a client's valid `cache_control` or `prompt_cache_key` when the
  destination supports it. Inject hints only for known capabilities; generic
  OpenAI compatibility does not prove a provider accepts every OpenAI field.
- Keep system/developer instructions, tools, and early history byte-stable.
  Reordering, rewriting, or moving a cache breakpoint between turns can turn a
  nominally identical conversation into a provider miss.
- For Anthropic-format requests, preserve client breakpoints, enforce the
  provider limit, and add bounded automatic breakpoints only where missing.
- For routed blends, explicit `prompt_cache_key`, session, or conversation
  identity may pin a round-robin conversation to its successful credential.
  Store that affinity in Redis with key/tenant/model isolation and a bounded
  TTL; never use a broadly shared derived system-prefix hash for route pinning.
- OpenAI-style `cached_tokens` is normally a subset of total input. Store the
  uncached remainder as input and cached/created portions separately so costs
  and cache rates are not double-counted.

## Concurrency

- Reuse the shared HTTP client/transport; close or fully drain bodies.
- Avoid per-request global mutation and unbounded goroutines.
- Coordinate round robin, health bans, provider quota/exhaustion, and active
  account state through Redis in distributed deployments.
- Stress tests should measure success/error rate, latency percentiles, stream
  time-to-first-token, total duration, active connections, CPU/memory, and
  cache-token results at bounded concurrency levels.
