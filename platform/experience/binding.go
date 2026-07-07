// binding.go -- TRD 140-04, must-have #3.
//
// ServiceTransportBinding (proto) is the transport- AND scope-agnostic contract;
// this file is its RUNTIME logic: the AOID identity model + the scope projection
// that maps ONE identity to each backend's tenant scope.
//
// DECISION #1 (locked): identity = AOID + AOEdge, ONE identity projected
// per-backend -- NOT a bespoke BFF, NOT a device multi-slot store. The device
// holds a single AoidIdentity; a binding's ScopeAuthority selects whether that
// identity projects to a COMPANY-scoped (eden-biz) or ORG-scoped (aocore)
// context. There is NO second credential.
//
// AUTHORITY IS NEVER BODY-BOUND. ResolveScope derives the scope strictly from
// the verified AoidIdentity (which AOEdge mints and aocore verifies). The
// caller passes the REQUESTED scope id (the entity the request targets); the
// projection authorizes it against the identity's grants and collapses any
// unauthorized/nonexistent target to a single ErrScopeDenied -- no existence
// oracle. AoidIdentity carries NO json tags: it is assembled from the verified
// identity context, never unmarshaled from a request body.
package experience

import (
	"errors"

	experiencev1 "github.com/aocybersystems/eden-platform-go/gen/go/experience/v1"
)

// ErrScopeDenied is the SINGLE non-leaking outcome of a scope projection that
// the AOID identity is not authorized for -- whether the requested scope exists
// for another tenant or does not exist at all. Collapsing both to one sentinel
// (with identical text) denies an existence oracle. 140-08/09 enforce isolation
// at the service chokepoint; this is the projection-layer guarantee.
var ErrScopeDenied = errors.New("experience: scope projection denied")

// AoidIdentity is the device's ONE verified AOID identity, assembled from the
// AOEdge identity-context (aocore verifies it; biz aligns to accept it inbound,
// task #16). It enumerates the company + org scopes the single identity is
// authorized for. NO json tags by design -- it is built from the verified
// identity context, NEVER bound from a request body (anti-pattern guard:
// authority must not be body-bindable).
type AoidIdentity struct {
	// Subject is the AOID subject (the "sub" claim) -- the stable identity id.
	Subject string
	// CompanyIDs are the eden-biz company scopes this identity may project to.
	CompanyIDs []string
	// OrgIDs are the aocore org scopes this identity may project to.
	OrgIDs []string
}

// ScopedContext is the result of projecting an AoidIdentity to ONE backend
// scope. It is what a Repository call runs under -- carrying the projecting
// identity's Subject (proving it is the SAME identity, no second credential),
// the chosen Authority, and the concrete ScopeID the call is bound to.
type ScopedContext struct {
	Subject   string                      // AOID subject the scope projects FROM (one identity)
	Authority experiencev1.ScopeAuthority // COMPANY (biz) | ORG (aocore)
	ScopeID   string                      // concrete company_id or org_id the call is bound to
}

// ResolveScope projects a single AoidIdentity to the requested backend scope.
//
//   - COMPANY -> the requestedScopeID must be one of identity.CompanyIDs.
//   - ORG     -> the requestedScopeID must be one of identity.OrgIDs.
//
// Authority comes ONLY from the verified identity (never a request body); the
// requestedScopeID is the target the request names. If the identity is not
// authorized for that target -- for ANY reason, including the target not
// existing -- the result is ErrScopeDenied, identical in both cases (no
// existence oracle). An UNSPECIFIED/unknown authority is fail-closed.
func ResolveScope(
	identity AoidIdentity,
	authority experiencev1.ScopeAuthority,
	requestedScopeID string,
) (ScopedContext, error) {
	var grants []string
	switch authority {
	case experiencev1.ScopeAuthority_SCOPE_AUTHORITY_COMPANY:
		grants = identity.CompanyIDs
	case experiencev1.ScopeAuthority_SCOPE_AUTHORITY_ORG:
		grants = identity.OrgIDs
	default:
		// UNSPECIFIED / future / unknown authority fails closed -- same
		// non-leaking outcome, never a render of an unscoped context.
		return ScopedContext{}, ErrScopeDenied
	}

	for _, g := range grants {
		if g == requestedScopeID && requestedScopeID != "" {
			return ScopedContext{
				Subject:   identity.Subject,
				Authority: authority,
				ScopeID:   requestedScopeID,
			}, nil
		}
	}
	// Not authorized -- collapse to the single sentinel. No distinction between
	// "exists for another tenant" and "does not exist".
	return ScopedContext{}, ErrScopeDenied
}

// BindingHasOperation reports whether the binding exposes the given operation.
// Lets callers distinguish reads (GET/LIST) from writes (CREATE/UPDATE/DELETE).
func BindingHasOperation(b *experiencev1.ServiceTransportBinding, op experiencev1.Operation) bool {
	for _, have := range b.GetOperations() {
		if have == op {
			return true
		}
	}
	return false
}

// BindingIsBindable reports whether a binding can actually be wired today. A
// binding with an UNSPECIFIED transport or scope_authority is structurally
// VALID (forward-compat -- it round-trips and reserves room for a future
// transport) but NOT yet bindable; the runtime must not attempt to dispatch it.
func BindingIsBindable(b *experiencev1.ServiceTransportBinding) bool {
	return b.GetTransportKind() != experiencev1.TransportKind_TRANSPORT_KIND_UNSPECIFIED &&
		b.GetScopeAuthority() != experiencev1.ScopeAuthority_SCOPE_AUTHORITY_UNSPECIFIED
}
