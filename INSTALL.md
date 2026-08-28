# GoRouter Installation

This file is the installation contract for both human operators and AI
assistants. Do not modify the installation section in `README.md` when using
these instructions.

## Safety Rules

- Use Docker Engine and Docker Compose v2.
- Run exactly one durable backend: PostgreSQL or ClickHouse.
- Never run `docker compose down -v`, `docker system prune`, or a broad
  container stop/delete command during installation or upgrade.
- Never stop, restart, or replace unrelated user services.
- Replace only a GoRouter deployment identified by its Compose project and
  service metadata. If ownership is uncertain, stop and ask the operator.
- Preserve the existing `.env` and named volumes during upgrades.
- Keep `.env` mode `0600`. Never print `MASTER_KEY`, database passwords, Redis
  passwords, API keys, OAuth tokens, or authorization headers.

## Releases

The source repository and release list are:

<https://github.com/kimnt93/gorouter>

<https://github.com/kimnt93/gorouter/releases>

Before installing a released version, inspect the latest non-draft release and
pin its tag. Prefer the published image for that exact tag:

```text
ghcr.io/kimnt93/gorouter:<release-tag>
```

Do not silently substitute `latest` for a requested version. Use `latest` only
when the operator explicitly requests the latest release.

## New Docker Install

Choose one backend. PostgreSQL is the default recommendation:

```bash
cp .env.example .env
chmod 600 .env
```

Generate fresh values for `MASTER_KEY`, `DB_PASSWORD`, and `REDIS_PASSWORD`,
write them to `.env`, and do not display them. Start PostgreSQL with:

```bash
docker compose --env-file .env -f docker-compose.postgres.yml up -d --build
```

For ClickHouse, configure its required variables and run:

```bash
docker compose --env-file .env -f docker-compose.clickhouse.yml up -d --build
```

The default PostgreSQL dashboard URL is `http://localhost:8090/`. Verify:

```bash
curl -fsS http://127.0.0.1:8090/healthz
```

For a different port, set `ROUTER_PORT` for PostgreSQL or
`CLICKHOUSE_ROUTER_PORT` for ClickHouse before starting the stack.

## Port and Service Collisions

Before starting, inspect the requested port and existing Compose projects. If
the port is used by anything other than the current GoRouter service, do not
stop that service. Select the next available port, record it in `.env`, and
report the selected URL.

If a container or project name collides, use a separate Compose project name.
Only override the existing GoRouter application service when its identity is
confirmed. Do not alter another user's services to make room.

## Upgrade or Deploy

Find the requested version from the GitHub release list, pin the image/source
to that release, and validate the Compose configuration before changing the
running service. Preserve `.env`, database volumes, Redis volumes, and the
current backend. Recreate only the GoRouter application container when an
application upgrade is needed. Do not recreate databases or Redis unless the
operator explicitly requests it.

After startup, require HTTP 200 from `/healthz` and inspect only sanitized
container status. If the new application fails health checks, restore the
previous GoRouter image or binary and leave unrelated services untouched.

## Harness / AI Installer Output

After a successful install, the AI or harness must tell the user:

- the installed release tag or image;
- the selected backend;
- the dashboard URL and health URL;
- the Compose project and install directory;
- whether an existing GoRouter deployment was upgraded or a new port was used;
- that the master key is stored in `.env` with mode `0600`.

The AI must not echo the master key into chat, logs, shell history, or tool
output. Tell the user to retrieve it privately with a local command such as:

```bash
grep '^MASTER_KEY=' .env
```

If the user explicitly requires the key in the final response, first obtain
confirmation that the response channel is private, then expose it only once
and warn the user to rotate it after any disclosure. Never include it in a
public deployment report.

For duplicate ports, choose the next free port and continue without stopping
user services. For duplicate GoRouter services, override only the positively
identified current GoRouter service. Report any skipped or ambiguous service
instead of guessing.
