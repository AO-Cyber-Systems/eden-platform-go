package aigateway

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Default values applied when fields are zero.
const (
	defaultTimeout      = 30 * time.Second
	defaultMaxRetries   = 3
	defaultDefaultModel = "gpt-4o-mini"
)

// Environment variable names recognized by ConfigFromEnv.
const (
	EnvBaseURL      = "AOSENTRY_BASE_URL"
	EnvAPIKey       = "AOSENTRY_API_KEY"
	EnvDefaultModel = "AOSENTRY_DEFAULT_MODEL"
	EnvTimeout      = "AOSENTRY_TIMEOUT"
	EnvMaxRetries   = "AOSENTRY_MAX_RETRIES"
)

// Config holds connection parameters for the AOSentry AI gateway.
//
// Zero values are normalized by NewClient: Timeout defaults to 30s,
// MaxRetries to 3, DefaultModel to "gpt-4o-mini". An empty BaseURL means the
// gateway is intentionally disabled and NewClient returns ErrNotConfigured so
// consumers can degrade gracefully.
type Config struct {
	// BaseURL of the AOSentry deployment, e.g. https://api.aosentry.ai.
	// Trailing slashes are trimmed at construction time.
	BaseURL string

	// APIKey is the bearer token sent in the Authorization header on every
	// request. The key drives budget, rate-limit, model allowlist, and spend
	// attribution server-side.
	APIKey string

	// DefaultModel is used by methods (chat/embeddings) when the request
	// does not specify a model. Defaults to "gpt-4o-mini".
	DefaultModel string

	// Timeout is the per-request HTTP client timeout. Streaming methods
	// override this with a context-bound deadline.
	Timeout time.Duration

	// MaxRetries is the number of retry attempts for transient failures
	// (HTTP 429 and 5xx). 0 disables retries; negative values are treated
	// as 0.
	MaxRetries int
}

// ConfigFromEnv builds a Config from process environment variables.
//
// Required: AOSENTRY_BASE_URL.
// Optional: AOSENTRY_API_KEY, AOSENTRY_DEFAULT_MODEL, AOSENTRY_TIMEOUT
// (Go duration string), AOSENTRY_MAX_RETRIES (non-negative integer).
func ConfigFromEnv() (Config, error) {
	cfg := Config{
		BaseURL:      strings.TrimRight(os.Getenv(EnvBaseURL), "/"),
		APIKey:       os.Getenv(EnvAPIKey),
		DefaultModel: os.Getenv(EnvDefaultModel),
	}
	if cfg.BaseURL == "" {
		return Config{}, fmt.Errorf("aigateway: %s is required", EnvBaseURL)
	}
	if cfg.DefaultModel == "" {
		cfg.DefaultModel = defaultDefaultModel
	}
	if v := os.Getenv(EnvTimeout); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("aigateway: invalid %s %q: %w", EnvTimeout, v, err)
		}
		cfg.Timeout = d
	}
	if v := os.Getenv(EnvMaxRetries); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return Config{}, fmt.Errorf("aigateway: invalid %s %q (want non-negative integer)", EnvMaxRetries, v)
		}
		cfg.MaxRetries = n
	}
	return cfg, nil
}

// withDefaults returns a copy of cfg with zero-valued fields filled in.
func (c Config) withDefaults() Config {
	c.BaseURL = strings.TrimRight(c.BaseURL, "/")
	if c.Timeout <= 0 {
		c.Timeout = defaultTimeout
	}
	if c.MaxRetries < 0 {
		c.MaxRetries = 0
	}
	if c.MaxRetries == 0 {
		c.MaxRetries = defaultMaxRetries
	}
	if c.DefaultModel == "" {
		c.DefaultModel = defaultDefaultModel
	}
	return c
}
