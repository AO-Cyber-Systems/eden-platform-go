// repository_test.go -- TRD 140-11, the transport-agnostic binding PROOF.
//
// These tests are the load-bearing evidence that ONE Repository abstraction
// backs BOTH a CONNECT (eden-biz, COMPANY-scoped) stub repo AND a REST_OPENAPI
// (aocore, ORG-scoped, edge-signed) stub repo -- and that the SAME surface code
// drives either transport through the interface with ZERO surface change.
//
// The aocore-REST stub replays a committed cassette via an in-process httptest
// server: NO live aocore call ever happens. Cross-tenant / cross-org reads
// collapse to ONE permission-denied (ErrScopeDenied) with no existence oracle.
package experience_test

import (
	"context"
	"errors"
	"testing"

	experiencev1 "github.com/aocybersystems/eden-platform-go/gen/go/experience/v1"
	"github.com/aocybersystems/eden-platform-go/platform/experience"
	"github.com/aocybersystems/eden-platform-go/platform/experience/fixtures"
)

// fullOps is the read+write operation set every binding in this proof exposes.
func fullOps() []experiencev1.Operation {
	return []experiencev1.Operation{
		experiencev1.Operation_OPERATION_GET,
		experiencev1.Operation_OPERATION_LIST,
		experiencev1.Operation_OPERATION_CREATE,
		experiencev1.Operation_OPERATION_UPDATE,
		experiencev1.Operation_OPERATION_DELETE,
	}
}

// connectBinding is a CONNECT/COMPANY binding over the Tenant entity.
func connectBinding() *experiencev1.ServiceTransportBinding {
	return fixtures.NewBinding(
		"Tenant",
		experiencev1.TransportKind_TRANSPORT_KIND_CONNECT,
		experiencev1.ScopeAuthority_SCOPE_AUTHORITY_COMPANY,
		experiencev1.PaginationKind_PAGINATION_KIND_CURSOR,
		fullOps()...,
	)
}

// restBinding is a REST_OPENAPI/ORG binding over the SAME logical Tenant entity.
// Same shape as connectBinding -- only transport+scope differ. That sameness is
// the whole point: the surface binds identically, the runtime picks the impl.
func restBinding() *experiencev1.ServiceTransportBinding {
	return fixtures.NewBinding(
		"Tenant",
		experiencev1.TransportKind_TRANSPORT_KIND_REST_OPENAPI,
		experiencev1.ScopeAuthority_SCOPE_AUTHORITY_ORG,
		experiencev1.PaginationKind_PAGINATION_KIND_CURSOR,
		fullOps()...,
	)
}

// bothScopesIdentity is the ONE AOID identity (no second credential) authorized
// for BOTH the company scope (biz) and the org scope (aocore). The aocore stub's
// org scope MUST match the committed cassette's org_id.
func bothScopesIdentity() experience.AoidIdentity {
	return experience.AoidIdentity{
		Subject:    "aoid-subject-fixture",
		CompanyIDs: []string{fixtures.DefaultTenantID},
		OrgIDs:     []string{fixtures.DefaultOrgID},
	}
}

// exerciseSurface is the SAME surface code run against either transport. It is
// written ONCE against the Repository interface and is unaware which concrete
// impl (Connect or aocore-REST) it received. If this function needs a
// transport-specific branch, the abstraction has leaked.
func exerciseSurface(t *testing.T, repo experience.Repository, sc experience.ScopedContext, getID string) {
	t.Helper()
	ctx := context.Background()

	// READ one.
	got, err := repo.Get(ctx, sc, getID)
	if err != nil {
		t.Fatalf("Get(%q): unexpected error: %v", getID, err)
	}
	if got.ID != getID {
		t.Errorf("Get returned id %q, want %q", got.ID, getID)
	}
	if got.ScopeID != sc.ScopeID {
		t.Errorf("Get returned scope %q, want %q (entity must carry its scope)", got.ScopeID, sc.ScopeID)
	}

	// LIST with pagination.
	page, err := repo.List(ctx, sc, experience.PageRequest{Limit: 10})
	if err != nil {
		t.Fatalf("List: unexpected error: %v", err)
	}
	if len(page.Items) == 0 {
		t.Errorf("List returned no items")
	}
	for _, it := range page.Items {
		if it.ScopeID != sc.ScopeID {
			t.Errorf("List item %q escaped scope: got %q want %q", it.ID, it.ScopeID, sc.ScopeID)
		}
	}

	// WRITE (create).
	created, err := repo.Create(ctx, sc, experience.Entity{Fields: map[string]string{"name": "Created Via Surface"}})
	if err != nil {
		t.Fatalf("Create: unexpected error: %v", err)
	}
	if created.ID == "" {
		t.Errorf("Create returned an entity with no id")
	}
	if created.ScopeID != sc.ScopeID {
		t.Errorf("Create returned scope %q, want %q", created.ScopeID, sc.ScopeID)
	}
}

