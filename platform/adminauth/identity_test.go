package adminauth_test

// Test list:
// - TestAdminIdentity_IsZero/zero_value_is_zero
// - TestAdminIdentity_IsZero/populated_is_not_zero
// - TestAdminIdentity_HasTenantAccess/super_admin_has_access_to_any_tenant
// - TestAdminIdentity_HasTenantAccess/tenant_admin_has_access_to_own_tenant
// - TestAdminIdentity_HasTenantAccess/tenant_admin_denied_for_other_tenant
// - TestAdminIdentity_HasTenantAccess/tenant_admin_with_nil_tenant_denied
// - TestAdminIdentity_HasRole/exact_match
// - TestAdminIdentity_HasRole/no_match
// - TestAdminIdentity_HasRole/empty_roles_returns_false
// - TestWithAdminIdentity_RoundTrip
// - TestAdminIdentityFromContext/nil_when_absent
// - TestSentinelErrors_Identity

import (
	"context"
	"errors"
	"testing"

	"github.com/aocybersystems/eden-platform-go/platform/adminauth"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAdminIdentity_IsZero_ZeroValueIsZero(t *testing.T) {
	var a adminauth.AdminIdentity
	require.True(t, a.IsZero())
}

func TestAdminIdentity_IsZero_PopulatedIsNotZero(t *testing.T) {
	cases := []adminauth.AdminIdentity{
		{SubjectCN: "x"},
		{TenantID: uuid.MustParse("11111111-1111-1111-1111-111111111111")},
		{IsSuperAdmin: true},
		{Roles: []string{"tenant_admin"}},
	}
	for _, c := range cases {
		require.False(t, c.IsZero(), "expected %+v to be non-zero", c)
	}
}

func TestAdminIdentity_HasTenantAccess_SuperAdminAnyTenant(t *testing.T) {
	a := adminauth.AdminIdentity{
		SubjectCN:    "aoid-superadmin",
		IsSuperAdmin: true,
	}
	require.True(t, a.HasTenantAccess(uuid.Nil))
	require.True(t, a.HasTenantAccess(uuid.MustParse("11111111-1111-1111-1111-111111111111")))
	require.True(t, a.HasTenantAccess(uuid.MustParse("22222222-2222-2222-2222-222222222222")))
}

func TestAdminIdentity_HasTenantAccess_TenantAdminOwnTenant(t *testing.T) {
	tenantID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	a := adminauth.AdminIdentity{
		SubjectCN: "aoid-admin-acme",
		TenantID:  tenantID,
	}
	require.True(t, a.HasTenantAccess(tenantID))
}

func TestAdminIdentity_HasTenantAccess_TenantAdminOtherTenantDenied(t *testing.T) {
	tenantID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	other := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	a := adminauth.AdminIdentity{
		SubjectCN: "aoid-admin-acme",
		TenantID:  tenantID,
	}
	require.False(t, a.HasTenantAccess(other))
}

func TestAdminIdentity_HasTenantAccess_TenantAdminWithNilTenantDenied(t *testing.T) {
	// A non-super admin with uuid.Nil tenant is a misconfigured identity; deny by default.
	a := adminauth.AdminIdentity{
		SubjectCN: "broken",
		TenantID:  uuid.Nil,
	}
	require.False(t, a.HasTenantAccess(uuid.Nil))
	require.False(t, a.HasTenantAccess(uuid.MustParse("11111111-1111-1111-1111-111111111111")))
}

func TestAdminIdentity_HasRole_ExactMatch(t *testing.T) {
	a := adminauth.AdminIdentity{Roles: []string{"tenant_admin"}}
	require.True(t, a.HasRole("tenant_admin"))
	require.False(t, a.HasRole("super_admin"))
}

func TestAdminIdentity_HasRole_MultipleRoles(t *testing.T) {
	a := adminauth.AdminIdentity{Roles: []string{"tenant_admin", "tenant_viewer"}}
	require.True(t, a.HasRole("tenant_admin"))
	require.True(t, a.HasRole("tenant_viewer"))
	require.False(t, a.HasRole("super_admin"))
}

func TestAdminIdentity_HasRole_EmptyRolesReturnsFalse(t *testing.T) {
	a := adminauth.AdminIdentity{}
	require.False(t, a.HasRole("tenant_admin"))
}

func TestWithAdminIdentity_RoundTrip(t *testing.T) {
	tenantID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	want := adminauth.AdminIdentity{
		SubjectCN:    "aoid-admin-acme",
		TenantID:     tenantID,
		IsSuperAdmin: false,
		Roles:        []string{"tenant_admin"},
	}
	ctx := adminauth.WithAdminIdentity(context.Background(), want)
	got := adminauth.AdminIdentityFromContext(ctx)
	require.NotNil(t, got)
	require.Equal(t, want.SubjectCN, got.SubjectCN)
	require.Equal(t, want.TenantID, got.TenantID)
	require.Equal(t, want.IsSuperAdmin, got.IsSuperAdmin)
	require.Equal(t, want.Roles, got.Roles)
}

func TestAdminIdentityFromContext_NilWhenAbsent(t *testing.T) {
	got := adminauth.AdminIdentityFromContext(context.Background())
	require.Nil(t, got)
}

func TestSentinelErrors_Identity(t *testing.T) {
	require.True(t, errors.Is(adminauth.ErrUnknownAdmin, adminauth.ErrUnknownAdmin))
	require.True(t, errors.Is(adminauth.ErrMissingCN, adminauth.ErrMissingCN))
	require.False(t, errors.Is(adminauth.ErrUnknownAdmin, adminauth.ErrMissingCN))
}
