# Local proxy comparison — 2026-09-03

## Summary

This benchmark compared GoRouter local mode (`SQLite + in-memory coordination`),
OmniRoute 3.8.50-fixed, and CLIProxyAPI 7.2.147 against the same isolated
OpenAI-compatible mock upstream with a fixed 5 ms delay.

GoRouter local and CLIProxyAPI both completed all 240 measured requests. Their
latency and throughput were close: GoRouter showed modestly better streaming
p95/p99 latency at concurrency 8 and 32, while CLIProxyAPI led some p50, TTFB,
and throughput cells. The tested OmniRoute configuration was materially slower
and timed out under concurrency; this is a result for that configuration, not a
claim about every possible OmniRoute deployment.

## Environment

| Field | Value |
|---|---|
| GoRouter revision | `3bdd243` |
| Host | Linux 6.18.7, x86-64, 24 logical CPUs, 31,190 MiB RAM |
| GoRouter mode | Local: SQLite durability and process-memory coordination |
| Targets | Isolated same-host Docker containers |
| Upstream | One OpenAI-compatible mock, 5 ms fixed response delay |
| Workload | Short chat request, maximum 16 output tokens |
| Modes | Non-streaming and streaming |
| Concurrency | 1, 8, 32 |
| Samples | 40 requests per target/mode/concurrency cell; 720 total |
| Repetitions | Two measured batches of 20 after five warm-up calls |
| Client timeout | 2 seconds |
| Cache | GoRouter response cache disabled; no cache result interpreted |

All three targets passed a correctness probe before measurement. Invalid or
timed-out responses were counted as failures, not latency samples.

## Latency and throughput

### Non-streaming

| Target | Concurrency | Success | Error rate | p50 | p95 | p99 | Median req/s |
|---|---:|---:|---:|---:|---:|---:|---:|
| GoRouter local | 1 | 40/40 | 0% | 5.707 ms | 6.306 ms | 6.491 ms | 170.3 |
| CLIProxyAPI | 1 | 40/40 | 0% | 5.696 ms | 6.383 ms | 6.507 ms | 171.6 |
| OmniRoute | 1 | 36/40 | 10% | 349.832 ms | 409.261 ms | 409.509 ms | 2.2 |
| GoRouter local | 8 | 40/40 | 0% | 7.201 ms | 8.313 ms | 8.570 ms | 870.0 |
| CLIProxyAPI | 8 | 40/40 | 0% | 7.053 ms | 8.221 ms | 8.698 ms | 905.6 |
| OmniRoute | 8 | 0/40 | 100% | — | — | — | 3.3 attempted |
| GoRouter local | 32 | 40/40 | 0% | 7.191 ms | 7.833 ms | 8.015 ms | 1,697.2 |
| CLIProxyAPI | 32 | 40/40 | 0% | 7.220 ms | 8.098 ms | 8.363 ms | 1,647.6 |
| OmniRoute | 32 | 0/40 | 100% | — | — | — | 9.9 attempted |

### Streaming

| Target | Concurrency | Success | Error rate | p50 | p95 | p99 | TTFB p50 | Median req/s |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| GoRouter local | 1 | 40/40 | 0% | 5.762 ms | 6.028 ms | 6.676 ms | 5.706 ms | 170.5 |
| CLIProxyAPI | 1 | 40/40 | 0% | 5.896 ms | 6.477 ms | 6.753 ms | 5.791 ms | 165.5 |
| OmniRoute | 1 | 19/40 | 52.5% | 350.091 ms | 1,533.551 ms | 1,533.551 ms | 348.526 ms | 0.7 |
| GoRouter local | 8 | 40/40 | 0% | 7.492 ms | 8.398 ms | 8.874 ms | 7.042 ms | 863.7 |
| CLIProxyAPI | 8 | 40/40 | 0% | 7.519 ms | 8.830 ms | 9.332 ms | 7.116 ms | 852.8 |
| OmniRoute | 8 | 0/40 | 100% | — | — | — | — | 3.3 attempted |
| GoRouter local | 32 | 40/40 | 0% | 7.809 ms | 8.503 ms | 8.863 ms | 7.429 ms | 1,694.9 |
| CLIProxyAPI | 32 | 40/40 | 0% | 7.501 ms | 8.932 ms | 9.597 ms | 7.006 ms | 1,663.8 |
| OmniRoute | 32 | 0/40 | 100% | — | — | — | — | 5.0 attempted |

