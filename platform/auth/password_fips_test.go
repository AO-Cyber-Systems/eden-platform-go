// Test list (TRD 03-01 Task 2)
//
//   - TestNewFIPSPasswordHasher_DefaultParams    — asserts iters=600000, saltLen=16, keyLen=32, algo=AlgoPBKDF2SHA256.
//   - TestFIPSHasher_HashRoundTrip               — Hash + Verify with OWASP example + empty + unicode + emoji.
//   - TestFIPSHasher_HashFormat                  — encoded string matches the pbkdf2-sha256 layout regex.
//   - TestFIPSHasher_HashDifferentSalts          — two Hash calls produce distinct outputs.
//   - TestFIPSHasher_VerifyWrongPassword         — (false, nil) — wrong password is a normal flow, NOT an error.
//   - TestFIPSHasher_VerifyMalformedSalt         — corrupted salt base64 → wraps ErrMalformedHash.
//   - TestFIPSHasher_VerifyMalformedHash         — corrupted hash base64 → wraps ErrMalformedHash.
//   - TestFIPSHasher_VerifyBadIterParam          — non-numeric "i=ABC" → wraps ErrMalformedHash.
//   - TestFIPSHasher_VerifyIterFloor             — iters<1000 → wraps ErrMalformedHash (sanity floor).
//   - TestFIPSHasher_VerifyWrongPartCount        — too-few/too-many $-separated parts → wraps ErrMalformedHash.
//   - TestCrossAlgo_Argon2idHasherVerifiesPBKDF2 — NewPasswordHasher() can verify a PBKDF2 hash.
//   - TestCrossAlgo_FIPSHasherVerifiesArgon2id   — NewFIPSPasswordHasher() can verify an Argon2id hash.
//   - TestCrossAlgo_WrongPasswordEitherDirection — cross-algo dispatch still returns false on wrong password.

package auth

import (
	"errors"
	"regexp"
	"strings"
	"testing"
)

// owaspExamples is the hand-curated input set per TRD context: OWASP example,
// empty string, an ASCII edge case, and a small Unicode set including emoji.
// No LLM-generated payloads.
var owaspExamples = []struct {
	name     string
	password string
}{
	{"owasp_correcthorse", "correct horse battery staple"},
	{"empty", ""},
	{"ascii_punct", "p@ssw0rd!#$%^&*()"},
	{"unicode_basic", "пароль123"},     // Cyrillic
	{"unicode_kanji", "パスワード"},        // Japanese
	{"emoji", "hunter2🔐🛡️"},
}

func TestNewFIPSPasswordHasher_DefaultParams(t *testing.T) {
	h := NewFIPSPasswordHasher()
	if h.algo != AlgoPBKDF2SHA256 {
		t.Errorf("algo = %v; want AlgoPBKDF2SHA256", h.algo)
	}
	if h.iters != 600_000 {
		t.Errorf("iters = %d; want 600000", h.iters)
	}
	if h.saltLen != 16 {
		t.Errorf("saltLen = %d; want 16", h.saltLen)
	}
	if h.keyLen != 32 {
		t.Errorf("keyLen = %d; want 32", h.keyLen)
	}
}

func TestFIPSHasher_HashRoundTrip(t *testing.T) {
	h := NewFIPSPasswordHasher()
	for _, tc := range owaspExamples {
		t.Run(tc.name, func(t *testing.T) {
			enc, err := h.Hash(tc.password)
			if err != nil {
				t.Fatalf("Hash(%q) err = %v", tc.password, err)
			}
			if !strings.HasPrefix(enc, "$pbkdf2-sha256$") {
				t.Errorf("encoded hash missing pbkdf2-sha256 prefix: %s", enc)
			}
			ok, err := h.Verify(tc.password, enc)
			if err != nil {
				t.Fatalf("Verify(%q) err = %v", tc.password, err)
			}
			if !ok {
				t.Errorf("Verify(%q) = false; want true", tc.password)
			}
		})
	}
}

func TestFIPSHasher_HashFormat(t *testing.T) {
	h := NewFIPSPasswordHasher()
	enc, err := h.Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("Hash err = %v", err)
	}
	// $pbkdf2-sha256$i=600000$<b64>$<b64>
	pattern := regexp.MustCompile(`^\$pbkdf2-sha256\$i=600000\$[A-Za-z0-9+/=]+\$[A-Za-z0-9+/=]+$`)
	if !pattern.MatchString(enc) {
		t.Errorf("encoded form does not match pbkdf2-sha256 regex: %s", enc)
	}
}

func TestFIPSHasher_HashDifferentSalts(t *testing.T) {
	h := NewFIPSPasswordHasher()
	a, _ := h.Hash("same-password")
	b, _ := h.Hash("same-password")
	if a == b {
		t.Errorf("two Hash() calls produced identical output (salt collision): %s", a)
	}
}

func TestFIPSHasher_VerifyWrongPassword(t *testing.T) {
	h := NewFIPSPasswordHasher()
	enc, _ := h.Hash("correct horse battery staple")
	ok, err := h.Verify("incorrect cat charging dock", enc)
	if err != nil {
		t.Errorf("Verify wrong password returned error %v; expected (false, nil)", err)
	}
	if ok {
		t.Errorf("Verify wrong password returned true; expected false")
	}
}

