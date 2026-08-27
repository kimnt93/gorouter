---
name: fiber-api
description: Add, change, review, or diagnose GoRouter Fiber v3 HTTP APIs, authentication middleware, typed JSON contracts, request-scoped fluent responses, SSE streaming, and route registration. Use for internal/api handlers, routes, or OpenAI-compatible endpoint behavior. Use swaggo-docs for generated API documentation and specialized skills for provider wires or schemas.
---

# Fiber API

Read [fiber-contracts.md](references/fiber-contracts.md) before editing handlers,
middleware, or routes. Its examples are the canonical handler style; adapt the
types and policy calls, but keep the request-scoped response pattern.

## Implement an endpoint

1. Define explicit request and response structs with JSON tags in the handler
   contract boundary. Reuse domain entities only when their public JSON shape is
   intentionally the API contract.
2. Bind with Fiber v3 (`c.Bind().Body(&input)`), validate fields and bounded
   query parameters, and reject malformed input with the request-scoped
   responder's `BadRequest` method.
3. Resolve the authenticated session from the existing middleware helpers.
   Enforce both scope and object policy; never trust an organization ID from a
   query/body to broaden the principal's visibility.
4. Call a service or typed repository query. Keep SQL, encryption, Redis, and
   provider HTTP out of the handler.
5. Bind `responseapi.For(c)` to the current request and return every defined
   JSON success or failure through its fluent builder. Never call `c.JSON`,
   `responseapi.JSON`, `responseapi.JSONStatus`, or `internal/api/presenter`
   from handlers.
6. Register the route in `internal/api/routes/routes.go` with the narrowest
   `handlers.Require` scope. Server-side policy remains required even if the
   React UI hides the action.
7. Add Swag annotations and route/handler tests. Use `$swaggo-docs` to
   regenerate and verify all three files in `internal/docs` when the contract
   changes.

## Streaming

- Set SSE headers before writing: `Content-Type: text/event-stream`,
  `Cache-Control: no-cache`, and `X-Accel-Buffering: no`.
- Preserve cancellation and always settle usage/quota on completion or error.
- Emit valid OpenAI-compatible chunks and a terminal `[DONE]` where required.
- Do not leak raw upstream errors or bodies.

## Verify

Prefer `app.Test(httptest.NewRequest(...))`. Test success, invalid input,
missing scope, horizontal access, visibility narrowing, and safe errors. Run
`go test ./internal/api/...`, then the broader Go suite. For contract changes,
use `$swaggo-docs` and run `internal/api/routes/swagger_test.go` plus the shared
response-contract tests.
