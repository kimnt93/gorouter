# New Router Specifications

This directory is the implementation contract for `gorouter`. AI agents must read this file and `CHECKLIST.md` before changing code.

## Goal

Build a small, fast, maintainable Go LLM gateway for centralized provider credentials and multi-tenant access. Expose an OpenAI-compatible API while keeping provider credentials, quotas, costs, routing, usage, and prompt cache under centralized control.

This is intentionally smaller than OmniRoute, CLIProxyAPIHome, or new-api. Do not add agents, combos, workflow orchestration, CLI process management, VM provisioning, payments, or unrelated provider features.

## Required Stack

- Go
- Fiber v3 for HTTP delivery
- `html/template` and `embed.FS` for the server-rendered UI
- HTMX for UI interactions and partial updates
- Tailwind CSS for UI styling
- PostgreSQL for durable configuration and initial usage storage
- Redis for distributed prompt cache, rate limits, and quota coordination
- `pgx` for PostgreSQL
- `go-redis/v9` for Redis

## Reference Implementations

Agents should study these references before implementing or changing behavior:

- The OmniRoute repository, <https://github.com/diegosouzapw/OmniRoute>, for gateway feature ideas, routing behavior, usage tracking, pricing, caching, and fallback semantics.
- Fiber clean-architecture practice for package boundaries and dependency direction: <https://docs.gofiber.io/recipes/clean-architecture/>

These are design references only. Do not copy unrelated OmniRoute features or make this service depend on OmniRoute's runtime stack.

## Architecture Rule

Follow the Fiber clean-architecture convention:

```text
api/                 HTTP handlers, presenters, routes, views
pkg/entities/        Domain entities, ports, errors, scopes
pkg/<feature>/       Feature use cases and repository interfaces
repositories/        Durable repository implementations
platform/            PostgreSQL, Redis, provider HTTP adapters
cmd/                 Composition root and executable entrypoints
```

Domain and use-case packages must not import Fiber. HTTP concerns stay in `api/`. Database, Redis, and provider clients stay in `platform/` or `repositories/`.

## Feature Summary

1. Setup master key with full permissions.
2. Login using either the master key or a scoped API key.
3. Multi-tenant API-key management.
4. Per-key model allowlists and access scopes.
5. Multiple provider credentials per model, including multiple accounts for the same provider.
6. Provider catalog split into OAuth subscriptions and API-key connections.
7. Encrypted credential storage.
8. Priority and round-robin routing.
9. Retry/failover and unhealthy-credential cooldown.
10. Per-key USD quotas for UTC day, ISO week, UTC calendar month, or no limit, plus optional RPM limits.
11. Model price management and hourly OpenRouter catalog synchronization into compact database records and an in-memory resolver.
12. Usage and cost logging.
13. Redis-backed, multi-tenant prompt caching.
14. OpenAI-compatible `/v1/models` and `/v1/chat/completions`.
15. Anthropic translation plus Claude Code OAuth refresh support.
16. Codex subscription routing under `cx/<model>` with request-scoped `reasoning` options.
17. Provider health checks, model discovery/import, and direct streaming chat tests.
18. Derived API keys restricted to selected imported models.
19. Embedded Tailwind/HTMX administration UI with inline expandable provider cards.

## Specification Files

| File | Scope |
|---|---|
| `architecture.md` | Package boundaries, dependency rules, composition root |
| `auth-tenancy.md` | Master key, sessions, tenants, API keys, scopes |
| `credentials-routing.md` | Credentials, provider adapters, routes, retry behavior |
| `gateway.md` | OpenAI API, request lifecycle, errors, streaming |
| `quota-cost-usage.md` | Prices, quota reservation, usage and cost accounting |
| `prompt-cache.md` | Redis cache contract, isolation, deterministic caching |
| `storage.md` | PostgreSQL schema, migrations, Redis responsibilities |
| `ui.md` | Templates, HTMX interactions, Tailwind UI requirements |
| `testing.md` | Unit, integration, multi-node, and smoke-test requirements |
| `CHECKLIST.md` | Parallel-agent work plan and completion checklist |
| `identity-organizations-usage/` | User/organization identity, key ownership, usage attribution, API/UI contracts, storage parity, and delegated implementation checklist |

## Agent Rules

- Read the relevant specification before editing.
- Work only on the checklist item assigned to you.
- Do not rewrite another agent's files without checking current changes first.
- Preserve existing user changes.
- Use `apply_patch` for manual edits.
- Add tests with each feature.
- Use typed Go structs for request and response models. Do not use `map[string]any` for defined API responses, persistence models, domain data, or provider payloads when a struct can represent the shape.
- API response envelopes, error bodies, usage summaries, cache statistics, and provider payloads must have explicit structs with JSON tags.
- Run `gofmt`, `go vet ./...`, and relevant tests before reporting completion.
- Do not log secrets, OAuth tokens, prompts, completions, or raw API keys.
- Do not weaken fail-closed authorization or cache isolation.

## Global Definition Of Done

The implementation is complete when all items in `CHECKLIST.md` are checked, the active executable uses Fiber v3, all required tests pass, and a live smoke test proves master login, scoped API-key login, model authorization, provider routing, streaming, quota enforcement, Redis caching, and usage logging.
