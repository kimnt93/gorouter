# Smoke, Docker, and live tests

## Docker profiles

- PostgreSQL: `docker-compose.postgres.yml`, default router port 8090.
- ClickHouse: `docker-compose.clickhouse.yml`, default router port 18091.
- Each profile has Redis and one durable backend. Never start a profile that
  silently connects to the other backend.
- Use `docker compose --env-file .env -f <file> config` and `ps` to resolve the
  exact project/services/volumes before a destructive reset.

Stopping services is reversible; removing named volumes destroys the local
fixture. Do it only when the user explicitly requested a clean rebuild, name
the affected Compose profile, and report what was removed.

## Seed fixture

`scripts/seed-smoke.mjs` is the current end-to-end fixture generator. It is
designed to create:

- 50 English-name/email users;
- 10 named smoke organizations;
- fewer than 10 multi-organization users, with those users in 2–4 orgs and the
  remainder in one;
- admin/member memberships, personal and organization credentials, personal,
  organization-scoped, and organization-owned API keys;
- provider model imports and namespaced routes;
- short, medium, and long request/usage samples;
- authorization assertions for personal and organization visibility.

Pass secrets by environment. If an access output is needed, put it in `/tmp`
and enforce mode `0600`. Keep the report secret-free and do not paste access
material into agent messages.

## Browser smoke

The `scripts/ui-smoke.mjs`, `ui-view-as-smoke.mjs`, and
`ui-member-smoke.mjs` scripts cover complementary roles. Set `UI_BASE_URL`,
`MASTER_KEY` or the required protected access environment, and `CHROME_BIN`.
For current product work, prioritize 1440×900 desktop/PC assertions. Capture
screenshots under `/tmp`, check browser/page errors, layout overflow, dialogs,
context changes, API failures, and cleanup of temporary records.

## Live-provider verification

Prefer mock upstream tests. When the user explicitly requests a live functional
check, bound request count, input/output, timeout, and expected spend. Verify
the exact model ID, HTTP status, response shape, stream termination, usage
record, token/cost components, and cache attribution without retaining content.

Use `$benchmark-report` for concurrent load, latency/throughput/resource
measurements, cache-rate experiments, or local-versus-remote comparisons.
