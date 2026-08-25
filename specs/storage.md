# Storage Specification

## PostgreSQL

PostgreSQL is the source of truth for transactional configuration:

- Tenants
- Credential metadata and encrypted secret blobs
- API keys and scopes
- Models
- Model routes
- Prices
- Usage events in the PostgreSQL deployment profile

Migrations must be versioned, idempotent, transactional where practical, and run at startup before serving traffic.

## Required Tables

### `tenants`

`id`, unique `name`, `created_at`.

### `credentials`

`id`, name, provider, kind, base URL, encrypted API-key blob, encrypted OAuth blob, preview, status, owner tenant, timestamps.

### `api_keys`

`id`, tenant, name, hash, prefix, JSON model list, JSON scope list, monthly quota, RPM, enabled, timestamp.

### `models`

Name, strategy, upstream model alias, enabled, timestamp.

### `model_routes`

Model, credential, priority, weight, enabled. Unique model/credential pair.

### `prices`

Model, input/output/cache-read/cache-write prices, updated timestamp.

### `usage_events`

Timestamp, tenant, key, credential, model fields, token fields, cost, cache hit, status, duration, safe error.

Indexes must support API-key/month quota sums, time-range summaries, model summaries, and recent events.

## Redis

Redis stores:

- Prompt-cache entries
- Cache hit/miss/store counters
- Distributed quota reservations
- RPM counters
- Optional health/circuit-breaker state

Redis must not be the only durable store for OAuth refresh tokens or authorization configuration.

The ClickHouse deployment profile adds ClickHouse for high-volume usage analytics. PostgreSQL remains present for transactional tenants, credentials, API keys, models, routes, and prices because ClickHouse is not a replacement for those relational authorization queries in this implementation.

## PostgreSQL vs ClickHouse

They are not interchangeable by changing a schema. PostgreSQL provides transactional relational configuration. ClickHouse is optimized for append-heavy analytics. Start with PostgreSQL and isolate usage persistence so ClickHouse can be introduced later.
