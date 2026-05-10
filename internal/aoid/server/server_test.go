package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/aocybersystems/eden-platform-go/internal/aoid/config"
)

// runForTest spins up a Server on a free port and returns its base URL.
// The cleanup hook cancels the context and verifies the server tears
// down cleanly.
func runForTest(t *testing.T, cfg *config.Config, opts ...func(*Server)) string {
	t.Helper()
	if cfg.ShutdownTimeout == 0 {
		cfg.ShutdownTimeout = time.Second
	}
	if cfg.Issuer == "" {
		cfg.Issuer = "http://127.0.0.1:0"
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	s := New(cfg)
	for _, o := range opts {
		o(s)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Start(ctx, ln) }()

	base := "http://" + ln.Addr().String()
	deadline := time.Now().Add(2 * time.Second)
	for {
		resp, err := http.Get(base + "/readyz")
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			break
		}
		if resp != nil {
			resp.Body.Close()
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("server not ready: last err=%v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("server returned error: %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Errorf("server did not shut down within deadline")
		}
	})
	return base
}

func TestServer_Healthz(t *testing.T) {
	cfg := &config.Config{}
	base := runForTest(t, cfg)

	resp, err := http.Get(base + "/healthz")
	if err != nil {
		t.Fatalf("get /healthz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/healthz status = %d", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Errorf("decode health: %v", err)
	}
	if body["status"] == nil {
		t.Errorf("/healthz missing status field; body=%v", body)
	}
}

func TestServer_Readyz(t *testing.T) {
	cfg := &config.Config{}
	base := runForTest(t, cfg)

	resp, err := http.Get(base + "/readyz")
	if err != nil {
		t.Fatalf("get /readyz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/readyz status = %d", resp.StatusCode)
	}
}

func TestServer_PreReadyReadyzReturns503(t *testing.T) {
	s := New(&config.Config{Release: "test"})

	r, _ := http.NewRequest(http.MethodGet, "/readyz", nil)
	rw := newCaptureWriter()
	s.readyHandler(rw, r)

	if rw.status != http.StatusServiceUnavailable {
		t.Errorf("status = %d want 503", rw.status)
	}
}

func TestServer_ContextCancellationShutsDown(t *testing.T) {
	cfg := &config.Config{ShutdownTimeout: time.Second}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := New(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Start(ctx, ln) }()

	url := fmt.Sprintf("http://%s/readyz", ln.Addr().String())
	for i := 0; i < 50; i++ {
		resp, err := http.Get(url)
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			break
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(20 * time.Millisecond)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return within deadline after cancel")
	}
}

// captureWriter is a minimal http.ResponseWriter for unit tests.
type captureWriter struct {
	header http.Header
	body   []byte
	status int
}

func newCaptureWriter() *captureWriter { return &captureWriter{header: http.Header{}} }

func (c *captureWriter) Header() http.Header        { return c.header }
func (c *captureWriter) Write(b []byte) (int, error) { c.body = append(c.body, b...); return len(b), nil }
func (c *captureWriter) WriteHeader(s int)          { c.status = s }
