package telephony

import (
	"context"
	"net/http"
)

// Provider is the per-tenant telephony backend.
//
// Implementations MUST be effectively immutable after construction —
// credentials are passed in via the factory once. Methods are safe for
// concurrent use.
type Provider interface {
	// Type identifies the concrete backend.
	Type() ProviderType

	// SendSMS sends an outbound SMS. fromOverride may set the sending number
	// for this single send (e.g. for pool selection); pass "" to use the
	// tenant's configured SendingNumber.
	SendSMS(ctx context.Context, to, body, fromOverride string) (SMSResult, error)

	// InitiateCall places a click-to-dial outbound call. fromOverride is the
	// caller-ID DID (defaults to tenant SendingNumber when ""); statusCallbackURL
	// may be empty when the caller does not need delivery callbacks.
	InitiateCall(ctx context.Context, to, fromOverride, statusCallbackURL string) (CallResult, error)

	// InitiateBridge places a two-leg masked bridge call: the provider rings
	// toA first and, on answer, dials toB and connects the two legs. Both
	// parties see the tenant's DID (fromOverride, defaulting to the tenant
	// SendingNumber when "") as caller-ID, so neither sees the other's
	// personal number. statusCallbackURL may be empty.
	InitiateBridge(ctx context.Context, toA, toB, fromOverride, statusCallbackURL string) (CallResult, error)

	// VerifyWebhookSignature returns nil when the request is authentic.
	// bodyBytes MUST be the raw, unparsed request body.
	VerifyWebhookSignature(r *http.Request, bodyBytes []byte) error

	// ParseStatusWebhook extracts a StatusEvent from a status-callback POST.
	// Both SMS status (MessageSid/MessageStatus) and call status
	// (CallSid/CallStatus) are recognized; StatusEvent.Kind disambiguates.
	ParseStatusWebhook(r *http.Request, bodyBytes []byte) (StatusEvent, error)

	// ParseInboundSMS extracts an InboundSMS from an inbound-SMS POST.
	ParseInboundSMS(r *http.Request, bodyBytes []byte) (InboundSMS, error)
}

// NoopProvider is a fallback that returns ErrProviderNotConfigured for every
// outbound call and ErrInvalidSignature for every webhook verify. A resolver
// returns it when a tenant has no telephony configuration and no fallback is
// wired — fail loud rather than silently dropping messages.
type NoopProvider struct{}

// Type implements Provider.
func (NoopProvider) Type() ProviderType { return ProviderType("noop") }

// SendSMS implements Provider.
func (NoopProvider) SendSMS(_ context.Context, _, _, _ string) (SMSResult, error) {
	return SMSResult{}, ErrProviderNotConfigured
}

// InitiateCall implements Provider.
func (NoopProvider) InitiateCall(_ context.Context, _, _, _ string) (CallResult, error) {
	return CallResult{}, ErrProviderNotConfigured
}

// InitiateBridge implements Provider.
func (NoopProvider) InitiateBridge(_ context.Context, _, _, _, _ string) (CallResult, error) {
	return CallResult{}, ErrProviderNotConfigured
}

// VerifyWebhookSignature implements Provider.
func (NoopProvider) VerifyWebhookSignature(_ *http.Request, _ []byte) error {
	return ErrInvalidSignature
}

// ParseStatusWebhook implements Provider.
func (NoopProvider) ParseStatusWebhook(_ *http.Request, _ []byte) (StatusEvent, error) {
	return StatusEvent{}, ErrProviderNotConfigured
}

// ParseInboundSMS implements Provider.
func (NoopProvider) ParseInboundSMS(_ *http.Request, _ []byte) (InboundSMS, error) {
	return InboundSMS{}, ErrProviderNotConfigured
}
