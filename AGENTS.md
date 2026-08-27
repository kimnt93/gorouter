# GoRouter agent instructions

## Mission

GoRouter is a small, fast, maintainable, multi-user LLM gateway. It exposes
OpenAI-compatible model APIs while centrally enforcing provider routing,
identity and organization policy, model allowlists, quotas, pricing, usage
attribution, provider cache-token accounting, and distributed prompt caching.

Prefer explicit, secure behavior over clever abstractions. Keep the gateway
focused: do not add unrelated agent orchestration, workflow engines, payments,
VM/process management, or provider features without a product requirement.

## Instruction and contract precedence

Apply guidance in this order:

1. The current user request.
2. This `AGENTS.md` and any closer `AGENTS.override.md`.
3. Current code, tests, migrations, and generated contracts.

The implemented behavior is now the living product contract. Preserve the
React dashboard, identity/organization rules, and single-backend runtime model
documented below and in the repository skills. Exactly one durable backend is
active: PostgreSQL or ClickHouse.

## Use the repository skills

Select the smallest relevant set from `.agents/skills/` and read each selected
`SKILL.md` completely before editing:

- `$development` — project map, Go conventions, and feature workflow.
- `$go-refactoring` — behavior-preserving Go restructuring and analysis tools.
- `$fiber-api` — Fiber v3 handlers, middleware, typed JSON, request-scoped
  fluent responses, routes, SSE, and Swagger.
- `$swaggo-docs` — Swag annotations, deterministic generation, and API-doc
  verification.
- `$provider-connections` — provider catalog, API-key/OAuth adapters,
  discovery/import, model names, streaming, usage, and quotas.
- `$data-backends` — PostgreSQL/ClickHouse parity, migrations,
  repositories, Redis, and distributed state.
- `$solution-design` — authorization, ownership, organization context,
  View As, privacy, routing, pricing, and feature design.
- `$react-dashboard` — Vite/React/TypeScript UI, contracts, styling,
  filters, generated SPA assets, and browser behavior.
- `$code-verification` — unit/integration/browser verification, Docker profiles,
  smoke fixtures, and completion evidence.
- `$benchmark-report` — reproducible performance, concurrency, cache, and
  resource comparisons with a structured report.

Use several skills when a change crosses boundaries. For example, a new
provider normally needs provider, Fiber API, data backend, React dashboard, and
testing guidance.

When a selected skill links a reference as required or marks an example as
canonical, read that reference before editing and follow its structure unless
the existing local contract has a concrete reason to differ. Adapt example
domain names and types; do not copy hypothetical identifiers literally.

## Non-negotiable domain rules

### Authorization and visibility

- Every protected operation checks both capability/scope and object-level
  ownership. UI visibility is never the authorization boundary.
- Master may see global data and may use organization context as a reversible
  **View As organization admin** filter. Master must always be able to return
  to master context.
- Organization admins see only their organization's members and organization
  data. Ordinary users see only their own data.
- A user may join multiple organizations. Organization context narrows access;
  it never grants access the principal did not already have.
- Foreign private resources should return 404 when concealment is appropriate;
  missing capability returns 403.
- API keys are assigned to one user or organization, have immutable ownership
  and context, and expose plaintext only once on creation or rotation.
- An organization admin may assign model-limited keys to users in that
  organization, but may not inspect or manage a user's unrelated personal
  credentials or personal usage.
- Personal API-key and OAuth provider connections belong to the authenticated
  user. Organization admins cannot see the secret or learn which personal
  connection the user used. Master-created connections retain global
  operational compatibility.

### Model names, pricing, and usage

- Personal/global provider models use `<provider-prefix>/<model>`, for example
  `cx/gpt-5.6-luna` or `ocz/deepseek-v4-flash`.
- Organization-owned routes use
  `<organization-slug>/<provider-prefix>/<model>`, for example
  `microsoft/cx/gpt-5.6-luna`.
- Use `pkg/provider` helpers for public IDs and organization slugs. Do not
  assemble model namespaces ad hoc.
- A request is charged and attributed only to the owner/context of the route
  and key used. Personal models must not acquire organization attribution.
- Preserve input, output, provider cache-read, and provider cache-write tokens
  and cost components separately end to end.
- Missing prices mean **Free**: all rates and recorded cost are zero. Do not
  reintroduce an ambiguous `unpriced` state. Display rates rounded sensibly
  (currently four decimal places where model rates are shown).
- Provider-side prompt cache tokens are different from GoRouter's deterministic
  Redis response cache. Never merge their metrics.
- Usage actor fields are immutable snapshots. Do not reconstruct historical
  actor identity by joining current users or organizations at render time.

