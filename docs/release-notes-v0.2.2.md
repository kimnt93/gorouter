# GoRouter v0.2.2 — Accounting reports and canonical-key reads

**Status: development preview (`0.2.2-dev`), not a completed accounting release.**
Date: 2026-09-14. These notes cover the implemented read-side changes since
v0.2.1. The broader accounting v0.2.2 requirements remain partially implemented;
see [remaining gates and design decisions](accounting-v0.2.2/ADR.md).

The runtime capability endpoint identifies this source as `0.2.2-dev`. No v0.2.2
tag, GitHub release, published image or deployment is implied by these notes.
Do not infer accounting guarantees from the version string alone.

## Added

- `GET /admin/capabilities`: selected backend and explicit accounting/query
  capability flags, with no secrets or request content.
- `GET /admin/usage/report`: combined totals, independent agent/model/user
  grouping, and hour/day/week/month series from one bounded SQL aggregate.
  Authorization and filtering occur before aggregation. Query count does not
  grow with the number of selected agents.
- `GET /admin/usage/summary?breakdown=none`: one totals aggregate, preserving the
  existing response fields with empty `by_model` and `by_key` objects. Omitted
  `breakdown` retains the v0.2.1 default breakdowns.
- `GET /admin/users/{id}/api-key`: canonical-key metadata for the owner or
  unrestricted master with `keys:manage`. Returns `key_id`, `owner_id`,
  `enabled`, `scopes` and an opaque public-metadata `revision`. No plaintext,
  hash, ciphertext or key prefix is returned. Organization administration does
  not authorize inspection of another user's personal key.
- Dashboard **Analysis → Accounting report**: searchable multi-agent selection,
  manual stable/deleted agent IDs, conversation filtering, agent/model/user
  stacked charts and grouped totals. Legacy activity/health is a separate mode.
- Typed Go/TypeScript contracts, generated Swagger, and a checked-in
  [synthetic report fixture](accounting-v0.2.2/report.fixture.json).

## Changed

- Canonical authentication delegates selection to backend queries instead of
  materializing every user's keys in the service. Selection still includes
  disabled keys and prefers personal context, then creation time and ID.
  PostgreSQL and SQLite have targeted lookup indexes. ClickHouse currently uses
  server-side filtering over current configuration records with `FINAL`; its
  physical lookup optimization remains outstanding.
- SQLite recent/detail/report/summary/weekly queries no longer call the
  ledger-wide `all()` loader. Statistics do not fetch encrypted content.
  Legacy health/activity computations read SQL-filtered metadata only.
- SQLite timestamp ordering uses fixed-width UTC sort values, preserving
  nanoseconds and avoiding variable-length RFC3339 fractional ordering errors.
- Multi-select query values are normalized, deduplicated and sorted. Existing
  OR-within/AND-between semantics and the 100-value selection bound remain.

## Usage

Use the same canonical user key used for inference. Agent, conversation, run,
parent-run, trace and client-request IDs remain correlation—not authentication,
not a payer selector, and not inference idempotency. Supply tracking headers on
Chat Completions, Responses or Messages as documented in
[usage tracking](usage-tracking.md). Metadata tracking does not require content
capture; leave `ENABLE_STORE_COMPLETIONS=false` unless explicitly needed.

The examples below are read-only. Authenticate with a Router user key carrying
`usage:read` (or an authorized management session); the metadata endpoint also
requires `keys:manage`. Organization-wide reports require authorized organization
administration. An ordinary member's organization report stays self-scoped.
The same schemas are available through `/docs`.

### 1. Check capabilities before using accounting APIs

```http
GET /admin/capabilities
```

For the complete production repository wiring in this development preview:

```json
{
  "version": "0.2.2-dev",
  "capabilities": {
    "usage_report": "gorouter-usage-report-v1",
    "user_weekly_usage": "gorouter-user-usage-v1",
    "totals_only": true,
    "canonical_key_metadata": true,
    "durable_acceptance": false,
    "usage_receipts": false,
    "member_allocations": false,
    "atomic_credit_counters": false
  }
}
```

This fragment omits the actual response's `backend` and `measurement` fields.
Require the individual features your application needs. In particular, a report
capability is not evidence that durable receipts or admission counters exist.

### 2. Read lightweight conversation totals

```http
GET /admin/usage/summary?range=all&agent_id=client-a/alpha&conversation_id=conv_1&breakdown=none
```

This retains legacy completion-time semantics and all-authorized selection.
For explicit personal scope and accounting-time semantics, use the new report:

```http
GET /admin/usage/report?scope=personal&range=30d&agent_id=client-a/alpha&conversation_id=conv_1
```

### 3. Query multiple agents and a stacked chart together

```http
GET /admin/usage/report?scope=all_owned&range=30d&agent_id=client-a/alpha,client-a/beta&group_by=agent&series_by=agent&bucket=day
```

`all_owned` includes only the caller's own personal and sponsored activity. Use
`personal` to exclude organization-charged activity. Repeating `agent_id` is
also supported. Omitted selections mean all authorized values; unknown or
foreign matching filters cannot broaden access. Use stable client-namespaced
IDs to avoid collisions between applications using the same user key.

### 4. Inspect organization-charged usage by member

