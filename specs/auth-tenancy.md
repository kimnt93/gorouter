# Authentication and Tenancy Specification

## Master Key

`MASTER_KEY` is required during setup. Startup must fail if it is absent. It is never stored in the database.

The master key can:

- Log into the UI.
- Call all JSON admin endpoints.
- Manage all tenants, credentials, keys, models, routes, prices, cache, and usage.
- Call the gateway if the product explicitly supports master-key gateway calls; otherwise API keys are required for model calls.

Use constant-time comparison.

## API Keys

API keys must include:

- ID
- Tenant ID
- Name
- SHA-256 secret hash
- Display prefix
- Enabled state
- Allowed model names
- Scopes
- Monthly USD quota, nullable
- RPM limit, nullable
- Created timestamp

Plaintext is returned exactly once from create. Lists and updates never return it.

## Scopes

Valid scopes:

```text
chat
usage:read
keys:manage
credentials:manage
models:manage
cache:purge
```

Master sessions pass all scopes. API-key sessions pass only stored scopes.

## Sessions

`POST /login` accepts JSON `{ "key": "..." }` and form field `key`.

The key may be the master key or an enabled API key. Success creates a signed HTTP-only cookie containing role, key ID, tenant ID, scopes, and expiration. Session expiration must be finite, recommended 12 hours.

`POST /logout` clears the cookie.

## Tenant Rules

- Every API key belongs to one tenant.
- Credentials with no owner tenant are shared candidates.
- Tenant-owned credentials are candidates only for that tenant.
- Model allowlists are checked before route selection.
- Empty model allowlists deny all model requests.
- Scope denial returns HTTP 403, not a silent success.
