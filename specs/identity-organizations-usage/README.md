# Identity, Organizations, Keys, and Usage

This directory is the implementation contract for adding users, organizations,
principal-owned API keys, principal-aware usage history, and organization
administration to `gorouter`.

Agents implementing this feature must read, in order:

1. This file.
2. `SPEC.md`.
3. The file for their workstream (`API.md`, `STORAGE.md`, or `UI.md`).
4. `CHECKLIST.md`.

## Scope

The feature adds:

- Master-created users with a unique normalized email username.
- Master-created organizations.
- Many-to-many user/organization membership with `member` and `admin` roles.
- Personal, organization-scoped personal, and organization-owned API keys.
- Login using the master key or any enabled API key.
- Object-level authorization in addition to existing scopes.
- User and organization attribution on usage events.
- Master, personal, and organization-specific usage visibility.
- User, organization, membership, key, usage, and audit web pages.
- Master-key model calls attributed to `master`.

It does not add passwords, email verification, invitations, billing, payments,
SSO, SCIM, or a standalone terminal application.

## Normative precedence

For this feature, files in this directory override older tenant/auth/usage/UI
statements in the top-level `specs/` directory when they conflict. Existing
unrelated gateway, provider, pricing, quota, and cache requirements remain in
force.

## Storage mode requirement

Exactly one durable backend is active at runtime:

```text
DATABASE_BACKEND=postgres   -> PostgreSQL repositories only
DATABASE_BACKEND=clickhouse -> ClickHouse repositories only
```

Both modes expose the same domain behavior and HTTP API. They do not mirror,
replicate, fall back to, join against, or write to each other.

- PostgreSQL may use relational tables, constraints, joins, and transactions.
- ClickHouse uses ClickHouse-native tables and denormalized configuration
  records. It must not require PostgreSQL or emulate SQL foreign keys.

Redis remains a coordination/cache service where already configured; it is not
a durable identity source of truth.

## UI clarification

The terminal-style wireframes discussed during design are visual references
only. Implement a normal accessible server-rendered web UI using the existing
Go templates, HTMX, CSS, links, forms, tables, dialogs, and buttons.

Do not implement:

- A terminal emulator.
- A command parser or command prompt.
- Keyboard-only navigation.
- Animated typing, scanlines, fake shell output, or a CLI application.

Subtle monospace typography and compact operational styling are allowed, but
clarity, accessibility, and responsive layout take priority.

## Definition of done

The feature is complete only when every required item in `CHECKLIST.md` is
checked and the same behavioral integration suite passes in PostgreSQL mode and
ClickHouse mode.