// --- HAPPY: same surface, both transports --------------------------------

func TestSurface_ConnectCompany_RoundTrips(t *testing.T) {
	identity := bothScopesIdentity()
	repo, err := experience.RepositoryFor(connectBinding(), identity)
	if err != nil {
		t.Fatalf("RepositoryFor(CONNECT/COMPANY): %v", err)
	}
	sc, err := experience.ResolveScope(identity, experiencev1.ScopeAuthority_SCOPE_AUTHORITY_COMPANY, fixtures.DefaultTenantID)
	if err != nil {
		t.Fatalf("ResolveScope COMPANY: %v", err)
	}
	exerciseSurface(t, repo, sc, "tenant-biz-1")
}

func TestSurface_RestOrg_RoundTrips_FromCassette(t *testing.T) {
	identity := bothScopesIdentity()
	repo, err := experience.RepositoryFor(restBinding(), identity)
	if err != nil {
		t.Fatalf("RepositoryFor(REST_OPENAPI/ORG): %v", err)
	}
	sc, err := experience.ResolveScope(identity, experiencev1.ScopeAuthority_SCOPE_AUTHORITY_ORG, fixtures.DefaultOrgID)
	if err != nil {
		t.Fatalf("ResolveScope ORG: %v", err)
	}
	// "tenant-aocore-1" is the id recorded in the committed aocore_org.json cassette.
	exerciseSurface(t, repo, sc, "tenant-aocore-1")
}

// TestSurface_IdenticalCode_BothTransports is THE proof: one surface function,
// two transports, zero branch. We run the exact same closure against each repo.
func TestSurface_IdenticalCode_BothTransports(t *testing.T) {
	identity := bothScopesIdentity()

	connectRepo, err := experience.RepositoryFor(connectBinding(), identity)
	if err != nil {
		t.Fatalf("RepositoryFor CONNECT: %v", err)
	}
	restRepo, err := experience.RepositoryFor(restBinding(), identity)
	if err != nil {
		t.Fatalf("RepositoryFor REST: %v", err)
	}

	companyScope, err := experience.ResolveScope(identity, experiencev1.ScopeAuthority_SCOPE_AUTHORITY_COMPANY, fixtures.DefaultTenantID)
	if err != nil {
		t.Fatalf("ResolveScope COMPANY: %v", err)
	}
	orgScope, err := experience.ResolveScope(identity, experiencev1.ScopeAuthority_SCOPE_AUTHORITY_ORG, fixtures.DefaultOrgID)
	if err != nil {
		t.Fatalf("ResolveScope ORG: %v", err)
	}

	// The SAME exercise body, no per-transport branching.
	exerciseSurface(t, connectRepo, companyScope, "tenant-biz-1")
	exerciseSurface(t, restRepo, orgScope, "tenant-aocore-1")
}

// --- EDGE: selection by transport, pagination, forward-compat -------------

func TestRepositoryFor_SelectsImplByTransportAndScope(t *testing.T) {
	identity := bothScopesIdentity()

	connectRepo, err := experience.RepositoryFor(connectBinding(), identity)
	if err != nil {
		t.Fatalf("RepositoryFor CONNECT: %v", err)
	}
	restRepo, err := experience.RepositoryFor(restBinding(), identity)
	if err != nil {
		t.Fatalf("RepositoryFor REST: %v", err)
	}

	if connectRepo.Transport() != experiencev1.TransportKind_TRANSPORT_KIND_CONNECT {
		t.Errorf("CONNECT binding resolved to transport %v", connectRepo.Transport())
	}
	if connectRepo.Authority() != experiencev1.ScopeAuthority_SCOPE_AUTHORITY_COMPANY {
		t.Errorf("CONNECT binding resolved to authority %v, want COMPANY", connectRepo.Authority())
	}
	if restRepo.Transport() != experiencev1.TransportKind_TRANSPORT_KIND_REST_OPENAPI {
		t.Errorf("REST binding resolved to transport %v", restRepo.Transport())
	}
	if restRepo.Authority() != experiencev1.ScopeAuthority_SCOPE_AUTHORITY_ORG {
		t.Errorf("REST binding resolved to authority %v, want ORG", restRepo.Authority())
	}
}

