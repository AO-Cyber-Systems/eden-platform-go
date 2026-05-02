package connectapi

import (
	"context"
	"strings"

	connect "connectrpc.com/connect"
	platformv1 "github.com/aocybersystems/eden-platform-go/gen/go/platform/v1"
	platformv1connect "github.com/aocybersystems/eden-platform-go/gen/go/platform/v1/platformv1connect"
	"github.com/aocybersystems/eden-platform-go/platform/rbac"
	"github.com/aocybersystems/eden-platform-go/platform/server"
	"github.com/google/uuid"
)

var _ platformv1connect.RBACServiceHandler = (*RBACHandler)(nil)

// RBACHandler implements the RBACService Connect handler.
type RBACHandler struct {
	service  *rbac.Service
	enforcer *rbac.Enforcer
	resolver *rbac.HierarchyResolver
}

// NewRBACHandler creates a new RBAC handler.
func NewRBACHandler(service *rbac.Service, enforcer *rbac.Enforcer, resolver *rbac.HierarchyResolver) *RBACHandler {
	return &RBACHandler{service: service, enforcer: enforcer, resolver: resolver}
}

func (h *RBACHandler) ListRoles(ctx context.Context, req *connect.Request[platformv1.ListRolesRequest]) (*connect.Response[platformv1.ListRolesResponse], error) {
	companyID, err := uuid.Parse(req.Msg.GetCompanyId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	roles, err := h.service.ListRoles(ctx, companyID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	protoRoles := make([]*platformv1.RoleData, 0, len(roles))
	for _, role := range roles {
		protoRoles = append(protoRoles, roleToProto(role))
	}

	return connect.NewResponse(&platformv1.ListRolesResponse{Roles: protoRoles}), nil
}

func (h *RBACHandler) CreateRole(ctx context.Context, req *connect.Request[platformv1.CreateRoleRequest]) (*connect.Response[platformv1.CreateRoleResponse], error) {
	companyID, err := uuid.Parse(req.Msg.GetCompanyId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	permissionIDs := make([]uuid.UUID, 0, len(req.Msg.GetPermissionIds()))
	for _, pidStr := range req.Msg.GetPermissionIds() {
		pid, err := uuid.Parse(pidStr)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		permissionIDs = append(permissionIDs, pid)
	}

	role, err := h.service.CreateRole(ctx, companyID, req.Msg.GetName(), req.Msg.GetDescription(), rbac.RoleLevel(req.Msg.GetLevel()), permissionIDs)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	return connect.NewResponse(&platformv1.CreateRoleResponse{Role: roleToProto(role)}), nil
}

func (h *RBACHandler) AssignRole(ctx context.Context, req *connect.Request[platformv1.AssignRoleRequest]) (*connect.Response[platformv1.AssignRoleResponse], error) {
	companyID, err := uuid.Parse(req.Msg.GetCompanyId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	userID, err := uuid.Parse(req.Msg.GetUserId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	roleID, err := uuid.Parse(req.Msg.GetRoleId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	currentUserIDStr, _, _ := server.ExtractClaims(ctx)
	if currentUserIDStr == "" {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}

	currentUserID, err := uuid.Parse(currentUserIDStr)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}

	if err := h.service.AssignRole(ctx, companyID, userID, roleID, currentUserID); err != nil {
		if strings.Contains(err.Error(), "only the owner") {
			return nil, connect.NewError(connect.CodePermissionDenied, err)
		}
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	return connect.NewResponse(&platformv1.AssignRoleResponse{}), nil
}

func (h *RBACHandler) RemoveRole(ctx context.Context, req *connect.Request[platformv1.RemoveRoleRequest]) (*connect.Response[platformv1.RemoveRoleResponse], error) {
	companyID, err := uuid.Parse(req.Msg.GetCompanyId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	userID, err := uuid.Parse(req.Msg.GetUserId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	roleID, err := uuid.Parse(req.Msg.GetRoleId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	if err := h.service.RemoveRole(ctx, companyID, userID, roleID); err != nil {
		if strings.Contains(err.Error(), "cannot remove the owner") {
			return nil, connect.NewError(connect.CodePermissionDenied, err)
		}
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	return connect.NewResponse(&platformv1.RemoveRoleResponse{}), nil
}

func (h *RBACHandler) ListPermissions(ctx context.Context, _ *connect.Request[platformv1.ListPermissionsRequest]) (*connect.Response[platformv1.ListPermissionsResponse], error) {
	perms, err := h.service.ListPermissions(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	protoPerms := make([]*platformv1.PermissionResponse, 0, len(perms))
	for _, p := range perms {
		protoPerms = append(protoPerms, &platformv1.PermissionResponse{
			Id:       p.ID.String(),
			Feature:  p.Feature,
			Action:   p.Action,
			Resource: p.Resource,
		})
	}

	return connect.NewResponse(&platformv1.ListPermissionsResponse{Permissions: protoPerms}), nil
}

func (h *RBACHandler) GetUserPermissions(ctx context.Context, req *connect.Request[platformv1.GetUserPermissionsRequest]) (*connect.Response[platformv1.GetUserPermissionsResponse], error) {
	companyID, err := uuid.Parse(req.Msg.GetCompanyId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	userID, err := uuid.Parse(req.Msg.GetUserId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	perms, role, err := h.service.GetUserPermissions(ctx, companyID, userID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}

	protoPerms := make([]*platformv1.PermissionResponse, 0, len(perms))
	for _, p := range perms {
		protoPerms = append(protoPerms, &platformv1.PermissionResponse{
			Id:       p.ID.String(),
			Feature:  p.Feature,
			Action:   p.Action,
			Resource: p.Resource,
		})
	}

	return connect.NewResponse(&platformv1.GetUserPermissionsResponse{
		Permissions: protoPerms,
		Role:        roleToProto(role),
	}), nil
}

func (h *RBACHandler) CheckPermission(ctx context.Context, req *connect.Request[platformv1.CheckPermissionRequest]) (*connect.Response[platformv1.CheckPermissionResponse], error) {
	companyID, err := uuid.Parse(req.Msg.GetCompanyId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	userID, err := uuid.Parse(req.Msg.GetUserId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	permission := req.Msg.GetFeature() + ":" + req.Msg.GetAction()
	allowed, err := h.enforcer.HasPermission(ctx, userID, companyID, permission)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&platformv1.CheckPermissionResponse{Allowed: allowed}), nil
}

func (h *RBACHandler) ResolveMembership(ctx context.Context, req *connect.Request[platformv1.ResolveMembershipRequest]) (*connect.Response[platformv1.ResolveMembershipResponse], error) {
	companyID, err := uuid.Parse(req.Msg.GetCompanyId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	userID, err := uuid.Parse(req.Msg.GetUserId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	resolved, err := h.resolver.ResolveMembership(ctx, companyID, userID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}

	return connect.NewResponse(&platformv1.ResolveMembershipResponse{
		CompanyId:       resolved.CompanyID.String(),
		UserId:          resolved.UserID.String(),
		RoleLevel:       int32(resolved.RoleLevel),
		RoleName:        resolved.RoleName,
		SourceCompanyId: resolved.SourceCompany.String(),
		IsDirect:        resolved.IsDirect,
		CappedLevel:     int32(resolved.CappedLevel),
		AccessLevel:     resolved.AccessLevel,
	}), nil
}

func roleToProto(role rbac.Role) *platformv1.RoleData {
	return &platformv1.RoleData{
		Id:          role.ID.String(),
		Name:        role.Name,
		Description: role.Description,
		Level:       int32(role.Level),
		IsSystem:    role.IsSystem,
	}
}
