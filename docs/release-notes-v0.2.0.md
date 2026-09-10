# GoRouter v0.2.0 — Authoritative workload usage accounting

## Overview

Adds application-neutral, authoritative Router accounting for services, agents,
workspaces, automation workers, and other workloads. GoRouter records request,
token, cache, cost, owner, workload, conversation, run, and provider-attempt
attribution without requiring prompt or completion storage.

## Highlights

- Immutable API-key workload binding with application, optional environment,
  workspace, and agent identities.
- Dedicated request correlation headers for conversation, run, parent run, and
  logical request IDs.
- New authoritative current-week endpoint:

  ```http
  GET /admin/usage/workloads/weekly
  ```

- Capability marker: `gorouter-workload-usage-v1`.
- Weekly windows follow configured `WEEK_START` and use an exclusive period end.
- Separate accounting for input, output, provider cache-read, provider
  cache-write, and GoRouter response-cache use.
- Synchronous durable handoff and idempotent persistence for workload-bound
  accounting events.
- Workload, agent, conversation, run, provider, credential, and logical-request
  filters on usage queries.
- PostgreSQL, ClickHouse, and SQLite schema support.
- General integration reference: `docs/workload-usage-contract.md`.

## Compatibility and security

- Existing unbound API keys remain compatible and explicitly unattributed.
- Inference callers cannot assign or mutate their workload authority.
- Rotation retains workload identity and deletion does not remove history.
- Existing user and organization authorization applies before aggregation.
- No keys, tokens, prompts, completions, or raw upstream failures are exposed.

## Limitations

- Weekly usage is suitable for soft admission decisions, not strict concurrent
  per-workload budget enforcement.
- Provider invoice totals can remain unknown when an upstream does not report
  usage after failure.
- Legacy unbound records retain the existing asynchronous queue behavior.
- Live PostgreSQL/ClickHouse and multi-replica HA suites require configured test
  environments and were not executed for the source implementation.

## Verification

- `go test ./...` passed.
- `go vet ./...` passed.
- Frontend tests passed: 15 files, 49 tests.
- Frontend build passed.
- Swagger generation/drift and `git diff --check` passed.

## Upgrade notes

- Run the new workload-usage migrations before enabling workload-bound keys.
- Update clients from the earlier draft `/admin/usage/agents/weekly` route to
  `/admin/usage/workloads/weekly`.
- Negotiate the final `gorouter-workload-usage-v1` capability marker.
- Existing unbound keys require no migration and continue to work normally.

## Delivery status

- Target release: `v0.2.0`.
- Source implementation: complete.
- Unit and SQLite integration coverage: complete.
- Release publication and deployment: pending.
