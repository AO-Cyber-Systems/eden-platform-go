// Test list (hand-written — no LLM-generated payloads):
//   - TestHIBPScreener_KnownCompromisedPassword
//   - TestHIBPScreener_UnknownPassword
//   - TestHIBPScreener_TransientHTTP500_Retries
//   - TestHIBPScreener_Persistent500_FailsOpen
//   - TestHIBPScreener_HTTP429_Retries
//   - TestHIBPScreener_MalformedBody_ReturnsErr
//   - TestHIBPScreener_RespectsContextCancellation
//   - TestHIBPScreener_AddPaddingHeader
//   - TestHIBPScreener_Prefix5Chars
//   - TestHIBPScreener_EmptyPassword
//   - TestHIBPScreener_RateLimiter
package breach

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// hibpHashSuffix returns the SHA-1 of password in uppercase hex; the test
// fixtures use this to build canned response bodies.
func hibpHashSuffix(t *testing.T, password string) (prefix, suffix string) {
	t.Helper()
	sum := sha1.Sum([]byte(password))
	full := strings.ToUpper(hex.EncodeToString(sum[:]))
	return full[:5], full[5:]
}

// fastClient builds an HIBPScreener pointed at the given stub with
// no rate limit and short backoffs so tests stay sub-second.
func fastClient(t *testing.T, ts *httptest.Server, opts ...HIBPOption) *HIBPScreener {
	t.Helper()
	base := []HIBPOption{
		WithEndpoint(ts.URL + "/range/"),
		WithRateLimit(1000), // effectively unlimited
		WithMaxRetries(3),
		WithRetryBackoff(5 * time.Millisecond),
	}
	return NewHIBPScreener(append(base, opts...)...)
}

// TestHIBPScreener_KnownCompromisedPassword — stub returns a suffix list
// containing the SHA-1 suffix of "password"; assert compromised=true.
func TestHIBPScreener_KnownCompromisedPassword(t *testing.T) {
	_, suffix := hibpHashSuffix(t, "password")
	body := fmt.Sprintf("0018A45C4D1DEF81644B54AB7F969B88D65:1\r\n%s:9545824\r\nDEADBEEFCAFEBABEDEADBEEFCAFEBABEDEAD:0\r\n", suffix)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, body)
	}))
	defer ts.Close()

	s := fastClient(t, ts)
	compromised, count, err := s.Check(context.Background(), "password")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !compromised {
		t.Fatal("expected compromised=true")
	}
	if count != 9545824 {
		t.Fatalf("expected occurrences=9545824, got %d", count)
	}
}

// TestHIBPScreener_UnknownPassword — stub returns no suffix matching
// the hash of the queried password; assert compromised=false, err=nil.
func TestHIBPScreener_UnknownPassword(t *testing.T) {
	body := "0018A45C4D1DEF81644B54AB7F969B88D65:1\r\nDEADBEEFCAFEBABEDEADBEEFCAFEBABEDEAD:0\r\n"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, body)
	}))
	defer ts.Close()

	s := fastClient(t, ts)
	compromised, count, err := s.Check(context.Background(), "correct horse battery staple")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if compromised {
		t.Fatal("expected compromised=false")
	}
	if count != 0 {
		t.Fatalf("expected occurrences=0, got %d", count)
	}
}

// TestHIBPScreener_TransientHTTP500_Retries — stub returns 500 twice then 200;
// assert success after retries.
func TestHIBPScreener_TransientHTTP500_Retries(t *testing.T) {
	var calls atomic.Int32
	_, suffix := hibpHashSuffix(t, "qwerty")
	body := fmt.Sprintf("%s:3912816\r\n", suffix)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n < 3 {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		fmt.Fprint(w, body)
	}))
	defer ts.Close()

	s := fastClient(t, ts)
	compromised, count, err := s.Check(context.Background(), "qwerty")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !compromised {
		t.Fatalf("expected compromised=true after retry, got false")
	}
	if count != 3912816 {
		t.Fatalf("expected occurrences=3912816, got %d", count)
	}
	if calls.Load() != 3 {
		t.Fatalf("expected 3 attempts, got %d", calls.Load())
	}
}

// TestHIBPScreener_Persistent500_FailsOpen — all attempts return 500;
// assert (false, 0, ErrScreenerUnavailable).
func TestHIBPScreener_Persistent500_FailsOpen(t *testing.T) {
	var calls atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer ts.Close()

	s := fastClient(t, ts)
	compromised, count, err := s.Check(context.Background(), "password")
	if !errors.Is(err, ErrScreenerUnavailable) {
		t.Fatalf("expected ErrScreenerUnavailable, got %v", err)
	}
	if compromised {
		t.Fatalf("expected compromised=false on fail-open")
	}
	if count != 0 {
		t.Fatalf("expected count=0 on fail-open, got %d", count)
	}
	if calls.Load() < 4 { // maxRetries=3 → 1 initial + 3 retries
		t.Fatalf("expected at least 4 attempts, got %d", calls.Load())
	}
}

// TestHIBPScreener_HTTP429_Retries — stub returns 429 once then 200; success.
func TestHIBPScreener_HTTP429_Retries(t *testing.T) {
	var calls atomic.Int32
	_, suffix := hibpHashSuffix(t, "123456")
	body := fmt.Sprintf("%s:37359195\r\n", suffix)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			http.Error(w, "rate limited", http.StatusTooManyRequests)
			return
		}
		fmt.Fprint(w, body)
	}))
	defer ts.Close()

	s := fastClient(t, ts)
	compromised, _, err := s.Check(context.Background(), "123456")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !compromised {
		t.Fatalf("expected compromised=true")
	}
	if calls.Load() != 2 {
		t.Fatalf("expected 2 attempts, got %d", calls.Load())
	}
}

