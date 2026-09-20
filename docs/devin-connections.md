# Devin connections

These are separate products and credential types:

| Connection | Key | Transport and discovery |
|---|---|---|
| Devin CLI (`dv/…`) | `apk_user_…` | Official `devin acp --agent-type summarizer`; authenticated `session/new` configuration selectors, not `models list`. |
| Windsurf / Devin Desktop (`dd/…`) | Imported Desktop key | Codeium Connect-protobuf / `GetUserJwt`. Its small static fallback catalog is **not account entitlement discovery**. Do not put a CLI key here. |
| Devin cloud API | PAT/service token (`cog_…`; legacy formats differ) | Cloud agent sessions at `api.devin.ai/v3`, not OpenAI chat completions. Not this connection. |

The documented Desktop `server.codeium.com/api/v1` service-key API is enterprise
**analytics/configuration**, not a chat/model catalog API. The previous CLI
implementation also incorrectly used `"protocolVersion":"0.3"` and
`devin models list`, which ignores `WINDSURF_API_KEY` and fails in a clean HOME.
The adapter now negotiates **ACP version 1** and uses the same credential-scoped
session setup for health, catalog discovery and inference.

## Run an image that includes the CLI

The small default image still excludes the external CLI. Build the explicit
variant from this checkout:

```sh
docker build --target devin-cli -t gorouter:devin-cli .
docker run --rm --entrypoint devin gorouter:devin-cli --version
```

Use the overlay with exactly one existing backend profile (same project name):

```sh
docker compose -f docker-compose.local.yml -f docker-compose.devin-cli.yml up -d --build
# Or use docker-compose.postgres.yml / docker-compose.clickhouse.yml instead.
```

Alternatively, set your existing Compose `gorouter` service to `build.target: devin-cli`, or
use the image above with your **existing** environment, ports, network, secrets
and volumes. For a prebuilt-binary deployment, the image must also install or
copy `/usr/local/bin/devin`; merely replacing the GoRouter binary is insufficient.
No Docker socket, privileged container, host HOME mount, or host repository
mount is needed. The bundle is a native executable and must match the node's
architecture.

Future GitHub release builds publish both `<release>` / `latest` and
`<release>-devin-cli` / `latest-devin-cli` to `ghcr.io/kimnt93/gorouter`.
**A Git push is not an image release**; check package availability before
pulling. Use the CLI suffix when upgrading this variant (the dashboard's
manual update instructions default to the small image).

`scripts/install-devin-cli.sh` downloads the official **3000.10.31** Linux
bundle from `static.devin.ai` and checks pinned SHA-256 values for amd64/arm64.
There is no runtime installer or automatic executable update. Review and
update the version/checksums when upgrading the CLI. CLI use remains subject
to the provider's license and subscription terms. The Docker target runs as
the same non-root UID as GoRouter.

Every replica routing these credentials needs the CLI. Existing Redis catalog
caches remain credential-scoped; no CLI HOME/auth state is shared among nodes
or accounts. At most four local subprocesses run per adapter instance, with
bounded queue wait, frame size, output size and request lifetime. Each call
gets a fresh private HOME/XDG/cwd; cancellation kills the process group on
Unix, waits, releases capacity and removes the directory. This is credential
isolation, **not an OS security sandbox for the CLI**: run only a trusted
provider binary in the unprivileged container.

## Models and reasoning without a GoRouter release

1. Add the key to **Providers → Devin CLI**. Existing Desktop credentials are
   not silently reinterpreted; create a new CLI connection, import its models,
   then remove obsolete `dd/…` routes if no longer needed.
2. Health authenticates via `session/new` without sending chat or consuming an
   inference turn. Missing binary is 503, authentication rejection is 401.
3. Discovery reads the live ACP model selector, selecting each model to read
   **that model's** current reasoning selector. No hardcoded family names,
   fabricated fallback efforts, marketing-page scraping or separate cached
   CLI login is used. Failed/unsupported discovery returns an error instead of
   inventing a successful catalog.
4. Existing model-catalog synchronization periodically refreshes these
   snapshots; use the model refresh action to force a check. Newly exposed
   models/options do not require a GoRouter rebuild **if the installed CLI
   reports them**. Provider CLI protocol changes may require a CLI or adapter
   update. Changes are subject to the existing cache/sync intervals.
5. Requests select the advertised upstream model and `reasoning.effort` via
   `session/set_config_option`. Reasoning stays request metadata, not an
   invented suffix on a public model ID. Provider-native variant IDs remain
   unchanged. Unsupported selections are rejected before sending a prompt.

## Scope and accounting

The adapter is text-only and uses Devin's **no-tool summarizer**. It does not
run agent workflows, execute requested tools, or grant filesystem/terminal/MCP
permissions. Tools, tool history, images and unsupported reasoning-summary
controls return 400. Streaming emits text/thought deltas as they arrive, not
at the end of the turn. Cancelled, truncated or malformed streams do not emit
a successful `[DONE]`. Clients must treat an interrupted stream as a failure.

When the CLI returns ACP turn usage, input/output/cache-read/cache-write values
are retained separately; otherwise the existing GoRouter text estimator is
used. Context-window `usage_update.used` is not billed usage. The CLI owns
inference defaults; sampling controls are not a general OpenAI parameter pass-
through. There is no live-account verification claim from synthetic tests.

## Verification and references

- Official binary `--version`: 3000.10.31; checksum verified.
- Real binary: ACP v1 initialize and invalid-key rejection at `session/new`;
  no real account key or inference request was used.
- Strict mock ACP: no-prompt health, credential isolation, dynamic grouped
  catalogs, per-model reasoning, selection confirmation, streaming before
  completion, usage components, timeouts, cancellation, EOF/error denial.
- The application/repositories still use the previously migrated `devin-cli`
  ID. No new schema or backend-specific persisted field is introduced here.

Sources: [CLI models](https://docs.devin.ai/cli/models),
[CLI ACP setup](https://docs.devin.ai/cli/acp/zed),
[Desktop enterprise API](https://docs.devin.ai/desktop/accounts/api-reference/api-introduction),
[cloud authentication](https://docs.devin.ai/api-reference/authentication),
[ACP schema](https://github.com/agentclientprotocol/agent-client-protocol/tree/main/schema/v1),
and the official CLI installer/manifest at `https://cli.devin.ai/install.sh`.
Never paste tokens into command arguments, tracked files or support messages;
rotate any key already shared publicly.
