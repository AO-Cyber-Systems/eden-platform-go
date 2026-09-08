package telephony

import (
	"errors"
	"net/http"
	"net/url"

	"github.com/google/uuid"
)

// Provider type identifiers. Persisted alongside a tenant's telephony
// configuration by later TRDs in this objective.
const (
	ProviderSignalWire ProviderType = "signalwire"
	ProviderTwilio     ProviderType = "twilio"
)

// ProviderType identifies a concrete telephony backend.
type ProviderType string

// Sentinel errors returned across the package.
var (
	// ErrInvalidSignature is returned by VerifyWebhookSignature when the
	// provider's signature header is missing or fails HMAC validation.
	// Webhook handlers map this to HTTP 403.
	ErrInvalidSignature = errors.New("telephony: invalid webhook signature")

	// ErrUnsupportedProvider is returned by Registry.For when no factory has
	// been registered for the configured ProviderType.
	ErrUnsupportedProvider = errors.New("telephony: unsupported provider")

	// ErrTenantNotConfigured is returned by a config store when the tenant
	// has no active telephony configuration.
	ErrTenantNotConfigured = errors.New("telephony: tenant has no telephony configuration")

	// ErrProviderNotConfigured is returned by an adapter constructor when
	// required credentials are missing, and by NoopProvider for every
	// outbound call — a misconfigured or unresolved tenant fails loud
	// rather than silently dropping messages.
	ErrProviderNotConfigured = errors.New("telephony: provider credentials incomplete")

	// ErrTierNotAllowed is returned by a resolver when the resolved
	// provider is gated behind a tier the tenant does not currently hold.
	// Handlers should map this to HTTP 402.
	ErrTierNotAllowed = errors.New("telephony: provider not allowed at current tier")
)

// SMSResult is returned by Provider.SendSMS.
type SMSResult struct {
	MessageSID string
	Status     string // provider-neutral; e.g. "queued", "sent", "delivered", "failed"
}

// CallResult is returned by Provider.InitiateCall and Provider.InitiateBridge.
type CallResult struct {
	CallSID string
	Status  string
}

// StatusEvent is the parsed result of a status callback (SMS or call).
type StatusEvent struct {
	Kind      string // "sms" | "call"
	SID       string
	Status    string
	ErrorCode string
	From      string
	To        string
	DurationS int // call only; 0 for SMS
}

// InboundSMS is the parsed inbound SMS payload (provider-agnostic).
type InboundSMS struct {
	MessageSID string
	From       string
	To         string
	Body       string
	Raw        []byte
}

// Config holds the resolved per-tenant telephony configuration after
// credentials have been decrypted. Producers decrypt; provider factories
// read plaintext.
type Config struct {
	CompanyID     uuid.UUID
	Provider      ProviderType
	AccountSID    string
	AuthToken     string // plaintext after decryption
	WebhookSecret string // plaintext; empty means "use AuthToken for signature verification"
	SpaceURL      string // SignalWire only (e.g. https://example.signalwire.com)
	SendingNumber string
	IsActive      bool
}

// SignatureToken returns the secret used for HMAC signature verification.
// Falls back to AuthToken when WebhookSecret is empty — both Twilio and
// SignalWire treat the auth token as the signing secret by default.
func (c Config) SignatureToken() string {
	if c.WebhookSecret != "" {
		return c.WebhookSecret
	}
	return c.AuthToken
}

// reconstructURL builds the full external URL the provider used when signing
// the request. Edge-aware: honours X-Forwarded-Proto and X-Forwarded-Host so
// signature verification still passes when the request crosses a reverse
// proxy or CDN edge.
//
// Shared by every adapter in this package.
func reconstructURL(r *http.Request) string {
	scheme := "https"
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}
	host := r.Host
	if fwdHost := r.Header.Get("X-Forwarded-Host"); fwdHost != "" {
		host = fwdHost
	}
	u := &url.URL{
		Scheme:   scheme,
		Host:     host,
		Path:     r.URL.Path,
		RawPath:  r.URL.RawPath,
		RawQuery: r.URL.RawQuery,
	}
	return u.String()
}

// mapStatus maps Twilio/SignalWire MessageStatus or CallStatus values to a
// stable, provider-neutral status string. Both providers share the value set
// (SignalWire ships a Twilio-compatible API).
func mapStatus(s string) string {
	switch s {
	case "delivered":
		return "delivered"
	case "sent":
		return "sent"
	case "failed", "undelivered":
		return "failed"
	case "queued":
		return "queued"
	case "sending":
		return "sending"
	case "in-progress":
		return "in_progress"
	case "ringing":
		return "ringing"
	case "completed":
		return "completed"
	case "busy":
		return "busy"
	case "no-answer":
		return "no_answer"
	case "canceled":
		return "canceled"
	default:
		return "pending"
	}
}
