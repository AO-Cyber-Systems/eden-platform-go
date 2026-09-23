package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// DefaultCloudflareBaseURL is the Cloudflare API v4 base. Overridable via
// CloudflareConfig.BaseURL (tests point it at a stub server).
const DefaultCloudflareBaseURL = "https://api.cloudflare.com/client/v4"

// CloudflareConfig configures the Cloudflare Email Sending REST transport.
//
// This transport exists because DigitalOcean (and most clouds) block outbound
// SMTP (ports 25/465/587) at the network layer, so the SMTP sender cannot reach
// smtp.mx.cloudflare.net from DOKS. The REST API is plain HTTPS (443), which
// egress allows, and reaches the same Cloudflare Email Sending pipeline (DKIM/ARC
// signing, shared logs) as SMTP.
type CloudflareConfig struct {
	AccountID string // Cloudflare account ID (path param; non-secret)
	APIToken  string // Cloudflare API token with "Email Sending: Edit" (secret)
	// BaseURL overrides DefaultCloudflareBaseURL (test seam).
	BaseURL string
	// HTTPClient overrides the default 15s-timeout client (test seam).
	HTTPClient *http.Client
	// MaxAttempts bounds total send attempts (not retries). Zero means
	// DefaultCloudflareMaxAttempts; 1 disables retrying entirely.
	MaxAttempts int
	// RetryBaseDelay is the first backoff interval, doubled per attempt.
	// Zero means DefaultCloudflareRetryBaseDelay. A Retry-After header on the
	// response always wins over the computed backoff.
	RetryBaseDelay time.Duration
}

// Retry defaults for throttled/transient Cloudflare responses.
//
// Cloudflare documents 429 / code 10004 (email.sending.error.throttled) and the
// 5xx codes as RETRYABLE — back off and re-send rather than dropping the
// message. Before this existed, a single 429 surfaced as a permanent send
// failure: the caller logged "outbound email was NOT sent" and the user simply
// never received their mail. That is how AODex signups silently stopped getting
// activation email (aodex#616) while the account was over its shared,
// account-wide Email Sending quota.
const (
	DefaultCloudflareMaxAttempts    = 4
	DefaultCloudflareRetryBaseDelay = 500 * time.Millisecond
)

// retryableCFCodes are Cloudflare error codes worth re-sending on. Everything
// else in the 101xx/102xx ranges is a permanent request/auth/content fault and
// retrying it only burns quota that is already scarce.
var retryableCFCodes = map[int]bool{
	10004: true, // throttled        (429)
	10002: true, // internal_server  (500)
	10003: true, // transient upstream
}

type cloudflareSender struct {
	cfg    CloudflareConfig
	client *http.Client
	base   string
}

// NewCloudflareAPI constructs a Sender that delivers via the Cloudflare Email
// Sending REST API (HTTPS). Use this instead of NewSMTP wherever outbound SMTP
// is blocked (e.g. DigitalOcean-hosted clusters).
func NewCloudflareAPI(cfg CloudflareConfig) Sender {
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	base := cfg.BaseURL
	if base == "" {
		base = DefaultCloudflareBaseURL
	}
	return &cloudflareSender{cfg: cfg, client: client, base: base}
}

