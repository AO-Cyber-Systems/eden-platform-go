package encryption

import (
	"strings"
	"testing"
)

func newTestEncryptor(t *testing.T) *FieldEncryptor {
	t.Helper()
	enc, err := New(make([]byte, 32), make([]byte, 32))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return enc
}

func TestEncryptString_RoundTrip(t *testing.T) {
	enc := newTestEncryptor(t)
	ct, err := enc.EncryptString("hello world")
	if err != nil {
		t.Fatalf("EncryptString error = %v", err)
	}
	if strings.HasPrefix(ct, "v1:") {
		t.Errorf("EncryptString must NOT add v1 prefix; got %q", ct)
	}
	pt, err := enc.DecryptString(ct)
	if err != nil {
		t.Fatalf("DecryptString error = %v", err)
	}
	if pt != "hello world" {
		t.Errorf("plaintext = %q, want hello world", pt)
	}
}

func TestEncryptStringV1_RoundTrip(t *testing.T) {
	enc := newTestEncryptor(t)
	ct, err := enc.EncryptStringV1("payload")
	if err != nil {
		t.Fatalf("EncryptStringV1 error = %v", err)
	}
	if !strings.HasPrefix(ct, "v1:") {
		t.Errorf("expected v1: prefix, got %q", ct)
	}
	pt, err := enc.DecryptString(ct)
	if err != nil {
		t.Fatalf("DecryptString error = %v", err)
	}
	if pt != "payload" {
		t.Errorf("plaintext = %q, want payload", pt)
	}
}

func TestDecryptString_AcceptsBothForms(t *testing.T) {
	enc := newTestEncryptor(t)
	plain := []string{"alpha", "beta gamma"}
	for _, p := range plain {
		legacy, _ := enc.EncryptString(p)
		v1, _ := enc.EncryptStringV1(p)

		got1, err := enc.DecryptString(legacy)
		if err != nil || got1 != p {
			t.Errorf("legacy decrypt = %q,%v; want %q", got1, err, p)
		}
		got2, err := enc.DecryptString(v1)
		if err != nil || got2 != p {
			t.Errorf("v1 decrypt = %q,%v; want %q", got2, err, p)
		}
	}
}

func TestDecryptString_BadBase64(t *testing.T) {
	enc := newTestEncryptor(t)
	if _, err := enc.DecryptString("not!base64!!!"); err == nil {
		t.Errorf("expected error for invalid base64")
	}
	if _, err := enc.DecryptString("v1:not!base64!!!"); err == nil {
		t.Errorf("expected error for invalid base64 with v1 prefix")
	}
}

func TestBlindIndexLower_CaseInsensitive(t *testing.T) {
	enc := newTestEncryptor(t)
	a := enc.BlindIndexLower("Test@Example.com")
	b := enc.BlindIndexLower("test@example.com")
	c := enc.BlindIndexLower("TEST@EXAMPLE.COM")
	if a != b || b != c {
		t.Errorf("BlindIndexLower not case-insensitive: %q %q %q", a, b, c)
	}
}

func TestBlindIndexLower_DiffersFromBlindIndex(t *testing.T) {
	enc := newTestEncryptor(t)
	if enc.BlindIndex("Test") == enc.BlindIndexLower("Test") {
		t.Errorf("BlindIndex and BlindIndexLower should differ for non-lowercase input")
	}
}
