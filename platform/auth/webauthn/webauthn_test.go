package webauthn

import (
	"bytes"
	"encoding/base64"
	"testing"

	gowebauthn "github.com/go-webauthn/webauthn/webauthn"
)

func TestNew(t *testing.T) {
	w, err := New(Config{
		RPDisplayName: "Test Platform",
		RPID:          "localhost",
		RPOrigins:     []string{"http://localhost:3000"},
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if w == nil {
		t.Fatal("nil webauthn")
	}
	if w.Config.RPID != "localhost" {
		t.Errorf("RPID: %q", w.Config.RPID)
	}
	if w.Config.RPDisplayName != "Test Platform" {
		t.Errorf("RPDisplayName: %q", w.Config.RPDisplayName)
	}
}

func TestNew_DefaultDisplayName(t *testing.T) {
	w, err := New(Config{
		RPID:      "localhost",
		RPOrigins: []string{"http://localhost:3000"},
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if w.Config.RPDisplayName != "AOC Platform" {
		t.Errorf("expected default display name, got %q", w.Config.RPDisplayName)
	}
}

func TestNew_InvalidConfig(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Error("expected error for empty RPID")
	}
}

func TestUser_Interface(t *testing.T) {
	id := base64.RawURLEncoding.EncodeToString([]byte("test-id"))
	creds := []gowebauthn.Credential{{ID: []byte("c1"), PublicKey: []byte("p1")}}
	u := NewUser(id, "u@example.com", "User", creds)

	var _ gowebauthn.User = u

	if !bytes.Equal(u.WebAuthnID(), []byte("test-id")) {
		t.Errorf("ID: %v", u.WebAuthnID())
	}
	if u.WebAuthnName() != "u@example.com" {
		t.Errorf("name: %q", u.WebAuthnName())
	}
	if u.WebAuthnDisplayName() != "User" {
		t.Errorf("display: %q", u.WebAuthnDisplayName())
	}
	if len(u.WebAuthnCredentials()) != 1 {
		t.Errorf("creds: %d", len(u.WebAuthnCredentials()))
	}
}

func TestUser_NilCredentials(t *testing.T) {
	u := NewUser(base64.RawURLEncoding.EncodeToString([]byte("id")), "u@example.com", "U", nil)
	creds := u.WebAuthnCredentials()
	if creds == nil {
		t.Fatal("expected empty slice, got nil")
	}
	if len(creds) != 0 {
		t.Errorf("len: %d", len(creds))
	}
}

func TestUser_InvalidBase64Fallback(t *testing.T) {
	u := NewUser("not-base64!!!", "u@example.com", "U", nil)
	if len(u.WebAuthnID()) == 0 {
		t.Fatal("expected non-empty ID even with invalid base64")
	}
}

func TestGenerateUserID(t *testing.T) {
	id1 := GenerateUserID()
	id2 := GenerateUserID()
	if id1 == "" {
		t.Fatal("empty ID")
	}
	if id1 == id2 {
		t.Error("expected unique IDs")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(id1)
	if err != nil {
		t.Errorf("not base64url: %v", err)
	}
	if len(decoded) != 64 {
		t.Errorf("expected 64 bytes, got %d", len(decoded))
	}
}

func TestBeginRegistration(t *testing.T) {
	w, err := New(Config{
		RPDisplayName: "TP",
		RPID:          "localhost",
		RPOrigins:     []string{"http://localhost:3000"},
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	u := NewUser(GenerateUserID(), "u@example.com", "U", nil)
	creation, session, err := w.BeginRegistration(u)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if creation == nil || session == nil {
		t.Fatal("nil result")
	}
	if session.Challenge == "" {
		t.Error("empty challenge")
	}
	if creation.Response.RelyingParty.Name != "TP" {
		t.Errorf("rp name: %q", creation.Response.RelyingParty.Name)
	}
}

func TestBeginLogin(t *testing.T) {
	w, _ := New(Config{
		RPID:      "localhost",
		RPOrigins: []string{"http://localhost:3000"},
	})
	u := NewUser(GenerateUserID(), "u@example.com", "U", []gowebauthn.Credential{
		{ID: []byte("c1"), PublicKey: []byte("p1")},
	})
	assertion, session, err := w.BeginLogin(u)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if assertion == nil || session == nil {
		t.Fatal("nil result")
	}
	if session.Challenge == "" {
		t.Error("empty challenge")
	}
}
