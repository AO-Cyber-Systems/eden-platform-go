package aigateway

import (
	"strings"
	"testing"
	"time"
)

func TestConfigFromEnvDefaults(t *testing.T) {
	t.Setenv(EnvBaseURL, "https://api.aosentry.ai")
	t.Setenv(EnvAPIKey, "")
	t.Setenv(EnvDefaultModel, "")
	t.Setenv(EnvTimeout, "")
	t.Setenv(EnvMaxRetries, "")

	cfg, err := ConfigFromEnv()
	if err != nil {
		t.Fatalf("ConfigFromEnv: %v", err)
	}
	if cfg.BaseURL != "https://api.aosentry.ai" {
		t.Errorf("BaseURL=%q want trimmed env value", cfg.BaseURL)
	}
	if cfg.DefaultModel != defaultDefaultModel {
		t.Errorf("DefaultModel=%q want %q", cfg.DefaultModel, defaultDefaultModel)
	}
	if cfg.Timeout != 0 {
		t.Errorf("Timeout=%s want zero (defaulted by withDefaults at construction)", cfg.Timeout)
	}
	if cfg.MaxRetries != 0 {
		t.Errorf("MaxRetries=%d want zero", cfg.MaxRetries)
	}
}

func TestConfigFromEnvTrimsTrailingSlash(t *testing.T) {
	t.Setenv(EnvBaseURL, "https://api.aosentry.ai///")
	cfg, err := ConfigFromEnv()
	if err != nil {
		t.Fatalf("ConfigFromEnv: %v", err)
	}
	if cfg.BaseURL != "https://api.aosentry.ai" {
		t.Errorf("BaseURL=%q want trailing slashes trimmed", cfg.BaseURL)
	}
}

func TestConfigFromEnvMissingBaseURL(t *testing.T) {
	t.Setenv(EnvBaseURL, "")
	if _, err := ConfigFromEnv(); err == nil {
		t.Fatal("ConfigFromEnv with empty BaseURL: expected error, got nil")
	} else if !strings.Contains(err.Error(), EnvBaseURL) {
		t.Errorf("error %q does not mention env var name", err.Error())
	}
}

func TestConfigFromEnvOverrides(t *testing.T) {
	t.Setenv(EnvBaseURL, "https://example.com")
	t.Setenv(EnvAPIKey, "sk-test")
	t.Setenv(EnvDefaultModel, "claude-sonnet-4-6")
	t.Setenv(EnvTimeout, "45s")
	t.Setenv(EnvMaxRetries, "5")

	cfg, err := ConfigFromEnv()
	if err != nil {
		t.Fatalf("ConfigFromEnv: %v", err)
	}
	if cfg.APIKey != "sk-test" {
		t.Errorf("APIKey=%q", cfg.APIKey)
	}
	if cfg.DefaultModel != "claude-sonnet-4-6" {
		t.Errorf("DefaultModel=%q", cfg.DefaultModel)
	}
	if cfg.Timeout != 45*time.Second {
		t.Errorf("Timeout=%s", cfg.Timeout)
	}
	if cfg.MaxRetries != 5 {
		t.Errorf("MaxRetries=%d", cfg.MaxRetries)
	}
}

func TestConfigFromEnvBadTimeout(t *testing.T) {
	t.Setenv(EnvBaseURL, "https://example.com")
	t.Setenv(EnvTimeout, "not-a-duration")
	if _, err := ConfigFromEnv(); err == nil {
		t.Fatal("expected error for bad timeout")
	}
}

func TestConfigFromEnvBadRetries(t *testing.T) {
	t.Setenv(EnvBaseURL, "https://example.com")
	t.Setenv(EnvMaxRetries, "-2")
	if _, err := ConfigFromEnv(); err == nil {
		t.Fatal("expected error for negative retries")
	}
}

func TestConfigWithDefaults(t *testing.T) {
	cfg := Config{BaseURL: "https://example.com/"}.withDefaults()
	if cfg.BaseURL != "https://example.com" {
		t.Errorf("BaseURL not trimmed: %q", cfg.BaseURL)
	}
	if cfg.Timeout != defaultTimeout {
		t.Errorf("Timeout=%s want %s", cfg.Timeout, defaultTimeout)
	}
	if cfg.MaxRetries != defaultMaxRetries {
		t.Errorf("MaxRetries=%d want %d", cfg.MaxRetries, defaultMaxRetries)
	}
	if cfg.DefaultModel != defaultDefaultModel {
		t.Errorf("DefaultModel=%q want %q", cfg.DefaultModel, defaultDefaultModel)
	}
}
