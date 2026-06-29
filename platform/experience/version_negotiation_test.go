// version_negotiation_test.go — TRD 140-03, must-have #1.
//
// The three version axes (spec_schema_version / surface_contract_version /
// min_binary_version) are INDEPENDENT (140-01 proved round-trip independence;
// here we prove they DRIVE negotiation independently). A binary compiles in a
// SurfaceRegistryManifest declaring which surfaces it knows and which
// surface_contract_version it speaks. A served spec carries referenced surface
// ids + an UnknownSurfacePolicy + a min_binary_version floor. Negotiate()
// decides render / render-degraded / block-and-prompt-upgrade.
//
// IRREVERSIBLE: once devices cache v1 specs this behavior is frozen. These
// tests LOCK it before any device caches a spec.
//
// Fixtures: built via platform/experience/fixtures (140-02). No hand-built
// literals — NewSpec + options + NewManifest + WrongTenant only.
package experience_test

import (
	"testing"

	experiencev1 "github.com/aocybersystems/eden-platform-go/gen/go/experience/v1"
	"github.com/aocybersystems/eden-platform-go/platform/experience"
	"github.com/aocybersystems/eden-platform-go/platform/experience/fixtures"
)

// binaryVersion the running app reports. Kept high so a default spec renders
// unless a test deliberately raises the spec's floor above it.
const runningBinaryVersion = "1.0.0"

// --- Happy ----------------------------------------------------------------

// A spec whose surfaces are all in the manifest's known_surface_ids and whose
// min_binary_version floor the binary satisfies resolves with NO degradation.
func TestNegotiate_AllKnown_BinarySatisfiesFloor_Renders(t *testing.T) {
	spec := fixtures.NewSpec(
		fixtures.WithReferencedSurfaces("home", "orders"),
		fixtures.WithMinBinaryVersion("1.0.0"),
		fixtures.WithUnknownSurfacePolicy(experiencev1.UnknownSurfacePolicy_IGNORE),
	)
	manifest := fixtures.NewManifest("1.0.0", "home", "orders", "settings")

	res := experience.Negotiate(spec, manifest, runningBinaryVersion)

	if res.Blocked {
		t.Fatalf("all-known spec must not block; reason=%q", res.Reason)
	}
	if len(res.DroppedSurfaces) != 0 {
		t.Errorf("no surfaces should drop; got %v", res.DroppedSurfaces)
	}
	if len(res.DegradedSurfaces) != 0 {
		t.Errorf("no surfaces should degrade; got %v", res.DegradedSurfaces)
	}
	if len(res.RenderedSurfaces) != 2 {
		t.Errorf("both surfaces should render; got %v", res.RenderedSurfaces)
	}
}

// SurfaceRegistryManifest round-trips contract_version + known_surface_ids.
func TestSurfaceRegistryManifest_RoundTrips(t *testing.T) {
	in := fixtures.NewManifest("3.2.1", "home", "orders", "loyalty")

	if got, want := in.GetContractVersion(), "3.2.1"; got != want {
		t.Errorf("contract_version: got %q want %q", got, want)
	}
	if got, want := len(in.GetKnownSurfaceIds()), 3; got != want {
		t.Fatalf("known_surface_ids len: got %d want %d", got, want)
	}
	if in.GetKnownSurfaceIds()[2] != "loyalty" {
		t.Errorf("known_surface_ids[2]: got %q want loyalty", in.GetKnownSurfaceIds()[2])
	}
}

// --- Edge: unknown surface per policy -------------------------------------

// policy=IGNORE → the unknown surface is dropped, the rest renders.
func TestNegotiate_UnknownSurface_Ignore_Drops(t *testing.T) {
	spec := fixtures.NewSpec(
		fixtures.WithReferencedSurfaces("home", "future_surface"),
		fixtures.WithUnknownSurfacePolicy(experiencev1.UnknownSurfacePolicy_IGNORE),
	)
	manifest := fixtures.NewManifest("1.0.0", "home")

	res := experience.Negotiate(spec, manifest, runningBinaryVersion)

	if res.Blocked {
		t.Fatalf("IGNORE must not block; reason=%q", res.Reason)
	}
	if len(res.DroppedSurfaces) != 1 || res.DroppedSurfaces[0] != "future_surface" {
		t.Errorf("future_surface should be dropped; got %v", res.DroppedSurfaces)
	}
	if len(res.RenderedSurfaces) != 1 || res.RenderedSurfaces[0] != "home" {
		t.Errorf("home should still render; got %v", res.RenderedSurfaces)
	}
}

