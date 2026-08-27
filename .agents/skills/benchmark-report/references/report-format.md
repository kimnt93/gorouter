# Benchmark report format

Lead with the outcome and test validity, then provide enough detail to reproduce
it.

## Summary

- Claim tested.
- Best-supported conclusion.
- Sample size and date.
- Material limitations or uncontrolled variables.

## Environment

| Field | Value |
|---|---|
| Revision/worktree | Commit plus whether local changes were present |
| Host/runtime | OS, architecture, Go version, container limits |
| Targets | Local/remote classification without secrets |
| Model/workload | Exact model ID, input profile, output cap, stream mode |
| Client | Timeout, pool/connections, repetitions, concurrency |
| Cache state | Cold/warm procedure and cache type measured |

## Results

Use one row per comparable cell:

| Target | Workload | Stream | Concurrency | N | Success | Error rate | p50 | p95 | TTFT p50 | Req/s | CPU | Memory | Cache result |
|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---|

Add a compact error-class table and token/cost table when they materially
explain outcomes. Never average percentiles from separate runs.

## Interpretation

Separate:

- observations directly supported by samples;
- likely explanations supported by logs/resource data;
- open questions requiring a controlled follow-up.

State whether failures came from GoRouter validation/coordination, network
transport, upstream HTTP status, malformed streaming, timeout, or unknown safe
classification. Avoid pasting raw provider bodies.

## Reproduction

List the sanitized command/script, non-secret environment names, workload
fixture, and result-file path. Keep raw output in `/tmp` or another approved
untracked location and state its format. Do not include access tokens or exact
prompt/completion content in the report.
