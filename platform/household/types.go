// Package household provides the family / parent-of-record / child-account
// model that backs AOFamily-AI today and Eden Family at launch.
//
// The package is deliberately transport-agnostic: it exposes Go domain types
// and a Service that wraps a Store. Persistence implementations live in
// platform/pgstore (PostgreSQL) and platform/devstore (in-memory dev backend).
//
// Key concepts:
//   - Household: a billable / governable group keyed on a primary contact user
//   - Member: an individual associated with a household, with a role and status
//   - ParentOfRecord: the legally-responsible parent for a child member
//     (COPPA / GDPR Article 8). A child may have multiple parents-of-record
//     across split households.
//
// All mutations route through Service to guarantee an audit trail.
package household

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Role enumerates the relationship a member has within a household.
//
// COPPA / GDPR-K logic keys off RoleChild + a member birthdate < 13 years.
// Only RoleParentOfRecord and RoleGuardian are eligible to grant consent
// for a child member.
type Role string

const (
	// RoleParentOfRecord is the legally responsible parent.
	RoleParentOfRecord Role = "parent"
	// RoleChild is a minor; birthdate is required.
	RoleChild Role = "child"
	// RoleGuardian is a non-parent legal guardian; eligible to grant consent.
	RoleGuardian Role = "guardian"
	// RoleAdultNonParent is an adult member who is not a parent-of-record
	// (e.g., shared family-plan with extended family).
	RoleAdultNonParent Role = "adult_non_parent"
	// RoleOther is a catch-all for relationships that don't fit the model.
	RoleOther Role = "other"
)

// Valid reports whether r is a known role.
func (r Role) Valid() bool {
	switch r {
	case RoleParentOfRecord, RoleChild, RoleGuardian, RoleAdultNonParent, RoleOther:
		return true
	}
	return false
}

// CanGrantConsent reports whether a member with this role may grant
// COPPA/GDPR-K consent on behalf of a child member.
func (r Role) CanGrantConsent() bool {
	return r == RoleParentOfRecord || r == RoleGuardian
}

// Status is the lifecycle state of a household member.
type Status string

const (
	// StatusPending — invited but not accepted.
	StatusPending Status = "pending"
	// StatusActive — full member.
	StatusActive Status = "active"
	// StatusRemoved — soft-deleted; preserved for audit.
	StatusRemoved Status = "removed"
)

// Valid reports whether s is a known status.
func (s Status) Valid() bool {
	switch s {
	case StatusPending, StatusActive, StatusRemoved:
		return true
	}
	return false
}

// Capabilities is a forward-compatible bag of per-member permissions that
// are independent of role. Persisted as JSONB.
//
// A new capability can be added without a migration; consumers reading older
// rows will see the zero value (false) for unknown fields.
type Capabilities struct {
	CanInviteMembers bool `json:"can_invite_members,omitempty"`
	CanManageBilling bool `json:"can_manage_billing,omitempty"`
	CanGrantConsent  bool `json:"can_grant_consent,omitempty"`
	CanViewAuditLog  bool `json:"can_view_audit_log,omitempty"`
}

// DefaultCapabilities returns the recommended capability set for a role.
// Callers may override before calling AddMember.
func DefaultCapabilities(r Role) Capabilities {
	switch r {
	case RoleParentOfRecord:
		return Capabilities{
			CanInviteMembers: true,
			CanManageBilling: true,
			CanGrantConsent:  true,
			CanViewAuditLog:  true,
		}
	case RoleGuardian:
		return Capabilities{
			CanInviteMembers: true,
			CanGrantConsent:  true,
			CanViewAuditLog:  true,
		}
	case RoleAdultNonParent:
		return Capabilities{
			CanInviteMembers: false,
		}
	case RoleChild, RoleOther:
		return Capabilities{}
	}
	return Capabilities{}
}

// Household represents a family / billable group.
type Household struct {
	ID                   uuid.UUID
	PrimaryContactUserID uuid.UUID
	DisplayName          string
	Metadata             json.RawMessage
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// Member is a person associated with a household.
type Member struct {
	ID           uuid.UUID
	HouseholdID  uuid.UUID
	UserID       uuid.UUID
	Role         Role
	Status       Status
	Birthdate    *time.Time
	Capabilities Capabilities
	AddedAt      time.Time
	RemovedAt    *time.Time
}

// ParentOfRecord links a child member to a legally-responsible parent
// member. Used by platform/consent to determine consent eligibility.
type ParentOfRecord struct {
	ID             uuid.UUID
	ChildMemberID  uuid.UUID
	ParentMemberID uuid.UUID
	EstablishedAt  time.Time
	RevokedAt      *time.Time
}
