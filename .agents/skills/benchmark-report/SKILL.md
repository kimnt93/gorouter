---
name: benchmark-report
description: Benchmark or stress-test GoRouter latency, throughput, concurrency, streaming, cache behavior, errors, CPU, memory, or connection use and produce a reproducible comparison report. Use for performance experiments and local-versus-remote evaluations. Do not use for ordinary correctness verification without a performance claim.
---

# Benchmark and report

Read [methodology.md](references/methodology.md) before designing or running the
experiment. Read [report-format.md](references/report-format.md) when collecting
results or writing the handoff.

## Workflow

1. Turn the request into a falsifiable comparison: targets, workload profiles,
   concurrency, stream mode, repetitions, timeouts, metrics, and success rules.
2. Establish correctness with `$code-verification` before interpreting speed.
   Invalid responses and retries are results, not successful latency samples.
3. Control variables: same model, prompt bytes/tokens, output cap, client,
   network vantage point, warm-up, request order, cache state, and observation
   interval.
4. Run enough repetitions to show distributions. Preserve raw sanitized JSONL
   or Go benchmark output so summaries can be reproduced.
5. Measure latency percentiles and error classes alongside throughput,
   time-to-first-token, CPU, memory, active connections, token/cost components,
   and both provider-cache and router-cache results where relevant.
6. Report facts separately from hypotheses. Do not claim causation or improved
   reliability from a small, biased, or rate-limited sample.

## Safety

- Prefer mock/local benchmarks. Real providers require explicit user intent,
  bounded spend, request count, concurrency, input/output, and timeout.
- Read credentials only from the process environment. Never persist or report
  keys, cookies, authorization headers, prompts, completions, or raw provider
  bodies.
- Do not disable authorization, quota, or cache isolation to make a benchmark
  faster.
