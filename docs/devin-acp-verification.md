# Devin ACP fix verification — 2026-09-20

Source change after `cd46353`. No deployment or paid inference is claimed.

## Single-image packaging correction — 2026-09-20

The optional-image packaging described in the original verification below is
superseded. The standard Dockerfile now **always** includes the checksum-pinned
Devin runtime; there is one release job for the normal version/`latest` tags,
not a provider-specific suffix, build target or overlay. Every existing provider
remains in the same catalog and application. Current setup instructions are in
[Devin connections](devin-connections.md).

Verified after the correction:

- Plain `docker build -t gorouter-standard-check:local .` (no target/overlay).
- `docker run --rm --entrypoint devin gorouter-standard-check:local --version`
  reports 3000.10.31 under UID 65532:65532.
- Local SQLite smoke: `/healthz` 200, all 27 provider entries present, synthetic
  CLI credential created (201), invalid-key health status 401 from the bundled
  official CLI (not missing executable/503), and credential deleted (200).
- Chrome 1440×900: standard-image hint and avatar present, no old image-target
  instruction or ACP base-URL input, no horizontal overflow or page errors.
- Local, PostgreSQL and ClickHouse Compose profiles resolve the normal
  Dockerfile without selecting a provider-specific target or adding a service.
- Packaging regression test covers the final stage, pinned installer, single
  release workflow, and all three Compose profiles. UI tests cover the revised
  one-image instruction. Full Go tests/vet, 120 frontend tests, SPA build,
  Swaggo drift check and diff whitespace check passed.

Only amd64 was built/run locally. arm64 pins and the release platform remain
configured, not locally execution-tested. No provider secrets or paid chat were
used. Database schemas and routing are unchanged; this packaging-only fix does
not claim new PostgreSQL/ClickHouse live-inference coverage. Existing images
and the remote server are unchanged until a normal image upgrade/rebuild.

## Original ACP verification (before the packaging correction)

## Reproduced with the official CLI

Downloaded official Linux amd64 CLI **3000.10.31** to a temporary directory,
verified archive SHA-256 against its published manifest, and inspected `acp
--help` and the ACP v1 schema. These checks did not read existing account
login files.

- `models list --format json` under fresh HOME with a synthetic
  `WINDSURF_API_KEY` exits 1 with “Not logged in.” The old discovery path was
  not credential-aware.
- Numeric ACP protocol version 1 initializes successfully. The old string
  version was incorrect.
- `session/new` rejects a synthetic invalid key with an authentication error;
  the new adapter maps this to 401 without exposing upstream diagnostics.
- No `session/prompt` call was made to the real provider.

## Automated evidence

- Full Go suite and vet.
- Focused race suite for LLM adapters, provider capabilities and credentials.
- Strict mock ACP tests cover numeric version, grouped/dynamic models,
  model-specific thought selectors, selection confirmation, empty/unsupported
  catalogs, no-inference health, invalid auth, missing binary, preserved usage
  components, incremental thought/text SSE, EOF/malformed/ID mismatch,
  cancellation, bounded capacity, and temporary-directory cleanup.
- PostgreSQL/ClickHouse/SQLite persistence logic and interfaces are unchanged;
  this patch relies on migration 33 from the preceding deployment. It changes
  no durable schema or SQL query.
- UI suite: 38 files, 120 tests; regenerated embedded SPA.
- Docker `devin-cli` image built on Linux amd64 and executed the official CLI
  as UID 65532; default Docker target also builds without downloading the CLI.
- Local SQLite Docker smoke: `/healthz` 200; synthetic credential create 201,
  official CLI health rejection 401, deletion 200. No paid prompt.
- Desktop Chrome 1440×900: CLI connection modal/hint/avatar present, no HTTP
  base-URL field for ACP, no horizontal overflow or page errors.
- Compose overlay resolves the `devin-cli` target for local, PostgreSQL and
  ClickHouse profiles without adding services.

## Limits

Successful authenticated discovery/chat is mocked: no valid account key was
used. We have not tested every provider-reported catalog shape or a successful
Devin paid inference with this CLI version. Linux arm64 is checksum-pinned in
the installer and configured in release CI, but only amd64 ran locally.
Health/catalog errors must remain failures if future CLI versions change the
protocol. Updating only the Go binary in a custom prebuilt deployment still
does not supply external runtime dependencies; use the current standard image
described above. The optional-image instructions in the original evidence are
historical, not the current setup.
