// Package household provides the Eden Family household model: a first-class
// "household" (a family) whose members are keyed on a logical AOID identity.
//
// Re-homing (Eden Family ADR, Option B): membership keys on a logical
// identity_id = aoid.identities(id), validated at provisioning time. It is NOT
// an enforced cross-DB foreign key — platform (eden_platform) and AOID (aoid)
// live in separate databases. platform_households.primary_contact_identity_id
// is likewise a logical identity reference.
//
// Two axes describe a member:
//
//   - Role (relationship): guardian | adult | child
//   - Capabilities:        manager       (0..n per household, >=1 required)
//                          account_owner (exactly 1 per household — holds the
//                                         AOcyber subscription / card)
//
// Invariants (enforced across DB constraints + service guards + tests):
//   - a child can never be a manager or an account_owner;
//   - the account_owner must be an adult (a non-child member);
//   - a household has at least one manager;
//   - a household has exactly one account_owner.
//
// The package is transport-agnostic: it exposes Go domain types and a Service
// that wraps a Store. Persistence lives in platform/pgstore (PostgreSQL).
package household

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Role enumerates the relationship a member has within a household. It is the
// relationship axis only; billing / management authority is the capability
// axis (IsManager, IsAccountOwner).
type Role string

const (
	// RoleGuardian is a legally-responsible adult (parent / legal guardian).
	// Guardians are the members eligible to grant COPPA / GDPR-K consent.
	RoleGuardian Role = "guardian"
	// RoleAdult is an adult member who is not a legal guardian of a child in
	// the household (e.g. extended family on a shared family plan).
	RoleAdult Role = "adult"
	// RoleChild is a minor; birthdate is required. A child can hold no
	// capability.
	RoleChild Role = "child"
)

// Valid reports whether r is a known role.
func (r Role) Valid() bool {
	switch r {
	case RoleGuardian, RoleAdult, RoleChild:
		return true
	}
	return false
}

// IsChild reports whether r is the child role.
func (r Role) IsChild() bool { return r == RoleChild }

// CanGrantConsent reports whether a member with this role may grant
// COPPA / GDPR-K consent on behalf of a child member.
//
// NOTE (Phase 0 decision — flagged for the ADR owner): under the re-homed
// role model only guardians grant consent. The old model's "parent" role maps
// to guardian. Whether a non-guardian adult who is a parent_of_record should
// also be eligible is intentionally deferred to the consent-eligibility
// design (Phase 0.3 / consent package owner).
func (r Role) CanGrantConsent() bool { return r == RoleGuardian }

// CanBeAccountOwner reports whether a member with this role may hold the
// account_owner capability. Per the ADR the account_owner "must be an adult";
// this is read as "must not be a child" (guardian and adult both qualify).
// Flagged: if the ADR means strictly role == adult, tighten this predicate.
func (r Role) CanBeAccountOwner() bool { return r != RoleChild }

// CanBeManager reports whether a member with this role may hold the manager
// capability. A child can never be a manager.
func (r Role) CanBeManager() bool { return r != RoleChild }

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

// Household represents a family / billable group. primary_contact_identity_id
// is a logical AOID identity reference (see package doc).
type Household struct {
	ID                       uuid.UUID
	PrimaryContactIdentityID uuid.UUID
	DisplayName              string
	Metadata                 json.RawMessage
	CreatedAt                time.Time
	UpdatedAt                time.Time
}

// Member is a person associated with a household, keyed on a logical AOID
// identity. IsManager / IsAccountOwner are the enforced capability axis;
// Capabilities is a forward-compatible JSONB bag for future, non-enforced
// per-member flags.
type Member struct {
	ID             uuid.UUID
	HouseholdID    uuid.UUID
	IdentityID     uuid.UUID
	Role           Role
	Status         Status
	IsManager      bool
	IsAccountOwner bool
	Birthdate      *time.Time
	Capabilities   json.RawMessage
	AddedAt        time.Time
	RemovedAt      *time.Time
}

// ParentOfRecord links a child member to a legally-responsible parent /
// guardian member. Used by platform/consent to determine consent eligibility.
// A child may have multiple parents-of-record across split households.
type ParentOfRecord struct {
	ID             uuid.UUID
	ChildMemberID  uuid.UUID
	ParentMemberID uuid.UUID
	EstablishedAt  time.Time
	RevokedAt      *time.Time
}
