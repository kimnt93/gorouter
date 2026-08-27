# Coding conventions

## Package boundaries

- Domain entities must not import Fiber, pgx, ClickHouse, Redis, or provider
  clients.
- Feature services express business behavior through consumer-owned interfaces.
- Handlers own transport parsing and presentation, not business invariants.
- Repositories own query/schema knowledge, not HTTP status decisions.
- Platform adapters translate external protocols; do not make the gateway
  switch on every provider-specific field.
- The composition root constructs concrete dependencies. Avoid hidden global
  service locators.

## Types and syntax

- Prefer concrete typed structs for JSON, SQL scan targets, domain values, and
  provider payloads. Use pointers only for meaningful optionality.
- Copy slices/maps when returning catalog or mutable state snapshots if callers
  must not mutate shared storage.
- Normalize user input once at the business boundary and persist both display
  and normalized forms where the domain requires uniqueness.
- Keep exported names/doc comments focused on public behavior; avoid comments
  that merely restate syntax.
- Use sentinel/domain errors for decisions callers must branch on. Use `%w` to
  retain causal errors internally.
- Compare secret material in constant time and store only hashes/ciphertext.
- Use UTC throughout storage and API timestamps.

## Concurrency and lifecycle

- Protect mutable process-local hints with a mutex or atomic snapshot.
- Do not hold a mutex across network/database calls.
- Respect request cancellation in provider, service, and repository calls.
- Bound goroutines, queues, request bodies, response bodies, timeouts, and
  streaming buffers.
- Drain or settle usage/quota work during errors and shutdown where possible.
- Treat queueing as a durability buffer, not as the authoritative quota total.

## Change quality

- Keep behavior changes and their tests in the same patch.
- Avoid compatibility aliases in new domain code unless a migration contract
  requires them.
- Preserve typed, fail-closed behavior instead of adding permissive fallbacks.
- Do not edit `.env`, generated Swagger, or generated SPA files by hand.
- Run `gofmt`, focused tests, full tests, vet, frontend tests/build when
  relevant, and `git diff --check`.
