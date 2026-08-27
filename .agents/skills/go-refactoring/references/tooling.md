# Go refactoring tools

Use repository-native and standard Go tools first. Check availability before
relying on optional tools; do not modify `go.mod` merely to run an analyzer.

## Discover impact

```bash
rg 'SymbolOrContract'
go list ./...
go list -deps ./path/to/package
go doc ./path/to/package.Symbol
go mod why <module>
go mod graph
```

Use `gopls` references and rename from the IDE/CLI when available for symbols
that cross packages. Still search string-based route names, JSON tags, SQL
columns, Redis keys, templates, Swagger annotations, reflection, and generated
contracts because language-aware rename cannot update all of them.

## Mechanical rewrites

- `gofmt -w <files>` is mandatory after edits.
- `goimports` may organize imports when already installed; verify it does not
  introduce/remove unintended dependencies.
- `gofmt -r '<pattern> -> <replacement>'` is acceptable only for a reviewed,
  narrowly scoped structural rewrite. Preview on copies or inspect the entire
  diff immediately.
- `go fix` is for a deliberate Go/API migration, not routine cleanup. Run it
  only with a cleanly understood target and inspect every edit.

## Static and dynamic checks

```bash
go test ./path/to/affected/...
go vet ./...
go test -race ./path/to/concurrent/...
go test -cover ./path/to/affected/...
go test -run TestName -count=20 ./path/to/package
```

Use `staticcheck` only when already available or explicitly authorized. Treat
an analyzer warning as evidence to investigate, not permission for a semantic
rewrite.

## Performance-guided refactoring

- Start with a stable benchmark and profile; do not optimize from intuition.
- Use `go test -bench ... -benchmem`, `go tool pprof`, compiler escape output,
  and trace data only for a defined performance claim.
- Keep race-instrumented and profiled timings separate from normal benchmark
  results.
- Use `$benchmark-report` to compare before/after distributions and resources.

## GoRouter-specific hazards

- Keep `cmd/gorouter/main.go` as the composition root.
- Do not move Fiber, pgx, ClickHouse, Redis, or provider HTTP types into
  `pkg/entities`.
- Preserve PostgreSQL/ClickHouse interface parity and Redis key compatibility.
- Preserve API response structs/JSON tags, Swaggo type resolution, embedded SPA
  paths, public model namespaces, usage actor snapshots, and authorization
  concealment behavior.
- Never hand-edit generated Swagger or SPA bundle files as part of a refactor.
