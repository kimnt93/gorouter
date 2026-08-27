---
name: go-refactoring
description: Refactor GoRouter Go code while preserving observable behavior, using characterization tests, gopls-aware symbol changes, gofmt, vet, race tests, profiles, and dependency analysis. Use for structural cleanup, package/API reshaping, duplication removal, concurrency simplification, or performance-motivated refactoring. Do not use for a feature whose behavior is intentionally changing.
---

# Go refactoring

Read [tooling.md](references/tooling.md) before a cross-package, concurrency, or
performance-motivated refactor.

## Workflow

1. Define the behavior and public/internal contracts that must remain stable.
   Add characterization tests when current behavior is not already protected.
2. Use language-aware references/rename tools when available. Inspect callers,
   interface implementations, route wiring, tests, build tags, generated files,
   and reflection/JSON names before moving or renaming symbols.
3. Make one coherent structural change at a time. Keep generated artifacts,
   database migrations, API JSON, authorization, model names, Redis keys, and
   error semantics stable unless the user explicitly requests a migration.
4. Run focused tests after each risky step, then `$code-verification` for the
   affected layers. Use `$benchmark-report` only when performance is part of
   the acceptance claim.
5. Report the structural improvement, behavior-preservation evidence, and any
   intentionally retained compatibility debt.

Do not combine a wide refactor with unrelated feature work. Do not use blind
text replacement for exported symbols or interface methods.