func TestFIPSHasher_VerifyMalformedSalt(t *testing.T) {
	h := NewFIPSPasswordHasher()
	// Valid hash component, salt has illegal base64 chars.
	encoded := "$pbkdf2-sha256$i=600000$!!!not-base64!!!$YWJjZGVmZ2hpamtsbW5vcA"
	ok, err := h.Verify("password", encoded)
	if ok {
		t.Errorf("Verify returned true on malformed salt")
	}
	if !errors.Is(err, ErrMalformedHash) {
		t.Errorf("Verify error = %v; want errors.Is(err, ErrMalformedHash)", err)
	}
}

func TestFIPSHasher_VerifyMalformedHash(t *testing.T) {
	h := NewFIPSPasswordHasher()
	encoded := "$pbkdf2-sha256$i=600000$YWJjZGVmZ2hpamtsbW5vcA$!!!not-base64!!!"
	ok, err := h.Verify("password", encoded)
	if ok {
		t.Errorf("Verify returned true on malformed hash")
	}
	if !errors.Is(err, ErrMalformedHash) {
		t.Errorf("Verify error = %v; want errors.Is(err, ErrMalformedHash)", err)
	}
}

func TestFIPSHasher_VerifyBadIterParam(t *testing.T) {
	h := NewFIPSPasswordHasher()
	encoded := "$pbkdf2-sha256$i=ABC$YWJjZGVmZ2hpamtsbW5vcA$YWJjZGVmZ2hpamtsbW5vcA"
	ok, err := h.Verify("password", encoded)
	if ok {
		t.Errorf("Verify returned true on non-numeric iters")
	}
	if !errors.Is(err, ErrMalformedHash) {
		t.Errorf("Verify error = %v; want errors.Is(err, ErrMalformedHash)", err)
	}
}

func TestFIPSHasher_VerifyIterFloor(t *testing.T) {
	h := NewFIPSPasswordHasher()
	// i=100 — below the 1000-iter sanity floor.
	encoded := "$pbkdf2-sha256$i=100$YWJjZGVmZ2hpamtsbW5vcA$YWJjZGVmZ2hpamtsbW5vcA"
	ok, err := h.Verify("password", encoded)
	if ok {
		t.Errorf("Verify returned true with iters below floor")
	}
	if !errors.Is(err, ErrMalformedHash) {
		t.Errorf("Verify error = %v; want errors.Is(err, ErrMalformedHash)", err)
	}
}

func TestFIPSHasher_VerifyWrongPartCount(t *testing.T) {
	h := NewFIPSPasswordHasher()
	cases := []string{
		"$pbkdf2-sha256$i=600000$saltonly",
		"$pbkdf2-sha256$i=600000$salt$hash$extra",
	}
	for _, c := range cases {
		ok, err := h.Verify("password", c)
		if ok {
			t.Errorf("Verify returned true on %q", c)
		}
		if !errors.Is(err, ErrMalformedHash) {
			t.Errorf("Verify(%q) error = %v; want errors.Is(err, ErrMalformedHash)", c, err)
		}
	}
}

func TestCrossAlgo_Argon2idHasherVerifiesPBKDF2(t *testing.T) {
	argonHasher := NewPasswordHasher()
	fipsHasher := NewFIPSPasswordHasher()

	pbkdf2Encoded, err := fipsHasher.Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("fipsHasher.Hash err = %v", err)
	}
	// Argon2id-constructed hasher verifies a PBKDF2-encoded hash.
	ok, err := argonHasher.Verify("correct horse battery staple", pbkdf2Encoded)
	if err != nil {
		t.Fatalf("argonHasher.Verify(pbkdf2-encoded) err = %v", err)
	}
	if !ok {
		t.Errorf("argonHasher.Verify(pbkdf2-encoded) = false; want true")
	}
}

func TestCrossAlgo_FIPSHasherVerifiesArgon2id(t *testing.T) {
	argonHasher := NewPasswordHasher()
	fipsHasher := NewFIPSPasswordHasher()

	argonEncoded, err := argonHasher.Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("argonHasher.Hash err = %v", err)
	}
	// PBKDF2-constructed hasher verifies an Argon2id-encoded hash.
	ok, err := fipsHasher.Verify("correct horse battery staple", argonEncoded)
	if err != nil {
		t.Fatalf("fipsHasher.Verify(argon2id-encoded) err = %v", err)
	}
	if !ok {
		t.Errorf("fipsHasher.Verify(argon2id-encoded) = false; want true")
	}
}

func TestCrossAlgo_WrongPasswordEitherDirection(t *testing.T) {
	argonHasher := NewPasswordHasher()
	fipsHasher := NewFIPSPasswordHasher()

	argonEncoded, _ := argonHasher.Hash("correct horse battery staple")
	pbkdf2Encoded, _ := fipsHasher.Hash("correct horse battery staple")

	ok, err := fipsHasher.Verify("wrong password", argonEncoded)
	if err != nil || ok {
		t.Errorf("fips.Verify(wrong, argon) = (%v, %v); want (false, nil)", ok, err)
	}
	ok, err = argonHasher.Verify("wrong password", pbkdf2Encoded)
	if err != nil || ok {
		t.Errorf("argon.Verify(wrong, pbkdf2) = (%v, %v); want (false, nil)", ok, err)
	}
}
