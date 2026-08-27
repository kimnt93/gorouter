// Package api provides the shared HTTP response boundary for Fiber handlers.
// It preserves each endpoint's documented typed payload while centralizing
// status handling and the OpenAI-compatible error envelope.
package api

import (
	"errors"

	"github.com/gofiber/fiber/v3"
)

// ErrMissingContext reports a response builder that was not bound with For.
var ErrMissingContext = errors.New("response builder has no Fiber context")

type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

type ErrorDetail struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code,omitempty"`
}

// Responder binds response construction to one Fiber request. Keeping the
// context on a request-scoped value allows Send to stay argument-free without
// introducing process-global state that could cross concurrent requests.
type Responder struct {
	ctx fiber.Ctx
}

type ResponseBuilder struct {
	responder  *Responder
	status     int
	data       any
	object     string
	nextCursor string
	envelope   bool
	total      *int
	offset     *int
	limit      *int
}

type responseEnvelope struct {
	Object     string `json:"object,omitempty"`
	Data       any    `json:"data"`
	NextCursor string `json:"next_cursor,omitempty"`
	Total      *int   `json:"total,omitempty"`
	Offset     *int   `json:"offset,omitempty"`
	Limit      *int   `json:"limit,omitempty"`
}

// For creates a fluent responder bound to the current Fiber request.
func For(c fiber.Ctx) *Responder {
	return &Responder{ctx: c}
}

func (a *Responder) Response() *ResponseBuilder {
	return &ResponseBuilder{responder: a, status: fiber.StatusOK}
}

func (r *ResponseBuilder) Status(status int) *ResponseBuilder {
	r.status = status
	return r
}

func (r *ResponseBuilder) Data(data any) *ResponseBuilder {
	r.data = data
	return r
}

// Object sets the top-level object discriminator and wraps Data in the shared
// list/page envelope.
func (r *ResponseBuilder) Object(object string) *ResponseBuilder {
	r.object = object
	r.envelope = true
	return r
}

// Next sets the opaque next_cursor value and wraps Data in the shared envelope.
func (r *ResponseBuilder) Next(next string) *ResponseBuilder {
	r.nextCursor = next
	r.envelope = true
	return r
}

func (r *ResponseBuilder) NextCursor(next string) *ResponseBuilder {
	return r.Next(next)
}

// Paging adds the established top-level total, offset, and limit values.
func (r *ResponseBuilder) Paging(total, offset, limit int) *ResponseBuilder {
	r.envelope = true
	r.total = &total
	r.offset = &offset
	r.limit = &limit
	return r
}

func (r *ResponseBuilder) Error(status int, message, errorType, code string) *ResponseBuilder {
	r.status = status
	r.data = ErrorResponse{Error: ErrorDetail{Message: message, Type: errorType, Code: code}}
	return r
}

func (r *ResponseBuilder) BadRequest(message string) *ResponseBuilder {
	return r.Error(fiber.StatusBadRequest, message, "invalid_request_error", "")
}

func (r *ResponseBuilder) Unauthorized(message string) *ResponseBuilder {
	return r.Error(fiber.StatusUnauthorized, message, "authentication_error", "invalid_api_key")
}

func (r *ResponseBuilder) Forbidden(message string) *ResponseBuilder {
	return r.Error(fiber.StatusForbidden, message, "permission_error", "")
}

func (r *ResponseBuilder) NotFound(message string) *ResponseBuilder {
	return r.Error(fiber.StatusNotFound, message, "invalid_request_error", "not_found")
}

func (r *ResponseBuilder) Conflict(message, code string) *ResponseBuilder {
	return r.Error(fiber.StatusConflict, message, "conflict", code)
}

func (r *ResponseBuilder) InternalError(message string) *ResponseBuilder {
	return r.Error(fiber.StatusInternalServerError, message, "server_error", "")
}

func (a *Responder) Error(status int, message, errorType, code string) *ResponseBuilder {
	return a.Response().Error(status, message, errorType, code)
}

func (a *Responder) BadRequest(message string) *ResponseBuilder {
	return a.Response().BadRequest(message)
}

func (a *Responder) Unauthorized(message string) *ResponseBuilder {
	return a.Response().Unauthorized(message)
}

func (a *Responder) Forbidden(message string) *ResponseBuilder {
	return a.Response().Forbidden(message)
}

func (a *Responder) NotFound(message string) *ResponseBuilder {
	return a.Response().NotFound(message)
}

func (a *Responder) Conflict(message, code string) *ResponseBuilder {
	return a.Response().Conflict(message, code)
}

func (a *Responder) InternalError(message string) *ResponseBuilder {
	return a.Response().InternalError(message)
}

func (r *ResponseBuilder) Send() error {
	if r.responder == nil || r.responder.ctx == nil {
		return ErrMissingContext
	}
	c := r.responder.ctx
	if r.status == fiber.StatusNoContent {
		return c.SendStatus(r.status)
	}
	data := r.data
	if r.envelope {
		data = responseEnvelope{
			Object: r.object, Data: r.data, NextCursor: r.nextCursor,
			Total: r.total, Offset: r.offset, Limit: r.limit,
		}
	}
	return c.Status(r.status).JSON(data)
}
