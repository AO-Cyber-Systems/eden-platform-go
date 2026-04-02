package aosentry

import (
	"encoding/json"
	"fmt"
)

// Error is the base error type for AOSentry client errors.
type Error struct {
	Message    string
	StatusCode int
	Cause      error
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("aosentry: %s (status %d): %v", e.Message, e.StatusCode, e.Cause)
	}
	return fmt.Sprintf("aosentry: %s (status %d)", e.Message, e.StatusCode)
}

func (e *Error) Unwrap() error { return e.Cause }

// Typed error subtypes for caller matching via errors.As().
type (
	TimeoutError            struct{ Err Error }
	RateLimitError          struct{ Err Error }
	BadRequestError         struct{ Err Error }
	AuthenticationError     struct{ Err Error }
	BudgetExceededError     struct{ Err Error }
	ContentFilterError      struct{ Err Error }
	NotFoundError           struct{ Err Error }
	InternalServerError     struct{ Err Error }
	BadGatewayError         struct{ Err Error }
	ServiceUnavailableError struct{ Err Error }
)

func (e *TimeoutError) Error() string            { return e.Err.Error() }
func (e *RateLimitError) Error() string           { return e.Err.Error() }
func (e *BadRequestError) Error() string          { return e.Err.Error() }
func (e *AuthenticationError) Error() string      { return e.Err.Error() }
func (e *BudgetExceededError) Error() string      { return e.Err.Error() }
func (e *ContentFilterError) Error() string       { return e.Err.Error() }
func (e *NotFoundError) Error() string            { return e.Err.Error() }
func (e *InternalServerError) Error() string      { return e.Err.Error() }
func (e *BadGatewayError) Error() string          { return e.Err.Error() }
func (e *ServiceUnavailableError) Error() string  { return e.Err.Error() }

func (e *TimeoutError) Unwrap() error            { return e.Err.Cause }
func (e *RateLimitError) Unwrap() error           { return e.Err.Cause }
func (e *BadRequestError) Unwrap() error          { return e.Err.Cause }
func (e *AuthenticationError) Unwrap() error      { return e.Err.Cause }
func (e *BudgetExceededError) Unwrap() error      { return e.Err.Cause }
func (e *ContentFilterError) Unwrap() error       { return e.Err.Cause }
func (e *NotFoundError) Unwrap() error            { return e.Err.Cause }
func (e *InternalServerError) Unwrap() error      { return e.Err.Cause }
func (e *BadGatewayError) Unwrap() error          { return e.Err.Cause }
func (e *ServiceUnavailableError) Unwrap() error  { return e.Err.Cause }

// MapHTTPError maps an HTTP status code to a typed error.
func MapHTTPError(statusCode int, body []byte) error {
	msg := extractErrorMessage(body)
	base := Error{Message: msg, StatusCode: statusCode}

	switch statusCode {
	case 400:
		return &BadRequestError{Err: base}
	case 401:
		return &AuthenticationError{Err: base}
	case 402:
		return &BudgetExceededError{Err: base}
	case 404:
		return &NotFoundError{Err: base}
	case 429:
		return &RateLimitError{Err: base}
	case 500:
		return &InternalServerError{Err: base}
	case 502:
		return &BadGatewayError{Err: base}
	case 503:
		return &ServiceUnavailableError{Err: base}
	default:
		return &Error{Message: msg, StatusCode: statusCode}
	}
}

func extractErrorMessage(body []byte) string {
	var errResp struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error.Message != "" {
		return errResp.Error.Message
	}
	if len(body) > 200 {
		return string(body[:200])
	}
	return string(body)
}
