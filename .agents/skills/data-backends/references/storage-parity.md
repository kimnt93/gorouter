# PostgreSQL and ClickHouse parity

## Runtime model

`cmd/gorouter/main.go` chooses exactly one complete repository set from
`cfg.DatabaseBackend`:

```text
postgresql -> internal/repositories/postgres
clickhouse -> internal/repositories/clickhouse
```

The runtime never mirrors, joins, falls back, or synchronizes across the two.
Redis is coordination/cache state, not a durable identity backend.

## Canonical locations

| Concern | PostgreSQL | ClickHouse |
|---|---|---|
| Embedded migration runner | `internal/platform/database/postgres.go` | `internal/platform/database/clickhouse.go` |
| Migrations | `internal/platform/database/migrations/*.sql` | `internal/platform/database/clickhouse/*.sql` |
| Repositories | `internal/repositories/postgres/` | `internal/repositories/clickhouse/` |
| Shared behavior contract | `internal/integration/identity_contract.go` | Invoked by both backend integration suites |

## Migration rules

- Add the next numeric version; never rewrite an applied migration to change
  deployed state.
- PostgreSQL migrations run transactionally where practical.
- ClickHouse migration files are split on semicolons by the runner; keep
  statements compatible with that behavior.
- Go generates every application record ID and UTC timestamp and inserts them
  explicitly. Do not add `SERIAL`, UUID generators, `now()`,
  `current_timestamp`, or equivalent defaults for owned fields.
- Preserve compatibility reads only where an existing migration contract
  requires them. New writes use the current organization/principal shape.

## Repository behavior

Keep both modes aligned for:

- normalization and uniqueness;
- user, organization, and membership status and last-admin rules; users have no
  persisted application role—organization authority comes from membership role;
- API-key owner/context validation and visibility;
- personal versus organization credential visibility;
- model routes, prices, and Free/zero resolution;
- usage actor snapshots, four token/cost components, activity aggregation,
  filters, cursor order, and details;
- audit ordering, visibility, and secret-safe metadata;
- cascading user deletion: invalidate authorization, remove user-owned/dependent
  API keys, personal credentials, their model routes and provider-quota
  snapshots, memberships, username lookup, and user record while retaining
  immutable usage/audit snapshots. PostgreSQL performs the mutation
  transactionally; local/ClickHouse must expose equivalent behavior.

PostgreSQL may use constraints, joins, and transactions. ClickHouse uses
append/replacing patterns and denormalized records; uniqueness-sensitive config
mutations require the Redis mutation locker in multi-writer deployments. Do not
force identical SQL or storage shape—require identical observable behavior.

## Usage writes

- `pkg/usage` owns the bounded per-process asynchronous write queue.
- Both repositories accept the same typed events and explicitly persist all
  actor, model, token, cost, cache, status, duration, and safe-error fields.
- Queue state is not an authorization, quota, or durability source of truth.
- Queries receive typed visibility/filter objects. Never concatenate raw query
  parameters into authorization-sensitive SQL.

## Verification

Run the migration ownership test, focused repository tests, and the same shared
contract against both live backends. Prove each profile starts with the other
database unavailable. For a new query, compare ordering, cursor stability,
empty/null handling, and numeric aggregation, not merely row count.