### Distributed and storage behavior

- A runtime uses one complete durable repository set: PostgreSQL or
  ClickHouse. Never dual-write, cross-read, synchronize, or silently fall back
  between them.
- Both backend implementations satisfy the same domain interfaces and behavior.
  Any schema/query/service change that affects persisted behavior must be made
  and tested in both modes unless it is inherently backend-specific.
- Go owns IDs and UTC timestamps. Inserts explicitly persist them; migrations
  must not add database-generated identity/time defaults.
- Redis is shared coordination state for production: prompt cache, quota/RPM,
  API-token invalidation, OAuth flows, provider quota state, routing health,
  round robin, pricing invalidation, and ClickHouse mutation locking.
- Correctness-critical multi-node behavior must not rely on process-local maps.
  In-memory implementations are development or explicit error fallbacks only;
  production must fail closed where required.
- Never weaken per-key/organization cache namespaces. Cross-user or
  cross-organization cache leakage is a release blocker.

## Architecture and coding rules

Keep dependency direction:

```text
internal/api -> pkg use cases -> pkg/entities
internal/repositories and internal/platform -> pkg interfaces
cmd/gorouter -> all concrete implementations
```

- `pkg/entities`: typed domain records, ports, IDs, scopes, domain errors.
- `pkg/<feature>`: business services and repository interfaces; no Fiber.
- `internal/repositories/{postgres,clickhouse}`: durable implementations.
- `internal/platform`: database, Redis, pricing, provider protocols, cache.
- `internal/api`: Fiber handlers, middleware, routes, presenters, SPA embed.
- `src`: React dashboard and typed browser API client.
- `cmd/gorouter`: the only production composition root.

Use typed Go structs with JSON tags for defined request, response, persistence,
and provider shapes. Do not use `map[string]any` when a stable shape is known.
Keep handlers thin: bind and validate HTTP input, call services/policy, map
errors, and return through the request-scoped `internal/api` fluent response
boundary. Bind it with `responseapi.For(c)`; never store Fiber context in
package-global state. Keep SQL, Redis, and provider HTTP details out of
handlers and domain entities.

Preserve user work in a dirty tree. Inspect current changes before editing and
make the smallest cohesive patch. Do not edit generated files by hand.

## Change workflow

1. Read the relevant specs, selected skills, interfaces, implementation, and
   tests before editing.
2. Trace the full behavior slice: entity/port, policy/service, both durable
   backends, Redis coordination, handler/route/contract, React client/UI, and
   composition root.
3. Update the canonical source first and add focused tests with the behavior.
4. Regenerate affected artifacts:
   - React bundle: `npm run build` writes committed
     `internal/api/spa/dist` assets.
   - Swagger: use `$swaggo-docs` to regenerate and verify the three committed
     files under `internal/docs`.
5. Verify proportionally to risk and inspect the final diff for unrelated or
   secret-bearing changes.

## Verification baseline

For ordinary cross-stack changes, run:

```bash
gofmt -w <changed-go-files>
go test ./...
go vet ./...
npm test -- --run
npm run build
git diff --check
```

Add the shared PostgreSQL and ClickHouse integration suites for repository or
schema changes, Redis/multi-node tests for distributed state, and desktop
browser smoke tests for UI behavior. Prefer Fiber `app.Test()` and mock upstream
HTTP servers in tests. Do not spend real provider quota unless the user asks
for a live test and the target, model, cost, and concurrency are bounded.

## Secrets and sensitive data

- Never read or copy more of `.env` than a task strictly requires.
- Never commit or print provider keys, OAuth tokens, master/session secrets,
  plaintext API keys, cookies, authorization headers, database credentials, or
  protected smoke access files.
- Never put prompts, completions, or raw provider error bodies in logs, audit
  events, tracked fixtures, UI telemetry, skills, or agent handoffs.
- Treat `/tmp/gorouter-*-smoke-access.json` and stress-test JSONL files as
  sensitive local artifacts. If created, use mode `0600` and keep them out of
  Git.
- Use synthetic secrets in tests. Sanitize errors before persistence or display.

## Code review rules

- Flag any authorization query/filter that can broaden visibility from user to
  organization or from organization to global scope.
- Flag provider ownership supplied by the client when it should be derived
  from the authenticated principal.
- Flag PostgreSQL-only or ClickHouse-only behavior changes without an explicit
  backend-specific reason and parity evidence.
- Flag process-local correctness state in production paths that should use
  Redis.
- Flag any response, log, audit metadata, fixture, or error that can expose a
  secret, prompt, completion, or raw upstream body.
- Flag manual edits to generated SPA/Swagger artifacts and missing regeneration
  after their source changed.
