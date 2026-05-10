// Package billingrail defines the adapter contract for "payment received →
// notify Eden-Biz." Eden-Biz is the financial system of record (decision D1
// in PORTFOLIO_STANDARDIZATION_PLAN.md); rails (Stripe, Apple IAP, Google
// Play, ACH) shuttle events into Eden-Biz through this contract.
//
// This package defines:
//
//   - The Rail interface every adapter implements
//   - The EdenBizSink interface every consumer implements
//   - The Dispatcher that wires Rail webhook events into the Sink
//   - MockRail / MockSink for tests
//
// Concrete adapters (StripeAdapter, AppleIAPAdapter, GooglePlayAdapter)
// live in separate sub-packages and import this package.
//
// Boundary:
//
//   - billing-rail = "did this caller pay?"
//   - feature-flags (platform/feature-flags) = "is feature X turned on?"
//   - entitlements (platform/entitlements) = "did this caller pay for tier X?"
//
// A typical product gate composes all three: feature flag on AND entitlement
// satisfied AND (for first-time payments) a successful charge has landed.
package billingrail

import "time"

// Money is an ISO-4217 amount in minor units (cents, etc.). All rails normalize
// to minor units to avoid floating-point drift.
type Money struct {
	// AmountMinor is the amount in the currency's minor unit (cents for USD,
	// pence for GBP, etc.). For zero-decimal currencies (JPY, KRW), this is
	// the whole-unit amount.
	AmountMinor int64

	// Currency is the ISO-4217 alpha-3 code, lowercase preferred (e.g. "usd").
	Currency string
}

// Customer is the billable identity. The ID field is Eden-Biz's customer id
// (the system of record); rails carry it through Metadata when their native
// customer concept differs.
type Customer struct {
	// ID is the Eden-Biz customer id (UUID string). Authoritative.
	ID string

	// Email is the contact email; some rails require it on charge creation.
	Email string

	// TenantID is the Eden-Biz tenant scope. May be empty for B2C flows.
	TenantID string

	// Metadata is rail-agnostic. Adapters MAY pass it through to the rail
	// where supported (e.g. Stripe metadata, IAP custom payload).
	Metadata map[string]string
}

// ChargeRequest captures the inputs to a single charge attempt.
type ChargeRequest struct {
	Amount      Money
	Customer    Customer
	Description string

	// IdempotencyKey lets the rail dedupe a retry. Required for production
	// use; mocks accept empty values for test ergonomics.
	IdempotencyKey string

	// Metadata is rail-agnostic key/value attached to the charge.
	Metadata map[string]string
}

// ChargeResult describes the outcome of a charge attempt.
type ChargeResult struct {
	// RailChargeID is the rail-side identifier (Stripe charge id, IAP
	// receipt id, etc.). Treat as opaque.
	RailChargeID string

	// Status is the canonical lifecycle bucket.
	Status ChargeStatus

	// ProcessedAt is the rail's reported timestamp (UTC). Zero if unknown.
	ProcessedAt time.Time

	// FailureCode is rail-specific (e.g. "card_declined"). Empty on success.
	FailureCode string

	// FailureMessage is human-readable failure detail. Empty on success.
	FailureMessage string

	// RawResponse is the adapter's verbatim payload (e.g. JSON body).
	// Consumers SHOULD persist this for audit; treat as opaque.
	RawResponse []byte
}

// ChargeStatus is the canonical bucket for charge outcomes.
type ChargeStatus int

const (
	ChargeStatusUnknown ChargeStatus = iota
	ChargeStatusPending
	ChargeStatusSucceeded
	ChargeStatusFailed
)

// String returns a stable lowercase token for logs / persistence.
func (s ChargeStatus) String() string {
	switch s {
	case ChargeStatusPending:
		return "pending"
	case ChargeStatusSucceeded:
		return "succeeded"
	case ChargeStatusFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// RefundRequest captures the inputs to a refund.
type RefundRequest struct {
	// RailChargeID is the original charge being refunded.
	RailChargeID string

	// Amount is the refund amount; if zero, treated as a full refund.
	Amount Money

	// IdempotencyKey for retry safety.
	IdempotencyKey string

	// Reason is rail-agnostic ("requested_by_customer", "duplicate", etc.).
	Reason string

	// Metadata passes through to the rail when supported.
	Metadata map[string]string
}

// RefundResult describes the outcome of a refund.
type RefundResult struct {
	RailRefundID string
	Status       RefundStatus
	ProcessedAt  time.Time
	RawResponse  []byte
}

// RefundStatus is the canonical bucket for refund outcomes.
type RefundStatus int

const (
	RefundStatusUnknown RefundStatus = iota
	RefundStatusPending
	RefundStatusSucceeded
	RefundStatusFailed
)

// String returns a stable lowercase token.
func (s RefundStatus) String() string {
	switch s {
	case RefundStatusPending:
		return "pending"
	case RefundStatusSucceeded:
		return "succeeded"
	case RefundStatusFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// SubscriptionRequest captures the inputs to subscription creation or update.
type SubscriptionRequest struct {
	// PlanID is the rail-side or Eden-Biz-side plan identifier. Adapters
	// resolve as needed.
	PlanID string

	// Customer is the subscriber.
	Customer Customer

	// TrialEnd is the trial end timestamp (UTC). Nil for no trial.
	TrialEnd *time.Time

	// IdempotencyKey for retry safety.
	IdempotencyKey string

	// Metadata is rail-agnostic.
	Metadata map[string]string
}

// SubscriptionResult describes a subscription's current state.
type SubscriptionResult struct {
	RailSubscriptionID string
	Status             SubscriptionStatus
	CurrentPeriodEnd   time.Time
	RawResponse        []byte
}

// SubscriptionStatus is the canonical bucket.
type SubscriptionStatus int

const (
	SubStatusUnknown SubscriptionStatus = iota
	SubStatusActive
	SubStatusPastDue
	SubStatusCanceled
	SubStatusTrialing
	SubStatusIncomplete
)

// String returns a stable lowercase token.
func (s SubscriptionStatus) String() string {
	switch s {
	case SubStatusActive:
		return "active"
	case SubStatusPastDue:
		return "past_due"
	case SubStatusCanceled:
		return "canceled"
	case SubStatusTrialing:
		return "trialing"
	case SubStatusIncomplete:
		return "incomplete"
	default:
		return "unknown"
	}
}
