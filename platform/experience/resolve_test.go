// resolve_test.go -- TRD 140-08, must-have #4.
//
// Server-side resolution FILTER + content-hash preimage + LockedSurface.
//
// Resolve(spec, tuple) is "spec REQUESTS, server GRANTS": the resolved spec
// carries ONLY surfaces the tuple's entitlement set grants; every ungranted
// referenced surface becomes a LockedSurface{surface_id, upsell_reason} -- so a
// device shows "locked" WITHOUT ever holding the entitlement RULES. The resolver
// stamps a ResolutionContext{tenant, org, resolved_at, resolver_version}.
//
// ContentHash(resolvedSpec, tuple) puts the FULL resolution tuple {role,
// entitlements, form_factor, tenant, org} IN THE PREIMAGE -- the irreversible
// cache-key shape: two distinct tuples MUST yield distinct hashes; an identical
// tuple MUST yield an identical deterministic hash. This is test-locked BEFORE
// devices cache specs because the preimage shape can never change afterward.
//
// Wrong-tenant (PRIME): resolving tenant A's tuple over a spec NEVER surfaces
// tenant B's granted surfaces, and a cross-tenant resolve collapses to a single
// non-leaking denial -- identical outcome for wrong-tenant vs nonexistent (no
// existence oracle). Cross-org variant asserted identically.
//
// Fixtures only -- NewSpec + WithReferencedSurfaces + NewTuple/WithResolutionTuple/
// WithGrantedEntitlements + WrongTenant/WrongTenantTuple. No hand literals.
package experience_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	experiencev1 "github.com/aocybersystems/eden-platform-go/gen/go/experience/v1"
	"github.com/aocybersystems/eden-platform-go/platform/experience"
	"github.com/aocybersystems/eden-platform-go/platform/experience/fixtures"
	"google.golang.org/protobuf/proto"
)

// resolverVersionFixture is the resolver build id the tests stamp + assert.
const resolverVersionFixture = "experience-resolver-test-1"

// specArg is the resolver input/output spec type, aliased for terse helper
// signatures below.
type specArg = *experiencev1.ExperienceSpec

// --- Happy ----------------------------------------------------------------

// Resolve returns only surfaces the tuple's entitlements grant; an ungranted
// referenced surface becomes a LockedSurface with a non-empty upsell_reason.
func TestResolve_FiltersUngrantedIntoLockedSurfaces(t *testing.T) {
	spec := fixtures.NewSpec(
		fixtures.WithReferencedSurfaces("invoices", "scheduling", "payroll"),
	)
	tuple := fixtures.NewTuple(
		fixtures.WithGrantedEntitlements("invoices", "scheduling"),
	)

	resolved, err := experience.Resolve(
		context.Background(), spec, tuple,
		experience.ResolverConfig{Version: resolverVersionFixture, ResolvedAt: "2026-06-29T00:00:00Z"},
	)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// Granted surfaces pass through.
	got := resolved.GetReferencedSurfaceIds()
	if !containsAll(got, "invoices", "scheduling") {
		t.Fatalf("granted surfaces missing from result: %v", got)
	}
	// Ungranted surface is FILTERED OUT of the referenced set.
	if contains(got, "payroll") {
		t.Fatalf("ungranted surface 'payroll' leaked into resolved referenced surfaces: %v", got)
	}
	// ...and appears as a LockedSurface WITH an upsell reason -- not silently dropped.
	locked := resolved.GetLockedSurfaces()
	if len(locked) != 1 {
		t.Fatalf("want exactly 1 locked surface, got %d: %+v", len(locked), locked)
	}
	if locked[0].GetSurfaceId() != "payroll" {
		t.Fatalf("locked surface id = %q, want payroll", locked[0].GetSurfaceId())
	}
	if strings.TrimSpace(locked[0].GetUpsellReason()) == "" {
		t.Fatalf("locked surface has empty upsell_reason")
	}
}

