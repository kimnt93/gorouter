---
name: development
description: Implement or update GoRouter features using its Go architecture, typed contracts, project structure, and end-to-end change workflow. Use for general backend work, cross-layer feature changes, or locating the correct package. Use go-refactoring for behavior-preserving structural cleanup and specialized skills for providers, storage, Fiber, or React.
---

# Development

Follow `AGENTS.md` first. Read [project-map.md](references/project-map.md) when
locating ownership or tracing a feature. Read
[coding-conventions.md](references/coding-conventions.md) for ordinary Go
changes; its examples are the canonical package/service style. Use
`$go-refactoring` for a behavior-preserving structural refactor.

## Workflow

1. Inspect the working tree and preserve unrelated edits.
2. Read the relevant code, typed contracts, migrations, and existing tests.
   Treat implemented behavior plus `AGENTS.md` and the repository skills as the
   maintained product contract.
3. Trace the behavior across domain types/ports, service/policy, repository,
   handler/route, browser contract, composition root, and tests. Change only the
   layers the behavior actually crosses.
4. Put business decisions in `pkg/<feature>`, transport translation in
   `internal/api`, and external/database details in `internal/platform` or
   `internal/repositories`.
5. Define stable shapes as typed structs. Avoid `map[string]any` except truly
   open-ended provider metadata.
6. Add or update tests at the closest boundary, then run the required
   verification from `AGENTS.md` and the specialized skill.

## Go conventions

- Use Go 1.26 syntax and standard-library patterns already present in the repo.
- Accept `context.Context` in service/repository/provider operations and pass it
  through; do not replace request context with `context.Background()` unless
  the work intentionally outlives the request.
- Wrap operational errors with useful context, but expose only safe messages
  through the request-scoped response API at HTTP boundaries.
- Keep constructors explicit and wire concrete dependencies only in
  `cmd/gorouter/main.go`.
- Generate IDs with `entities.NewID` and timestamps with `time.Now().UTC()`
  before persistence.
- Keep interfaces consumer-oriented and small. Add compile-time interface
  assertions for adapters when useful.
- Use `gofmt`; do not hand-align or create custom style rules.

## Completion

Do not report a feature complete because one layer compiles. Verify the full
slice, generated artifacts, both storage modes when persisted behavior changes,
and distributed correctness when Redis-backed state is involved.