func TestRestOrg_PaginationThreads(t *testing.T) {
	identity := bothScopesIdentity()
	repo, err := experience.RepositoryFor(restBinding(), identity)
	if err != nil {
		t.Fatalf("RepositoryFor REST: %v", err)
	}
	sc, err := experience.ResolveScope(identity, experiencev1.ScopeAuthority_SCOPE_AUTHORITY_ORG, fixtures.DefaultOrgID)
	if err != nil {
		t.Fatalf("ResolveScope ORG: %v", err)
	}
	page, err := repo.List(context.Background(), sc, experience.PageRequest{Limit: 1, Cursor: "cursor-page-1"})
	if err != nil {
		t.Fatalf("List with cursor: %v", err)
	}
	// The cassette records a next_cursor -- pagination must thread through the
	// REST transport, not be dropped on the floor.
	if page.NextCursor == "" {
		t.Errorf("REST List dropped the next_cursor from the cassette response")
	}
}

func TestConnectCompany_PaginationThreads(t *testing.T) {
	identity := bothScopesIdentity()
	repo, err := experience.RepositoryFor(connectBinding(), identity)
	if err != nil {
		t.Fatalf("RepositoryFor CONNECT: %v", err)
	}
	sc, err := experience.ResolveScope(identity, experiencev1.ScopeAuthority_SCOPE_AUTHORITY_COMPANY, fixtures.DefaultTenantID)
	if err != nil {
		t.Fatalf("ResolveScope COMPANY: %v", err)
	}
	page, err := repo.List(context.Background(), sc, experience.PageRequest{Limit: 1})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if page.NextCursor == "" {
		t.Errorf("CONNECT List dropped the next_cursor")
	}
}

func TestRepositoryFor_UnspecifiedTransport_TypedNotSupported(t *testing.T) {
	identity := bothScopesIdentity()
	// An UNSPECIFIED transport is structurally valid (forward-compat reservation)
	// but NOT yet bindable -- selecting an impl must fail with a typed,
	// not-yet-supported error, never a nil repo or a panic.
	b := fixtures.NewBinding(
		"Tenant",
		experiencev1.TransportKind_TRANSPORT_KIND_UNSPECIFIED,
		experiencev1.ScopeAuthority_SCOPE_AUTHORITY_UNSPECIFIED,
		experiencev1.PaginationKind_PAGINATION_KIND_NONE,
		fullOps()...,
	)
	repo, err := experience.RepositoryFor(b, identity)
	if err == nil {
		t.Fatalf("RepositoryFor(UNSPECIFIED): expected typed error, got nil (repo=%v)", repo)
	}
	if !errors.Is(err, experience.ErrTransportNotSupported) {
		t.Errorf("RepositoryFor(UNSPECIFIED): error %v is not ErrTransportNotSupported", err)
	}
	if repo != nil {
		t.Errorf("RepositoryFor(UNSPECIFIED): expected nil repo alongside error, got %v", repo)
	}
}

// --- FAILURE / WRONG-TENANT: non-leaking across BOTH auth shapes ----------

