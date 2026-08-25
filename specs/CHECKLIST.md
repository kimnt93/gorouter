# Parallel Implementation Checklist

Agents may work in parallel only when their file ownership does not overlap. Each agent must read `specs/README.md` and the referenced specification first.

## Coordination Rules

- One agent owns each workstream at a time.
- Do not mark an item complete until code and tests are present.
- If an interface changes, update its specification and notify dependent workstreams.
- Rebase or reconcile before integration; never discard unrelated changes.
- Final integration agent runs the complete test and smoke-test suite.
- Before implementing a feature, compare the corresponding OmniRoute behavior and the Fiber clean-architecture guidance.
- OmniRoute reference URL: <https://github.com/diegosouzapw/OmniRoute>.
- Review all new API responses and replace avoidable `map[string]any` values with typed structs.

## Workstreams

### A. Architecture and Interfaces

- [x] Define final package boundaries from `architecture.md`.
- [x] Define domain entities and repository/service ports.
- [x] Remove duplicate or legacy active implementations after migration.
- [x] Keep Fiber dependencies out of domain/use-case packages.
- [x] Add composition-root dependency wiring.

### B. Configuration and Database

- [x] Implement required environment configuration.
- [x] Require `DATABASE_URL`, `MASTER_KEY`, and `ENCRYPTION_KEY`.
- [x] Add idempotent PostgreSQL migrations.
- [x] Add tenants, credentials, API keys, scopes, models, routes, prices, and usage tables.
- [x] Add indexes required by quota and usage queries.
- [x] Add startup database ping and migration execution.

### C. Authentication and Tenancy

- [x] Implement master-key authentication with constant-time comparison.
- [x] Implement API-key hashing and one-time plaintext response.
- [x] Implement signed HTTP-only session cookie.
- [x] Implement API-key login.
- [x] Implement scope middleware for JSON and UI routes.
- [x] Enforce tenant ownership and shared-credential rules.
- [x] Add auth and scope tests.

### D. Credentials and Providers

- [x] Implement credential CRUD without secret leakage.
- [x] Implement AES-GCM credential encryption.
- [x] Implement API-key provider runtime loading.
- [x] Implement OAuth credential runtime loading.
- [x] Implement Anthropic API-key adapter.
- [x] Implement Anthropic OAuth refresh-on-401.
- [x] Implement OpenAI-compatible adapter.
- [x] Add credential connectivity testing.
- [x] Add provider adapter tests with mock upstreams.

### E. Routing and Gateway

- [x] Implement model registry and model allowlists.
- [x] Implement priority routing.
- [x] Implement concurrent-safe round-robin routing.
- [x] Implement retry/failover for retryable errors.
- [x] Implement credential health cooldown.
- [x] Implement `/v1/models` filtering.
- [x] Implement non-streaming `/v1/chat/completions`.
- [x] Implement streaming SSE `/v1/chat/completions`.
- [x] Add OpenAI-compatible error responses.
- [x] Add request-size and timeout controls.

### F. Quota, Pricing, and Usage

- [x] Implement model price CRUD.
- [x] Implement price import abstraction for OpenRouter/LiteLLM data.
- [x] Implement actual token-cost calculation.
- [x] Implement pre-flight quota estimation.
- [x] Implement distributed quota reservation with Redis.
- [x] Implement actual-cost settlement.
- [x] Implement RPM limits with Redis for multi-node operation.
- [x] Implement asynchronous batched usage writes.
- [x] Implement summary and recent-usage queries.
- [x] Add quota, price, and usage tests.

### G. Redis Prompt Cache

- [x] Implement cache interface in the use-case/domain boundary.
- [x] Implement Redis cache backend.
- [x] Implement memory fallback only for development.
- [x] Implement key, tenant, and explicit global scopes.
- [x] Enforce default key-level isolation.
- [x] Implement deterministic-request gate.
- [x] Implement TTL and entry-size limits.
- [x] Implement cache-hit usage records with zero provider cost.
- [x] Implement streaming response replay.
- [x] Test cache sharing across two router instances.
- [x] Test cross-tenant and cross-key isolation.

### H. Fiber API and HTMX UI

- [x] Implement Fiber v3 route registration.
- [x] Implement Fiber middleware and graceful shutdown.
- [x] Embed templates and static assets through `embed.FS`.
- [x] Implement master/API-key login page.
- [x] Implement scope-aware navigation.
- [x] Implement dashboard and cache fragment refresh with HTMX.
- [x] Implement API-key management UI.
- [x] Implement credential management UI.
- [x] Implement model/routing management UI.
- [x] Implement usage UI.
- [x] Implement responsive Tailwind styling.
- [x] Avoid SPA/React state and browser-exposed management secrets.

### I. Testing and Integration

- [x] Convert active integration tests to Fiber `app.Test()`.
- [x] Add PostgreSQL test setup and cleanup.
- [x] Add Redis test setup and cleanup.
- [x] Add mock OpenAI-compatible upstream.
- [x] Add mock Anthropic upstream.
- [x] Test master login and API-key login.
- [x] Test scope denial.
- [x] Test model denial.
- [x] Test routing and failover.
- [x] Test quota and cost accounting.
- [x] Test Redis cache across instances.
- [x] Test encrypted secrets at rest.
- [x] Run `go build ./...`.
- [x] Run `go vet ./...`.
- [x] Run `go test ./...`.
- [x] Run live curl/browser smoke test.

## Final Integration

- [x] Remove dead duplicate implementations.
- [x] Confirm README matches actual runtime behavior.
- [x] Confirm `.env.example` includes all required variables.
- [x] Confirm Docker Compose starts PostgreSQL and Redis.
- [x] Confirm all secret values are absent from logs and list responses.
- [x] Confirm no unchecked TODO remains in a required feature.
