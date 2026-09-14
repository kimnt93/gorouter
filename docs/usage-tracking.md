# Usage tracking API (GoRouter v0.2.1)

This is an application-neutral integration contract for coding clients, services,
and agent runtimes. It tracks **agent, user, request, run, parent run, trace, and
session/conversation** without requiring prompt/completion capture.

## 1. Trusted identity: agent and user

GoRouter derives `user_id` from the authenticated principal and `agent_id` from
an immutable API-key workload binding. Do **not** use `X-Agent-ID`, `X-User-ID`,
OpenAI `user`, or a model's generated response ID as an authorization mechanism.
Those values cannot replace the authenticated accounting owner.

Create workload-bound keys through an authorized `POST /admin/api-keys` call
(`keys:manage` and existing owner/model-grant policy):

```json
{
  "name": "Worker A",
  "owner_type": "user",
  "owner_user_id": "usr_example",
  "models": ["openai/example-model"],
  "scopes": ["chat", "usage:read"],
  "workload": {
    "application": "example-app",
    "environment": "production",
    "workspace_id": "workspace_example",
    "agent_id": "agent_a"
  }
}
```

Replace the example model with an allowed, configured model. Workload application,
workspace, and agent are required together; environment is optional. Key creation
requires the existing authorized management identity (master or permitted org
admin), not an ordinary chat-only caller. Rotation retains the binding. Use a
separate key for each agent/user authority; application namespaces are arbitrary,
not tied to a particular consuming project.

## 2. Request headers and defaults

Send these headers on any of `/v1/chat/completions`, `/v1/responses`, or
`/v1/messages`, with or without streaming:

```yaml
X-GoRouter-Conversation-Id: conversation_123
X-GoRouter-Run-Id: run_123
X-GoRouter-Parent-Run-Id: run_parent_123
X-GoRouter-Request-Id: request_123
X-GoRouter-Trace-Id: trace_123
```

| Tracked dimension | Source | Default if omitted | Usage record / query field |
|---|---|---|---|
| Agent | API key `workload.agent_id` | Unattributed | `agent_id` |
| User | Authenticated Router user | Empty for organization/master actors | `user_id` |
| Request | `X-GoRouter-Request-Id` | Router generates a unique ID | `logical_request_id` |
| Run | `X-GoRouter-Run-Id` | Empty | `run_id` |
| Parent run | `X-GoRouter-Parent-Run-Id` | Empty | `parent_run_id` |
| Trace | `X-GoRouter-Trace-Id` | Empty | `trace_id` |
| Session/conversation | `X-GoRouter-Conversation-Id` | Empty | `conversation_id` |

Use `conversation_id` as the canonical session field; there is no separate
`session_id` accounting header/query alias. Provider session-affinity fields and
W3C `traceparent` remain separate and are not automatically interpreted as these
application correlation fields. Reuse a trace across related runs and a
conversation across turns. Use a new request ID for each independent inference
call. A request ID is **correlation, not inference idempotency**: repeating it
still makes another inference request. Usage event `id` remains independent.

Each supplied ID is trimmed, limited to 128 bytes, and allows ASCII letters,
digits, `.`, `_`, `-`, `:`, `/`. Invalid values return 400 before provider work.
Headers contain one ID each, not CSV. Do not put personal content or secrets in
IDs. Once tracking is validated, the response includes
`X-GoRouter-Request-Id` (caller supplied or generated), including stream headers.
Tracking headers are copied into request-owned state for asynchronous streams,
persisted as metadata, and never forwarded to providers or used to alter prompt
cache keys. Traces remain absent on older records; no historical IDs are guessed.

## 3. Query endpoints

All reads require `usage:read` and object-level authorization. Use a management
read identity for authorized cross-agent queries. A workload-bound key is
restricted to its own exact application/environment/workspace/agent, even when
filters are omitted. A user-owned workload key is additionally restricted to its
user/context; owning an org-admin account does not broaden that workload key.

| Endpoint | Result | Time behavior |
|---|---|---|
| `GET /admin/usage/recent` | `{object:"list", data:[], next_cursor?:"…"}` | All retained time by default; optional `since`/`until` |
| `GET /admin/usage/summary` | Request/token/cache/cost summary | Default 24h; `range=7d`, `30d`, `all`, or explicit time bounds |
| `GET /admin/usage/activity` | `{group_by, data, summary, health}` | Default rolling 7d; existing range presets; explicit time bounds |
| `GET /admin/usage/events/{id}` | One usage event and existing opt-in content availability fields | ID lookup; optional single organization/View As context |
| `GET /admin/usage/workloads/weekly` | Existing `gorouter-workload-usage-v1` weekly envelope | Exact `quota.Window("week")`, default Sunday UTC, configured `WEEK_START` |

Summary, activity, and recent accept:

