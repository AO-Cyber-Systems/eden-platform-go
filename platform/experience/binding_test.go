// binding_test.go -- TRD 140-04, must-have #3.
//
// ServiceTransportBinding is the transport- AND scope-AGNOSTIC contract that
// lets ONE ExperienceSpec bind a FeatureSurface to BOTH backends:
//   - eden-biz  -> TransportKind CONNECT,      ScopeAuthority COMPANY
//   - aocore    -> TransportKind REST_OPENAPI,  ScopeAuthority ORG
//
// The binding carries reads AND writes (get/list/create/update/delete are
// first-class Operations -- it is NOT read-only) and a PaginationKind.
//
// AOID identity model (Decision #1, locked): the device holds ONE AOID
// identity. Per-binding scope_authority selects which backend tenant scope that
// single identity projects to via ResolveScope -- COMPANY-scoped (biz) or
// ORG-scoped (aocore) -- WITHOUT a second device-side credential. A projection
// to a scope the identity is not authorized for collapses to a single
// permission-denied outcome (no existence oracle).
//
// Fixtures: built via platform/experience/fixtures (140-02). No hand-built
// proto literals -- NewSpec + NewBinding + WrongTenant only.
package experience_test

import (
	"errors"
	"testing"

	experiencev1 "github.com/aocybersystems/eden-platform-go/gen/go/experience/v1"
	"github.com/aocybersystems/eden-platform-go/platform/experience"
	"github.com/aocybersystems/eden-platform-go/platform/experience/fixtures"
)

// --- Happy: binding round-trip preserving all fields ----------------------

// A ServiceTransportBinding round-trips every field through the factory: a
// CONNECT/COMPANY binding declaring all five operations + cursor pagination
// preserves entity, service coordinates, operations, transport, scope,
// pagination, and repo interface id.
func TestBinding_RoundTripsAllFields(t *testing.T) {
	b := fixtures.NewBinding(
		"Invoice",
		experiencev1.TransportKind_TRANSPORT_KIND_CONNECT,
		experiencev1.ScopeAuthority_SCOPE_AUTHORITY_COMPANY,
		experiencev1.PaginationKind_PAGINATION_KIND_CURSOR,
		fixtures.AllOperations()...,
	)

	if got, want := b.GetEntity(), "Invoice"; got != want {
		t.Errorf("entity: got %q want %q", got, want)
	}
	if b.GetServicePackage() == "" {
		t.Error("service_package empty -- not round-tripped")
	}
	if b.GetServiceName() == "" {
		t.Error("service_name empty -- not round-tripped")
	}
	if b.GetRepoInterfaceId() == "" {
		t.Error("repo_interface_id empty -- not round-tripped")
	}
	if got, want := b.GetTransportKind(), experiencev1.TransportKind_TRANSPORT_KIND_CONNECT; got != want {
		t.Errorf("transport_kind: got %v want %v", got, want)
	}
	if got, want := b.GetScopeAuthority(), experiencev1.ScopeAuthority_SCOPE_AUTHORITY_COMPANY; got != want {
		t.Errorf("scope_authority: got %v want %v", got, want)
	}
	if got, want := b.GetPagination(), experiencev1.PaginationKind_PAGINATION_KIND_CURSOR; got != want {
		t.Errorf("pagination: got %v want %v", got, want)
	}
	if got, want := len(b.GetOperations()), 5; got != want {
		t.Fatalf("operations len: got %d want %d (all five must round-trip)", got, want)
	}
}

// --- Happy: writes are first-class, distinguishable from reads --------------

