package httputil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aocybersystems/eden-platform-go/platform/apierror"
)

func TestWriteJSON(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		data       interface{}
		wantStatus int
		wantBody   string
	}{
		{"success with data", http.StatusOK, map[string]string{"message": "hello"}, http.StatusOK, `{"message":"hello"}`},
		{"created with data", http.StatusCreated, map[string]int{"id": 42}, http.StatusCreated, `{"id":42}`},
		{"no content with nil data", http.StatusNoContent, nil, http.StatusNoContent, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			WriteJSON(w, tt.status, tt.data)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", w.Code, tt.wantStatus)
			}
			if got := w.Header().Get("Content-Type"); got != "application/json" {
				t.Fatalf("Content-Type = %q, want application/json", got)
			}
			if tt.wantBody != "" {
				if got := strings.TrimSpace(w.Body.String()); got != tt.wantBody {
					t.Fatalf("body = %q, want %q", got, tt.wantBody)
				}
			}
		})
	}
}

func TestWriteJSON_ComplexStruct(t *testing.T) {
	type resp struct {
		ID    string   `json:"id"`
		Items []string `json:"items"`
	}
	w := httptest.NewRecorder()
	WriteJSON(w, http.StatusOK, resp{ID: "abc", Items: []string{"a", "b"}})

	var got resp
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ID != "abc" {
		t.Fatalf("id = %q, want %q", got.ID, "abc")
	}
	if len(got.Items) != 2 || got.Items[0] != "a" || got.Items[1] != "b" {
		t.Fatalf("items = %v, want [a b]", got.Items)
	}
}

func TestWriteError(t *testing.T) {
	tests := []struct {
		name       string
		apiErr     *apierror.APIError
		wantStatus int
		wantType   string
	}{
		{"authentication", apierror.AuthenticationError("Invalid key"), http.StatusUnauthorized, "authentication_error"},
		{"not found", apierror.NotFound("Model not found"), http.StatusNotFound, "not_found"},
		{"validation", apierror.ValidationError("Bad input"), http.StatusBadRequest, "invalid_request_error"},
		{"internal", apierror.InternalError("Server crash"), http.StatusInternalServerError, "internal_error"},
		{"rate limit", apierror.RateLimitExceeded("Slow down"), http.StatusTooManyRequests, "rate_limit_exceeded"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			WriteError(w, tt.apiErr)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", w.Code, tt.wantStatus)
			}
			if got := w.Header().Get("Content-Type"); got != "application/json" {
				t.Fatalf("Content-Type = %q, want application/json", got)
			}

			var resp apierror.ErrorResponse
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if resp.Error.Type != tt.wantType {
				t.Fatalf("type = %q, want %q", resp.Error.Type, tt.wantType)
			}
			if resp.Error.Message != tt.apiErr.Message {
				t.Fatalf("message = %q, want %q", resp.Error.Message, tt.apiErr.Message)
			}
		})
	}
}

func TestWriteSSEEvent(t *testing.T) {
	w := httptest.NewRecorder()
	WriteSSEEvent(w, "", []byte(`{"text":"hello"}`))

	const want = "data: {\"text\":\"hello\"}\n\n"
	if got := w.Body.String(); got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func TestWriteSSEDone(t *testing.T) {
	w := httptest.NewRecorder()
	WriteSSEDone(w)
	if got := w.Body.String(); got != "data: [DONE]\n\n" {
		t.Fatalf("body = %q, want %q", got, "data: [DONE]\n\n")
	}
}
