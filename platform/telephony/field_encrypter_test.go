package telephony

import (
	"bytes"
	"testing"
)

// testKey is a fixed 32-byte AES-256 key for deterministic tests.
func testKey() []byte { return bytes.Repeat([]byte("a"), 32) }

// Compile-time assertion that *FieldEncrypter satisfies the Encrypter interface.
var _ Encrypter = (*FieldEncrypter)(nil)

func TestFieldEncrypterValidKey(t *testing.T) {
	enc, err := NewFieldEncrypter(testKey())
	if err != nil {
		t.Fatalf("NewFieldEncrypter with 32-byte key: unexpected error: %v", err)
	}
	if enc == nil {
		t.Fatal("NewFieldEncrypter with 32-byte key: returned nil encrypter")
	}
}

func TestFieldEncrypterBadKeyLength(t *testing.T) {
	// platform/encryption.New rejects non-32-byte keys; the adapter must surface that.
	enc, err := NewFieldEncrypter(bytes.Repeat([]byte("a"), 16))
	if err == nil {
		t.Fatal("NewFieldEncrypter with 16-byte key: expected error, got nil")
	}
	if enc != nil {
		t.Errorf("NewFieldEncrypter with 16-byte key: expected nil encrypter, got %v", enc)
	}
}

func TestFieldEncrypterRoundTrip(t *testing.T) {
	enc, err := NewFieldEncrypter(testKey())
	if err != nil {
		t.Fatalf("NewFieldEncrypter: %v", err)
	}

	const plaintext = "AC_secret_token"
	ciphertext, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if string(ciphertext) == plaintext {
		t.Error("Encrypt: ciphertext equals plaintext (not encrypted)")
	}

	got, err := enc.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if got != plaintext {
		t.Errorf("round-trip: got %q, want %q", got, plaintext)
	}
}

func TestFieldEncrypterNonceUniqueness(t *testing.T) {
	enc, err := NewFieldEncrypter(testKey())
	if err != nil {
		t.Fatalf("NewFieldEncrypter: %v", err)
	}

	const plaintext = "AC_secret_token"
	c1, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt #1: %v", err)
	}
	c2, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt #2: %v", err)
	}
	if bytes.Equal(c1, c2) {
		t.Error("two Encrypt of same plaintext produced identical ciphertext (random nonce missing)")
	}

	// Both must still decrypt back to the same plaintext.
	for i, c := range [][]byte{c1, c2} {
		got, err := enc.Decrypt(c)
		if err != nil {
			t.Fatalf("Decrypt #%d: %v", i+1, err)
		}
		if got != plaintext {
			t.Errorf("Decrypt #%d: got %q, want %q", i+1, got, plaintext)
		}
	}
}

func TestFieldEncrypterEmptyString(t *testing.T) {
	enc, err := NewFieldEncrypter(testKey())
	if err != nil {
		t.Fatalf("NewFieldEncrypter: %v", err)
	}

	ciphertext, err := enc.Encrypt("")
	if err != nil {
		t.Fatalf("Encrypt empty: %v", err)
	}
	got, err := enc.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt empty: %v", err)
	}
	if got != "" {
		t.Errorf("empty round-trip: got %q, want \"\"", got)
	}
}
