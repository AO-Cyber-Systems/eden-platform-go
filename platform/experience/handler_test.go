// handler_test.go -- TRD 140-09, Task 1 (RED).
//
// OUTSIDE-IN tests for the ExperienceService Connect handler, in the order the
// playbook (habit #5) + this TRD demand:
//
//  1. HTTP-through-the-REAL-route-map (httptest server + the actual mux the
//     production wiring registers) -- this is where routing/dispatch bugs hide
//     (memory: a fully-dead reconcile path was invisible to handler-only tests).
//  2. handler-level (edge cases: coherence wiring, version wiring, determinism).
//  3. service-level composition is exercised THROUGH the handler (the handler is
//     a thin adapter over resolve.go / navgraph.go / version_negotiation.go).
//
// The WRONG-TENANT proofs are the PRIME surface and run at the HTTP layer:
//   (a) principal scoped to company X resolving company Y's spec -> ONE
//       permission-denied code, identical to a nonexistent spec (no oracle).
//   (b) a request body carrying a DIFFERENT tenant than the principal does NOT
//       override the principal (the body-binding-authority lesson).
//   (c) cross-ORG (aocore org scope) is denied equivalently.
package experience_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	connect "connectrpc.com/connect"
	experiencev1 "github.com/aocybersystems/eden-platform-go/gen/go/experience/v1"
	experiencev1connect "github.com/aocybersystems/eden-platform-go/gen/go/experience/v1/experiencev1connect"
	"github.com/aocybersystems/eden-platform-go/platform/auth"
	"github.com/aocybersystems/eden-platform-go/platform/experience"
	"github.com/aocybersystems/eden-platform-go/platform/experience/fixtures"
)

// --- principal scope constants -------------------------------------------
//
// The principal's COMPANY claim is the experience tenant_id; an "org:<id>" scope
// claim carries the aocore ORG scope. These are the AUTHENTICATED-principal
// values -- the body can never override them.
const (
	companyA = "tenant-fixture-0001" // == fixtures.DefaultTenantID
	orgA     = "org-fixture-0001"    // == fixtures.DefaultOrgID
	companyB = "tenant-fixture-9999" // == fixtures.WrongTenantID
	orgB     = "org-fixture-9999"    // == fixtures.WrongOrgID

	surfaceHome     = "surface.home"
	surfaceInvoices = "surface.invoices"
	surfaceLocked   = "surface.premium" // referenced but NOT granted -> LockedSurface
)

// testHarness spins the REAL route map (the same RegisterExperienceHandlers the
// production wiring uses) behind an httptest server, with a JWT manager so we can
// mint principal tokens. Every test issues HTTP Connect calls -- never a direct
// handler method call.
type testHarness struct {
	ts     *httptest.Server
	client experiencev1connect.ExperienceServiceClient
	jwt    *auth.JWTManager
}

func newHarness(t *testing.T) *testHarness {
	t.Helper()

	jwtManager, err := auth.NewJWTManager(auth.JWTConfig{
		Issuer:             "eden-experience-test",
		AccessTokenExpiry:  auth.DefaultJWTConfig().AccessTokenExpiry,
		RefreshTokenExpiry: auth.DefaultJWTConfig().RefreshTokenExpiry,
	})
	if err != nil {
		t.Fatalf("NewJWTManager() error = %v", err)
	}

	// Deterministic clock + an entitlements provider granting companyA's tuple
	// the home+invoices surfaces (NOT surface.premium -> it locks).
	handler := experience.NewExperienceHandler(
		experience.NewMemorySpecStore(),
		entitlementsFor(map[string]map[string]struct{}{
			companyA + "|" + orgA: granted(surfaceHome, surfaceInvoices),
			companyB + "|" + orgB: granted(surfaceHome, surfaceInvoices),
		}),
		experience.WithClock(func() string { return "2026-06-29T00:00:00Z" }),
	)

	mux := http.NewServeMux()
	experience.RegisterExperienceHandlers(
		mux,
		handler,
		connect.WithInterceptors(experience.NewAuthInterceptor(jwtManager)),
	)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	return &testHarness{
		ts:     ts,
		client: experiencev1connect.NewExperienceServiceClient(ts.Client(), ts.URL),
		jwt:    jwtManager,
	}
}

