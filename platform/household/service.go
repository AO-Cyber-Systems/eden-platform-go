package household

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/aocybersystems/eden-platform-go/platform/audit"
	"github.com/google/uuid"
)

// AuditContext carries the actor / company / IP triple needed for audit
// emission. For the re-homed household model ActorID is the acting AOID
// identity (aoid.identities(id)) — NOT a platform.users(id). This is a true
// contract as of migration 017, which dropped the audit_logs.actor_id ->
// users(id) FK so actor_id is a logical actor UUID spanning both id-spaces.
// Before 017 an identity actor would FK-fail on insert and be silently
// dropped by the audit logger; see platform/audit/logger.go.
type AuditContext struct {
	CompanyID uuid.UUID
	ActorID   uuid.UUID
	IPAddress string
}

// Audit action constants. Stable strings, used as filter values for
// platform/audit log queries.
const (
	ActionHouseholdCreated          = "household.created"
	ActionHouseholdUpdated          = "household.updated"
	ActionHouseholdDeleted          = "household.deleted"
	ActionMemberAdded               = "household.member_added"
	ActionMemberRoleChanged         = "household.member_role_changed"
	ActionMemberRemoved             = "household.member_removed"
	ActionAccountOwnerTransferred   = "household.account_owner_transferred"
	ActionParentOfRecordEstablished = "household.parent_of_record_established"
	ActionParentOfRecordRevoked     = "household.parent_of_record_revoked"

	resourceHousehold = "household"
)

// Service-layer typed errors. Callers can use errors.Is to discriminate.
var (
	// ErrInvalidRole is returned when a Role value is not recognized.
	ErrInvalidRole = errors.New("household: invalid role")
	// ErrInvalidStatus is returned when a Status value is not recognized.
	ErrInvalidStatus = errors.New("household: invalid status")
	// ErrChildBirthdateRequired indicates a child member was added without the
	// birthdate needed for COPPA / GDPR-K eligibility checks.
	ErrChildBirthdateRequired = errors.New("household: birthdate required for child member")
	// ErrChildCannotHoldCapability indicates an attempt to make a child a
	// manager or account_owner.
	ErrChildCannotHoldCapability = errors.New("household: a child cannot be a manager or account_owner")
	// ErrAccountOwnerMustBeAdult indicates an attempt to make a child the
	// account_owner (the account_owner must be an adult / non-child member).
	ErrAccountOwnerMustBeAdult = errors.New("household: the account_owner must be an adult")
	// ErrLastManager is returned when removing a member would leave the
	// household with no manager.
	ErrLastManager = errors.New("household: cannot remove the last manager")
	// ErrCannotRemoveAccountOwner is returned when removing the account_owner
	// without first transferring the capability.
	ErrCannotRemoveAccountOwner = errors.New("household: cannot remove the account_owner; transfer it first")
	// ErrParentNotEligible indicates the proposed parent_of_record member is
	// not in a role that can grant consent.
	ErrParentNotEligible = errors.New("household: proposed parent is not eligible (must be a guardian)")
)

type auditEmitter interface {
	Log(audit.Event)
}

type noopEmitter struct{}

func (noopEmitter) Log(audit.Event) {}

// Service is the public household API. It wraps a Store and emits an audit
// event for every mutating call, and enforces the household invariants that
// cannot be expressed as DB constraints alone (>=1 manager, transfer-before-
// remove of the account_owner).
type Service struct {
	store   Store
	auditor auditEmitter
}

// NewService constructs a Service. If logger is nil, audit emission is a no-op.
func NewService(store Store, logger *audit.Logger) *Service {
	var em auditEmitter = noopEmitter{}
	if logger != nil {
		em = logger
	}
	return &Service{store: store, auditor: em}
}

// CreateHousehold creates a new household. The primary contact identity is the
// acting identity (ac.ActorID). Members (including the required account_owner
// and >=1 manager) are added via AddMember by the provisioning seam.
func (s *Service) CreateHousehold(ctx context.Context, ac AuditContext, displayName string, metadata json.RawMessage) (Household, error) {
	if metadata == nil {
		metadata = json.RawMessage("{}")
	}
	h, err := s.store.CreateHousehold(ctx, Household{
		PrimaryContactIdentityID: ac.ActorID,
		DisplayName:              displayName,
		Metadata:                 metadata,
	})
	if err != nil {
		return Household{}, fmt.Errorf("create household: %w", err)
	}
	s.emit(ac, ActionHouseholdCreated, h.ID, map[string]any{"display_name": displayName})
	return h, nil
}

