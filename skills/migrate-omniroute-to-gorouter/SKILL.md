---
name: migrate-omniroute-to-gorouter
description: Perform a one-time PostgreSQL migration of OmniRoute provider credentials, model routes, and usage history into GoRouter. Use for cutovers or side-by-side validation, not continuous synchronization.
---

# Migrate OmniRoute to GoRouter

Use `scripts/migrate.py` for a deterministic, one-time import. Read
`references/mapping.md` before changing provider mappings or interpreting token
totals.

## Safety contract

- Keep OmniRoute running unless the user separately authorizes a cutover.
- Open the OmniRoute SQLite database read-only. Never refresh or rotate source
  OAuth credentials during migration.
- Run without `--apply` first. The default is inspection only and prints no
  secrets.
- Before `--apply`, require a private backup directory. The script copies the
  source SQLite database and creates a GoRouter PostgreSQL dump there.
- Import in one PostgreSQL transaction, using deterministic IDs and conflict
  protection. Run once, reconcile, and exit. Do not install timers, cron jobs,
  services, watchers, or recurring sync.
- Do not stop either router until reconciliation is exact and the user approves
  the separate cutover.

## Run

```bash
python3 scripts/migrate.py \
  --source-db /path/to/omniroute/storage.sqlite \
  --source-env /path/to/omniroute/server.env \
  --target-env /path/to/gorouter/.env \
  --postgres-container gorouter-postgres-1
```

Review the provider/model summary and skipped-provider list. Then apply:

```bash
python3 scripts/migrate.py \
  --source-db /path/to/omniroute/storage.sqlite \
  --source-env /path/to/omniroute/server.env \
  --target-env /path/to/gorouter/.env \
  --postgres-container gorouter-postgres-1 \
  --backup-dir /private/path/migration-backups \
  --apply
```

The script requires Python 3, `cryptography`, Docker, and a migrated GoRouter
PostgreSQL container. It does not support ClickHouse targets.

After import, compare source and target event counts, input/output/cache token
totals, credential counts, and model counts. Test each imported connection from
the GoRouter dashboard before changing any agent base URL.

