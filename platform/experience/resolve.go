// resolve.go -- TRD 140-08, must-have #4. The server-side resolution FILTER.
//
// Resolve is "spec REQUESTS, server GRANTS": it returns a resolved spec carrying
// ONLY the surfaces the resolution tuple's entitlement set GRANTS. Every
// referenced surface the tuple does NOT grant becomes a LockedSurface{surface_id,
// upsell_reason} -- so a device can render "locked + upsell" WITHOUT ever holding
// the entitlement RULES. The resolved spec ships the RESULT, never the rules.
//
// SINGLE TENANCY CHOKEPOINT: this function is the one place tenant/org scope is
// enforced for resolution. A resolution whose tuple scope diverges from the
// spec's scope collapses to ErrResolutionDenied -- a SINGLE non-leaking sentinel,
// identical whether the divergent scope "exists for another tenant" or "does not
// exist". This denies a cross-tenant existence oracle, mirroring
// binding.ResolveScope's ErrScopeDenied and navgraph's CoherenceSurfaceNotEntitled.
//
// ENTITLEMENT SINGLE-SOURCE: grants come from the tuple's Granted set -- the
// already-evaluated answer of the single-source entitlements service. Resolve
// NEVER reads verticalpreset.FeatureGates / verticalFeatureProfiles (the inert,
// manually-synced duplicate). A spec can never resolve a surface its tuple is not
// granted.
package experience

import (
	"context"
	"errors"

	experiencev1 "github.com/aocybersystems/eden-platform-go/gen/go/experience/v1"
	"github.com/aocybersystems/eden-platform-go/platform/experience/fixtures"
	"google.golang.org/protobuf/proto"
)

// ErrResolutionDenied is the SINGLE non-leaking outcome of a resolution whose
// tuple scope is not authorized for the spec's scope. It is identical for a
// wrong-tenant request and a nonexistent-tenant request -- no existence oracle.
var ErrResolutionDenied = errors.New("experience: resolution denied")

// lockedUpsellReason is the upsell copy stamped on a LockedSurface. It names the
// surface generically (no entitlement-rule material) -- the device shows it
// verbatim. A non-empty reason is REQUIRED so an ungranted surface is never a
// silent drop.
const lockedUpsellReason = "This feature is not included in your current plan."

// ResolverConfig carries the resolver's provenance stamped onto every resolved
// spec's ResolutionContext. Version defaults to ResolverVersionTag when empty;
// ResolvedAt is an RFC3339 timestamp supplied by the caller (injected so the
// resolver stays a pure function -- deterministic + testable, no wall-clock read).
type ResolverConfig struct {
	Version    string
	ResolvedAt string
}

// Resolve filters spec against the tuple's granted entitlement set and returns a
// resolved spec.
//
//   - Scope chokepoint: if the tuple's tenant/org diverge from the spec's, the
//     result is ErrResolutionDenied (single non-leaking sentinel).
//   - Filter: a referenced surface in tuple.Granted passes; one not granted is
//     dropped from referenced_surface_ids AND emitted as a LockedSurface.
//   - No phantom: a granted surface NOT referenced by the spec never appears
//     (the spec requests; the server cannot mint an unrequested surface).
//   - Stamp: a ResolutionContext{tenant, org, resolved_at, resolver_version} is
//     attached, and content_hash is computed over the resolved spec + tuple.
//
// ctx is accepted for symmetry with the real entitlements-service call path
// (single-source); the grant decision itself is the tuple's already-evaluated set.
func Resolve(
	ctx context.Context,
	spec *experiencev1.ExperienceSpec,
	tuple fixtures.ResolutionTuple,
	cfg ResolverConfig,
) (*experiencev1.ExperienceSpec, error) {
	_ = ctx
	if spec == nil {
		return nil, ErrResolutionDenied
	}

	// (1) Tenancy chokepoint -- the tuple's scope MUST match the spec's scope.
	// Any divergence collapses to the single sentinel. No distinction between
	// "belongs to another tenant/org" and "does not exist".
	if tuple.TenantID != spec.GetTenantId() || tuple.OrgID != spec.GetOrgId() {
		return nil, ErrResolutionDenied
	}

	// Deep-copy so the input spec is never mutated and no rule material can alias.
	resolved := proto.Clone(spec).(*experiencev1.ExperienceSpec)

	// (2) Filter referenced surfaces against the granted set. Granted -> keep;
	// ungranted -> LockedSurface (never a silent drop).
	granted := make([]string, 0, len(resolved.GetReferencedSurfaceIds()))
	locked := make([]*experiencev1.LockedSurface, 0)
	for _, surfaceID := range resolved.GetReferencedSurfaceIds() {
		if _, ok := tuple.Granted[surfaceID]; ok {
			granted = append(granted, surfaceID)
			continue
		}
		locked = append(locked, &experiencev1.LockedSurface{
			SurfaceId:    surfaceID,
			UpsellReason: lockedUpsellReason,
		})
	}
	// (3) No phantom surface: the result carries only what the spec referenced,
	// filtered -- a granted-but-unreferenced surface is simply absent (we never
	// add ids from tuple.Granted that the spec did not reference).
	resolved.ReferencedSurfaceIds = granted
	resolved.LockedSurfaces = locked

	// (4) Stamp provenance. tuple scope == spec scope here (chokepoint passed), so
	// the context records the authoritative resolved scope.
	version := cfg.Version
	if version == "" {
		version = ResolverVersionTag
	}
	resolved.ResolutionContext = &experiencev1.ResolutionContext{
		TenantId:        tuple.TenantID,
		OrgId:           tuple.OrgID,
		ResolvedAt:      cfg.ResolvedAt,
		ResolverVersion: version,
	}

	// (5) Content hash over the resolved spec + the FULL resolution tuple (the
	// irreversible cache-key shape). Stamped last so the hash reflects the final
	// resolved bytes; ContentHash zeroes content_hash internally for a fixed point.
	resolved.ContentHash = ContentHash(resolved, tuple)

	return resolved, nil
}
