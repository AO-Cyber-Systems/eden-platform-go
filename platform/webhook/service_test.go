package webhook

import (
	"testing"
)

func TestVerifySignature_Valid(t *testing.T) {
	secret := "test-secret"
	timestamp := "1234567890"
	payload := `{"event":"user.created"}`

	sig := sign(secret, timestamp, payload)
	header := "t=" + timestamp + ",v1=" + sig

	if !VerifySignature(secret, header, payload) {
		t.Errorf("VerifySignature() = false, want true for valid signature")
	}
}

func TestVerifySignature_Invalid(t *testing.T) {
	secret := "test-secret"
	header := "t=1234567890,v1=tampered"
	payload := `{"event":"user.created"}`

	if VerifySignature(secret, header, payload) {
		t.Errorf("VerifySignature() = true, want false for tampered signature")
	}
}

func TestVerifySignature_MalformedHeader(t *testing.T) {
	tests := []struct {
		name   string
		header string
	}{
		{"empty", ""},
		{"no parts", "invalid"},
		{"too many parts", "t=1,v1=abc,extra=bad"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if VerifySignature("secret", tt.header, "payload") {
				t.Errorf("VerifySignature(%q) = true, want false", tt.header)
			}
		})
	}
}

func TestMatchesEvent_Exact(t *testing.T) {
	if !matchesEvent([]string{"user.created"}, "user.created") {
		t.Errorf("matchesEvent(exact) = false, want true")
	}
}

func TestMatchesEvent_Wildcard(t *testing.T) {
	if !matchesEvent([]string{"*"}, "anything.here") {
		t.Errorf("matchesEvent(*) = false, want true")
	}
}

func TestMatchesEvent_PrefixWildcard(t *testing.T) {
	if !matchesEvent([]string{"user.*"}, "user.created") {
		t.Errorf("matchesEvent('user.*', 'user.created') = false, want true")
	}
	if !matchesEvent([]string{"user.*"}, "user.deleted") {
		t.Errorf("matchesEvent('user.*', 'user.deleted') = false, want true")
	}
}

func TestMatchesEvent_NoMatch(t *testing.T) {
	if matchesEvent([]string{"user.created"}, "order.created") {
		t.Errorf("matchesEvent('user.created', 'order.created') = true, want false")
	}
}
