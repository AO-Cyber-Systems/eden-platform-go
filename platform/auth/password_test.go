// Test list
//
// Existing (Argon2id baseline — MUST stay green):
//   - TestPasswordHasher_HashAndVerify         — round-trip + wrong-password
//   - TestPasswordHasher_DifferentSalts        — independent salts per Hash call
//   - TestPasswordHasher_InvalidHash           — table of malformed inputs (now via ErrMalformedHash)
//
// New (TRD 03-01 Task 1):
//   - TestPasswordHasher_VerifyUnknownAlgorithm   — $scrypt$... → ErrUnsupportedAlgorithm
//   - TestPasswordHasher_VerifyMalformedNoLeadingDollar — "argon2id$v=19$..." → ErrMalformedHash
//   - TestPasswordHasher_VerifyEmptyString        — "" → ErrMalformedHash
//   - TestSentinelErrors_Hasher                   — errors.Is identity check for both sentinels
//   - TestPasswordHasher_HashHasArgon2idAlgo      — default constructor uses AlgoArgon2id

package auth

import (
	"errors"
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

func TestPasswordHasher_VerifyUnknownAlgorithm(t *testing.T) {
	hasher := NewPasswordHasher()

	// Synthetic scrypt-flavored string with a plausible 4-part-after-leading-empty shape.
	encoded := "$scrypt$N=16384,r=8,p=1$" +
		"YWJjZGVmZ2hpamtsbW5vcA$" + // 16 bytes base64 RawStd
		"MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY" // 32 bytes
	ok, err := hasher.Verify("password", encoded)
	if ok {
		t.Errorf("Verify() with $scrypt$ prefix returned true, expected false")
	}
	if !errors.Is(err, ErrUnsupportedAlgorithm) {
		t.Errorf("Verify() error = %v; want errors.Is(err, ErrUnsupportedAlgorithm)", err)
	}
}

func TestPasswordHasher_VerifyMalformedNoLeadingDollar(t *testing.T) {
	hasher := NewPasswordHasher()
	encoded := "argon2id$v=19$m=47104,t=1,p=1$YWJjZGVmZ2hpamtsbW5vcA$aGFzaA"
	ok, err := hasher.Verify("password", encoded)
	if ok {
		t.Errorf("Verify() with no leading $ returned true, expected false")
	}
	if !errors.Is(err, ErrMalformedHash) {
		t.Errorf("Verify() error = %v; want errors.Is(err, ErrMalformedHash)", err)
	}
}

func TestPasswordHasher_VerifyEmptyString(t *testing.T) {
	hasher := NewPasswordHasher()
	ok, err := hasher.Verify("password", "")
	if ok {
		t.Errorf("Verify() of empty string returned true, expected false")
	}
	if !errors.Is(err, ErrMalformedHash) {
		t.Errorf("Verify() error = %v; want errors.Is(err, ErrMalformedHash)", err)
	}
}

func TestSentinelErrors_Hasher(t *testing.T) {
	if !errors.Is(ErrMalformedHash, ErrMalformedHash) {
		t.Errorf("errors.Is(ErrMalformedHash, ErrMalformedHash) = false; expected true")
	}
	if !errors.Is(ErrUnsupportedAlgorithm, ErrUnsupportedAlgorithm) {
		t.Errorf("errors.Is(ErrUnsupportedAlgorithm, ErrUnsupportedAlgorithm) = false; expected true")
	}
	// They must NOT collide.
	if errors.Is(ErrMalformedHash, ErrUnsupportedAlgorithm) {
		t.Errorf("ErrMalformedHash should not satisfy ErrUnsupportedAlgorithm")
	}
}

func TestPasswordHasher_HashHasArgon2idAlgo(t *testing.T) {
	h := NewPasswordHasher()
	if h.algo != AlgoArgon2id {
		t.Errorf("NewPasswordHasher().algo = %v; want AlgoArgon2id", h.algo)
	}
}
