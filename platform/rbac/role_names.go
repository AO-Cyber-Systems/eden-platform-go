package rbac

// RoleLevelByName maps system role names to their numeric level.
// Donor: eden-biz/permissions.RoleLevel.
//
// Use this when a consumer identifies roles by string ("viewer"/"member"/...)
// rather than UUID. For permission checks, prefer Enforcer; this map is the
// compatibility seam for older string-role call sites.
var RoleLevelByName = map[string]RoleLevel{
	"viewer":      RoleLevelViewer,
	"member":      RoleLevelMember,
	"manager":     RoleLevelManager,
	"admin":       RoleLevelAdmin,
	"owner":       RoleLevelOwner,
	"super_admin": RoleLevelSuperAdmin,
}

// RoleNameByLevel returns the canonical role name for a level. The result
// is the highest role whose Level threshold is satisfied by the input.
// For example RoleLevel(85) returns "admin", RoleLevel(95) returns "owner".
func RoleNameByLevel(level RoleLevel) string {
	switch {
	case level >= RoleLevelSuperAdmin:
		return "super_admin"
	case level >= RoleLevelOwner:
		return "owner"
	case level >= RoleLevelAdmin:
		return "admin"
	case level >= RoleLevelManager:
		return "manager"
	case level >= RoleLevelMember:
		return "member"
	default:
		return "viewer"
	}
}

// AllowedByRoleName checks whether a role name has access to the given
// (feature, action) tuple in the supplied matrix. Compatibility shim for
// eden-biz/permissions.Service.Allowed.
//
// Returns false if the role name is unknown, the feature is not in the
// matrix, or the action is not in the feature's matrix.
func AllowedByRoleName(matrix PermissionMatrix, roleName string, feature Feature, action string) bool {
	level, ok := RoleLevelByName[roleName]
	if !ok {
		return false
	}
	actionMatrix, ok := matrix[feature]
	if !ok {
		return false
	}
	required, ok := actionMatrix[action]
	if !ok {
		return false
	}
	return level >= required
}
