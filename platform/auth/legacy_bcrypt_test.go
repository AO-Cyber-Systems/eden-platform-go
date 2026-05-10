package auth

import (
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestHashLegacyPassword_RoundTrip(t *testing.T) {
	hash, err := HashLegacyPassword("hunter2")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if err := VerifyLegacyPassword(hash, "hunter2"); err != nil {
		t.Errorf("verify: %v", err)
	}
	if err := VerifyLegacyPassword(hash, "wrong"); err == nil {
		t.Error("expected verify to fail for wrong password")
	}
}

func TestVerifyLegacyPassword_DeviseCompatible(t *testing.T) {
	// Mimic a Devise-generated $2a$ hash by directly using bcrypt at cost 12
	// (Go's bcrypt produces $2a$ by default, same as Devise).
	hash, err := bcrypt.GenerateFromPassword([]byte("devise-secret"), 12)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := VerifyLegacyPassword(string(hash), "devise-secret"); err != nil {
		t.Errorf("expected Devise hash to verify: %v", err)
	}
}

func TestLegacyBcryptCost(t *testing.T) {
	if LegacyBcryptCost != 12 {
		t.Errorf("expected cost 12 (Devise default), got %d", LegacyBcryptCost)
	}
}
