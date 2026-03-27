// Package entitlements provides a client for checking subscription entitlements
// and feature flags from the Eden Biz platform. It pre-fetches entitlement state
// per request and injects it into the request context via middleware.
package entitlements

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// Entitlements holds the resolved entitlement state for a company/user.
type Entitlements struct {
	CompanyID   string            `json:"company_id"`
	Tier        string            `json:"tier"`         // "free", "starter", "pro", "enterprise"
	Features    map[string]bool   `json:"features"`     // feature flag name -> enabled
	Limits      map[string]int64  `json:"limits"`       // limit name -> value
	ExpiresAt   *time.Time        `json:"expires_at"`
	FetchedAt   time.Time         `json:"fetched_at"`
}

// HasFeature returns true if the given feature is enabled.
func (e *Entitlements) HasFeature(name string) bool {
	if e == nil || e.Features == nil {
		return false
	}
	return e.Features[name]
}

// GetLimit returns the limit value, or the default if not set.
func (e *Entitlements) GetLimit(name string, defaultVal int64) int64 {
	if e == nil || e.Limits == nil {
		return defaultVal
	}
	if v, ok := e.Limits[name]; ok {
		return v
	}
	return defaultVal
}

// contextKey is unexported to prevent collisions.
type contextKey struct{}

// FromContext retrieves Entitlements from the request context.
// Returns nil if not present (middleware not applied or fetch failed).
func FromContext(ctx context.Context) *Entitlements {
	e, _ := ctx.Value(contextKey{}).(*Entitlements)
	return e
}

// Client fetches and caches entitlement state from Eden Biz API.
type Client struct {
	apiURL       string
	serviceToken string
	httpClient   *http.Client
	cacheTTL     time.Duration

	mu    sync.RWMutex
	cache map[string]*cacheEntry
}

type cacheEntry struct {
	entitlements *Entitlements
	expiresAt    time.Time
}

// Option configures the entitlements Client.
type Option func(*Client)

// WithServiceToken sets the service-to-service auth token.
func WithServiceToken(token string) Option {
	return func(c *Client) {
		c.serviceToken = token
	}
}

// WithCacheTTL sets how long entitlement lookups are cached.
func WithCacheTTL(ttl time.Duration) Option {
	return func(c *Client) {
		c.cacheTTL = ttl
	}
}

// NewClient creates an entitlements client.
func NewClient(apiURL string, opts ...Option) *Client {
	c := &Client{
		apiURL:   apiURL,
		cacheTTL: 5 * time.Minute,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
		cache: make(map[string]*cacheEntry),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// GetEntitlements fetches entitlements for a company, using cache if fresh.
func (c *Client) GetEntitlements(ctx context.Context, companyID string) (*Entitlements, error) {
	if companyID == "" {
		return &Entitlements{
			Tier:      "free",
			Features:  map[string]bool{},
			Limits:    map[string]int64{},
			FetchedAt: time.Now(),
		}, nil
	}

	// Check cache
	c.mu.RLock()
	if entry, ok := c.cache[companyID]; ok && time.Now().Before(entry.expiresAt) {
		c.mu.RUnlock()
		return entry.entitlements, nil
	}
	c.mu.RUnlock()

	// Fetch from API
	ent, err := c.fetchFromAPI(ctx, companyID)
	if err != nil {
		slog.WarnContext(ctx, "[Entitlements] fetch failed, using defaults",
			"company_id", companyID, "error", err)
		// Fail open with defaults
		ent = &Entitlements{
			CompanyID: companyID,
			Tier:      "free",
			Features:  map[string]bool{},
			Limits:    map[string]int64{},
			FetchedAt: time.Now(),
		}
	}

	// Cache result
	c.mu.Lock()
	c.cache[companyID] = &cacheEntry{
		entitlements: ent,
		expiresAt:    time.Now().Add(c.cacheTTL),
	}
	c.mu.Unlock()

	return ent, nil
}

func (c *Client) fetchFromAPI(ctx context.Context, companyID string) (*Entitlements, error) {
	if c.apiURL == "" {
		// No API configured — return defaults
		return &Entitlements{
			CompanyID: companyID,
			Tier:      "pro", // Default to pro when no API configured (dev mode)
			Features:  map[string]bool{},
			Limits:    map[string]int64{},
			FetchedAt: time.Now(),
		}, nil
	}

	url := fmt.Sprintf("%s/api/v1/entitlements/%s", c.apiURL, companyID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	if c.serviceToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.serviceToken)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching entitlements: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var ent Entitlements
	if err := json.NewDecoder(resp.Body).Decode(&ent); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	ent.FetchedAt = time.Now()

	return &ent, nil
}

// InjectEntitlements returns middleware that pre-fetches entitlements and injects
// them into the request context. The companyIDFunc extracts the company ID from
// the request context (typically from the authenticated user).
//
// Fails open: if the fetch fails, the request continues with nil entitlements.
// Handlers should check FromContext() for nil and apply sensible defaults.
func InjectEntitlements(client *Client, companyIDFunc func(ctx context.Context) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			companyID := companyIDFunc(r.Context())
			if companyID == "" {
				next.ServeHTTP(w, r)
				return
			}

			ent, err := client.GetEntitlements(r.Context(), companyID)
			if err != nil {
				slog.WarnContext(r.Context(), "[Entitlements] middleware fetch failed",
					"company_id", companyID, "error", err)
				// Fail open — continue without entitlements
				next.ServeHTTP(w, r)
				return
			}

			ctx := context.WithValue(r.Context(), contextKey{}, ent)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
