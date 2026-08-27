# Fiber v3 and HTTP contracts

## Request-scoped response pattern

Handlers use Fiber v3's value context type:

```go
func (a *Admin) Example(c fiber.Ctx) error {
	api := responseapi.For(c)

	var input ExampleRequest
	if err := c.Bind().Body(&input); err != nil {
		return api.BadRequest("invalid example request").Send()
	}
	result, err := a.Service.Do(c.Context(), input)
	if err != nil {
		return api.InternalError("example failed").Send()
	}
	return api.Response().
		Status(fiber.StatusCreated).
		Data(ExampleResponse{Data: result}).
		Send()
}
```

`responseapi.For(c)` binds the responder to one request. This binding is
required because `Send()` is intentionally argument-free and package-global
Fiber context would mix concurrent requests. Inline use is also valid:

```go
return responseapi.For(c).BadRequest("the invalid message").Send()
```

Bind the responder once when a handler has multiple return paths. Inline
`responseapi.For(c)` is acceptable for a one-return handler. Keep a multi-step
chain one method per line; a short response may stay on one line.

Use `c.Method()`, `c.Params`, `c.Query`, `c.Get`, `c.Context`, and
`c.Bind().Body` consistently with nearby handlers. Bound list limits (normally
100 by default and 500 maximum where established), parse RFC3339 explicitly,
and use opaque cursor helpers instead of exposing offsets for identity/usage
history.

## Responses and errors

- Return defined JSON through `responseapi.For(c).Response()`, set the status
  explicitly, attach the typed payload with `Data`, and finish with `Send()`.
- Return failures through request-scoped helpers such as `BadRequest`,
  `Unauthorized`, `Forbidden`, `NotFound`, `Conflict`, and `InternalError`.
  Use `Error(status, message, type, code)` for a status without a named helper.
- Document failures as `responseapi.ErrorResponse`, the shared OpenAI-style
  error envelope.
- Do not use direct `c.JSON`, legacy `JSON`/`JSONStatus` helpers, or the
  compatibility `internal/api/presenter` package in handlers.
- Do not leak repository errors or upstream bodies in an error message.
- Use 401 for missing/invalid authentication, 403 for missing capability, 404
  to conceal a foreign private object, 409 for domain conflicts, 429 for quota
  or rate limits, and a safe 502/503 for upstream/coordination failures.
- Keep response envelopes and list fields stable (`object`, `data`,
  `next_cursor`) and typed.

For a cursor list, use the fluent envelope fields instead of constructing a
duplicate wrapper at the return site:

```go
return api.Response().
	Status(fiber.StatusOK).
	Object("list").
	Data(items).
	Next(nextCursor).
	Send()
```

For an offset page, preserve the established top-level fields with `Paging`:

```go
return api.Response().
	Status(fiber.StatusOK).
	Data(items).
	Paging(total, offset, limit).
	Send()
```

`NextCursor` is an alias for `Next`. Passing an empty cursor omits
`next_cursor`; paging values remain present even when they are zero.

## Canonical handler examples

### Typed create operation

This is the preferred shape for a JSON mutation: typed input, server-derived
actor, policy before the service call, safe error mapping, explicit status,
and typed output.

```go
type WidgetCreateRequest struct {
	Name string `json:"name"`
}

type WidgetCreateResponse struct {
	Widget *entities.Widget `json:"widget"`
}

func (a *Admin) CreateWidget(c fiber.Ctx) error {
	api := responseapi.For(c)
	actor := principalFromSession(SessionFrom(c))
	if err := policy.ManageWidgets(actor); err != nil {
		return api.Forbidden("widget management is not allowed").Send()
	}

	var input WidgetCreateRequest
	if err := c.Bind().Body(&input); err != nil {
		return api.BadRequest("invalid widget request").Send()
	}
	widget, err := a.Widgets.Create(c.Context(), actor, input.Name)
	if errors.Is(err, entities.ErrConflict) {
		return api.Conflict("widget already exists", "widget_exists").Send()
	}
	if err != nil {
		return api.InternalError("failed to create widget").Send()
	}
	return api.Response().
		Status(fiber.StatusCreated).
		Data(WidgetCreateResponse{Widget: widget}).
		Send()
}
```

Do not return `err.Error()` unless the error is an explicitly safe validation
contract. Repository, Redis, encryption, and upstream errors are operational
details and receive a stable public message.

### Cursor list

```go
func (a *Admin) Widgets(c fiber.Ctx) error {
	api := responseapi.For(c)
	limit, err := strconv.Atoi(c.Query("limit", "100"))
	if err != nil || limit < 1 || limit > 500 {
		return api.BadRequest("limit must be between 1 and 500").Send()
	}
	items, next, err := a.Widgets.List(c.Context(), entities.PageQuery{
		Cursor: c.Query("cursor"),
		Limit:  limit,
		Query:  strings.TrimSpace(c.Query("q")),
	})
	if err != nil {
		return api.InternalError("failed to list widgets").Send()
	}
	return api.Response().
		Status(fiber.StatusOK).
		Object("list").
		Data(items).
		Next(next).
		Send()
}
```

### No-content and non-JSON exceptions

```go
return api.Response().Status(fiber.StatusNoContent).Send()
```

The fluent responder is for defined JSON responses. Static assets, HTML,
redirects, and an SSE stream may use the appropriate Fiber send/stream API.
Those branches must still set their explicit status, content type, cache, and
streaming headers.

### Handler test

```go
func TestCreateWidgetRejectsInvalidBody(t *testing.T) {
	app := fiber.New()
	app.Post("/widgets", admin.CreateWidget)

	request := httptest.NewRequest(fiber.MethodPost, "/widgets", strings.NewReader("{"))
	request.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
	response, err := app.Test(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status=%d", response.StatusCode)
	}
	var body responseapi.ErrorResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Type != "invalid_request_error" {
		t.Fatalf("error=%+v", body.Error)
	}
}
```

Use real services/fakes when the handler has policy or ownership behavior; do
not bypass middleware in a route-level authorization test.

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
