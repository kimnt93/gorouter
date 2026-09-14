# Accounting v0.2.2 implementation status and read-contract ADR

Source baseline: `ae14722`. The external accounting-v0.2.2 handoff is the target,
not evidence of shipped capabilities. No production migration or rollout is authorized.

## Decisions

* Keep one selected durable backend (PostgreSQL, ClickHouse, or local SQLite).
* Preserve existing summary envelopes/default breakdowns. `breakdown=none`
  performs only the totals aggregate.
* The new report is one SQL aggregate, not one call per agent. Aggregate cells
  form one database statement snapshot; totals/groups/series derive from those
  same cells. Health and encrypted content are never selected.
* Report authorization uses immutable usage actor snapshots and the existing
  visibility policy. Explicit `scope=personal|organization|all_owned|global`
  narrows that policy. Default is personal for users, organization in an
  authorized organization context, global for unrestricted master.
* Report time defaults to accounting time; completion time is explicit. UTC
  buckets use configured WEEK_START. Month buckets are calendar months, never
  whole weeks folded into months. Ranges and result cardinality are bounded.
* Until durable acceptance and recovery are implemented, report coverage is
  **unknown**, freshness is **stored_only**. Empty rows do not prove free or
  fully settled work. Do not advertise durable receipts/allocations prematurely.
* No cached aggregate is authority for admission. Reports are currently uncached
  (`Cache-Control: no-store`), eliminating stale authorization/watermark reuse.
* Historical money and all four token/cost components remain unchanged. Reports
  do not reprice events. Float costs remain compatibility projections, not new
  hard-credit counters. Cache-hit replay tokens are excluded from incurred-token
  report totals and counted separately as response-cache hits.

## Audit / migration inventory

| Record/index | Owner/callers | Decision and cost |
|---|---|---|
| PG usage actor/correlation/numeric columns | usage writes, summary/recent/weekly | Keep; no speculative wide/partial indexes |
| CH time-first usage MergeTree | usage writes/reports | Keep canonical raw aggregation; no retry-unsafe materialized sums |
| SQLite usage JSON + content | usage writes/detail | Retain JSON compatibility; SQL generated hot fields/indexes for filtered reads; content selected only for authorized detail |
| SQLite duplicate ts,id indexes | legacy recent | Retain until measured drop; new normalized timestamp ordering does not rely on variable RFC3339 text |
| API key ownership/creation | canonical authentication | Targeted repository lookup, disabled keys included, legacy selection order preserved |
| PG legacy workload indexes / orgmodel PK overlap | weekly, policy | No drop without EXPLAIN/write evidence |
| JSON org budget holds | orgmodel admission | Existing v0.2.1 behavior; counter/journal replacement outstanding |
| event seq/ID, tenant, workload_* | compatibility, audit/correlation | Preserve; never infer historic user attribution |

SQLite additive generated-column migration computes fixed-width UTC timestamp
sort keys from existing Go-written UTC RFC3339 values, preserving nanoseconds.
It does not rewrite IDs, money or content; indexes read existing rows once.
New code requires the new migration. Rollback to v0.2.1 can leave additive
columns/indexes installed. PG adds only the canonical-key lookup index; CH
requires no migration for this read slice. Apply only through the
selected-backend startup migration workflow after backup and owner approval;
no deployment-safe concurrent-index or 10m-row migration-cost claim is made.

## Remaining release gates

Durable accepted/running/terminal accounting journal, accounting-ID receipts,
fixed-point policy-window counters, allocation/limits API and authoritative
reservation integration, canonical tombstone/restore lifecycle, targeted alias
resolution, dashboard receipt/allocation adoption, crash/replay/Redis-loss tests, full upgrade
reconciliation and the 10k/1m/10m performance matrix remain required before the
full handoff can be declared implemented. Capability discovery must state this.


## Canonical-key scope of this change

The service no longer enumerates all keys. PG uses an owner-first B-tree; SQLite
uses an indexed expression. CH filters server-side but still reads the current
`api_key` configuration entity with FINAL. This is **not** an indexed CH canonical
reference redesign; broad physical configuration reads remain a performance gap.
Creation retains existing backend serialization. Disabled keys remain candidates.
Metadata `revision` is an opaque fingerprint of returned public fields, **not** a
secret revision or monotonic authorization watermark; rotating a secret alone
need not change it. Reads never reveal, rotate, enable or reset scopes. Key
removal/restore tombstones and atomic encrypted-secret rotation are not redesigned.

## Alternatives deferred

* Permanent canonical reference/tombstone: required for stronger delete/restore
  invariants, but must be coupled to provisioning/deletion/rotation atomically.
* Typed acceptance journal + fixed-point counter tables: required for accounting
  acknowledgements. Cannot be approximated by synchronous terminal logging or
  Redis-only state; CH crash/replay/fencing proof is outstanding.
* Materialized aggregate views: rejected until retry deduplication is proven
  before summation. Existing CH unbound retry duplicates remain a coverage risk.
* Display cache: deferred until principal/membership revisions and durable scoped
  watermarks exist. Current one-query reports do not add singleflight or caching.