## Resource observations

Resource sampling ran once per second during the mixed benchmark interval.
Because targets ran sequential batches on the same host, these figures are
useful operational observations rather than per-cell CPU attribution.

| Target | Memory median | Memory peak | CPU median | CPU peak | Peak PIDs |
|---|---:|---:|---:|---:|---:|
| GoRouter local | 30.48 MiB | 34.73 MiB | 0.09% | 2.07% | 16 |
| CLIProxyAPI | 22.58 MiB | 25.94 MiB | 0.00% | 3.32% | 24 |
| OmniRoute | 744.20 MiB | 815.70 MiB | 23.24% | 96.25% | 22 |

| Image | Size |
|---|---:|
| GoRouter | 28.36 MB |
| CLIProxyAPI | 75.05 MB |
| OmniRoute | 1.79 GB |

## Interpretation

### Supported observations

- GoRouter local and CLIProxyAPI were effectively tied for raw proxy latency.
- Both had zero errors across 240 measured requests each.
- At concurrency 32, GoRouter non-stream throughput was about 3% higher and its
  non-stream p99 was about 4% lower than CLIProxyAPI in this run.
- At concurrency 32, GoRouter stream p95 was about 5% lower and p99 about 8%
  lower, while CLIProxyAPI had slightly better p50 and TTFB.
- GoRouter's image was about 2.6 times smaller than CLIProxyAPI's and about 63
  times smaller than OmniRoute's.
- CLIProxyAPI used less memory than GoRouter local in this raw-proxy profile.
- OmniRoute showed additional routing/session/credential middleware work and
  timeout accumulation in this isolated configuration.

### Limitations

- The mock emitted stream chunks immediately after the 5 ms delay; this is not
  a sustained long-lived stream test.
- GoRouter retained SQLite usage persistence and its management runtime, while
  CLIProxyAPI had usage statistics disabled for the proxy-overhead profile.
- The 2-second timeout intentionally bounded OmniRoute stalls. Throughput shown
  for failed OmniRoute cells is attempted throughput, not successful throughput.
- The test does not compare real-model quality, provider-side generation speed,
  provider prompt caching, or real quota behavior.
- Preliminary larger runs were stopped after OmniRoute accumulated long-lived
  requests; incomplete preliminary data was excluded from the tables.

## Recommended follow-up

1. Run 1,000 requests per cell and five repetitions for GoRouter versus
   CLIProxyAPI at concurrency 1, 8, 32, 64, and 128.
2. Add slow streaming: 25 ms TTFB, 32 chunks, and 10 ms between chunks.
3. Compare a raw-proxy profile and a feature-equivalent production profile.
4. Stress SQLite usage writes while querying logs and verify restart durability.
5. Test deterministic failover: account 1 returns 429, account 2 returns 500,
   and account 3 succeeds; verify attempts, checkpoint, and terminal status.
6. Keep real OpenCode Zen/Luna tests separate because provider variance
   dominates gateway overhead.

## Reproduction and artifacts

The run used isolated containers on one Docker bridge and temporary loopback
ports. Benchmark-only containers and network were removed afterward; production
GoRouter remained healthy. Raw sanitized artifacts were retained on the test
host with mode `0600`:

```text
/tmp/gorouter-benchmark-20260903/proxy-comparison-v3.jsonl
/tmp/gorouter-benchmark-20260903/load-stats-v3.jsonl
```

Artifacts contain timings, statuses, byte counts, and safe error classes. They
do not contain provider keys, authorization headers, prompts, or completions.
