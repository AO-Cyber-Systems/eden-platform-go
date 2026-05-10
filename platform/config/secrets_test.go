package config

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestGetSecret_Base64(t *testing.T) {
	t.Setenv("MY_B64_SECRET_BASE64", base64.StdEncoding.EncodeToString([]byte("decoded-value")))
	if v := GetSecret("MY_B64_SECRET", "fallback"); v != "decoded-value" {
		t.Errorf("GetSecret() = %q, want decoded-value", v)
	}
}

func TestGetSecret_Base64URLEncoding(t *testing.T) {
	t.Setenv("MY_URL_B64_BASE64", base64.URLEncoding.EncodeToString([]byte("urlsafe")))
	if v := GetSecret("MY_URL_B64", "fb"); v != "urlsafe" {
		t.Errorf("GetSecret() = %q, want urlsafe", v)
	}
}

func TestGetSecret_Base64RawNoPadding(t *testing.T) {
	t.Setenv("MY_RAW_B64_BASE64", base64.RawStdEncoding.EncodeToString([]byte("raw")))
	if v := GetSecret("MY_RAW_B64", "fb"); v != "raw" {
		t.Errorf("GetSecret() = %q, want raw", v)
	}
}

func TestGetSecret_Base64Invalid_FallsThrough(t *testing.T) {
	t.Setenv("MY_BAD_B64_BASE64", "!!!not-base64!!!")
	if v := GetSecret("MY_BAD_B64", "fb"); v != "fb" {
		t.Errorf("GetSecret() = %q, want fb (fallback after bad b64)", v)
	}
}

func TestGetSecret_PrefersFileOverBase64(t *testing.T) {
	tmp := t.TempDir()
	secretFile := filepath.Join(tmp, "s.txt")
	if err := os.WriteFile(secretFile, []byte("from-file"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv("MY_PREF_SECRET_FILE", secretFile)
	t.Setenv("MY_PREF_SECRET_BASE64", base64.StdEncoding.EncodeToString([]byte("from-b64")))
	if v := GetSecret("MY_PREF_SECRET", "fb"); v != "from-file" {
		t.Errorf("GetSecret() = %q, want from-file", v)
	}
}

func TestGetSecret_FileMissing_FallsThroughToBase64(t *testing.T) {
	t.Setenv("MY_FB_SECRET_FILE", "/nonexistent/path/that/does/not/exist")
	t.Setenv("MY_FB_SECRET_BASE64", base64.StdEncoding.EncodeToString([]byte("recovered")))
	if v := GetSecret("MY_FB_SECRET", "fb"); v != "recovered" {
		t.Errorf("GetSecret() = %q, want recovered", v)
	}
}

func TestGetSecret_EnvSpecificOverride(t *testing.T) {
	t.Setenv("MY_ENV_SEC", "default-val")
	t.Setenv("MY_ENV_SEC__prod", "prod-val")
	if v := GetSecretFor("MY_ENV_SEC", "fb", "prod"); v != "prod-val" {
		t.Errorf("GetSecretFor(prod) = %q, want prod-val", v)
	}
	if v := GetSecretFor("MY_ENV_SEC", "fb", "stage"); v != "default-val" {
		t.Errorf("GetSecretFor(stage) = %q, want default-val (no override)", v)
	}
}
