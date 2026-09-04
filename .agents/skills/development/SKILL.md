---
name: development
description: Implement or update GoRouter features across its Go services, Fiber v3 API, React/TypeScript dashboard, and SQLite/PostgreSQL/ClickHouse repositories. Use for general coding, cross-layer feature work, project navigation, or choosing the correct specialized GoRouter skill.
---

# Development

Follow `AGENTS.md` first. GoRouter is a Go gateway with a Fiber v3 HTTP boundary,
a Vite React/TypeScript dashboard, and one selected durable backend: local
SQLite, PostgreSQL, or ClickHouse. Redis coordinates distributed production
state but is not a durable fallback backend.

## Read before editing

- Read [project-map.md](references/project-map.md) to locate ownership and trace
  a change through the repository.
- Read [coding-conventions.md](references/coding-conventions.md) before ordinary
  Go implementation; its package boundaries and examples are canonical.
- Read the smallest applicable specialized skill completely:
  - `$fiber-api` for handlers, middleware, routes, SSE, or JSON contracts.
  - `$react-dashboard` for `src`, browser contracts, CSS, tests, and SPA builds.
  - `$data-backends` for SQLite, PostgreSQL, ClickHouse, migrations, persisted
    fields, repository queries, or Redis coordination.
  - `$provider-connections` for provider adapters, OAuth, discovery, streaming,
    model identifiers, token usage, or provider quotas.
  - `$solution-design` before ambiguous cross-layer identity, ownership,
    routing, quota, pricing, privacy, or distributed-state changes.
  - `$swaggo-docs` when an HTTP route or API contract changes.
  - `$code-verification` for proportional correctness and deployment evidence.
  - `$benchmark-report` for performance claims or comparisons.
  - `$go-refactoring` only for behavior-preserving structural work.
- Treat `go.mod`, `package.json`, current source, tests, migrations, generated
  contracts, and the repository skills as version-specific references. Do not
  rely on generic Fiber, React, SQL, or provider examples when the checked-in
  contract differs.

## End-to-end workflow

1. Inspect the worktree and preserve unrelated user changes.
2. Trace the complete behavior slice before editing:

   ```text
   pkg/entities port/type
     -> pkg/<feature> service and policy
     -> local + PostgreSQL + ClickHouse repositories/migrations
     -> internal/api Fiber handler and route
     -> src typed client and React UI
     -> cmd/gorouter composition
     -> tests and generated artifacts
   ```

   Skip layers only when the behavior genuinely does not cross them.
3. Keep dependency direction:

   ```text
   internal/api -> pkg use cases -> pkg/entities
   internal/repositories and internal/platform -> pkg interfaces
   cmd/gorouter -> concrete composition
   ```

4. Put business decisions in `pkg/<feature>`, Fiber binding/presentation in
   `internal/api`, provider/database mechanics in `internal/platform` or
   `internal/repositories`, and production wiring only in `cmd/gorouter`.
5. Define stable request, response, persistence, and provider shapes as typed
   structs. Avoid `map[string]any` except for genuinely open provider metadata.
6. Add focused tests with the change. For persisted behavior, implement and
   test backend parity in:
   - `internal/repositories/local` and embedded SQLite migrations;
   - `internal/repositories/postgres` and PostgreSQL migrations;
   - `internal/repositories/clickhouse` and ClickHouse migrations.

   SQLite uses compact JSON payload records in places, but fields tagged
   `json:"-"` require explicit columns and repository reads/writes; never assume
   JSON serialization persists them. PostgreSQL and ClickHouse may use
   different SQL mechanics but must expose the same domain behavior.
7. Regenerate rather than hand-edit generated outputs:
   - `npm run build` for `internal/api/spa/dist` after React changes.
   - `$swaggo-docs` generation for `internal/docs` after API changes.

## Go conventions

- Use the Go version and dependencies declared by `go.mod` (currently Go 1.26)
  and Fiber v3 patterns already established in the repository.
- Pass `context.Context` through service, repository, and provider operations.
  Do not substitute `context.Background()` unless work intentionally outlives
  the request.
- Keep Fiber handlers thin and return JSON through the request-scoped response
  boundary (`responseapi.For(c)`), not direct ad hoc responses.
- Wrap operational errors with context internally and expose stable safe errors
  at HTTP boundaries.
- Generate IDs with `entities.NewID` and UTC timestamps in Go before storage.
- Keep interfaces consumer-owned and constructors explicit. Avoid hidden global
  service locators and concrete infrastructure dependencies in domain code.
- Use `gofmt`; do not manually align Go declarations or edit generated files.

## Completion

Do not call a cross-layer change complete because it compiles in one package or
works with one backend. Verify the affected SQLite, PostgreSQL, and ClickHouse
contracts; Redis/multi-node behavior when distributed correctness changes;
Fiber routes and Swagger when APIs change; and React tests plus the embedded SPA
build when the dashboard changes. Finish with the proportional baseline from
`$code-verification` and inspect `git diff --check` for accidental or
secret-bearing changes.