func granted(ids ...string) map[string]struct{} {
	m := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		m[id] = struct{}{}
	}
	return m
}

// entitlementsFor builds an EntitlementsProvider whose grant set is keyed by
// "tenant|org" -- the single-source grant result for a principal scope.
func entitlementsFor(byScope map[string]map[string]struct{}) experience.EntitlementsProvider {
	return experience.EntitlementsFunc(func(scope experience.PrincipalScope, role, formFactor string) fixtures.ResolutionTuple {
		key := scope.TenantID + "|" + scope.OrgID
		return fixtures.ResolutionTuple{
			Role:        role,
			FormFactor:  formFactor,
			TenantID:    scope.TenantID,
			OrgID:       scope.OrgID,
			Entitlement: "entitlement-set-" + key,
			Granted:     byScope[key],
		}
	})
}

// token mints a principal access token: companyID is the tenant claim, the
// "org:<id>" scope carries the aocore org scope.
func (h *testHarness) token(t *testing.T, company, org string) string {
	t.Helper()
	tok, err := h.jwt.CreateAccessTokenWithScopes(
		"user-"+company, company, "owner", 100, nil, []string{"org:" + org},
	)
	if err != nil {
		t.Fatalf("CreateAccessTokenWithScopes() error = %v", err)
	}
	return tok
}

func authed[T any](t *testing.T, token string, msg *T) *connect.Request[T] {
	t.Helper()
	req := connect.NewRequest(msg)
	req.Header().Set("Authorization", "Bearer "+token)
	return req
}

// coherentAppDef builds a storeable AppDefinition whose spec references the
// granted surfaces with a coherent 1-PRIMARY nav graph + surface.premium as a
// referenced-but-ungranted (will lock on resolve).
func coherentAppDef(id string) *experiencev1.AppDefinition {
	spec := fixtures.NewSpec(
		fixtures.WithReferencedSurfaces(surfaceHome, surfaceInvoices, surfaceLocked),
		fixtures.WithUnknownSurfacePolicy(experiencev1.UnknownSurfacePolicy_UNKNOWN_SURFACE_POLICY_IGNORE),
		fixtures.WithNavGraph(surfaceHome,
			fixtures.NewNavSlot(surfaceHome, experiencev1.Placement_PLACEMENT_PRIMARY, 0),
			fixtures.NewNavSlot(surfaceInvoices, experiencev1.Placement_PLACEMENT_PRIMARY, 1),
		),
	)
	return &experiencev1.AppDefinition{
		Id:               id,
		Meta:             &experiencev1.AppMeta{Name: "Fixture App", BundleId: "ai.aocyber.fixture"},
		Spec:             spec,
		MinBinaryVersion: "1.0.0",
		ContractVersion:  "experience.v1",
	}
}

// storeAs stores appDef under the principal scope (company/org) via the HTTP
// route map and returns the stored app_def_id.
func (h *testHarness) storeAs(t *testing.T, company, org string, appDef *experiencev1.AppDefinition) string {
	t.Helper()
	resp, err := h.client.StoreSpec(
		t.Context(),
		authed(t, h.token(t, company, org), &experiencev1.StoreSpecRequest{AppDefinition: appDef}),
	)
	if err != nil {
		t.Fatalf("StoreSpec() error = %v", err)
	}
	return resp.Msg.GetAppDefId()
}

// =========================================================================
// (1) HTTP integration -- happy paths through the REAL route map.
// =========================================================================

func TestStoreSpec_PersistsVersionedSpec(t *testing.T) {
	h := newHarness(t)
	resp, err := h.client.StoreSpec(
		t.Context(),
		authed(t, h.token(t, companyA, orgA), &experiencev1.StoreSpecRequest{
			AppDefinition: coherentAppDef("app-1"),
		}),
	)
	if err != nil {
		t.Fatalf("StoreSpec() error = %v", err)
	}
	if resp.Msg.GetAppDefId() != "app-1" {
		t.Errorf("StoreSpec app_def_id = %q, want %q", resp.Msg.GetAppDefId(), "app-1")
	}
	if resp.Msg.GetContentHash() == "" {
		t.Errorf("StoreSpec content_hash is empty")
	}
	if resp.Msg.GetSpecSchemaVersion() == "" {
		t.Errorf("StoreSpec spec_schema_version is empty")
	}
}

