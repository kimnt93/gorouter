# Devin connections

Devin supplies **different credentials for different products**. They cannot be
interchanged:

| GoRouter connection | Credential | Transport | Model catalog |
|---|---|---|---|
| Devin CLI (`dv/…`) | `apk_user_…` CLI key | Official `devin acp` local process | `devin models list --format json` at discovery time; **not** a cloud REST model endpoint. Needs the installed official CLI on each GoRouter node. |
| Windsurf / Devin Desktop (`dd/…`) | Imported Desktop key, not a CLI or cloud API key | Codeium Connect-protobuf / `GetUserJwt` | No authenticated, documented live listing endpoint identified; current small fallback list is not entitlement-aware. |
| Devin cloud API | `cog_…` PAT or service-user token (legacy keys differ) | `api.devin.ai/v3` sessions API | Cloud agent session orchestration, **not** OpenAI chat completions. Not a GoRouter provider connection. |

Devin Desktop's documented `server.codeium.com/api/v1` API is enterprise
**analytics/configuration** with service keys in the request body. It is not a
chat or model-discovery API. The CLI's model list and ACP are not available in
the standard GoRouter image, which deliberately does not bundle Devin's
proprietary binary. On distributed deployments, every router node that can
route a Devin CLI credential must have the binary installed separately. If the
CLI is missing, health/discovery/chat must fail rather than report a working
connection.

The CLI adapter runs a bounded `summarizer` ACP subprocess in a fresh, isolated
working directory per call, with credentials only in the subprocess environment.
Its health check sends a small real prompt and may consume provider quota. It
currently supports text messages, not tools or request-scoped reasoning effort;
model variants and any reported reasoning options follow the CLI's catalog.
The current implementation buffers an ACP turn before returning stream chunks,
so it is not true incremental streaming. Model discovery returns only names
reported by the CLI; it does not invent availability from marketing pages.

[Devin CLI models](https://docs.devin.ai/cli/models),
[Devin Desktop enterprise API](https://docs.devin.ai/desktop/accounts/api-reference/api-introduction),
and [Devin cloud authentication](https://docs.devin.ai/api-reference/authentication)
describe these separate products. Avoid putting tokens in terminal arguments,
tracked files, or support messages. Revoke any key already shared publicly.
