package auth

import (
	"strings"
	"testing"
)

func TestPasswordHasher_HashAndVerify(t *testing.T) {
	hasher := NewPasswordHasher()
	password := "secure-password-123"

	hash, err := hasher.Hash(password)
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}

	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Errorf("Hash() output does not have argon2id prefix: %s", hash)
	}

	match, err := hasher.Verify(password, hash)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if !match {
		t.Errorf("Verify() = false, expected true for correct password")
	}

	match, err = hasher.Verify("wrong-password", hash)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if match {
		t.Errorf("Verify() = true, expected false for wrong password")
	}
}

func TestPasswordHasher_DifferentSalts(t *testing.T) {
	hasher := NewPasswordHasher()
	password := "same-password"

	hash1, err := hasher.Hash(password)
	if err != nil {
		t.Fatalf("Hash() #1 error = %v", err)
	}
	hash2, err := hasher.Hash(password)
	if err != nil {
		t.Fatalf("Hash() #2 error = %v", err)
	}

	if hash1 == hash2 {
		t.Errorf("Hash() produced identical hashes for same password (different salts expected)")
	}
}

func TestPasswordHasher_InvalidHash(t *testing.T) {
	hasher := NewPasswordHasher()

	tests := []struct {
		name string
		hash string
	}{
		{"empty string", ""},
		{"random text", "not-a-hash"},
		{"wrong parts count", "$argon2id$v=19$m=47104"},
		{"malformed params", "$argon2id$v=19$bad-params$salt$hash"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := hasher.Verify("password", tt.hash)
			if err == nil {
				t.Errorf("Verify() with invalid hash %q expected error, got nil", tt.hash)
			}
		})
	}
}
