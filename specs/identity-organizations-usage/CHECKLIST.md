# Implementation Checklist

Agents may work in parallel only when file ownership does not overlap. Interface
changes must be committed/communicated before repository, handler, or UI agents
consume them.

Do not mark an item complete until implementation and tests are present.

## Coordination and safety

- [ ] All assigned agents read this directory's `README.md` and `SPEC.md`.
- [ ] Existing unrelated worktree changes are preserved.
- [ ] Typed structs replace avoidable `map[string]any` values.
- [ ] No secrets, prompts, completions, hashes, or cookies enter logs/audit/UI.
- [ ] IDs and timestamps are generated in Go and explicitly persisted.
- [ ] PostgreSQL and ClickHouse are never active together.
- [ ] Terminal mockups are treated only as visual references; no terminal/CLI is
      implemented for this feature.

## Workstream A — Domain and policy interfaces

Suggested ownership: `pkg/entities`, new identity/organization packages, policy
package, repository interfaces, interface tests.

- [ ] Add User, Organization, Membership, principal, usage actor, and audit
      entities.
- [ ] Extend API-key entity with exclusive owner and organization context.
- [ ] Add `members:manage` to valid scopes and scope tests.
- [ ] Define typed repository/use-case interfaces for both storage modes.
- [ ] Define centralized policy checks for users, memberships, keys,
      credentials, usage, and audit.
- [ ] Validate email/name normalization and owner/context combinations.
- [ ] Add unit tests covering the authorization matrix.

## Workstream B — PostgreSQL storage and migration

Suggested ownership: `platform/database/migrations`, PostgreSQL repositories and
their integration tests.

- [ ] Add users, organizations, memberships, and audit tables.
- [ ] Add key ownership/context columns and constraints.
- [ ] Add usage actor snapshot columns and indexes.
- [ ] Migrate tenants to organizations while preserving IDs.
- [ ] Migrate existing keys as organization-owned.
- [ ] Mark existing usage actors as legacy without database-generated IDs/time.
- [ ] Implement PostgreSQL user/organization/membership repositories.
- [ ] Extend PostgreSQL API-key ownership queries.
- [ ] Implement principal-filtered usage and audit queries.
- [ ] Use transactions for compound identity/key mutations.
- [ ] Add migration and repository integration tests.

## Workstream C — ClickHouse storage and migration

Suggested ownership: ClickHouse migrations/repositories and ClickHouse
integration tests. This agent must not import/use PostgreSQL repositories.

- [ ] Add typed user, username lookup, organization, name lookup, membership,
      and extended key records.
- [ ] Add configuration mutation serialization/conflict behavior.
- [ ] Add fail-closed Redis distributed locking for multi-replica,
      uniqueness-sensitive ClickHouse configuration mutations.
- [ ] Add usage actor columns and explicit-column batch inserts.
- [ ] Add append-only audit table/repository.
- [ ] Normalize legacy tenant/key records into organization domain objects.
- [ ] Preserve legacy usage and mark unknown actors as legacy.
- [ ] Implement the same ownership, usage, and audit repository behavior as
      PostgreSQL.
- [ ] Add ClickHouse integration tests using the shared behavior suite.
- [ ] Prove ClickHouse mode starts and operates without PostgreSQL.

## Workstream D — Authentication and API-key use cases

Suggested ownership: `pkg/auth`, `pkg/apikey`, identity/organization use cases,
auth/key handler tests.

- [ ] Resolve master/user/organization principals during login.
- [ ] Add principal fields to signed sessions.
- [ ] Revalidate key, owner, organization, and membership state.
- [ ] Add master-only user/organization creation.
- [ ] Add optional initial user key with one-time plaintext.
- [ ] Add membership add/change/remove and last-admin protection.
- [ ] Add self-service personal and organization-scoped personal keys.
- [ ] Add organization-owned key management for organization admins.
- [ ] Add key rotation with immediate old-secret invalidation.
- [ ] Invalidate auth/key caches on every relevant mutation.
- [ ] Test disabled users/orgs, removed memberships, horizontal access, scope
      escalation, and stale cookie behavior.

## Workstream E — Gateway and usage attribution

Suggested ownership: gateway request lifecycle, usage service/entities, quota and
cache integration tests affected by access context.