// The binding declares writes (create/update/delete), not just reads -- prove a
// write operation is representable and distinguishable from a read.
func TestBinding_DeclaresReadsAndWrites(t *testing.T) {
	b := fixtures.NewBinding(
		"Invoice",
		experiencev1.TransportKind_TRANSPORT_KIND_CONNECT,
		experiencev1.ScopeAuthority_SCOPE_AUTHORITY_COMPANY,
		experiencev1.PaginationKind_PAGINATION_KIND_CURSOR,
		fixtures.AllOperations()...,
	)

	if !experience.BindingHasOperation(b, experiencev1.Operation_OPERATION_CREATE) {
		t.Error("CREATE (a write) must be representable on the binding -- it is NOT read-only")
	}
	if !experience.BindingHasOperation(b, experiencev1.Operation_OPERATION_GET) {
		t.Error("GET (a read) must be representable on the binding")
	}

	// A read-only binding must NOT report a write.
	readOnly := fixtures.NewBinding(
		"Report",
		experiencev1.TransportKind_TRANSPORT_KIND_REST_OPENAPI,
		experiencev1.ScopeAuthority_SCOPE_AUTHORITY_ORG,
		experiencev1.PaginationKind_PAGINATION_KIND_OFFSET,
		experiencev1.Operation_OPERATION_GET,
		experiencev1.Operation_OPERATION_LIST,
	)
	if experience.BindingHasOperation(readOnly, experiencev1.Operation_OPERATION_DELETE) {
		t.Error("a read-only binding must not report a DELETE write operation")
	}
}

// --- Happy: the two-transport, two-scope proof on ONE spec (the core) -------

// THE core proof: a single ExperienceSpec expresses BOTH a CONNECT/COMPANY
// binding (eden-biz) AND a REST_OPENAPI/ORG binding (aocore). The contract is
// transport- AND scope-agnostic.
func TestSpec_ExpressesBothTransportsAndScopesAtOnce(t *testing.T) {
	bizBinding := fixtures.NewBinding(
		"Invoice",
		experiencev1.TransportKind_TRANSPORT_KIND_CONNECT,
		experiencev1.ScopeAuthority_SCOPE_AUTHORITY_COMPANY,
		experiencev1.PaginationKind_PAGINATION_KIND_CURSOR,
		fixtures.AllOperations()...,
	)
	aocoreBinding := fixtures.NewBinding(
		"Tenant",
		experiencev1.TransportKind_TRANSPORT_KIND_REST_OPENAPI,
		experiencev1.ScopeAuthority_SCOPE_AUTHORITY_ORG,
		experiencev1.PaginationKind_PAGINATION_KIND_OFFSET,
		experiencev1.Operation_OPERATION_GET,
		experiencev1.Operation_OPERATION_LIST,
		experiencev1.Operation_OPERATION_CREATE,
	)

	spec := fixtures.NewSpec(
		fixtures.WithBinding(bizBinding),
		fixtures.WithBinding(aocoreBinding),
	)

	if got, want := len(spec.GetBindings()), 2; got != want {
		t.Fatalf("spec bindings len: got %d want %d", got, want)
	}

	// Both transports present on one spec.
	transports := map[experiencev1.TransportKind]bool{}
	scopes := map[experiencev1.ScopeAuthority]bool{}
	for _, b := range spec.GetBindings() {
		transports[b.GetTransportKind()] = true
		scopes[b.GetScopeAuthority()] = true
	}
	if !transports[experiencev1.TransportKind_TRANSPORT_KIND_CONNECT] ||
		!transports[experiencev1.TransportKind_TRANSPORT_KIND_REST_OPENAPI] {
		t.Error("one spec must express BOTH CONNECT and REST_OPENAPI transports")
	}
	if !scopes[experiencev1.ScopeAuthority_SCOPE_AUTHORITY_COMPANY] ||
		!scopes[experiencev1.ScopeAuthority_SCOPE_AUTHORITY_ORG] {
		t.Error("one spec must express BOTH COMPANY and ORG scope authorities")
	}
}

// --- Happy: AOID identity projects to each backend scope --------------------

