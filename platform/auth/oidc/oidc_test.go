package oidc

import (
	"net/url"
	"strings"
	"testing"
)

func TestBuildAuthURLStatic(t *testing.T) {
	cfg := Config{
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		RedirectURI:  "https://app.example.com/cb",
	}
	authURL := BuildAuthURLStatic("https://auth.example.com/authorize", cfg, "STATE", "NONCE")

	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !strings.HasPrefix(authURL, "https://auth.example.com/authorize?") {
		t.Errorf("missing auth endpoint prefix: %s", authURL)
	}
	q := parsed.Query()
	if q.Get("client_id") != cfg.ClientID {
		t.Errorf("client_id: %s", q.Get("client_id"))
	}
	if q.Get("redirect_uri") != cfg.RedirectURI {
		t.Errorf("redirect_uri: %s", q.Get("redirect_uri"))
	}
	if q.Get("state") != "STATE" {
		t.Errorf("state: %s", q.Get("state"))
	}
	if q.Get("nonce") != "NONCE" {
		t.Errorf("nonce: %s", q.Get("nonce"))
	}
	if q.Get("response_type") != "code" {
		t.Errorf("response_type: %s", q.Get("response_type"))
	}
	scope := q.Get("scope")
	for _, expected := range []string{"openid", "email", "profile"} {
		if !strings.Contains(scope, expected) {
			t.Errorf("scope missing %s: %s", expected, scope)
		}
	}
}

func TestBuildAuthURLStatic_CustomScopes(t *testing.T) {
	cfg := Config{
		ClientID:    "c",
		RedirectURI: "https://x/cb",
		Scopes:      []string{"openid", "email"},
	}
	authURL := BuildAuthURLStatic("https://auth/authorize", cfg, "s", "n")
	parsed, _ := url.Parse(authURL)
	scope := parsed.Query().Get("scope")
	if !strings.Contains(scope, "email") {
		t.Errorf("expected email scope, got %s", scope)
	}
	if strings.Contains(scope, "profile") {
		t.Errorf("did not expect profile scope, got %s", scope)
	}
}

func TestDefaultScopes(t *testing.T) {
	scopes := DefaultScopes()
	want := []string{"openid", "email", "profile"}
	if len(scopes) != len(want) {
		t.Fatalf("expected %d scopes, got %d", len(want), len(scopes))
	}
	for i, s := range want {
		if scopes[i] != s {
			t.Errorf("scope[%d]: want %s got %s", i, s, scopes[i])
		}
	}
}