// OwnerInput describes the first owner of a household created via
// CreateHouseholdWithOwner. Role must be a non-child role (guardian or adult);
// the owner is always seeded as a manager + account_owner.
type OwnerInput struct {
	IdentityID uuid.UUID
	Role       Role
	Birthdate  *time.Time
}

// CreateHouseholdWithOwner is the provisioning entry point: it atomically
// creates a household AND its first member — a manager + account_owner — so a
// household created via the real path always has exactly one account_owner and
// at least one manager. The empty-household CreateHousehold is retained for
// internal / test use; anything that trusts "the household's account_owner"
// must be provisioned through this seam.
//
// The owner's role must be non-child; a child is rejected with
// ErrChildCannotHoldCapability (an account_owner must be an adult). The
// household's primary_contact_identity_id is set to the owner's identity. The
// insert is one transaction in the store, so a failure on either statement
// rolls back and leaves no orphan household.
func (s *Service) CreateHouseholdWithOwner(ctx context.Context, ac AuditContext, displayName string, owner OwnerInput, metadata json.RawMessage) (*Household, *Member, error) {
	if !owner.Role.Valid() {
		return nil, nil, fmt.Errorf("%w: %q", ErrInvalidRole, owner.Role)
	}
	// The owner is always manager + account_owner: validateCapabilities rejects
	// a child (ErrChildCannotHoldCapability) and any non-adult owner role
	// (ErrAccountOwnerMustBeAdult).
	if err := validateCapabilities(owner.Role, true, true); err != nil {
		return nil, nil, err
	}
	if metadata == nil {
		metadata = json.RawMessage("{}")
	}
	h := Household{
		PrimaryContactIdentityID: owner.IdentityID,
		DisplayName:              displayName,
		Metadata:                 metadata,
	}
	m := Member{
		IdentityID:     owner.IdentityID,
		Role:           owner.Role,
		Status:         StatusActive,
		IsManager:      true,
		IsAccountOwner: true,
		Birthdate:      owner.Birthdate,
	}
	createdHH, createdMember, err := s.store.CreateHouseholdWithOwner(ctx, h, m)
	if err != nil {
		return nil, nil, fmt.Errorf("create household with owner: %w", err)
	}
	s.emit(ac, ActionHouseholdCreated, createdHH.ID, map[string]any{"display_name": displayName})
	s.emit(ac, ActionMemberAdded, createdHH.ID, map[string]any{
		"member_id":        createdMember.ID.String(),
		"identity_id":      createdMember.IdentityID.String(),
		"role":             string(createdMember.Role),
		"is_manager":       createdMember.IsManager,
		"is_account_owner": createdMember.IsAccountOwner,
	})
	return &createdHH, &createdMember, nil
}

// GetHouseholdByID returns the household by id (or ErrNotFound).
func (s *Service) GetHouseholdByID(ctx context.Context, id uuid.UUID) (Household, error) {
	return s.store.GetHouseholdByID(ctx, id)
}

// GetHouseholdForIdentity returns the household an identity belongs to, plus
// that identity's membership (role + capabilities). This is the clean read
// path provisioning and AOID consume.
func (s *Service) GetHouseholdForIdentity(ctx context.Context, identityID uuid.UUID) (Household, Member, error) {
	return s.store.GetHouseholdForIdentity(ctx, identityID)
}

// ListHouseholdsForIdentity returns all active households an identity belongs to.
func (s *Service) ListHouseholdsForIdentity(ctx context.Context, identityID uuid.UUID) ([]Household, error) {
	return s.store.ListHouseholdsForIdentity(ctx, identityID)
}

// UpdateHousehold updates display_name and metadata.
func (s *Service) UpdateHousehold(ctx context.Context, ac AuditContext, h Household) (Household, error) {
	updated, err := s.store.UpdateHousehold(ctx, h)
	if err != nil {
		return Household{}, fmt.Errorf("update household: %w", err)
	}
	s.emit(ac, ActionHouseholdUpdated, updated.ID, map[string]any{"display_name": updated.DisplayName})
	return updated, nil
}

