# Testing Specification

## Commands

```bash
docker compose up -d
go build ./...
go vet ./...
go test ./...
```

## Unit Tests

Cover:

- Encryption round trips, wrong key, unique nonces
- API-key generation and hashing
- Master/API-key login
- Session signing, expiry, and tamper rejection
- Scope checks
- Model allowlist fail-closed behavior
- Cost calculations and missing prices
- Priority and round-robin selection
- Health cooldown
- Cache key scope and deterministic gate
- Cache TTL and size limits
- OpenAI parsing
- Anthropic translation and SSE conversion

## Integration Tests

Use real PostgreSQL and Redis where possible. Use Fiber `app.Test()` instead of starting a network listener for HTTP tests.

Required scenarios:

1. Startup migrations create all tables.
2. Master login succeeds and bypasses scopes.
3. API-key login succeeds.
4. Scoped key receives 403 for missing permissions.
5. API key sees/calls only assigned models.
6. Private credentials cannot route for another tenant.
7. OpenAI-compatible non-stream request works.
8. OpenAI-compatible SSE stream works.
9. Anthropic request/response translation works.
10. OAuth refresh-on-401 works and rotated token stays encrypted.
11. Priority routing and retry failover work.
12. Round-robin distributes requests.
13. Quota rejects before forwarding and settles actual cost.
14. RPM limit works.
15. Usage event includes token counts, cost, status, duration, and cache flag.
16. Identical deterministic request hits cache.
17. Different API keys do not share key-scoped entries.
18. Different tenants do not share tenant-scoped entries.
19. Two router instances share Redis cache.
20. Cache flush removes entries.

## Security Tests

- Search responses and logs for raw provider secrets.
- Verify database ciphertext does not contain plaintext credentials.
- Verify tampered cookies are rejected.
- Verify missing scopes return 403.
- Verify unknown models and empty allowlists fail closed.
- Verify cache keys contain no raw prompt text.

## Live Smoke Test

Start PostgreSQL, Redis, router, and mock upstream. Then:

1. Log into UI with master key.
2. Create a credential.
3. Create a model and route.
4. Set model price.
5. Create a scoped API key.
6. Call `/v1/models` with the API key.
7. Call non-streaming chat.
8. Call streaming chat.
9. Repeat deterministic chat and confirm `X-Cache: hit`.
10. Log into UI with the scoped key.
11. Confirm permitted usage access and denied credential access.
12. Restart router and verify Redis cache remains available.
