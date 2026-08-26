# Implementation Checklist

Agents may work in parallel only when file ownership does not overlap. Interface
changes must be committed/communicated before repository, handler, or UI agents
consume them.

Do not mark an item complete until implementation and tests are present.

## Coordination and safety

- [x] All assigned agents read this directory's `README.md` and `SPEC.md`.
- [x] Existing unrelated worktree changes are preserved.
- [x] Typed structs replace avoidable `map[string]any` values.
- [x] No secrets, prompts, completions, hashes, or cookies enter logs/audit/UI.
- [x] IDs and timestamps are generated in Go and explicitly persisted.
- [x] PostgreSQL and ClickHouse are never active together.
- [x] Terminal mockups are treated only as visual references; no terminal/CLI is
      implemented for this feature.

## Workstream A — Domain and policy interfaces

Suggested ownership: `pkg/entities`, new identity/organization packages, policy
package, repository interfaces, interface tests.

- [x] Add User, Organization, Membership, principal, usage actor, and audit
      entities.
- [x] Extend API-key entity with exclusive owner and organization context.
- [x] Add `members:manage` to valid scopes and scope tests.
- [x] Define typed repository/use-case interfaces for both storage modes.
- [x] Define centralized policy checks for users, memberships, keys,
      credentials, usage, and audit.
- [x] Validate email/name normalization and owner/context combinations.
- [x] Add unit tests covering the authorization matrix.

## Workstream B — PostgreSQL storage and migration

Suggested ownership: `platform/database/migrations`, PostgreSQL repositories and
their integration tests.

- [x] Add users, organizations, memberships, and audit tables.
- [x] Add key ownership/context columns and constraints.
- [x] Add usage actor snapshot columns and indexes.
- [x] Migrate tenants to organizations while preserving IDs.
- [x] Migrate existing keys as organization-owned.
- [x] Mark existing usage actors as legacy without database-generated IDs/time.
- [x] Implement PostgreSQL user/organization/membership repositories.
- [x] Extend PostgreSQL API-key ownership queries.
- [x] Implement principal-filtered usage and audit queries.
- [x] Use transactions for compound identity/key mutations.
- [x] Add migration and repository integration tests.

## Workstream C — ClickHouse storage and migration

Suggested ownership: ClickHouse migrations/repositories and ClickHouse
integration tests. This agent must not import/use PostgreSQL repositories.

- [x] Add typed user, username lookup, organization, name lookup, membership,
      and extended key records.
- [x] Add configuration mutation serialization/conflict behavior.
- [x] Add fail-closed Redis distributed locking for multi-replica,
      uniqueness-sensitive ClickHouse configuration mutations.
- [x] Add usage actor columns and explicit-column batch inserts.
- [x] Add append-only audit table/repository.
- [x] Normalize legacy tenant/key records into organization domain objects.
- [x] Preserve legacy usage and mark unknown actors as legacy.
- [x] Implement the same ownership, usage, and audit repository behavior as
      PostgreSQL.
- [x] Add ClickHouse integration tests using the shared behavior suite.
- [x] Prove ClickHouse mode starts and operates without PostgreSQL.

## Workstream D — Authentication and API-key use cases

Suggested ownership: `pkg/auth`, `pkg/apikey`, identity/organization use cases,
auth/key handler tests.

- [x] Resolve master/user/organization principals during login.
- [x] Add principal fields to signed sessions.
- [x] Revalidate key, owner, organization, and membership state.
- [x] Add master-only user/organization creation.
- [x] Add optional initial user key with one-time plaintext.
- [x] Add membership add/change/remove and last-admin protection.
- [x] Add self-service personal and organization-scoped personal keys.
- [x] Add organization-owned key management for organization admins.
- [x] Add key rotation with immediate old-secret invalidation.
- [x] Invalidate auth/key caches on every relevant mutation.
- [x] Test disabled users/orgs, removed memberships, horizontal access, scope
      escalation, and stale cookie behavior.

