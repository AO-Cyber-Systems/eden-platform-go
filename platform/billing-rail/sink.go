package billingrail

import "context"

// EdenBizSink is the destination contract every rail event flows into.
// Eden-Biz is the financial system of record (decision D1) — the rail does
// not maintain its own ledger. This interface is what the rail calls (or
// what the dispatcher calls on behalf of the rail) to update Eden-Biz.
//
// Implementations live in eden-biz-dev; this package defines the contract
// and ships a MockSink for tests.
//
// All methods MUST be idempotent on the (rail, event id) pair: the dispatcher
// will retry on transient errors, and rails MAY redeliver webhooks. Use the
// rail event id (WebhookEvent.RailEventID) as the dedup key.
type EdenBizSink interface {
	// RecordCharge persists a charge attempt's outcome. Called by the
	// adapter directly when the consumer drives the charge from Eden-Biz,
	// and/or by the dispatcher in response to a charge.* webhook.
	RecordCharge(ctx context.Context, customer Customer, amount Money, result ChargeResult) error

	// RecordRefund persists a refund's outcome.
	RecordRefund(ctx context.Context, customer Customer, amount Money, result RefundResult) error

	// RecordSubscriptionEvent persists a subscription lifecycle change. The
	// event carries the rail-side state (status, period end) for Eden-Biz
	// to reconcile against its plan record.
	RecordSubscriptionEvent(ctx context.Context, customer Customer, event WebhookEvent) error
}
