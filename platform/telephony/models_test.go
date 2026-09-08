package telephony

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestConfig_SignatureToken(t *testing.T) {
	c := Config{AuthToken: "auth", WebhookSecret: ""}
	if got := c.SignatureToken(); got != "auth" {
		t.Errorf("falls back to auth: got %s", got)
	}
	c.WebhookSecret = "sig"
	if got := c.SignatureToken(); got != "sig" {
		t.Errorf("uses webhook secret: got %s", got)
	}
}

func TestReconstructURL_Direct(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/webhooks/sms/status?a=b", nil)
	r.Host = "example.com"
	if got, want := reconstructURL(r), "https://example.com/webhooks/sms/status?a=b"; got != want {
		t.Errorf("reconstructURL() = %q, want %q", got, want)
	}
}

func TestReconstructURL_HonoursForwardedHeaders(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/webhooks/sms/status", nil)
	r.Host = "internal.local:8080"
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("X-Forwarded-Host", "public.example.com")
	if got, want := reconstructURL(r), "https://public.example.com/webhooks/sms/status"; got != want {
		t.Errorf("reconstructURL() = %q, want %q", got, want)
	}
}

func TestMapStatus(t *testing.T) {
	cases := map[string]string{
		"delivered":   "delivered",
		"sent":        "sent",
		"failed":      "failed",
		"undelivered": "failed",
		"queued":      "queued",
		"sending":     "sending",
		"in-progress": "in_progress",
		"ringing":     "ringing",
		"completed":   "completed",
		"busy":        "busy",
		"no-answer":   "no_answer",
		"canceled":    "canceled",
		"bogus-value": "pending",
	}
	for in, want := range cases {
		if got := mapStatus(in); got != want {
			t.Errorf("mapStatus(%q) = %q, want %q", in, got, want)
		}
	}
}
