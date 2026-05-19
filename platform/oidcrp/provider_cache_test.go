package oidcrp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeProvider stands up the minimum subset of an OIDC provider needed for
// oidc.NewProvider(ctx, issuer) to succeed: a well-known discovery doc.
func newFakeProvider(t *testing.T, counter *int64) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	var srv *httptest.Server

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(counter, 1)
		resp := map[string]any{
			"issuer":                                srv.URL,
			"authorization_endpoint":                srv.URL + "/auth",
			"token_endpoint":                        srv.URL + "/token",
			"jwks_uri":                              srv.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestProviderCache_Singleflight(t *testing.T) {
	t.Parallel()
	var counter int64
	srv := newFakeProvider(t, &counter)

	cache := NewProviderCache()
	const N = 100
	var wg sync.WaitGroup
	wg.Add(N)
	errs := make([]error, N)
	for i := 0; i < N; i++ {
		i := i
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, err := cache.Get(ctx, "tenant-1/idp-1", srv.URL)
			errs[i] = err
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
	}
	got := atomic.LoadInt64(&counter)
	if got != 1 {
		t.Fatalf("expected exactly 1 discovery call, got %d", got)
	}
}

func TestProviderCache_DifferentKeys(t *testing.T) {
	t.Parallel()
	var counter int64
	srv := newFakeProvider(t, &counter)

	cache := NewProviderCache()
	ctx := context.Background()
	p1, err := cache.Get(ctx, "tenant-1/idp-1", srv.URL)
	if err != nil {
		t.Fatalf("first Get: %v", err)
	}
	p2, err := cache.Get(ctx, "tenant-2/idp-1", srv.URL)
	if err != nil {
		t.Fatalf("second Get: %v", err)
	}
	if p1 == p2 {
		// Different cache keys should populate independently; each key gets its
		// own provider instance via separate NewProvider calls.
		t.Errorf("different cache keys returned same provider pointer")
	}
	if got := atomic.LoadInt64(&counter); got != 2 {
		t.Fatalf("expected 2 discovery calls (one per key), got %d", got)
	}
}

func TestProviderCache_Invalidate(t *testing.T) {
	t.Parallel()
	var counter int64
	srv := newFakeProvider(t, &counter)

	cache := NewProviderCache()
	ctx := context.Background()

	if _, err := cache.Get(ctx, "k", srv.URL); err != nil {
		t.Fatalf("Get 1: %v", err)
	}
	if _, err := cache.Get(ctx, "k", srv.URL); err != nil {
		t.Fatalf("Get 2: %v", err)
	}
	if got := atomic.LoadInt64(&counter); got != 1 {
		t.Fatalf("expected 1 hit before invalidate, got %d", got)
	}
	cache.Invalidate("k")
	if _, err := cache.Get(ctx, "k", srv.URL); err != nil {
		t.Fatalf("Get post-invalidate: %v", err)
	}
	if got := atomic.LoadInt64(&counter); got != 2 {
		t.Fatalf("expected 2 hits after invalidate, got %d", got)
	}
}

func TestProviderCache_DiscoveryError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	cache := NewProviderCache()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := cache.Get(ctx, "k", srv.URL); err == nil {
		t.Fatal("expected error on bad discovery, got nil")
	}
}
