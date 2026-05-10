package encryption

import (
	"encoding/base64"
	"encoding/hex"
	"testing"
)

func TestKeyFromHex_OK(t *testing.T) {
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = byte(i)
	}
	s := hex.EncodeToString(raw)
	got, err := KeyFromHex(s)
	if err != nil {
		t.Fatalf("KeyFromHex error = %v", err)
	}
	if len(got) != 32 {
		t.Errorf("len = %d, want 32", len(got))
	}
}

func TestKeyFromHex_WrongLength(t *testing.T) {
	short := hex.EncodeToString(make([]byte, 16))
	if _, err := KeyFromHex(short); err == nil {
		t.Errorf("expected error for 16-byte hex")
	}
}

func TestKeyFromHex_InvalidChars(t *testing.T) {
	if _, err := KeyFromHex("zzz"); err == nil {
		t.Errorf("expected error for non-hex characters")
	}
}

func TestKeyFromBase64_StdEncoding(t *testing.T) {
	raw := make([]byte, 32)
	s := base64.StdEncoding.EncodeToString(raw)
	got, err := KeyFromBase64(s)
	if err != nil {
		t.Fatalf("KeyFromBase64 error = %v", err)
	}
	if len(got) != 32 {
		t.Errorf("len = %d, want 32", len(got))
	}
}

func TestKeyFromBase64_URLEncoding(t *testing.T) {
	raw := make([]byte, 32)
	s := base64.URLEncoding.EncodeToString(raw)
	if _, err := KeyFromBase64(s); err != nil {
		t.Errorf("URL encoding rejected: %v", err)
	}
}

func TestKeyFromBase64_RawNoPadding(t *testing.T) {
	raw := make([]byte, 32)
	s := base64.RawStdEncoding.EncodeToString(raw)
	if _, err := KeyFromBase64(s); err != nil {
		t.Errorf("raw std encoding rejected: %v", err)
	}
}

func TestKeyFromBase64_WrongLength(t *testing.T) {
	s := base64.StdEncoding.EncodeToString(make([]byte, 16))
	if _, err := KeyFromBase64(s); err == nil {
		t.Errorf("expected error for 16-byte base64")
	}
}

func TestKeyFromEnv_Hex(t *testing.T) {
	t.Setenv("ENC_KEY_HEX_TEST", hex.EncodeToString(make([]byte, 32)))
	got, err := KeyFromEnv("ENC_KEY_HEX_TEST")
	if err != nil {
		t.Fatalf("KeyFromEnv error = %v", err)
	}
	if len(got) != 32 {
		t.Errorf("len = %d, want 32", len(got))
	}
}

func TestKeyFromEnv_Base64(t *testing.T) {
	t.Setenv("ENC_KEY_B64_TEST", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if _, err := KeyFromEnv("ENC_KEY_B64_TEST"); err != nil {
		t.Errorf("KeyFromEnv(base64) error = %v", err)
	}
}

func TestKeyFromEnv_Empty(t *testing.T) {
	if _, err := KeyFromEnv("DEFINITELY_UNSET_ENC_KEY"); err == nil {
		t.Errorf("expected error for unset env var")
	}
}

func TestKeyFromEnv_Invalid(t *testing.T) {
	t.Setenv("ENC_KEY_BAD_TEST", "not-a-valid-key")
	if _, err := KeyFromEnv("ENC_KEY_BAD_TEST"); err == nil {
		t.Errorf("expected error for invalid value")
	}
}

func TestLooksLikeHex(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"abcd", false},
		{string(make([]byte, 64)), false}, // null bytes
		{hex.EncodeToString(make([]byte, 32)), true},
		{"GG" + hex.EncodeToString(make([]byte, 31)), false}, // G is not hex
	}
	for _, tt := range tests {
		if got := looksLikeHex(tt.in); got != tt.want {
			t.Errorf("looksLikeHex(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}
