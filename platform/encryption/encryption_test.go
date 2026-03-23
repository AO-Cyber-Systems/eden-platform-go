package encryption

import (
	"bytes"
	"testing"
)

func TestFieldEncryptor_EncryptDecrypt(t *testing.T) {
	encKey, _ := GenerateKey()
	idxKey, _ := GenerateKey()
	enc, err := New(encKey, idxKey)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	plaintext := []byte("sensitive data")
	ciphertext, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	decrypted, err := enc.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Errorf("Decrypt() = %q, want %q", decrypted, plaintext)
	}
}

func TestFieldEncryptor_DifferentCiphertexts(t *testing.T) {
	encKey, _ := GenerateKey()
	idxKey, _ := GenerateKey()
	enc, _ := New(encKey, idxKey)

	plaintext := []byte("same data")
	ct1, _ := enc.Encrypt(plaintext)
	ct2, _ := enc.Encrypt(plaintext)

	if bytes.Equal(ct1, ct2) {
		t.Errorf("Encrypt() produced identical ciphertexts for same plaintext (nonce should differ)")
	}
}

func TestFieldEncryptor_DecryptTampered(t *testing.T) {
	encKey, _ := GenerateKey()
	idxKey, _ := GenerateKey()
	enc, _ := New(encKey, idxKey)

	ciphertext, _ := enc.Encrypt([]byte("data"))
	ciphertext[len(ciphertext)-1] ^= 0xff // flip last byte

	_, err := enc.Decrypt(ciphertext)
	if err == nil {
		t.Errorf("Decrypt() of tampered ciphertext expected error, got nil")
	}
}

func TestFieldEncryptor_BlindIndex_Deterministic(t *testing.T) {
	encKey, _ := GenerateKey()
	idxKey, _ := GenerateKey()
	enc, _ := New(encKey, idxKey)

	idx1 := enc.BlindIndex("test@example.com")
	idx2 := enc.BlindIndex("test@example.com")

	if idx1 != idx2 {
		t.Errorf("BlindIndex() not deterministic: %q != %q", idx1, idx2)
	}
}

func TestFieldEncryptor_BlindIndex_Different(t *testing.T) {
	encKey, _ := GenerateKey()
	idxKey, _ := GenerateKey()
	enc, _ := New(encKey, idxKey)

	idx1 := enc.BlindIndex("alice@example.com")
	idx2 := enc.BlindIndex("bob@example.com")

	if idx1 == idx2 {
		t.Errorf("BlindIndex() same for different inputs")
	}
}

func TestFieldEncryptor_InvalidKeyLength(t *testing.T) {
	short := make([]byte, 16)
	valid := make([]byte, 32)

	_, err := New(short, valid)
	if err == nil {
		t.Errorf("New() with short encryption key expected error")
	}

	_, err = New(valid, short)
	if err == nil {
		t.Errorf("New() with short blind index key expected error")
	}
}

func TestGenerateKey(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	if len(key) != 32 {
		t.Errorf("GenerateKey() length = %d, want 32", len(key))
	}
}
