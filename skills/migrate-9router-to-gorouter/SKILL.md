---
name: migrate-9router-to-gorouter
description: Perform a one-time PostgreSQL migration of 9Router SQLite provider credentials, model routes, and usage history into GoRouter. Use for 9Router cutovers, not OmniRoute or recurring synchronization.
---

# Migrate 9Router to GoRouter

Use `scripts/migrate.py` against the current 9Router SQLite database. Read
`references/schema.md` before adapting the script to a different 9Router fork
or schema version.

## Safety contract

- Keep the source service running unless the user separately authorizes a
  cutover. SQLite is opened read-only.
- Run the script without `--apply` first. Dry-run output contains counts and
  unsupported provider names, never credential values.
- With `--apply`, require a private backup directory and back up both the source
  SQLite file and target PostgreSQL database before writing.
- Use one PostgreSQL transaction, deterministic source-derived IDs, and
  conflict-safe inserts. Reconcile once and exit.
- Never create a timer, cron entry, watcher, daemon, or synchronization loop.

## Run

The standard 9Router database is `~/.9router/db/data.sqlite` on macOS/Linux and
`/app/data/db/data.sqlite` inside its container.

```bash
python3 scripts/migrate.py \
  --source-db "$HOME/.9router/db/data.sqlite" \
  --target-env /path/to/gorouter/.env \
  --postgres-container gorouter-postgres-1
```

After reviewing the dry run:

```bash
python3 scripts/migrate.py \
  --source-db "$HOME/.9router/db/data.sqlite" \
  --target-env /path/to/gorouter/.env \
  --postgres-container gorouter-postgres-1 \
  --backup-dir /private/path/migration-backups \
  --apply
```

The script supports PostgreSQL GoRouter targets and requires Python 3,
`cryptography`, Docker, and a fully migrated target database. Unsupported
9Router provider protocols are reported and skipped; reconnect those from the
GoRouter provider dashboard instead of coercing them into another connector.

