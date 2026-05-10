package main

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

// TestBootService_FullStack boots the full aoid HTTP service via the
// production wiring (composition + fixtures + AddRoutes) and asserts
// every well-known + issuer-stub endpoint replies as documented. This
// is the integration check for 29-03 that the boot code threads
// composition.Services through the routes correctly.
func TestBootService_FullStack(t *testing.T) {
	cfg := &config.Config{
		ListenAddr:      ":0",
		Issuer:          "http://aoid-boot.test",
		ShutdownTimeout: time.Second,
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
				t.Errorf("server: %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Error("shutdown timeout")
		}
	})

	base := "http://" + ln.Addr().String()
	deadline := time.Now().Add(2 * time.Second)
	for {
		r, err := http.Get(base + "/readyz")
		if err == nil {
			r.Body.Close()
			if r.StatusCode == http.StatusOK {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("readyz never returned 200")
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Run("discovery", func(t *testing.T) {
		r, err := http.Get(base + "/.well-known/openid-configuration")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		defer r.Body.Close()
		var doc map[string]any
		if err := json.NewDecoder(r.Body).Decode(&doc); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if doc["service_status"] != "scaffold" {
			t.Errorf("service_status = %v", doc["service_status"])
		}
		if doc["issuer"] != "http://aoid-boot.test" {
			t.Errorf("issuer = %v", doc["issuer"])
		}
	})

	t.Run("jwks", func(t *testing.T) {
		r, err := http.Get(base + "/.well-known/jwks.json")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		defer r.Body.Close()
		var set struct {
			Keys []map[string]any `json:"keys"`
		}
		if err := json.NewDecoder(r.Body).Decode(&set); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(set.Keys) < 1 {
			t.Errorf("expected >=1 key, got %d", len(set.Keys))
		}
	})

	t.Run("issuer_endpoints_503", func(t *testing.T) {
		for _, p := range []string{"/oauth2/token", "/oauth2/authorize", "/oauth2/userinfo"} {
			r, err := http.Get(base + p)
			if err != nil {
				t.Fatalf("get %s: %v", p, err)
			}
			body, _ := io.ReadAll(r.Body)
			r.Body.Close()
			if r.StatusCode != http.StatusServiceUnavailable {
				t.Errorf("%s status=%d body=%s", p, r.StatusCode, body)
			}
		}
	})
}
