# Feature design checklist

Answer the relevant questions before implementation.

## Product boundary

- Does the feature strengthen centralized LLM access, routing, pricing, quota,
  usage, cache, identity, or provider management?
- Is a smaller extension of an existing concept sufficient?
- Which current spec/checklist section owns the contract, and is any older
  statement superseded?

## Authorization matrix

For master, ordinary user, organization member, organization admin, and
organization key, define:

- required capability/scope;
- owned object and active-status requirements;
- list/read/create/update/delete/rotate permissions;
- visibility filter and 403 versus concealed 404 behavior;
- whether View As is available and how it narrows results;
- cache invalidation required after mutations.

## Request and attribution

- What is the public model name and upstream model?
- Which credentials/routes are eligible for personal and organization calls?
- What is the cache namespace?
- Which key owns quota/RPM and which principal/organization receives usage?
- How are four token and cost components obtained or estimated?
- What happens when pricing is absent, Redis is down, the provider streams an
  error, or the client disconnects?

## State placement

- Which records are durable in the selected PostgreSQL or ClickHouse backend?
- What changes are required in both schemas and repository implementations?
- Which state must be atomic/shared in Redis across replicas?
- Is any process-local state merely a performance snapshot or health hint?
- Who generates IDs/timestamps, and how is uniqueness/locking enforced?

## Contracts and privacy

- Are request, response, persistence, provider, and filter shapes typed?
- Can query filters only narrow server-computed visibility?
- Which response/error fields are safe for API, UI, logs, and audit?
- Are prompts/completions or raw upstream bodies excluded?
- Does one-time plaintext stay out of lists, browser persistence, and audit?

## Delivery and proof

- Which Fiber routes/Swagger contracts change?
- Which React contracts, reusable components, pages, and View As flows change?
- Which generated files must be rebuilt?
- What unit, shared-backend, distributed Redis, route, React, browser, and live
  evidence proves the behavior and its denial paths?
