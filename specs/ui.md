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
- Providers
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

## Providers

`/ui/providers` is the primary credential connection page. It remains server-rendered and is divided into:

- OAuth subscriptions: Claude Code and OpenAI Codex.
- API keys: OpenAI, Anthropic, Gemini, Groq, OpenRouter, OpenCode Zen, xAI, DeepSeek, Moonshot, Qwen, and a custom OpenAI-compatible endpoint.

Provider cards use inline expandable `<details>` content; selecting a provider must not navigate to a provider-specific page. Each card shows every connected account for that provider and allows another connection to be added.

API-key cards prefill the provider endpoint. Only the custom OpenAI-compatible preset requires a user-supplied endpoint. Secrets use password inputs and must never be returned to browser JavaScript after submission.

OAuth cards start a short-lived browser authorization flow. The UI opens the returned authorization URL in a new tab, then accepts either the full callback URL or `code#state`. Flow state, verifier, access token, and refresh token remain server-side; the only authorization material in the completion request is the opaque flow ID and pasted callback value.

For each connected credential, the UI provides:

- Health test.
- Live model discovery in a dialog.
- Selection and import of discovered models into global routes (master session only).
- Direct streaming chat test against a selected discovered model, with a warning that it can incur provider cost.
- Enable/disable and delete actions.

Public imported model IDs use the provider prefix. In particular, Claude Code uses `cc/<model>` and Codex uses `cx/<model>`. Codex reasoning effort is a request field and must not be represented as a model-name suffix.

## Derived API Keys

The API-key creation form lists imported models as checkboxes and submits repeated `models` fields. It must reject an empty selection and direct the administrator to import models from Providers when none exist. Quota choices are no limit, daily, weekly, and monthly.

## Provider Management Endpoints

- `GET /admin/providers`
- `POST /admin/oauth/:provider/start`
- `POST /admin/oauth/:provider/complete`
- `POST /admin/credentials/:id/test`
- `GET /admin/credentials/:id/models`
- `POST /admin/credentials/:id/models/import`
- `POST /admin/credentials/:id/chat-tests`

All endpoints require their registered scope and must enforce tenant ownership. Importing global model routes additionally requires a master session.

## HTMX Behavior

Forms should return HTML fragments for HTMX requests and JSON for JSON API requests. Use `hx-target`, `hx-swap`, and `hx-confirm` for common operations. Do not put the master key or provider secrets into browser JavaScript.

## Styling

The UI must be usable on desktop and mobile. Production styling should not depend on an external CDN; bundled Tailwind CSS is preferred. HTMX should preferably be bundled into embedded static assets.
