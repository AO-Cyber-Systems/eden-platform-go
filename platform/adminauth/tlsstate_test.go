package adminauth_test

// Test list:
// - TestWithTLSConnectionState/stashes_tls_state_when_present
// - TestWithTLSConnectionState/passes_through_when_TLS_nil
// - TestTLSConnectionStateFromContext/nil_for_fresh_context

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aocybersystems/eden-platform-go/platform/adminauth"
	"github.com/stretchr/testify/require"
)

func TestWithTLSConnectionState_StashesTLSStateWhenPresent(t *testing.T) {
	fakeState := &tls.ConnectionState{ServerName: "test.local"}
	var observed *tls.ConnectionState
	var invoked bool
	handler := adminauth.WithTLSConnectionState(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		invoked = true
		observed = adminauth.TLSConnectionStateFromContext(r.Context())
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.TLS = fakeState
	handler.ServeHTTP(httptest.NewRecorder(), req)
	require.True(t, invoked, "downstream handler should be invoked")
	require.NotNil(t, observed, "TLS state should be stashed in context")
	require.Equal(t, "test.local", observed.ServerName)
}

func TestWithTLSConnectionState_PassesThroughWhenTLSNil(t *testing.T) {
	var observed *tls.ConnectionState
	var invoked bool
	handler := adminauth.WithTLSConnectionState(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		invoked = true
		observed = adminauth.TLSConnectionStateFromContext(r.Context())
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	// Explicitly leave req.TLS == nil
	req.TLS = nil
	handler.ServeHTTP(httptest.NewRecorder(), req)
	require.True(t, invoked, "downstream handler should still be invoked when TLS is absent")
	require.Nil(t, observed, "no TLS state should be stashed when r.TLS is nil")
}

func TestTLSConnectionStateFromContext_NilForFreshContext(t *testing.T) {
	got := adminauth.TLSConnectionStateFromContext(context.Background())
	require.Nil(t, got)
}
