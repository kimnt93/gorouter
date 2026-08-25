# Credentials and Routing Specification

## Credential Kinds

Supported kinds:

- `api_key`
- `oauth`

Initial providers:

- `openai-compatible`
- `anthropic`

Provider adapters must implement a common runtime interface and be selectable without changing gateway logic.

## Secret Handling

- Encrypt API keys and OAuth blobs with AES-256-GCM.
- Use a unique random nonce per encryption operation.
- Store only ciphertext in PostgreSQL.
- Never log or return raw secrets.
- OAuth refresh updates the encrypted blob.

## Model Routes

Each model has one or more enabled credential routes. A route has credential ID, priority, weight, and enabled state.

Only active credentials, enabled routes, and tenant-eligible credentials may be selected.

## Algorithms

### Priority

Sort descending by priority, with deterministic ID tie-breaking. Try candidates in order.

### Round Robin

Rotate the candidate start position for each request. Protect counters under concurrency. For multiple nodes, strict global distribution requires Redis coordination; local rotation is acceptable only as a documented best-effort mode.

## Retry

Retry the next candidate for network errors and HTTP 408, 429, 500, 502, 503, and 504. Do not retry ordinary 4xx errors.

After three consecutive failures, place a credential into a short cooldown. A successful request clears its failure state.

## OAuth

OAuth adapters must support access-token use, refresh-token use, refresh-on-401, and encrypted persistence of rotated tokens. Provider-specific OAuth behavior must remain isolated in its adapter.
