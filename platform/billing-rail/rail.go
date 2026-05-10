package billingrail

import (
	"context"
	"errors"
	"time"
)

// Rail is the adapter interface every payment processor implements.
//
// Lifecycle responsibilities split across Rail and the consumer:
//
//   - Rail owns: rail-side API calls, signature verification, native →
//     canonical type mapping, and webhook parsing.
//   - Consumer owns: authoritative state in Eden-Biz, retries, scheduling,
//     observability, and idempotency-key minting.
//
// Methods take context.Context and SHOULD honor deadlines / cancellation.
// Errors returned by Rail are wrapped (errors.Is / errors.As compatible) and
// classified via the sentinel errors below where possible.
type Rail interface {
	// Name returns a short stable identifier ("stripe", "apple_iap",
	// "google_play", "ach"). Used for logs, metrics, and event tagging.
	Name() string

	// Charge attempts to charge the customer once. Idempotent on
	// req.IdempotencyKey: a retry with the same key MUST yield the same
	// rail-side outcome (the rail enforces this).
	Charge(ctx context.Context, req ChargeRequest) (ChargeResult, error)

	// Refund issues a refund (full or partial) against a prior charge.
	Refund(ctx context.Context, req RefundRequest) (RefundResult, error)

	// CreateSubscription creates a recurring subscription on the rail.
	CreateSubscription(ctx context.Context, req SubscriptionRequest) (SubscriptionResult, error)

	// UpdateSubscription mutates an existing subscription (plan change,
	// quantity, metadata). req.PlanID + req.Metadata describe the desired
	// post-state; the rail may interpret no-op fields as "leave alone".
	UpdateSubscription(ctx context.Context, req SubscriptionRequest) (SubscriptionResult, error)

	// CancelSubscription cancels at-period-end (the rail-default behavior).
	// Adapters MAY expose immediate cancel via a separate method later.
	CancelSubscription(ctx context.Context, railSubscriptionID string) (SubscriptionResult, error)

	// SubscriptionStatus fetches current state for reconciliation.
	SubscriptionStatus(ctx context.Context, railSubscriptionID string) (SubscriptionResult, error)

	// ParseWebhook verifies the request signature and parses the payload.
	// Returns the canonicalized event. Implementations MUST reject any
	// payload whose signature is invalid; signature failures return
	// ErrInvalidSignature.
	ParseWebhook(ctx context.Context, headers map[string]string, body []byte) (WebhookEvent, error)
}

// EventType is the canonical taxonomy for rail-emitted events. Adapters map
// rail-native event names to these constants.
type EventType string

const (
	EventChargeSucceeded EventType = "charge.succeeded"
	EventChargeFailed    EventType = "charge.failed"
	EventChargeRefunded  EventType = "charge.refunded"
	EventSubCreated      EventType = "subscription.created"
	EventSubUpdated      EventType = "subscription.updated"
	EventSubCanceled     EventType = "subscription.canceled"
	EventSubRenewed      EventType = "subscription.renewed"
	EventSubTrialEnded   EventType = "subscription.trial_ended"
)

// WebhookEvent is the parsed envelope a rail emits. Adapters fill in the
// rail-native object via RailObject for downstream inspection.
type WebhookEvent struct {
	// RailEventID is the rail-side identifier (used for dedup).
	RailEventID string

	// RailName is the Rail.Name() value that produced this event. Set by
	// the dispatcher on the way into the Sink so consumers don't need to
	// inspect the rail directly.
	RailName string

	// Type is the canonical event type.
	Type EventType

	// OccurredAt is when the event happened on the rail (UTC).
	OccurredAt time.Time

	// Customer is the rail's best-effort mapping of the event's subject to
	// an Eden-Biz Customer. Adapters SHOULD populate ID + TenantID when the
	// rail's metadata carries them; otherwise leave fields empty for the
	// consumer to enrich.
	Customer Customer

	// Amount is set for charge / refund events; zero otherwise.
	Amount Money

	// SubscriptionID is set for subscription events; empty otherwise.
	SubscriptionID string

	// ChargeID is set for charge / refund events; empty otherwise.
	ChargeID string

	// RailObject is the rail-native event body (verbatim JSON or similar).
	// Treat as opaque; consumers persist for audit.
	RailObject []byte
}

// Sentinel errors. Adapters wrap these via fmt.Errorf("%w", ...).
var (
	// ErrInvalidSignature is returned by ParseWebhook when signature
	// verification fails. The dispatcher converts this into a 400-class
	// HTTP response and DOES NOT call the sink.
	ErrInvalidSignature = errors.New("billingrail: invalid webhook signature")

	// ErrUnsupportedEvent is returned by ParseWebhook for events the
	// adapter chooses to ignore. The dispatcher treats it as a no-op.
	ErrUnsupportedEvent = errors.New("billingrail: unsupported webhook event")

	// ErrCharge is wrapped around rail-reported charge failures.
	ErrCharge = errors.New("billingrail: charge failed")

	// ErrRefund is wrapped around rail-reported refund failures.
	ErrRefund = errors.New("billingrail: refund failed")
)
