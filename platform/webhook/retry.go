package webhook

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
)

// RetryFailedDeliveries finds deliveries with status="failed" whose webhook
// is still active and re-enqueues them for delivery via the standard
// deliver() path. Designed to be called from a cron / River job at a cadence
// of your choosing (typical: every minute).
//
// Returns the number of deliveries re-enqueued. Inactive webhooks (e.g.
// auto-paused after consecutive failures) are skipped silently — restart
// them from your admin surface and they will be picked up on the next run.
//
// Donor: eden-biz/internal/webhooks.RetryFailedDeliveries.
func (s *Service) RetryFailedDeliveries(ctx context.Context) (int, error) {
	deliveries, err := s.store.ListFailedDeliveriesForRetry(ctx, s.maxRetries)
	if err != nil {
		return 0, fmt.Errorf("list failed deliveries: %w", err)
	}

	retried := 0
	for _, d := range deliveries {
		hook, err := s.store.GetWebhook(ctx, d.WebhookID)
		if err != nil {
			slog.WarnContext(ctx, "webhook retry: get webhook failed",
				"delivery_id", d.ID, "webhook_id", d.WebhookID, "error", err)
			continue
		}
		if !hook.Active {
			continue
		}
		s.wg.Add(1)
		go func(wh Webhook, del WebhookDelivery) {
			defer s.wg.Done()
			s.deliver(context.Background(), wh, del)
		}(hook, d)
		retried++
	}

	if retried > 0 {
		slog.InfoContext(ctx, "webhook retry: re-enqueued failed deliveries", "count", retried)
	}
	return retried, nil
}

// ExecuteDelivery performs a single delivery by ID synchronously (the deliver
// loop still runs on a goroutine for I/O isolation, but ExecuteDelivery
// blocks until that goroutine exits via Shutdown semantics).
//
// This is the lower-level handle when a caller wants to drive delivery
// itself — e.g. an external job runner pulling pending rows. Most callers
// should prefer Trigger.
//
// Donor: eden-biz/internal/webhooks.ExecuteDelivery.
func (s *Service) ExecuteDelivery(ctx context.Context, deliveryID uuid.UUID) error {
	d, err := s.store.GetDelivery(ctx, deliveryID)
	if err != nil {
		return fmt.Errorf("get delivery: %w", err)
	}
	hook, err := s.store.GetWebhook(ctx, d.WebhookID)
	if err != nil {
		return fmt.Errorf("get webhook: %w", err)
	}
	s.deliver(ctx, hook, d)
	return nil
}
