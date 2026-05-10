package audit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestAction_String(t *testing.T) {
	if got := ActionUserLogin.String(); got != "auth.user.login" {
		t.Errorf("ActionUserLogin = %q, want %q", got, "auth.user.login")
	}
}

func TestEvent_WithDetail(t *testing.T) {
	e := Event{}.WithDetail("k", "v")
	if e.Details["k"] != "v" {
		t.Errorf("Details[k] = %v, want v", e.Details["k"])
	}
	// Chain still works on populated map.
	e = e.WithDetail("k2", 42)
	if e.Details["k2"] != 42 {
		t.Errorf("Details[k2] = %v, want 42", e.Details["k2"])
	}
}

func TestEvent_WithBeforeAfter(t *testing.T) {
	type s struct{ V int }
	e := Event{}.WithBeforeAfter(s{1}, s{2})
	if e.Details[DetailBefore].(s).V != 1 {
		t.Errorf("before = %v, want {1}", e.Details[DetailBefore])
	}
	if e.Details[DetailAfter].(s).V != 2 {
		t.Errorf("after = %v, want {2}", e.Details[DetailAfter])
	}
}

func TestEvent_WithRequestID(t *testing.T) {
	e := Event{}.WithRequestID("")
	if _, ok := e.Details[DetailRequestID]; ok {
		t.Errorf("empty request id should be ignored")
	}
	e = Event{}.WithRequestID("rid-123")
	if e.Details[DetailRequestID] != "rid-123" {
		t.Errorf("request id = %v, want rid-123", e.Details[DetailRequestID])
	}
}

func TestEvent_WithReason(t *testing.T) {
	e := Event{}.WithReason("")
	if _, ok := e.Details[DetailReason]; ok {
		t.Errorf("empty reason should be ignored")
	}
	e = Event{}.WithReason("expired")
	if e.Details[DetailReason] != "expired" {
		t.Errorf("reason = %v, want expired", e.Details[DetailReason])
	}
}

func TestEvent_WithAction(t *testing.T) {
	e := Event{}.WithAction(ActionUserLogin)
	if e.Action != "auth.user.login" {
		t.Errorf("Action = %q, want auth.user.login", e.Action)
	}
}

func TestEventFromHTTP_NilRequest(t *testing.T) {
	e := EventFromHTTP(nil)
	if e.IPAddress != "" || e.Details != nil {
		t.Errorf("nil request should yield zero event, got %+v", e)
	}
}

func TestEventFromHTTP_XForwardedFor(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Forwarded-For", "203.0.113.5, 10.0.0.1")
	r.Header.Set("User-Agent", "ua/1.0")
	r.Header.Set("X-Request-ID", "rid-9")

	e := EventFromHTTP(r)
	if e.IPAddress != "203.0.113.5" {
		t.Errorf("IP = %q, want 203.0.113.5", e.IPAddress)
	}
	if e.Details[DetailUserAgent] != "ua/1.0" {
		t.Errorf("user agent = %v, want ua/1.0", e.Details[DetailUserAgent])
	}
	if e.Details[DetailRequestID] != "rid-9" {
		t.Errorf("request id = %v, want rid-9", e.Details[DetailRequestID])
	}
}

func TestEventFromHTTP_XRealIP(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Real-IP", "198.51.100.7")
	e := EventFromHTTP(r)
	if e.IPAddress != "198.51.100.7" {
		t.Errorf("IP = %q, want 198.51.100.7", e.IPAddress)
	}
}

func TestEventFromHTTP_RemoteAddrFallback(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "192.0.2.10:54321"
	e := EventFromHTTP(r)
	if e.IPAddress != "192.0.2.10" {
		t.Errorf("IP = %q, want 192.0.2.10", e.IPAddress)
	}
}

func TestEventFromHTTP_RemoteAddrNoPort(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "no-port"
	e := EventFromHTTP(r)
	if e.IPAddress != "no-port" {
		t.Errorf("IP = %q, want no-port", e.IPAddress)
	}
}

func TestEventFromHTTP_XFFSingleHop(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Forwarded-For", "  203.0.113.99  ")
	e := EventFromHTTP(r)
	if e.IPAddress != "203.0.113.99" {
		t.Errorf("IP = %q, want 203.0.113.99 (trimmed)", e.IPAddress)
	}
}

func TestLogger_LogSync_NilStore(t *testing.T) {
	l := NewLogger(nil)
	if err := l.LogSync(context.Background(), Event{}); err == nil {
		t.Errorf("expected error for nil store")
	}
}

func TestLogger_LogSync_InvalidCompanyID(t *testing.T) {
	store := &mockAuditStore{}
	l := NewLogger(store)
	err := l.LogSync(context.Background(), Event{
		CompanyID: "not-a-uuid",
		ActorID:   uuid.New().String(),
	})
	if err == nil {
		t.Errorf("expected error for invalid company id")
	}
}

func TestLogger_LogSync_InvalidActorID(t *testing.T) {
	store := &mockAuditStore{}
	l := NewLogger(store)
	err := l.LogSync(context.Background(), Event{
		CompanyID: uuid.New().String(),
		ActorID:   "not-a-uuid",
	})
	if err == nil {
		t.Errorf("expected error for invalid actor id")
	}
}

func TestLogger_LogSync_Success(t *testing.T) {
	store := &mockAuditStore{}
	l := NewLogger(store)
	err := l.LogSync(context.Background(), Event{
		CompanyID: uuid.New().String(),
		ActorID:   uuid.New().String(),
		Action:    ActionUserLogin.String(),
		Resource:  "user",
	}.WithDetail("k", "v"))
	if err != nil {
		t.Fatalf("LogSync error = %v, want nil", err)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.events) != 1 {
		t.Errorf("store events = %d, want 1", len(store.events))
	}
	if store.events[0].Action != "auth.user.login" {
		t.Errorf("action = %q, want auth.user.login", store.events[0].Action)
	}
}
