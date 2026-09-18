# GoRouter v0.0.24 — Devin Desktop credential storage fix

Changes since the v0.0.23 notes. This is a source/deployment note, not a
published GitHub Release or GHCR image. The repository also contains v0.1.x
and v0.2.x milestones; v0.0.24 is not the next semantic version after those
tags. Do not tag or advertise a v0.0.24 image without an explicit release
plan.

## Fixed

- PostgreSQL rejected imported-key **Windsurf / Devin Desktop** connections
  because its `credentials_provider_valid` CHECK constraint lacked the new
  `devin-desktop` provider. Migration **0032** updates the allowlist on
  existing databases and fresh installations. It preserves stored credentials
  and leaves historical rows unvalidated (`NOT VALID`). The service still
  validates provider IDs against its catalog before writing.
- A parity test compares the PostgreSQL constraint with every catalog provider
  to prevent the mismatch recurring. SQLite and ClickHouse use JSON-backed
  config records and have no SQL provider allowlist; Devin keys are still
  encrypted and stored through the same credential service in each backend.

## Verification and upgrade

- Tested with synthetic Devin keys: SQLite credential round trip; disposable
  PostgreSQL 16 fresh-schema and old-constraint upgrade/retry; disposable
  ClickHouse 25.8 credential round trip. No live Devin subscription call was
  made. The disposable databases were removed after testing.
- Before upgrading PostgreSQL, back up its volume/database. Restart the
  GoRouter service on the new binary so migration 0032 runs automatically;
  do **not** delete volumes. Verify migration 32 and connection creation.
  Local SQLite and ClickHouse deployments need no provider-constraint
  migration, but should still update the application binary.
- Do not reuse a connection key previously pasted into a chat. Revoke and
  replace it through the provider before connecting.