```http
GET /admin/usage/report?scope=organization&organization_id=org_1&range=30d&group_by=user&series_by=user&bucket=day
```

Only the selected organization's charged events are visible to its admin, not
members' unrelated personal usage or credentials. Existing View As context
continues to narrow authority.

### 5. Set exact UTC bounds or inspect canonical-key metadata

```http
GET /admin/usage/report?scope=personal&since=2026-09-01T00:00:00Z&until=2026-09-03T00:00:00Z&time_basis=accounting&group_by=model&series_by=model&bucket=day
GET /admin/users/usr_1/api-key
```

Report bounds are half-open `[since, until)`. Explicit bounds replace the
corresponding preset bounds. The default is `range=24h` and
`time_basis=accounting`; `completion` is available explicitly. Weeks use UTC
`WEEK_START`; months use calendar boundaries, not folded weekly buckets.
The key metadata `revision` fingerprints the returned public fields; it is not
an authentication watermark and need not change when only the secret rotates.

### Interpret cost and coverage correctly

- Report token totals add four disjoint components:
  `input_tokens + cache_read_tokens + cache_write_tokens + output_tokens`.
  Reasoning subsets are not added again.
- Provider cache-read ratio is
  `cache_read_tokens / (input_tokens + cache_read_tokens)`; zero denominator
  means undefined. Router response-cache hits are separate. Replayed token
  metadata is excluded from new report incurred-token totals, while legacy
  summary behavior remains compatible.
- Missing prices retain the existing **Free/zero** policy. Stored cost is
  historical Router-priced USD, not a provider invoice; old events are not
  repriced by the report.
- Reports currently say `coverage.state=unknown` and
  `freshness.state=stored_only`. `as_of` is query observation time, not a durable
  acceptance watermark. Unknown measurement is counted explicitly. Empty or
  zero-shaped results do **not** prove that all inference was free or settled.
- Reports are uncached and must not be used as reservation authority. A report
  GET followed by inference is not an atomic credit check.

Report limits: five × 366 days, 100 groups and series IDs, 2,000 time buckets,
20,000 joint aggregate/potential series cells and 16 MiB encoded output. Limit
violations return 400 `usage_report_limit`; service failures return 503
`usage_unavailable`. No rows are silently truncated. Narrow the query rather
than treating errors as zero.

See the [full report usage contract](accounting-v0.2.2/README.md) for details.

## Upgrade and rollback

Exactly one durable backend remains selected per process; no dual-write or
fallback store is introduced.

| Backend | Additive migration | Operational impact |
|---|---|---|
| PostgreSQL | `0031_canonical_key_lookup.sql` | Canonical owner/order index; existing transactional runner, not concurrent index creation. Plan a write-lock window proportional to key count. |
| SQLite | `004_usage_read_projections.sql` | Virtual hot fields, fixed-precision time projections and three indexes. Index creation scans retained records; JSON and content remain intact. |
| ClickHouse | None for this read slice | Existing schema supports reporting; time-first usage and `FINAL` configuration reads remain. |

Do not rewrite applied migrations. PostgreSQL's duplicate-0023 repair remains
in `0028`; history, money, IDs and correlation are retained without guessed
owner backfills. SQLite's projections assume existing Go-written UTC records;
validate/normalize foreign offset-formatted imports before cutover.

Back up the selected store and schedule normal startup migrations through the
owner's upgrade workflow. Large-scale migration duration, index size and write
cost are not measured. v0.2.1 rollback can leave the additive columns/indexes
installed; first disable consumer use of the new endpoints. No production
migration or deployment was performed for this source change.

## Verification and known limitations

- Shared populated contracts ran against real disposable PostgreSQL 17,
  ClickHouse 25.8 + Redis 7.4, and file/WAL SQLite.
- Live-enabled Go suite: **675 tests/subtests passed** with two explicitly
  excluded baseline failures, both also reproduced on untouched `ae14722`:
  - `TestIdentityOrganizationAPIAndRoleMatrix` expects a forbidden secondary
    user key to be created (actual 409 `user_key_exists`).
  - `TestDistributedQuotaAndRPM` observes `200,200` instead of `200,429`.
- Default `go test ./...` passed with environment-gated integration skips.
  Vet, targeted race tests, Swagger drift and diff checks passed.
- Frontend: **18 test files / 56 tests passed**; embedded SPA regenerated.
  Chrome desktop smoke at 1440×900 verified grouped charts, agent filtering,
  no horizontal overflow and no page errors.
- SQLite 10k-event warm microbenchmarks measured means of approximately
  **0.25 ms** for populated session totals and **8.3 ms** for a 100-agent report.
  These are not p95 results, cross-backend comparisons or scale acceptance.

**Still required for the full v0.2.2 accounting handoff:** durable
accepted/running/terminal journaling and accounting-ID receipts; fixed-point
policy counters; member allocations and advisory limits; replay/crash recovery;
canonical delete/restore lifecycle and ClickHouse lookup redesign; targeted
alias resolution; receipt/allocation dashboard flows; complete upgrade
reconciliation and 1m/10m-event performance gates. Existing terminal usage writes
and budget-hold behavior are not a new durable accounting guarantee.

See [verification evidence](accounting-v0.2.2/verification.md) for exact commands,
baseline failures and experiment limitations. No real provider quota was spent.
