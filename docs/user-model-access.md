# v0.2.1 — User keys, one-to-one aliases and assignment limits

**Final contract:** the user is the authenticated owner. Agents are request
tracking beneath that user. One canonical API key accesses personal models and
models assigned by users or organizations. Groups are only bulk-assignment
packages: they never appear in inference model listings and are not callable.
This replaces the earlier callable-group/shared-limit draft.

## Names and one-to-one mappings

| Resource | Public name | Example |
|---|---|---|
| Original personal model | Existing provider ID | `cx/gpt-6-astra` |
| Personal alias | `<username>/<alias>` | `kimnt93/gpt-5.5` |
| Organization alias | `org/<organization-slug>/<alias>` | `org/xno/gpt-5.6-luna` |
| Assignment package | Internal `org/<organization-slug>/g/<package>` | Not listed/callable |

Each alias points to exactly one original model, which may have several provider
accounts serving that same model. The public alias is unique, and each owner can
publish only one alias for a source. Conflicting names, retargeting an existing
alias, or adding a second alias for the same source return `409 alias_conflict`.
Disabled aliases retain their mapping. These checks are serialized across
replicas (PostgreSQL transactional lock, ClickHouse Redis lock) and in local mode.
Do not treat aliases as independent routing blends or rename a provider model ID.

For email-based Router usernames, the personal prefix uses the normalized local
account name (`kimnt93@example.test` → `kimnt93`); namespace collisions are
rejected, not silently reassigned. Provider-reserved prefixes and `org` cannot
be personal alias namespaces.

When the owner aliases `cx/gpt-5.5` as `kimnt93/gpt-5.5`, **both IDs remain callable
by that owner**, but the alias replaces the original in `/v1/models` and the
user's dashboard model catalog. Grantees can call only the assigned alias, not
its private raw source. A typical user's list is:

```json
["cx/gpt-6-astra", "org/xno/gpt-5.6-luna", "kimnt93/gpt-5.5"]
```

## Canonical user key and tracking

New user keys are personal, not fixed to one organization or agent. Duplicate
creation returns `409 user_key_exists`; rotate instead. Legacy secondary keys
remain stored for history but no longer authenticate. Canonical selection prefers
an existing personal key, then earliest creation timestamp and ID; disabling the
canonical key does not activate another secret. Review existing multi-key users
before upgrading. No secret or retained usage is deleted by this migration.
Legacy organization-owned keys retain their compatibility path.

```yaml
X-GoRouter-Agent-Id: agent_a
X-GoRouter-Conversation-Id: conversation_123
X-GoRouter-Run-Id: run_123
X-GoRouter-Parent-Run-Id: run_parent_123
X-GoRouter-Request-Id: request_123
X-GoRouter-Trace-Id: trace_123
```

Use these on Chat Completions, Responses, or Messages, streaming or non-streaming.
`user_id` is always authenticated. Agent IDs and other correlation headers cannot
change grants, ownership, or quotas. Each optional ID is limited to 128 bytes and
ASCII letters/digits plus `-_.:/`. Missing agent/run/trace/conversation IDs remain
empty; missing request ID is generated and echoed. New key creation rejects
workload bindings. Old binding fields are metadata, not authority.

## Publish organization aliases and assignment packages

The caller must be an active org admin with `models:manage`, or master. Only
sources backed by the publisher's own connections may be published (master can
publish global sources). Org admins cannot appropriate members' personal keys.

```http
POST /admin/organizations/org_xno/models
Authorization: Bearer <org-admin-user-key>
Content-Type: application/json
```

```json
{
  "name": "gpt-5.6-luna",
  "kind": "alias",
  "targets": ["cx/gpt-5.6-luna"],
  "enabled": true,
  "weekly_limit_usd": 0
}
```

Returns `name: "org/xno/gpt-5.6-luna"`. Use the actual configured source ID from
your connection catalog. No org/global cap is set here: nonzero definition limits
are rejected; budgets belong to user assignments only.

A package is created at the same endpoint:

```json
{
  "name": "default",
  "kind": "group",
  "targets": ["org/xno/gpt-5.6-luna", "org/xno/gpt-6-astra"],
  "enabled": true,
  "weekly_limit_usd": 0
}
```

Packages contain up to 32 enabled aliases from the same organization, not nested
packages, raw sources or aliases owned by someone else. Applying a package
creates individual grants. Existing grants retain their individual limits;
editing a package later does not silently change earlier grants. Bulk assignment
is sequential and idempotent, not a cross-record transaction: on partial failure,
retry or inspect the resulting grants. Package names are never sent upstream.

## Assign a model to different users with different limits

```http
POST /admin/organizations/org_xno/model-grants
Authorization: Bearer <org-admin-user-key>
Content-Type: application/json
```

