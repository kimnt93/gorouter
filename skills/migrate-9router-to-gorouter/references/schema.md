# Supported 9Router schema

This skill targets the SQLite schema in `decolua/9router` where the database is
normally `~/.9router/db/data.sqlite`.

Required tables:

- `providerConnections`: typed columns plus a JSON `data` field containing
  `apiKey`, `accessToken`, `refreshToken`, `idToken`, `defaultModel`, and
  provider-specific metadata.
- `usageHistory`: request identity, provider/model, prompt/completion columns,
  aggregate cost, and a JSON `tokens` object.

The script validates these tables and required columns before reading. A fork
with different table names or encrypted source fields must be handled by an
explicit adapter; do not guess its schema.

## Mapping

- Credential ID: `cred_9r_` plus the first 24 hexadecimal characters of the
  SHA-256 digest of the source connection ID.
- Usage event ID: `usage_9r_<usageHistory.id>`.
- Model namespace: `codex` → `cx`, `opencode-zen` → `ocz`; other supported
  providers retain their GoRouter prefix.
- `tokens.cached_tokens` or `cache_read_input_tokens` becomes GoRouter
  cache-read tokens. Cache-write tokens use
  `cache_creation_input_tokens` when present.

9Router stores provider secrets as plaintext inside its local JSON data field.
The script seals them with the target GoRouter master key before staging and
never prints them. It does not import 9Router client API keys: create scoped
GoRouter keys so ownership, model allowlists, quota, and plaintext-once rules
are enforced by GoRouter.

