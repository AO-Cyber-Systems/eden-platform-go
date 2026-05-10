package encryption

import (
	"bytes"
	"testing"
)

func TestDeriveKeyFromPassphrase_Length(t *testing.T) {
	key := DeriveKeyFromPassphrase("any-secret")
	if len(key) != 32 {
		t.Fatalf("DeriveKeyFromPassphrase length = %d, want 32", len(key))
	}
}

func TestDeriveKeyFromPassphrase_Deterministic(t *testing.T) {
	a := DeriveKeyFromPassphrase("my-secret-key-base")
	b := DeriveKeyFromPassphrase("my-secret-key-base")
	if !bytes.Equal(a, b) {
		t.Fatal("DeriveKeyFromPassphrase not deterministic for the same input")
	}
}

func TestDeriveKeyFromPassphrase_DistinctSecrets(t *testing.T) {
	if bytes.Equal(
		DeriveKeyFromPassphrase("secret-one"),
		DeriveKeyFromPassphrase("secret-two"),
	) {
		t.Fatal("DeriveKeyFromPassphrase collision on distinct inputs")
	}
}

func TestDeriveKeyFromPassphrase_RoundTripWithFieldEncryptor(t *testing.T) {
	encKey := DeriveKeyFromPassphrase("test-secret-key")
	idxKey := DeriveKeyFromPassphrase("test-blind-index")

	enc, err := New(encKey, idxKey)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	plaintext := []byte(`{"email":"test@example.com","ssn":"123-45-6789"}`)
	ct, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	pt, err := enc.Decrypt(ct)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if !bytes.Equal(pt, plaintext) {
		t.Fatalf("Decrypt() = %q, want %q", pt, plaintext)
	}
}
