// navgraph_test.go — TRD 140-05, must-have #2.
//
// NavGraph is a GRAPH, not a flat surface list: a landing_surface_id + typed
// NavSlots (surface + placement + order) + typed NavEdges that carry
// param_bindings. The param_bindings on an edge are the customer->invoice->
// payment SELECTION-passing proof — the builder composes FLOWS, not a launcher.
//
// DeepLinkSpec (url_scheme + route_templates) lives on AppDefinition because a
// store binary can't change its route scheme post-submit.
//
// Coherence is MACHINE-CHECKED (ValidateNavGraph), not a ProblemsPanel of two
// ad-hoc checks: <=5 PRIMARY slots, landing_surface_id exists in the graph,
// every edge endpoint exists in the graph. Wrong-tenant: a graph referencing a
// surface granted only to tenant B from an A-scoped spec collapses to the SAME
// not-entitled coherence error as a nonexistent surface (no existence oracle).
//
// Fixtures: built via platform/experience/fixtures (140-02). No hand-built
// literals — NewSpec + options + NewNavSlot/NewNavEdge/NewDeepLink + WrongTenant.
package experience_test

import (
	"testing"

	experiencev1 "github.com/aocybersystems/eden-platform-go/gen/go/experience/v1"
	"github.com/aocybersystems/eden-platform-go/platform/experience"
	"github.com/aocybersystems/eden-platform-go/platform/experience/fixtures"
	"google.golang.org/protobuf/proto"
)

// entitledSurfaces is the set of surfaces the validator is told the requesting
// scope is granted (its referenced_surface_ids + landing). A graph node not in
// this set is "not entitled" — the SAME outcome as a node that does not exist,
// so a wrong-tenant probe cannot use the error to oracle another tenant's
// surfaces.
func entitledSurfaces(ids ...string) map[string]struct{} {
	m := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		m[id] = struct{}{}
	}
	return m
}

// --- Happy ----------------------------------------------------------------

// A NavGraph with a landing surface, <=5 PRIMARY slots, and typed edges whose
// endpoints all exist validates clean (no coherence errors).
func TestNavGraph_ValidGraph_ValidatesClean(t *testing.T) {
	spec := fixtures.NewSpec(
		fixtures.WithNavGraph(
			"customers",
			fixtures.NewNavSlot("customers", experiencev1.Placement_PLACEMENT_PRIMARY, 0),
			fixtures.NewNavSlot("invoices", experiencev1.Placement_PLACEMENT_PRIMARY, 1),
			fixtures.NewNavSlot("payment", experiencev1.Placement_PLACEMENT_DETAIL_ONLY, 0),
		),
		fixtures.WithNavEdge(fixtures.NewNavEdge(
			"customers", "invoices",
			map[string]string{"customerId": "$selection.id"},
			"onSelect",
		)),
		fixtures.WithNavEdge(fixtures.NewNavEdge(
			"invoices", "payment",
			map[string]string{"invoiceId": "$selection.id"},
			"onSelect",
		)),
	)

	errs := experience.ValidateNavGraph(
		spec.GetNavGraph(),
		entitledSurfaces("customers", "invoices", "payment"),
	)
	if len(errs) != 0 {
		t.Fatalf("valid nav graph must produce no coherence errors; got %v", errs)
	}
}

