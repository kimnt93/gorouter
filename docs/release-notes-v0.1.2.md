# GoRouter v0.1.2 — XNOBrain agent-attributed usage accounting

## Overview

Adds an authoritative, Router-backed usage contract for XNOBrain workloads.
GoRouter now records request, token, cache, cost, user, organization, workspace,
agent, conversation, run, and provider-attempt attribution without requiring
prompt or completion storage.

## Highlights

- Added immutable workload binding for API keys:
  - application namespace
  - environment namespace
  - workspace identity
  - agent identity
- Added dedicated inference correlation headers:
  - `X-GoRouter-Conversation-Id`
  - `X-GoRouter-Run-Id`
  - `X-GoRouter-Parent-Run-Id`
  - `X-GoRouter-Request-Id`
- Added authoritative weekly usage endpoint:

  ```http
  GET /admin/usage/agents/weekly
  ```

- Weekly usage follows the configured `WEEK_START` policy and uses an exclusive
  period end rather than a rolling seven-day calculation.
- Added Router-accounted cost and token aggregation by workload dimensions.
- Preserved separate accounting for:
  - uncached input tokens
  - provider cache-read tokens
  - provider cache-write tokens
  - completion tokens
  - Router response-cache hits
- Agent-bound usage events are synchronously persisted before accounting is
  acknowledged, reducing loss from the existing asynchronous queue.
- Added idempotent event persistence for SQLite, PostgreSQL, and ClickHouse.
- Added provider, credential, workspace, agent, conversation, run, and logical
  request filters to usage queries.
- Added PostgreSQL, ClickHouse, and SQLite schema migrations.
- Added the XNOBrain integration contract:

  ```text
  docs/xnobrain-agent-usage-contract.md
  ```

## Compatibility and security

- Existing API keys remain compatible and are reported as unbound/unattributed.
- Workload bindings cannot be assigned through inference requests or arbitrary
  request headers.
- Key rotation retains the workload identity.
- Historical usage remains after key or agent deletion.
- Authorization and organization visibility are applied before aggregation.
- No secrets, prompts, completions, provider headers, or raw upstream failures
  are returned by the new usage API.
- Missing model prices continue to follow GoRouter's Free/zero policy.

## Limitations

- This release provides soft budget observation for XNOBrain; it does not add
  strict atomic per-agent budget enforcement.
- Exact provider invoice reconciliation is unavailable when a provider does not
  report usage after a failed connection.
- Legacy unbound events still use the existing asynchronous usage queue and do
  not receive crash-safe durable handoff guarantees.
- Live PostgreSQL and ClickHouse integration suites and multi-replica HA tests
  require their configured test environments and were not run in this source
  change.

## Verification

- `go test ./...` passed.
- `go vet ./...` passed.
- `npm test -- --run` passed: 15 files, 49 tests.
- `npm run build` passed.
- Swagger generation and drift check passed.
- `git diff --check` passed.

## Delivery status

- Source implementation: complete.
- Unit tests: complete.
- SQLite integration coverage: complete.
- Released: not yet.
- Deployed: not yet.
