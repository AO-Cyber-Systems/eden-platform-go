package billingrail

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMockRail_ChargeReturnsConfiguredResponse(t *testing.T) {
	rail := &MockRail{
		ChargeResp: ChargeResult{
			RailChargeID: "ch_1",
			Status:       ChargeStatusSucceeded,
		},
	}
	got, err := rail.Charge(context.Background(), ChargeRequest{
		Amount:         Money{AmountMinor: 100, Currency: "usd"},
		IdempotencyKey: "key-1",
	})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if got.RailChargeID != "ch_1" {
		t.Errorf("RailChargeID=%q", got.RailChargeID)
	}
	if len(rail.ChargeCalls) != 1 {
		t.Errorf("ChargeCalls=%d", len(rail.ChargeCalls))
	}
	if rail.ChargeCalls[0].IdempotencyKey != "key-1" {
		t.Errorf("idempotency key not preserved: %q", rail.ChargeCalls[0].IdempotencyKey)
	}
}

func TestMockRail_ChargeFn(t *testing.T) {
	called := 0
	rail := &MockRail{
		ChargeFn: func(_ context.Context, req ChargeRequest) (ChargeResult, error) {
			called++
			return ChargeResult{RailChargeID: req.IdempotencyKey}, nil
		},
	}
	res, _ := rail.Charge(context.Background(), ChargeRequest{IdempotencyKey: "abc"})
	if called != 1 {
		t.Errorf("ChargeFn called %d times", called)
	}
	if res.RailChargeID != "abc" {
		t.Errorf("ChargeFn return not used: %q", res.RailChargeID)
	}
}

func TestMockRail_RefundFlow(t *testing.T) {
	rail := &MockRail{
		RefundResp: RefundResult{RailRefundID: "rfn_1", Status: RefundStatusSucceeded},
	}
	got, err := rail.Refund(context.Background(), RefundRequest{
		RailChargeID: "ch_1", Amount: Money{AmountMinor: 50, Currency: "usd"},
	})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if got.RailRefundID != "rfn_1" {
		t.Errorf("RailRefundID=%q", got.RailRefundID)
	}
	if len(rail.RefundCalls) != 1 {
		t.Errorf("RefundCalls=%d", len(rail.RefundCalls))
	}
}

func TestMockRail_SubscriptionLifecycle(t *testing.T) {
	rail := &MockRail{
		CreateSubscriptionResp: SubscriptionResult{RailSubscriptionID: "sub_1", Status: SubStatusActive},
		CancelSubscriptionResp: SubscriptionResult{RailSubscriptionID: "sub_1", Status: SubStatusCanceled, CurrentPeriodEnd: time.Unix(0, 0)},
	}
	created, err := rail.CreateSubscription(context.Background(), SubscriptionRequest{PlanID: "pro"})
	if err != nil {
		t.Fatalf("create err=%v", err)
	}
	if created.Status != SubStatusActive {
		t.Errorf("status=%v", created.Status)
	}

	canceled, err := rail.CancelSubscription(context.Background(), "sub_1")
	if err != nil {
		t.Fatalf("cancel err=%v", err)
	}
	if canceled.Status != SubStatusCanceled {
		t.Errorf("cancel status=%v", canceled.Status)
	}
	if len(rail.CreateSubCalls) != 1 || len(rail.CancelSubCalls) != 1 {
		t.Errorf("call counts: create=%d cancel=%d", len(rail.CreateSubCalls), len(rail.CancelSubCalls))
	}
	if rail.CancelSubCalls[0] != "sub_1" {
		t.Errorf("cancel arg=%q", rail.CancelSubCalls[0])
	}
}

