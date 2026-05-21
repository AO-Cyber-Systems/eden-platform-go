package connectapi

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	connect "connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	platformv1 "github.com/aocybersystems/eden-platform-go/gen/go/platform/v1"
)

// fakeBreakGlassIngester records the last IngestBreakGlass call and returns
// pre-configured results. created==false simulates an idempotent replay.
type fakeBreakGlassIngester struct {
	eventID string
	created bool
	err     error

	calls    int
	received BreakGlassEvent
}

func (f *fakeBreakGlassIngester) IngestBreakGlass(_ context.Context, ev BreakGlassEvent) (string, bool, error) {
	f.calls++
	f.received = ev
	return f.eventID, f.created, f.err
}

func goodRequest(jti string) *platformv1.IngestBreakGlassEventRequest {
	return &platformv1.IngestBreakGlassEventRequest{
		OriginalJti:       jti,
		OriginalIssuedAt:  timestamppb.New(time.Now().Add(-1 * time.Hour)),
		Subject:           "ops@example.com",
		Tenant:            "acme",
		Scope:             "admin.recovery",
		Reason:            "outage 2026-05-20: restore primary KMS access",
		Operator:          "operator@example.com",
		ManifestDigestB64: "abc123==",
	}
}

func assertConnectCode(t *testing.T, err error, want connect.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error with code %v, got nil", want)
	}
	var ce *connect.Error
	if !errors.As(err, &ce) {
		t.Fatalf("expected *connect.Error, got %T: %v", err, err)
	}
	if ce.Code() != want {
		t.Fatalf("got code %v (%v), want %v", ce.Code(), ce.Message(), want)
	}
}

func TestIngestBreakGlassEvent_AcceptsValidEvent(t *testing.T) {
	jti := uuid.NewString()
	ing := &fakeBreakGlassIngester{eventID: uuid.NewString(), created: true}
	h := NewAuditHandler(nil, WithBreakGlassIngester(ing))

	req := goodRequest(jti)
	resp, err := h.IngestBreakGlassEvent(context.Background(), connect.NewRequest(req))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Msg.Accepted {
		t.Fatal("Accepted = false, want true")
	}
	if resp.Msg.EventId != ing.eventID {
		t.Fatalf("EventId = %q, want %q", resp.Msg.EventId, ing.eventID)
	}
	if ing.calls != 1 {
		t.Fatalf("ingester called %d times, want 1", ing.calls)
	}
	if ing.received.OriginalJTI.String() != jti {
		t.Errorf("OriginalJTI = %q, want %q", ing.received.OriginalJTI, jti)
	}
	if !ing.received.OriginalIssuedAt.Equal(req.OriginalIssuedAt.AsTime()) {
		t.Errorf("OriginalIssuedAt = %v, want %v",
			ing.received.OriginalIssuedAt, req.OriginalIssuedAt.AsTime())
	}
	if ing.received.Subject != "ops@example.com" {
		t.Errorf("Subject = %q", ing.received.Subject)
	}
	if ing.received.Scope != "admin.recovery" {
		t.Errorf("Scope = %q", ing.received.Scope)
	}
}

func TestIngestBreakGlassEvent_IdempotentReplay(t *testing.T) {
	// A repeated event: ingester returns created=false but the SAME event_id.
	// Handler must still report Accepted=true with that event_id — re-replay
	// is a successful no-op per the proto contract.
	originalEventID := uuid.NewString()
	ing := &fakeBreakGlassIngester{eventID: originalEventID, created: false}
	h := NewAuditHandler(nil, WithBreakGlassIngester(ing))

	resp, err := h.IngestBreakGlassEvent(context.Background(),
		connect.NewRequest(goodRequest(uuid.NewString())))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Msg.Accepted {
		t.Fatal("Accepted = false, want true for idempotent replay")
	}
	if resp.Msg.EventId != originalEventID {
		t.Fatalf("EventId = %q, want original %q", resp.Msg.EventId, originalEventID)
	}
}

func TestIngestBreakGlassEvent_NilIngesterReturnsUnimplemented(t *testing.T) {
	// No WithBreakGlassIngester => handler must refuse rather than silently
	// drop the event.
	h := NewAuditHandler(nil)
	_, err := h.IngestBreakGlassEvent(context.Background(),
		connect.NewRequest(goodRequest(uuid.NewString())))
	assertConnectCode(t, err, connect.CodeUnimplemented)
}

