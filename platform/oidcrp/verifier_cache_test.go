package oidcrp

import (
	"context"
	"sync/atomic"
	"testing"

	oidc "github.com/coreos/go-oidc/v3/oidc"
)

func newTestProvider(t *testing.T) *oidc.Provider {
	t.Helper()
	var counter int64
	srv := newFakeProvider(t, &counter)
	ctx := context.Background()
	p, err := oidc.NewProvider(ctx, srv.URL)
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	// silence unused atomic load
	_ = atomic.LoadInt64(&counter)
	return p
}

func TestVerifierCache_SameKeyReturnsSame(t *testing.T) {
	t.Parallel()
	p := newTestProvider(t)
	cache := NewVerifierCache()

	v1 := cache.Get("tenant-1/idp-1", p, "client-A", nil)
	v2 := cache.Get("tenant-1/idp-1", p, "client-A", nil)
	if v1 != v2 {
		t.Fatalf("same key returned different verifier pointers: %p vs %p", v1, v2)
	}
}

func TestVerifierCache_DifferentKeysAreDistinct(t *testing.T) {
	t.Parallel()
	p := newTestProvider(t)
	cache := NewVerifierCache()

	v1 := cache.Get("tenant-1/idp-1", p, "client-A", nil)
	v2 := cache.Get("tenant-2/idp-1", p, "client-A", nil)
	if v1 == v2 {
		t.Fatalf("different keys returned same verifier pointer")
	}
}

func TestVerifierCache_DefaultAlgsWhenNil(t *testing.T) {
	t.Parallel()
	p := newTestProvider(t)
	cache := NewVerifierCache()
	// Just confirming the call doesn't panic with nil algs (the default
	// path is exercised). The actual alg-restriction behavior is exercised
	// in the flow_test.go end-to-end suite.
	v := cache.Get("k", p, "client-A", nil)
	if v == nil {
		t.Fatal("nil verifier returned")
	}
}

func TestVerifierCache_EmptyClientIDPanics(t *testing.T) {
	t.Parallel()
	p := newTestProvider(t)
	cache := NewVerifierCache()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on empty clientID, got none")
		}
	}()
	_ = cache.Get("k", p, "", nil)
}

func TestVerifierCache_Invalidate(t *testing.T) {
	t.Parallel()
	p := newTestProvider(t)
	cache := NewVerifierCache()

	v1 := cache.Get("k", p, "client-A", nil)
	cache.Invalidate("k")
	v2 := cache.Get("k", p, "client-A", nil)
	if v1 == v2 {
		t.Fatal("Invalidate did not force re-build")
	}
}
