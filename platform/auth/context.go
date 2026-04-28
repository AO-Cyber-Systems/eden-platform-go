package auth

import "context"

// contextKey is an unexported type to avoid context-value collisions with
// keys defined in other packages. Same string value/type as the
// platform/server context key it replaced, so context lookups via either
// the canonical helpers below or the platform/server re-exports resolve
// to the same value.
type contextKey string

const claimsKey contextKey = "auth_claims"

// WithClaims stores auth Claims in ctx. This is the CANONICAL home for the
// claims context key — the platform/server package re-exports a pair of
// thin wrappers so existing ConnectRPC interceptor code keeps compiling.
//
// HTTP middleware (e.g., eden-biz-go's internal/middleware/auth.go) MUST
// call this directly so that downstream RequireCompany / ClaimsFromContext
// reads succeed.
func WithClaims(ctx context.Context, claims *Claims) context.Context {
	return context.WithValue(ctx, claimsKey, claims)
}

// ClaimsFromContext retrieves auth Claims from ctx. Returns nil when ctx
// carries no claims.
func ClaimsFromContext(ctx context.Context) *Claims {
	c, _ := ctx.Value(claimsKey).(*Claims)
	return c
}
