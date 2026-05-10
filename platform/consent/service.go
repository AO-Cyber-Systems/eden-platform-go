package consent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/aocybersystems/eden-platform-go/platform/audit"
	"github.com/google/uuid"
)

// AuditContext carries actor + company + IP for audit emission, mirroring
// platform/household.AuditContext for symmetry.
type AuditContext struct {
	CompanyID uuid.UUID
	ActorID   uuid.UUID
	IPAddress string
}

// Audit action constants. Stable strings — used as filter values for
// platform/audit log queries.
const (
	ActionConsentGranted = "consent.granted"
	ActionConsentRevoked = "consent.revoked"
	// ActionConsentRead is emitted for validity checks AND list queries; per
	// R35.3, auditors want to know who looked up consent records.
	ActionConsentRead = "consent.read"

	resourceConsent = "consent"
)

// Typed errors.
var (
	// ErrEmptyPurpose indicates Grant was called without a purpose.
	ErrEmptyPurpose = errors.New("consent: purpose is required")
	// ErrAlreadyRevoked indicates the targeted grant entry has already been
	// revoked. Service.Revoke is idempotent in spirit — callers can re-call
	// safely — but we surface this so callers can distinguish "already done"
	// from "newly revoked" if they care.
	ErrAlreadyRevoked = errors.New("consent: entry already revoked")
)

type auditEmitter interface {
	Log(audit.Event)
}

type noopEmitter struct{}

func (noopEmitter) Log(audit.Event) {}

// Service is the public consent API.
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

// Grant inserts a new consent grant entry. The caller is responsible for
// verifying that the consenter member is eligible (Role parent | guardian);
// see platform/household.Role.CanGrantConsent.
func (s *Service) Grant(ctx context.Context, ac AuditContext, req GrantRequest) (Entry, error) {
	if req.Purpose == "" {
		return Entry{}, ErrEmptyPurpose
	}
	if len(req.Scope) == 0 {
		req.Scope = json.RawMessage("{}")
	}
	if len(req.Evidence) == 0 {
		req.Evidence = json.RawMessage("{}")
	}
	e := Entry{
		HouseholdID:        req.HouseholdID,
		PrincipalMemberID:  req.PrincipalMemberID,
		ConsenterMemberID:  req.ConsenterMemberID,
		Purpose:            req.Purpose,
		Scope:              req.Scope,
		ConsentTextVersion: req.ConsentTextVersion,
		Evidence:           req.Evidence,
	}
	stored, err := s.store.InsertEntry(ctx, e)
	if err != nil {
		return Entry{}, fmt.Errorf("insert consent grant: %w", err)
	}
	s.emit(ac, ActionConsentGranted, stored.ID, map[string]any{
		"principal_member_id":  stored.PrincipalMemberID.String(),
		"consenter_member_id":  stored.ConsenterMemberID.String(),
		"purpose":              string(stored.Purpose),
		"consent_text_version": stored.ConsentTextVersion,
	})
	return stored, nil
}

