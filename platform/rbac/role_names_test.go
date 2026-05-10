package rbac_test

import (
	"testing"

	"github.com/aocybersystems/eden-platform-go/platform/rbac"
)

func TestRoleLevelByName_AllSystemRoles(t *testing.T) {
	want := map[string]rbac.RoleLevel{
		"viewer":      rbac.RoleLevelViewer,
		"member":      rbac.RoleLevelMember,
		"manager":     rbac.RoleLevelManager,
		"admin":       rbac.RoleLevelAdmin,
		"owner":       rbac.RoleLevelOwner,
		"super_admin": rbac.RoleLevelSuperAdmin,
	}
	for name, level := range want {
		got, ok := rbac.RoleLevelByName[name]
		if !ok {
			t.Errorf("RoleLevelByName[%q] missing", name)
			continue
		}
		if got != level {
			t.Errorf("RoleLevelByName[%q] = %d, want %d", name, got, level)
		}
	}
}

func TestRoleNameByLevel_KnownLevels(t *testing.T) {
	cases := []struct {
		level rbac.RoleLevel
		want  string
	}{
		{rbac.RoleLevelViewer, "viewer"},
		{rbac.RoleLevelMember, "member"},
		{rbac.RoleLevelManager, "manager"},
		{rbac.RoleLevelAdmin, "admin"},
		{rbac.RoleLevelOwner, "owner"},
		{rbac.RoleLevelSuperAdmin, "super_admin"},
		// Roundtrip: name -> level -> name should be stable for canonical levels.
	}
	for _, tc := range cases {
		if got := rbac.RoleNameByLevel(tc.level); got != tc.want {
			t.Errorf("RoleNameByLevel(%d) = %q, want %q", tc.level, got, tc.want)
		}
	}
}

func TestRoleNameByLevel_OutOfRange(t *testing.T) {
	if got := rbac.RoleNameByLevel(0); got != "viewer" {
		t.Errorf("RoleNameByLevel(0) = %q, want viewer", got)
	}
	if got := rbac.RoleNameByLevel(rbac.RoleLevel(150)); got != "super_admin" {
		t.Errorf("RoleNameByLevel(150) = %q, want super_admin", got)
	}
}

func TestRoleNameByLevel_Roundtrip(t *testing.T) {
	for name, level := range rbac.RoleLevelByName {
		got := rbac.RoleNameByLevel(level)
		if got != name {
			t.Errorf("Roundtrip %q -> %d -> %q", name, level, got)
		}
	}
}

func TestAllowedByRoleName_KnownRole_Allowed(t *testing.T) {
	matrix := rbac.DefaultPermissionMatrix()
	if !rbac.AllowedByRoleName(matrix, "admin", rbac.FeatureCRM, "view") {
		t.Errorf("AllowedByRoleName(admin, crm, view) = false, want true")
	}
	if !rbac.AllowedByRoleName(matrix, "owner", rbac.FeatureBanking, "admin") {
		t.Errorf("AllowedByRoleName(owner, banking, admin) = false, want true")
	}
}

func TestAllowedByRoleName_KnownRole_Denied(t *testing.T) {
	matrix := rbac.DefaultPermissionMatrix()
	if rbac.AllowedByRoleName(matrix, "viewer", rbac.FeatureCRM, "delete") {
		t.Errorf("AllowedByRoleName(viewer, crm, delete) = true, want false")
	}
}

func TestAllowedByRoleName_UnknownRole(t *testing.T) {
	matrix := rbac.DefaultPermissionMatrix()
	if rbac.AllowedByRoleName(matrix, "wizard", rbac.FeatureCRM, "view") {
		t.Errorf("AllowedByRoleName(wizard, crm, view) = true, want false")
	}
}

func TestAllowedByRoleName_UnknownFeature(t *testing.T) {
	matrix := rbac.DefaultPermissionMatrix()
	if rbac.AllowedByRoleName(matrix, "admin", rbac.Feature("nonexistent"), "view") {
		t.Errorf("AllowedByRoleName(admin, nonexistent, view) = true, want false")
	}
}

func TestAllowedByRoleName_UnknownAction(t *testing.T) {
	matrix := rbac.DefaultPermissionMatrix()
	if rbac.AllowedByRoleName(matrix, "owner", rbac.FeatureCRM, "telekinesis") {
		t.Errorf("AllowedByRoleName(owner, crm, telekinesis) = true, want false")
	}
}

func TestDefaultPermissionMatrix_EdenBizFeatures(t *testing.T) {
	matrix := rbac.DefaultPermissionMatrix()

	for _, f := range []rbac.Feature{
		rbac.FeatureBanking,
		rbac.FeatureCustomerPortal,
		rbac.FeatureSoftware,
		rbac.FeatureSettings,
		rbac.FeatureNotifications,
	} {
		if _, ok := matrix[f]; !ok {
			t.Errorf("DefaultPermissionMatrix() missing eden-biz feature %q", f)
		}
	}
}
