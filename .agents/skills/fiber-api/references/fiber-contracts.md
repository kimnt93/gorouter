# Fiber v3 and HTTP contracts

## Existing pattern

Handlers use Fiber v3's value context type:

```go
func (a *Admin) Example(c fiber.Ctx) error {
	var input ExampleRequest
	if err := c.Bind().Body(&input); err != nil {
		return presenter.BadRequest(c, "invalid example request")
	}
	result, err := a.Service.Do(c.Context(), input)
	if err != nil {
		return presenter.ServerError(c, "example failed")
	}
	return responseapi.JSONStatus(c, fiber.StatusCreated, ExampleResponse{Data: result})
}
```

Use `c.Method()`, `c.Params`, `c.Query`, `c.Get`, `c.Context`, and
`c.Bind().Body` consistently with nearby handlers. Bound list limits (normally
100 by default and 500 maximum where established), parse RFC3339 explicitly,
and use opaque cursor helpers instead of exposing offsets for identity/usage
history.

## Responses and errors

- Return defined JSON through `internal/api.JSON` or `JSONStatus`.
- Return failures through `internal/api/presenter`; its `Error` alias is the
  documented OpenAI-style envelope.
- Do not leak repository errors or upstream bodies in a presenter message.
- Use 401 for missing/invalid authentication, 403 for missing capability, 404
  to conceal a foreign private object, 409 for domain conflicts, 429 for quota
  or rate limits, and a safe 502/503 for upstream/coordination failures.
- Keep response envelopes and list fields stable (`object`, `data`,
  `next_cursor`) and typed.

## Authentication and object policy

- Register coarse capability checks with `handlers.Require(auth, scope)`.
- Read the revalidated session/access context provided by middleware helpers.
- Call `pkg/policy` and services for object-level decisions. A matching scope
  does not authorize access to another user or organization.
- Treat `organization_id` as a requested narrowing context. Validate it against
  master/View As or active organization-admin membership before using it.
- Query filters may narrow the server-computed visibility only.

## Routes and SPA

- Add JSON management endpoints under `/admin`.
- Keep OpenAI-compatible model endpoints under `/v1` with Bearer/API-key
  authentication.
- Dashboard GET routes serve the React SPA; do not add a second page-specific
  frontend router.
- Legacy `/ui` mutation routes may remain for compatibility but new dashboard
  behavior should use typed `/admin` APIs.

## Swagger

Use `$swaggo-docs` for annotation requirements, pinned generation, drift checks,
and API documentation tests. Do not edit `internal/docs` by hand.

## SSE

- Send `Content-Type: text/event-stream`, `Cache-Control: no-cache`, and
  `X-Accel-Buffering: no`.
- Use OpenAI chunk types and valid `data:` frames separated by blank lines.
- Forward safe incremental tool/function data where the adapter contract
  supports it.
- Handle client disconnect and provider EOF distinctly from malformed frames.
- Finalize usage, quota settlement, health reporting, and cache decisions even
  when streaming exits early.