// ONE AOID identity, authorized for company-A and org-A, projects to a
// COMPANY-scoped context for a CONNECT binding and an ORG-scoped context for a
// REST_OPENAPI binding -- the SAME identity, no second credential.
func TestResolveScope_OneIdentityProjectsToBothBackends(t *testing.T) {
	id := experience.AoidIdentity{
		Subject:    "user-aoid-1",
		CompanyIDs: []string{"company-A"},
		OrgIDs:     []string{"org-A"},
	}

	companyScope, err := experience.ResolveScope(id, experiencev1.ScopeAuthority_SCOPE_AUTHORITY_COMPANY, "company-A")
	if err != nil {
		t.Fatalf("COMPANY projection must succeed for an authorized company: %v", err)
	}
	if companyScope.Authority != experiencev1.ScopeAuthority_SCOPE_AUTHORITY_COMPANY {
		t.Errorf("company scope authority: got %v", companyScope.Authority)
	}
	if companyScope.ScopeID != "company-A" {
		t.Errorf("company scope id: got %q want company-A", companyScope.ScopeID)
	}

	orgScope, err := experience.ResolveScope(id, experiencev1.ScopeAuthority_SCOPE_AUTHORITY_ORG, "org-A")
	if err != nil {
		t.Fatalf("ORG projection must succeed for an authorized org: %v", err)
	}
	if orgScope.Authority != experiencev1.ScopeAuthority_SCOPE_AUTHORITY_ORG {
		t.Errorf("org scope authority: got %v", orgScope.Authority)
	}
	if orgScope.ScopeID != "org-A" {
		t.Errorf("org scope id: got %q want org-A", orgScope.ScopeID)
	}

	// Same identity, both projections -- no device-side multi-credential store.
	if companyScope.Subject != orgScope.Subject {
		t.Error("both scopes must project from the SAME AOID subject (one identity)")
	}
}

// --- Edge: TransportKind RESERVED accepted but not bindable -----------------

// A binding with an unspecified/reserved transport is structurally accepted but
// flagged not-yet-bindable (forward-compat for a future transport).
func TestBinding_ReservedTransport_NotBindable(t *testing.T) {
	b := fixtures.NewBinding(
		"Future",
		experiencev1.TransportKind_TRANSPORT_KIND_UNSPECIFIED,
		experiencev1.ScopeAuthority_SCOPE_AUTHORITY_COMPANY,
		experiencev1.PaginationKind_PAGINATION_KIND_NONE,
		experiencev1.Operation_OPERATION_GET,
	)
	if experience.BindingIsBindable(b) {
		t.Error("an unspecified/reserved transport must be flagged not-yet-bindable")
	}

	connect := fixtures.NewBinding(
		"Invoice",
		experiencev1.TransportKind_TRANSPORT_KIND_CONNECT,
		experiencev1.ScopeAuthority_SCOPE_AUTHORITY_COMPANY,
		experiencev1.PaginationKind_PAGINATION_KIND_CURSOR,
		experiencev1.Operation_OPERATION_GET,
	)
	if !experience.BindingIsBindable(connect) {
		t.Error("a CONNECT binding with a real scope must be bindable")
	}
}

// --- Edge: pagination kinds round-trip regardless of transport --------------

func TestBinding_PaginationKindsRoundTrip(t *testing.T) {
	cases := []experiencev1.PaginationKind{
		experiencev1.PaginationKind_PAGINATION_KIND_NONE,
		experiencev1.PaginationKind_PAGINATION_KIND_CURSOR,
		experiencev1.PaginationKind_PAGINATION_KIND_OFFSET,
	}
	for _, pk := range cases {
		b := fixtures.NewBinding(
			"Invoice",
			experiencev1.TransportKind_TRANSPORT_KIND_CONNECT,
			experiencev1.ScopeAuthority_SCOPE_AUTHORITY_COMPANY,
			pk,
			experiencev1.Operation_OPERATION_LIST,
		)
		if b.GetPagination() != pk {
			t.Errorf("pagination kind %v did not round-trip; got %v", pk, b.GetPagination())
		}
	}
}

// --- Failure / wrong-tenant (COMPANY) ---------------------------------------