// TestHIBPScreener_MalformedBody_ReturnsErr — 200 with non-parseable
// suffix:count line that happens to match the queried hash returns err.
func TestHIBPScreener_MalformedBody_ReturnsErr(t *testing.T) {
	_, suffix := hibpHashSuffix(t, "letmein")
	body := fmt.Sprintf("%s:not-a-number\r\n", suffix)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, body)
	}))
	defer ts.Close()

	s := fastClient(t, ts)
	compromised, _, err := s.Check(context.Background(), "letmein")
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
	if compromised {
		t.Fatal("expected compromised=false on parse error")
	}
}

// TestHIBPScreener_RespectsContextCancellation — cancel ctx mid-request.
func TestHIBPScreener_RespectsContextCancellation(t *testing.T) {
	// Stub blocks until the request context is cancelled OR the server
	// shuts down — either signal releases the handler so ts.Close() doesn't
	// hang waiting for an orphaned connection.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer ts.Close()

	s := fastClient(t, ts)
	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after 30ms so the request has started.
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()
	_, _, err := s.Check(ctx, "password")
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	// Acceptable signals: ctx.Err() directly, ErrScreenerUnavailable
	// wrapping it (after retries exhaust), or any wrap of context.Canceled.
	if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, ErrScreenerUnavailable) {
		t.Fatalf("expected context-cancellation or ErrScreenerUnavailable, got %v", err)
	}
}

// TestHIBPScreener_AddPaddingHeader — outgoing request MUST set Add-Padding: true.
func TestHIBPScreener_AddPaddingHeader(t *testing.T) {
	var sawHeader atomic.Bool
	_, suffix := hibpHashSuffix(t, "password")
	body := fmt.Sprintf("%s:9545824\r\n", suffix)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Add-Padding") == "true" {
			sawHeader.Store(true)
		}
		fmt.Fprint(w, body)
	}))
	defer ts.Close()

	s := fastClient(t, ts)
	if _, _, err := s.Check(context.Background(), "password"); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !sawHeader.Load() {
		t.Fatal("Add-Padding: true header missing on outgoing request")
	}
}

// TestHIBPScreener_Prefix5Chars — outgoing request path must end in /range/<5 uppercase hex>.
func TestHIBPScreener_Prefix5Chars(t *testing.T) {
	var seenPath atomic.Value
	_, suffix := hibpHashSuffix(t, "password")
	body := fmt.Sprintf("%s:9545824\r\n", suffix)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath.Store(r.URL.Path)
		fmt.Fprint(w, body)
	}))
	defer ts.Close()

	s := fastClient(t, ts)
	if _, _, err := s.Check(context.Background(), "password"); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	got, _ := seenPath.Load().(string)
	if !strings.HasPrefix(got, "/range/") {
		t.Fatalf("expected path /range/<prefix>, got %q", got)
	}
	prefix := strings.TrimPrefix(got, "/range/")
	if len(prefix) != 5 {
		t.Fatalf("expected 5-char prefix, got %q (len %d)", prefix, len(prefix))
	}
	for _, c := range prefix {
		isUpperHex := (c >= '0' && c <= '9') || (c >= 'A' && c <= 'F')
		if !isUpperHex {
			t.Fatalf("prefix %q contains non-uppercase-hex char %q", prefix, c)
		}
	}
	expectedPrefix, _ := hibpHashSuffix(t, "password")
	if prefix != expectedPrefix {
		t.Fatalf("expected prefix %q, got %q", expectedPrefix, prefix)
	}
}

// TestHIBPScreener_EmptyPassword — empty input returns (false, 0, nil)
// without making any HTTP request.
func TestHIBPScreener_EmptyPassword(t *testing.T) {
	var calls atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, "")
	}))
	defer ts.Close()

	s := fastClient(t, ts)
	compromised, count, err := s.Check(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if compromised {
		t.Fatal("expected compromised=false for empty password")
	}
	if count != 0 {
		t.Fatalf("expected count=0, got %d", count)
	}
	if calls.Load() != 0 {
		t.Fatalf("expected 0 HTTP calls for empty password, got %d", calls.Load())
	}
}

// TestHIBPScreener_RateLimiter — rapid-fire 3 Checks under a tight rate
// limit and assert wall-clock spacing is observed.
func TestHIBPScreener_RateLimiter(t *testing.T) {
	if testing.Short() {
		t.Skip("rate-limiter wall-clock test")
	}
	_, suffix := hibpHashSuffix(t, "password")
	body := fmt.Sprintf("%s:9545824\r\n", suffix)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, body)
	}))
	defer ts.Close()

	// Rate = 5 RPS; 3 calls should take >= ~0.4s (the first is free; subsequent two wait ~0.2s each).
	s := NewHIBPScreener(
		WithEndpoint(ts.URL+"/range/"),
		WithRateLimit(5),
		WithMaxRetries(0),
		WithRetryBackoff(5*time.Millisecond),
	)
	ctx := context.Background()
	start := time.Now()
	for i := 0; i < 3; i++ {
		if _, _, err := s.Check(ctx, "password"); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	elapsed := time.Since(start)
	if elapsed < 350*time.Millisecond {
		t.Fatalf("3 calls at 5 RPS should take >= 350ms (got %v) — rate limiter not engaged", elapsed)
	}
}

// (sanity) ensure stub server compiles + drains correctly.
func TestHIBPScreener_DrainsResponseBody(t *testing.T) {
	body := strings.Repeat("DEADBEEFCAFEBABEDEADBEEFCAFEBABEDEAD:1\r\n", 100)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		io.WriteString(w, body)
	}))
	defer ts.Close()

	s := fastClient(t, ts)
	if _, _, err := s.Check(context.Background(), "password"); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
}