func TestIngestBreakGlassEvent_RejectsInvalidJTI(t *testing.T) {
	ing := &fakeBreakGlassIngester{eventID: uuid.NewString()}
	h := NewAuditHandler(nil, WithBreakGlassIngester(ing))

	req := goodRequest("not-a-uuid")
	_, err := h.IngestBreakGlassEvent(context.Background(), connect.NewRequest(req))
	assertConnectCode(t, err, connect.CodeInvalidArgument)
	if ing.calls != 0 {
		t.Fatalf("ingester was called %d times on bad input; must be 0", ing.calls)
	}
}

func TestIngestBreakGlassEvent_RejectsMissingTimestamp(t *testing.T) {
	ing := &fakeBreakGlassIngester{}
	h := NewAuditHandler(nil, WithBreakGlassIngester(ing))

	req := goodRequest(uuid.NewString())
	req.OriginalIssuedAt = nil
	_, err := h.IngestBreakGlassEvent(context.Background(), connect.NewRequest(req))
	assertConnectCode(t, err, connect.CodeInvalidArgument)
}

func TestIngestBreakGlassEvent_RejectsTimestampOlderThan30Days(t *testing.T) {
	ing := &fakeBreakGlassIngester{}
	h := NewAuditHandler(nil, WithBreakGlassIngester(ing))

	req := goodRequest(uuid.NewString())
	req.OriginalIssuedAt = timestamppb.New(time.Now().Add(-31 * 24 * time.Hour))
	_, err := h.IngestBreakGlassEvent(context.Background(), connect.NewRequest(req))
	assertConnectCode(t, err, connect.CodeInvalidArgument)
	if !strings.Contains(err.Error(), "older than") {
		t.Errorf("error message %q should mention 'older than'", err.Error())
	}
	if ing.calls != 0 {
		t.Fatalf("ingester called %d times on stale event; must be 0", ing.calls)
	}
}

func TestIngestBreakGlassEvent_RejectsFutureTimestamp(t *testing.T) {
	ing := &fakeBreakGlassIngester{}
	h := NewAuditHandler(nil, WithBreakGlassIngester(ing))

	req := goodRequest(uuid.NewString())
	// Beyond the clock-skew tolerance (5m).
	req.OriginalIssuedAt = timestamppb.New(time.Now().Add(10 * time.Minute))
	_, err := h.IngestBreakGlassEvent(context.Background(), connect.NewRequest(req))
	assertConnectCode(t, err, connect.CodeInvalidArgument)
}

func TestIngestBreakGlassEvent_RejectsUnknownScope(t *testing.T) {
	ing := &fakeBreakGlassIngester{}
	h := NewAuditHandler(nil, WithBreakGlassIngester(ing))

	for _, scope := range []string{"", "admin", "service.recovery.extra", "ADMIN.RECOVERY"} {
		t.Run("scope="+scope, func(t *testing.T) {
			req := goodRequest(uuid.NewString())
			req.Scope = scope
			_, err := h.IngestBreakGlassEvent(context.Background(), connect.NewRequest(req))
			assertConnectCode(t, err, connect.CodeInvalidArgument)
		})
	}
}

func TestIngestBreakGlassEvent_RequiresSubjectAndTenant(t *testing.T) {
	ing := &fakeBreakGlassIngester{}
	h := NewAuditHandler(nil, WithBreakGlassIngester(ing))

	t.Run("empty_subject", func(t *testing.T) {
		req := goodRequest(uuid.NewString())
		req.Subject = "   "
		_, err := h.IngestBreakGlassEvent(context.Background(), connect.NewRequest(req))
		assertConnectCode(t, err, connect.CodeInvalidArgument)
	})

	t.Run("empty_tenant", func(t *testing.T) {
		req := goodRequest(uuid.NewString())
		req.Tenant = ""
		_, err := h.IngestBreakGlassEvent(context.Background(), connect.NewRequest(req))
		assertConnectCode(t, err, connect.CodeInvalidArgument)
	})
}

func TestIngestBreakGlassEvent_IngesterErrorMapsToInternal(t *testing.T) {
	ing := &fakeBreakGlassIngester{err: errors.New("db down")}
	h := NewAuditHandler(nil, WithBreakGlassIngester(ing))

	_, err := h.IngestBreakGlassEvent(context.Background(),
		connect.NewRequest(goodRequest(uuid.NewString())))
	assertConnectCode(t, err, connect.CodeInternal)
}
