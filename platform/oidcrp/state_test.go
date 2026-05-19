package oidcrp

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var stateKey = []byte("test-key-32-bytes-long-for-hmac---")

func TestSignedState_RoundTrip(t *testing.T) {
	t.Parallel()
	in := State{
		Tenant:    "tenant-1",
		Idp:       "idp-okta",
		ReturnURL: "https://app.example.com/welcome",
		Nonce:     "abc123",
		CreatedAt: time.Now().Unix(),
	}
	raw := SignState(stateKey, in)
	if !strings.Contains(raw, ".") {
		t.Fatalf("signed state must be payload.mac, got %q", raw)
	}
	out, err := VerifyState(stateKey, raw, time.Hour)
	if err != nil {
		t.Fatalf("VerifyState: %v", err)
	}
	if out.Tenant != in.Tenant || out.Idp != in.Idp || out.Nonce != in.Nonce || out.ReturnURL != in.ReturnURL {
		t.Errorf("round-trip mismatch: in=%+v out=%+v", in, out)
	}
}

func TestSignedState_TamperedMACFails(t *testing.T) {
	t.Parallel()
	in := State{Tenant: "t", Idp: "i", Nonce: "n", CreatedAt: time.Now().Unix()}
	raw := SignState(stateKey, in)

	// Flip last char of mac.
	last := raw[len(raw)-1]
	var swap byte
	if last == 'A' {
		swap = 'B'
	} else {
		swap = 'A'
	}
	tampered := raw[:len(raw)-1] + string(swap)

	_, err := VerifyState(stateKey, tampered, time.Hour)
	if !errors.Is(err, ErrStateInvalid) {
		t.Fatalf("expected ErrStateInvalid, got: %v", err)
	}
}

func TestSignedState_WrongKeyFails(t *testing.T) {
	t.Parallel()
	in := State{Tenant: "t", Idp: "i", Nonce: "n", CreatedAt: time.Now().Unix()}
	raw := SignState(stateKey, in)
	_, err := VerifyState([]byte("different-key-also-32-byte-aligned"), raw, time.Hour)
	if !errors.Is(err, ErrStateInvalid) {
		t.Fatalf("expected ErrStateInvalid, got: %v", err)
	}
}

func TestSignedState_Expired(t *testing.T) {
	t.Parallel()
	in := State{
		Tenant:    "t",
		Idp:       "i",
		Nonce:     "n",
		CreatedAt: time.Now().Add(-2 * time.Hour).Unix(),
	}
	raw := SignState(stateKey, in)
	_, err := VerifyState(stateKey, raw, 1*time.Hour)
	if !errors.Is(err, ErrStateExpired) {
		t.Fatalf("expected ErrStateExpired, got: %v", err)
	}
}

func TestSignedState_MissingTenantRejected(t *testing.T) {
	t.Parallel()
	in := State{Tenant: "", Idp: "i", Nonce: "n", CreatedAt: time.Now().Unix()}
	raw := SignState(stateKey, in)
	_, err := VerifyState(stateKey, raw, time.Hour)
	if !errors.Is(err, ErrStateInvalid) {
		t.Fatalf("expected ErrStateInvalid, got: %v", err)
	}
}

func TestSignedState_MissingIdpRejected(t *testing.T) {
	t.Parallel()
	in := State{Tenant: "t", Idp: "", Nonce: "n", CreatedAt: time.Now().Unix()}
	raw := SignState(stateKey, in)
	_, err := VerifyState(stateKey, raw, time.Hour)
	if !errors.Is(err, ErrStateInvalid) {
		t.Fatalf("expected ErrStateInvalid, got: %v", err)
	}
}

func TestSignedState_MissingNonceRejected(t *testing.T) {
	t.Parallel()
	in := State{Tenant: "t", Idp: "i", Nonce: "", CreatedAt: time.Now().Unix()}
	raw := SignState(stateKey, in)
	_, err := VerifyState(stateKey, raw, time.Hour)
	if !errors.Is(err, ErrStateInvalid) {
		t.Fatalf("expected ErrStateInvalid, got: %v", err)
	}
}

func TestSignedState_MalformedRejected(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		"",
		"no-dot-here",
		".justmac",
		"justpayload.",
		"!!!.!!!",
	} {
		_, err := VerifyState(stateKey, raw, time.Hour)
		if !errors.Is(err, ErrStateInvalid) {
			t.Errorf("input %q: expected ErrStateInvalid, got: %v", raw, err)
		}
	}
}
