package clients

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestMemoryRegistry_LookupRegister(t *testing.T) {
	r := NewMemoryRegistry()
	ctx := context.Background()

	if _, err := r.Lookup(ctx, "missing"); !errors.Is(err, ErrClientNotFound) {
		t.Fatalf("Lookup unknown: want ErrClientNotFound, got %v", err)
	}

	c := Client{
		ID:            "test-client",
		SecretHash:    HashSecret("topsecret"),
		Name:          "Test",
		Type:          ClientTypeConfidential,
		RedirectURIs:  []string{"http://localhost/cb"},
		AllowedScopes: []string{"openid"},
		AllowedGrants: []string{"authorization_code"},
		AuthMethod:    "client_secret_basic",
	}
	if err := r.Register(ctx, c); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := r.Register(ctx, c); !errors.Is(err, ErrDuplicateClient) {
		t.Fatalf("Register dup: want ErrDuplicateClient, got %v", err)
	}

	got, err := r.Lookup(ctx, "test-client")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got.Name != "Test" {
		t.Errorf("Name=%q want Test", got.Name)
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt zero — Register should stamp it")
	}

	// Mutating the returned copy must not affect the stored entry.
	got.RedirectURIs[0] = "http://evil/cb"
	again, _ := r.Lookup(ctx, "test-client")
	if again.RedirectURIs[0] == "http://evil/cb" {
		t.Error("registry leaked internal slice — mutating copy poisoned store")
	}
}

func TestMemoryRegistry_RegisterEmptyID(t *testing.T) {
	r := NewMemoryRegistry()
	if err := r.Register(context.Background(), Client{}); err == nil {
		t.Error("Register with empty ID: want error")
	}
}

func TestMemoryRegistry_Authenticate(t *testing.T) {
	r := NewMemoryRegistry()
	ctx := context.Background()
	const secret = "shared-secret-1234"
	if err := r.Register(ctx, Client{
		ID:           "c",
		SecretHash:   HashSecret(secret),
		Type:         ClientTypeConfidential,
		AuthMethod:   "client_secret_basic",
		RedirectURIs: []string{"http://localhost/cb"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// happy path
	got, err := r.Authenticate(ctx, "c", secret)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if got.ID != "c" {
		t.Errorf("got %q want c", got.ID)
	}

	// wrong secret
	if _, err := r.Authenticate(ctx, "c", "wrong"); !errors.Is(err, ErrInvalidSecret) {
		t.Errorf("wrong secret: want ErrInvalidSecret, got %v", err)
	}

	// unknown client
	if _, err := r.Authenticate(ctx, "missing", secret); !errors.Is(err, ErrClientNotFound) {
		t.Errorf("unknown client: want ErrClientNotFound, got %v", err)
	}

	// public client: no secret hash → reject
	_ = r.Register(ctx, Client{
		ID:           "public",
		Type:         ClientTypePublic,
		AuthMethod:   "none",
		RedirectURIs: []string{"http://localhost/cb"},
	})
	if _, err := r.Authenticate(ctx, "public", "anything"); !errors.Is(err, ErrInvalidSecret) {
		t.Errorf("public client via Authenticate: want ErrInvalidSecret, got %v", err)
	}
}

func TestMemoryRegistry_ValidateRedirect(t *testing.T) {
	r := NewMemoryRegistry()
	ctx := context.Background()
	_ = r.Register(ctx, Client{
		ID:           "c",
		SecretHash:   HashSecret("s"),
		RedirectURIs: []string{"http://a/cb", "http://b/cb"},
	})
	c, _ := r.Lookup(ctx, "c")

	if err := r.ValidateRedirect(c, "http://a/cb"); err != nil {
		t.Errorf("exact match: %v", err)
	}
	if err := r.ValidateRedirect(c, "http://A/cb"); err == nil {
		t.Error("case-different match should fail (exact match required)")
	}
	if err := r.ValidateRedirect(c, "http://a/cb?extra=1"); err == nil {
		t.Error("query-param-different match should fail")
	}
	if err := r.ValidateRedirect(nil, "x"); !errors.Is(err, ErrClientNotFound) {
		t.Errorf("nil client: want ErrClientNotFound, got %v", err)
	}
}

func TestSeedAODex(t *testing.T) {
	r := NewMemoryRegistry()
	ctx := context.Background()
	if err := SeedAODex(ctx, r, "dev-secret", []string{"http://localhost:8080/auth/aoid/callback"}); err != nil {
		t.Fatalf("SeedAODex: %v", err)
	}
	c, err := r.Lookup(ctx, AODexClientID)
	if err != nil {
		t.Fatalf("Lookup AODex: %v", err)
	}
	if c.Name != "AODex (pilot)" {
		t.Errorf("Name=%q", c.Name)
	}
	if !c.HasScope("openid") || !c.HasScope("offline_access") {
		t.Error("AODex must have openid + offline_access scopes")
	}
	if !c.HasGrant("authorization_code") || !c.HasGrant("refresh_token") {
		t.Error("AODex must have auth_code + refresh grants")
	}

	// idempotent
	if err := SeedAODex(ctx, r, "dev-secret", []string{"http://localhost:8080/auth/aoid/callback"}); err != nil {
		t.Errorf("SeedAODex re-run: want nil (idempotent), got %v", err)
	}

	// validates inputs
	if err := SeedAODex(ctx, NewMemoryRegistry(), "", []string{"http://x"}); err == nil {
		t.Error("SeedAODex with empty secret: want error")
	}
	if err := SeedAODex(ctx, NewMemoryRegistry(), "x", nil); err == nil {
		t.Error("SeedAODex with no redirects: want error")
	}
	if err := SeedAODex(ctx, nil, "x", []string{"y"}); err == nil {
		t.Error("SeedAODex with nil registry: want error")
	}
}

func TestHasScopeAndHasGrant(t *testing.T) {
	c := &Client{
		AllowedScopes: []string{"openid", "profile"},
		AllowedGrants: []string{"authorization_code"},
	}
	if !c.HasScope("openid") || c.HasScope("admin") {
		t.Error("HasScope")
	}
	if !c.HasGrant("authorization_code") || c.HasGrant("password") {
		t.Error("HasGrant")
	}
}

func TestHashSecret(t *testing.T) {
	a := HashSecret("hello")
	b := HashSecret("hello")
	if a != b {
		t.Error("HashSecret not deterministic")
	}
	if !strings.HasPrefix(a, "2cf24dba") {
		// known sha256("hello") prefix
		t.Errorf("HashSecret(hello)=%q, want sha256-hex starting 2cf24dba", a)
	}
}
