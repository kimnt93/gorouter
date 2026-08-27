---
name: data-backends
description: Add, change, review, or diagnose GoRouter PostgreSQL, ClickHouse, Redis, migrations, repositories, usage persistence, cache invalidation, and distributed coordination. Use for any persisted field, query, schema, multi-node state, quota/cache/routing coordination, or backend parity work. Do not use for pure HTTP or UI changes.
---

# Data backends

Read [storage-parity.md](references/storage-parity.md) for schemas and repository
workflow. Read [redis-coordination.md](references/redis-coordination.md) before
changing caches, quotas, OAuth state, routing state, or invalidation.

## Durable storage workflow

1. Start from a domain-facing interface and typed entity/query. Avoid making
   services depend on pgx or ClickHouse types.
2. Update PostgreSQL migrations/repositories and ClickHouse
   migrations/repositories for the same behavior. A process uses one complete
   backend implementation set; never dual-write or cross-read.
3. Generate IDs and UTC timestamps in Go and explicitly include them in every
   insert. Keep the ownership test passing; do not add database-generated
   identity/time defaults.
4. Use PostgreSQL transactions for compound relational changes. Design
   ClickHouse changes around its append/replacing model and distributed Redis
   mutation lock; do not imitate foreign keys with hidden PostgreSQL lookups.
5. Keep usage event columns, token/cost components, filters, ordering, cursors,
   and actor snapshots behaviorally aligned.
6. Extend the shared identity/storage contract or equivalent tests so the same
   scenario executes against both backends.

## Redis workflow

- Treat Redis as required shared coordination in production, not as a durable
  identity store.
- Use atomic scripts/transactions where quota, RPM, reservation, locking, or
  ownership invalidation requires it.
- Namespace keys with stable prefixes and all isolation dimensions.
- Permit process-local state only as documented development or Redis-error
  fallback. Never silently accept stale authorization or exceed quota because
  Redis is unavailable in strict mode.
- Invalidate API key/session/pricing/provider state on every relevant mutation.

## Verify

Run focused repository and migration tests, `internal/platform/database`'s
ownership tests, the shared PostgreSQL and ClickHouse behavior suites, and Redis
multi-node/outage tests when coordination changes. Then run `go test ./...` and
`go vet ./...`.