func TestResolveSpec_ReturnsFilteredContentHashedSpec(t *testing.T) {
	h := newHarness(t)
	h.storeAs(t, companyA, orgA, coherentAppDef("app-1"))

	resp, err := h.client.ResolveSpec(
		t.Context(),
		authed(t, h.token(t, companyA, orgA), &experiencev1.ResolveSpecRequest{
			AppDefId:   "app-1",
			Role:       "owner",
			FormFactor: "form-factor-mobile",
		}),
	)
	if err != nil {
		t.Fatalf("ResolveSpec() error = %v", err)
	}
	resolved := resp.Msg.GetResolvedSpec()
	if resolved == nil {
		t.Fatalf("ResolveSpec resolved_spec is nil")
	}
	// Granted surfaces pass; the ungranted surface.premium becomes a LockedSurface.
	if got := resolved.GetReferencedSurfaceIds(); len(got) != 2 {
		t.Errorf("resolved referenced_surface_ids = %v, want 2 granted", got)
	}
	if len(resolved.GetLockedSurfaces()) != 1 {
		t.Errorf("resolved locked_surfaces = %d, want 1 (surface.premium)", len(resolved.GetLockedSurfaces()))
	}
	if resolved.GetContentHash() == "" {
		t.Errorf("resolved content_hash is empty (140-08 must stamp it)")
	}
	// The resolution context is stamped for the PRINCIPAL's scope, not the body.
	if rc := resolved.GetResolutionContext(); rc == nil || rc.GetTenantId() != companyA || rc.GetOrgId() != orgA {
		t.Errorf("resolution_context scope = %+v, want tenant=%s org=%s", resolved.GetResolutionContext(), companyA, orgA)
	}
}

func TestValidateSpec_CoherentSpecHasNoProblems(t *testing.T) {
	h := newHarness(t)
	resp, err := h.client.ValidateSpec(
		t.Context(),
		authed(t, h.token(t, companyA, orgA), &experiencev1.ValidateSpecRequest{
			AppDefinition: coherentAppDef("app-1"),
		}),
	)
	if err != nil {
		t.Fatalf("ValidateSpec() error = %v", err)
	}
	if !resp.Msg.GetValid() {
		t.Errorf("ValidateSpec valid = false, problems = %v", resp.Msg.GetProblems())
	}
	if len(resp.Msg.GetProblems()) != 0 {
		t.Errorf("ValidateSpec problems = %v, want none", resp.Msg.GetProblems())
	}
}

// =========================================================================
// (2) handler edge cases -- coherence (140-05) + version (140-03) + determinism.
// =========================================================================

