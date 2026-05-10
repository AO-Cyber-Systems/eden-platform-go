package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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

func TestGetInt_Valid(t *testing.T) {
	t.Setenv("INT_KEY", "42")
	if v := GetInt("INT_KEY", 7); v != 42 {
		t.Errorf("GetInt() = %d, want 42", v)
	}
}

func TestGetInt_FallbackOnEmpty(t *testing.T) {
	if v := GetInt("DEFINITELY_UNSET_INT", 99); v != 99 {
		t.Errorf("GetInt() = %d, want 99", v)
	}
}

func TestGetInt_FallbackOnInvalid(t *testing.T) {
	t.Setenv("INT_BAD", "not-a-number")
	if v := GetInt("INT_BAD", 5); v != 5 {
		t.Errorf("GetInt() = %d, want 5 (fallback)", v)
	}
}

func TestGetBool(t *testing.T) {
	t.Setenv("BOOL_TRUE", "true")
	t.Setenv("BOOL_FALSE", "0")
	t.Setenv("BOOL_BAD", "yes-please")
	if !GetBool("BOOL_TRUE", false) {
		t.Errorf("BOOL_TRUE → false, want true")
	}
	if GetBool("BOOL_FALSE", true) {
		t.Errorf("BOOL_FALSE → true, want false")
	}
	if !GetBool("BOOL_BAD", true) {
		t.Errorf("BOOL_BAD invalid value should fall back to true")
	}
	if GetBool("BOOL_UNSET_NEVER", false) {
		t.Errorf("unset key should return fallback")
	}
}

func TestGetDuration(t *testing.T) {
	t.Setenv("DUR_KEY", "750ms")
	if d := GetDuration("DUR_KEY", time.Second); d != 750*time.Millisecond {
		t.Errorf("GetDuration() = %v, want 750ms", d)
	}
	if d := GetDuration("DUR_UNSET", time.Hour); d != time.Hour {
		t.Errorf("GetDuration() = %v, want 1h fallback", d)
	}
	t.Setenv("DUR_BAD", "not-a-duration")
	if d := GetDuration("DUR_BAD", 5*time.Second); d != 5*time.Second {
		t.Errorf("GetDuration() = %v, want 5s fallback on invalid", d)
	}
}

func TestMustGet_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("MustGet on unset key did not panic")
		}
	}()
	_ = MustGet("DEFINITELY_UNSET_MUSTGET_KEY")
}

func TestMustGet_OK(t *testing.T) {
	t.Setenv("MUST_KEY", "v")
	if got := MustGet("MUST_KEY"); got != "v" {
		t.Errorf("MustGet() = %q, want v", got)
	}
}

func TestRequired_AllSet(t *testing.T) {
	t.Setenv("R1", "a")
	t.Setenv("R2", "b")
	if err := Required("R1", "R2"); err != nil {
		t.Errorf("Required() error = %v, want nil", err)
	}
}

func TestRequired_Missing(t *testing.T) {
	t.Setenv("R_PRESENT", "ok")
	err := Required("R_PRESENT", "R_DEFINITELY_NOT_SET_AT_ALL")
	if err == nil {
		t.Fatalf("Required() expected error for missing key")
	}
}

func TestPlatformConfig_Validate_OK(t *testing.T) {
	c := &PlatformConfig{DatabaseURL: "postgres://x", PlatformMode: "b2b"}
	if err := c.Validate(); err != nil {
		t.Errorf("Validate() error = %v, want nil", err)
	}
}

func TestPlatformConfig_Validate_MissingDB(t *testing.T) {
	c := &PlatformConfig{}
	if err := c.Validate(); err == nil {
		t.Errorf("Validate() expected error for missing DATABASE_URL")
	}
}

func TestPlatformConfig_Validate_BadMode(t *testing.T) {
	c := &PlatformConfig{DatabaseURL: "x", PlatformMode: "garbage"}
	if err := c.Validate(); err == nil {
		t.Errorf("Validate() expected error for invalid PLATFORM_MODE")
	}
}

func TestLoadFor_EnvOverride(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://default")
	t.Setenv("DATABASE_URL__prod", "postgres://prod")
	cfg := LoadFor("prod")
	if cfg.DatabaseURL != "postgres://prod" {
		t.Errorf("DatabaseURL = %q, want postgres://prod", cfg.DatabaseURL)
	}
}

func TestLoadFor_NoEnvFallsBackToDefault(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://default")
	t.Setenv("DATABASE_URL__prod", "postgres://prod")
	cfg := LoadFor("staging")
	if cfg.DatabaseURL != "postgres://default" {
		t.Errorf("DatabaseURL = %q, want postgres://default", cfg.DatabaseURL)
	}
}

func TestLoadFor_EmptyEnvUsesEDEN_ENV(t *testing.T) {
	t.Setenv("EDEN_ENV", "prod")
	t.Setenv("DATABASE_URL__prod", "postgres://from-eden-env")
	cfg := LoadFor("")
	if cfg.DatabaseURL != "postgres://from-eden-env" {
		t.Errorf("DatabaseURL = %q, want from-eden-env", cfg.DatabaseURL)
	}
}

func TestGetEnvFor_Override(t *testing.T) {
	t.Setenv("FOO", "default")
	t.Setenv("FOO__staging", "staged")
	if v := GetEnvFor("FOO", "fb", "staging"); v != "staged" {
		t.Errorf("GetEnvFor(staging) = %q, want staged", v)
	}
	if v := GetEnvFor("FOO", "fb", "prod"); v != "default" {
		t.Errorf("GetEnvFor(prod) = %q, want default (no override)", v)
	}
}
