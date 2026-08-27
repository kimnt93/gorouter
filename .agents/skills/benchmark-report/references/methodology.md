# Benchmark methodology

## Choose the benchmark mode

### Go microbenchmark

Use for a deterministic function or package boundary. Add a real
`BenchmarkXxx(*testing.B)` near the code, move setup outside the timed region,
use `b.ResetTimer()` only when necessary, and report allocations:

```bash
go test ./path/to/package -run '^$' -bench 'BenchmarkName$' \
  -benchmem -count=10
```

Capture baseline and candidate output separately. Use an already-available
`benchstat` to compare distributions; do not add a build dependency merely to
format a report. Check that compiler optimization has not eliminated the work.

### Gateway/load benchmark

Use for concurrency, streaming, provider pooling/failover, cache, and resource
behavior. The repository's `scripts/long_context_compare.py` is the starting
harness for long-context local/remote comparisons. Extend it instead of
creating a second incompatible result schema when its workload fits.

Define a Cartesian matrix only as large as needed. A typical requested matrix
is concurrency `1,3,5,8`, stream and non-stream, and short/long inputs, with a
fixed Luna public model and bounded output. Record the exact public model ID;
do not replace it with a generated benchmark label.

## Experimental controls

- Verify each target with one request before loading it.
- Record date/timezone, commit/worktree state, Go/OS/architecture, Compose
  profile, target URL class, model, prompt byte/token estimate, output cap,
  timeout, client connection-pool settings, and cache preparation.
- Separate warm-up from measured requests.
- For implementation comparisons, run the order requested by the user; if none
  is specified, alternate or randomize target order to reduce time-window bias.
- Repeat each cell enough to calculate distributions. Never combine stream and
  non-stream samples.
- Use exactly matching canonical requests for cache-hit trials and one
  controlled mutation for misses.

## Metrics

Per cell capture:

```text
attempts, successes, failures, cancellations
HTTP/provider error classes and error rate
latency p50, p95, p99, max
stream time-to-first-token and total duration
throughput (requests/s and, when valid, tokens/s)
input/output/cache-read/cache-write tokens and costs
GoRouter X-Cache hit/miss separately from provider cache tokens
process/container CPU and memory
open/active connections and Redis/database health
```

Use monotonic client timing. Do not treat missing usage as zero without stating
that the provider omitted it. Correlate resource samples to request intervals.

## Profiling tools

For local Go hotspots, prefer built-in tools:

- `go test -bench ... -cpuprofile /tmp/profile.cpu`
- `go test -bench ... -memprofile /tmp/profile.mem`
- `go tool pprof <binary-or-test> /tmp/profile.cpu`
- `go test -race` for race evidence; do not compare its performance numbers to
  a normal build.

Keep profiles under `/tmp`; they may contain symbol or request metadata. Do not
commit them.