func TestValidateSpec_SixPrimaryNavSlots_ReturnsCoherenceError(t *testing.T) {
	h := newHarness(t)
	// 6 PRIMARY slots -> ValidateNavGraph CoherenceTooManyPrimary (max 5).
	spec := fixtures.NewSpec(
		fixtures.WithReferencedSurfaces(surfaceHome),
		fixtures.WithNavGraph(surfaceHome,
			fixtures.NewNavSlot("s0", experiencev1.Placement_PLACEMENT_PRIMARY, 0),
			fixtures.NewNavSlot("s1", experiencev1.Placement_PLACEMENT_PRIMARY, 1),
			fixtures.NewNavSlot("s2", experiencev1.Placement_PLACEMENT_PRIMARY, 2),
			fixtures.NewNavSlot("s3", experiencev1.Placement_PLACEMENT_PRIMARY, 3),
			fixtures.NewNavSlot("s4", experiencev1.Placement_PLACEMENT_PRIMARY, 4),
			fixtures.NewNavSlot("s5", experiencev1.Placement_PLACEMENT_PRIMARY, 5),
		),
	)
	appDef := &experiencev1.AppDefinition{
		Id:   "app-bad-nav",
		Meta: &experiencev1.AppMeta{Name: "Bad Nav", BundleId: "ai.aocyber.badnav"},
		Spec: spec,
	}

	resp, err := h.client.ValidateSpec(
		t.Context(),
		authed(t, h.token(t, companyA, orgA), &experiencev1.ValidateSpecRequest{AppDefinition: appDef}),
	)
	if err != nil {
		t.Fatalf("ValidateSpec() error = %v", err)
	}
	if resp.Msg.GetValid() {
		t.Errorf("ValidateSpec valid = true, want false (6 primary slots)")
	}
	found := false
	for _, p := range resp.Msg.GetProblems() {
		if p.GetCode() == string(experience.CoherenceTooManyPrimary) {
			found = true
		}
	}
	if !found {
		t.Errorf("ValidateSpec problems = %v, want a %s problem", resp.Msg.GetProblems(), experience.CoherenceTooManyPrimary)
	}
}

func TestResolveSpec_DeterministicContentHash(t *testing.T) {
	h := newHarness(t)
	h.storeAs(t, companyA, orgA, coherentAppDef("app-1"))

	req := func() *connect.Request[experiencev1.ResolveSpecRequest] {
		return authed(t, h.token(t, companyA, orgA), &experiencev1.ResolveSpecRequest{
			AppDefId: "app-1", Role: "owner", FormFactor: "form-factor-mobile",
		})
	}
	r1, err := h.client.ResolveSpec(t.Context(), req())
	if err != nil {
		t.Fatalf("ResolveSpec() #1 error = %v", err)
	}
	r2, err := h.client.ResolveSpec(t.Context(), req())
	if err != nil {
		t.Fatalf("ResolveSpec() #2 error = %v", err)
	}
	if r1.Msg.GetResolvedSpec().GetContentHash() != r2.Msg.GetResolvedSpec().GetContentHash() {
		t.Errorf("ResolveSpec content_hash not deterministic: %q != %q",
			r1.Msg.GetResolvedSpec().GetContentHash(), r2.Msg.GetResolvedSpec().GetContentHash())
	}
}

// =========================================================================
// (3) FAILURE / WRONG-TENANT -- the PRIME surface, at the HTTP layer.
// =========================================================================

// (a) principal company A resolving company B's spec -> ONE permission-denied
// code, IDENTICAL to resolving a spec that does not exist (no existence oracle).
func TestResolveSpec_WrongTenant_NonLeakingDenial(t *testing.T) {
	h := newHarness(t)
	// Company B stores "app-secret"; company A must not be able to tell it exists.
	h.storeAs(t, companyB, orgB, coherentAppDef("app-secret"))

	_, errExisting := h.client.ResolveSpec(
		t.Context(),
		authed(t, h.token(t, companyA, orgA), &experiencev1.ResolveSpecRequest{
			AppDefId: "app-secret", Role: "owner", FormFactor: "form-factor-mobile",
		}),
	)
	_, errNonexistent := h.client.ResolveSpec(
		t.Context(),
		authed(t, h.token(t, companyA, orgA), &experiencev1.ResolveSpecRequest{
			AppDefId: "app-does-not-exist", Role: "owner", FormFactor: "form-factor-mobile",
		}),
	)
	if errExisting == nil || errNonexistent == nil {
		t.Fatalf("both wrong-tenant + nonexistent must error; got existing=%v nonexistent=%v", errExisting, errNonexistent)
	}
	if connect.CodeOf(errExisting) != connect.CodePermissionDenied {
		t.Errorf("wrong-tenant code = %v, want PermissionDenied", connect.CodeOf(errExisting))
	}
	// NO ORACLE: the two codes MUST be identical -- a different code would leak
	// "this spec exists for another tenant".
	if connect.CodeOf(errExisting) != connect.CodeOf(errNonexistent) {
		t.Errorf("oracle leak: wrong-tenant code %v != nonexistent code %v (must be identical)",
			connect.CodeOf(errExisting), connect.CodeOf(errNonexistent))
	}
	if errExisting.Error() != errNonexistent.Error() {
		t.Errorf("oracle leak: messages differ: %q vs %q", errExisting.Error(), errNonexistent.Error())
	}
}

