# Verification matrix

Choose every row affected by the change.

| Change | Minimum focused proof | Required expansion |
|---|---|---|
| Go domain/service | Package unit tests | `go test ./...`, `go vet ./...` |
| Fiber handler/route | `app.Test()` success and denial/error tests | API route, response-contract, and Swagger tests |
| Provider adapter | `httptest.Server`, stream/non-stream, errors, cancellation | Connectivity/discovery/import and route tests |
| PostgreSQL repository/migration | Live PostgreSQL focused suite | Shared backend contract and ownership test |
| ClickHouse repository/migration | Live ClickHouse focused suite | Shared backend contract, Redis lock/multi-writer proof |
| Redis state | Unit/atomic/TTL/outage tests | Two-instance or real Redis integration when needed |
| Gateway lifecycle | Mock upstream, usage/quota/cache assertions | Streaming, failure, retry, and concurrency paths |
| React contract/component/page | Vitest/Testing Library | `npm run build`; desktop browser smoke for layout/wiring |
| Swagger annotations | Regenerate all docs | Swagger and named-contract tests |
| CSS/layout | Relevant component test | Desktop screenshot/overflow and interaction smoke |

## Baseline commands

```bash
gofmt -w <changed-go-files>
go test ./...
go vet ./...
npm test -- --run
npm run build
git diff --check
```

`make test` supplies default live PostgreSQL and Redis URLs for the full Go
suite when those containers are available. Tests may skip without those
variables; inspect output and report skipped integration evidence accurately.

## Behavioral assertions

Always include the relevant negative paths:

- missing scope, inactive owner/membership, foreign ID, and forged
  organization context;
- empty/unknown model allowlist; provider/blend `auto` listing, authorization,
  bounded attempts, random failover, and selected-route pricing;
- cross-user, cross-key, and cross-organization cache/query isolation;
- Redis strict outage and contention;
- provider timeout, retryable versus non-retryable status, malformed stream,
  client cancellation, and omitted usage;
- Free/zero price and four token/cost components;
- one-time/encrypted-reveal secret exclusion from list responses, audit, logs, UI
  persistence, and generated fixtures;
- user deletion removes live authentication references but retains immutable
  historical usage/audit snapshots;
- provider bulk actions skip disabled accounts and show partial-success details
  without aborting remaining work.

## Generated artifacts

Run `npm run build` after `src`, Vite, or frontend dependency changes. Run the
Swag command in `AGENTS.md` after handler annotations or contract types change.
Review generated diffs for expected route/type changes rather than accepting a
large unexplained rewrite.