// cfSendRequest is the REST body: POST /accounts/{id}/email/sending/send.
// The API takes flat fields with a single `to` (platform lifecycle mail is
// single-recipient); cc/bcc collapse into additional recipients here.
type cfSendRequest struct {
	From    string            `json:"from"`
	To      string            `json:"to"`
	Subject string            `json:"subject"`
	Text    string            `json:"text,omitempty"`
	HTML    string            `json:"html,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

type cfSendResponse struct {
	Success bool `json:"success"`
	Errors  []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
	Result struct {
		MessageID        string   `json:"message_id"`
		Delivered        []string `json:"delivered"`
		Queued           []string `json:"queued"`
		PermanentBounces []string `json:"permanent_bounces"`
	} `json:"result"`
}

func (s *cloudflareSender) Send(ctx context.Context, msg Message) (SendResult, error) {
	if err := validate(msg); err != nil {
		return SendResult{}, err
	}
	if s.cfg.AccountID == "" || s.cfg.APIToken == "" {
		return SendResult{}, fmt.Errorf("email: cloudflare transport misconfigured (account id / token empty)")
	}

	body := cfSendRequest{
		From:    msg.From.String(),
		To:      msg.To[0].Email,
		Subject: msg.Subject,
		Text:    msg.TextBody,
		HTML:    msg.HTMLBody,
		Headers: msg.Headers,
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return SendResult{}, fmt.Errorf("email: cloudflare marshal: %w", err)
	}

	attempts := s.cfg.MaxAttempts
	if attempts <= 0 {
		attempts = DefaultCloudflareMaxAttempts
	}
	delay := s.cfg.RetryBaseDelay
	if delay <= 0 {
		delay = DefaultCloudflareRetryBaseDelay
	}

	var lastErr error
	for attempt := 1; ; attempt++ {
		res, retryAfter, retryable, err := s.attempt(ctx, buf)
		if err == nil {
			return res, nil
		}
		lastErr = err

		// Permanent fault, or attempts exhausted: surface it unchanged so the
		// caller's existing error handling and log lines keep working.
		if !retryable || attempt >= attempts {
			if retryable {
				return SendResult{}, fmt.Errorf("%w (gave up after %d attempts)", err, attempt)
			}
			return SendResult{}, err
		}

		// Cloudflare's Retry-After is authoritative when present — guessing a
		// shorter interval just spends quota that is already exhausted.
		wait := delay
		if retryAfter > 0 {
			wait = retryAfter
		}

		// Never outlive the caller's deadline: a signup request blocking on
		// email retries is a worse failure than a late email.
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return SendResult{}, fmt.Errorf("%w (retry abandoned: %v)", lastErr, ctx.Err())
		case <-timer.C:
		}
		delay *= 2
	}
}

// attempt performs one send. It reports whether the failure is worth retrying
// and any server-supplied Retry-After delay.
func (s *cloudflareSender) attempt(ctx context.Context, buf []byte) (SendResult, time.Duration, bool, error) {
	url := fmt.Sprintf("%s/accounts/%s/email/sending/send", s.base, s.cfg.AccountID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return SendResult{}, 0, false, fmt.Errorf("email: cloudflare request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.cfg.APIToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		// Transport-level failures (dial, TLS, timeout) are transient by nature.
		return SendResult{}, 0, true, fmt.Errorf("email: cloudflare send: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	var cr cfSendResponse
	_ = json.Unmarshal(raw, &cr)
	if resp.StatusCode != http.StatusOK || !cr.Success {
		retryable := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		for _, e := range cr.Errors {
			if retryableCFCodes[e.Code] {
				retryable = true
			}
		}
		return SendResult{}, parseRetryAfter(resp.Header.Get("Retry-After")), retryable,
			fmt.Errorf("email: cloudflare send failed (http %d): %s", resp.StatusCode, cloudflareErr(cr, raw))
	}
	return SendResult{MessageID: cr.Result.MessageID, AcceptedAt: time.Now().UTC()}, 0, false, nil
}

// parseRetryAfter reads the delay-seconds form of Retry-After. The HTTP-date
// form is ignored rather than mis-parsed — falling back to computed backoff is
// safe, guessing a date is not.
func parseRetryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	secs, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || secs < 0 {
		return 0
	}
	if secs > 60 {
		secs = 60 // don't stall a request thread on a long server-side hint
	}
	return time.Duration(secs) * time.Second
}

// cloudflareErr renders the API error list, falling back to the raw body.
func cloudflareErr(cr cfSendResponse, raw []byte) string {
	if len(cr.Errors) > 0 {
		parts := make([]string, 0, len(cr.Errors))
		for _, e := range cr.Errors {
			parts = append(parts, fmt.Sprintf("%d %s", e.Code, e.Message))
		}
		b, _ := json.Marshal(parts)
		return string(b)
	}
	if len(raw) > 0 {
		if len(raw) > 300 {
			raw = raw[:300]
		}
		return string(raw)
	}
	return "no response body"
}
