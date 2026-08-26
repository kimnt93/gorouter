# Storage and Migration Contract

## 1. Repository parity

Define domain-facing repositories/use cases for:

- Users.
- Organizations.
- Memberships.
- API keys with owner/context queries.
- Principal-filtered usage.
- Audit events.

Both PostgreSQL and ClickHouse implementations satisfy the same interfaces and
behavior tests. The composition root constructs one complete implementation set
based on `DATABASE_BACKEND`.

Forbidden architecture:

```text
PostgreSQL config + ClickHouse usage in one runtime
ClickHouse lookup falling back to PostgreSQL
dual writes for migration or analytics
cross-backend synchronization jobs
```

## 2. Backend-owned identity and time

The Go backend owns all IDs and timestamps.

- Services or repositories generate IDs with `entities.NewID` before inserts.
- Services or repositories set `time.Now().UTC()` before inserts/updates.
- Every insert explicitly includes ID/time fields.
- Database schemas and migrations do not use serial identity, UUID generators,
  `now()`, `current_timestamp`, or equivalent defaults for IDs/times.
- Extend the existing migration ownership test to cover all new files.

## 3. PostgreSQL implementation

PostgreSQL may and should use relational integrity.

### 3.1 Tables

#### `users`

```text
id TEXT PRIMARY KEY
username TEXT NOT NULL
normalized_username TEXT NOT NULL UNIQUE
status TEXT NOT NULL
created_at TIMESTAMPTZ NOT NULL
updated_at TIMESTAMPTZ NOT NULL
```

#### `organizations`

```text
id TEXT PRIMARY KEY
name TEXT NOT NULL
normalized_name TEXT NOT NULL UNIQUE
status TEXT NOT NULL
created_at TIMESTAMPTZ NOT NULL
updated_at TIMESTAMPTZ NOT NULL
```

#### `organization_memberships`

```text
organization_id TEXT NOT NULL REFERENCES organizations(id)
user_id TEXT NOT NULL REFERENCES users(id)
role TEXT NOT NULL
created_at TIMESTAMPTZ NOT NULL
created_by_actor_type TEXT NOT NULL
created_by_actor_id TEXT NOT NULL
PRIMARY KEY (organization_id, user_id)
```

#### `api_keys`

Add:

```text
owner_type TEXT NOT NULL
owner_user_id TEXT NULL REFERENCES users(id)
owner_organization_id TEXT NULL REFERENCES organizations(id)
context_organization_id TEXT NULL REFERENCES organizations(id)
```

Add a check enforcing exactly one matching owner. Organization-context
membership validity remains a use-case check because it spans mutable rows.

#### `usage_events`

Add:

```text
actor_type TEXT NOT NULL
user_id TEXT NOT NULL
username TEXT NOT NULL
organization_id TEXT NOT NULL
```

Usage actor IDs are snapshots and intentionally do not require foreign keys.

Indexes must support:

- `(user_id, ts DESC, event_id DESC)`.
- `(organization_id, ts DESC, event_id DESC)`.
- Existing key/time quota sums.
- Filtered model/status history where query plans require it.

#### `audit_events`

Use an append-only table ordered/indexed by timestamp and event ID, with indexes
for organization and actor visibility.

### 3.2 Transactions

Use transactions for compound mutations such as:

- Creating a user plus initial key.
- Membership role changes with last-admin validation.
- Key rotation and old-hash invalidation.
- Disabling a user and invalidating affected keys where materialized state is
  updated.
- Writing mutation result plus audit event when practical.

## 4. ClickHouse implementation

ClickHouse mode does not use PostgreSQL tables, relations, joins, or foreign
keys. Store mutable configuration as versioned/denormalized records using the
existing `config_records` pattern.

### 4.1 Configuration entities

Use explicit typed payloads and entity names such as:

```text
user                         key = user ID
user_username                key = normalized username, payload = user ID
organization                 key = organization ID
organization_name            key = normalized name, payload = organization ID
organization_membership      key = organization ID + ":" + user ID
api_key                      key = API key ID
api_key_hash                 key = secret hash, payload = API key ID
```

API-key records denormalize owner labels/context needed for safe listing, but
authorization re-reads current owner and membership state.

Repository methods validate logical references in Go before appending a new
record version. Deletes append tombstones; they do not erase historical usage.

Username/name reservation records provide lookup and conflict detection.
Configuration mutations must be serialized per normalized username,
organization name, membership, or key. Use the configured Redis coordination
service for a bounded distributed lock when more than one router replica can
perform administration. If the lock cannot be acquired or Redis is unavailable,
the uniqueness-sensitive mutation fails closed; Redis is coordination only and
never stores the durable identity record. A declared single-writer deployment
may use process-level keyed locks. Model traffic and usage ingestion do not
require this configuration lock.

### 4.2 Usage table

Add actor columns to the ClickHouse `usage_events` table. Inserts include an
explicit column list rather than relying on physical table column order.

Use organization/user fields in the sort key or data-skipping indexes only when
benchmarks justify a table rebuild. Correctness must not depend on sort-key
placement.

### 4.3 Audit table

Use a dedicated append-only `audit_events` MergeTree table. Do not store audit
history as replaceable configuration records. Audit events carry backend-owned
event IDs so retries can be deduplicated in queries. A configuration mutation
must report failure if its required audit append fails, and retries must use the
same operation/event identity where the use case supports retry.

## 5. Migration

### 5.1 PostgreSQL

In a versioned migration/use-case sequence:

1. Create users, organizations, memberships, and audit structures.
2. Copy existing tenant rows to organizations preserving IDs/names/timestamps.
3. Add nullable key ownership columns.
4. Backfill existing keys as organization-owned using their legacy tenant ID.
5. Validate and make ownership columns/constraints final.
6. Add usage actor columns.
7. Backfill existing usage as `legacy`, preserving tenant as organization.
8. Add indexes and constraints.

Static backfill labels such as `legacy` are allowed. SQL must not generate an ID
or current timestamp.

### 5.2 ClickHouse

1. Convert/list existing tenant config records as organization records while
   preserving IDs.
2. Rewrite or lazily normalize existing API-key payloads as organization-owned.
3. Add usage actor columns with migration-safe static defaults for old parts,
   while all new inserts supply explicit values.
4. Preserve old records for compatibility reads until the migration is proven.

### 5.3 Compatibility

Repository code may temporarily read legacy tenant/key payloads and normalize
them into the new domain representation. New writes use only the new shape.

## 6. Cache invalidation

Invalidate API-key/session lookup caches when:

- A key changes or rotates.
- A user is disabled.
- An organization is disabled.
- Membership is removed or changes role.
- Key owner/context-dependent authorization changes.

Cache keys must include enough version/identity information to prevent stale
authorization from crossing principals or organizations.