```json
{
  "model": "org/xno/gpt-5.6-luna",
  "user_id": "usr_a",
  "enabled": true,
  "weekly_limit_usd": 500
}
```

Repeat for `usr_b` with a different amount. Each organization + alias + user has
its own budget. `0`, omitted, or `null` means **unlimited**, not disabled. Revoke
with `enabled:false`; setting zero is not revocation. Repeat POST to edit that
user's limit. A package name applies its member aliases individually, using the
provided limit only for new assignments. The response is an array of affected
grants, even when assigning a single alias. Users must be active org members.

Read definitions and grants through:

```http
GET /admin/organizations/org_xno/models
GET /admin/organizations/org_xno/model-grants
```

These lists require org administration. Dashboard:
**Organizations → Models and limits**. No additional user API key is created.

## Personal aliases and user-to-user sharing

Users with `models:manage` can rename their own models:

```http
POST /admin/model-aliases
Authorization: Bearer <personal-user-key>
Content-Type: application/json
```

```json
{"name":"gpt-5.5","kind":"alias","targets":["cx/gpt-5.5"],"enabled":true,"weekly_limit_usd":0}
```

List owned aliases with `GET /admin/model-aliases`. No user ID is accepted as an
owner override. The username comes from the authenticated Router user. Share
that alias with an active Router user:

```http
POST /admin/model-grants
Authorization: Bearer <alias-owner-user-key>
Content-Type: application/json
```

```json
{"model":"kimnt93/gpt-5.5","user_id":"usr_recipient","enabled":true,"weekly_limit_usd":25}
```

This response is one grant object. The original owner always retains their own
raw/alias access. Recipients cannot republish someone else's assigned source.
Shared personal usage is attributed to the recipient, not an organization.

## Additional personal limit on an assigned model

```http
POST /admin/model-limits
Authorization: Bearer <recipient-user-key>
Content-Type: application/json
```

```json
{"model":"org/xno/gpt-5.6-luna","weekly_limit_usd":100}
```

Requires `chat` and access to that assigned model. The assigning org's $500 and
the user's $100 caps both apply. Setting personal cap to 0 removes only the
personal cap; it never changes the org's $500 cap. The assigner cannot overwrite
this separate self-limit through the assignment endpoint. Dashboard:
**Models → Aliases and my limits**.

## Accounting and failure behavior

Only **assigned models** get assignment/self budgets. Own raw models and aliases
have no new model cap (existing key quota/RPM checks remain). The week follows
UTC `WEEK_START`, Sunday by default. Unlimited assignment scopes still record
spend, so adding a cap later includes earlier current-week consumption. Changing
agent IDs, rotating keys, reassigning packages, or editing caps cannot reset it.

Before each attempt, estimated cost is durably reserved against that recipient's
assignment and optional self cap together. Successful work settles to Router
model-priced cost. Output can exceed the estimate, so this is conservative
admission—not a guarantee of an exact dollar ceiling or provider invoice total.
Model limits follow original upstream pricing, not the alias spelling.

- Exhaustion: **429** `organization_model_quota_exceeded`.
- Backend/coordination failure: **503**, never unlimited access.
- Completed HTTP 4xx rejection releases that attempt's estimate.
- Network/5xx failures, interrupted streams, or process loss leave estimates
  reserved because consumption is unknown; operators must reconcile them.
- Holds belong to their admission week, including after a week boundary.
- Aliased requests bypass Router response caching; provider-side caching remains.

Budget records use the selected durable backend only. PostgreSQL uses a
transactional advisory lock. ClickHouse uses durable config records plus a shared
nonexpiring Redis fence; after a dead writer/uncertain acknowledgement an operator
must reconcile holds and verify no writer remains before clearing
`gorouter:budget-lock:<owner-scope>`. Never auto-clear on errors. Namespace locks
use the same fail-closed mechanism. Local mode uses SQLite's single-process
mutation serialization. No extra database architecture is introduced.

## Usage queries and permissions

Existing multi-select usage filters remain: user, agent, request, run, parent
run, trace and conversation; absent means all **authorized** records. Personal
calls stay personal; org-assigned calls record recipient and organization plus
alias and original upstream model. `/admin/usage/workloads/weekly` remains as a
user/org-scoped compatibility path (`gorouter-user-usage-v1`). It does not restore
per-agent key authority. Ordinary logs still have the documented persistence and
unknown-usage limits; do not treat successful inference as a durable log receipt.

## Deployment

PostgreSQL requires 0030 plus v0.2.1 repair/trace migrations 0028/0029.
ClickHouse/SQLite reuse their durable config stores for definitions/grants/budgets.
No schema rewrite is needed for this clarification. The previous callable-group
commit was **not deployed** to the target server; its staged binary was replaced
before service recreation. Existing legacy group records, if any elsewhere,
are not callable and must be reapplied as individual assignments.
