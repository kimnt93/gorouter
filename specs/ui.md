# UI Specification

## Rendering

- Use Go `html/template`.
- Embed templates and static assets with `embed.FS`.
- Use HTMX for form submissions, partial refreshes, and navigation updates.
- Use Tailwind CSS.
- Do not build a React/SPA frontend.

## Pages

- Login
- Dashboard
- API keys
- Credentials
- Models and routes
- Usage
- Prompt cache

## Login

One login form accepts master key or API key. On success the server sets an HTTP-only signed cookie and redirects to the dashboard.

## Scope-Aware UI

Hide actions the current session cannot perform, but always enforce access server-side. Display current role and scopes for debugging and administrator clarity.

Expected visibility:

- `usage:read`: dashboard and usage
- `keys:manage`: API-key management
- `credentials:manage`: credential management
- `models:manage`: model/routes/prices
- `cache:purge`: cache flush
- `chat`: API access, not necessarily an admin page

## HTMX Behavior

Forms should return HTML fragments for HTMX requests and JSON for JSON API requests. Use `hx-target`, `hx-swap`, and `hx-confirm` for common operations. Do not put the master key or provider secrets into browser JavaScript.

## Styling

The UI must be usable on desktop and mobile. Production styling should not depend on an external CDN; bundled Tailwind CSS is preferred. HTMX should preferably be bundled into embedded static assets.
