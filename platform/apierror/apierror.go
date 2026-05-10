// Package apierror provides a typed HTTP API error structure shaped to match
// the OpenAI error wire format:
//
//	{ "error": { "message": "...", "type": "...", "code": "..." } }
//
// Promoted from aosentry/pkg/apierror (the only product in the portfolio that
// already exposes a clean public errors surface) per the standardization plan
// §3 Hidden Gems and Objective 10 (AOSentry pkg/ Promotion).
//
// Pair with platform/httputil.WriteError to render an *APIError to an HTTP
// response with the correct status code and JSON body.
package apierror

import (
	"fmt"
	"net/http"
)

// APIError is a typed API error. The HTTP status code is carried separately
// from the wire body so callers can map it to http.ResponseWriter.WriteHeader
// without leaking it into the JSON response.
type APIError struct {
	StatusCode int    `json:"-"`
	Message    string `json:"message"`
	Type       string `json:"type"`
	Code       string `json:"code,omitempty"`
}

// Error implements the error interface.
func (e *APIError) Error() string {
	return fmt.Sprintf("%s: %s", e.Type, e.Message)
}

// ErrorResponse is the wire envelope: { "error": { ... } }.
type ErrorResponse struct {
	Error *APIError `json:"error"`
}

// AuthenticationError → 401 invalid / missing credentials.
func AuthenticationError(msg string) *APIError {
	return &APIError{
		StatusCode: http.StatusUnauthorized,
		Message:    msg,
		Type:       "authentication_error",
		Code:       "invalid_api_key",
	}
}

// BudgetExceeded → 402 budget cap reached for key / user / team / org.
func BudgetExceeded(msg string) *APIError {
	return &APIError{
		StatusCode: http.StatusPaymentRequired,
		Message:    msg,
		Type:       "budget_exceeded",
		Code:       "budget_exceeded",
	}
}

// RateLimitExceeded → 429 RPM/TPM cap hit.
func RateLimitExceeded(msg string) *APIError {
	return &APIError{
		StatusCode: http.StatusTooManyRequests,
		Message:    msg,
		Type:       "rate_limit_exceeded",
		Code:       "rate_limit_exceeded",
	}
}

// GuardrailBlocked → 400 content policy violation.
func GuardrailBlocked(msg string) *APIError {
	return &APIError{
		StatusCode: http.StatusBadRequest,
		Message:    msg,
		Type:       "guardrail_blocked",
		Code:       "content_policy_violation",
	}
}

// NotFound → 404 resource missing.
func NotFound(msg string) *APIError {
	return &APIError{
		StatusCode: http.StatusNotFound,
		Message:    msg,
		Type:       "not_found",
		Code:       "not_found",
	}
}

// ValidationError → 400 malformed / invalid request body or params.
func ValidationError(msg string) *APIError {
	return &APIError{
		StatusCode: http.StatusBadRequest,
		Message:    msg,
		Type:       "invalid_request_error",
		Code:       "invalid_request",
	}
}

// ProviderError → upstream LLM (or other) provider returned a failure. The
// status code is propagated so a 503 from upstream stays a 503 to the caller.
func ProviderError(msg string, statusCode int) *APIError {
	return &APIError{
		StatusCode: statusCode,
		Message:    msg,
		Type:       "provider_error",
		Code:       "provider_error",
	}
}

// InternalError → 500 unexpected server-side failure.
func InternalError(msg string) *APIError {
	return &APIError{
		StatusCode: http.StatusInternalServerError,
		Message:    msg,
		Type:       "internal_error",
		Code:       "internal_error",
	}
}

// Forbidden → 403 caller is authenticated but lacks permission.
func Forbidden(msg string) *APIError {
	return &APIError{
		StatusCode: http.StatusForbidden,
		Message:    msg,
		Type:       "permission_error",
		Code:       "forbidden",
	}
}

// Conflict → 409 resource state collides (duplicate name, version mismatch).
func Conflict(msg string) *APIError {
	return &APIError{
		StatusCode: http.StatusConflict,
		Message:    msg,
		Type:       "conflict",
		Code:       "conflict",
	}
}
