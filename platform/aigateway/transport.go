package aigateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

// errorBodyLimit caps the bytes read from an error response into HTTPError.
// Large error payloads are useful for debugging but unbounded reads are not.
const errorBodyLimit = 8 * 1024

// extraHeaders is a private interface implemented by request types that carry
// per-request HTTP headers (e.g. ChatRequest, GuardrailsCheckRequest). The
// headers are applied after Content-Type and Authorization on every attempt.
// Ranging over a nil map is safe (zero iterations).
type extraHeaders interface {
	extraHeaderMap() map[string]string
}

// doJSON marshals body to JSON, POSTs it to path under the configured base
// URL, retries transient failures (HTTP 429 / 5xx) up to cfg.MaxRetries with
// exponential backoff, and decodes a successful response into out (which may
// be nil to discard the body).
//
// Authentication: sets Authorization: Bearer <APIKey> when APIKey is
// non-empty. The gateway expects this header on every endpoint.
//
// Per-request headers: if body implements the extraHeaders interface, those
// headers are applied after Content-Type and Authorization on every attempt.
//
// Error mapping:
//
//   - 401 -> ErrUnauthorized (wrapped with %w; errors.Is still matches)
//   - 402 -> ErrBudgetExceeded (wrapped)
//   - other 4xx -> *HTTPError, no retry
//   - 429 / 5xx -> retried up to MaxRetries; final failure returns *HTTPError
//   - transport error -> retried; final failure returned as the underlying error
func (c *Client) doJSON(ctx context.Context, method, path string, body any, out any) error {
	var encoded []byte
	if body != nil {
		var err error
		encoded, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("aigateway: marshal request: %w", err)
		}
	}

	// Extract per-request extra headers from the body, if it carries them.
	// Types that do not implement extraHeaders contribute a nil map, which is
	// safe to range over (zero iterations — no behavior change).
	var extra map[string]string
	if h, ok := body.(extraHeaders); ok {
		extra = h.extraHeaderMap()
	}

	resp, err := c.do(ctx, method, path, "application/json", bytes.NewReader(encoded), encoded, extra)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("aigateway: decode response from %s: %w", path, err)
	}
	return nil
}

// doMultipart POSTs a multipart/form-data request built by the supplied
// builder. The builder receives a *multipart.Writer and is expected to write
// every form field / file then return without closing the writer (doMultipart
// handles closing). The successful response body is decoded into out.
func (c *Client) doMultipart(ctx context.Context, path string, build func(w *multipart.Writer) error, out any) error {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := build(mw); err != nil {
		_ = mw.Close()
		return fmt.Errorf("aigateway: build multipart body: %w", err)
	}
	if err := mw.Close(); err != nil {
		return fmt.Errorf("aigateway: close multipart writer: %w", err)
	}
	bodyBytes := buf.Bytes()
	resp, err := c.do(ctx, http.MethodPost, path, mw.FormDataContentType(), bytes.NewReader(bodyBytes), bodyBytes, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("aigateway: decode response from %s: %w", path, err)
	}
	return nil
}

// do executes the HTTP request with retry. body is the io.Reader used for
// the first attempt; bodyBytes is the buffered copy used to rebuild the body
// on each retry (ReadSeeker is awkward when the caller passed bytes.Reader).
// extra contains per-request HTTP headers applied after Content-Type and
// Authorization on every attempt; nil is safe (zero iterations).
func (c *Client) do(ctx context.Context, method, path, contentType string, _ io.Reader, bodyBytes []byte, extra map[string]string) (*http.Response, error) {
	if !c.IsConfigured() {
		return nil, ErrNotConfigured
	}
	url := c.cfg.BaseURL + path
	maxRetries := c.cfg.MaxRetries
	if maxRetries < 0 {
		maxRetries = 0
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			delay := backoffDelay(attempt)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		var reader io.Reader
		if bodyBytes != nil {
			reader = bytes.NewReader(bodyBytes)
		}
		req, err := http.NewRequestWithContext(ctx, method, url, reader)
		if err != nil {
			return nil, fmt.Errorf("aigateway: build request: %w", err)
		}
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		if c.cfg.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
		}
		// Apply per-request extra headers (e.g. X-Household-ID, X-Member-ID,
		// X-Child-Mode) AFTER Content-Type and Authorization on every attempt,
		// so retried requests still carry the X-* headers. Ranging over a nil
		// map is safe (zero iterations — backward-compatible).
		for k, v := range extra {
			req.Header.Set(k, v)
		}

		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("aigateway: %s %s: %w", method, path, err)
			continue
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return resp, nil
		}

		// Read up to errorBodyLimit for the error message; close before
		// deciding whether to retry so we don't leak connections.
		body := readLimited(resp.Body, errorBodyLimit)
		_ = resp.Body.Close()

		switch {
		case resp.StatusCode == http.StatusUnauthorized:
			return nil, fmt.Errorf("%w: %s", ErrUnauthorized, body)
		case resp.StatusCode == http.StatusPaymentRequired:
			return nil, fmt.Errorf("%w: %s", ErrBudgetExceeded, body)
		case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
			lastErr = &HTTPError{Status: resp.StatusCode, Body: body, Path: path}
			continue
		default:
			return nil, &HTTPError{Status: resp.StatusCode, Body: body, Path: path}
		}
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("aigateway: %s %s: exhausted retries", method, path)
	}
	return nil, lastErr
}

// backoffDelay returns the wait time before retry attempt n (n >= 1).
// Exponential: 500ms, 1s, 2s, 4s, ... capped at 8s.
func backoffDelay(attempt int) time.Duration {
	if attempt < 1 {
		return 0
	}
	d := 500 * time.Millisecond
	for i := 1; i < attempt; i++ {
		d *= 2
		if d > 8*time.Second {
			return 8 * time.Second
		}
	}
	return d
}

// readLimited reads up to limit bytes from r and returns them as a string.
// Errors are silently ignored — the body is purely for human-readable error
// messages.
func readLimited(r io.Reader, limit int64) string {
	if r == nil {
		return ""
	}
	b, _ := io.ReadAll(io.LimitReader(r, limit))
	return string(b)
}
