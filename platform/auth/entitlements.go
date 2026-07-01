package auth

import (
	"context"
	"errors"
	"fmt"
)

// ErrNoClaims is returned by RequirePlan when the request context carries no
// verified JWT claims. Handlers MUST translate this to HTTP 401/403 — the
// principal is unauthenticated (or the auth middleware did not run). Distinct
// from ErrMissingEntitlement so callers can map the two conditions to
// different status codes.
var ErrNoClaims = errors.New("auth: no claims in context")

// ErrMissingEntitlement is returned (wrapped, with the entitlement name) by
// RequirePlan when the verified token does not carry the required entitlement.
// Handlers MUST translate this to HTTP 402 Payment Required — the principal is
// authenticated but their plan does not include the requested capability.
// Match with errors.Is(err, ErrMissingEntitlement).
var ErrMissingEntitlement = errors.New("auth: missing entitlement")

// HasEntitlement reports whether the token carries the given entitlement
// string. Pure membership over c.Entitlements; empty/nil → false (deny).
//
// Mint-agnostic and least-privilege: eden only knows how to CARRY and TEST an
// arbitrary entitlement string — the entitlement vocabulary (e.g.
// "aofamily:plan:premium") is owned by the issuing product's billing service,
// never defined here. Never fails open.
func (c *Claims) HasEntitlement(entitlement string) bool {
	for _, e := range c.Entitlements {
		if e == entitlement {
			return true
		}
	}
	return false
}

// RequirePlan reads the verified Claims from context and returns them iff the
// token carries entitlement. Mint-agnostic — mirrors RequireHousehold /
// RequireParentMode: it performs a pure claim read with ZERO request-time
// billing I/O (the plan scope was resolved at issuance and rides in the `ent`
// claim).
//
// Returns:
//   - ErrNoClaims when ctx carries no claims (translate to HTTP 401/403).
//   - a wrapped ErrMissingEntitlement when the entitlement is absent
//     (translate to HTTP 402 Payment Required — the premium gate).
//
// Use as the primitive the per-route 402/403 gates compose:
//
//	claims, err := platformauth.RequirePlan(r.Context(), "aofamily:plan:premium")
//	if errors.Is(err, platformauth.ErrMissingEntitlement) {
//	    writeError(w, http.StatusPaymentRequired, "premium plan required")
//	    return
//	}
//	if err != nil {
//	    writeError(w, http.StatusUnauthorized, "missing auth context")
//	    return
//	}
func RequirePlan(ctx context.Context, entitlement string) (*Claims, error) {
	claims := ClaimsFromContext(ctx)
	if claims == nil {
		return nil, ErrNoClaims
	}
	if !claims.HasEntitlement(entitlement) {
		return nil, fmt.Errorf("%w %q", ErrMissingEntitlement, entitlement)
	}
	return claims, nil
}
