# Architecture Specification

## Dependency Direction

```text
api -> pkg use cases -> pkg entities
repositories/platform -> pkg interfaces
cmd -> all concrete implementations
```

The entities package must contain no Fiber, PostgreSQL, Redis, or provider HTTP imports.

## Layers

### `pkg/entities`

Contains `Tenant`, `Credential`, `CredentialRuntime`, `ApiKey`, `ModelDef`, `ModelRoute`, `Price`, `UsageEvent`, `Session`, cache entities, scopes, errors, and ports.

### `pkg/<feature>`

Contains feature services and interfaces consumed by the service. Features include `auth`, `tenant`, `apikey`, `credential`, `modelroute`, `usage`, and `chat`.

### `repositories/postgres`

Implements repository interfaces using `pgxpool`. It may know SQL and database schema but must not contain Fiber handlers.

### `platform`

Contains PostgreSQL connection/migrations, Redis clients/cache, provider adapters, protocol translators, and external HTTP behavior.

### `api`

Contains Fiber handlers, middleware, presenters, route registration, templates, and HTMX fragments. Handlers translate HTTP input into use-case calls and translate results into JSON/HTML.

### `cmd/gorouter`

The only composition root. It loads configuration, connects dependencies, constructs repositories/services/controllers, registers routes, and handles shutdown.

## Request Flow

```text
Fiber request
  -> authentication middleware
  -> scope/model authorization
  -> prompt-cache lookup
  -> quota reservation
  -> model route selection
  -> provider adapter
  -> translation/streaming response
  -> actual usage and cost settlement
  -> asynchronous usage write
  -> optional cache store
```

## Operational Constraints

- Multiple router instances may run concurrently.
- Redis-backed state must be atomic where quotas or rate limits are involved.
- In-memory state may be used only for local health hints or development fallback.
- A provider failure must not crash the process.
- Graceful shutdown must drain usage buffers where possible.