// policy=RENDER_DEGRADED → the unknown surface is kept with a degraded marker.
func TestNegotiate_UnknownSurface_RenderDegraded_Degrades(t *testing.T) {
	spec := fixtures.NewSpec(
		fixtures.WithReferencedSurfaces("home", "future_surface"),
		fixtures.WithUnknownSurfacePolicy(experiencev1.UnknownSurfacePolicy_RENDER_DEGRADED),
	)
	manifest := fixtures.NewManifest("1.0.0", "home")

	res := experience.Negotiate(spec, manifest, runningBinaryVersion)

	if res.Blocked {
		t.Fatalf("RENDER_DEGRADED must not block; reason=%q", res.Reason)
	}
	if len(res.DegradedSurfaces) != 1 || res.DegradedSurfaces[0] != "future_surface" {
		t.Errorf("future_surface should be degraded; got %v", res.DegradedSurfaces)
	}
	if len(res.DroppedSurfaces) != 0 {
		t.Errorf("nothing should drop under RENDER_DEGRADED; got %v", res.DroppedSurfaces)
	}
}

// policy=BLOCK_UPGRADE → negotiation signals the binary to block (typed result,
// not a panic).
func TestNegotiate_UnknownSurface_BlockUpgrade_Blocks(t *testing.T) {
	spec := fixtures.NewSpec(
		fixtures.WithReferencedSurfaces("home", "future_surface"),
		fixtures.WithUnknownSurfacePolicy(experiencev1.UnknownSurfacePolicy_BLOCK_UPGRADE),
	)
	manifest := fixtures.NewManifest("1.0.0", "home")

	res := experience.Negotiate(spec, manifest, runningBinaryVersion)

	if !res.Blocked {
		t.Fatalf("BLOCK_UPGRADE on an unknown surface must block")
	}
	if res.Reason == "" {
		t.Errorf("blocked result must carry a non-empty reason")
	}
}

// UNSPECIFIED policy is treated as the safe default (BLOCK_UPGRADE) so an
// unset policy can never silently render a surface the binary cannot handle.
func TestNegotiate_UnknownSurface_UnspecifiedPolicy_BlocksSafely(t *testing.T) {
	spec := fixtures.NewSpec(
		fixtures.WithReferencedSurfaces("home", "future_surface"),
		fixtures.WithUnknownSurfacePolicy(experiencev1.UnknownSurfacePolicy_UNKNOWN_SURFACE_POLICY_UNSPECIFIED),
	)
	manifest := fixtures.NewManifest("1.0.0", "home")

	res := experience.Negotiate(spec, manifest, runningBinaryVersion)

	if !res.Blocked {
		t.Fatalf("UNSPECIFIED policy must fail safe (block), not silently render")
	}
}

// --- Edge: min_binary_version floor ---------------------------------------

// Spec min_binary_version newer than the binary's reported version → blocks
// per policy regardless of surface knowledge (all surfaces known here).
func TestNegotiate_MinBinaryVersionTooNew_Blocks(t *testing.T) {
	spec := fixtures.NewSpec(
		fixtures.WithReferencedSurfaces("home"),
		fixtures.WithMinBinaryVersion("2.0.0"), // floor above the running 1.0.0
		fixtures.WithUnknownSurfacePolicy(experiencev1.UnknownSurfacePolicy_IGNORE),
	)
	manifest := fixtures.NewManifest("1.0.0", "home")

	res := experience.Negotiate(spec, manifest, runningBinaryVersion)

	if !res.Blocked {
		t.Fatalf("a spec demanding binary 2.0.0 on a 1.0.0 binary must block (upgrade prompt)")
	}
	if res.Reason == "" {
		t.Errorf("min-binary block must carry a reason for the upgrade prompt")
	}
}

