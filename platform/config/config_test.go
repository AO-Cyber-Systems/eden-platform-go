package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetEnv_WithValue(t *testing.T) {
	t.Setenv("TEST_CONFIG_KEY", "my-value")
	if v := GetEnv("TEST_CONFIG_KEY", "fallback"); v != "my-value" {
		t.Errorf("GetEnv() = %q, want %q", v, "my-value")
	}
}

func TestGetEnv_Fallback(t *testing.T) {
	if v := GetEnv("DEFINITELY_UNSET_KEY_12345", "fallback"); v != "fallback" {
		t.Errorf("GetEnv() = %q, want %q", v, "fallback")
	}
}

func TestGetSecret_FromEnvVar(t *testing.T) {
	t.Setenv("MY_SECRET", "secret-value")
	if v := GetSecret("MY_SECRET", "fallback"); v != "secret-value" {
		t.Errorf("GetSecret() = %q, want %q", v, "secret-value")
	}
}

func TestGetSecret_FromFile(t *testing.T) {
	tmpDir := t.TempDir()
	secretFile := filepath.Join(tmpDir, "secret.txt")
	if err := os.WriteFile(secretFile, []byte("file-secret\n"), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	t.Setenv("MY_FILE_SECRET_FILE", secretFile)
	if v := GetSecret("MY_FILE_SECRET", "fallback"); v != "file-secret" {
		t.Errorf("GetSecret() = %q, want %q", v, "file-secret")
	}
}

func TestLoad(t *testing.T) {
	cfg := Load()
	if cfg == nil {
		t.Fatalf("Load() returned nil")
	}
	if cfg.ServerAddr == "" {
		t.Errorf("Load() ServerAddr is empty, want default")
	}
	if cfg.DatabaseURL == "" {
		t.Errorf("Load() DatabaseURL is empty, want default")
	}
}
