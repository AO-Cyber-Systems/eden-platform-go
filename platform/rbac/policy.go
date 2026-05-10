package rbac

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// Sentinel errors for policy-style authorization outcomes.
var (
	ErrUnauthorized  = errors.New("rbac: not authorized")
	ErrUnknownAction = errors.New("rbac: unknown policy action")
)

// PolicyUser abstracts the authenticated user for policy checks. Donor: aodex-go/policy.
//
// Consumers usually wrap their own user struct in a small adapter that
// implements this interface. Keep it minimal — this exists so the policy
// surface does not couple to any one product's user shape.
type PolicyUser interface {
	GetID() uuid.UUID
	IsAdmin() bool
	IsSuperAdmin() bool
	GetCompanyID() *uuid.UUID
}

// Policy defines the standard CRUD authorization interface. Methods return
// bool only; the Authorize helper wraps results into errors.
type Policy interface {
	Show() bool
	Create() bool
	Update() bool
	Destroy() bool
}

// ActionPolicy extends Policy with custom named actions
// (e.g., "send_message", "archive", "publish").
type ActionPolicy interface {
	Policy
	Can(action string) bool
}

// Authorize returns nil if allowed, ErrUnauthorized if denied,
// ErrUnknownAction if the action is not recognised by the policy.
//
// Standard actions are "show", "create", "update", "destroy".
// Custom actions are dispatched to ActionPolicy.Can.
func Authorize(p Policy, action string) error {
	var allowed bool
	switch action {
	case "show":
		allowed = p.Show()
	case "create":
		allowed = p.Create()
	case "update":
		allowed = p.Update()
	case "destroy":
		allowed = p.Destroy()
	default:
		ap, ok := p.(ActionPolicy)
		if !ok {
			return fmt.Errorf("%w: %s", ErrUnknownAction, action)
		}
		allowed = ap.Can(action)
	}
	if !allowed {
		return ErrUnauthorized
	}
	return nil
}

// BasePolicy provides default-deny behaviour. Embed in concrete policies
// and override the methods you want to allow.
type BasePolicy struct {
	User  PolicyUser
	Admin bool // cached from User.IsAdmin()
}

// NewBasePolicy creates a BasePolicy with admin status cached from the user.
func NewBasePolicy(user PolicyUser) BasePolicy {
	return BasePolicy{
		User:  user,
		Admin: user.IsAdmin(),
	}
}

// Show returns false (default deny).
func (b BasePolicy) Show() bool { return false }

// Create returns false (default deny).
func (b BasePolicy) Create() bool { return false }

// Update returns false (default deny).
func (b BasePolicy) Update() bool { return false }

// Destroy returns false (default deny).
func (b BasePolicy) Destroy() bool { return false }
