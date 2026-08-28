# GoRouter Installation

This file is the installation contract for human operators and AI Agent
installers. Read it before installing or upgrading GoRouter. The installation
section in `README.md` is intentionally unchanged; use this file for the
complete Docker procedure.

## Safety rules

- Use Docker Engine and Docker Compose v2.
- Run exactly one durable backend: PostgreSQL or ClickHouse.
- Never run `docker compose down -v`, `docker system prune`, or a broad
  container stop/delete command.
- Never stop, restart, or replace unrelated user services.
- Preserve the existing `.env` and named volumes during upgrades.
- Keep `.env` mode `0600`; never print secrets in logs or tool output.
- Replace only a positively identified GoRouter service. If ownership is
  ambiguous, stop and ask the operator instead of guessing.

## Release selection

For a versioned deployment, first inspect the requested version in the GitHub
repository and its Releases page:

- Repository: <https://github.com/kimnt93/gorouter>
- Releases: <https://github.com/kimnt93/gorouter/releases>

Pin the selected release tag. Prefer the matching published image:

```text
ghcr.io/kimnt93/gorouter:<release-tag>
```

Do not silently use `latest` for a versioned request. Use `latest` only when
the operator explicitly requests the latest release. If no image is available,
check out the selected tag and build the Docker image locally.

## New Docker installation

Choose exactly one backend. PostgreSQL is the default recommendation.

1. Create the configuration without overwriting an existing installation:

   ```bash
   test -e .env || cp .env.example .env
   chmod 600 .env
   ```

2. Generate fresh values for `MASTER_KEY`, `DB_PASSWORD`, and `REDIS_PASSWORD`
   and write them to `.env` without displaying them. Never use the example
   placeholder values in a production deployment.

3. Check the requested host port before starting. `8090` is the default. If it
   is already used by anything other than this GoRouter installation, choose
   the next free port (`8091`, then `8092`, and so on), set `ROUTER_PORT` in
   `.env`, and continue. Do not stop the service using the original port.

4. Start the PostgreSQL stack:

   ```bash
   docker compose --env-file .env -f docker-compose.postgres.yml up -d --build
   ```

   For ClickHouse, configure the ClickHouse variables in `.env` and run:

   ```bash
   docker compose --env-file .env -f docker-compose.clickhouse.yml up -d --build
   ```

5. Verify the installation:

   ```bash
   curl -fsS http://127.0.0.1:${ROUTER_PORT:-8090}/healthz
   ```

The dashboard is available at `http://localhost:${ROUTER_PORT:-8090}/`.

## Duplicate services and upgrades

Before changing a running deployment, inspect the Compose project, container
labels, service name, and port. A duplicate port alone is not evidence that a
service belongs to GoRouter.

- If the port belongs to another application, select the next available port;
  do not stop, restart, or reconfigure that application.
- If the existing service is positively identified as the current GoRouter
  service, override only its application service (`gorouter`). Do not recreate
  PostgreSQL, ClickHouse, or Redis, and do not delete their volumes.
- If a duplicate GoRouter service cannot be identified safely, leave it alone
  and ask the operator.
- Preserve the current backend and `.env` during an upgrade. Validate the
  Compose configuration before applying it.
- After an upgrade, require HTTP 200 from `/healthz`. If it fails, restore the
  previous GoRouter image or binary; leave unrelated services untouched.

For an application-only rebuild of a confirmed current Compose project, use
that project's existing `.env` and Compose file and target only the service:

```bash
docker compose --env-file .env -f docker-compose.postgres.yml up -d --build gorouter
```

Use the equivalent ClickHouse file when that is the active backend. For a
released image, set the image to the pinned release tag before running the
same application-only update; never replace a release with an unpinned image.

## Required AI Agent completion response

After a successful installation, the AI Agent must tell the user:

- the installed release tag or image;
- the selected backend;
- the dashboard URL and health-check URL;
- the Compose project and installation directory;
- whether this was a new installation, an upgrade, or a next-port install;
- that the master key was generated and stored in `.env` with mode `0600`.

The AI Agent must also provide the master key to the user **only after
confirming that the response channel is private**. Reveal it once, never put
it in logs, shell history, or a public deployment report, and advise the user
to rotate it after disclosure. If privacy cannot be confirmed, do not echo the
key; instead tell the user to retrieve it locally:

```bash
grep '^MASTER_KEY=' .env
```

For a port collision, report the original port, the selected next free port,
and that no user service was stopped. For a duplicate GoRouter deployment,
report that only the positively identified current GoRouter application
service was overridden. Never claim success until `/healthz` returns HTTP 200.
