package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

	url := fmt.Sprintf("%s/accounts/%s/email/sending/send", s.base, s.cfg.AccountID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return SendResult{}, fmt.Errorf("email: cloudflare request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.cfg.APIToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return SendResult{}, fmt.Errorf("email: cloudflare send: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	var cr cfSendResponse
	_ = json.Unmarshal(raw, &cr)
	if resp.StatusCode != http.StatusOK || !cr.Success {
		return SendResult{}, fmt.Errorf("email: cloudflare send failed (http %d): %s", resp.StatusCode, cloudflareErr(cr, raw))
	}
	return SendResult{MessageID: cr.Result.MessageID, AcceptedAt: time.Now().UTC()}, nil
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