// The NavGraph round-trips through the wire: landing + slots + edges survive.
func TestNavGraph_RoundTrips(t *testing.T) {
	spec := fixtures.NewSpec(
		fixtures.WithNavGraph(
			"home",
			fixtures.NewNavSlot("home", experiencev1.Placement_PLACEMENT_PRIMARY, 0),
			fixtures.NewNavSlot("more", experiencev1.Placement_PLACEMENT_MORE, 1),
		),
		fixtures.WithNavEdge(fixtures.NewNavEdge(
			"home", "more", map[string]string{"q": "x"}, "tap",
		)),
	)

	wire, err := proto.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out experiencev1.ExperienceSpec
	if err := proto.Unmarshal(wire, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	g := out.GetNavGraph()
	if g.GetLandingSurfaceId() != "home" {
		t.Errorf("landing not preserved; got %q", g.GetLandingSurfaceId())
	}
	if len(g.GetSlots()) != 2 {
		t.Fatalf("want 2 slots; got %d", len(g.GetSlots()))
	}
	if len(g.GetEdges()) != 1 {
		t.Fatalf("want 1 edge; got %d", len(g.GetEdges()))
	}
}

// The three Placements are distinguishable after a round-trip (PRIMARY vs MORE
// vs DETAIL_ONLY are not collapsed).
func TestNavGraph_PlacementsDistinguishable(t *testing.T) {
	spec := fixtures.NewSpec(
		fixtures.WithNavGraph(
			"a",
			fixtures.NewNavSlot("a", experiencev1.Placement_PLACEMENT_PRIMARY, 0),
			fixtures.NewNavSlot("b", experiencev1.Placement_PLACEMENT_MORE, 1),
			fixtures.NewNavSlot("c", experiencev1.Placement_PLACEMENT_DETAIL_ONLY, 2),
		),
	)
	wire, err := proto.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out experiencev1.ExperienceSpec
	if err := proto.Unmarshal(wire, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got := map[string]experiencev1.Placement{}
	for _, sl := range out.GetNavGraph().GetSlots() {
		got[sl.GetSurfaceId()] = sl.GetPlacement()
	}
	if got["a"] != experiencev1.Placement_PLACEMENT_PRIMARY {
		t.Errorf("a should be PRIMARY; got %v", got["a"])
	}
	if got["b"] != experiencev1.Placement_PLACEMENT_MORE {
		t.Errorf("b should be MORE; got %v", got["b"])
	}
	if got["c"] != experiencev1.Placement_PLACEMENT_DETAIL_ONLY {
		t.Errorf("c should be DETAIL_ONLY; got %v", got["c"])
	}
	// All three must be mutually distinct enum values.
	if got["a"] == got["b"] || got["b"] == got["c"] || got["a"] == got["c"] {
		t.Errorf("placements collapsed: a=%v b=%v c=%v", got["a"], got["b"], got["c"])
	}
}

// A NavEdge carries typed param_bindings — the customerId selection survives the
// wire. THIS is the flow-passing proof: an edge is not a bare launcher link, it
// passes a typed selection from the source surface into the target.
func TestNavEdge_ParamBindings_RoundTrip(t *testing.T) {
	spec := fixtures.NewSpec(
		fixtures.WithNavGraph(
			"customers",
			fixtures.NewNavSlot("customers", experiencev1.Placement_PLACEMENT_PRIMARY, 0),
			fixtures.NewNavSlot("invoices", experiencev1.Placement_PLACEMENT_PRIMARY, 1),
		),
		fixtures.WithNavEdge(fixtures.NewNavEdge(
			"customers", "invoices",
			map[string]string{"customerId": "$selection.id"},
			"onSelect",
		)),
	)

	wire, err := proto.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out experiencev1.ExperienceSpec
	if err := proto.Unmarshal(wire, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	edges := out.GetNavGraph().GetEdges()
	if len(edges) != 1 {
		t.Fatalf("want 1 edge; got %d", len(edges))
	}
	e := edges[0]
	if e.GetFromSurfaceId() != "customers" || e.GetToSurfaceId() != "invoices" {
		t.Errorf("edge endpoints lost; from=%q to=%q", e.GetFromSurfaceId(), e.GetToSurfaceId())
	}
	if e.GetTrigger() != "onSelect" {
		t.Errorf("trigger lost; got %q", e.GetTrigger())
	}
	if got := e.GetParamBindings()["customerId"]; got != "$selection.id" {
		t.Errorf("customerId selection binding lost; got %q", got)
	}
}

// DeepLinkSpec round-trips url_scheme + route_templates on AppDefinition. It
// lives on AppDefinition (not ExperienceSpec) because store binaries can't
// change route schemes post-submit.
func TestDeepLinkSpec_RoundTrips_OnAppDefinition(t *testing.T) {
	app := &experiencev1.AppDefinition{
		Id:       "app-1",
		DeepLink: fixtures.NewDeepLink("edenbiz", "/customers/:id", "/invoices/:id/pay"),
	}

	wire, err := proto.Marshal(app)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out experiencev1.AppDefinition
	if err := proto.Unmarshal(wire, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	dl := out.GetDeepLink()
	if dl.GetUrlScheme() != "edenbiz" {
		t.Errorf("url_scheme lost; got %q", dl.GetUrlScheme())
	}
	if len(dl.GetRouteTemplates()) != 2 {
		t.Fatalf("want 2 route templates; got %d", len(dl.GetRouteTemplates()))
	}
	if dl.GetRouteTemplates()[1] != "/invoices/:id/pay" {
		t.Errorf("route template lost; got %q", dl.GetRouteTemplates()[1])
	}
}

// --- Edge: coherence rules machine-checked --------------------------------

// 6 PRIMARY slots -> coherence error (<=5 PRIMARY rule). The DATA can express
// >5 PRIMARY (factory does not block it); the VALIDATOR rejects it.
func TestNavGraph_SixPrimarySlots_CoherenceError(t *testing.T) {
	slots := []*experiencev1.NavSlot{}
	ids := []string{}
	for i, name := range []string{"s0", "s1", "s2", "s3", "s4", "s5"} {
		slots = append(slots, fixtures.NewNavSlot(name, experiencev1.Placement_PLACEMENT_PRIMARY, int32(i)))
		ids = append(ids, name)
	}
	spec := fixtures.NewSpec(fixtures.WithNavGraph("s0", slots...))

	errs := experience.ValidateNavGraph(spec.GetNavGraph(), entitledSurfaces(ids...))
	if !hasCoherenceCode(errs, experience.CoherenceTooManyPrimary) {
		t.Fatalf("6 PRIMARY slots must yield a too-many-primary error; got %v", errs)
	}
}

// landing_surface_id referencing a surface not in any NavSlot -> coherence error.
func TestNavGraph_LandingNotInGraph_CoherenceError(t *testing.T) {
	spec := fixtures.NewSpec(
		fixtures.WithNavGraph(
			"ghost", // landing references a surface with no slot
			fixtures.NewNavSlot("real", experiencev1.Placement_PLACEMENT_PRIMARY, 0),
		),
	)
	errs := experience.ValidateNavGraph(spec.GetNavGraph(), entitledSurfaces("ghost", "real"))
	if !hasCoherenceCode(errs, experience.CoherenceLandingMissing) {
		t.Fatalf("landing not in graph must yield a landing-missing error; got %v", errs)
	}
}

// A NavEdge whose `to` surface isn't in the graph -> coherence error.
func TestNavGraph_EdgeEndpointMissing_CoherenceError(t *testing.T) {
	spec := fixtures.NewSpec(
		fixtures.WithNavGraph(
			"home",
			fixtures.NewNavSlot("home", experiencev1.Placement_PLACEMENT_PRIMARY, 0),
		),
		fixtures.WithNavEdge(fixtures.NewNavEdge(
			"home", "nowhere", map[string]string{"x": "y"}, "tap",
		)),
	)
	errs := experience.ValidateNavGraph(spec.GetNavGraph(), entitledSurfaces("home", "nowhere"))
	if !hasCoherenceCode(errs, experience.CoherenceEdgeEndpointMissing) {
		t.Fatalf("edge to a non-slot surface must yield an edge-endpoint-missing error; got %v", errs)
	}
}

// --- Failure / wrong-tenant -----------------------------------------------

// Wrong-tenant: a NavGraph validated for tenant A never resolves a slot into a
// surface granted only to tenant B. From an A-scoped spec, a graph node that is
// NOT in A's entitled set collapses to the SAME not-entitled coherence error a
// nonexistent surface produces — no existence oracle. We assert (1) the error
// fires and (2) it carries the SAME code as a plainly-missing entitlement, so a
// caller can't distinguish "exists for tenant B" from "does not exist".
func TestNavGraph_WrongTenant_NotEntitled_SameAsNonexistent(t *testing.T) {
	baseline := fixtures.NewSpec(
		fixtures.WithNavGraph(
			"home",
			fixtures.NewNavSlot("home", experiencev1.Placement_PLACEMENT_PRIMARY, 0),
			fixtures.NewNavSlot("tenant-b-only-surface", experiencev1.Placement_PLACEMENT_PRIMARY, 1),
		),
	)
	// WrongTenant flips tenant_id/org_id; the A-scoped entitled set deliberately
	// excludes "tenant-b-only-surface" (granted only to B).
	wrong := fixtures.WrongTenant(baseline)

	// Sanity: tenancy scope is intact / swapped on the clone, never bled.
	if wrong.GetTenantId() == baseline.GetTenantId() || wrong.GetOrgId() == baseline.GetOrgId() {
		t.Fatalf("WrongTenant must diverge scope; tenant=%q org=%q", wrong.GetTenantId(), wrong.GetOrgId())
	}
	// And the nav graph itself is carried THROUGH untouched (no tenant swap of
	// surface ids — the graph doesn't leak/relabel scope).
	if wrong.GetNavGraph().GetLandingSurfaceId() != baseline.GetNavGraph().GetLandingSurfaceId() {
		t.Fatalf("nav graph landing must survive the tenant flip unchanged")
	}

	aEntitled := entitledSurfaces("home") // A is NOT granted tenant-b-only-surface
	gotB := experience.ValidateNavGraph(wrong.GetNavGraph(), aEntitled)

	// A graph referencing a plainly nonexistent surface from the same A scope.
	nonexistentSpec := fixtures.NewSpec(
		fixtures.WithNavGraph(
			"home",
			fixtures.NewNavSlot("home", experiencev1.Placement_PLACEMENT_PRIMARY, 0),
			fixtures.NewNavSlot("does-not-exist", experiencev1.Placement_PLACEMENT_PRIMARY, 1),
		),
	)
	gotNonexistent := experience.ValidateNavGraph(nonexistentSpec.GetNavGraph(), aEntitled)

	if !hasCoherenceCode(gotB, experience.CoherenceSurfaceNotEntitled) {
		t.Fatalf("tenant-B-only surface must be not-entitled from A scope; got %v", gotB)
	}
	if !hasCoherenceCode(gotNonexistent, experience.CoherenceSurfaceNotEntitled) {
		t.Fatalf("nonexistent surface must be not-entitled from A scope; got %v", gotNonexistent)
	}
}

// hasCoherenceCode reports whether errs contains an error of the given code.
func hasCoherenceCode(errs []experience.CoherenceError, code experience.CoherenceCode) bool {
	for _, e := range errs {
		if e.Code == code {
			return true
		}
	}
	return false
}
