package webhook

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// WebhookStore defines database operations for webhooks.
type WebhookStore interface {
	CreateWebhook(ctx context.Context, companyID uuid.UUID, url, secret string, events []string) (Webhook, error)
	GetWebhook(ctx context.Context, id uuid.UUID) (Webhook, error)
	ListWebhooksByCompany(ctx context.Context, companyID uuid.UUID) ([]Webhook, error)
	UpdateWebhookStatus(ctx context.Context, id uuid.UUID, active bool) error
	DeleteWebhook(ctx context.Context, id uuid.UUID) error
	IncrementFailureCount(ctx context.Context, id uuid.UUID) (int, error)
	ResetFailureCount(ctx context.Context, id uuid.UUID) error

	CreateDelivery(ctx context.Context, webhookID uuid.UUID, eventType, payload string) (WebhookDelivery, error)
	UpdateDelivery(ctx context.Context, id uuid.UUID, status string, statusCode int, responseBody string, nextRetryAt *time.Time) error
	GetPendingDeliveries(ctx context.Context) ([]WebhookDelivery, error)

	// GetDelivery looks up a single delivery by ID. Used by ExecuteDelivery.
	GetDelivery(ctx context.Context, id uuid.UUID) (WebhookDelivery, error)

	// ListFailedDeliveriesForRetry returns deliveries with status="failed"
	// and Attempts < maxAttempts. Used by RetryFailedDeliveries.
	ListFailedDeliveriesForRetry(ctx context.Context, maxAttempts int) ([]WebhookDelivery, error)
}

// Webhook represents a webhook subscription.
type Webhook struct {
	ID               uuid.UUID
	CompanyID        uuid.UUID
	URL              string
	Secret           string
	Events           []string
	Active           bool
	ConsecutiveFails int
	CreatedAt        time.Time
}

// WebhookDelivery represents a delivery attempt.
type WebhookDelivery struct {
	ID           uuid.UUID
	WebhookID    uuid.UUID
	EventType    string
	Payload      string
	Status       string // "pending", "success", "failed", "exhausted"
	StatusCode   int
	ResponseBody string
	Attempts     int
	NextRetryAt  *time.Time
	CreatedAt    time.Time
}
