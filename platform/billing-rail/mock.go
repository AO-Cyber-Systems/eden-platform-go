package billingrail

import (
	"context"
	"sync"
)

// MockRail is a programmable Rail for tests. Configure responses on the
// public fields, then use the recorded slices for assertions.
//
// Concurrency: methods are safe for use from a single test goroutine; mix
// concurrent calls only when ResponseFn handlers are used and explicitly
// goroutine-safe.
type MockRail struct {
	NameValue string

	// Programmable responses. If a Fn variant is set, it takes precedence.
	ChargeResp                ChargeResult
	ChargeErr                 error
	ChargeFn                  func(ctx context.Context, req ChargeRequest) (ChargeResult, error)
	RefundResp                RefundResult
	RefundErr                 error
	RefundFn                  func(ctx context.Context, req RefundRequest) (RefundResult, error)
	CreateSubscriptionResp    SubscriptionResult
	CreateSubscriptionErr     error
	UpdateSubscriptionResp    SubscriptionResult
	UpdateSubscriptionErr     error
	CancelSubscriptionResp    SubscriptionResult
	CancelSubscriptionErr     error
	SubscriptionStatusResp    SubscriptionResult
	SubscriptionStatusErr     error
	ParseWebhookResp          WebhookEvent
	ParseWebhookErr           error
	ParseWebhookFn            func(ctx context.Context, headers map[string]string, body []byte) (WebhookEvent, error)

	mu                       sync.Mutex
	ChargeCalls              []ChargeRequest
	RefundCalls              []RefundRequest
	CreateSubCalls           []SubscriptionRequest
	UpdateSubCalls           []SubscriptionRequest
	CancelSubCalls           []string
	SubStatusCalls           []string
	WebhookCalls             []MockWebhookCall
}

// MockWebhookCall records a single ParseWebhook invocation.
type MockWebhookCall struct {
	Headers map[string]string
	Body    []byte
}

// Compile-time interface satisfaction.
var _ Rail = (*MockRail)(nil)

// Name returns the configured name (or "mock" if unset).
func (m *MockRail) Name() string {
	if m.NameValue == "" {
		return "mock"
	}
	return m.NameValue
}

func (m *MockRail) Charge(ctx context.Context, req ChargeRequest) (ChargeResult, error) {
	m.record(func() { m.ChargeCalls = append(m.ChargeCalls, req) })
	if m.ChargeFn != nil {
		return m.ChargeFn(ctx, req)
	}
	return m.ChargeResp, m.ChargeErr
}

func (m *MockRail) Refund(ctx context.Context, req RefundRequest) (RefundResult, error) {
	m.record(func() { m.RefundCalls = append(m.RefundCalls, req) })
	if m.RefundFn != nil {
		return m.RefundFn(ctx, req)
	}
	return m.RefundResp, m.RefundErr
}

func (m *MockRail) CreateSubscription(_ context.Context, req SubscriptionRequest) (SubscriptionResult, error) {
	m.record(func() { m.CreateSubCalls = append(m.CreateSubCalls, req) })
	return m.CreateSubscriptionResp, m.CreateSubscriptionErr
}

func (m *MockRail) UpdateSubscription(_ context.Context, req SubscriptionRequest) (SubscriptionResult, error) {
	m.record(func() { m.UpdateSubCalls = append(m.UpdateSubCalls, req) })
	return m.UpdateSubscriptionResp, m.UpdateSubscriptionErr
}

func (m *MockRail) CancelSubscription(_ context.Context, id string) (SubscriptionResult, error) {
	m.record(func() { m.CancelSubCalls = append(m.CancelSubCalls, id) })
	return m.CancelSubscriptionResp, m.CancelSubscriptionErr
}

func (m *MockRail) SubscriptionStatus(_ context.Context, id string) (SubscriptionResult, error) {
	m.record(func() { m.SubStatusCalls = append(m.SubStatusCalls, id) })
	return m.SubscriptionStatusResp, m.SubscriptionStatusErr
}

func (m *MockRail) ParseWebhook(ctx context.Context, headers map[string]string, body []byte) (WebhookEvent, error) {
	m.record(func() {
		// Defensive copies of input.
		hcopy := make(map[string]string, len(headers))
		for k, v := range headers {
			hcopy[k] = v
		}
		bcopy := make([]byte, len(body))
		copy(bcopy, body)
		m.WebhookCalls = append(m.WebhookCalls, MockWebhookCall{Headers: hcopy, Body: bcopy})
	})
	if m.ParseWebhookFn != nil {
		return m.ParseWebhookFn(ctx, headers, body)
	}
	return m.ParseWebhookResp, m.ParseWebhookErr
}

func (m *MockRail) record(fn func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	fn()
}

// MockSink is an EdenBizSink that records calls for assertions.
type MockSink struct {
	mu sync.Mutex

	// Errors to return from each method. Configure before use.
	RecordChargeErr           error
	RecordRefundErr           error
	RecordSubscriptionErr     error

	ChargeCalls       []MockSinkCharge
	RefundCalls       []MockSinkRefund
	SubscriptionCalls []MockSinkSubscription
}

// MockSinkCharge captures one RecordCharge invocation.
type MockSinkCharge struct {
	Customer Customer
	Amount   Money
	Result   ChargeResult
}

// MockSinkRefund captures one RecordRefund invocation.
type MockSinkRefund struct {
	Customer Customer
	Amount   Money
	Result   RefundResult
}

// MockSinkSubscription captures one RecordSubscriptionEvent invocation.
type MockSinkSubscription struct {
	Customer Customer
	Event    WebhookEvent
}

// Compile-time interface satisfaction.
var _ EdenBizSink = (*MockSink)(nil)

func (s *MockSink) RecordCharge(_ context.Context, c Customer, amt Money, r ChargeResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ChargeCalls = append(s.ChargeCalls, MockSinkCharge{Customer: c, Amount: amt, Result: r})
	return s.RecordChargeErr
}

func (s *MockSink) RecordRefund(_ context.Context, c Customer, amt Money, r RefundResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.RefundCalls = append(s.RefundCalls, MockSinkRefund{Customer: c, Amount: amt, Result: r})
	return s.RecordRefundErr
}

func (s *MockSink) RecordSubscriptionEvent(_ context.Context, c Customer, ev WebhookEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.SubscriptionCalls = append(s.SubscriptionCalls, MockSinkSubscription{Customer: c, Event: ev})
	return s.RecordSubscriptionErr
}
