# Project map

Use this map to find the canonical owner of a behavior. Search the code before
assuming every package listed is involved.

## Composition and delivery

| Path | Responsibility |
|---|---|
| `cmd/gorouter/main.go` | Production composition root, backend selection, Redis wiring, provider registration, graceful shutdown |
| `cmd/mock-gorouter/` | Local/mock upstream executable used by tests and smoke work |
| `internal/api/routes/routes.go` | Fiber app construction, route registration, middleware, SPA/static delivery, Swagger |
| `internal/api/handlers/` | HTTP binding, policy boundary, typed response contracts, gateway lifecycle |
| `internal/api/presenter/` and `internal/api/response.go` | Shared OpenAI-style error and JSON response boundary |
| `internal/api/spa/` | Embedded generated React bundle |
| `internal/docs/` | Generated Swagger Go, JSON, and YAML artifacts |

## Domain and use cases

| Path | Responsibility |
|---|---|
| `pkg/entities/` | Domain types, IDs, prices/costs, identity, scopes, repository/provider ports |
| `pkg/policy/` | Centralized object-level authorization decisions |
| `pkg/auth/` | Master/API-key authentication, signed sessions, revalidation |
| `pkg/identity/` | Users, organizations, memberships, audit mutations |
| `pkg/apikey/` | API-key creation, hashing, rotation, authorization cache |
| `pkg/credential/` | Encrypted provider credential lifecycle and safe runtime projection |
| `pkg/modelroute/` | Public models, route configuration, model selection metadata |
| `pkg/chat/` | Route selection, health/cooldown, prompt-cache interfaces |
| `pkg/pricing/` | Manual/catalog price resolution and distributed invalidation |
| `pkg/quota/` | API-key quota and RPM coordination |
| `pkg/providerquota/` | Provider-account quota snapshots and active-account state |
| `pkg/usage/` | Buffered asynchronous usage writes and typed usage queries |
| `pkg/oauth/` | Provider OAuth/device flows and Redis flow storage |
| `pkg/provider/` | Static provider catalog, protocols, prefixes, public/organization model IDs |
| `pkg/seal/` | Authenticated credential encryption |

## Infrastructure and persistence

| Path | Responsibility |
|---|---|
| `internal/platform/database/` | PostgreSQL and ClickHouse connectors plus embedded migrations |
| `internal/repositories/postgres/` | Complete PostgreSQL repository set |
| `internal/repositories/clickhouse/` | Complete ClickHouse repository set and Redis mutation locking |
| `internal/platform/llm/` | Provider HTTP adapters, protocol translation, streaming, discovery |
| `internal/platform/promptcache/` | Redis/noop/development-memory deterministic response cache |
| `internal/platform/pricing/` | External pricing catalog importer |

## React dashboard and operations

| Path | Responsibility |
|---|---|
| `src/api/contracts.ts` | Browser-side API types |
| `src/api/client.ts` | Typed request, streaming, filter, and management operations |
| `src/context/SessionContext.tsx` | Current principal, scopes, memberships, View As context |
| `src/components/` | Reusable controls, dialogs, charts, token/cost presentation |
| `src/pages/` | Dashboard screens |
| `src/styles/app.css` | Current dashboard design system |
| `vite.config.ts` | Vite/Vitest config and committed SPA output path |
| `scripts/seed-smoke.mjs` | Secret-safe multi-user/multi-organization fixture generation |
| `scripts/ui-*.mjs` | Browser smoke and role/visibility checks |
| `scripts/long_context_compare.py` | Bounded local/remote long-context comparison harness |

## Trace a change

For a new persisted admin feature, inspect in this order:

```text
pkg/entities and feature interface
  -> pkg policy/service
  -> postgres and clickhouse repositories/migrations
  -> internal/api handler contract and route
  -> src API contract/client and page/component
  -> cmd/gorouter wiring
  -> unit, backend-contract, route, React, and browser tests
  -> generated Swagger and SPA artifacts
```

For a gateway behavior, start at `internal/api/handlers/gateway.go`, then trace
the model, credential, quota, cache, provider adapter, pricing, and usage ports.