// Revoke inserts a revocation entry pointing at originalID.
//
// revokerMemberID is the household_member id of the person triggering the
// revocation (typically the same as the original consenter, but may be a
// different parent-of-record). It is FK-validated by the persistence layer.
//
// evidence describes the revocation event itself (timestamp, IP, who clicked
// Revoke). Evidence schema is opaque per R35.2.
//
// Returns ErrAlreadyRevoked if the latest entry for the same (principal,
// purpose) is already a revocation referencing originalID.
func (s *Service) Revoke(ctx context.Context, ac AuditContext, originalID, revokerMemberID uuid.UUID, evidence json.RawMessage) (Entry, error) {
	original, err := s.store.GetEntry(ctx, originalID)
	if err != nil {
		return Entry{}, fmt.Errorf("get original entry: %w", err)
	}
	if original.IsRevocation() {
		return Entry{}, fmt.Errorf("consent: cannot revoke a revocation entry")
	}
	// Check most-recent entry for this (principal, purpose) to detect
	// already-revoked.
	latest, err := s.store.LatestForPurpose(ctx, original.PrincipalMemberID, original.Purpose)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Entry{}, fmt.Errorf("check latest entry: %w", err)
	}
	if err == nil && latest.IsRevocation() && latest.RevokesID != nil && *latest.RevokesID == originalID {
		return latest, ErrAlreadyRevoked
	}
	if len(evidence) == 0 {
		evidence = json.RawMessage("{}")
	}
	id := originalID
	rev := Entry{
		HouseholdID:        original.HouseholdID,
		PrincipalMemberID:  original.PrincipalMemberID,
		ConsenterMemberID:  revokerMemberID,
		Purpose:            original.Purpose,
		Scope:              original.Scope,
		ConsentTextVersion: original.ConsentTextVersion,
		Evidence:           evidence,
		RevokesID:          &id,
	}
	stored, err := s.store.InsertEntry(ctx, rev)
	if err != nil {
		return Entry{}, fmt.Errorf("insert revocation: %w", err)
	}
	s.emit(ac, ActionConsentRevoked, stored.ID, map[string]any{
		"original_entry_id":   originalID.String(),
		"principal_member_id": stored.PrincipalMemberID.String(),
		"revoker_member_id":   revokerMemberID.String(),
		"purpose":             string(stored.Purpose),
	})
	return stored, nil
}

// IsValid checks whether a consent is currently valid for (principal, purpose)
// at the given time. Time is supplied to support historical queries; the
// validity check is "as-of T" semantics.
//
// Emits an ActionConsentRead audit event regardless of result.
func (s *Service) IsValid(ctx context.Context, ac AuditContext, principalMemberID uuid.UUID, purpose Purpose, at time.Time) (Validity, error) {
	if purpose == "" {
		return Validity{}, ErrEmptyPurpose
	}
	latest, err := s.store.LatestForPurpose(ctx, principalMemberID, purpose)
	s.emit(ac, ActionConsentRead, principalMemberID, map[string]any{
		"principal_member_id": principalMemberID.String(),
		"purpose":             string(purpose),
		"as_of":               at.Format(time.RFC3339),
	})
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Validity{Valid: false}, nil
		}
		return Validity{}, fmt.Errorf("latest for purpose: %w", err)
	}
	if latest.GrantedAt.After(at) {
		// All entries for this principal/purpose are after the requested time
		// — there was nothing in effect at T.
		return Validity{Valid: false, LatestEntry: &latest}, nil
	}
	v := Validity{LatestEntry: &latest, Valid: !latest.IsRevocation()}
	return v, nil
}

// ListForPrincipal returns the principal's consent history, newest first.
// Emits ActionConsentRead.
func (s *Service) ListForPrincipal(ctx context.Context, ac AuditContext, principalMemberID uuid.UUID, limit, offset int32) ([]Entry, error) {
	entries, err := s.store.ListForPrincipal(ctx, principalMemberID, limit, offset)
	s.emit(ac, ActionConsentRead, principalMemberID, map[string]any{
		"principal_member_id": principalMemberID.String(),
		"operation":           "list",
		"limit":               limit,
		"offset":              offset,
	})
	if err != nil {
		return nil, fmt.Errorf("list for principal: %w", err)
	}
	return entries, nil
}

// GetEntry returns a single ledger row by id. Emits ActionConsentRead.
func (s *Service) GetEntry(ctx context.Context, ac AuditContext, id uuid.UUID) (Entry, error) {
	entry, err := s.store.GetEntry(ctx, id)
	s.emit(ac, ActionConsentRead, id, map[string]any{
		"entry_id":  id.String(),
		"operation": "get",
	})
	if err != nil {
		return Entry{}, err
	}
	return entry, nil
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
		Resource:   resourceConsent,
		ResourceID: resID,
		Details:    details,
		IPAddress:  ac.IPAddress,
	})
}
