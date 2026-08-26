// Package api provides the shared HTTP response boundary for Fiber handlers.
// It preserves each endpoint's documented typed payload while centralizing
// status handling and the OpenAI-compatible error envelope.
package api

import "github.com/gofiber/fiber/v3"

type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

type ErrorDetail struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code,omitempty"`
}

type ResponseBuilder struct {
	status int
	data   any
}

func Response() *ResponseBuilder {
	return &ResponseBuilder{status: fiber.StatusOK}
}

func JSON(c fiber.Ctx, data any) error {
	return Response().Data(data).Send(c)
}

func JSONStatus(c fiber.Ctx, status int, data any) error {
	return Response().Status(status).Data(data).Send(c)
}

func (r *ResponseBuilder) Status(status int) *ResponseBuilder {
	r.status = status
	return r
}

func (r *ResponseBuilder) Data(data any) *ResponseBuilder {
	r.data = data
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

func (r *ResponseBuilder) Send(c fiber.Ctx) error {
	if r.status == fiber.StatusNoContent {
		return c.SendStatus(r.status)
	}
	return c.Status(r.status).JSON(r.data)
}
