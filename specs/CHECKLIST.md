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

- [ ] Define final package boundaries from `architecture.md`.
- [ ] Define domain entities and repository/service ports.
- [ ] Remove duplicate or legacy active implementations after migration.
- [ ] Keep Fiber dependencies out of domain/use-case packages.
- [ ] Add composition-root dependency wiring.

### B. Configuration and Database

- [ ] Implement required environment configuration.
- [ ] Require `DATABASE_URL`, `MASTER_KEY`, and `ENCRYPTION_KEY`.
- [ ] Add idempotent PostgreSQL migrations.
- [ ] Add tenants, credentials, API keys, scopes, models, routes, prices, and usage tables.
- [ ] Add indexes required by quota and usage queries.
- [ ] Add startup database ping and migration execution.

### C. Authentication and Tenancy

- [ ] Implement master-key authentication with constant-time comparison.
- [ ] Implement API-key hashing and one-time plaintext response.
- [ ] Implement signed HTTP-only session cookie.
- [ ] Implement API-key login.
- [ ] Implement scope middleware for JSON and UI routes.
- [ ] Enforce tenant ownership and shared-credential rules.
- [ ] Add auth and scope tests.

### D. Credentials and Providers

- [ ] Implement credential CRUD without secret leakage.
- [ ] Implement AES-GCM credential encryption.
- [ ] Implement API-key provider runtime loading.
- [ ] Implement OAuth credential runtime loading.
- [ ] Implement Anthropic API-key adapter.
- [ ] Implement Anthropic OAuth refresh-on-401.
- [ ] Implement OpenAI-compatible adapter.
- [ ] Add credential connectivity testing.
- [ ] Add provider adapter tests with mock upstreams.

### E. Routing and Gateway

- [ ] Implement model registry and model allowlists.
- [ ] Implement priority routing.
- [ ] Implement concurrent-safe round-robin routing.
- [ ] Implement retry/failover for retryable errors.
- [ ] Implement credential health cooldown.
- [ ] Implement `/v1/models` filtering.
- [ ] Implement non-streaming `/v1/chat/completions`.
- [ ] Implement streaming SSE `/v1/chat/completions`.
- [ ] Add OpenAI-compatible error responses.
- [ ] Add request-size and timeout controls.

### F. Quota, Pricing, and Usage

- [ ] Implement model price CRUD.
- [ ] Implement price import abstraction for OpenRouter/LiteLLM data.
- [ ] Implement actual token-cost calculation.
- [ ] Implement pre-flight quota estimation.
- [ ] Implement distributed quota reservation with Redis.
- [ ] Implement actual-cost settlement.
- [ ] Implement RPM limits with Redis for multi-node operation.
- [ ] Implement asynchronous batched usage writes.
- [ ] Implement summary and recent-usage queries.
- [ ] Add quota, price, and usage tests.

### G. Redis Prompt Cache

- [ ] Implement cache interface in the use-case/domain boundary.
- [ ] Implement Redis cache backend.
- [ ] Implement memory fallback only for development.
- [ ] Implement key, tenant, and explicit global scopes.
- [ ] Enforce default key-level isolation.
- [ ] Implement deterministic-request gate.
- [ ] Implement TTL and entry-size limits.
- [ ] Implement cache-hit usage records with zero provider cost.
- [ ] Implement streaming response replay.
- [ ] Test cache sharing across two router instances.
- [ ] Test cross-tenant and cross-key isolation.

### H. Fiber API and HTMX UI

- [ ] Implement Fiber v3 route registration.
- [ ] Implement Fiber middleware and graceful shutdown.
- [ ] Embed templates and static assets through `embed.FS`.
- [ ] Implement master/API-key login page.
- [ ] Implement scope-aware navigation.
- [ ] Implement dashboard and cache fragment refresh with HTMX.
- [ ] Implement API-key management UI.
- [ ] Implement credential management UI.
- [ ] Implement model/routing management UI.
- [ ] Implement usage UI.
- [ ] Implement responsive Tailwind styling.
- [ ] Avoid SPA/React state and browser-exposed management secrets.

### I. Testing and Integration

- [ ] Convert active integration tests to Fiber `app.Test()`.
- [ ] Add PostgreSQL test setup and cleanup.
- [ ] Add Redis test setup and cleanup.
- [ ] Add mock OpenAI-compatible upstream.
- [ ] Add mock Anthropic upstream.
- [ ] Test master login and API-key login.
- [ ] Test scope denial.
- [ ] Test model denial.
- [ ] Test routing and failover.
- [ ] Test quota and cost accounting.
- [ ] Test Redis cache across instances.
- [ ] Test encrypted secrets at rest.
- [ ] Run `go build ./...`.
- [ ] Run `go vet ./...`.
- [ ] Run `go test ./...`.
- [ ] Run live curl/browser smoke test.

## Final Integration

- [ ] Remove dead duplicate implementations.
- [ ] Confirm README matches actual runtime behavior.
- [ ] Confirm `.env.example` includes all required variables.
- [ ] Confirm Docker Compose starts PostgreSQL and Redis.
- [ ] Confirm all secret values are absent from logs and list responses.
- [ ] Confirm no unchecked TODO remains in a required feature.
