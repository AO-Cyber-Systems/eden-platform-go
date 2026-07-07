// repository.go -- TRD 140-11, the transport-agnostic binding PROOF.
//
// The "pluggable data binding" claim (must-have #3) is only freezable once it is
// proven against BOTH transports AND BOTH scope authorities through ONE
// abstraction. aocore has NO protobuf/Connect: it is REST/OpenAPI, org-scoped,
// edge-signed. eden-biz is Connect, company-scoped. This file defines the single
// Repository interface those two backends sit behind, plus the selector that a
// ServiceTransportBinding resolves to.
//
// THE PROOF: a FeatureSurface holds a Repository. It calls Get/List/Create/
// Update/Delete. It is UNAWARE whether the concrete impl talks Connect to
// eden-biz under a COMPANY scope or REST/OpenAPI to aocore under an ORG scope.
// ServiceTransportBinding.transport_kind x scope_authority picks the impl; the
// surface never branches on transport. That sameness IS the agnosticism.
//
// SCOPE IS ENFORCED AT THE REPO CHOKEPOINT. Every Repository method takes a
// ScopedContext (the projection of the ONE AoidIdentity to this backend's
// scope). A call whose ScopedContext names a scope the repo is not bound to --
// for ANY reason, including a forged cross-tenant/cross-org context -- collapses
// to ErrScopeDenied, identical whether the target exists or not (no oracle).
// Authority is NEVER read from a request body.
package experience

import (
	"context"
	"errors"
	"fmt"

	experiencev1 "github.com/aocybersystems/eden-platform-go/gen/go/experience/v1"
)

// ErrTransportNotSupported is the typed, forward-compat outcome of asking
// RepositoryFor to bind a transport/scope that has no impl today (e.g. an
// UNSPECIFIED transport reserved for a future backend). It is distinct from a
// scope denial: the binding is structurally valid but not yet wireable. Callers
// errors.Is against it to render a "not yet supported" affordance rather than a
// permission error.
var ErrTransportNotSupported = errors.New("experience: transport/scope has no Repository impl")

// Entity is the backend-NEUTRAL row a Repository moves. It deliberately carries
// no transport- or backend-specific shape: an id, the scope it belongs to, and
// an opaque field bag. The Connect stub fills it from biz-shaped fixtures; the
// aocore-REST stub fills it from cassette JSON. The surface sees ONE shape.
type Entity struct {
	// ID is the entity's stable identifier within its scope.
	ID string
	// ScopeID is the company_id (COMPANY) or org_id (ORG) the entity belongs to.
	// It is set BY the repo from the authorized ScopedContext, never trusted from
	// inbound data -- so a leaked cross-scope row would be structurally visible.
	ScopeID string
	// Fields is the opaque, backend-neutral payload (e.g. {"name": "..."}).
	Fields map[string]string
}

// PageRequest is the transport-neutral pagination input. Both the Connect
// (cursor) and REST (cursor/offset) backends thread it; the surface expresses
// pagination ONCE regardless of which transport honors it.
type PageRequest struct {
	// Cursor is an opaque continuation token (empty = first page).
	Cursor string
	// Limit is the max items to return (0 = backend default).
	Limit int
}

// Page is the transport-neutral pagination output. NextCursor is empty when no
// further page exists.
type Page struct {
	Items      []Entity
	NextCursor string
}

// Repository is the ONE abstraction every backend sits behind. A FeatureSurface
// binds to a Repository and never knows its transport or scope authority.
//
// Reads: Get, List. Writes: Create, Update, Delete. Each takes a ScopedContext
// projected from the single AoidIdentity; the impl enforces that the context's
// scope matches the scope the repo is bound to (chokepoint), collapsing any
// mismatch to ErrScopeDenied with no existence oracle.
type Repository interface {
	// Get reads one entity by id within the ScopedContext's scope.
	Get(ctx context.Context, sc ScopedContext, id string) (Entity, error)
	// List reads a page of entities within the scope, honoring pagination.
	List(ctx context.Context, sc ScopedContext, page PageRequest) (Page, error)
	// Create writes a new entity within the scope and returns the stored row.
	Create(ctx context.Context, sc ScopedContext, e Entity) (Entity, error)
	// Update writes changes to an existing entity within the scope.
	Update(ctx context.Context, sc ScopedContext, e Entity) (Entity, error)
	// Delete removes an entity by id within the scope.
	Delete(ctx context.Context, sc ScopedContext, id string) error

	// Transport reports the concrete transport this impl speaks. Diagnostic /
	// selection-proof only -- the surface does NOT branch on it.
	Transport() experiencev1.TransportKind
	// Authority reports the scope authority this impl is bound to. Diagnostic /
	// selection-proof only.
	Authority() experiencev1.ScopeAuthority
}

