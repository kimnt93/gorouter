# Redis coordination

## Redis-owned runtime state

Production replicas share Redis for:

- deterministic prompt-cache entries and counters;
- API-key quota reservations/settlement and RPM windows;
- API-token/session lookup invalidation;
- OAuth/device flow state and single-use consumption;
- provider quota snapshots, exhaustion cooldowns, and active accounts;
- route round-robin positions, explicit session-to-credential affinity, and
  provider health cooldowns;
- pricing snapshot invalidation;
- ClickHouse uniqueness-sensitive configuration mutation locks.

Redis must not be the only durable location for users, organizations,
memberships, API-key authorization configuration, encrypted provider
credentials, model routes/prices, usage, audit, or OAuth refresh tokens.

## Design rules

- Include every isolation dimension in keys: key/user/organization/global scope,
  model/credential identity, and version/window where relevant.
- Use SHA-256 canonical request hashes for prompt cache; never raw prompt text
  in keys.
- Use Lua/atomic commands or transactions for read-modify-write invariants.
- Give ephemeral keys bounded TTLs aligned with session, quota window, cooldown,
  cache, or flow lifetime.
- On authorization mutations, invalidate the affected token/session caches
  immediately across replicas.
- Strict outage policy fails closed for quota/RPM and ClickHouse locking.
- Process-local maps may be explicit development or Redis-error hints, but they
  cannot silently become the production source of correctness.

## Prompt-cache isolation

- Default scope is API key.
- Tenant/organization sharing is opt-in and remains inside one organization.
- Global sharing is explicit and must not contain private cross-principal data.
- Cache only deterministic requests under the current gate.
- A router cache hit records `cache_hit=true` and zero provider cost. It does
  not become provider cache-read tokens.

## Provider credential affinity

- Affinity is routing coordination, not a response-cache entry or durable
  credential assignment.
- Use only explicit client cache/session/conversation identity. A derived
  system/tools prefix can be common to unrelated conversations and would
  collapse round-robin distribution.
- Hash the API-key/global scope, tenant, model, and affinity value into the
  Redis key. Never store raw session or prompt-cache identifiers in key names.
- Apply affinity to round-robin blends; priority/fill-first routing is already
  deterministic. Refresh a bounded TTL while active and rebind after successful
  failover so the next turn follows the credential that actually worked.
- Affinity improves provider cache reuse but is not correctness-critical. If
  Redis is unavailable, continue with the normal eligible routing order; do not
  weaken authorization, quota, or credential visibility.

## Tests

Exercise two service instances against one Redis, affinity continuity and
isolation, atomic contention, TTL/end-of-window behavior, invalidation after
mutations, strict outage behavior, and namespace isolation. Miniredis is useful
for units; retain real Redis integration coverage for scripts/commands or
behavior miniredis cannot model.
