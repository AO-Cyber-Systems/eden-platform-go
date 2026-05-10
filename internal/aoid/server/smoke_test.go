package server_test

// End-to-end smoke test that boots the full aoid HTTP service, exercises
// every public endpoint, and tears it down cleanly. Provides the same
// coverage as the CI smoke step but inside `go test ./...` so local
// developers catch regressions without needing Docker.

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/aocybersystems/eden-platform-go/internal/aoid/composition"
	"github.com/aocybersystems/eden-platform-go/internal/aoid/config"
	"github.com/aocybersystems/eden-platform-go/internal/aoid/discovery"
	"github.com/aocybersystems/eden-platform-go/internal/aoid/fixtures"
	"github.com/aocybersystems/eden-platform-go/internal/aoid/jwks"
	"github.com/aocybersystems/eden-platform-go/internal/aoid/server"
)

func TestSmoke_FullService(t *testing.T) {
	cfg := &config.Config{
		Issuer:             "http://aoid-smoke.test",
		AccessTokenExpiry:  15 * time.Minute,
		RefreshTokenExpiry: 7 * 24 * time.Hour,
		ShutdownTimeout:    2 * time.Second,
		Release:            "smoke",
	}
	svcs, err := composition.BuildInMemory(cfg)
	if err != nil {
		t.Fatalf("BuildInMemory: %v", err)
	}
	t.Cleanup(func() { _ = svcs.Close() })

	if _, err := fixtures.Seed(context.Background(), svcs); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	srv := server.New(cfg)
	srv.AddRoutes(func(mux *http.ServeMux) {
		mux.HandleFunc("/.well-known/openid-configuration", discovery.Handler(cfg))
		mux.HandleFunc("/oauth2/token", discovery.IssuerNotActive)
		mux.HandleFunc("/oauth2/authorize", discovery.IssuerNotActive)
		mux.HandleFunc("/oauth2/userinfo", discovery.IssuerNotActive)
		mux.HandleFunc("/.well-known/jwks.json", jwks.Handler(svcs.JWTManager))
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Start(ctx, ln) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("server error: %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Error("shutdown timeout")
		}
	})

	base := "http://" + ln.Addr().String()
	client := &http.Client{Timeout: 2 * time.Second}

	deadline := time.Now().Add(2 * time.Second)
	for {
		r, err := client.Get(base + "/readyz")
		if err == nil {
			r.Body.Close()
			if r.StatusCode == http.StatusOK {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("readyz never returned 200; last err=%v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Run("healthz", func(t *testing.T) {
		r, err := client.Get(base + "/healthz")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		defer r.Body.Close()
		if r.StatusCode != http.StatusOK {
			t.Errorf("status = %d", r.StatusCode)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode: %v", err)
		}
		if body["status"] == nil {
			t.Errorf("missing status field")
		}
	})

	t.Run("discovery", func(t *testing.T) {
		r, err := client.Get(base + "/.well-known/openid-configuration")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		defer r.Body.Close()
		if r.StatusCode != http.StatusOK {
			t.Errorf("status = %d", r.StatusCode)
		}
		var doc map[string]any
		if err := json.NewDecoder(r.Body).Decode(&doc); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if doc["service_status"] != "scaffold" {
			t.Errorf("service_status = %v", doc["service_status"])
		}
		if doc["issuer"] != "http://aoid-smoke.test" {
			t.Errorf("issuer = %v", doc["issuer"])
		}
		if doc["jwks_uri"] == nil {
			t.Errorf("jwks_uri missing")
		}
	})

	t.Run("jwks_has_keys", func(t *testing.T) {
		r, err := client.Get(base + "/.well-known/jwks.json")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		defer r.Body.Close()
		if r.StatusCode != http.StatusOK {
			t.Errorf("status = %d", r.StatusCode)
		}
		var set struct {
			Keys []map[string]any `json:"keys"`
		}
		if err := json.NewDecoder(r.Body).Decode(&set); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(set.Keys) < 1 {
			t.Errorf("expected at least 1 key, got %d", len(set.Keys))
		}
		if set.Keys[0]["alg"] != "ML-DSA-65" {
			t.Errorf("alg = %v", set.Keys[0]["alg"])
		}
	})

	t.Run("issuer_endpoints_503", func(t *testing.T) {
		for _, path := range []string{"/oauth2/token", "/oauth2/authorize", "/oauth2/userinfo"} {
			r, err := client.Get(base + path)
			if err != nil {
				t.Fatalf("get %s: %v", path, err)
			}
			body, _ := io.ReadAll(r.Body)
			r.Body.Close()
			if r.StatusCode != http.StatusServiceUnavailable {
				t.Errorf("%s status = %d", path, r.StatusCode)
			}
			var b map[string]string
			if err := json.Unmarshal(body, &b); err != nil {
				t.Errorf("decode %s: %v", path, err)
			}
			if b["error"] != "issuer_not_active" {
				t.Errorf("%s error = %q", path, b["error"])
			}
		}
	})
}