// The resolver stamps a ResolutionContext with the tuple's tenant/org + the
// configured resolver version + resolved_at.
func TestResolve_StampsResolutionContext(t *testing.T) {
	spec := fixtures.NewSpec(fixtures.WithReferencedSurfaces("invoices"))
	tuple := fixtures.NewTuple(
		fixtures.WithResolutionTuple("role-tech", "ent-pro", "form-factor-pos", "tenant-stamp", "org-stamp"),
		fixtures.WithGrantedEntitlements("invoices"),
	)

	resolved, err := experience.Resolve(
		context.Background(), spec, tuple,
		experience.ResolverConfig{Version: resolverVersionFixture, ResolvedAt: "2026-06-29T12:34:56Z"},
	)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	rc := resolved.GetResolutionContext()
	if rc == nil {
		t.Fatal("resolved spec has no ResolutionContext")
	}
	if rc.GetTenantId() != "tenant-stamp" || rc.GetOrgId() != "org-stamp" {
		t.Fatalf("ResolutionContext scope = (%q,%q), want (tenant-stamp,org-stamp)", rc.GetTenantId(), rc.GetOrgId())
	}
	if rc.GetResolverVersion() != resolverVersionFixture {
		t.Fatalf("resolver_version = %q, want %q", rc.GetResolverVersion(), resolverVersionFixture)
	}
	if rc.GetResolvedAt() != "2026-06-29T12:34:56Z" {
		t.Fatalf("resolved_at = %q, want stamped value", rc.GetResolvedAt())
	}
}

// A surface GRANTED by entitlements but NOT present in the spec is absent from
// the result -- no phantom surface (the spec requests; the server cannot mint a
// surface the spec never referenced).
func TestResolve_GrantedButNotInSpec_NoPhantomSurface(t *testing.T) {
	spec := fixtures.NewSpec(fixtures.WithReferencedSurfaces("invoices"))
	tuple := fixtures.NewTuple(
		fixtures.WithGrantedEntitlements("invoices", "ghost-surface-not-in-spec"),
	)

	resolved, err := experience.Resolve(
		context.Background(), spec, tuple,
		experience.ResolverConfig{Version: resolverVersionFixture, ResolvedAt: "2026-06-29T00:00:00Z"},
	)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if contains(resolved.GetReferencedSurfaceIds(), "ghost-surface-not-in-spec") {
		t.Fatalf("phantom surface appeared in result: %v", resolved.GetReferencedSurfaceIds())
	}
}

// content_hash is deterministic: the same spec + same tuple yields an identical
// hash across independent resolves (different process invocations would too --
// the encoding is canonical bytes -> sha256 hex).
func TestContentHash_DeterministicForIdenticalInputs(t *testing.T) {
	spec := fixtures.NewSpec(fixtures.WithReferencedSurfaces("invoices", "scheduling"))
	tuple := fixtures.NewTuple(fixtures.WithGrantedEntitlements("invoices", "scheduling"))

	h1 := mustResolveHash(t, spec, tuple)
	h2 := mustResolveHash(t, fixtures.NewSpec(fixtures.WithReferencedSurfaces("invoices", "scheduling")),
		fixtures.NewTuple(fixtures.WithGrantedEntitlements("invoices", "scheduling")))

	if h1 == "" {
		t.Fatal("content hash is empty")
	}
	if h1 != h2 {
		t.Fatalf("identical inputs produced different hashes:\n  %s\n  %s", h1, h2)
	}
	// And the resolved spec carries it (rollback id + cache key).
	resolved, _ := experience.Resolve(context.Background(), spec, tuple,
		experience.ResolverConfig{Version: resolverVersionFixture, ResolvedAt: "2026-06-29T00:00:00Z"})
	if resolved.GetContentHash() != h1 {
		t.Fatalf("resolved spec content_hash = %q, want stamped hash %q", resolved.GetContentHash(), h1)
	}
}

// --- Edge: tuple is in the preimage (the irreversible cache-key proof) -----

