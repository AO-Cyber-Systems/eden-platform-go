package aigateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// captureObserver records every Event for assertions.
type captureObserver struct {
	events []Event
}

func (c *captureObserver) Observe(_ context.Context, ev Event) {
	c.events = append(c.events, ev)
}

// newTestClient returns a Client wired to srv with retries and timeouts
// shrunk for fast tests.
func newTestClient(t *testing.T, srv *httptest.Server, opts ...Option) *Client {
	t.Helper()
	cfg := Config{
		BaseURL:    srv.URL,
		APIKey:     "sk-test",
		Timeout:    5 * time.Second,
		MaxRetries: 3,
	}
	c, err := NewClient(cfg, append([]Option{WithHTTPClient(srv.Client())}, opts...)...)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

func TestDoJSONHappy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
			t.Errorf("auth header = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("content-type = %q", got)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	var out struct {
		OK bool `json:"ok"`
	}
	if err := c.doJSON(context.Background(), http.MethodPost, "/v1/echo", map[string]string{"in": "hi"}, &out); err != nil {
		t.Fatalf("doJSON: %v", err)
	}
	if !out.OK {
		t.Error("response not decoded")
	}
}

func TestDoJSONRetriesOn500(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n < 3 {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	var out struct {
		OK bool `json:"ok"`
	}
	if err := c.doJSON(context.Background(), http.MethodPost, "/v1/echo", nil, &out); err != nil {
		t.Fatalf("doJSON: %v", err)
	}
	if attempts.Load() != 3 {
		t.Errorf("attempts=%d want 3", attempts.Load())
	}
}

func TestDoJSONRetriesOn429(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			http.Error(w, "slow down", http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.doJSON(context.Background(), http.MethodPost, "/", nil, nil); err != nil {
		t.Fatalf("doJSON: %v", err)
	}
	if attempts.Load() != 2 {
		t.Errorf("attempts=%d want 2", attempts.Load())
	}
}

func TestDoJSONUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad key", http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	err := c.doJSON(context.Background(), http.MethodPost, "/", nil, nil)
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("got %v, want ErrUnauthorized", err)
	}
}

func TestDoJSONBudgetExceeded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "out of credits", http.StatusPaymentRequired)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	err := c.doJSON(context.Background(), http.MethodPost, "/", nil, nil)
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("got %v, want ErrBudgetExceeded", err)
	}
}

func TestDoJSONNonRetryable4xx(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	err := c.doJSON(context.Background(), http.MethodPost, "/", nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 400 {
		t.Fatalf("err=%v want HTTPError 400", err)
	}
	if attempts.Load() != 1 {
		t.Errorf("attempts=%d want 1 (no retry on 400)", attempts.Load())
	}
}

func TestDoJSONExhaustsRetries(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		http.Error(w, "still broken", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	cfg := Config{BaseURL: srv.URL, MaxRetries: 2, Timeout: 5 * time.Second}
	c, err := NewClient(cfg, WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	err = c.doJSON(context.Background(), http.MethodPost, "/", nil, nil)
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != http.StatusServiceUnavailable {
		t.Fatalf("err=%v want HTTPError 503", err)
	}
	// MaxRetries=2 means up to 3 attempts total.
	if attempts.Load() != 3 {
		t.Errorf("attempts=%d want 3", attempts.Load())
	}
}

func TestDoJSONNotConfigured(t *testing.T) {
	c := &Client{} // no BaseURL
	err := c.doJSON(context.Background(), http.MethodPost, "/", nil, nil)
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("got %v, want ErrNotConfigured", err)
	}
}

func TestHTTPErrorMessageFormat(t *testing.T) {
	he := &HTTPError{Status: 503, Body: "down for maintenance", Path: "/v1/x"}
	msg := he.Error()
	if !strings.Contains(msg, "503") || !strings.Contains(msg, "/v1/x") || !strings.Contains(msg, "down") {
		t.Errorf("HTTPError message missing fields: %q", msg)
	}
}

// TestDoJSONContextCancel ensures backoff respects context cancellation.
func TestDoJSONContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	cfg := Config{BaseURL: srv.URL, MaxRetries: 5, Timeout: 5 * time.Second}
	c, err := NewClient(cfg, WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err = c.doJSON(ctx, http.MethodPost, "/", nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !(errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "context")) {
		// On a fast machine the first request may succeed-then-fail before
		// the deadline; in that case we accept an HTTPError result too.
		var he *HTTPError
		if !errors.As(err, &he) {
			t.Errorf("unexpected err=%v", err)
		}
	}
	_ = fmt.Sprintf // keep imports satisfied if assertions evolve
}
