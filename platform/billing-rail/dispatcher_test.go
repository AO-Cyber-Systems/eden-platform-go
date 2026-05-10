package billingrail

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDispatcher_ChargeSucceededFlowsToSink(t *testing.T) {
	cust := Customer{ID: "cust_1", Email: "a@b", TenantID: "tenant_1"}
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)

	rail := &MockRail{
		ParseWebhookResp: WebhookEvent{
			RailEventID: "evt_1",
			Type:        EventChargeSucceeded,
			OccurredAt:  now,
			Customer:    cust,
			Amount:      Money{AmountMinor: 1500, Currency: "usd"},
			ChargeID:    "ch_abc",
			RailObject:  []byte(`{"rail":"object"}`),
		},
	}
	sink := &MockSink{}
	d := NewDispatcher(rail, sink)

	err := d.Handle(context.Background(), map[string]string{"X-Sig": "ok"}, []byte("body"))
	if err != nil {
		t.Fatalf("Handle err=%v", err)
	}
	if len(sink.ChargeCalls) != 1 {
		t.Fatalf("expected 1 charge call, got %d", len(sink.ChargeCalls))
	}
	got := sink.ChargeCalls[0]
	if got.Customer.ID != "cust_1" {
		t.Errorf("customer.ID=%q", got.Customer.ID)
	}
	if got.Amount.AmountMinor != 1500 {
		t.Errorf("amount=%v", got.Amount)
	}
	if got.Result.RailChargeID != "ch_abc" {
		t.Errorf("RailChargeID=%q", got.Result.RailChargeID)
	}
	if got.Result.Status != ChargeStatusSucceeded {
		t.Errorf("status=%v", got.Result.Status)
	}
	if !got.Result.ProcessedAt.Equal(now) {
		t.Errorf("processed_at=%v", got.Result.ProcessedAt)
	}
	if string(got.Result.RawResponse) != `{"rail":"object"}` {
		t.Errorf("raw=%q", got.Result.RawResponse)
	}
	if len(sink.RefundCalls) != 0 || len(sink.SubscriptionCalls) != 0 {
		t.Error("only charge sink should be invoked")
	}
}

func TestDispatcher_ChargeFailedFlowsToSink(t *testing.T) {
	rail := &MockRail{
		ParseWebhookResp: WebhookEvent{
			Type:     EventChargeFailed,
			ChargeID: "ch_fail",
		},
	}
	sink := &MockSink{}
	d := NewDispatcher(rail, sink)

	if err := d.Handle(context.Background(), nil, nil); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(sink.ChargeCalls) != 1 {
		t.Fatalf("charge calls=%d", len(sink.ChargeCalls))
	}
	if sink.ChargeCalls[0].Result.Status != ChargeStatusFailed {
		t.Errorf("expected failed status, got %v", sink.ChargeCalls[0].Result.Status)
	}
}

func TestDispatcher_RefundFlowsToSink(t *testing.T) {
	rail := &MockRail{
		ParseWebhookResp: WebhookEvent{
			RailEventID: "evt_refund_1",
			Type:        EventChargeRefunded,
			Customer:    Customer{ID: "cust_1"},
			Amount:      Money{AmountMinor: 500, Currency: "usd"},
			ChargeID:    "ch_xyz",
		},
	}
	sink := &MockSink{}
	d := NewDispatcher(rail, sink)

	if err := d.Handle(context.Background(), nil, nil); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(sink.RefundCalls) != 1 {
		t.Fatalf("refund calls=%d", len(sink.RefundCalls))
	}
	got := sink.RefundCalls[0]
	if got.Result.RailRefundID != "evt_refund_1" {
		t.Errorf("RailRefundID=%q", got.Result.RailRefundID)
	}
	if got.Amount.AmountMinor != 500 {
		t.Errorf("amount=%d", got.Amount.AmountMinor)
	}
}

