# Read-slice verification — 2026-09-14

## Outcome

The read slice is implemented and verified. **The complete accounting-v0.2.2
handoff is not implemented or release-ready.** See the ADR's remaining gates.
No paid inference, production migration, key rotation, release or deployment.

### Correctness

| Command / scenario | Result |
|---|---|
| `go test ./...` with no integration URLs | Passed; environment-gated integration suites skip |
| Live-enabled `go test -count=1 ./...` | Failed on two existing route tests below |
| Live-enabled Go suite excluding the two named baseline failures | **675 tests/subtests passed**, zero failures or runtime skips; two explicitly excluded tests |
| `go vet ./...` | Passed |
| `go test -race ./pkg/apikey ./pkg/usage ./internal/repositories/local ./internal/api/handlers` | Passed; usage/local/handlers rerun after final test additions |
| `npm test -- --run` | **18 files, 56 tests passed** |
| `npm run build` | Passed; embedded SPA regenerated |
| `.agents/skills/swaggo-docs/scripts/generate.sh generate` and `check` | Passed; three generated artifacts updated |
| `git diff --check` | Passed |
| `scripts/ui-accounting-smoke.mjs` | Actual local SQLite Router, synthetic populated events, Chrome 1440×900: two grouped charts, searchable agent filter returning 200, no page errors or horizontal overflow |

### Existing full-suite failures

Both reproduced unchanged from a `git archive ae14722` checkout, with fresh
per-test PostgreSQL schemas and separate Redis logical database:

1. `internal/api/routes.TestIdentityOrganizationAPIAndRoleMatrix`: the old
   fixture creates a user with an initial key then expects a secondary org key
   to succeed; actual result is 409 `user_key_exists` (v0.2.1 invariant).
2. `internal/api/routes.TestDistributedQuotaAndRPM`: the second request returns
   200 rather than expected 429. Baseline reproduces `RPM statuses=200,200`.
   This write/admission bug is not fixed by the read slice.

They are not represented as passing or silently removed. Raw synthetic test
logs: `/tmp/gorouter-accounting-full.log`,
`/tmp/gorouter-accounting-baseline-routes.log`. Final selected-suite JSONL:
`/tmp/gorouter-accounting-final-go.jsonl`.

### Disposable services

PostgreSQL `postgres:17-alpine` on loopback 25439; ClickHouse
`clickhouse/clickhouse-server:25.8-alpine` native loopback 29009;
Redis `redis:7.4-alpine` loopback 26389. Names prefixed
`gorouter-accounting-test-`, labeled `gorouter.accounting-test=true`.
Disposable containers only, no application volumes mounted. Real SQLite tests
use temporary files/WAL and one writer. The three labeled containers were stopped/removed after testing; no existing
application volumes were removed. Existing XNOBrain containers untouched.

Set only synthetic disposable `TEST_DATABASE_URL`, `TEST_CLICKHOUSE_URL`, and
`TEST_REDIS_URL` for reproduction. Never point migration-enabled tests at an
application database. The selected suite executes shared report, tracking,
identity, canonical creation/disable/rotate and org-model contracts, including
existing two-replica CH/Redis contention and fail-closed outage tests. This is
not a new accounting crash-recovery proof.

New assertions include one/multi-agent and session totals, foreign selectors,
personal/org isolation, empty results, four disjoint tokens, unknown measurement,
cache replay exclusion, reconciling groups/series, independent grouping,
month/week edges, range/cardinality rejection, safe metadata/denials, typed
consumer fixture drift, and secret/content-free report SQL. SQLite migration
upgrade preserves content/money/counts and exact fractional timestamp boundaries.

## Performance experiment (microbenchmark only)

**Claim tested:** populated file/WAL SQLite selective totals and a 100-agent
report execute without Go ledger-wide decoding. No baseline speedup or full
handoff latency acceptance claim is made.

| Environment | Value |
|---|---|
| Source | `ae14722` + read-slice worktree |
| Host | Linux amd64, Intel i7-10750H, 12 logical CPUs, ~31 GiB RAM |
| Go | 1.26.5 |
| Data | 10,000 synthetic events, 100 users × 100 agents, 30 days, no content |
| Client/cache | In-process Go repository/service, concurrency 1, no report cache, repeated warm reads |
| Sampling | 1,000 operations per case × 3 runs |
| Timing | Go benchmark average, not request percentiles |

| Populated case | Three means | Allocations/op | Bytes/op |
|---|---|---:|---:|
| One-agent/conversation totals (1 match) | 0.244 / 0.264 / 0.252 ms | 52 | 3,808 |
| 100-agent report (100 matches) | 8.168 / 8.322 / 8.305 ms | 1,346–1,347 | 412,983–418,489 |

All benchmark operations assert populated matching counts. One aggregate SQL
statement per totals/report operation, independent of selected agent count.
SQLite EXPLAIN QUERY PLAN test confirms the user/agent/accounting index is
eligible. Warm disk/page cache is uncontrolled; host runs other workloads.
No p50/p95/p99, CPU/RSS/IO, lock wait, cold-cache or concurrent-ingestion data was
collected. No PG/CH latency, projection/part/merge analysis, 1m/10m scale,
index-write comparison or <=100/300ms p95 acceptance result is claimed.

Reproduce:

```sh
go test ./internal/repositories/local -run '^$' \
  -bench BenchmarkLocalAccountingReads -benchmem -benchtime=1000x -count=3
```

Raw: `/tmp/gorouter-accounting-benchmark.log`. Future baseline comparisons must
match fixture, semantics, cache preparation and contention. Migration/index
build duration and size/write costs are **not measured**. New indexes add
storage/write work; plan an authorized maintenance window and measurement before
large deployments. No speculative PG usage indexes or CH rollups were added.

## Source-publication recheck — 2026-09-14

Before committing/pushing this development preview, documentation links were
checked and the non-live baseline rerun:

- `go test -json ./...`: 601 tests/subtests passed, zero failures, 12
  environment-gated integration skips (disposable services were already removed).
- `go vet ./...`: passed.
- `npm test -- --run`: 18 files, 56 tests passed.
- `npm run build`: passed, generated SPA unchanged.
- Swaggo drift check and `git diff --check`: passed.

This rerun does not replace the earlier real-backend evidence or resolve the two
baseline live-route failures. Source publication does not publish a release,
create a version tag, migrate a runtime or deploy an image.
