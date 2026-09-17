# GoRouter v0.0.23 — Changes since v0.0.22

This document compares the current source to the `v0.0.22` Git tag. The
repository also contains later `v0.1.x` and `v0.2.x` milestones; **v0.0.23
is not the next semantic version after those tags**. Do not interpret this
note as a rollback of the newer features or as proof that a `v0.0.23` image
has already been published. Publish the corresponding GitHub release to run
the GHCR image workflow.

## Changes

- User-first access: one canonical API key per user, personal and organization
  one-to-one model aliases, bulk-assignment groups, and per-recipient weekly
  model limits; metadata tracking remains separate from authentication.
- Usage attribution and reporting by user, agent, model, conversation, run,
  parent run and trace; expanded accounting summaries and dashboard views.
  The accounting capability endpoint describes which features are available;
  read-side reports do not imply durable receipt or credit guarantees.
- OAuth maintenance for idle connected accounts now coordinates token refresh
  across replicas (Redis) or locally and shares a lock with request-time
  refresh. Grok Build has a weekly credit usage bar.
- Added an imported-key **Windsurf / Devin Desktop** connection with a logo,
  mock-tested native chat/stream translation. It is neither Devin CLI (ACP)
  nor browser OAuth; no background token renewal is promised for imported keys.
- Added a master-only **Updates** dashboard page and
  `GET /admin/updates/check`. Checking is explicit and reads GitHub's latest
  release metadata; released GHCR builds embed their version. The router
  **does not** control the Docker daemon or replace containers: operators
  pull a pinned image and recreate their existing deployment without dropping
  volumes. GitHub release metadata can appear before the GHCR image is ready.
- Updated README comparison and [interactive Swaggo API docs](/docs)
  (on a running router, e.g. `http://localhost:8090/docs`).

## Verification and upgrade

Run `go test ./...`, `go vet ./...`, `npm test -- --run`,
`npm run build`, and `.agents/skills/swaggo-docs/scripts/generate.sh check`.
The release comparison spans multiple milestones and includes migrations:
back up your durable database before updating, keep the existing Compose
configuration, environment and persistent volumes, and verify `/healthz`
and `/docs` after restarting. The Updates page cannot verify that a matching
GHCR image has been published; check the package before pulling.
