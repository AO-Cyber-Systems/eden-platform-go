package aigateway

import (
	"net/http"
)

// Client is a thread-safe HTTP client for the AOSentry AI gateway.
//
// Methods on Client are safe for concurrent use by multiple goroutines.
// Construct via NewClient. A nil *Client is not valid; methods called on a
// nil receiver return ErrNotConfigured rather than panicking.
type Client struct {
	cfg      Config
	http     *http.Client
	observer Observer
}

// Option configures optional Client behavior. Applied in order by NewClient.
type Option func(*Client)

// WithObserver attaches an Observer that receives an Event for every
// outbound call (including failed ones). Pass NoopObserver{} explicitly to
// disable observation; not setting an observer is equivalent.
func WithObserver(o Observer) Option {
	return func(c *Client) {
		if o != nil {
			c.observer = o
		}
	}
}

// WithHTTPClient swaps the underlying *http.Client. Primarily useful in
// tests (httptest) and for callers wiring a custom Transport for proxies,
// mTLS, or instrumentation.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) {
		if h != nil {
			c.http = h
		}
	}
}

// NewClient constructs a Client from the provided Config and options.
//
// Returns ErrNotConfigured when Config.BaseURL is empty so callers can
// degrade gracefully in environments where AOSentry is intentionally not
// provisioned. Other zero-valued Config fields are filled with defaults
// (Timeout=30s, MaxRetries=3, DefaultModel="gpt-4o-mini").
func NewClient(cfg Config, opts ...Option) (*Client, error) {
	if cfg.BaseURL == "" {
		return nil, ErrNotConfigured
	}
	cfg = cfg.withDefaults()
	c := &Client{
		cfg:      cfg,
		http:     &http.Client{Timeout: cfg.Timeout},
		observer: NoopObserver{},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// IsConfigured reports whether the client has a non-empty BaseURL. Always
// true for a Client returned by NewClient (NewClient rejects empty BaseURL),
// and always false for a nil *Client.
func (c *Client) IsConfigured() bool {
	return c != nil && c.cfg.BaseURL != ""
}

// BaseURL returns the configured gateway base URL.
func (c *Client) BaseURL() string {
	if c == nil {
		return ""
	}
	return c.cfg.BaseURL
}

// DefaultModel returns the model used when a request omits the Model field.
func (c *Client) DefaultModel() string {
	if c == nil {
		return ""
	}
	return c.cfg.DefaultModel
}
