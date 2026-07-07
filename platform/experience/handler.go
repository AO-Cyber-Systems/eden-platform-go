// handler.go -- TRD 140-09, must-have (M0 ExperienceService handler).
//
// The ExperienceService Connect handler: the server surface every consumer
// calls. It is a THIN adapter that COMPOSES the already-tested pieces:
//   - StoreSpec    -> SpecStore.Put under the PRINCIPAL scope (140-09 store).
//   - ResolveSpec  -> Resolve (140-08 filter + content hash) over the tuple the
//                     EntitlementsProvider grants the principal scope.
//   - ValidateSpec -> ValidateNavGraph (140-05 coherence) + Negotiate (140-03
//                     version) under the principal scope.
//
// TENANCY CHOKEPOINT: every method derives its scope from the AUTHENTICATED
// principal (principalScopeFromContext), NEVER from the request body. StoreSpec
// OVERWRITES the body spec's tenant_id/org_id with the principal scope before
// persisting -- a body value can never plant a spec under another tenant. The
// store is scope-keyed, and Resolve re-checks scope; both a store miss and a
// resolve-scope divergence map to the SAME permission-denied sentinel
// (errDenied) with an IDENTICAL message -- no existence oracle.
package experience

import (
	"context"
	"errors"

	connect "connectrpc.com/connect"
	experiencev1 "github.com/aocybersystems/eden-platform-go/gen/go/experience/v1"
	"github.com/aocybersystems/eden-platform-go/platform/experience/fixtures"
)

// errDenied is the SINGLE non-leaking message returned for EVERY scope failure:
// spec-not-found, wrong-tenant, wrong-org. Identical text + code so a caller can
// never distinguish "exists for another tenant" from "does not exist".
var errDenied = errors.New("experience: spec not found or not permitted for this scope")

// EntitlementsProvider yields the resolution tuple (incl. the GRANTED surface
// set) for a principal scope + role + form_factor. It is the single-source
// entitlements answer the resolver filters against -- injected so the handler
// stays decoupled from the real entitlements service (swap-stable for obj 14x).
type EntitlementsProvider interface {
	TupleFor(scope PrincipalScope, role, formFactor string) fixtures.ResolutionTuple
}

// EntitlementsFunc adapts a func to EntitlementsProvider.
type EntitlementsFunc func(scope PrincipalScope, role, formFactor string) fixtures.ResolutionTuple

// TupleFor calls f.
func (f EntitlementsFunc) TupleFor(scope PrincipalScope, role, formFactor string) fixtures.ResolutionTuple {
	return f(scope, role, formFactor)
}

// ExperienceHandler implements experiencev1connect.ExperienceServiceHandler.
type ExperienceHandler struct {
	store        SpecStore
	entitlements EntitlementsProvider
	// now supplies the RFC3339 resolved_at stamp; injected for deterministic
	// content hashes in tests (the resolver itself stays a pure function).
	now func() string
}

// HandlerOption configures an ExperienceHandler.
type HandlerOption func(*ExperienceHandler)

// WithClock injects the resolved_at clock (RFC3339 string). Defaults to a fixed
// stamp when unset so a hash is always reproducible; production wiring passes a
// real clock.
func WithClock(now func() string) HandlerOption {
	return func(h *ExperienceHandler) { h.now = now }
}

// NewExperienceHandler builds the handler over a store + entitlements provider.
func NewExperienceHandler(store SpecStore, entitlements EntitlementsProvider, opts ...HandlerOption) *ExperienceHandler {
	h := &ExperienceHandler{
		store:        store,
		entitlements: entitlements,
		now:          func() string { return "" },
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// StoreSpec persists the app definition under the PRINCIPAL scope. The body's
// nested spec tenant_id/org_id are OVERWRITTEN by the principal scope -- a body
// value cannot plant a spec under another tenant.
func (h *ExperienceHandler) StoreSpec(
	ctx context.Context,
	req *connect.Request[experiencev1.StoreSpecRequest],
) (*connect.Response[experiencev1.StoreSpecResponse], error) {
	scope, ok := principalScopeFromContext(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, nil)
	}

	def := req.Msg.GetAppDefinition()
	if def == nil || def.GetSpec() == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("experience: app_definition with spec is required"))
	}
	if def.GetId() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("experience: app_definition.id is required"))
	}

	// AUTHORITY OVERWRITE: stamp the principal scope onto the spec BEFORE storing.
	// Whatever tenant_id/org_id the body carried is discarded -- identity wins.
	def.Spec.TenantId = scope.TenantID
	def.Spec.OrgId = scope.OrgID

	h.store.Put(scope, def)

	return connect.NewResponse(&experiencev1.StoreSpecResponse{
		AppDefId:          def.GetId(),
		ContentHash:       def.GetSpec().GetContentHash(),
		SpecSchemaVersion: def.GetSpec().GetSpecSchemaVersion(),
	}), nil
}

