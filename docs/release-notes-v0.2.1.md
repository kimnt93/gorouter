# GoRouter v0.2.1 — User access, organization models and tracking

## User-first aliases and assignment limits (final contract)

- One canonical key per user; agent IDs are request correlation, not principals.
- One-to-one unique aliases: `org/<org>/<alias>` and `<username>/<alias>`.
- A personal alias replaces its source in listings; both remain callable by the
  owner. Recipients can call only their assigned public aliases.
- Groups are bulk-assignment packages only: no group entries in `/v1/models`,
  no group inference route and no shared group spending limit.
- Per-recipient, per-model weekly limits: **0 = unlimited (default)**.
  Different users can have different limits on the same model.
- Recipients may add a separate personal cap, never override their assigner's cap.
- Org admins and personal model owners can grant/revoke access without creating
  additional user keys. Dashboard includes organization assignment packages,
  personal aliases/sharing and self-limit controls.
- Serialized alias/source uniqueness and durable assignment budget reservations
  across PostgreSQL, ClickHouse/Redis and local SQLite.

See [user model access and REST examples](user-model-access.md). The earlier
callable-group draft was replaced before deployment to the target server.

## Added

- `X-GoRouter-Trace-Id` → persisted `trace_id` on inference usage records.
- Query filters for parent run and trace IDs, alongside agent, user, logical
  request, run, and session/conversation IDs.
- CSV and repeated-parameter multi-selection with OR within each filter and AND
  across filters; omitted selections mean all **authorized** values.
- Tracking headers documented in Swagger for Chat Completions, Responses, and
  Messages, streaming and non-streaming.
- `X-GoRouter-Request-Id` response header exposes the accepted/generated logical
  request ID. Missing trace/run/conversation IDs are not fabricated.
- Integration guide: [Usage tracking API](usage-tracking.md).

## Fixed

- PostgreSQL workload migration could be skipped because two migrations used
  version 0023. A later idempotent repair migration installs missing columns.
- PostgreSQL's empty/nil agent-filter predicate could exclude records; batch
  agent filters could select unbound records. All backends now share filter
  semantics with independent authorization constraints.
- ClickHouse usage cursor comparisons now preserve subsecond precision, avoiding
  skipped records at tied timestamps.
- Trace and existing attribution metadata are returned consistently by usage
  list/detail paths, including PostgreSQL legacy recent helpers.
- Workload-bound keys cannot broaden queries to other workload namespaces.
  Organization and user View As context constrain aggregation and detail reads.
- Tracking header strings are owned by the request's accounting state so Fiber
  buffer reuse cannot corrupt delayed streaming/queued records.

## Upgrade

Apply new migrations through the normal selected-backend startup process:
PostgreSQL 0028/0029; ClickHouse 009. SQLite stores trace IDs in its existing
JSON ledger payload and requires no new column. No extra tracking flag or
prompt/completion capture is required. Existing API paths and weekly capability
marker stay compatible; see the guide for exact defaults and query examples.

## Scope and limitations

This release adds tracking and filtering, not an exactly-once accounting engine
or strict agent budgets. Existing persistence/in-flight/failed-attempt accounting
limitations remain; a successful chat is not a durable accounting receipt.
The weekly endpoint now uses user/org authority; general authorized multi-agent
queries also use summary, activity, and recent APIs.

## Verification and delivery

- `go test ./...` and `go vet ./...`: passed.
- Race tests: handlers, usage service, entities, SQLite repositories passed.
- Shared tracking/query contract and full repository suites passed against
  disposable **PostgreSQL 17**, **ClickHouse 25.8**, and real temporary SQLite.
  No tests were skipped in the targeted PostgreSQL/ClickHouse repository runs.
- Inference tests cover all three protocol endpoints, stream/non-stream, bound
  and unbound callers, cache hits, retries, failed streams, header isolation,
  generated IDs, invalid IDs, and unchanged authenticated agent/user identity.
- Filter tests cover OR/AND selection, omitted filters, all seven requested
  dimensions, user/organization/workload isolation, weekly filters and outage,
  half-open windows, tied/subsecond cursor timestamps, and legacy metadata.
- Frontend tests: 15 files, 50 tests passed; embedded SPA regenerated.
- Swagger generation/drift and `git diff --check`: passed.

No production migration, real-provider quota, multi-replica HA test, release
publication, push, or deployment is included in this source change. The two
isolated test database containers were removed after verification; existing
application/database containers and their volumes were not modified.


### Continued v0.2.1 user-model verification

- User-first personal/group inference, grants/revocation, duplicate-key rejection,
  per-agent correlation, and no agent-ID quota bypass: unit/integration tested.
- Concurrent budget and primary-key contracts: SQLite/PostgreSQL/ClickHouse.
- Two ClickHouse repository instances sharing real Redis: serialized admission
  and Redis outage fail-closed behavior tested.
- Desktop Chrome 1440×900: organization models/limits modal, displayed shared and
  per-user limits, revocation request, no horizontal overflow or JS errors.
- Frontend: 17 files / 52 tests. Go/vet/race and Swagger checks run for this update.

### Final one-to-one alias clarification — verification

- `/v1/models` lists assigned aliases and unrenamed personal sources, never
  package names. Own raw and aliased names both work; grantees cannot use raw
  private sources or republish received aliases.
- All three inference protocols tested streaming/non-streaming with assignment
  limits, zero/unlimited, self caps, revocation, and agent-ID changes.
- Shared backend tests cover concurrent alias/source uniqueness, globally
  conflicting public names, separate user budgets, and adding a limit after
  unlimited spend. PostgreSQL, ClickHouse and SQLite suites passed.
- Final frontend: **17 test files, 52 tests**; regenerated SPA and Swagger.
- Desktop browser smoke verified the assignment-package display and revoke API.