// A binary AT the floor satisfies it (>= semantics, not strictly greater).
func TestNegotiate_BinaryExactlyAtFloor_Renders(t *testing.T) {
	spec := fixtures.NewSpec(
		fixtures.WithReferencedSurfaces("home"),
		fixtures.WithMinBinaryVersion("1.0.0"),
		fixtures.WithUnknownSurfacePolicy(experiencev1.UnknownSurfacePolicy_IGNORE),
	)
	manifest := fixtures.NewManifest("1.0.0", "home")

	res := experience.Negotiate(spec, manifest, "1.0.0")

	if res.Blocked {
		t.Fatalf("binary exactly at the floor must render, not block; reason=%q", res.Reason)
	}
}

// --- Axis independence ----------------------------------------------------

// Bumping spec_schema_version does NOT change surface_contract_version
// negotiation: with surfaces all-known and the floor satisfied, a schema bump
// alone never blocks/degrades. (The axes are orthogonal — 140-01.)
func TestNegotiate_SpecSchemaBump_DoesNotAffectSurfaceNegotiation(t *testing.T) {
	manifest := fixtures.NewManifest("1.0.0", "home", "orders")

	base := fixtures.NewSpec(
		fixtures.WithReferencedSurfaces("home", "orders"),
		fixtures.WithUnknownSurfacePolicy(experiencev1.UnknownSurfacePolicy_BLOCK_UPGRADE),
	)
	base.SpecSchemaVersion = "1.0.0"

	bumped := fixtures.NewSpec(
		fixtures.WithReferencedSurfaces("home", "orders"),
		fixtures.WithUnknownSurfacePolicy(experiencev1.UnknownSurfacePolicy_BLOCK_UPGRADE),
	)
	bumped.SpecSchemaVersion = "9.9.9" // bump ONLY the schema axis

	rb := experience.Negotiate(base, manifest, runningBinaryVersion)
	rp := experience.Negotiate(bumped, manifest, runningBinaryVersion)

	if rb.Blocked || rp.Blocked {
		t.Fatalf("neither should block (all surfaces known, floor met); base=%v bumped=%v", rb.Blocked, rp.Blocked)
	}
	if len(rb.RenderedSurfaces) != len(rp.RenderedSurfaces) {
		t.Errorf("a spec_schema_version bump changed surface negotiation: base=%v bumped=%v",
			rb.RenderedSurfaces, rp.RenderedSurfaces)
	}
}

// --- Failure / wrong-tenant ------------------------------------------------

// Wrong-tenant: negotiation must scope strictly to the requesting tenant. The
// result must never carry a different tenant's surfaces, and the tenant/org
// fields must survive negotiation UNTOUCHED so 140-08 can enforce isolation.
// A cross-tenant spec collapses to the same not-entitled outcome as an unknown
// surface — no oracle distinguishing "exists for another tenant" from "doesn't
// exist".
func TestNegotiate_WrongTenant_ScopesToRequesterAndPreservesScope(t *testing.T) {
	baseline := fixtures.NewSpec(
		fixtures.WithReferencedSurfaces("home", "orders"),
		fixtures.WithUnknownSurfacePolicy(experiencev1.UnknownSurfacePolicy_IGNORE),
	)
	wrong := fixtures.WrongTenant(baseline)

	manifest := fixtures.NewManifest("1.0.0", "home", "orders")

	res := experience.Negotiate(wrong, manifest, runningBinaryVersion)

	// Scope fields survive negotiation untouched (140-08 enforces; we preserve).
	if res.TenantID != wrong.GetTenantId() {
		t.Errorf("negotiation must preserve the requesting tenant_id untouched: got %q want %q",
			res.TenantID, wrong.GetTenantId())
	}
	if res.OrgID != wrong.GetOrgId() {
		t.Errorf("negotiation must preserve the requesting org_id untouched: got %q want %q",
			res.OrgID, wrong.GetOrgId())
	}
	// Must not echo the baseline (other tenant's) scope — no cross-tenant bleed.
	if res.TenantID == baseline.GetTenantId() {
		t.Errorf("negotiation leaked the baseline tenant's scope into a wrong-tenant request")
	}
}
