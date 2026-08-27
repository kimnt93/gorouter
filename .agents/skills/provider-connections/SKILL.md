---
name: provider-connections
description: Add, update, test, or diagnose GoRouter LLM provider connections, API-key and OAuth flows, model discovery/import, provider adapters, streaming translation, quota snapshots, cache-token usage, and provider model namespaces. Use whenever a provider or connection behavior changes. Do not use for generic model-route UI edits with no provider behavior.
---

# Provider connections

Read [provider-playbook.md](references/provider-playbook.md) for the end-to-end
implementation path. Read [provider-contracts.md](references/provider-contracts.md)
when working on wire formats, OAuth, streaming, or token accounting.

## Required approach

1. Identify the provider's authentication kind, protocol, canonical base URL,
   public prefix, discovery behavior, OAuth refresh needs, and quota support.
2. Extend `pkg/provider/catalog.go`; use its helpers for base URLs and public
   model IDs. Never duplicate prefix/namespace logic in handlers or UI.
3. Implement the smallest adapter against the common runtime interfaces under
   `internal/platform/llm`. Keep provider request/response structs typed and
   isolate non-standard translation in that adapter.
4. Add OAuth driver/service behavior under `pkg/oauth` only for OAuth
   providers. Keep verifier, state, tokens, and refresh metadata server-side.
5. Register connectivity, upstream, OAuth, quota, and composition dependencies
   in `cmd/gorouter/main.go` and expose safe static metadata through the provider
   catalog/API.
6. Preserve credential ownership: personal connections are derived from the
   authenticated user; organization-owned connections require authorized
   organization context; master-created connections may be global. Never trust
   a client-supplied owner beyond policy validation.
7. Import personal/global models as `<prefix>/<model>` and organization routes
   as `<org-slug>/<prefix>/<model>`. Cost resolution follows the original
   upstream model while usage retains the public model.
8. Treat prompt caching as a provider capability: preserve supported client
   markers/keys, keep stable prefixes byte-stable, normalize provider usage,
   and preserve explicit session affinity across routed credentials.
9. Test connectivity, discovery, non-stream and stream chat, refresh-on-401,
   model namespace, safe errors, concurrent requests, and all four token/cost
   components.

## Safety

Use mock upstreams for routine tests. Do not print credentials, OAuth blobs,
authorization headers, request prompts, completions, or raw upstream bodies.
Run live or stress tests only when explicitly requested, with bounded
concurrency/output and a known-cost model.
