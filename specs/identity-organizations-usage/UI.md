# Web UI Contract

## 1. Implementation style

Build a conventional server-rendered web administration UI using the existing:

- `html/template` and embedded assets.
- HTMX form submissions/partial refreshes where useful.
- Existing CSS design system and responsive patterns.
- Semantic forms, tables, details, dialogs, links, and buttons.

The earlier terminal-style mockups are reference material for density,
monospace identifiers, status clarity, and operational hierarchy only.

Do not build a terminal, shell prompt, command parser, terminal emulator, SPA,
React application, or keyboard-command subsystem.

## 2. Global shell

The header displays:

- Current principal label (`master`, user email, or `org:<name>`).
- Active organization context when present.
- Scope-aware navigation.
- Logout.

Navigation by visibility:

- Master: Dashboard, Users, Organizations, API keys, Providers, Models & routes,
  Usage, Audit, Cache.
- User: Dashboard, Organizations, API keys, Models (when allowed), Usage.
- Organization admin/key: Organization, Members, API keys, Providers (when
  allowed), Models (when allowed), Usage, Audit.

Hiding an action is not authorization; every form endpoint enforces policy.

## 3. Context selector

Users with multiple memberships can select:

- Personal.
- Each active organization.

Master can select all or one organization. Organization keys are fixed to their
own organization.

Context selection filters pages and supplies safe defaults. It never broadens
server-side access and does not silently mutate API-key context.

## 4. Users page (master only)

Provide:

- Search and status filter.
- Paginated user list with username, status, organization count, key count, and
  created time.
- Create-user form with optional initial key configuration.
- User detail with memberships, safe key metadata, aggregate usage, status
  management, and recent activity.
- One-time initial-key secret dialog requiring explicit acknowledgement before
  closing.

Do not allow username mutation in the first implementation.

## 5. Organizations and members

Organization list shows name, status, members, administrators, keys, and recent
request count where authorized.

Organization detail uses normal tabs/sections:

```text
Overview | Members | API keys | Providers | Usage | Audit
```

Members UI supports:

- Search existing users by exact email/username.
- Add an existing user as member/admin.
- Change role.
- Remove membership with confirmation.
- Clear error when attempting to remove/demote the last administrator.

Organization admins cannot create users from the member dialog. Explain that a
master must create the user first.

## 6. API-key UI

Use a unified key list with ownership/context columns:

```text
Name | Owner | Context | Prefix | Models | Status | Created
```

Forms adapt to principal policy:

- User: create personal or organization-scoped personal key for an active
  membership.
- Organization admin/key: create organization-owned key for its organization.
- Master: select any valid owner/context.

Key detail shows safe metadata, scopes, models, quota, RPM, created time, and
last-used time. Rotate, disable, and delete actions require confirmation where
appropriate.

Organization-key forms and detail show a warning:

```text
This is a shared organization principal. Calls are attributed to org:<name>,
not to an individual user.
```

Never place plaintext keys in list HTML, JavaScript state, data attributes, or
audit output. Show create/rotation plaintext once in a modal with copy and
acknowledgement controls.

## 7. Usage UI

The primary responsive table columns are exactly:

```text
Model | User | I/O | Time
```

Rows expand to reveal safe secondary fields:

- Organization/context.
- API-key name and prefix.
- Upstream model.
- Input/output/cache-read/cache-write tokens.
- Cost/priced state.
- Cache hit.
- Status and safe error.
- Duration.
- Credential ID.
- Exact UTC RFC3339 timestamp.

Filters include time range, organization, user, model, key, and status according
to principal permissions. History is cursor-paginated.

On narrow screens, rows become stacked cards. No horizontal clipping may hide
chat, key, membership, or pagination controls.

## 8. Time rendering

Render exact RFC3339 server-side in `<time datetime>`. Use a small progressive-
enhancement script only if needed to update events under 60 seconds old.

Rules:

- `0 seconds ago` through `59 seconds ago`.
- At 60 seconds: RFC3339 UTC.
- If JavaScript is unavailable, server-rendered text remains valid.

## 9. Audit UI

Audit history shows:

```text
Actor | Action | Target | Time
```

Expandable details show safe IDs, organization, and changed non-secret fields.
Use the same principal visibility and pagination model as the audit API.

## 10. Accessibility and responsiveness

- All controls work with keyboard and pointer without custom terminal commands.
- Use visible labels and focus states.
- Status does not rely on color alone.
- Dialogs are viewport-bounded and vertically scrollable.
- Tables have mobile card fallbacks.
- Confirmation text names the exact user, organization, membership, or key.
- Use `aria-live` for async HTMX results where appropriate.
