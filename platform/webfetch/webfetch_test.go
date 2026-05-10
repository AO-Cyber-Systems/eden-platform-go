package webfetch

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBlocksDisallowedScheme(t *testing.T) {
	c, err := NewClient(SafeDefault())
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Fetch(context.Background(), "file:///etc/passwd")
	if !errors.Is(err, ErrPolicyViolation) {
		t.Errorf("expected ErrPolicyViolation, got %v", err)
	}
}

func TestBlocksLoopback(t *testing.T) {
	c, _ := NewClient(SafeDefault())
	_, err := c.Fetch(context.Background(), "http://127.0.0.1:1/")
	if !errors.Is(err, ErrPolicyViolation) {
		t.Errorf("expected ErrPolicyViolation for 127.0.0.1, got %v", err)
	}
	_, err = c.Fetch(context.Background(), "http://localhost:1/")
	if !errors.Is(err, ErrPolicyViolation) {
		t.Errorf("expected ErrPolicyViolation for localhost, got %v", err)
	}
}

func TestBlocksPrivateIPs(t *testing.T) {
	c, _ := NewClient(SafeDefault())
	for _, target := range []string{
		"http://10.0.0.1/",
		"http://192.168.1.1/",
		"http://172.16.0.1/",
		"http://169.254.169.254/",  // cloud metadata
	} {
		_, err := c.Fetch(context.Background(), target)
		if !errors.Is(err, ErrPolicyViolation) {
			t.Errorf("expected ErrPolicyViolation for %s, got %v", target, err)
		}
	}
}

func TestAllowOverridesDeny(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	p := SafeDefault()
	p.AllowHostsRegexp = []string{`^127\.0\.0\.1$`}
	c, err := NewClient(p)
	if err != nil {
		t.Fatal(err)
	}

	res, err := c.Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("expected allow override to permit fetch, got %v", err)
	}
	if string(res.Body) != "ok" {
		t.Errorf("unexpected body: %s", res.Body)
	}
}

func TestSizeCapTruncates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("a", 4096)))
	}))
	defer srv.Close()

	p := SafeDefault()
	p.AllowHostsRegexp = []string{`^127\.0\.0\.1$`}
	p.MaxResponseBytes = 100
	c, _ := NewClient(p)

	res, err := c.Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Truncated {
		t.Errorf("expected truncated response")
	}
	if res.BytesRead != 100 {
		t.Errorf("expected 100 bytes, got %d", res.BytesRead)
	}
}

func TestRedirectCap(t *testing.T) {
	var hits int
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/redir/", func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.Redirect(w, r, fmt.Sprintf("%s/redir/%d", srv.URL, hits), http.StatusFound)
	})

	p := SafeDefault()
	p.AllowHostsRegexp = []string{`^127\.0\.0\.1$`}
	p.MaxRedirects = 2
	c, _ := NewClient(p)

	_, err := c.Fetch(context.Background(), srv.URL+"/redir/0")
	if !errors.Is(err, ErrTooManyRedirects) {
		t.Errorf("expected ErrTooManyRedirects, got %v", err)
	}
}

func TestRedirectSchemeRestriction(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/r", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "file:///etc/passwd", http.StatusFound)
	})

	p := SafeDefault()
	p.AllowHostsRegexp = []string{`^127\.0\.0\.1$`}
	c, _ := NewClient(p)

	_, err := c.Fetch(context.Background(), srv.URL+"/r")
	if !errors.Is(err, ErrPolicyViolation) {
		t.Errorf("expected ErrPolicyViolation for redirect scheme, got %v", err)
	}
}

func TestUserAgentPropagated(t *testing.T) {
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	p := SafeDefault()
	p.AllowHostsRegexp = []string{`^127\.0\.0\.1$`}
	p.UserAgent = "test-agent/2.0"
	p.AdditionalHeaders = map[string]string{"X-Trace": "abc"}
	c, _ := NewClient(p)

	if _, err := c.Fetch(context.Background(), srv.URL); err != nil {
		t.Fatal(err)
	}
	if seen != "test-agent/2.0" {
		t.Errorf("expected user-agent propagated, got %q", seen)
	}
}

func TestHappyPathFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("hello"))
	}))
	defer srv.Close()

	p := SafeDefault()
	p.AllowHostsRegexp = []string{`^127\.0\.0\.1$`}
	c, _ := NewClient(p)

	res, err := c.Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 || string(res.Body) != "hello" || res.ContentType != "text/plain" {
		t.Errorf("unexpected: %+v", res)
	}
}

func TestConnectTimeoutTooSmall(t *testing.T) {
	// 1ms timeout — should fail immediately even on a working host.
	p := SafeDefault()
	p.AllowHostsRegexp = []string{`^127\.0\.0\.1$`}
	p.ConnectTimeout = time.Millisecond
	c, _ := NewClient(p)

	// Use an unroutable IP to force a timeout.
	_, err := c.Fetch(context.Background(), "http://198.51.100.1/")
	if err == nil {
		t.Errorf("expected timeout error against unroutable IP")
	}
}

func TestDenyHostsRegexp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	p := SafeDefault()
	p.AllowHostsRegexp = []string{`^127\.0\.0\.1$`}
	p.DenyHostsRegexp = []string{`^127\..*`} // overlap with allow — allow wins (allow checked first)
	c, _ := NewClient(p)

	if _, err := c.Fetch(context.Background(), srv.URL); err != nil {
		t.Errorf("allow should override deny, got %v", err)
	}
}
