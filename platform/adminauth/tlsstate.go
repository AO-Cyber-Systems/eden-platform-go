package adminauth

import (
	"context"
	"crypto/tls"
	"net/http"
)

// tlsContextKey is a separate unexported context-key type so the TLS-state
// value cannot be accidentally retrieved via the admin-identity key (and
// vice versa).
type tlsContextKey int

const tlsStateKey tlsContextKey = iota

// WithTLSConnectionState is an http.Handler middleware that stashes the
// request's *tls.ConnectionState in the request context BEFORE the wrapped
// handler runs. This is REQUIRED for Connect-RPC services that need to read
// the verified client cert chain (see RESEARCH.md Pitfall 6) — Connect's
// req.Peer() does not expose the verified chain directly.
//
// Wrap the Connect mux with this middleware:
//
//	srv := &http.Server{
//	    Handler:   adminauth.WithTLSConnectionState(connectMux),
//	    TLSConfig: mtls.BuildServerTLSConfig(...),
//	}
//
// If r.TLS is nil (request did not arrive over TLS), the middleware passes
// the request through unchanged — downstream interceptors see no TLS state
// and decide how to react (typically by returning Unauthenticated).
func WithTLSConnectionState(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS != nil {
			ctx := context.WithValue(r.Context(), tlsStateKey, r.TLS)
			r = r.WithContext(ctx)
		}
		next.ServeHTTP(w, r)
	})
}

// TLSConnectionStateFromContext returns the *tls.ConnectionState stashed
// by WithTLSConnectionState, or nil if not present.
func TLSConnectionStateFromContext(ctx context.Context) *tls.ConnectionState {
	s, _ := ctx.Value(tlsStateKey).(*tls.ConnectionState)
	return s
}