func TestDispatcher_SubscriptionEventsFlowToSink(t *testing.T) {
	tests := []EventType{
		EventSubCreated,
		EventSubUpdated,
		EventSubCanceled,
		EventSubRenewed,
		EventSubTrialEnded,
	}
	for _, et := range tests {
		t.Run(string(et), func(t *testing.T) {
			rail := &MockRail{
				ParseWebhookResp: WebhookEvent{
					Type:           et,
					Customer:       Customer{ID: "c"},
					SubscriptionID: "sub_1",
				},
			}
			sink := &MockSink{}
			d := NewDispatcher(rail, sink)
			if err := d.Handle(context.Background(), nil, nil); err != nil {
				t.Fatalf("Handle: %v", err)
			}
			if len(sink.SubscriptionCalls) != 1 {
				t.Fatalf("calls=%d", len(sink.SubscriptionCalls))
			}
			if sink.SubscriptionCalls[0].Event.Type != et {
				t.Errorf("event type=%v", sink.SubscriptionCalls[0].Event.Type)
			}
		})
	}
}

func TestDispatcher_RailNameStampedOnEvent(t *testing.T) {
	rail := &MockRail{
		NameValue: "stripe",
		ParseWebhookResp: WebhookEvent{
			Type:           EventSubCreated,
			SubscriptionID: "sub_1",
		},
	}
	sink := &MockSink{}
	d := NewDispatcher(rail, sink)
	if err := d.Handle(context.Background(), nil, nil); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got := sink.SubscriptionCalls[0].Event.RailName; got != "stripe" {
		t.Errorf("RailName=%q want stripe", got)
	}
}

func TestDispatcher_PreservesRailNameWhenAdapterSetsIt(t *testing.T) {
	rail := &MockRail{
		NameValue: "stripe",
		ParseWebhookResp: WebhookEvent{
			Type:     EventSubCreated,
			RailName: "preset",
		},
	}
	sink := &MockSink{}
	d := NewDispatcher(rail, sink)
	if err := d.Handle(context.Background(), nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := sink.SubscriptionCalls[0].Event.RailName; got != "preset" {
		t.Errorf("RailName=%q want preset (adapter-supplied)", got)
	}
}

func TestDispatcher_InvalidSignatureSkipsSink(t *testing.T) {
	rail := &MockRail{ParseWebhookErr: ErrInvalidSignature}
	sink := &MockSink{}
	d := NewDispatcher(rail, sink)

	err := d.Handle(context.Background(), nil, nil)
	if !errors.Is(err, ErrInvalidSignature) {
		t.Errorf("err=%v want ErrInvalidSignature", err)
	}
	if len(sink.ChargeCalls)+len(sink.RefundCalls)+len(sink.SubscriptionCalls) != 0 {
		t.Error("sink must not be called on invalid signature")
	}
}

func TestDispatcher_UnknownEventTypeReturnsUnsupported(t *testing.T) {
	rail := &MockRail{
		ParseWebhookResp: WebhookEvent{Type: "completely.unknown"},
	}
	sink := &MockSink{}
	d := NewDispatcher(rail, sink)

	err := d.Handle(context.Background(), nil, nil)
	if !errors.Is(err, ErrUnsupportedEvent) {
		t.Errorf("err=%v want ErrUnsupportedEvent", err)
	}
	if len(sink.ChargeCalls)+len(sink.RefundCalls)+len(sink.SubscriptionCalls) != 0 {
		t.Error("sink must not be called for unsupported events")
	}
}

func TestDispatcher_SinkErrorBubblesUp(t *testing.T) {
	bang := errors.New("db down")
	rail := &MockRail{
		ParseWebhookResp: WebhookEvent{Type: EventChargeSucceeded},
	}
	sink := &MockSink{RecordChargeErr: bang}
	d := NewDispatcher(rail, sink)

	err := d.Handle(context.Background(), nil, nil)
	if !errors.Is(err, bang) {
		t.Errorf("expected underlying error to bubble; got %v", err)
	}
}

func TestDispatcher_NilArgumentsPanic(t *testing.T) {
	t.Run("nil rail", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic on nil rail")
			}
		}()
		_ = NewDispatcher(nil, &MockSink{})
	})
	t.Run("nil sink", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic on nil sink")
			}
		}()
		_ = NewDispatcher(&MockRail{}, nil)
	})
}