// ResolveSpec resolves a stored spec for the principal scope. It loads the spec
// from the scope-keyed store (a cross-tenant load misses), then runs the 140-08
// Resolve filter over the principal's granted tuple. BOTH a store miss and a
// resolve-scope divergence map to the SAME permission-denied sentinel -- no
// existence oracle, identical to navgraph/binding/resolve single-sentinel
// contracts.
func (h *ExperienceHandler) ResolveSpec(
	ctx context.Context,
	req *connect.Request[experiencev1.ResolveSpecRequest],
) (*connect.Response[experiencev1.ResolveSpecResponse], error) {
	scope, ok := principalScopeFromContext(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, nil)
	}

	// Scope-keyed load: a spec stored under another tenant/org simply misses.
	def, found := h.store.Get(scope, req.Msg.GetAppDefId())
	if !found {
		return nil, connect.NewError(connect.CodePermissionDenied, errDenied)
	}

	// Build the resolution tuple the entitlements service grants this principal
	// scope + the requested role/form_factor (the non-authority axes).
	tuple := h.entitlements.TupleFor(scope, req.Msg.GetRole(), req.Msg.GetFormFactor())

	// Compose 140-08 Resolve: filters referenced surfaces against the granted set
	// + stamps ResolutionContext + content hash. Resolve re-enforces the scope
	// chokepoint (tuple scope must match spec scope) -- a divergence collapses to
	// ErrResolutionDenied, which we map to the SAME sentinel as a store miss.
	resolved, err := Resolve(ctx, def.GetSpec(), tuple, ResolverConfig{ResolvedAt: h.now()})
	if err != nil {
		// errors.Is(ErrResolutionDenied) -> denied; any other error is internal.
		if errors.Is(err, ErrResolutionDenied) {
			return nil, connect.NewError(connect.CodePermissionDenied, errDenied)
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&experiencev1.ResolveSpecResponse{ResolvedSpec: resolved}), nil
}

// ValidateSpec runs the 140-05 nav coherence + 140-03 version checks on the
// supplied spec under the principal scope. It NEVER short-circuits -- every
// problem accumulates so the builder sees them all at once. Scope is stamped
// from the principal (the body cannot claim another tenant's scope for the
// entitled-surface check).
func (h *ExperienceHandler) ValidateSpec(
	ctx context.Context,
	req *connect.Request[experiencev1.ValidateSpecRequest],
) (*connect.Response[experiencev1.ValidateSpecResponse], error) {
	scope, ok := principalScopeFromContext(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, nil)
	}

	def := req.Msg.GetAppDefinition()
	if def == nil || def.GetSpec() == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("experience: app_definition with spec is required"))
	}
	spec := def.GetSpec()
	// Stamp the principal scope -- validation reasons over the requesting scope.
	spec.TenantId = scope.TenantID
	spec.OrgId = scope.OrgID

	tuple := h.entitlements.TupleFor(scope, "", "")

	var problems []*experiencev1.ValidationProblem

	// (1) 140-05 nav coherence -- machine-checked rules over the granted set.
	for _, ce := range ValidateNavGraph(spec.GetNavGraph(), tuple.Granted) {
		problems = append(problems, &experiencev1.ValidationProblem{
			Code:      string(ce.Code),
			SurfaceId: ce.SurfaceID,
			Message:   ce.Message,
		})
	}

	// (2) 140-03 version negotiation -- a spec that would BLOCK the binary it
	// targets (min_binary_version unmet, or an unknown surface under a blocking
	// policy) is a validation problem too. We negotiate against a manifest that
	// knows the GRANTED surfaces (the surfaces a conformant binary would carry)
	// at the spec's own min_binary_version floor.
	manifest := grantedManifest(spec, tuple)
	if neg := Negotiate(spec, manifest, spec.GetMinBinaryVersion()); neg.Blocked {
		problems = append(problems, &experiencev1.ValidationProblem{
			Code:    "version.block_upgrade",
			Message: neg.Reason,
		})
	}

	return connect.NewResponse(&experiencev1.ValidateSpecResponse{
		Valid:    len(problems) == 0,
		Problems: problems,
	}), nil
}

// grantedManifest builds the SurfaceRegistryManifest a CONFORMANT binary would
// compile in for this spec: the spec's surface contract version + the granted
// surfaces. Negotiating against it makes ValidateSpec flag an unknown-surface or
// floor violation the same way a real binary would (140-03 wired).
func grantedManifest(spec *experiencev1.ExperienceSpec, tuple fixtures.ResolutionTuple) *experiencev1.SurfaceRegistryManifest {
	known := make([]string, 0, len(tuple.Granted))
	for id := range tuple.Granted {
		known = append(known, id)
	}
	return &experiencev1.SurfaceRegistryManifest{
		ContractVersion: spec.GetSurfaceContractVersion(),
		KnownSurfaceIds: known,
	}
}
