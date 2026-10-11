package devstore

import (
	"context"
	"testing"

	"go.aocyber.ai/eden-platform-go/platform/company"
	"go.aocyber.ai/eden-platform-go/platform/rbac"
)

// TestAuthStore_GetCompanyMembershipByUser_OldestFirst is the in-memory twin of
// pgstore's TestAuthStore_GetCompanyMembershipByUser_OldestFirst: a
// multi-company user must resolve to the OLDEST membership (the "home"
// company), exactly like pgstore's ORDER BY created_at ASC. If the twins
// disagree, dev mode and memstore-backed tests sign the user into a different
// company than production does.
//
// The home membership is created FIRST and belongs to the company with the
// LARGER id, so ordering by company_id would pick the wrong one.
func TestAuthStore_GetCompanyMembershipByUser_OldestFirst(t *testing.T) {
	backend := NewMemoryBackend()
	authStore := backend.AuthStore()
	companyStore := backend.CompanyStore()
	ctx := context.Background()

	user, err := authStore.CreateUser(ctx, "multi@example.com", "hash", "Multi")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	c1, err := companyStore.CreateCompany(ctx, company.Company{Name: "Co 1", Slug: "co-1"})
	if err != nil {
		t.Fatalf("create company 1: %v", err)
	}
	c2, err := companyStore.CreateCompany(ctx, company.Company{Name: "Co 2", Slug: "co-2"})
	if err != nil {
		t.Fatalf("create company 2: %v", err)
	}
	home, later := c1, c2
	if c2.ID.String() > c1.ID.String() {
		home, later = c2, c1 // home = larger id, so company_id ASC would NOT pick it
	}

	if err := authStore.CreateCompanyMembership(ctx, home.ID, user.ID, rbac.OwnerRoleID); err != nil {
		t.Fatalf("home membership: %v", err)
	}
	if err := authStore.CreateCompanyMembership(ctx, later.ID, user.ID, rbac.OwnerRoleID); err != nil {
		t.Fatalf("later membership: %v", err)
	}

	for i := 0; i < 5; i++ {
		m, err := authStore.GetCompanyMembershipByUser(ctx, user.ID)
		if err != nil {
			t.Fatalf("get membership: %v", err)
		}
		if m.CompanyID != home.ID {
			t.Fatalf("call %d: resolved company = %s, want oldest/home %s (other = %s)", i, m.CompanyID, home.ID, later.ID)
		}
	}
}