func TestConnectCompany_CrossCompany_Denied_NoOracle(t *testing.T) {
	identity := bothScopesIdentity()
	repo, err := experience.RepositoryFor(connectBinding(), identity)
	if err != nil {
		t.Fatalf("RepositoryFor CONNECT: %v", err)
	}
	// Project to a company the identity is NOT granted (fixtures.WrongTenantID).
	_, err = experience.ResolveScope(identity, experiencev1.ScopeAuthority_SCOPE_AUTHORITY_COMPANY, fixtures.WrongTenantID)
	if !errors.Is(err, experience.ErrScopeDenied) {
		t.Fatalf("cross-company ResolveScope: want ErrScopeDenied, got %v", err)
	}

	// And even if a caller forges a ScopedContext naming the wrong company, the
	// repo chokepoint must refuse it -- identical denial whether the wrong
	// company's entity exists or not (no oracle).
	forged := experience.ScopedContext{
		Subject:   identity.Subject,
		Authority: experiencev1.ScopeAuthority_SCOPE_AUTHORITY_COMPANY,
		ScopeID:   fixtures.WrongTenantID,
	}
	_, errExisting := repo.Get(context.Background(), forged, "tenant-biz-1")     // an id that "exists" in the right scope
	_, errMissing := repo.Get(context.Background(), forged, "does-not-exist-id") // an id that exists nowhere
	if !errors.Is(errExisting, experience.ErrScopeDenied) {
		t.Errorf("cross-company Get(existing): want ErrScopeDenied, got %v", errExisting)
	}
	if !errors.Is(errMissing, experience.ErrScopeDenied) {
		t.Errorf("cross-company Get(missing): want ErrScopeDenied, got %v", errMissing)
	}
	if errExisting.Error() != errMissing.Error() {
		t.Errorf("cross-company denial leaks an existence oracle: %q != %q", errExisting, errMissing)
	}
}

func TestRestOrg_CrossOrg_Denied_NoOracle(t *testing.T) {
	identity := bothScopesIdentity()
	repo, err := experience.RepositoryFor(restBinding(), identity)
	if err != nil {
		t.Fatalf("RepositoryFor REST: %v", err)
	}
	// Project to an org the identity is NOT granted (fixtures.WrongOrgID).
	_, err = experience.ResolveScope(identity, experiencev1.ScopeAuthority_SCOPE_AUTHORITY_ORG, fixtures.WrongOrgID)
	if !errors.Is(err, experience.ErrScopeDenied) {
		t.Fatalf("cross-org ResolveScope: want ErrScopeDenied, got %v", err)
	}

	// A forged ScopedContext naming the wrong org must be refused at the repo
	// chokepoint BEFORE any cassette request -- and the aocore stub must NOT
	// serve another org's data. Identical denial whether the entity exists or not.
	forged := experience.ScopedContext{
		Subject:   identity.Subject,
		Authority: experiencev1.ScopeAuthority_SCOPE_AUTHORITY_ORG,
		ScopeID:   fixtures.WrongOrgID,
	}
	_, errExisting := repo.Get(context.Background(), forged, "tenant-aocore-1")  // exists for the RIGHT org in the cassette
	_, errMissing := repo.Get(context.Background(), forged, "does-not-exist-id") // exists nowhere
	if !errors.Is(errExisting, experience.ErrScopeDenied) {
		t.Errorf("cross-org Get(existing): want ErrScopeDenied, got %v", errExisting)
	}
	if !errors.Is(errMissing, experience.ErrScopeDenied) {
		t.Errorf("cross-org Get(missing): want ErrScopeDenied, got %v", errMissing)
	}
	if errExisting.Error() != errMissing.Error() {
		t.Errorf("cross-org denial leaks an existence oracle: %q != %q", errExisting, errMissing)
	}
}

// TestRestOrg_NoLiveCall asserts the aocore stub is driven ENTIRELY by the
// committed cassette over an in-process httptest server: its base URL must be a
// loopback httptest address, never a real aocore host.
func TestRestOrg_NoLiveCall(t *testing.T) {
	identity := bothScopesIdentity()
	repo, err := experience.RepositoryFor(restBinding(), identity)
	if err != nil {
		t.Fatalf("RepositoryFor REST: %v", err)
	}
	rc, ok := repo.(experience.CassetteBackedRepository)
	if !ok {
		t.Fatalf("aocore-REST repo does not expose CassetteBackedRepository (cannot prove no live call)")
	}
	base := rc.BaseURL()
	if base == "" {
		t.Fatalf("aocore-REST repo has no base URL")
	}
	if !isLoopback(base) {
		t.Errorf("aocore-REST repo base URL %q is not loopback -- a live call could escape", base)
	}
}

func isLoopback(url string) bool {
	return containsAny(url, []string{"127.0.0.1", "::1", "localhost"})
}

func containsAny(s string, subs []string) bool {
	for _, sub := range subs {
		if len(sub) <= len(s) {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}
