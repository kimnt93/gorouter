# v0.2.1: user-first access, aliases, groups and organization limits

The **user** is the top-level authenticated identity. A user has one canonical
API key, personal provider connections, and memberships in zero or more
organizations. An organization grants access to model aliases/groups without
creating another user/agent key. This supersedes the earlier workload-bound-key
proposal in v0.2.0 and the draft v0.2.1 tracking documentation.

## Identity and tracking

New user keys are personal (no fixed organization context). A second user key
creation returns `409 user_key_exists`; rotate the existing key instead. Legacy
secondary keys remain stored for historical attribution, but only the canonical
key authenticates: prefer a personal key, then earliest creation timestamp and
ID. Disabled canonical keys do not cause automatic failover to another secret.
Legacy organization-owned keys retain their old compatibility path.

No existing secrets or historical usage rows are deleted. Review users with
multiple existing keys before upgrading: secondary secrets stop authenticating.
Use master administration to select/rotate the canonical personal key and remove
obsolete keys explicitly; do not rely on a secondary key for continued access.

The caller uses the same user key with:

```yaml
X-GoRouter-Agent-Id: agent_a
X-GoRouter-Conversation-Id: conversation_123
X-GoRouter-Run-Id: run_123
X-GoRouter-Parent-Run-Id: run_parent_123
X-GoRouter-Request-Id: request_123
X-GoRouter-Trace-Id: trace_123
```

`agent_id` is caller-provided correlation **under that user**, not a principal or
quota owner. It has the same 128-byte opaque-ID validation as the other headers.
Changing it does not change permissions, provider ownership, or reset limits.
`user_id` is always obtained from authentication; no user-ID header can forge it.
Missing agent/run/trace/conversation fields remain empty. Missing request IDs are
Router-generated. New key creation rejects nonempty `workload` bindings; old
bindings are legacy metadata, not authorization. All tracking headers stay local
to Router and are not forwarded upstream.

## Publish an alias (rename a model)

Org admins with `models:manage`, or master, can publish sources they own. Sources
must be real enabled model IDs from `/admin/models`, backed by the publisher's
provider connections. A master can publish global provider sources. Member
private connections cannot be appropriated by an org admin.

```http
POST /admin/organizations/org_xno/models
Content-Type: application/json
Authorization: Bearer <organization-admin-user-key>
```

```json
{
  "name": "xno-lite",
  "kind": "alias",
  "targets": ["xno/cx/gpt-5.6-luna"],
  "enabled": true,
  "weekly_limit_usd": 20
}
```

Returns `name: "xno/xno-lite"`. The organization slug is generated server-side
from the organization name; the input `name` is only the short alias. If the
publisher's source is registered as `cx/gpt-5.6-luna`, use that exact source ID.
Renaming does not rename the actual upstream model or invent its price.

## Publish an ordered fallback group

```json
{
  "name": "default",
  "kind": "group",
  "targets": ["xno/xno-lite-01", "xno/xno-lite-02", "cx/gpt-6-astra"],
  "enabled": true,
  "weekly_limit_usd": 100
}
```

POST to the same endpoint. Returns `name: "xno/g/default"`; `/g/` is reserved for
groups. Targets are evaluated in supplied order. A group can contain enabled
aliases from the same organization and concrete models backed by the publisher's
own connections. It cannot contain other groups, itself, foreign aliases,
arbitrary member-private models, or `/auto` routes. At most 32 targets and 512
resolved routes. Existing account retries precede fallback to the next route.
Grouping does not grant direct access to every underlying alias/source.

Repeat POST with the same short name and kind to update targets, weekly limit,
or enabled state. Disable with `enabled:false`. Names are stable: changing a
name creates another resource rather than silently resetting the old identity.

## Assign a model/group to a user

```http
POST /admin/organizations/org_xno/model-grants
Content-Type: application/json
Authorization: Bearer <organization-admin-user-key>
```

```json
{
  "model": "xno/g/default",
  "user_id": "usr_member",
  "enabled": true,
  "weekly_limit_usd": 10
}
```

