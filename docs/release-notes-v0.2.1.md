# GoRouter v0.2.1 — Usage tracking and multi-select queries

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
The weekly endpoint remains self-binding; general authorized multi-agent queries
use summary, activity, and recent APIs.

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
