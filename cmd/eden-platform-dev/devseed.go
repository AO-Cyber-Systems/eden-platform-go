//go:build dev

package main

import (
	"context"
	"log/slog"

	"github.com/aocybersystems/eden-platform-go/platform/auth"
	"github.com/aocybersystems/eden-platform-go/platform/company"
	"github.com/aocybersystems/eden-platform-go/platform/devstore"
	"github.com/aocybersystems/eden-platform-go/platform/rbac"
	"github.com/google/uuid"
)

// Fixed identifiers for the seeded demo tenant. Using fixed IDs (instead of
// uuid.New()) is what makes seedDevTenant idempotent: every call resolves to
// the same company/user rows instead of minting duplicates.
//
// DevTenantCompanyID intentionally reuses the SAME id that seedSSOForDev and
// seedWebhookData already seed against in main.go, so the demo company also
// carries SSO config and a webhook out of the box — one coherent tenant, not
// three disconnected fixtures.
var (
	DevTenantCompanyID = uuid.MustParse("20000000-0000-0000-0000-000000000001")
	DevTenantUserID    = uuid.MustParse("30000000-0000-0000-0000-000000000001")
)

// Credentials and identity for the seeded demo user/company. This file only
// compiles under `-tags dev` (see the build tag above), so these values never
// ship in a production binary.
const (
	DevTenantEmail       = "dev@eden.local"
	DevTenantPassword    = "devplayground123!"
	DevTenantDisplayName = "Dev Playground User"
	DevTenantCompanyName = "Dev Playground Co"
	DevTenantCompanySlug = "dev-playground"
)

// seedDevTenant seeds a complete, coherent demo tenant into the in-memory
// devstore backend: a company, a user with a real Argon2id password hash, a
// company membership, and (via RBACStore's fallback to auth memberships — see
// platform/devstore/rbac_store.go GetUserRole/GetUserPermissions) an RBAC
// owner-role binding that grants every seeded permission.
//
// This mirrors auth.Service.SignUp exactly: create user, create/resolve
// company, create the company membership with rbac.OwnerRoleID. No new
// devstore mutation paths are introduced — CreateUser, and
// CompanyStore.CreateCompany/GetCompany, and AuthStore.CreateCompanyMembership
// all already exist and are used the same way production signup uses them.
//
// Idempotent: safe to call on every process start. A second call resolves the
// existing company (by fixed ID) and user (by email) instead of creating new
// ones, and CreateCompanyMembership is itself a no-op if the membership
// already exists.
func seedDevTenant(backend *devstore.Backend) {
	ctx := context.Background()
	companyStore := backend.CompanyStore()
	authStore := backend.AuthStore()

	seededCompany, err := companyStore.GetCompany(ctx, DevTenantCompanyID)
	if err != nil {
		seededCompany, err = companyStore.CreateCompany(ctx, company.Company{
			ID:          DevTenantCompanyID,
			Name:        DevTenantCompanyName,
			Slug:        DevTenantCompanySlug,
			CompanyType: company.CompanyTypeStandalone,
		})
		if err != nil {
			slog.Error("seed dev tenant: create company", "error", err)
			return
		}
	}

	user, err := authStore.GetUserByEmail(ctx, DevTenantEmail)
	if err != nil {
		hasher := auth.NewPasswordHasher()
		passwordHash, hashErr := hasher.Hash(DevTenantPassword)
		if hashErr != nil {
			slog.Error("seed dev tenant: hash password", "error", hashErr)
			return
		}
		user, err = authStore.CreateUser(ctx, DevTenantEmail, passwordHash, DevTenantDisplayName)
		if err != nil {
			slog.Error("seed dev tenant: create user", "error", err)
			return
		}
	}

	if err := authStore.CreateCompanyMembership(ctx, seededCompany.ID, user.ID, rbac.OwnerRoleID); err != nil {
		slog.Error("seed dev tenant: create company membership", "error", err)
		return
	}

	slog.Info("seeded dev tenant",
		"company_id", seededCompany.ID,
		"user_id", user.ID,
		"email", DevTenantEmail,
		"role", "owner",
	)
}