The user must be active and belong to that organization. This grant is keyed by
organization + model/group + user, independent of their API-key ID and agent IDs.
Repeat POST to change the per-user limit or revoke with `enabled:false`.
`GET /admin/organizations/{id}/models` and `/model-grants` list definitions and
assignments for authorized org administrators. A grant takes effect on the next
request with the existing user key; membership removal, disabled org/source,
revocation, or publisher losing admin membership removes access.

The dashboard offers **Organizations → Models and limits** for publishing,
editing limits, enabling/disabling models, and assigning/revoking members.

## Calling and listing models

`GET /v1/models` with the user key returns personal models plus enabled assigned
aliases/groups. Calls to all three inference protocols accept the published
name, for example `model: "xno/g/default"`. No organization or agent header is
required to authorize it. Unassigned names return 404. Personal requests have no
organization usage attribution. Organization requests record both the user and
the granting organization, the public alias/group, and actual upstream model.
Raw credential identifiers are not exposed in response headers for org calls.
Organization calls bypass Router response caching so grants/limits cannot be
bypassed through cached results. Provider-side prompt caching is unchanged.

## Limit rules

`weekly_limit_usd` is optional (`null`/blank means unlimited; `0` blocks calls).
Limits are Router-priced USD, **not** request counts or token limits. The window
uses the existing UTC `WEEK_START` policy (Sunday by default).

Each provider attempt reserves an estimated cost against all applicable scopes:

1. The group's shared organization limit.
2. The selected alias's shared organization limit.
3. The directly assigned public alias/group's per-user limit.

Limits are ANDed: every scope must permit the request. Unlimited scopes are
also recorded, so adding a limit later includes earlier current-week spend. Shared alias limits also
apply when it is reached through different groups. A group grant's per-user
limit is independent of a separate direct alias grant. Direct raw source calls
by the source owner are personal and not charged to the organization grant.
The existing key-wide quota/RPM checks still apply.

Consumed, successfully reported work settles to calculated cost; the total may
exceed the estimate because output is not a guaranteed upper bound. This is a
conservative admission limit, not a dollar-exact provider spending cap. Completed
HTTP 4xx rejections release that attempt's estimate. Network/5xx failures,
interrupted streams, or process crashes leave the estimate reserved because
consumption is unknown. No automatic release at midnight or agent/key rotation.
Reservations belong to their admission week even if settled in the next week.
If an alias budget is exhausted, its route is skipped; later group routes can
still succeed if all their limits permit. Exhaustion returns HTTP 429. Storage or
coordination failure returns 503, never unlimited/zero spend.

Budget facts live in the **selected** backend, separate from the asynchronous
usage log queue. PostgreSQL uses a transaction and organization-scoped advisory
lock. ClickHouse uses durable config records and a shared Redis lock; local mode
uses the SQLite repository's single-process mutation lock. No dual-write database
architecture is introduced. ClickHouse budget locks have no expiry: an uncertain
write or dead worker must fail closed, not let the next node reopen capacity.
An orphaned `gorouter:budget-lock:<organization-id>` requires an operator to
verify no writer remains, reconcile durable holds, and then explicitly clear
that lock. Never clear it automatically on an error. Ordinary user APIs do not
expose an unsafe “reset spend” operation.

## Queries

The existing `recent`, `summary`, `activity` endpoints retain their v0.2.1
multi-select filters. Agent IDs now select within user/organization visibility,
not within a key-bound agent authority. Omitted filters mean all authorized user
records. `/admin/usage/workloads/weekly` is retained for compatibility but now
queries the authorized user/org scope; `agent_id` is optional and supports
multiple IDs. Its capability marker is `gorouter-user-usage-v1`.

## Deployment and compatibility

PostgreSQL adds `0030_organization_models.sql` (definitions, grants, and budget
records). ClickHouse/SQLite reuse their existing durable JSON config stores; no
additional database is needed. The v0.2.1 trace/workload repair migrations remain
required. Org aliases/grants are additive: no existing provider/model records are
renamed or deleted by migration. Existing multiple-key integrations must move to
the canonical user key before upgrading. This version does not claim exact
provider invoice reconciliation or complete durable inference usage logging.