## Workstream E — Gateway and usage attribution

Suggested ownership: gateway request lifecycle, usage service/entities, quota and
cache integration tests affected by access context.

- [x] Replace stored-key-only gateway assumptions with an access context.
- [x] Support master `/v1/models` and `/v1/chat/completions` calls.
- [x] Isolate master cache entries.
- [x] Enforce model/quota/RPM policies for user and organization keys.
- [x] Enforce global versus organization credential visibility.
- [x] Record user, organization, master, and shared-org actor snapshots.
- [x] Ensure personal usage never acquires organization context implicitly.
- [x] Keep quota accounting per API key and existing settlement guarantees.
- [x] Add streaming, non-streaming, cache-hit, error, and master attribution
      tests.

## Workstream F — JSON APIs and authorization

Suggested ownership: Fiber handlers/routes/presenters and route-level tests.

- [x] Implement typed user endpoints.
- [x] Implement typed organization endpoints and deprecated tenant aliases.
- [x] Implement membership endpoints.
- [x] Extend key endpoints with owner/context and rotation.
- [x] Add cursor-paginated principal-filtered usage endpoints.
- [x] Add cursor-paginated audit endpoint.
- [x] Return 403 for missing capability and conceal foreign private resources
      with 404 where specified.
- [x] Add route tests for every master/user/member/admin/org-key role.
- [x] Verify query filters can narrow but never broaden visibility.

## Workstream G — Server-rendered web UI

Suggested ownership: `api/views`, UI handlers, static CSS/JS, view tests.

- [x] Add principal label and safe organization context selector.
- [x] Add master-only Users page and create-user/initial-key flow.
- [x] Add Organizations page and member management.
- [x] Extend API-key UI with owner/context and rotation.
- [x] Replace usage primary columns with Model, User, I/O, Time.
- [x] Implement relative-under-60-seconds/RFC3339 time rendering.
- [x] Add expandable safe usage details and filters/pagination.
- [x] Add audit page.
- [x] Hide unauthorized actions while retaining server enforcement.
- [x] Validate desktop/mobile layouts and dialog bounds.
- [x] Keep the implementation a normal web UI; do not add terminal behavior.

## Workstream H — Audit and cross-cutting integration

- [x] Emit audit events for every required administrative mutation.
- [x] Ensure audit writes contain only safe metadata.
- [x] Add organization/master audit visibility tests.
- [x] Verify user/org disablement and membership removal invalidate sessions and
      key access across all routes.
- [x] Verify organization administrators cannot access unrelated personal usage
      or keys.
- [x] Verify organization-owned key activity displays `org:<name>`.
- [x] Verify master activity displays `master`.
- [x] Verify legacy events display `legacy` without guessed attribution.

## Shared backend behavior suite

Run the same contract scenarios against PostgreSQL and ClickHouse:

- [x] Unique normalized user email.
- [x] Unique normalized organization name.
- [x] Multiple organizations per user.
- [x] Membership role and last-admin rules.
- [x] Personal key ownership isolation.
- [x] Organization-scoped personal key validation.
- [x] Organization-owned key administration.
- [x] Key login, rotation, disablement, and cache invalidation.
- [x] User-only usage visibility.
- [x] Organization-only usage visibility.
- [x] Master global usage visibility.
- [x] Cursor pagination stability.
- [x] Audit visibility and secret exclusion.
- [x] Legacy migration compatibility.

## Final acceptance

- [x] `gofmt` passes for all changed Go files.
- [x] `go vet ./...` passes.
- [x] `go test ./...` passes.
- [x] PostgreSQL-mode integration suite passes with ClickHouse unavailable.
- [x] ClickHouse-mode integration suite passes with PostgreSQL unavailable.
- [x] Live master login/user creation/initial-key smoke test passes.
- [x] Live personal-key and organization-key login smoke tests pass.
- [x] Live master/user/organization model call attribution is correct.
- [x] Live user and organization usage visibility is correct.
- [x] Browser smoke test covers users, organizations, memberships, keys, usage,
      audit, and mobile layout.
