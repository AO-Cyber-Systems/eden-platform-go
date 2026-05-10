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
// emission. Callers populate this from their request context and thread it
// through to Service mutations.
//
// CompanyID is required by the platform audit_logs schema but household
// itself is company-agnostic — for Eden Family, the household's billable
// company id is appropriate; for AOFamily-AI a per-tenant company id works.
type AuditContext struct {
	CompanyID uuid.UUID
	ActorID   uuid.UUID
	IPAddress string
}

// Audit action constants. Stable strings, used as filter values for
// platform/audit log queries.
const (
	ActionHouseholdCreated               = "household.created"
	ActionHouseholdUpdated               = "household.updated"
	ActionHouseholdDeleted               = "household.deleted"
	ActionMemberAdded                    = "household.member_added"
	ActionMemberRoleChanged              = "household.member_role_changed"
	ActionMemberRemoved                  = "household.member_removed"
	ActionParentOfRecordEstablished      = "household.parent_of_record_established"
	ActionParentOfRecordRevoked          = "household.parent_of_record_revoked"

	resourceHousehold = "household"
)

// Service-layer typed errors. Callers can use errors.Is to discriminate.
var (
	// ErrInvalidRole is returned when a Role value is not recognized.
	ErrInvalidRole = errors.New("household: invalid role")
	// ErrChildBirthdateRequired indicates a child member was added without
	// the birthdate needed for COPPA / GDPR-K eligibility checks.
	ErrChildBirthdateRequired = errors.New("household: birthdate required for child member")
	// ErrParentNotEligible indicates the proposed parent_of_record member
	// is not in a role that can grant consent.
	ErrParentNotEligible = errors.New("household: proposed parent is not eligible (must be parent or guardian)")
	// ErrLastParentOfRecord is returned when revoking would leave a child
	// with no parent_of_record. Surfaced for the caller to confirm.
	ErrLastParentOfRecord = errors.New("household: cannot revoke last parent_of_record without replacement")
)

// auditEmitter is the minimal slice of *audit.Logger that Service uses.
// Defining a tiny interface here keeps Service trivially mockable in tests
// and avoids importing the full Logger machinery for in-memory unit tests.
type auditEmitter interface {
	Log(audit.Event)
}

// noopEmitter is used when no audit logger is supplied. Useful in tests
// and during early bring-up before the audit pipeline is wired.
type noopEmitter struct{}

func (noopEmitter) Log(audit.Event) {}

// Service is the public household API. It wraps a Store and emits an
// audit event for every mutating call.
type Service struct {
	store   Store
	auditor auditEmitter
}

// NewService constructs a Service. If logger is nil, audit emission is a
// no-op (suitable for unit tests; production callers should always supply
// a real *audit.Logger).
func NewService(store Store, logger *audit.Logger) *Service {
	var em auditEmitter = noopEmitter{}
	if logger != nil {
		em = logger
	}
	return &Service{store: store, auditor: em}
}

// CreateHousehold creates a new household with the given primary contact
// user as its first owner-class member. The primary contact is implicitly
// added with RoleParentOfRecord and DefaultCapabilities; if that's not
// what the caller wants, it can call UpdateMemberRole afterwards.
func (s *Service) CreateHousehold(ctx context.Context, ac AuditContext, displayName string, metadata json.RawMessage) (Household, error) {
	if metadata == nil {
		metadata = json.RawMessage("{}")
	}
	h, err := s.store.CreateHousehold(ctx, Household{
		PrimaryContactUserID: ac.ActorID,
		DisplayName:          displayName,
		Metadata:             metadata,
	})
	if err != nil {
		return Household{}, fmt.Errorf("create household: %w", err)
	}
	s.emit(ac, ActionHouseholdCreated, h.ID, map[string]any{
		"display_name": displayName,
	})
	return h, nil
}

// GetHousehold returns the household by id (or ErrNotFound).
func (s *Service) GetHousehold(ctx context.Context, id uuid.UUID) (Household, error) {
	return s.store.GetHousehold(ctx, id)
}

