# Accounting v0.2.2 — implemented read slice (not release complete)

This directory records the implemented portion of the external 2026-09-14
accounting-v0.2.2 handoff. It is **not** a claim that all requirements are met.
See [ADR/status](ADR.md) and [verification/performance evidence](verification.md).
No XNOBrain source, production configuration, keys or databases were changed.

See also the [v0.2.2 release notes and usage walkthrough](../release-notes-v0.2.2.md).

## Consumer contract

Authenticated direct-object responses (no new envelope):

* `GET /admin/capabilities` reports `version: 0.2.2-dev`, the selected backend,
  `capabilities.usage_report: gorouter-usage-report-v1`, `totals_only`, and
  `user_weekly_usage: gorouter-user-usage-v1` when supported. Require individual
  flags; do not infer durable accounting from a version or report capability.
  `durable_acceptance`, `usage_receipts`, `member_allocations`, and
  `atomic_credit_counters` are **false**.
* `GET /admin/usage/summary?breakdown=none` preserves the existing summary schema
  and completion-time semantics but returns empty `by_model` / `by_key` objects
  after one totals query. Default `breakdown=all` retains old behavior. Invalid
  breakdown returns 400. Legacy summary totals include replay metadata as before.
* `GET /admin/usage/report` returns the full typed
  [synthetic fixture](report.fixture.json). Totals, groups and series reconcile
  from one bounded SQL statement, without health queries or encrypted content.
* `GET /admin/users/{id}/api-key` requires `keys:manage` plus self ownership or
  unrestricted master. Organization admins cannot inspect another user's key.
  Response: `key_id, owner_id, enabled, scopes, revision`; never key material.
  Revision fingerprints public metadata only, not secret rotation or authority.

Swagger schemas/examples are generated in `internal/docs`. To regenerate the
shared fixture: `go test ./pkg/usage -run TestReportContractFixture -update-report-fixture`.

### Reports

```http
GET /admin/usage/report?range=30d&scope=all_owned&agent_id=alpha,beta&group_by=agent&series_by=agent&bucket=day
GET /admin/usage/report?scope=personal&agent_id=alpha&conversation_id=conv_1&range=7d
GET /admin/usage/report?organization_id=org_1&group_by=user&series_by=user&range=30d
```

* Scope defaults to personal for users, organization in authorized context,
  global for unrestricted master. `all_owned` includes only the user's own
  personal and sponsored events. Explicit scopes cannot widen View As context.
* CSV/repeated filters are OR within each dimension, AND across dimensions,
  normalized/deduplicated/sorted; maximum 100 supplied values. Unknown IDs yield
  no matches. Omitted group/series dimensions mean no groups/combined series.
* `range=24h|7d|30d`, default 24h. Explicit RFC3339 `since`/`until` replace the
  corresponding preset bounds; unbounded `all` requires both bounds.
* `time_basis=accounting|completion`, default accounting; UTC half-open bounds,
  configured `WEEK_START`. Bucket is `hour|day|week|month`, default day. Returned
  series edges are clipped to the requested interval. Missing series cells are
  absent, not fabricated measurement. Empty ID + `unattributed:true` is distinct
  from a literal agent named `unattributed`.
* Maximum 5×366 days, 100 groups and series IDs, 2,000 buckets, 20,000 joint
  aggregate cells and potential series cells, 16 MiB encoded response. Excess
  returns 400 `usage_report_limit`, never silently truncated rows. Timeouts and
  storage failures return 503 `usage_unavailable`; never cached zero totals.
* Four disjoint incurred components: `total_tokens = input_tokens + output_tokens
  + cache_read_tokens + cache_write_tokens`. Reasoning is not added again.
  Provider read ratio: `cache_read_tokens / (input_tokens + cache_read_tokens)`;
  denominator zero means undefined. Router replay tokens are excluded and
  `response_cache_hits` separate; historic stored cost is preserved, not repriced.
* `coverage.state=unknown`, `freshness.state=stored_only`, `as_of` is query-start
  observation time. These report stored facts, not durable accepted work coverage.
  `measurement_unknown_requests` is explicit. No receipt/watermark is promised.
  Empty/zero reports do not establish free or fully settled inference.

The dashboard defaults to this capability-gated report with searchable agents,
manual stable/deleted agent IDs, conversation filtering, user/model/agent grouping,
stacked charts, and explicit recorded-cost/coverage presentation. Legacy activity
and health remain an explicit separate mode; they are not requested by reporting.
For independent clients, use stable client-namespaced correlation IDs to avoid
collisions within the same user. Correlation never changes principal or payer.

## Upgrade / outstanding work

* PG additive `0031_canonical_key_lookup.sql`: owner-first canonical selection
  index, including disabled keys; existing `0028` duplicate-0023 repair retained.
  Index creation uses the existing transactional runner (not CONCURRENTLY).
  Schedule appropriate write-lock window; index build cost depends on key count.
* SQLite additive `004_usage_read_projections.sql`: virtual identity/numeric/time
  columns and three indexes. Existing JSON/content retained; canonical order and
  usage timestamps use fixed precision so fractional lexical ordering is correct.
  Existing Go-written UTC records are assumed; foreign offset-formatted imports
  require validation/normalization before this cutover. File/WAL upgrade fixture
  reconciles counts, four-token sum, money, content and nanosecond boundaries.
* CH no new migration: current schema supports the read slice. FINAL canonical
  lookup and time-first raw aggregation remain physical performance limitations.

Rollback to v0.2.1 can leave additive columns/indexes in place; disable consumer
report usage first. No destructive drop, guessed owner backfill or index
materialization ran on an application database. Durable receipt/admission,
allocations, fixed-point money, crash recovery, audit/metrics and full scale
verification remain release blockers listed in the ADR.