- [x] No dual storage reads/writes appear in runtime code or logs.
- [x] All required checklist items are checked with evidence in the final handoff.

## Completion evidence (2026-08-26)

- Domain, policy, authentication, ownership, session revalidation, cache
  invalidation, gateway attribution, audit safety, and time-format behavior are
  covered by unit tests in `pkg/` and `internal/api/handlers`.
- PostgreSQL migrations `0012`/`0013` and ClickHouse migration `003` implement
  the identity, ownership, usage-actor, compatibility, and audit storage shape.
  `internal/platform/database/ownership_test.go` enforces Go-owned IDs/time.
- `internal/integration/identity_contract.go` is executed unchanged against
  PostgreSQL and ClickHouse and covers normalization/uniqueness, multi-org
  membership and last-admin behavior, compound user/key provisioning, all key
  shapes, rotation/disablement, user/org/master usage visibility, stable
  cursors, safe audit visibility, and explicit legacy attribution.
- `internal/api/routes/integration_test.go` performs live master, personal,
  scoped-user, and organization-key flows, including login, role enforcement,
  horizontal isolation, disablement, streaming/non-streaming/cache behavior,
  model calls, actor snapshots, and principal-filtered usage.
- Every JSON route is represented by generated Swag documentation and named
  contracts (`internal/api/routes/swagger_test.go`). All handler JSON output and
  errors pass through the shared builder in `internal/api/response.go`, enforced
  by `internal/api/routes/response_contract_test.go`.
- UI render/security/responsiveness coverage is in
  `internal/api/handlers/ui_smoke_test.go`. A live headless Chrome run at 1280×800
  and 375×812 verified users, organizations, keys, usage, and audit without
  document overflow; membership-detail markup is exercised by the same render
  suite. One-time secrets are asserted to occur only in acknowledgement dialogs.
- Acceptance commands passed: `gofmt`, `go vet ./...`, `go test ./...`, live
  PostgreSQL/ClickHouse shared repository suites, live API route tests,
  PostgreSQL startup with ClickHouse variables absent, and ClickHouse startup
  with PostgreSQL variables absent. Both standalone servers returned 200 for
  health, master login, and `/docs`.

## React operations dashboard follow-up (2026-08-26)

- [x] Add a reusable Vite, React, and TypeScript application under `src/`.
- [x] Display request tokens as input/output/cache-read/cache-write and open a
      safe metadata modal from each request row.
- [x] Keep prompts, completions, provider errors, cookies, hashes, and secret
      material out of the React UI.
- [x] Add provider-reported cache-read/cache-write analysis while clearly
      separating gorouter response-cache counters.
- [x] Add horizontal activity bars with user/API-key filters, 1D/7D/30D/90D/
      YTD/All/custom ranges, and hour/day/week grouping.
- [x] Implement the typed activity endpoint for both PostgreSQL and ClickHouse
      with the existing principal-aware visibility filter.
- [x] Embed the production Vite build in Go, preserve management pages, and
      redirect the legacy usage/cache pages to the React views.
- [x] Verify desktop and mobile layouts, request modal safety, asset delivery,
      frontend tests/build, Go tests/vet, and live PostgreSQL/ClickHouse
      activity aggregation.

Follow-up evidence: `npm run build`, `npm test`, `go vet ./...`, and
`go test ./...` pass. `scripts/ui-smoke.mjs` logged in to the live PostgreSQL
deployment and exercised analysis, logs, the request modal, and provider cache
views at 1440x900 and 375x812 without document overflow. The PostgreSQL
`TestTenantUsageQueriesAreIsolated` and ClickHouse `TestPrimaryStoreRoundTrip`
integration tests assert identical hour-bucket request, cost, and all four token
aggregates.
