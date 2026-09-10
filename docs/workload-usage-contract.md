# Workload-attributed usage contract

Implemented from GoRouter source revision `1a237fe`; initial implementation revision `ea9b0fd`.

## Capability

Capability marker: `gorouter-workload-usage-v1`.

GoRouter is authoritative for Router-accounted request, token, provider-cache,
Router-cache, and model-priced cost facts for any application using workload-bound
API keys. Applications remain authoritative for their editable budget preferences.
The weekly API supports soft admission checks; it is not an atomic workload-budget
reservation or a strict concurrent spending limit.

## Workload-bound keys

Create a key through the existing authorized `POST /admin/api-keys` operation:

```json
{
  "name": "Production coding agent",
  "models": ["cx/gpt-5.6-luna"],
  "scopes": ["chat", "usage:read"],
  "owner_type": "user",
  "owner_user_id": "user_123",
  "workload": {
    "application": "coding-platform",
    "environment": "production",
    "workspace_id": "workspace_123",
    "agent_id": "agent_123"
  }
}
```

`application`, `workspace_id`, and `agent_id` are required together. `environment`
is optional. Values are opaque stable IDs, at most 128 bytes, using letters,
digits, `.`, `_`, `-`, `:`, or `/`. The binding is assigned only during an
authorized key creation operation. Key patch and inference APIs cannot mutate it.
Rotation preserves the binding. Key deletion does not remove historical usage.
Renames preserve stable IDs; clones must use new IDs. Existing keys remain valid
and explicitly unattributed.

This contract is application-neutral. Independent products should use distinct
application namespaces. The same agent ID in different application/workspace
namespaces does not collide.

## Inference correlation

Requests may include dedicated GoRouter correlation headers:

```text
X-GoRouter-Conversation-Id: conversation_123
X-GoRouter-Run-Id: run_123
X-GoRouter-Parent-Run-Id: run_parent_123
X-GoRouter-Request-Id: request_123
```

The values use the same bounded opaque-ID character set. They do not authorize,
change ownership, override the key binding, or get forwarded upstream. GoRouter
generates a logical request ID when omitted and a distinct provider-attempt ID
for each persisted accounting event.

## Weekly usage

```http
GET /admin/usage/workloads/weekly?application=coding-platform&environment=production&workspace_id=workspace_123&agent_id=agent_123
Authorization: Bearer <workload-bound key>
```

The key requires `usage:read`. It may query only its exact authenticated workload
binding. Query filters can narrow existing user/organization visibility but never
broaden it.

The response includes:

- `capability_version`
- resolved application, environment, workspace, and agent IDs
- `period_start` and exclusive `period_end`
- timezone and effective `week_starts_on`
- `as_of`, accounting state, completeness, freshness, and attribution coverage
- request count
- input, output, provider cache-read, and provider cache-write tokens
- corresponding Router-accounted cost components
- GoRouter response-cache hits

The period uses `quota.Window("week")` and the configured `WEEK_START` policy
(default Sunday UTC), not a rolling seven-day window. `accounting_ts` is fixed at
request admission, so a request finishing after the boundary remains assigned to
its admission week. Explicit `until` filters are half-open (`timestamp < until`).

## Accounting semantics and consistency

- `cost_usd` is Router model-priced cost, not an external provider invoice.
- Missing configured prices retain GoRouter's Free/zero policy.
- `usage_measurement` distinguishes provider/adapter-normalized usage,
  Router-cache usage, and unknown usage.
- Provider cache reads/writes remain distinct from GoRouter response-cache hits.
- A response-cache hit has zero provider cost.
- Accounting does not require prompt or completion capture.
- Workload-bound event IDs are idempotent in SQLite and PostgreSQL. ClickHouse
  serializes each workload-bound event ID through its configured distributed
  mutation lock and checks durable presence before insertion.
- Workload-bound records bypass the volatile queue and are synchronously handed
  to the selected durable backend.
- The weekly endpoint aggregates durable settled rows only. It does not merge a
  process-local pending map, so it cannot double count queued events.
- Backend failure returns `503 usage_unavailable`; it never becomes a complete
  zero. An authorized empty aggregate is a valid zero with
  `attribution_coverage: no_usage`.

## Errors

- `400`: malformed/oversized filters or use without a workload integration key.
- `403`: attempted binding broadening or missing object permission.
- `404`: workload binding is absent or concealed.
- `503` with `usage_unavailable`: durable aggregation is unavailable.

## Compatibility and limitations

Legacy unbound clients retain existing behavior and appear unattributed. Their
existing asynchronous queue is not a crash-safe durable handoff. GoRouter cannot
reconstruct exact provider charges if an upstream fails without reporting usage.
Strict atomic per-workload budget caps are not included in this contract.

## Safe fixture

Use synthetic immutable IDs such as `application_test`, `workspace_test`,
`agent_test`, and `run_test`. Do not put emails, secrets, prompts, or mutable
display names in workload or correlation IDs.

## Verification status

- Implemented: yes
- Unit-tested: yes (`go test ./...`, `go vet ./...`)
- SQLite integration-tested: yes (migration, isolation, half-open week, idempotency)
- PostgreSQL live integration-tested: no (`TEST_DATABASE_URL` not configured)
- ClickHouse live integration-tested: no (`TEST_CLICKHOUSE_URL` not configured)
- Released: no
- Deployed: no
