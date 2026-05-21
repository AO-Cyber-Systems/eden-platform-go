package audit_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/aocybersystems/eden-platform-go/platform/audit"
)

// otlpReceiver is a minimal httptest server that captures POST bodies. The
// OTLP HTTP exporter is protobuf-encoded; we sniff for the JWS attribute
// string occurring anywhere in the body, which is sufficient to prove the
// exporter wired the attribute into the request.
type otlpReceiver struct {
	mu     sync.Mutex
	bodies [][]byte
	status int
}

func newOTLPReceiver(status int) *otlpReceiver { return &otlpReceiver{status: status} }

func (r *otlpReceiver) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		body := make([]byte, req.ContentLength)
		_, _ = req.Body.Read(body)
		r.mu.Lock()
		r.bodies = append(r.bodies, body)
		r.mu.Unlock()
		w.WriteHeader(r.status)
		// OTLP HTTP responses are protobuf-encoded — empty body is fine on success.
		_, _ = w.Write([]byte{})
	}
}

func (r *otlpReceiver) bodiesCopy() [][]byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([][]byte, len(r.bodies))
	for i, b := range r.bodies {
		out[i] = append([]byte(nil), b...)
	}
	return out
}

func sampleBufferedEvent() audit.BufferedEvent {
	return audit.BufferedEvent{
		ID:           42,
		JTI:          "01HXYZ-TEST-JTI",
		TenantID:     uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		EmittedAt:    time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC),
		JWSCompact:   "header.payload.signature-XYZ",
		PayloadCanon: []byte(`{"event_type":"auth.user.login","iss":"aoid","iat":1700000000}`),
		Attempts:     0,
	}
}

func TestOTLPLogSink_EmitsEventWithJWSAttribute(t *testing.T) {
	rec := newOTLPReceiver(http.StatusOK)
	srv := httptest.NewServer(rec.handler())
	t.Cleanup(srv.Close)

	sink, err := audit.NewOTLPLogSink(context.Background(), srv.URL+"/v1/logs", nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sink.Shutdown(context.Background()) })

	require.NoError(t, sink.Send(context.Background(), []audit.BufferedEvent{sampleBufferedEvent()}))

	require.Eventually(t, func() bool {
		for _, b := range rec.bodiesCopy() {
			if strings.Contains(string(b), "header.payload.signature-XYZ") {
				return true
			}
		}
		return false
	}, 5*time.Second, 50*time.Millisecond, "expected JWS to appear in OTLP body")
}

func TestOTLPLogSink_AttributesIncludeSchemaVersion(t *testing.T) {
	rec := newOTLPReceiver(http.StatusOK)
	srv := httptest.NewServer(rec.handler())
	t.Cleanup(srv.Close)

	sink, err := audit.NewOTLPLogSink(context.Background(), srv.URL+"/v1/logs", nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sink.Shutdown(context.Background()) })
	require.NoError(t, sink.Send(context.Background(), []audit.BufferedEvent{sampleBufferedEvent()}))

	require.Eventually(t, func() bool {
		for _, b := range rec.bodiesCopy() {
			s := string(b)
			if strings.Contains(s, "aoid.audit.schema_version") && strings.Contains(s, "1") {
				return true
			}
		}
		return false
	}, 5*time.Second, 50*time.Millisecond)
}

func TestOTLPLogSink_NetworkFailure_ReturnsError(t *testing.T) {
	rec := newOTLPReceiver(http.StatusInternalServerError)
	srv := httptest.NewServer(rec.handler())
	t.Cleanup(srv.Close)

	sink, err := audit.NewOTLPLogSink(context.Background(), srv.URL+"/v1/logs", nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sink.Shutdown(context.Background()) })

	err = sink.Send(context.Background(), []audit.BufferedEvent{sampleBufferedEvent()})
	require.Error(t, err, "expected error from 500-returning OTLP receiver")
}

func TestOTLPLogSink_ShutdownClean(t *testing.T) {
	rec := newOTLPReceiver(http.StatusOK)
	srv := httptest.NewServer(rec.handler())
	t.Cleanup(srv.Close)

	sink, err := audit.NewOTLPLogSink(context.Background(), srv.URL+"/v1/logs", nil)
	require.NoError(t, err)
	require.NoError(t, sink.Shutdown(context.Background()))
}

func TestOTLPLogSink_EmptyEndpoint_Errors(t *testing.T) {
	_, err := audit.NewOTLPLogSink(context.Background(), "", nil)
	require.Error(t, err)
}
