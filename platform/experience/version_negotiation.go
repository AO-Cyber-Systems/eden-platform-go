// Package experience hosts the experience.v1 contract logic. This file
// implements MUST-HAVE #1: version negotiation between a served, resolved
// ExperienceSpec and the binary that must render it.
//
// THE THREE VERSION AXES ARE INDEPENDENT:
//   - spec_schema_version       — schema of the ExperienceSpec message itself.
//   - surface_contract_version  — the FeatureSurface contract the spec targets.
//   - min_binary_version        — the floor app-binary version that can render.
//
// Negotiate consumes min_binary_version + the referenced surfaces + the binary's
// compiled-in SurfaceRegistryManifest; it deliberately does NOT branch on
// spec_schema_version (orthogonality — a schema bump must not change surface
// negotiation).
//
// IRREVERSIBLE: once a device caches a v1 spec, this behavior is frozen. The
// negotiation here is the contract every future binary obeys.
package experience

import (
	"fmt"
	"strconv"
	"strings"

	experiencev1 "github.com/aocybersystems/eden-platform-go/gen/go/experience/v1"
)

// NegotiationResult is the typed outcome of negotiating a resolved spec against
// a running binary + its compiled-in manifest. It is never a panic and never an
// error — a binary that cannot render a spec gets a structured Blocked result it
// can turn into an upgrade prompt.
type NegotiationResult struct {
	// RenderedSurfaces are referenced surfaces the binary knows and will render.
	RenderedSurfaces []string
	// DroppedSurfaces are unknown surfaces silently dropped (UnknownSurfacePolicy IGNORE).
	DroppedSurfaces []string
	// DegradedSurfaces are unknown surfaces kept with a degraded marker
	// (UnknownSurfacePolicy RENDER_DEGRADED).
	DegradedSurfaces []string
	// Blocked is true when the binary must NOT render and should prompt upgrade
	// (min_binary_version unmet, or an unknown surface under BLOCK_UPGRADE /
	// the fail-safe UNSPECIFIED policy).
	Blocked bool
	// Reason is a human/telemetry-readable explanation when Blocked is true.
	Reason string

	// TenantID / OrgID are carried THROUGH untouched from the requesting spec.
	// Negotiation never substitutes another tenant's scope; 140-08/09 enforce
	// isolation. Preserving them here lets the caller assert the result is
	// scoped to the requester only (no cross-tenant bleed).
	TenantID string
	OrgID    string
}

// Negotiate decides render / render-degraded / block for a resolved spec given
// the running binary version and the binary's compiled-in manifest.
//
// Order of checks:
//  1. min_binary_version floor — if the binary is below it, BLOCK regardless of
//     surface knowledge (an upgrade is required before any surface is safe).
//  2. per-surface knowledge — each referenced surface not in the manifest's
//     known_surface_ids is handled per the spec's UnknownSurfacePolicy.
//
// The result always carries the requesting spec's tenant_id / org_id untouched.
func Negotiate(
	spec *experiencev1.ExperienceSpec,
	manifest *experiencev1.SurfaceRegistryManifest,
	binaryVersion string,
) NegotiationResult {
	res := NegotiationResult{
		TenantID: spec.GetTenantId(),
		OrgID:    spec.GetOrgId(),
	}

	// (1) min_binary_version floor. A spec demanding a newer binary than the one
	// running must block — never attempt to render a surface the binary may not
	// understand. This is the upgrade-prompt path.
	floor := spec.GetMinBinaryVersion()
	if floor != "" && compareVersions(binaryVersion, floor) < 0 {
		res.Blocked = true
		res.Reason = fmt.Sprintf(
			"binary %s is below the spec's min_binary_version %s; upgrade required",
			binaryVersion, floor,
		)
		return res
	}

	// (2) per-surface knowledge negotiation.
	known := make(map[string]struct{}, len(manifest.GetKnownSurfaceIds()))
	for _, id := range manifest.GetKnownSurfaceIds() {
		known[id] = struct{}{}
	}

	policy := spec.GetUnknownSurfacePolicy()
	for _, id := range spec.GetReferencedSurfaceIds() {
		if _, ok := known[id]; ok {
			res.RenderedSurfaces = append(res.RenderedSurfaces, id)
			continue
		}
		// Unknown surface — apply the spec's policy.
		switch policy {
		case experiencev1.UnknownSurfacePolicy_UNKNOWN_SURFACE_POLICY_IGNORE:
			res.DroppedSurfaces = append(res.DroppedSurfaces, id)
		case experiencev1.UnknownSurfacePolicy_UNKNOWN_SURFACE_POLICY_RENDER_DEGRADED:
			res.DegradedSurfaces = append(res.DegradedSurfaces, id)
		case experiencev1.UnknownSurfacePolicy_UNKNOWN_SURFACE_POLICY_BLOCK_UPGRADE:
			res.Blocked = true
			res.Reason = fmt.Sprintf(
				"spec references unknown surface %q and policy is BLOCK_UPGRADE; upgrade required", id,
			)
			return res
		default:
			// UNSPECIFIED (and any unrecognized future value) FAILS SAFE: block
			// rather than silently render a surface the binary cannot handle.
			res.Blocked = true
			res.Reason = fmt.Sprintf(
				"spec references unknown surface %q and unknown_surface_policy is unspecified; failing safe (block)", id,
			)
			return res
		}
	}

	return res
}

// compareVersions compares two dotted numeric version strings (e.g. "1.2.0").
// Returns -1 if a < b, 0 if equal, +1 if a > b. Missing trailing components are
// treated as 0 ("1.0" == "1.0.0"). Non-numeric / malformed components compare as
// 0 for that position (defensive — never panics on a bad served value).
//
// Self-contained on purpose: the frozen-contract negotiation must not couple its
// behavior to an external semver lib's prefix/pre-release rules. min_binary_version
// is a plain dotted release floor.
func compareVersions(a, b string) int {
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		av := componentAt(as, i)
		bv := componentAt(bs, i)
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}

func componentAt(parts []string, i int) int {
	if i >= len(parts) {
		return 0
	}
	v, err := strconv.Atoi(strings.TrimSpace(parts[i]))
	if err != nil {
		return 0
	}
	return v
}
