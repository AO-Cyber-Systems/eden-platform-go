package httputil

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSON(t *testing.T) {
	type payload struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}

	tests := []struct {
		name    string
		body    string
		wantErr bool
		errMsg  string
	}{
		{"valid JSON", `{"name":"test","value":42}`, false, ""},
		{"empty body", "", true, "request body is empty"},
		{"invalid JSON", `{not json}`, true, "invalid JSON"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/", strings.NewReader(tt.body))
			var dst payload
			apiErr := DecodeJSON(r, &dst)

			if tt.wantErr {
				if apiErr == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(apiErr.Message, tt.errMsg) {
					t.Fatalf("message = %q, want substring %q", apiErr.Message, tt.errMsg)
				}
				if apiErr.StatusCode != http.StatusBadRequest {
					t.Fatalf("status = %d, want %d", apiErr.StatusCode, http.StatusBadRequest)
				}
				return
			}
			if apiErr != nil {
				t.Fatalf("unexpected error: %v", apiErr)
			}
			if dst.Name != "test" || dst.Value != 42 {
				t.Fatalf("decoded = %+v, want {test 42}", dst)
			}
		})
	}
}

func TestReadBody(t *testing.T) {
	r := httptest.NewRequest("POST", "/", strings.NewReader("hello world"))
	data, apiErr := ReadBody(r)
	if apiErr != nil {
		t.Fatalf("ReadBody error: %v", apiErr)
	}
	if string(data) != "hello world" {
		t.Fatalf("body = %q, want %q", data, "hello world")
	}
}

func TestGetClientIP(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(r *http.Request)
		expected string
	}{
		{
			name:     "X-Forwarded-For",
			setup:    func(r *http.Request) { r.Header.Set("X-Forwarded-For", "203.0.113.50") },
			expected: "203.0.113.50",
		},
		{
			name:     "X-Real-IP",
			setup:    func(r *http.Request) { r.Header.Set("X-Real-IP", "198.51.100.1") },
			expected: "198.51.100.1",
		},
		{
			name:     "fallback to RemoteAddr",
			setup:    func(r *http.Request) { r.RemoteAddr = "192.0.2.1:1234" },
			expected: "192.0.2.1:1234",
		},
		{
			name: "X-Forwarded-For takes priority over X-Real-IP",
			setup: func(r *http.Request) {
				r.Header.Set("X-Forwarded-For", "203.0.113.50")
				r.Header.Set("X-Real-IP", "198.51.100.1")
			},
			expected: "203.0.113.50",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			tt.setup(r)
			if got := GetClientIP(r); got != tt.expected {
				t.Fatalf("GetClientIP = %q, want %q", got, tt.expected)
			}
		})
	}
}