// DeleteHousehold cascades through members and parent_of_record via FK.
func (s *Service) DeleteHousehold(ctx context.Context, ac AuditContext, id uuid.UUID) error {
	if err := s.store.DeleteHousehold(ctx, id); err != nil {
		return fmt.Errorf("delete household: %w", err)
	}
	s.emit(ac, ActionHouseholdDeleted, id, nil)
	return nil
}

// AddMember adds a new member to a household, enforcing the capability
// invariants. For RoleChild, birthdate is required.
func (s *Service) AddMember(ctx context.Context, ac AuditContext, m Member) (Member, error) {
	if !m.Role.Valid() {
		return Member{}, fmt.Errorf("%w: %q", ErrInvalidRole, m.Role)
	}
	if m.Role == RoleChild && m.Birthdate == nil {
		return Member{}, ErrChildBirthdateRequired
	}
	if m.Status == "" {
		m.Status = StatusActive
	}
	if !m.Status.Valid() {
		return Member{}, fmt.Errorf("%w: %q", ErrInvalidStatus, m.Status)
	}
	if err := validateCapabilities(m.Role, m.IsManager, m.IsAccountOwner); err != nil {
		return Member{}, err
	}
	added, err := s.store.AddMember(ctx, m)
	if err != nil {
		return Member{}, fmt.Errorf("add member: %w", err)
	}
	s.emit(ac, ActionMemberAdded, added.HouseholdID, map[string]any{
		"member_id":        added.ID.String(),
		"identity_id":      added.IdentityID.String(),
		"role":             string(added.Role),
		"is_manager":       added.IsManager,
		"is_account_owner": added.IsAccountOwner,
	})
	return added, nil
}

// UpdateMemberRole changes a member's role and capabilities atomically,
// enforcing the same capability invariants as AddMember.
func (s *Service) UpdateMemberRole(ctx context.Context, ac AuditContext, memberID uuid.UUID, role Role, isManager, isAccountOwner bool, caps json.RawMessage) (Member, error) {
	if !role.Valid() {
		return Member{}, fmt.Errorf("%w: %q", ErrInvalidRole, role)
	}
	if err := validateCapabilities(role, isManager, isAccountOwner); err != nil {
		return Member{}, err
	}
	if len(caps) == 0 {
		caps = json.RawMessage("{}")
	}
	updated, err := s.store.UpdateMemberRole(ctx, memberID, role, isManager, isAccountOwner, caps)
	if err != nil {
		return Member{}, fmt.Errorf("update member role: %w", err)
	}
	s.emit(ac, ActionMemberRoleChanged, updated.HouseholdID, map[string]any{
		"member_id": updated.ID.String(),
		"role":      string(updated.Role),
	})
	return updated, nil
}

// SetAccountOwner transfers the account_owner capability to newOwnerMemberID.
// The new owner must be an active, non-child member of householdID. The
// demote-then-promote is atomic in the store.
func (s *Service) SetAccountOwner(ctx context.Context, ac AuditContext, householdID, newOwnerMemberID uuid.UUID) (Member, error) {
	target, err := s.store.GetMember(ctx, newOwnerMemberID)
	if err != nil {
		return Member{}, fmt.Errorf("get new owner: %w", err)
	}
	if target.HouseholdID != householdID {
		return Member{}, ErrNotFound
	}
	if target.Status == StatusRemoved {
		return Member{}, fmt.Errorf("%w: member is removed", ErrNotFound)
	}
	if !target.Role.CanBeAccountOwner() {
		return Member{}, ErrAccountOwnerMustBeAdult
	}
	owner, err := s.store.SetAccountOwner(ctx, householdID, newOwnerMemberID)
	if err != nil {
		return Member{}, fmt.Errorf("set account owner: %w", err)
	}
	s.emit(ac, ActionAccountOwnerTransferred, householdID, map[string]any{
		"new_owner_member_id": newOwnerMemberID.String(),
	})
	return owner, nil
}

