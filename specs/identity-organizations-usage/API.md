# HTTP API Contract

All endpoints use the existing authentication middleware and typed JSON error
envelope. UI form handlers may wrap the same use cases but do not replace JSON
API coverage.

## 1. Login response

`POST /login` accepts the existing JSON or form `key` value.

The JSON success response adds resolved principal information:

```json
{
  "ok": true,
  "principal_type": "user",
  "user_id": "usr_123",
  "username": "user@example.com",
  "organization_id": "org_123",
  "membership_role": "member",
  "scopes": ["chat", "usage:read"]
}
```

Fields not applicable to the principal are omitted or empty according to the
project's established typed-response convention.

## 2. Users (master only)

### `GET /admin/users`

List users with cursor pagination and optional `q` and `status` filters.

### `POST /admin/users`

```json
{
  "username": "user@example.com",
  "generate_initial_key": true,
  "initial_key": {
    "name": "Initial login key",
    "models": ["cx/gpt-5.6-sol"],
    "scopes": ["chat", "usage:read"]
  }
}
```

Returns the user and optional one-time `plaintext` key.

### `GET /admin/users/:id`

Master returns the user, memberships, safe key metadata, and aggregate usage.

### `PATCH /admin/users/:id`

Initially supports only:

```json
{ "status": "disabled" }
```

No username mutation is supported in the first implementation.

## 3. Organizations

### `GET /admin/organizations`

- Master sees all organizations.
- User sees organizations where they have an active membership.
- Organization principal sees its own organization.

Supports cursor pagination and `q`/`status` filters.

### `POST /admin/organizations` (master only)

```json
{ "name": "Acme Corporation" }
```

### `GET /admin/organizations/:id`

Requires master, membership, or matching organization principal.

### `PATCH /admin/organizations/:id` (master only initially)

Supports safe name/status fields. Name changes are audited.

## 4. Memberships

### `GET /admin/organizations/:id/members`

Allowed for master and organization administrators. Ordinary users can instead
read their own membership from the organization detail response.

### `POST /admin/organizations/:id/members`

```json
{
  "user_id": "usr_123",
  "role": "member"
}
```

Requires master or target-organization admin plus `members:manage`.

### `PATCH /admin/organizations/:id/members/:user_id`

```json
{ "role": "admin" }
```

### `DELETE /admin/organizations/:id/members/:user_id`

Removes the membership and immediately revokes organization-context access.

## 5. API keys

Extend `POST /admin/api-keys`:

```json
{
  "name": "Acme development",
  "owner_type": "user",
  "owner_user_id": "usr_123",
  "context_organization_id": "org_acme",
  "models": ["cx/gpt-5.6-sol"],
  "scopes": ["chat", "usage:read"],
  "quota_usd": 25,
  "quota_period": "week",
  "rpm": 60
}
```

For self-service user creation, `owner_user_id` may be omitted and is forced to
the session user. For organization-key creation:

```json
{
  "name": "Acme automation",
  "owner_type": "organization",
  "owner_organization_id": "org_acme",
  "context_organization_id": "org_acme",
  "models": ["cx/gpt-5.6-sol"],
  "scopes": ["chat", "usage:read", "keys:manage", "members:manage"]
}
```

List, patch, and delete endpoints apply the ownership policy in `SPEC.md`.

### `POST /admin/api-keys/:id/rotate`

Rotates only the secret. Returns a typed response containing the one-time
plaintext, new prefix, and rotation timestamp.

### Key list filters

`GET /admin/api-keys` supports:

```text
owner_type
owner_id
organization_id
status
cursor
limit
```

Filters may narrow but never broaden session visibility.

## 6. Usage

### `GET /admin/usage/summary`

Retain `range` and add optional `organization_id` and `user_id`. Master can
select any permitted target. Other principals are constrained by policy.

### `GET /admin/usage/recent`

Query parameters:

```text
cursor
limit                   default 100, maximum 500
since                   RFC3339
until                   RFC3339
organization_id
user_id
model
api_key_id
status
```

Response:

```json
{
  "object": "list",
  "data": [
    {
      "id": "usage_123",
      "ts": "2026-08-26T04:12:08Z",
      "actor_type": "user",
      "user_id": "usr_123",
      "username": "user@example.com",
      "organization_id": "org_acme",
      "api_key_id": "key_123",
      "model": "cx/gpt-5.6-sol",
      "prompt_tokens": 1240,
      "completion_tokens": 318,
      "status_code": 200,
      "duration_ms": 2418
    }
  ],
  "next_cursor": "opaque-value"
}
```

The cursor is opaque and stable for ordering by descending timestamp and event
ID. Do not expose an offset as the primary pagination mechanism.

## 7. Audit

### `GET /admin/audit/events`

Master sees all. Organization administrators/keys see events for their
organization. Supports cursor, time, actor, action, and target filters.

No endpoint returns secret-bearing audit metadata.

## 8. Deprecated compatibility

`GET/POST /admin/tenants` remain aliases for organization list/create during
one compatibility release. Responses include a deprecation header. New UI and
tests use `/admin/organizations`.
