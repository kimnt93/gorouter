# Devin catalog and startup latency investigation — 2026-09-20

## Conclusion

At baseline `1881a87`, the GET models handler invalidated the discovery cache
on every visit, and `Send` launched `models list` before each ACP chat. Fresh
private HOME directories meant both calls paid provider-auth/catalog startup.
The IDE keeps authentication and runtime state warm; it is not an equivalent
per-request startup workload.

The candidate uses a credential/key-revision-scoped metadata cache shared by
listing and inference. Only an explicit refresh, cache miss/expiry, or health
probe performs `models list`. Cache hits never bypass ACP authentication or
model UID confirmation.

## Environment and controls

- Baseline: `1881a87`; candidate: working-tree caching changes on that revision.
- Date: 2026-09-20, Linux amd64, official Devin CLI `3000.10.31`.
- Production baseline: localhost HTTP measurement on the deployed VPN host.
- Candidate live diagnostic: local Go adapter, bounded in-memory cache, serial
  requests; no Docker/HTTP/Redis overhead included in those candidate numbers.
- Model: `swe-2`; reasoning: `medium`; streaming; identical tiny text input and
  requested short response for both chat samples. CLI owns output defaults, so
  this is not a claimed hard output-token cap.
- Bounds: two sequential chat requests, one explicit catalog load plus one cold
  chat reload; total test deadline 150 seconds. No content retained in results.
- Five warm metadata reads followed one cold catalog load. Chat order: cold
  then warm, one each. Upstream timing variation/order effects are uncontrolled.

## Evidence

| Measurement | N | Success | Time |
|---|---:|---:|---:|
| Production baseline PAT models endpoint | 1 | 200 | 2,699 ms |
| Production baseline legacy personal-key models endpoint | 1 | 401 | 1,483 ms |
| Candidate local cold catalog | 1 | 46 families | 2,601.56 ms |
| Candidate local warm catalog | 5 | 5/5 | median 0.999 ms, range 0.863–1.352 ms |
| Candidate chat, cold catalog | 1 | 200 + terminal SSE | headers 7,025 ms; first text 12,693 ms; total 12,702 ms |
| Candidate chat, warm catalog | 1 | 200 + terminal SSE | headers 5,017 ms; first text 9,979 ms; total 9,986 ms |

Warm catalog raw milliseconds: `1.151, 0.999, 0.942, 0.863, 1.352`.
No percentiles above the median, throughput, CPU/memory, load resilience or
IDE-equivalent latency claims are justified by these sample sizes. The two
chat observations are consistent with removing a discovery subprocess but do
not isolate upstream generation variability.

## Authentication evidence

The supplied legacy service key (`apk_`) and earlier legacy personal key
(`apk_user_`) both authenticated to `GET api.devin.ai/v1/sessions?limit=1`
with 200, returned 403 from `GET /v3/self`, and were rejected by the official
CLI catalog command. No Cloud session was created. This is a protocol/key-type
compatibility problem, not something a catalog cache or retry can fix.

Do not implement service-key chat by starting a billed autonomous Cloud agent
and presenting it as synchronous foundation-model inference.

## Reproduce safely

Synthetic cache/multi-node/rotation/cancellation/SSE checks:

```sh
go test ./internal/platform/llm -run 'TestDevin' -count=1
go test -race ./internal/platform/llm ./internal/platform/modeldiscovery ./pkg/credential
go test ./internal/platform/llm -run '^$' -bench '^BenchmarkDevinCatalogWarm$' -benchmem -count=5
```

For the opt-in two-turn live diagnostic, set `TEST_DEVIN_LIVE=1`,
`TEST_DEVIN_CLI_BINARY` to the verified executable, and `DEVIN_TEST_KEY` through
a protected environment/secret manager, never a literal command argument:

```sh
go test ./internal/platform/llm -run '^TestDevinLiveTimings$' -count=1 -v
```

`TEST_REDIS_URL` enables the real Redis two-adapter metadata-sharing check.
Production manual catalog refresh is authorized identically to cached listing:
`GET /admin/credentials/{id}/models?refresh=true`. No incoming cookie or provider
key is included in these commands. Sanitized original live timing output is in
`/tmp/gorouter-devin-safe-timings.txt` on the development host.

No persistent schema changes: SQLite/PostgreSQL/ClickHouse continue to own the
same encrypted credentials; Redis and local memory contain ephemeral metadata
only. Persistent ACP process/session pooling remains out of scope because
request/account/conversation isolation and token rotation need separate proof.