// RemoveMember soft-deletes a member (status='removed'), guarding the
// household invariants: the account_owner cannot be removed without first
// transferring the capability, and the last manager cannot be removed.
func (s *Service) RemoveMember(ctx context.Context, ac AuditContext, memberID uuid.UUID) error {
	m, err := s.store.GetMember(ctx, memberID)
	if err != nil {
		return fmt.Errorf("get member for removal: %w", err)
	}
	if m.IsAccountOwner {
		return ErrCannotRemoveAccountOwner
	}
	if m.IsManager {
		n, err := s.store.CountManagers(ctx, m.HouseholdID)
		if err != nil {
			return fmt.Errorf("count managers: %w", err)
		}
		if n <= 1 {
			return ErrLastManager
		}
	}
	if err := s.store.RemoveMember(ctx, memberID); err != nil {
		return fmt.Errorf("remove member: %w", err)
	}
	s.emit(ac, ActionMemberRemoved, m.HouseholdID, map[string]any{
		"member_id":   memberID.String(),
		"identity_id": m.IdentityID.String(),
	})
	return nil
}

// ListMembers returns active (non-removed) members of a household, oldest first.
func (s *Service) ListMembers(ctx context.Context, householdID uuid.UUID) ([]Member, error) {
	return s.store.ListMembers(ctx, householdID)
}

// EstablishParentOfRecord links a parent/guardian member to a child member.
// The parent must be eligible to grant consent (a guardian).
func (s *Service) EstablishParentOfRecord(ctx context.Context, ac AuditContext, childMemberID, parentMemberID uuid.UUID) (ParentOfRecord, error) {
	parent, err := s.store.GetMember(ctx, parentMemberID)
	if err != nil {
		return ParentOfRecord{}, fmt.Errorf("get proposed parent: %w", err)
	}
	if !parent.Role.CanGrantConsent() {
		return ParentOfRecord{}, fmt.Errorf("%w: role=%s", ErrParentNotEligible, parent.Role)
	}
	child, err := s.store.GetMember(ctx, childMemberID)
	if err != nil {
		return ParentOfRecord{}, fmt.Errorf("get child member: %w", err)
	}
	por, err := s.store.EstablishParentOfRecord(ctx, childMemberID, parentMemberID)
	if err != nil {
		return ParentOfRecord{}, fmt.Errorf("establish parent_of_record: %w", err)
	}
	s.emit(ac, ActionParentOfRecordEstablished, child.HouseholdID, map[string]any{
		"child_member_id":     childMemberID.String(),
		"parent_member_id":    parentMemberID.String(),
		"parent_of_record_id": por.ID.String(),
	})
	return por, nil
}

// RevokeParentOfRecord soft-deletes a parent_of_record link.
func (s *Service) RevokeParentOfRecord(ctx context.Context, ac AuditContext, porID uuid.UUID) error {
	if err := s.store.RevokeParentOfRecord(ctx, porID); err != nil {
		return fmt.Errorf("revoke parent_of_record: %w", err)
	}
	s.emit(ac, ActionParentOfRecordRevoked, uuid.Nil, map[string]any{
		"parent_of_record_id": porID.String(),
	})
	return nil
}

// ListParentsOfRecord returns active POR links for a child member.
func (s *Service) ListParentsOfRecord(ctx context.Context, childMemberID uuid.UUID) ([]ParentOfRecord, error) {
	return s.store.ListParentsOfRecord(ctx, childMemberID)
}

// ListChildrenForParent returns active POR links for a parent member.
func (s *Service) ListChildrenForParent(ctx context.Context, parentMemberID uuid.UUID) ([]ParentOfRecord, error) {
	return s.store.ListChildrenForParent(ctx, parentMemberID)
}

// validateCapabilities enforces the child-never-capability and
// account_owner-must-be-adult invariants (also backed by DB CHECKs).
func validateCapabilities(role Role, isManager, isAccountOwner bool) error {
	if role.IsChild() && (isManager || isAccountOwner) {
		return ErrChildCannotHoldCapability
	}
	if isAccountOwner && !role.CanBeAccountOwner() {
		return ErrAccountOwnerMustBeAdult
	}
	return nil
}

func (s *Service) emit(ac AuditContext, action string, resourceID uuid.UUID, details map[string]any) {
	if details == nil {
		details = map[string]any{}
	}
	resID := ""
	if resourceID != uuid.Nil {
		resID = resourceID.String()
	}
	s.auditor.Log(audit.Event{
		CompanyID:  ac.CompanyID.String(),
		ActorID:    ac.ActorID.String(),
		Action:     action,
		Resource:   resourceHousehold,
		ResourceID: resID,
		Details:    details,
		IPAddress:  ac.IPAddress,
	})
}
