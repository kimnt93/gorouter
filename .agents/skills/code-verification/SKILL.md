---
name: code-verification
description: Verify GoRouter code with focused unit, integration, distributed, browser, Docker Compose, and smoke tests, then report reproducible evidence. Use after implementation, while diagnosing failures, or when reviewing whether a change is complete. Use benchmark-report instead for comparative performance experiments.
---

# Code verification

Read [verification-matrix.md](references/verification-matrix.md) to choose test
depth. Read [smoke-and-live-tests.md](references/smoke-and-live-tests.md) before
Docker resets, seeding, browser checks, or bounded live-provider verification.

## Test strategy

1. Define the claim being tested and the observable evidence before running a
   command.
2. Start with deterministic unit/handler tests, then repository/integration
   tests, then browser/live tests only where they add evidence.
3. Use Fiber `app.Test()` and `httptest.Server` for HTTP boundaries. Simulate
   provider errors, streaming fragmentation, cancellation, retries, and token
   usage without spending quota.
4. For persisted behavior, run the same contract against PostgreSQL and
   ClickHouse. For cache, quota, routing, locks, OAuth, or invalidation, include
   Redis and multi-instance/outage behavior.
5. For React changes, run Vitest, build the embedded bundle, and exercise the
   relevant desktop route with the browser smoke scripts.
6. Report commands, environment/profile, pass/fail counts, skipped tests, and
   limitations. Never call a skipped suite "passed."

## Live safety

- Never place real secrets on a command line, in scripts, in logs, or in
  tracked reports. Read them from the environment only inside the process.
- Redact authorization headers, prompts, completions, raw upstream bodies, and
  access-file plaintext from results.
- Use `$benchmark-report` when the requested result is a latency, throughput,
  resource, concurrency, cache-rate, or implementation comparison.
- Destructive Compose resets require explicit user intent, exact Compose
  project/files, and read-only target checks. State which volumes were removed.
- Store one-time smoke access material only under `/tmp`, mode `0600`, and do
  not quote it in the handoff.