// UpdateHousehold updates display_name and metadata. Other fields are
// immutable through this API.
func (s *Service) UpdateHousehold(ctx context.Context, ac AuditContext, h Household) (Household, error) {
	updated, err := s.store.UpdateHousehold(ctx, h)
	if err != nil {
		return Household{}, fmt.Errorf("update household: %w", err)
	}
	s.emit(ac, ActionHouseholdUpdated, updated.ID, map[string]any{
		"display_name": updated.DisplayName,
	})
	return updated, nil
}

// DeleteHousehold cascades through household_members and parent_of_record
// via FK. Use with care; intended for test cleanup or hard-delete flows.
func (s *Service) DeleteHousehold(ctx context.Context, ac AuditContext, id uuid.UUID) error {
	if err := s.store.DeleteHousehold(ctx, id); err != nil {
		return fmt.Errorf("delete household: %w", err)
	}
	s.emit(ac, ActionHouseholdDeleted, id, nil)
	return nil
}

// AddMember adds a new member to a household. For RoleChild, birthdate is
// required (COPPA / GDPR-K compliance gate).
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
		return Member{}, fmt.Errorf("household: invalid status %q", m.Status)
	}
	added, err := s.store.AddMember(ctx, m)
	if err != nil {
		return Member{}, fmt.Errorf("add member: %w", err)
	}
	s.emit(ac, ActionMemberAdded, added.HouseholdID, map[string]any{
		"member_id": added.ID.String(),
		"user_id":   added.UserID.String(),
		"role":      string(added.Role),
	})
	return added, nil
}

// UpdateMemberRole changes a member's role and capabilities atomically.
func (s *Service) UpdateMemberRole(ctx context.Context, ac AuditContext, memberID uuid.UUID, role Role, caps Capabilities) (Member, error) {
	if !role.Valid() {
		return Member{}, fmt.Errorf("%w: %q", ErrInvalidRole, role)
	}
	updated, err := s.store.UpdateMemberRole(ctx, memberID, role, caps)
	if err != nil {
		return Member{}, fmt.Errorf("update member role: %w", err)
	}
	s.emit(ac, ActionMemberRoleChanged, updated.HouseholdID, map[string]any{
		"member_id": updated.ID.String(),
		"role":      string(updated.Role),
	})
	return updated, nil
}

// RemoveMember soft-deletes a member (sets status='removed').
func (s *Service) RemoveMember(ctx context.Context, ac AuditContext, memberID uuid.UUID) error {
	m, err := s.store.GetMember(ctx, memberID)
	if err != nil {
		return fmt.Errorf("get member for removal: %w", err)
	}
	if err := s.store.RemoveMember(ctx, memberID); err != nil {
		return fmt.Errorf("remove member: %w", err)
	}
	s.emit(ac, ActionMemberRemoved, m.HouseholdID, map[string]any{
		"member_id": memberID.String(),
		"user_id":   m.UserID.String(),
	})
	return nil
}

// ListMembers returns active (non-removed) members of a household,
// oldest-added first.
func (s *Service) ListMembers(ctx context.Context, householdID uuid.UUID) ([]Member, error) {
	return s.store.ListMembers(ctx, householdID)
}

// ListHouseholdsForUser returns all active households a user belongs to.
func (s *Service) ListHouseholdsForUser(ctx context.Context, userID uuid.UUID) ([]Household, error) {
	return s.store.ListHouseholdsForUser(ctx, userID)
}

// EstablishParentOfRecord links a parent member to a child member. The
// parent must be Role=parent or Role=guardian.
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
		"child_member_id":  childMemberID.String(),
		"parent_member_id": parentMemberID.String(),
		"parent_of_record_id": por.ID.String(),
	})
	return por, nil
}

// RevokeParentOfRecord soft-deletes a parent_of_record link.
//
// The "last parent" safety invariant is intentionally NOT enforced here
// because the store does not expose a GetParentOfRecord by-id query.
// Callers that want the invariant should call ListParentsOfRecord first
// and reject revocation themselves; ErrLastParentOfRecord is exported as
// a typed error those callers can return.
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

// Compile-time assertion that time is imported (used by Member.Birthdate).
var _ = time.Time{}
