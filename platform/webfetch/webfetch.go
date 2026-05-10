// Package webfetch is a hardened outbound HTTP client with safe-by-default
// policy controls. Donor: aosentry/internal/webfetch (URLValidator + Fetcher).
//
// Use this everywhere a product fetches user-supplied URLs. It blocks
// SSRF vectors (private IPs, link-local, loopback) by default, caps response
// sizes, restricts redirects, and exposes per-request policy overrides.
//
// See TRD 20-02.
package webfetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Defaults
const (
	DefaultMaxResponseBytes int64         = 10 * 1024 * 1024
	DefaultMaxRedirects     int           = 5
	DefaultConnectTimeout   time.Duration = 10 * time.Second
	DefaultReadTimeout      time.Duration = 60 * time.Second
	DefaultUserAgent                      = "aocyber-webfetch/1.0"
)

// Errors
var (
	ErrPolicyViolation = errors.New("webfetch: policy violation")
	ErrTooManyRedirects = errors.New("webfetch: too many redirects")
	ErrResponseTooLarge = errors.New("webfetch: response exceeded MaxResponseBytes")
)

// Policy configures the safety controls.
type Policy struct {
	AllowedSchemes         []string  // default: http, https
	DenyPrivateIPs         bool      // default: true (set false for trusted intranet)
	DenyHostsRegexp        []string  // additional host patterns to block
	AllowHostsRegexp       []string  // explicit allowlist (overrides DenyPrivateIPs)
	MaxResponseBytes       int64
	MaxRedirects           int
	AllowedRedirectSchemes []string
	ConnectTimeout         time.Duration
	ReadTimeout            time.Duration
	UserAgent              string
	AdditionalHeaders      map[string]string
}

// Result is a fetched HTTP response.
type Result struct {
	URL         string
	StatusCode  int
	ContentType string
	Body        []byte
	BytesRead   int64
	Headers     http.Header
	Truncated   bool
	Redirects   []string
}

// Client is the hardened HTTP client.
type Client struct {
	policy        Policy
	httpClient    *http.Client
	denyHosts     []*regexp.Regexp
	allowHosts    []*regexp.Regexp
}

// SafeDefault returns a Policy with safe defaults for fetching user-supplied URLs.
func SafeDefault() Policy {
	return Policy{
		AllowedSchemes:         []string{"http", "https"},
		DenyPrivateIPs:         true,
		MaxResponseBytes:       DefaultMaxResponseBytes,
		MaxRedirects:           DefaultMaxRedirects,
		AllowedRedirectSchemes: []string{"http", "https"},
		ConnectTimeout:         DefaultConnectTimeout,
		ReadTimeout:            DefaultReadTimeout,
		UserAgent:              DefaultUserAgent,
	}
}

// NewClient constructs a Client. Pass SafeDefault() if you want sane
// defaults; only override fields where the caller has a reason.
func NewClient(p Policy) (*Client, error) {
	p = applyDefaults(p)

	denyHosts, err := compileRegexps(p.DenyHostsRegexp)
	if err != nil {
		return nil, fmt.Errorf("webfetch: deny regexps: %w", err)
	}
	allowHosts, err := compileRegexps(p.AllowHostsRegexp)
	if err != nil {
		return nil, fmt.Errorf("webfetch: allow regexps: %w", err)
	}

	dialer := &net.Dialer{Timeout: p.ConnectTimeout}
	transport := &http.Transport{
		DialContext:           dialer.DialContext,
		ResponseHeaderTimeout: p.ReadTimeout,
		ExpectContinueTimeout: time.Second,
		TLSHandshakeTimeout:   p.ConnectTimeout,
	}

	c := &Client{
		policy:     p,
		denyHosts:  denyHosts,
		allowHosts: allowHosts,
	}

	httpClient := &http.Client{
		Transport: transport,
		Timeout:   p.ReadTimeout + p.ConnectTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= p.MaxRedirects {
				return ErrTooManyRedirects
			}
			if !c.schemeAllowed(req.URL.Scheme, p.AllowedRedirectSchemes) {
				return fmt.Errorf("%w: redirect scheme %q not allowed", ErrPolicyViolation, req.URL.Scheme)
			}
			if err := c.validateURL(req.URL); err != nil {
				return err
			}
			return nil
		},
	}
	c.httpClient = httpClient

	return c, nil
}

func applyDefaults(p Policy) Policy {
	if len(p.AllowedSchemes) == 0 {
		p.AllowedSchemes = []string{"http", "https"}
	}
	if p.MaxResponseBytes <= 0 {
		p.MaxResponseBytes = DefaultMaxResponseBytes
	}
	if p.MaxRedirects <= 0 {
		p.MaxRedirects = DefaultMaxRedirects
	}
	if len(p.AllowedRedirectSchemes) == 0 {
		p.AllowedRedirectSchemes = p.AllowedSchemes
	}
	if p.ConnectTimeout <= 0 {
		p.ConnectTimeout = DefaultConnectTimeout
	}
	if p.ReadTimeout <= 0 {
		p.ReadTimeout = DefaultReadTimeout
	}
	if p.UserAgent == "" {
		p.UserAgent = DefaultUserAgent
	}
	return p
}

// Fetch fetches the URL with the configured policy.
func (c *Client) Fetch(ctx context.Context, target string) (*Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("webfetch: new request: %w", err)
	}
	return c.FetchWith(ctx, req)
}

