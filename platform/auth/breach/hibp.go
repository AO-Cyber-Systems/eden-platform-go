package breach

import (
	"bufio"
	"context"
	"crypto/sha1" // intentionally — k-anonymity protocol; NOT a security boundary. See doc.go.
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

const (
	// defaultHIBPEndpoint is the canonical PwnedPasswords k-anonymity URL.
	defaultHIBPEndpoint = "https://api.pwnedpasswords.com/range/"

	// defaultHIBPTimeout caps a single HIBP HTTP round-trip.
	defaultHIBPTimeout = 5 * time.Second

	// defaultMaxRetries — total attempts = 1 initial + defaultMaxRetries retries.
	defaultMaxRetries = 3

	// defaultRateLimitRPS — HIBP's documented soft limit is ~1 RPS per client.
	// 0.6 keeps us well below their threshold and the limiter is per-instance.
	defaultRateLimitRPS = 0.6

	// defaultRetryBackoff — initial backoff between retries, doubles each attempt
	// (capped at maxRetryBackoff). Production default; tests inject a tiny value.
	defaultRetryBackoff = time.Second
	maxRetryBackoff     = 30 * time.Second
)

// HIBPScreener queries the HaveIBeenPwned PwnedPasswords k-anonymity API.
//
// Safe for concurrent use: both the embedded http.Client and rate.Limiter
// are goroutine-safe. Reuse the same instance across all callers — the
// rate limiter is per-instance.
type HIBPScreener struct {
	endpoint     string
	client       *http.Client
	limiter      *rate.Limiter
	maxRetries   int
	retryBackoff time.Duration
}

// HIBPOption configures an HIBPScreener.
type HIBPOption func(*HIBPScreener)

// WithHTTPClient overrides the default http.Client.
func WithHTTPClient(c *http.Client) HIBPOption {
	return func(s *HIBPScreener) { s.client = c }
}

// WithEndpoint overrides the HIBP base URL. Primarily for testing.
// The endpoint MUST end in a trailing slash; the 5-char hex prefix is appended directly.
func WithEndpoint(url string) HIBPOption {
	return func(s *HIBPScreener) { s.endpoint = url }
}

// WithRateLimit overrides the per-second rate limit (burst capacity = 1).
// Pass a large value (e.g. 1000) to effectively disable rate limiting in tests.
func WithRateLimit(rps float64) HIBPOption {
	return func(s *HIBPScreener) { s.limiter = rate.NewLimiter(rate.Limit(rps), 1) }
}

// WithMaxRetries overrides the retry count for transient failures.
// Total attempts = 1 + n.
func WithMaxRetries(n int) HIBPOption {
	return func(s *HIBPScreener) { s.maxRetries = n }
}

// WithRetryBackoff overrides the initial inter-retry backoff. The backoff
// doubles each attempt up to a 30-second cap. Primarily for test tuning.
func WithRetryBackoff(d time.Duration) HIBPOption {
	return func(s *HIBPScreener) { s.retryBackoff = d }
}

// NewHIBPScreener constructs an HIBPScreener with sensible defaults:
// 5-second per-request timeout, 0.6 RPS rate limit, 3 retries with
// exponential backoff capped at 30s, and the canonical pwnedpasswords.com
// endpoint.
func NewHIBPScreener(opts ...HIBPOption) *HIBPScreener {
	s := &HIBPScreener{
		endpoint:     defaultHIBPEndpoint,
		client:       &http.Client{Timeout: defaultHIBPTimeout},
		limiter:      rate.NewLimiter(rate.Limit(defaultRateLimitRPS), 1),
		maxRetries:   defaultMaxRetries,
		retryBackoff: defaultRetryBackoff,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Check implements Screener. It performs a SHA-1 of the password, sends
// only the first 5 hex chars (the "k-anonymity prefix") to HIBP, and
// scans the response for the matching 35-char suffix.
//
// Empty passwords short-circuit to (false, 0, nil) without any network
// I/O — length-floor enforcement belongs to a separate policy layer.
//
// On persistent failure after all retries, Check returns
// (false, 0, ErrScreenerUnavailable) so callers can fail-open per
// NIST 800-63B Rev 4 defense-in-depth.
func (s *HIBPScreener) Check(ctx context.Context, password string) (bool, int, error) {
	if password == "" {
		return false, 0, nil
	}

	sum := sha1.Sum([]byte(password))
	hashHex := strings.ToUpper(hex.EncodeToString(sum[:]))
	if len(hashHex) != 40 {
		return false, 0, fmt.Errorf("%w: hash length %d", ErrInvalidPrefix, len(hashHex))
	}
	prefix := hashHex[:5]
	suffix := hashHex[5:]

	// Rate-limit gate — Wait honours ctx and returns ctx.Err() on cancel.
	if err := s.limiter.Wait(ctx); err != nil {
		return false, 0, fmt.Errorf("breach: rate limit wait: %w", err)
	}

	var lastErr error
	backoff := s.retryBackoff
	if backoff <= 0 {
		backoff = defaultRetryBackoff
	}

	for attempt := 0; attempt <= s.maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return false, 0, ctx.Err()
			case <-time.After(backoff):
			}
			backoff *= 2
			if backoff > maxRetryBackoff {
				backoff = maxRetryBackoff
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.endpoint+prefix, nil)
		if err != nil {
			return false, 0, fmt.Errorf("breach: new request: %w", err)
		}
		req.Header.Set("Add-Padding", "true")
		req.Header.Set("User-Agent", "eden-platform-go/breach.HIBPScreener")

		resp, err := s.client.Do(req)
		if err != nil {
			// Context cancellation / deadline manifests here; surface immediately.
			if ctx.Err() != nil {
				return false, 0, ctx.Err()
			}
			lastErr = err
			continue
		}

		// Retryable status codes — drain + close, record, loop.
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("breach: HIBP status %d", resp.StatusCode)
			continue
		}

		// Any non-2xx other than the retryable set is a hard error.
		if resp.StatusCode != http.StatusOK {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			return false, 0, fmt.Errorf("breach: HIBP unexpected status %d", resp.StatusCode)
		}

		compromised, count, parseErr := s.scanResponse(resp, suffix)
		_ = resp.Body.Close()
		if parseErr != nil {
			return false, 0, parseErr
		}
		return compromised, count, nil
	}

	// All retries exhausted — fail-open via sentinel.
	if lastErr == nil {
		lastErr = errors.New("breach: unknown failure")
	}
	return false, 0, fmt.Errorf("%w: %w", ErrScreenerUnavailable, lastErr)
}

// scanResponse walks the suffix:count line list looking for an exact match.
// HIBP returns CRLF-delimited lines; bufio.Scanner handles both LF and CRLF
// because ScanLines strips \r.
func (s *HIBPScreener) scanResponse(resp *http.Response, suffix string) (bool, int, error) {
	scanner := bufio.NewScanner(resp.Body)
	// 35-char suffix + ':' + count + CRLF is well under 64 bytes.
	scanner.Buffer(make([]byte, 0, 256), 1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		if strings.EqualFold(parts[0], suffix) {
			count, err := strconv.Atoi(strings.TrimSpace(parts[1]))
			if err != nil {
				return false, 0, fmt.Errorf("breach: parse count for suffix %s: %w", suffix, err)
			}
			return count > 0, count, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return false, 0, fmt.Errorf("breach: scan response: %w", err)
	}
	return false, 0, nil // hash not in corpus
}
