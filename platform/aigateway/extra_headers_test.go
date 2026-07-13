package aigateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// capturedRequest holds the headers and decoded body from a single HTTP request
// received by the test server.
type capturedRequest struct {
	header http.Header
	body   map[string]any
}

// newCaptureServer creates an httptest.Server that records incoming requests
// and responds with a minimal valid JSON body. The supplied response body is
// written as-is so callers can shape it per-endpoint.
func newCaptureServer(t *testing.T, responseBody string, out *capturedRequest) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Clone the headers so they survive after the handler returns.
		out.header = r.Header.Clone()

		// Decode the body into a generic map so we can assert key presence/absence.
		out.body = make(map[string]any)
		_ = json.NewDecoder(r.Body).Decode(&out.body)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(responseBody))
	}))
}

// newHeadersTestClient returns a Client wired to srv with fast timeouts.
func newHeadersTestClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	c, err := NewClient(Config{
		BaseURL:    srv.URL,
		APIKey:     "sk-test",
		Timeout:    5 * time.Second,
		MaxRetries: 0,
	}, WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

// chatCompletionResponse is a minimal valid ChatResponse JSON body.
const chatCompletionResponse = `{"id":"c1","object":"chat.completion","created":1,"model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`

// guardrailsResponse is a minimal valid GuardrailsCheckResponse JSON body.
const guardrailsResponse = `{"safe":true,"confidence":1.0}`

// TestCase1_ChatRequestExtraHeadersReachServer verifies that X-* headers set
// on ChatRequest.ExtraHeaders are forwarded as HTTP request headers to the
// upstream (AOCore) service.
func TestCase1_ChatRequestExtraHeadersReachServer(t *testing.T) {
	var cap capturedRequest
	srv := newCaptureServer(t, chatCompletionResponse, &cap)
	defer srv.Close()

	c := newHeadersTestClient(t, srv)

	req := ChatRequest{
		Model: "gpt-4o",
		Messages: []ChatMessage{
			{Role: "user", Content: "hello"},
		},
		ExtraHeaders: map[string]string{
			"X-Household-ID": "hh-1",
			"X-Member-ID":    "u-1",
			"X-Child-Mode":   "false",
		},
	}

	_, err := c.ChatCompletion(context.Background(), req)
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}

	want := map[string]string{
		"X-Household-ID": "hh-1",
		"X-Member-ID":    "u-1",
		"X-Child-Mode":   "false",
	}
	for header, wantVal := range want {
		if got := cap.header.Get(header); got != wantVal {
			t.Errorf("header %q = %q, want %q", header, got, wantVal)
		}
	}
}

// TestCase2_ExtraHeadersNotInJSONBody verifies the json:"-" tag: ExtraHeaders
// must NOT appear in the serialised JSON body sent to the AOCore service.
// A stray "extra_headers" or "ExtraHeaders" key would break the wire contract.
func TestCase2_ExtraHeadersNotInJSONBody(t *testing.T) {
	var cap capturedRequest
	srv := newCaptureServer(t, chatCompletionResponse, &cap)
	defer srv.Close()

	c := newHeadersTestClient(t, srv)

	req := ChatRequest{
		Model: "gpt-4o",
		Messages: []ChatMessage{
			{Role: "user", Content: "hello"},
		},
		ExtraHeaders: map[string]string{
			"X-Household-ID": "hh-1",
		},
	}

	_, err := c.ChatCompletion(context.Background(), req)
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}

	// Neither the snake_case nor the PascalCase key may appear in the body.
	for _, forbiddenKey := range []string{"extra_headers", "ExtraHeaders"} {
		if _, present := cap.body[forbiddenKey]; present {
			t.Errorf("JSON body contains forbidden key %q — ExtraHeaders must use json:\"-\"", forbiddenKey)
		}
	}
}

// TestCase3_CheckGuardrailsWithHeadersForwardsHeaders verifies that the new
// CheckGuardrailsWithHeaders method forwards X-* headers to the guardrails
// endpoint.
func TestCase3_CheckGuardrailsWithHeadersForwardsHeaders(t *testing.T) {
	var cap capturedRequest
	srv := newCaptureServer(t, guardrailsResponse, &cap)
	defer srv.Close()

	c := newHeadersTestClient(t, srv)

	_, err := c.CheckGuardrailsWithHeaders(
		context.Background(),
		"some content",
		"family-safe",
		map[string]string{"user": "u-1"},
		map[string]string{"X-Household-ID": "hh-1"},
	)
	if err != nil {
		t.Fatalf("CheckGuardrailsWithHeaders: %v", err)
	}

	if got := cap.header.Get("X-Household-ID"); got != "hh-1" {
		t.Errorf("X-Household-ID = %q, want %q", got, "hh-1")
	}
}

// TestCase4_NilExtraHeadersNoPanic verifies backward compatibility: a
// ChatRequest with a nil ExtraHeaders map must not panic and must produce the
// same Content-Type + Authorization headers as today (no regression).
func TestCase4_NilExtraHeadersNoPanic(t *testing.T) {
	var cap capturedRequest
	srv := newCaptureServer(t, chatCompletionResponse, &cap)
	defer srv.Close()

	c := newHeadersTestClient(t, srv)

	// ExtraHeaders is nil (zero value) — must not panic.
	req := ChatRequest{
		Model:    "gpt-4o",
		Messages: []ChatMessage{{Role: "user", Content: "hello"}},
	}

	_, err := c.ChatCompletion(context.Background(), req)
	if err != nil {
		t.Fatalf("ChatCompletion with nil ExtraHeaders: %v", err)
	}

	// Standard headers must still be present.
	if got := cap.header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	if got := cap.header.Get("Authorization"); got != "Bearer sk-test" {
		t.Errorf("Authorization = %q, want Bearer sk-test", got)
	}
}
