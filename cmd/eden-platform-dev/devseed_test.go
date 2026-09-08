//go:build dev

package main

import (
	"context"
	"testing"

	"github.com/aocybersystems/eden-platform-go/platform/auth"
	"github.com/aocybersystems/eden-platform-go/platform/devstore"
)

// TestSeedDevTenant_CoherentIdentity proves the seeded demo tenant satisfies
// the same invariants a real signup would: a company exists, a user exists
// with a real (non-placeholder) password hash, the user is a member of that
// company, and the membership's role grants at least one real permission.
func TestSeedDevTenant_CoherentIdentity(t *testing.T) {
	backend := devstore.NewMemoryBackend()
	seedRBACData(backend) // roles + permissions the RBAC lookup fallback needs
	seedDevTenant(backend)

	ctx := context.Background()
	companyStore := backend.CompanyStore()
	authStore := backend.AuthStore()
	rbacStore := backend.RBACStore()

	seededCompany, err := companyStore.GetCompany(ctx, DevTenantCompanyID)
	if err != nil {
		t.Fatalf("seeded company not found: %v", err)
	}
	if seededCompany.Name == "" {
		t.Fatal("seeded company has an empty name")
	}

	user, err := authStore.GetUserByEmail(ctx, DevTenantEmail)
	if err != nil {
		t.Fatalf("seeded user not found: %v", err)
	}

	// The password hash must be a real Argon2id hash produced by
	// auth.NewPasswordHasher, not a placeholder string, and it must actually
	// verify against the known seed password.
	if user.PasswordHash == "" || user.PasswordHash == DevTenantPassword {
		t.Fatalf("seeded user does not have a real password hash: %q", user.PasswordHash)
	}
	hasher := auth.NewPasswordHasher()
	ok, err := hasher.Verify(DevTenantPassword, user.PasswordHash)
	if err != nil {
		t.Fatalf("verify seeded password hash: %v", err)
	}
	if !ok {
		t.Fatal("seeded password hash does not verify against the known seed password")
	}

	// The user must be a member of the seeded company (not some other
	// company, and not membership-less).
	membership, err := authStore.GetCompanyMembershipByUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("seeded user has no company membership: %v", err)
	}
	if membership.CompanyID != seededCompany.ID {
		t.Fatalf("membership company = %s, want %s", membership.CompanyID, seededCompany.ID)
	}

	// The membership's role must grant at least one real permission — this is
	// what makes authorization decisions genuine rather than a hollow pass.
	perms, err := rbacStore.GetUserPermissions(ctx, seededCompany.ID, user.ID)
	if err != nil {
		t.Fatalf("get user permissions: %v", err)
	}
	if len(perms) == 0 {
		t.Fatal("seeded user's role grants zero permissions")
	}

	role, err := rbacStore.GetUserRole(ctx, seededCompany.ID, user.ID)
	if err != nil {
		t.Fatalf("get user role: %v", err)
	}
	if role.Name != "owner" {
		t.Fatalf("seeded user role = %q, want %q", role.Name, "owner")
	}
}

// TestSeedDevTenant_Idempotent proves that seeding twice against the same
// backend yields one tenant, not two: the same company row and the same user
// row are resolved on the second call rather than new ones being minted.
func TestSeedDevTenant_Idempotent(t *testing.T) {
	backend := devstore.NewMemoryBackend()
	seedRBACData(backend)

	seedDevTenant(backend)
	seedDevTenant(backend)

	ctx := context.Background()
	companyStore := backend.CompanyStore()
	authStore := backend.AuthStore()

	companies, err := companyStore.ListCompanies(ctx)
	if err != nil {
		t.Fatalf("list companies: %v", err)
	}
	if len(companies) != 1 {
		t.Fatalf("got %d companies after seeding twice, want 1", len(companies))
	}
	if companies[0].ID != DevTenantCompanyID {
		t.Fatalf("company id = %s, want %s", companies[0].ID, DevTenantCompanyID)
	}

	user, err := authStore.GetUserByEmail(ctx, DevTenantEmail)
	if err != nil {
		t.Fatalf("seeded user not found: %v", err)
	}

	// A single, unambiguous membership must resolve for the user — if seeding
	// had created a second company or a second user, either this lookup would
	// be ambiguous or the membership would point at the wrong company.
	membership, err := authStore.GetCompanyMembershipByUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("seeded user has no company membership: %v", err)
	}
	if membership.CompanyID != DevTenantCompanyID {
		t.Fatalf("membership company = %s, want %s", membership.CompanyID, DevTenantCompanyID)
	}
}
