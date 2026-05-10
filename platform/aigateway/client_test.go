package aigateway

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestNewClientNotConfigured(t *testing.T) {
	_, err := NewClient(Config{})
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("NewClient with empty BaseURL: got %v, want ErrNotConfigured", err)
	}
}

func TestNewClientFillsDefaults(t *testing.T) {
	c, err := NewClient(Config{BaseURL: "https://example.com/"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if !c.IsConfigured() {
		t.Errorf("IsConfigured() = false after successful construction")
	}
	if c.cfg.BaseURL != "https://example.com" {
		t.Errorf("BaseURL=%q want trimmed", c.cfg.BaseURL)
	}
	if c.cfg.Timeout != defaultTimeout {
		t.Errorf("Timeout=%s want default", c.cfg.Timeout)
	}
	if c.cfg.MaxRetries != defaultMaxRetries {
		t.Errorf("MaxRetries=%d want default", c.cfg.MaxRetries)
	}
	if c.DefaultModel() != defaultDefaultModel {
		t.Errorf("DefaultModel=%q", c.DefaultModel())
	}
	if c.BaseURL() != "https://example.com" {
		t.Errorf("BaseURL()=%q", c.BaseURL())
	}
}

func TestNilClientIsConfiguredFalse(t *testing.T) {
	var c *Client
	if c.IsConfigured() {
		t.Error("nil client IsConfigured() should be false")
	}
	if c.BaseURL() != "" {
		t.Error("nil client BaseURL() should be empty")
	}
	if c.DefaultModel() != "" {
		t.Error("nil client DefaultModel() should be empty")
	}
}

func TestWithObserverAndHTTPClient(t *testing.T) {
	custom := &http.Client{Timeout: 7 * time.Second}
	obs := &captureObserver{}
	c, err := NewClient(Config{BaseURL: "https://example.com"},
		WithHTTPClient(custom),
		WithObserver(obs),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if c.http != custom {
		t.Errorf("WithHTTPClient was not applied")
	}
	if c.observer != obs {
		t.Errorf("WithObserver was not applied")
	}
}

func TestWithObserverNilIgnored(t *testing.T) {
	c, err := NewClient(Config{BaseURL: "https://example.com"}, WithObserver(nil))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, ok := c.observer.(NoopObserver); !ok {
		t.Errorf("WithObserver(nil) replaced default; want NoopObserver, got %T", c.observer)
	}
}