// (b) a request body carrying a DIFFERENT tenant than the principal does NOT
// let the principal store/read under another tenant. The body-binding-authority
// lesson: store as A with a spec whose body tenant_id claims B; resolving as B
// must NOT find it (the spec was planted under A, the principal scope, not B).
func TestStoreSpec_BodyTenantDoesNotOverridePrincipal(t *testing.T) {
	h := newHarness(t)

	// Build an app def whose nested spec's body carries tenant B / org B, but
	// store it authenticated as principal A. The principal scope MUST win.
	appDef := coherentAppDef("app-planted")
	appDef.Spec.TenantId = companyB
	appDef.Spec.OrgId = orgB
	h.storeAs(t, companyA, orgA, appDef)

	// Principal B tries to resolve "app-planted": if the body tenant had won, the
	// spec would be under B and resolvable. It must NOT be (planted under A).
	_, errB := h.client.ResolveSpec(
		t.Context(),
		authed(t, h.token(t, companyB, orgB), &experiencev1.ResolveSpecRequest{
			AppDefId: "app-planted", Role: "owner", FormFactor: "form-factor-mobile",
		}),
	)
	if errB == nil {
		t.Fatalf("principal B resolved a spec stored by principal A -- body tenant_id overrode the principal (cross-tenant write)")
	}
	if connect.CodeOf(errB) != connect.CodePermissionDenied {
		t.Errorf("body-override denial code = %v, want PermissionDenied", connect.CodeOf(errB))
	}

	// And principal A (the true owner) CAN resolve it -- proving it was stored
	// under the principal scope, not the body scope.
	respA, errA := h.client.ResolveSpec(
		t.Context(),
		authed(t, h.token(t, companyA, orgA), &experiencev1.ResolveSpecRequest{
			AppDefId: "app-planted", Role: "owner", FormFactor: "form-factor-mobile",
		}),
	)
	if errA != nil {
		t.Fatalf("true owner A could not resolve its own spec: %v", errA)
	}
	if rc := respA.Msg.GetResolvedSpec().GetResolutionContext(); rc.GetTenantId() != companyA {
		t.Errorf("resolved context tenant = %q, want principal A %q (body did not override)", rc.GetTenantId(), companyA)
	}
}

// (c) cross-ORG denial: same company token but a divergent org scope is denied
// equivalently (the aocore ORG axis is enforced at the chokepoint too).
func TestResolveSpec_CrossOrg_NonLeakingDenial(t *testing.T) {
	h := newHarness(t)
	// Stored under (companyB, orgB).
	h.storeAs(t, companyB, orgB, coherentAppDef("app-org"))

	// A principal with company A / org A (cross-org relative to the stored scope)
	// is denied the SAME non-leaking way.
	_, err := h.client.ResolveSpec(
		t.Context(),
		authed(t, h.token(t, companyA, orgA), &experiencev1.ResolveSpecRequest{
			AppDefId: "app-org", Role: "owner", FormFactor: "form-factor-mobile",
		}),
	)
	if err == nil {
		t.Fatalf("cross-org resolution must be denied")
	}
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Errorf("cross-org denial code = %v, want PermissionDenied", connect.CodeOf(err))
	}
}

// Unauthenticated calls are rejected before any handler logic runs (the route
// map's auth interceptor is wired) -- proves the chokepoint is in the real path.
func TestExperienceService_Unauthenticated(t *testing.T) {
	h := newHarness(t)
	_, err := h.client.ResolveSpec(
		t.Context(),
		connect.NewRequest(&experiencev1.ResolveSpecRequest{AppDefId: "app-1"}),
	)
	if err == nil {
		t.Fatalf("ResolveSpec without a token must fail")
	}
	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Errorf("unauthenticated code = %v, want Unauthenticated", connect.CodeOf(err))
	}
}