// FetchWith fetches with a caller-prepared http.Request (use to set custom
// methods, bodies, headers).
func (c *Client) FetchWith(ctx context.Context, req *http.Request) (*Result, error) {
	if err := c.validateURL(req.URL); err != nil {
		return nil, err
	}
	if !c.schemeAllowed(req.URL.Scheme, c.policy.AllowedSchemes) {
		return nil, fmt.Errorf("%w: scheme %q not allowed", ErrPolicyViolation, req.URL.Scheme)
	}

	if req.Header == nil {
		req.Header = make(http.Header)
	}
	req.Header.Set("User-Agent", c.policy.UserAgent)
	for k, v := range c.policy.AdditionalHeaders {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Surface our policy errors verbatim (CheckRedirect may have set them).
		if errors.Is(err, ErrTooManyRedirects) || errors.Is(err, ErrPolicyViolation) {
			return nil, err
		}
		// Unwrap wrapped policy errors from the http stdlib redirect path.
		if uerr, ok := err.(*url.Error); ok {
			if errors.Is(uerr.Err, ErrTooManyRedirects) || errors.Is(uerr.Err, ErrPolicyViolation) {
				return nil, uerr.Err
			}
		}
		return nil, fmt.Errorf("webfetch: request: %w", err)
	}
	defer resp.Body.Close()

	limited := io.LimitReader(resp.Body, c.policy.MaxResponseBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("webfetch: read body: %w", err)
	}

	truncated := int64(len(body)) > c.policy.MaxResponseBytes
	if truncated {
		body = body[:c.policy.MaxResponseBytes]
	}

	res := &Result{
		URL:         resp.Request.URL.String(),
		StatusCode:  resp.StatusCode,
		ContentType: resp.Header.Get("Content-Type"),
		Body:        body,
		BytesRead:   int64(len(body)),
		Headers:     resp.Header,
		Truncated:   truncated,
	}

	if truncated {
		// Note: truncation isn't a hard error — caller can decide. We expose
		// the flag so policy-strict consumers can wrap it as ErrResponseTooLarge.
		_ = ErrResponseTooLarge // referenced for godoc
	}

	return res, nil
}

// validateURL applies SSRF / scheme / host policy.
func (c *Client) validateURL(u *url.URL) error {
	if u == nil {
		return fmt.Errorf("%w: empty URL", ErrPolicyViolation)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("%w: no host in URL", ErrPolicyViolation)
	}

	// Allowlist short-circuits everything else.
	if matchAny(c.allowHosts, host) {
		return nil
	}

	// Deny patterns.
	if matchAny(c.denyHosts, host) {
		return fmt.Errorf("%w: host %q matches deny pattern", ErrPolicyViolation, host)
	}

	if c.policy.DenyPrivateIPs {
		if err := denyPrivateIP(host); err != nil {
			return err
		}
	}
	return nil
}

// schemeAllowed returns true if the scheme is in the allow list.
func (c *Client) schemeAllowed(scheme string, allowed []string) bool {
	for _, a := range allowed {
		if strings.EqualFold(a, scheme) {
			return true
		}
	}
	return false
}

// denyPrivateIP rejects loopback, RFC 1918, link-local (incl. cloud
// metadata 169.254.169.254), unspecified, multicast.
func denyPrivateIP(host string) error {
	// Direct-IP path: parse, then check.
	if ip := net.ParseIP(host); ip != nil {
		return checkIPDeny(ip, host)
	}

	// Hostname path: catch the well-known unsafe hostnames before resolution.
	hostname := strings.ToLower(host)
	for _, blocked := range []string{
		"localhost",
		"localhost.localdomain",
		"metadata.google.internal",
	} {
		if hostname == blocked {
			return fmt.Errorf("%w: host %q is blocked", ErrPolicyViolation, host)
		}
	}

	// Resolve and check every IP. Failure to resolve is treated as a
	// soft-allow — the underlying request will fail anyway.
	addrs, err := net.LookupIP(host)
	if err != nil {
		return nil
	}
	for _, ip := range addrs {
		if ipErr := checkIPDeny(ip, host); ipErr != nil {
			return ipErr
		}
	}
	return nil
}

func checkIPDeny(ip net.IP, host string) error {
	switch {
	case ip.IsLoopback():
		return fmt.Errorf("%w: loopback IP %s", ErrPolicyViolation, host)
	case ip.IsPrivate():
		return fmt.Errorf("%w: private IP %s (RFC 1918)", ErrPolicyViolation, host)
	case ip.IsLinkLocalUnicast():
		return fmt.Errorf("%w: link-local IP %s", ErrPolicyViolation, host)
	case ip.IsUnspecified():
		return fmt.Errorf("%w: unspecified IP %s", ErrPolicyViolation, host)
	case ip.IsMulticast():
		return fmt.Errorf("%w: multicast IP %s", ErrPolicyViolation, host)
	}
	return nil
}

func compileRegexps(patterns []string) ([]*regexp.Regexp, error) {
	out := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("compile %q: %w", p, err)
		}
		out = append(out, re)
	}
	return out, nil
}

func matchAny(rxs []*regexp.Regexp, s string) bool {
	for _, rx := range rxs {
		if rx.MatchString(s) {
			return true
		}
	}
	return false
}
