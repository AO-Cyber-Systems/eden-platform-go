package adminauth

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"

	"github.com/aocybersystems/eden-platform-go/platform/mtls"
)

// NewAdminContextInterceptor returns a Connect unary interceptor that:
//  1. Reads *tls.ConnectionState from context (set by WithTLSConnectionState).
//  2. Extracts the peer's CommonName via platform/mtls.ExtractPeerCommonName.
//  3. Calls resolver.ResolveFromCN to map CN → AdminIdentity.
//  4. Stores the AdminIdentity in the request context for downstream code.
//
// Error mapping:
//
//	TLS state missing        → CodeInternal         (operator misconfig)
//	no verified chain        → CodeUnauthenticated  (mTLS not enforced / cert invalid)
//	empty CN                 → CodeUnauthenticated
//	resolver ErrUnknownAdmin → CodePermissionDenied
//	resolver other err       → CodeInternal         (infra failure)
//
// Panics if resolver is nil — callers MUST provide a resolver.
func NewAdminContextInterceptor(resolver AdminIdentityResolver) connect.UnaryInterceptorFunc {
	if resolver == nil {
		panic("adminauth: NewAdminContextInterceptor requires a non-nil resolver")
	}
	return connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			state := TLSConnectionStateFromContext(ctx)
			if state == nil {
				return nil, connect.NewError(connect.CodeInternal,
					errors.New("adminauth: TLS state missing — wire WithTLSConnectionState before Connect mux"))
			}
			cn, err := mtls.ExtractPeerCommonName(state)
			if err != nil {
				return nil, connect.NewError(connect.CodeUnauthenticated,
					fmt.Errorf("adminauth: cn extract: %w", err))
			}
			if cn == "" {
				return nil, connect.NewError(connect.CodeUnauthenticated, ErrMissingCN)
			}
			identity, err := resolver.ResolveFromCN(ctx, cn)
			switch {
			case errors.Is(err, ErrUnknownAdmin):
				return nil, connect.NewError(connect.CodePermissionDenied, err)
			case err != nil:
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("adminauth: resolver: %w", err))
			}
			ctx = WithAdminIdentity(ctx, identity)
			return next(ctx, req)
		}
	})
}