func TestMockRail_ParseWebhookFn(t *testing.T) {
	rail := &MockRail{
		ParseWebhookFn: func(_ context.Context, _ map[string]string, body []byte) (WebhookEvent, error) {
			if string(body) == "boom" {
				return WebhookEvent{}, ErrInvalidSignature
			}
			return WebhookEvent{Type: EventChargeSucceeded, RailEventID: string(body)}, nil
		},
	}
	ev, err := rail.ParseWebhook(context.Background(), nil, []byte("ok-1"))
	if err != nil || ev.RailEventID != "ok-1" {
		t.Errorf("ev=%v err=%v", ev, err)
	}
	_, err = rail.ParseWebhook(context.Background(), nil, []byte("boom"))
	if !errors.Is(err, ErrInvalidSignature) {
		t.Errorf("expected ErrInvalidSignature, got %v", err)
	}
	if len(rail.WebhookCalls) != 2 {
		t.Errorf("WebhookCalls=%d", len(rail.WebhookCalls))
	}
}

func TestMockSink_ImplementsInterface(t *testing.T) {
	var _ EdenBizSink = (*MockSink)(nil)
}

func TestMockSink_RecordsCalls(t *testing.T) {
	sink := &MockSink{}
	ctx := context.Background()
	cust := Customer{ID: "c1"}

	if err := sink.RecordCharge(ctx, cust, Money{AmountMinor: 1, Currency: "usd"}, ChargeResult{}); err != nil {
		t.Errorf("RecordCharge err=%v", err)
	}
	if err := sink.RecordRefund(ctx, cust, Money{AmountMinor: 1, Currency: "usd"}, RefundResult{}); err != nil {
		t.Errorf("RecordRefund err=%v", err)
	}
	if err := sink.RecordSubscriptionEvent(ctx, cust, WebhookEvent{Type: EventSubCreated}); err != nil {
		t.Errorf("RecordSubscriptionEvent err=%v", err)
	}
	if len(sink.ChargeCalls) != 1 || len(sink.RefundCalls) != 1 || len(sink.SubscriptionCalls) != 1 {
		t.Errorf("call counts: charge=%d refund=%d sub=%d",
			len(sink.ChargeCalls), len(sink.RefundCalls), len(sink.SubscriptionCalls))
	}
}

func TestMockSink_ReturnsConfiguredErrors(t *testing.T) {
	bang := errors.New("nope")
	sink := &MockSink{
		RecordChargeErr:       bang,
		RecordRefundErr:       bang,
		RecordSubscriptionErr: bang,
	}
	ctx := context.Background()
	if err := sink.RecordCharge(ctx, Customer{}, Money{}, ChargeResult{}); !errors.Is(err, bang) {
		t.Errorf("RecordCharge err=%v", err)
	}
	if err := sink.RecordRefund(ctx, Customer{}, Money{}, RefundResult{}); !errors.Is(err, bang) {
		t.Errorf("RecordRefund err=%v", err)
	}
	if err := sink.RecordSubscriptionEvent(ctx, Customer{}, WebhookEvent{}); !errors.Is(err, bang) {
		t.Errorf("RecordSubscriptionEvent err=%v", err)
	}
}

func TestStatusStringers(t *testing.T) {
	cases := []struct {
		got, want string
	}{
		{ChargeStatusPending.String(), "pending"},
		{ChargeStatusSucceeded.String(), "succeeded"},
		{ChargeStatusFailed.String(), "failed"},
		{ChargeStatusUnknown.String(), "unknown"},
		{RefundStatusPending.String(), "pending"},
		{RefundStatusSucceeded.String(), "succeeded"},
		{RefundStatusFailed.String(), "failed"},
		{RefundStatusUnknown.String(), "unknown"},
		{SubStatusActive.String(), "active"},
		{SubStatusPastDue.String(), "past_due"},
		{SubStatusCanceled.String(), "canceled"},
		{SubStatusTrialing.String(), "trialing"},
		{SubStatusIncomplete.String(), "incomplete"},
		{SubStatusUnknown.String(), "unknown"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("got %q want %q", c.got, c.want)
		}
	}
}