// Changing ANY single tuple axis -- role | entitlements | form_factor | tenant |
// org -- changes the content hash. This is the preimage proof: distinct tuples
// MUST collide-freely map to distinct hashes.
func TestContentHash_EveryTupleAxisChangesHash(t *testing.T) {
	base := fixtures.NewTuple(
		fixtures.WithResolutionTuple("role-a", "ent-a", "ff-a", "tenant-a", "org-a"),
		fixtures.WithGrantedEntitlements("invoices"),
	)
	spec := func() specArg { return fixtures.NewSpec(fixtures.WithReferencedSurfaces("invoices")) }
	baseHash := mustResolveHash(t, spec(), base)

	axes := []struct {
		name  string
		tuple fixtures.ResolutionTuple
	}{
		{"role", withTupleAxis(base, "role-DIFFERENT", "ent-a", "ff-a", "tenant-a", "org-a")},
		{"entitlements", withTupleAxis(base, "role-a", "ent-DIFFERENT", "ff-a", "tenant-a", "org-a")},
		{"form_factor", withTupleAxis(base, "role-a", "ent-a", "ff-DIFFERENT", "tenant-a", "org-a")},
		{"tenant", withTupleAxis(base, "role-a", "ent-a", "ff-a", "tenant-DIFFERENT", "org-a")},
		{"org", withTupleAxis(base, "role-a", "ent-a", "ff-a", "tenant-a", "org-DIFFERENT")},
	}
	for _, ax := range axes {
		h := mustResolveHash(t, spec(), ax.tuple)
		if h == baseHash {
			t.Fatalf("changing tuple axis %q did NOT change the content hash -- axis is missing from the preimage", ax.name)
		}
	}
}

