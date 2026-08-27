# Swaggo workflow

## Source and generated files

- API metadata/security definition: `cmd/gorouter/main.go`.
- Operation annotations and typed HTTP contracts: `internal/api/handlers/`.
- Route truth: `internal/api/routes/routes.go`.
- Generated, committed outputs: `internal/docs/docs.go`, `swagger.json`, and
  `swagger.yaml`.

The repository requires dependency and internal-package parsing because the
shared presenter error aliases `internal/api.ErrorResponse` and contracts use
internal/domain types.

## Generate

From the repository root, run the pinned version:

```bash
go run github.com/swaggo/swag/cmd/swag@v1.16.6 init \
  -g cmd/gorouter/main.go \
  -o internal/docs \
  --parseDependency \
  --parseInternal
gofmt -w internal/docs/docs.go
```

The bundled script runs this exact command and supports a no-write drift check:

```bash
.agents/skills/swaggo-docs/scripts/generate.sh check
.agents/skills/swaggo-docs/scripts/generate.sh generate
```

## Annotation checklist

- `@Summary`, `@Tags`, and `@Security BearerAuth` for protected operations.
- `@Accept json` for JSON bodies and the correct `@Produce` media type.
- Typed body/query/path parameters with required flags and bounded values.
- Exact success status and response type.
- Expected 400/401/403/404/409/429/5xx presenter errors.
- No secret examples, tokens, cookies, prompts, or raw provider bodies.

## Diagnose generation errors

- `cannot find type definition`: keep `--parseDependency --parseInternal`,
  ensure the annotation points at an exported resolvable type, and avoid an
  untyped alias cycle.
- missing route: verify the handler has an operation annotation and the
  generated path/method matches `routes.go`.
- surprising schema: expose a dedicated typed HTTP contract instead of a
  generic `any`, interface, or stable-shape `map[string]any`.
- huge unrelated diff: confirm the pinned Swaggo version and run generation
  from the repository root.

## Verify

```bash
go test ./internal/api/routes -run 'Swagger|Contract|Response'
go test ./internal/api/...
git diff --check
```

Inspect all three generated files together. A successful generator exit does
not prove that every registered JSON route is documented; the route contract
tests provide that evidence.
