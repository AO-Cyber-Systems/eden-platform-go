package billingrail

import (
	"context"
	"errors"
	"fmt"
)

// Dispatcher wires a Rail's webhook intake to an EdenBizSink.
//
// Use it as the HTTP handler for a rail's webhook endpoint:
//
//	d := billingrail.NewDispatcher(rail, sink)
//	http.HandleFunc("/webhooks/stripe", func(w http.ResponseWriter, r *http.Request) {
//	    body, _ := io.ReadAll(r.Body)
//	    headers := map[string]string{}
//	    for k := range r.Header {
//	        headers[k] = r.Header.Get(k)
//	    }
//	    if err := d.Handle(r.Context(), headers, body); err != nil {
//	        if errors.Is(err, billingrail.ErrInvalidSignature) {
//	            http.Error(w, "bad signature", http.StatusBadRequest)
//	            return
//	        }
//	        if errors.Is(err, billingrail.ErrUnsupportedEvent) {
//	            w.WriteHeader(http.StatusNoContent) // ignore politely
//	            return
//	        }
//	        http.Error(w, "sink failed", http.StatusInternalServerError)
//	        return
//	    }
//	    w.WriteHeader(http.StatusOK)
//	})
//
// Sink failures bubble up so the caller can return 5xx and let the rail
// redeliver. The dispatcher does not retry internally; idempotency is the
// sink's responsibility.
type Dispatcher struct {
	rail Rail
	sink EdenBizSink
}

// NewDispatcher returns a Dispatcher; both arguments are required.
func NewDispatcher(rail Rail, sink EdenBizSink) *Dispatcher {
	if rail == nil {
		panic("billingrail: nil Rail")
	}
	if sink == nil {
		panic("billingrail: nil EdenBizSink")
	}
	return &Dispatcher{rail: rail, sink: sink}
}

// Handle parses the webhook payload and dispatches it to the sink. Returns
// ErrInvalidSignature on signature failure (do not retry), ErrUnsupportedEvent
// for events the adapter rejects (do not retry), or a wrapped sink error
// (DO retry).
func (d *Dispatcher) Handle(ctx context.Context, headers map[string]string, body []byte) error {
	event, err := d.rail.ParseWebhook(ctx, headers, body)
	if err != nil {
		return err
	}
	if event.RailName == "" {
		event.RailName = d.rail.Name()
	}

	switch event.Type {
	case EventChargeSucceeded:
		res := ChargeResult{
			RailChargeID: event.ChargeID,
			Status:       ChargeStatusSucceeded,
			ProcessedAt:  event.OccurredAt,
			RawResponse:  event.RailObject,
		}
		if err := d.sink.RecordCharge(ctx, event.Customer, event.Amount, res); err != nil {
			return fmt.Errorf("billingrail: sink record charge: %w", err)
		}
	case EventChargeFailed:
		res := ChargeResult{
			RailChargeID: event.ChargeID,
			Status:       ChargeStatusFailed,
			ProcessedAt:  event.OccurredAt,
			RawResponse:  event.RailObject,
		}
		if err := d.sink.RecordCharge(ctx, event.Customer, event.Amount, res); err != nil {
			return fmt.Errorf("billingrail: sink record charge: %w", err)
		}
	case EventChargeRefunded:
		res := RefundResult{
			RailRefundID: event.RailEventID,
			Status:       RefundStatusSucceeded,
			ProcessedAt:  event.OccurredAt,
			RawResponse:  event.RailObject,
		}
		if err := d.sink.RecordRefund(ctx, event.Customer, event.Amount, res); err != nil {
			return fmt.Errorf("billingrail: sink record refund: %w", err)
		}
	case EventSubCreated, EventSubUpdated, EventSubCanceled, EventSubRenewed, EventSubTrialEnded:
		if err := d.sink.RecordSubscriptionEvent(ctx, event.Customer, event); err != nil {
			return fmt.Errorf("billingrail: sink record subscription: %w", err)
		}
	default:
		return errors.Join(ErrUnsupportedEvent, fmt.Errorf("type=%q", event.Type))
	}
	return nil
}