// CassetteBackedRepository is implemented by repos whose backend is an
// in-process httptest cassette server (no live call). Tests assert the base URL
// is loopback so a real network call can never escape.
type CassetteBackedRepository interface {
	BaseURL() string
}

// RepositoryFor selects the concrete Repository for a ServiceTransportBinding,
// keyed on (transport_kind x scope_authority). This is the agnosticism seam: the
// SAME surface code receives whichever impl the binding names.
//
//   - CONNECT      x COMPANY -> the eden-biz Connect stub (company-scoped).
//   - REST_OPENAPI x ORG     -> the aocore-REST stub (org-scoped, cassette-replayed).
//   - anything else (UNSPECIFIED, or a transport/scope pairing with no impl) ->
//     (nil, ErrTransportNotSupported), so a forward-compat binding is rejected
//     cleanly instead of mis-bound.
//
// The identity is the ONE AoidIdentity; the chosen impl will only ever run under
// a ScopedContext projected from it (no second credential).
func RepositoryFor(b *experiencev1.ServiceTransportBinding, identity AoidIdentity) (Repository, error) {
	if !BindingIsBindable(b) {
		return nil, fmt.Errorf("%w: transport=%v scope=%v not bindable",
			ErrTransportNotSupported, b.GetTransportKind(), b.GetScopeAuthority())
	}

	switch {
	case b.GetTransportKind() == experiencev1.TransportKind_TRANSPORT_KIND_CONNECT &&
		b.GetScopeAuthority() == experiencev1.ScopeAuthority_SCOPE_AUTHORITY_COMPANY:
		return newConnectStubRepository(b, identity), nil

	case b.GetTransportKind() == experiencev1.TransportKind_TRANSPORT_KIND_REST_OPENAPI &&
		b.GetScopeAuthority() == experiencev1.ScopeAuthority_SCOPE_AUTHORITY_ORG:
		return newAocoreRestStubRepository(b, identity)

	default:
		return nil, fmt.Errorf("%w: transport=%v scope=%v has no impl",
			ErrTransportNotSupported, b.GetTransportKind(), b.GetScopeAuthority())
	}
}

// authorizeScope is the SHARED repo chokepoint both stubs run every method
// through. It confirms the ScopedContext was projected from the bound identity
// AND names a scope that identity is actually granted under the repo's authority
// -- collapsing every failure (wrong company, wrong org, missing, forged) to ONE
// ErrScopeDenied with no existence oracle. The body is NEVER consulted; only the
// verified identity's grants are.
func authorizeScope(identity AoidIdentity, authority experiencev1.ScopeAuthority, sc ScopedContext) error {
	// The context must belong to the same single identity (no second credential).
	if sc.Subject != identity.Subject {
		return ErrScopeDenied
	}
	// The context's authority must match the repo's bound authority.
	if sc.Authority != authority {
		return ErrScopeDenied
	}
	// Re-project from the verified identity: the named scope must be a real grant.
	// This re-runs ResolveScope so a FORGED ScopedContext (right shape, wrong
	// scope id) is refused exactly like a never-authorized one.
	reprojected, err := ResolveScope(identity, authority, sc.ScopeID)
	if err != nil {
		return err // ErrScopeDenied -- no distinction exists/not.
	}
	if reprojected.ScopeID != sc.ScopeID {
		return ErrScopeDenied
	}
	return nil
}