- [ ] Replace stored-key-only gateway assumptions with an access context.
- [ ] Support master `/v1/models` and `/v1/chat/completions` calls.
- [ ] Isolate master cache entries.
- [ ] Enforce model/quota/RPM policies for user and organization keys.
- [ ] Enforce global versus organization credential visibility.
- [ ] Record user, organization, master, and shared-org actor snapshots.
- [ ] Ensure personal usage never acquires organization context implicitly.
- [ ] Keep quota accounting per API key and existing settlement guarantees.
- [ ] Add streaming, non-streaming, cache-hit, error, and master attribution
      tests.

## Workstream F — JSON APIs and authorization

Suggested ownership: Fiber handlers/routes/presenters and route-level tests.

- [ ] Implement typed user endpoints.
- [ ] Implement typed organization endpoints and deprecated tenant aliases.
- [ ] Implement membership endpoints.
- [ ] Extend key endpoints with owner/context and rotation.
- [ ] Add cursor-paginated principal-filtered usage endpoints.
- [ ] Add cursor-paginated audit endpoint.
- [ ] Return 403 for missing capability and conceal foreign private resources
      with 404 where specified.
- [ ] Add route tests for every master/user/member/admin/org-key role.
- [ ] Verify query filters can narrow but never broaden visibility.

## Workstream G — Server-rendered web UI

Suggested ownership: `api/views`, UI handlers, static CSS/JS, view tests.

- [ ] Add principal label and safe organization context selector.
- [ ] Add master-only Users page and create-user/initial-key flow.
- [ ] Add Organizations page and member management.
- [ ] Extend API-key UI with owner/context and rotation.
- [ ] Replace usage primary columns with Model, User, I/O, Time.
- [ ] Implement relative-under-60-seconds/RFC3339 time rendering.
- [ ] Add expandable safe usage details and filters/pagination.
- [ ] Add audit page.
- [ ] Hide unauthorized actions while retaining server enforcement.
- [ ] Validate desktop/mobile layouts and dialog bounds.
- [ ] Keep the implementation a normal web UI; do not add terminal behavior.

## Workstream H — Audit and cross-cutting integration

- [ ] Emit audit events for every required administrative mutation.
- [ ] Ensure audit writes contain only safe metadata.
- [ ] Add organization/master audit visibility tests.
- [ ] Verify user/org disablement and membership removal invalidate sessions and
      key access across all routes.
- [ ] Verify organization administrators cannot access unrelated personal usage
      or keys.
- [ ] Verify organization-owned key activity displays `org:<name>`.
- [ ] Verify master activity displays `master`.
- [ ] Verify legacy events display `legacy` without guessed attribution.

## Shared backend behavior suite

Run the same contract scenarios against PostgreSQL and ClickHouse:

- [ ] Unique normalized user email.
- [ ] Unique normalized organization name.
- [ ] Multiple organizations per user.
- [ ] Membership role and last-admin rules.
- [ ] Personal key ownership isolation.
- [ ] Organization-scoped personal key validation.
- [ ] Organization-owned key administration.
- [ ] Key login, rotation, disablement, and cache invalidation.
- [ ] User-only usage visibility.
- [ ] Organization-only usage visibility.
- [ ] Master global usage visibility.
- [ ] Cursor pagination stability.
- [ ] Audit visibility and secret exclusion.
- [ ] Legacy migration compatibility.

## Final acceptance

- [ ] `gofmt` passes for all changed Go files.
- [ ] `go vet ./...` passes.
- [ ] `go test ./...` passes.
- [ ] PostgreSQL-mode integration suite passes with ClickHouse unavailable.
- [ ] ClickHouse-mode integration suite passes with PostgreSQL unavailable.
- [ ] Live master login/user creation/initial-key smoke test passes.
- [ ] Live personal-key and organization-key login smoke tests pass.
- [ ] Live master/user/organization model call attribution is correct.
- [ ] Live user and organization usage visibility is correct.
- [ ] Browser smoke test covers users, organizations, memberships, keys, usage,
      audit, and mobile layout.
- [ ] No dual storage reads/writes appear in runtime code or logs.
- [ ] All required checklist items are checked with evidence in the final handoff.
