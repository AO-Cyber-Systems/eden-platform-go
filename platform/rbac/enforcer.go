package rbac

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

type cacheEntry struct {
	permissions map[string]bool
	roleLevel   RoleLevel
	expiresAt   time.Time
}

// Enforcer checks user permissions against the RBAC model with caching.
type Enforcer struct {
	store    RBACStore
	cache    sync.Map
	cacheTTL time.Duration
	matrix   PermissionMatrix
}

// NewEnforcer creates a new RBAC enforcer.
func NewEnforcer(store RBACStore, matrix PermissionMatrix) *Enforcer {
	if matrix == nil {
		matrix = DefaultPermissionMatrix()
	}
	return &Enforcer{
		store:    store,
		cacheTTL: 5 * time.Minute,
		matrix:   matrix,
	}
}

// HasPermission checks if a user has a specific permission in a company.
func (e *Enforcer) HasPermission(ctx context.Context, userID, companyID uuid.UUID, permission string) (bool, error) {
	cacheKey := userID.String() + ":" + companyID.String()

	if entry, ok := e.cache.Load(cacheKey); ok {
		ce := entry.(*cacheEntry)
		if time.Now().Before(ce.expiresAt) {
			return ce.permissions[permission], nil
		}
		e.cache.Delete(cacheKey)
	}

	role, err := e.store.GetUserRole(ctx, companyID, userID)
	if err != nil {
		return false, fmt.Errorf("get user role: %w", err)
	}

	perms, err := e.store.ListPermissionsByRole(ctx, role.ID)
	if err != nil {
		return false, fmt.Errorf("list permissions: %w", err)
	}

	permSet := make(map[string]bool, len(perms))
	for _, p := range perms {
		permSet[p.Feature+":"+p.Action] = true
	}

	// Check permission overrides on membership
	membership, err := e.store.GetMembership(ctx, companyID, userID)
	if err == nil && membership.PermissionOverrides != nil {
		var overrides map[string]bool
		if err := json.Unmarshal(membership.PermissionOverrides, &overrides); err == nil {
			for k, v := range overrides {
				permSet[k] = v
			}
		}
	}

	e.cache.Store(cacheKey, &cacheEntry{
		permissions: permSet,
		roleLevel:   role.Level,
		expiresAt:   time.Now().Add(e.cacheTTL),
	})

	return permSet[permission], nil
}

// HasMinimumRole checks if a user meets the minimum role level.
func (e *Enforcer) HasMinimumRole(ctx context.Context, userID, companyID uuid.UUID, minLevel RoleLevel) (bool, error) {
	role, err := e.store.GetUserRole(ctx, companyID, userID)
	if err != nil {
		return false, fmt.Errorf("get user role: %w", err)
	}
	return role.Level >= minLevel, nil
}

// CheckFeatureAction checks if a user can perform an action on a feature.
func (e *Enforcer) CheckFeatureAction(ctx context.Context, userID, companyID uuid.UUID, feature Feature, action string) (bool, error) {
	if actionMatrix, ok := e.matrix[feature]; ok {
		if minLevel, ok := actionMatrix[action]; ok {
			role, err := e.store.GetUserRole(ctx, companyID, userID)
			if err != nil {
				return false, fmt.Errorf("get user role: %w", err)
			}
			if role.Level < minLevel {
				return false, nil
			}
		}
	}
	return e.HasPermission(ctx, userID, companyID, string(feature)+":"+action)
}

// InvalidateCache removes a user's cached permissions.
func (e *Enforcer) InvalidateCache(userID, companyID uuid.UUID) {
	e.cache.Delete(userID.String() + ":" + companyID.String())
}

// InvalidateAll clears the entire permission cache.
func (e *Enforcer) InvalidateAll() {
	e.cache.Range(func(key, _ any) bool {
		e.cache.Delete(key)
		return true
	})
}