// Wrong-tenant: a COMPANY-scoped projection for a company the AOID identity is
// NOT authorized for collapses to a single permission-denied outcome -- the
// SAME error whether the company exists or not (no existence oracle).
func TestResolveScope_WrongCompany_PermissionDeniedNoOracle(t *testing.T) {
	id := experience.AoidIdentity{
		Subject:    "user-aoid-1",
		CompanyIDs: []string{"company-A"},
		OrgIDs:     []string{"org-A"},
	}

	// company-B exists for SOMEONE else but not for this identity.
	_, errExisting := experience.ResolveScope(id, experiencev1.ScopeAuthority_SCOPE_AUTHORITY_COMPANY, "company-B")
	// company-NONEXISTENT does not exist at all.
	_, errMissing := experience.ResolveScope(id, experiencev1.ScopeAuthority_SCOPE_AUTHORITY_COMPANY, "company-NONEXISTENT")

	if errExisting == nil || errMissing == nil {
		t.Fatal("a COMPANY projection for an unauthorized company must be denied")
	}
	if !errors.Is(errExisting, experience.ErrScopeDenied) || !errors.Is(errMissing, experience.ErrScopeDenied) {
		t.Errorf("both must collapse to ErrScopeDenied; got existing=%v missing=%v", errExisting, errMissing)
	}
	// No existence oracle: the two errors must be identical text.
	if errExisting.Error() != errMissing.Error() {
		t.Errorf("existence oracle leak: %q != %q", errExisting.Error(), errMissing.Error())
	}
}

// --- Failure / wrong-org (ORG, aocore) --------------------------------------

// Same non-leaking guarantee for ORG scope (aocore): an unauthorized org and a
// nonexistent org are indistinguishable, both ErrScopeDenied.
func TestResolveScope_WrongOrg_PermissionDeniedNoOracle(t *testing.T) {
	id := experience.AoidIdentity{
		Subject:    "user-aoid-1",
		CompanyIDs: []string{"company-A"},
		OrgIDs:     []string{"org-A"},
	}

	_, errExisting := experience.ResolveScope(id, experiencev1.ScopeAuthority_SCOPE_AUTHORITY_ORG, "org-B")
	_, errMissing := experience.ResolveScope(id, experiencev1.ScopeAuthority_SCOPE_AUTHORITY_ORG, "org-NONEXISTENT")

	if errExisting == nil || errMissing == nil {
		t.Fatal("an ORG projection for an unauthorized org must be denied")
	}
	if !errors.Is(errExisting, experience.ErrScopeDenied) || !errors.Is(errMissing, experience.ErrScopeDenied) {
		t.Errorf("both must collapse to ErrScopeDenied; got existing=%v missing=%v", errExisting, errMissing)
	}
	if errExisting.Error() != errMissing.Error() {
		t.Errorf("existence oracle leak: %q != %q", errExisting.Error(), errMissing.Error())
	}
}

// --- Failure / wrong-tenant: scope fields survive on a divergent spec --------

// A spec carrying COMPANY-scoped bindings for tenant X, run through
// fixtures.WrongTenant, keeps tenant_id/org_id AND each binding's
// scope_authority intact -- 140-08/09 can enforce isolation because the scope
// fields are NOT silently dropped or swapped.
func TestSpec_WrongTenant_PreservesBindingScopeFields(t *testing.T) {
	baseline := fixtures.NewSpec(
		fixtures.WithBinding(fixtures.NewBinding(
			"Invoice",
			experiencev1.TransportKind_TRANSPORT_KIND_CONNECT,
			experiencev1.ScopeAuthority_SCOPE_AUTHORITY_COMPANY,
			experiencev1.PaginationKind_PAGINATION_KIND_CURSOR,
			fixtures.AllOperations()...,
		)),
	)
	wrong := fixtures.WrongTenant(baseline)

	// Tenancy scope diverged (it's a wrong-tenant copy)...
	if wrong.GetTenantId() == baseline.GetTenantId() {
		t.Error("WrongTenant must diverge tenant_id")
	}
	// ...but the binding's scope_authority must NOT be dropped or swapped.
	if got, want := len(wrong.GetBindings()), 1; got != want {
		t.Fatalf("WrongTenant dropped bindings: got %d want %d", got, want)
	}
	if got := wrong.GetBindings()[0].GetScopeAuthority(); got != experiencev1.ScopeAuthority_SCOPE_AUTHORITY_COMPANY {
		t.Errorf("WrongTenant silently changed binding scope_authority to %v", got)
	}
	if got := wrong.GetBindings()[0].GetTransportKind(); got != experiencev1.TransportKind_TRANSPORT_KIND_CONNECT {
		t.Errorf("WrongTenant silently changed binding transport_kind to %v", got)
	}
}
