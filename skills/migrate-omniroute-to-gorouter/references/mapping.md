# OmniRoute mapping

The included script supports OmniRoute's `provider_connections`,
`model_capabilities`, and `usage_history` SQLite tables.

## Stable IDs and namespaces

- Credential ID: `cred_omni_` plus the first 24 hexadecimal characters of the
  SHA-256 digest of the OmniRoute connection ID.
- Usage event ID: `usage_omni_<usage_history.id>`.
- Provider/model namespaces: `codex` → `cx`, `opencode-zen` → `ocz`, and
  `xai-oauth` → `xai`. Other supported providers use their provider ID as the
  prefix.

The script only imports providers accepted by the target GoRouter schema. It
reports unsupported providers instead of silently changing their connector or
protocol.

## Secrets

OmniRoute fields beginning with `enc:v1:` use AES-256-GCM. Their key is derived
with scrypt from `STORAGE_ENCRYPTION_KEY`, salt
`omniroute-field-encryption-v1`, `N=16384`, `r=8`, `p=1`, and `dkLen=32`.

GoRouter credential sealing derives SHA-256 from
`gorouter:credential-encryption:<MASTER_KEY>`, converts it to unpadded URL-safe
base64, hashes that string again, and stores AES-GCM nonce plus ciphertext.
Never print either plaintext or sealed credential material.

## Usage

OmniRoute `tokens_input` includes cache-read and cache-creation tokens. GoRouter
stores them separately, so:

```text
prompt_tokens = max(tokens_input - tokens_cache_read - tokens_cache_creation, 0)
```

The import recalculates cost components from GoRouter prices. A missing target
price means Free, with every imported cost component set to zero. Historical
actor snapshots are imported as the master migration actor because OmniRoute
does not provide GoRouter user/organization identity snapshots.

