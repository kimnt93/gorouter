---
name: solution-design
description: Design, review, or decompose GoRouter features involving identity, organizations, ownership, View As context, model namespaces, routing, quotas, pricing, cache, usage, privacy, or cross-layer architecture. Use before implementing ambiguous or cross-cutting behavior and for security/design reviews. Do not use for a narrow mechanical edit whose contract is already settled.
---

# Solution design

Read [domain-invariants.md](references/domain-invariants.md) for the authorization
and ownership model. Read [design-checklist.md](references/design-checklist.md)
when proposing or reviewing a feature.

## Design method

1. State the actor, owned resource, optional organization context, capability,
   allowed visibility, and billing/usage attribution for every operation.
2. Separate personal, organization, and master behavior. Organization context
   is a narrowing **View As** lens, not an authorization shortcut or mutation
   of stored key ownership.
3. Define the public model namespace and the eligible credentials/routes. Keep
   personal/global IDs at two segments and organization-owned IDs prefixed by
   the organization slug.
4. Trace request state in order: authentication, model allowlist, cache
   namespace, quota/RPM, route selection, provider call, translation,
   settlement, usage/audit, and cache store.
5. Decide which state is durable, which is Redis-coordinated, and which may be
   a process-local hint. Assume multiple router replicas.
6. Define typed API/repository contracts, safe errors, privacy boundaries,
   migrations for both backends, UI behavior, and verification before coding.

## Reject designs that

- infer authority from organization IDs supplied by the browser;
- expose member personal connections or unrelated personal usage to an org
  admin;
- combine provider cache-token metrics with the router response cache;
- reconstruct historical actor identity from mutable current records;
- create an `unpriced` billing state instead of explicit Free/zero;
- require both PostgreSQL and ClickHouse in one runtime;
- use process-local correctness state where replicas need Redis coordination;
- encode provider reasoning settings in the model name;
- add product scope unrelated to a centralized LLM gateway.