```text
agent_id, user_id, logical_request_id, run_id, parent_run_id, trace_id,
conversation_id, application, environment, workspace_id,
provider, credential_id, model, api_key_id, organization_id
```

Each dimension supports **CSV or repeated query parameters**, including both at
once. Values within a dimension use OR; different dimensions use AND. Omitted
(or empty) filters mean **all authorized values**, not global access. Do not send
literal `all` or `*` as an ID wildcard. At most 100 supplied selections per
dimension, at most 128 bytes each. Empty CSV elements, invalid characters, and
oversized selections return 400. Existing single-value callers still work.

`organization_id` is also an authorization context: only master may request
multiple organizations; ordinary users can narrow to one authorized context.
Organization admins/keys see that organization's records only. Members see only
their own records within selected context. Personal usage cannot be exposed by
org filters. Master View As narrows, never broadens, access. Missing scope is
403; a foreign detail ID is concealed with 404. Foreign selections on aggregate
or list queries are intersected with visibility and therefore yield no matches.

Weekly is a **self-binding endpoint**: it still requires a workload-bound key,
not a master key, and defaults to that binding. Run, parent-run, trace,
conversation, request, and user selections further narrow the week's totals.
Explicit attempts to change the weekly key's binding return 403. Use the other
three endpoints for authorized multi-agent queries.

### Examples

Headers below show placeholders; pass a real key securely, never commit it.

```http
GET /admin/usage/recent?agent_id=agent_a,agent_b&trace_id=trace_123&limit=100
Authorization: Bearer <usage-read-key>
```

```http
GET /admin/usage/summary?range=all&user_id=usr_a&user_id=usr_b&parent_run_id=parent_a,parent_b
Authorization: Bearer <authorized-management-read-key>
```

```http
GET /admin/usage/activity?group_by=hour&since=2026-09-07T00%3A00%3A00Z&until=2026-09-14T00%3A00%3A00Z&conversation_id=conversation_123&run_id=run_123
Authorization: Bearer <usage-read-key>
```

```http
GET /admin/usage/workloads/weekly?trace_id=trace_123,trace_456
Authorization: Bearer <workload-bound-usage-read-key>
```

An illustrative event fragment (other existing fields remain unchanged):

```json
{
  "id": "usage_example",
  "user_id": "usr_example",
  "application": "example-app",
  "workspace_id": "workspace_example",
  "agent_id": "agent_a",
  "logical_request_id": "request_123",
  "run_id": "run_123",
  "parent_run_id": "run_parent_123",
  "trace_id": "trace_123",
  "conversation_id": "conversation_123"
}
```

Recent lists default to 100 records, maximum 500. Pass the returned opaque
`next_cursor` unchanged along with the same filters until it is absent. The
ordering is `(ts DESC, event ID DESC)`; permission filtering happens before
pagination. An invalid cursor returns 400. Timestamps are RFC3339, with
half-open `[since, until)` semantics. Explicit summary/activity bounds override
preset bounds. `range=7d` is still rolling seven days, not quota week.

## 4. Accounting limits and safety

These are metadata/filter improvements, not a new billing or durable accounting
engine. Aggregation runs in Router repositories. Costs remain Router-priced,
not provider invoices. Provider cache tokens and Router response-cache hits are
separate. Requests represented by persisted events, internal provider attempt
counts, and external client retries are not interchangeable. Not every failed
provider attempt currently has its own ledger record.

The existing synchronous bound-event path and async unbound-event queue are
unchanged. This version does **not** claim crash-safe handoff, complete in-flight
visibility, exactly-once settlement, or strict concurrent budget enforcement.
Persistence errors can still be lost by existing inference record call sites;
a successful inference response is not a durable accounting receipt. Weekly
`settled_only` describes stored rows, not a complete multi-replica pending view.
Do not treat an empty result alone as proof of zero unrecorded/in-flight spend.
Reads use bounded service timeouts and return errors on database failure instead
of synthesizing totals. Existing summary/recent/activity backend failures return
500; weekly returns `503 usage_unavailable`.

Metadata tracking requires no new environment flag. Leave
`ENABLE_STORE_COMPLLETIONS=false` unless content capture is explicitly wanted.

## 5. Upgrade and verification

- PostgreSQL: `0028_repair_workload_usage_accounting.sql` repairs the earlier
  duplicate-version 0023 workload migration; `0029_usage_trace.sql` adds trace
  storage and trace/parent/request indexes. Historical migration files stay intact.
- ClickHouse: `009_usage_trace.sql` adds trace storage and skip indexes.
- SQLite: trace is serialized in the existing usage JSON payload; no column
  migration is needed. Round-trip and filtering are tested with real SQLite.
- Existing clients, API envelopes, binding fields, and weekly capability version
  are preserved. The additions require GoRouter v0.2.1; Swagger exposes them.

See [v0.2.1 release notes](release-notes-v0.2.1.md) for verification and delivery
status. Source baseline for this update: `045c6e7`.
