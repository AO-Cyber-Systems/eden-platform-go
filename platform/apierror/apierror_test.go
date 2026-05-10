package apierror

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestAPIError_Error(t *testing.T) {
	err := &APIError{
		StatusCode: 401,
		Message:    "Invalid API key",
		Type:       "authentication_error",
		Code:       "invalid_api_key",
	}
	const want = "authentication_error: Invalid API key"
	if got := err.Error(); got != want {
		t.Fatalf("APIError.Error() = %q, want %q", got, want)
	}
}

func TestErrorResponse_OpenAIWireShape(t *testing.T) {
	resp := ErrorResponse{Error: AuthenticationError("No API key provided")}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var parsed map[string]map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	body := parsed["error"]
	if body == nil {
		t.Fatal(`expected an "error" key in the JSON envelope`)
	}
	if got := body["message"]; got != "No API key provided" {
		t.Fatalf("error.message = %v, want %q", got, "No API key provided")
	}
	if got := body["type"]; got != "authentication_error" {
		t.Fatalf("error.type = %v, want %q", got, "authentication_error")
	}
	if got := body["code"]; got != "invalid_api_key" {
		t.Fatalf("error.code = %v, want %q", got, "invalid_api_key")
	}
}

func TestErrorResponse_CodeOmittedWhenEmpty(t *testing.T) {
	body := &APIError{
		StatusCode: 500,
		Message:    "something went wrong",
		Type:       "internal_error",
		Code:       "",
	}
	data, err := json.Marshal(ErrorResponse{Error: body})
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	if strings.Contains(string(data), `"code"`) {
		t.Fatalf("expected omitempty to drop code, got %q", string(data))
	}
}

func TestConstructors_StatusAndShape(t *testing.T) {
	cases := []struct {
		name       string
		err        *APIError
		wantStatus int
		wantType   string
		wantCode   string
	}{
		{"AuthenticationError", AuthenticationError("x"), http.StatusUnauthorized, "authentication_error", "invalid_api_key"},
		{"BudgetExceeded", BudgetExceeded("x"), http.StatusPaymentRequired, "budget_exceeded", "budget_exceeded"},
		{"RateLimitExceeded", RateLimitExceeded("x"), http.StatusTooManyRequests, "rate_limit_exceeded", "rate_limit_exceeded"},
		{"GuardrailBlocked", GuardrailBlocked("x"), http.StatusBadRequest, "guardrail_blocked", "content_policy_violation"},
		{"NotFound", NotFound("x"), http.StatusNotFound, "not_found", "not_found"},
		{"ValidationError", ValidationError("x"), http.StatusBadRequest, "invalid_request_error", "invalid_request"},
		{"InternalError", InternalError("x"), http.StatusInternalServerError, "internal_error", "internal_error"},
		{"Forbidden", Forbidden("x"), http.StatusForbidden, "permission_error", "forbidden"},
		{"Conflict", Conflict("x"), http.StatusConflict, "conflict", "conflict"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d", tc.err.StatusCode, tc.wantStatus)
			}
			if tc.err.Type != tc.wantType {
				t.Fatalf("type = %q, want %q", tc.err.Type, tc.wantType)
			}
			if tc.err.Code != tc.wantCode {
				t.Fatalf("code = %q, want %q", tc.err.Code, tc.wantCode)
			}
			if tc.err.Message != "x" {
				t.Fatalf("message = %q, want %q", tc.err.Message, "x")
			}
		})
	}
}

func TestProviderError_PropagatesStatusCode(t *testing.T) {
	err := ProviderError("OpenAI returned 503", http.StatusBadGateway)
	if err.StatusCode != http.StatusBadGateway {
		t.Fatalf("ProviderError status = %d, want %d", err.StatusCode, http.StatusBadGateway)
	}
	if err.Type != "provider_error" || err.Code != "provider_error" {
		t.Fatalf("ProviderError type/code = %q/%q", err.Type, err.Code)
	}
}

func TestAllConstructors_ImplementErrorInterface(t *testing.T) {
	all := []*APIError{
		AuthenticationError("test"),
		BudgetExceeded("test"),
		RateLimitExceeded("test"),
		GuardrailBlocked("test"),
		NotFound("test"),
		ValidationError("test"),
		ProviderError("test", 502),
		InternalError("test"),
		Forbidden("test"),
		Conflict("test"),
	}
	for _, e := range all {
		var asErr error = e
		if asErr.Error() == "" {
			t.Fatalf("APIError.Error() empty for %s", e.Type)
		}
		if !strings.Contains(asErr.Error(), "test") {
			t.Fatalf("APIError.Error() = %q; expected to contain message %q", asErr.Error(), "test")
		}
	}
}
