package entitlements

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultCacheTTL = 5 * time.Minute

// ClientOption configures an EntitlementClient.
type ClientOption func(*EntitlementClient)

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(c *http.Client) ClientOption {
	return func(ec *EntitlementClient) { ec.httpClient = c }
}

// WithCacheTTL sets the cache time-to-live. Default is 5 minutes.
func WithCacheTTL(d time.Duration) ClientOption {
	return func(ec *EntitlementClient) { ec.cacheTTL = d }
}

// WithServiceToken sets a Bearer token for service-to-service auth.
func WithServiceToken(token string) ClientOption {
	return func(ec *EntitlementClient) { ec.serviceToken = token }
}

// EntitlementClient is an HTTP client for Eden Biz's entitlements API.
// It caches bootstrap responses and entitlement check results to avoid
// per-request HTTP calls.
type EntitlementClient struct {
	baseURL      string
	httpClient   *http.Client
	serviceToken string
	cacheTTL     time.Duration

	bootstrapCache *Cache[string, *BootstrapResponse]
	checkCache     *Cache[string, *EntitlementResult]
}

// NewClient creates a new entitlement client pointing at the given Eden Biz base URL.
func NewClient(edenBizBaseURL string, opts ...ClientOption) *EntitlementClient {
	c := &EntitlementClient{
		baseURL:    strings.TrimRight(edenBizBaseURL, "/"),
		httpClient: &http.Client{Timeout: 10 * time.Second},
		cacheTTL:   defaultCacheTTL,
	}
	for _, opt := range opts {
		opt(c)
	}
	c.bootstrapCache = NewCache[string, *BootstrapResponse](c.cacheTTL)
	c.checkCache = NewCache[string, *EntitlementResult](c.cacheTTL)
	return c
}

// Bootstrap fetches the full entitlements state for a company. Results are cached.
func (c *EntitlementClient) Bootstrap(ctx context.Context, companyID string) (*BootstrapResponse, error) {
	if resp, ok := c.bootstrapCache.Get(companyID); ok {
		return resp, nil
	}

	u := c.baseURL + "/api/v1/entitlements/bootstrap?company_id=" + url.QueryEscape(companyID)
	resp, err := c.doGet(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("entitlements bootstrap: %w", err)
	}

	var result BootstrapResponse
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("entitlements bootstrap: parse: %w", err)
	}

	c.bootstrapCache.Set(companyID, &result)
	return &result, nil
}

// CheckEntitlement checks whether a specific feature is allowed for a subscription.
// Results are cached by subscriptionID:featureKey.
func (c *EntitlementClient) CheckEntitlement(ctx context.Context, subscriptionID, featureKey string) (*EntitlementResult, error) {
	cacheKey := subscriptionID + ":" + featureKey
	if result, ok := c.checkCache.Get(cacheKey); ok {
		return result, nil
	}

	u := c.baseURL + "/entitlements/check?subscription_id=" + url.QueryEscape(subscriptionID) + "&feature=" + url.QueryEscape(featureKey)
	resp, err := c.doGet(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("entitlements check: %w", err)
	}

	var result EntitlementResult
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("entitlements check: parse: %w", err)
	}

	c.checkCache.Set(cacheKey, &result)
	return &result, nil
}

// CanUseFeature is a convenience method that checks the bootstrap cache first,
// then falls back to a direct entitlement check. Returns false on any error.
func (c *EntitlementClient) CanUseFeature(ctx context.Context, companyID, featureKey string) (bool, error) {
	bootstrap, err := c.Bootstrap(ctx, companyID)
	if err != nil {
		return false, err
	}

	for _, e := range bootstrap.Entitlements {
		if e.FeatureKey == featureKey {
			return e.Allowed, nil
		}
	}

	// Feature not defined in plan → deny by default
	return false, nil
}

// RecordUsage records usage for a quota feature. Invalidates the cache for that subscription.
func (c *EntitlementClient) RecordUsage(ctx context.Context, subscriptionID, featureKey string, quantity int64) error {
	body, err := json.Marshal(map[string]any{
		"subscription_id": subscriptionID,
		"feature_key":     featureKey,
		"quantity":        quantity,
	})
	if err != nil {
		return fmt.Errorf("entitlements record usage: marshal: %w", err)
	}

	u := c.baseURL + "/entitlements/usage"
	req, err := http.NewRequestWithContext(ctx, "POST", u, strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("entitlements record usage: request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("entitlements record usage: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		return fmt.Errorf("entitlements record usage: status %d: %s", resp.StatusCode, string(respBody))
	}

	// Invalidate caches for this subscription
	c.checkCache.Invalidate(subscriptionID + ":" + featureKey)
	return nil
}

// InvalidateCompany clears all cached data for a company.
// Call this when a subscription or plan changes (e.g., from a webhook handler).
func (c *EntitlementClient) InvalidateCompany(companyID string) {
	c.bootstrapCache.Invalidate(companyID)
	// Note: checkCache uses subscriptionID keys, not companyID.
	// For a full invalidation, clear the entire check cache.
	c.checkCache.Clear()
}

func (c *EntitlementClient) doGet(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return nil, err
	}
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

func (c *EntitlementClient) setAuth(req *http.Request) {
	if c.serviceToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.serviceToken)
	}
}