// The entitlement RULES never appear in the resolved output -- the serialized
// resolved spec contains only the filtered RESULT (granted surfaces + locked
// surfaces + reasons + context), never the granting rule material.
func TestResolve_EntitlementRulesNotSerializedIntoOutput(t *testing.T) {
	const secretRule = "RULE-PLAN-PRO-GRANTS-PAYROLL-IF-SEATS-GT-5"
	spec := fixtures.NewSpec(fixtures.WithReferencedSurfaces("invoices", "payroll"))
	tuple := fixtures.NewTuple(
		fixtures.WithResolutionTuple("role-owner", secretRule, "ff", "tenant-r", "org-r"),
		fixtures.WithGrantedEntitlements("invoices"),
	)

	resolved, err := experience.Resolve(context.Background(), spec, tuple,
		experience.ResolverConfig{Version: resolverVersionFixture, ResolvedAt: "2026-06-29T00:00:00Z"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	raw, err := proto.Marshal(resolved)
	if err != nil {
		t.Fatalf("marshal resolved: %v", err)
	}
	if strings.Contains(string(raw), secretRule) {
		t.Fatal("entitlement RULE material leaked into the serialized resolved spec")
	}
}

// --- Failure / wrong-tenant (PRIME) ---------------------------------------

// Resolving tenant A's spec/tuple never surfaces tenant B's granted surfaces.
// We grant a surface ONLY to B's tuple, resolve A's spec with A's tuple, and
// assert B's surface is locked (not granted) for A -- and that a cross-tenant
// resolve (A's tuple over B's spec) collapses to the single non-leaking denial.
func TestResolve_WrongTenant_NoCrossTenantGrant(t *testing.T) {
	// A's spec references payroll; A's tuple does NOT grant it.
	specA := fixtures.NewSpec(fixtures.WithReferencedSurfaces("invoices", "payroll"))
	tupleA := fixtures.NewTuple(fixtures.WithGrantedEntitlements("invoices"))

	resolvedA, err := experience.Resolve(context.Background(), specA, tupleA,
		experience.ResolverConfig{Version: resolverVersionFixture, ResolvedAt: "2026-06-29T00:00:00Z"})
	if err != nil {
		t.Fatalf("Resolve A: %v", err)
	}
	// payroll is locked for A even though SOME other tenant may grant it.
	if contains(resolvedA.GetReferencedSurfaceIds(), "payroll") {
		t.Fatal("payroll granted to A despite A's entitlements not granting it")
	}
	if !lockedHas(resolvedA, "payroll") {
		t.Fatal("payroll not surfaced as locked for A")
	}

	// Cross-tenant: A's tuple resolving B's (wrong-tenant) spec must be DENIED
	// with the single non-leaking sentinel -- the scope axes diverge.
	specB := fixtures.WrongTenant(specA)
	_, err = experience.Resolve(context.Background(), specB, tupleA,
		experience.ResolverConfig{Version: resolverVersionFixture, ResolvedAt: "2026-06-29T00:00:00Z"})
	if !errors.Is(err, experience.ErrResolutionDenied) {
		t.Fatalf("cross-tenant resolve: err = %v, want ErrResolutionDenied", err)
	}
}

// No existence oracle: a wrong-tenant resolve and a resolve against a spec with
// a wholly nonexistent tenant produce the IDENTICAL error -- a probe cannot
// distinguish "exists for another tenant" from "does not exist".
func TestResolve_WrongTenant_NoExistenceOracle(t *testing.T) {
	specA := fixtures.NewSpec(fixtures.WithReferencedSurfaces("invoices"))
	tupleA := fixtures.NewTuple(fixtures.WithGrantedEntitlements("invoices"))

	wrongTenantSpec := fixtures.WrongTenant(specA)
	_, errWrong := experience.Resolve(context.Background(), wrongTenantSpec, tupleA,
		experience.ResolverConfig{Version: resolverVersionFixture, ResolvedAt: "2026-06-29T00:00:00Z"})

	nonexistentSpec := fixtures.NewSpec(
		fixtures.WithReferencedSurfaces("invoices"),
		fixtures.WithTenant("tenant-that-never-existed"),
		fixtures.WithOrg("org-that-never-existed"),
	)
	_, errNonexistent := experience.Resolve(context.Background(), nonexistentSpec, tupleA,
		experience.ResolverConfig{Version: resolverVersionFixture, ResolvedAt: "2026-06-29T00:00:00Z"})

	if !errors.Is(errWrong, experience.ErrResolutionDenied) || !errors.Is(errNonexistent, experience.ErrResolutionDenied) {
		t.Fatalf("want both ErrResolutionDenied, got wrong=%v nonexistent=%v", errWrong, errNonexistent)
	}
	if errWrong.Error() != errNonexistent.Error() {
		t.Fatalf("existence oracle: wrong-tenant err %q != nonexistent err %q", errWrong.Error(), errNonexistent.Error())
	}
}

// Cross-org variant: the ORG scope axis is enforced identically -- an org-diverged
// tuple over a spec collapses to the SAME non-leaking denial as the tenant case.
func TestResolve_CrossOrg_NonLeakingDenial(t *testing.T) {
	spec := fixtures.NewSpec(fixtures.WithReferencedSurfaces("invoices"))
	tuple := fixtures.NewTuple(fixtures.WithGrantedEntitlements("invoices"))

	// Diverge ONLY the org axis of the tuple from the spec's org.
	crossOrgTuple := fixtures.NewTuple(
		fixtures.WithResolutionTuple(tuple.Role, tuple.Entitlement, tuple.FormFactor, spec.GetTenantId(), "org-some-other"),
		fixtures.WithGrantedEntitlements("invoices"),
	)
	_, err := experience.Resolve(context.Background(), spec, crossOrgTuple,
		experience.ResolverConfig{Version: resolverVersionFixture, ResolvedAt: "2026-06-29T00:00:00Z"})
	if !errors.Is(err, experience.ErrResolutionDenied) {
		t.Fatalf("cross-org resolve: err = %v, want ErrResolutionDenied", err)
	}
}

// --- helpers --------------------------------------------------------------

func mustResolveHash(t *testing.T, spec specArg, tuple fixtures.ResolutionTuple) string {
	t.Helper()
	resolved, err := experience.Resolve(context.Background(), spec, tuple,
		experience.ResolverConfig{Version: resolverVersionFixture, ResolvedAt: "2026-06-29T00:00:00Z"})
	if err != nil {
		t.Fatalf("Resolve for hash: %v", err)
	}
	return experience.ContentHash(resolved, tuple)
}

func withTupleAxis(base fixtures.ResolutionTuple, role, ent, ff, tenant, org string) fixtures.ResolutionTuple {
	return fixtures.NewTuple(
		fixtures.WithResolutionTuple(role, ent, ff, tenant, org),
		fixtures.WithGrantedEntitlements("invoices"),
	)
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

func containsAll(haystack []string, needles ...string) bool {
	for _, n := range needles {
		if !contains(haystack, n) {
			return false
		}
	}
	return true
}

func lockedHas(resolved specArg, surfaceID string) bool {
	for _, l := range resolved.GetLockedSurfaces() {
		if l.GetSurfaceId() == surfaceID {
			return true
		}
	}
	return false
}
