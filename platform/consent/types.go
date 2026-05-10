// Package consent provides an append-only COPPA / GDPR-K consent ledger
// keyed to household members.
//
// The ledger stores one row per consent event (grant or revocation). Existing
// rows are NEVER mutated — revocations are new rows whose RevokesID points at
// the original grant. Append-only is enforced by both the Service layer and a
// PostgreSQL row-level trigger (defense in depth).
//
// All mutating and read operations emit audit events via platform/audit; the
// "consent.read" action is essential for compliance audits.
//
// This package intentionally does NOT import platform/household. Eligibility
// to grant consent (parent / guardian role check) is the caller's
// responsibility — typically composed via household.Service.GetMember +
// household.Role.CanGrantConsent. This keeps consent independently testable
// and avoids any future circular dependency.
package consent

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Purpose identifies what a consent record covers. The string is opaque from
// the package's perspective — callers may use any value — but standard
// purposes are exported for convention.
type Purpose string

const (
	// PurposeChildAccountCreation — allowing a child account to exist at all.
	PurposeChildAccountCreation Purpose = "child_account_creation"
	// PurposeAITutorInteraction — letting an AI talk to the child.
	PurposeAITutorInteraction Purpose = "ai_tutor_interaction"
	// PurposeMarketingCommunications — receiving marketing email / push.
	PurposeMarketingCommunications Purpose = "marketing_communications"
	// PurposeDataExport — exporting account data on request.
	PurposeDataExport Purpose = "data_export"
	// PurposeThirdPartySharing — sharing data with a named third party.
	PurposeThirdPartySharing Purpose = "third_party_sharing"
)

// Evidence describes what was captured at consent time. The package stores
// it as opaque JSONB; the struct documents the canonical shape but the JSON
// column accepts any object.
//
// Standard methods documented in README:
//   - "click_through": user clicked an Accept button
//   - "credit_card":   verified parent identity via card auth
//   - "signed_pdf":    cryptographically-signed PDF stored externally
//   - "webhook":       third-party verification provider returned approved
type Evidence struct {
	Method    string         `json:"method"`
	Recorded  time.Time      `json:"recorded_at"`
	IPAddress string         `json:"ip_address,omitempty"`
	UserAgent string         `json:"user_agent,omitempty"`
	Reference string         `json:"reference,omitempty"`
	Custom    map[string]any `json:"custom,omitempty"`
}

// MarshalJSON returns a JSONB-friendly encoding of Evidence. Use Evidence.JSON
// to encode for storage; the package never validates evidence shape.
func (e Evidence) JSON() (json.RawMessage, error) {
	return json.Marshal(e)
}

// Entry is one row of the append-only ledger. RevokesID is non-nil for
// revocation entries.
type Entry struct {
	ID                 uuid.UUID
	HouseholdID        uuid.UUID
	PrincipalMemberID  uuid.UUID
	ConsenterMemberID  uuid.UUID
	Purpose            Purpose
	Scope              json.RawMessage
	ConsentTextVersion string
	Evidence           json.RawMessage
	GrantedAt          time.Time
	RevokesID          *uuid.UUID
	CreatedAt          time.Time
}

// IsRevocation reports whether this entry revokes a prior grant.
func (e Entry) IsRevocation() bool {
	return e.RevokesID != nil
}

// Validity is the result of a "is consent valid?" lookup. LatestEntry is the
// ledger row whose presence (or absence of revocation) determined the result;
// it is exposed for traceability so callers can record the underlying ledger
// id alongside whatever business decision they make.
type Validity struct {
	Valid       bool
	LatestEntry *Entry
}

// GrantRequest is the input to Service.Grant. Held as a struct so future
// fields can be added without breaking the API.
type GrantRequest struct {
	HouseholdID        uuid.UUID
	PrincipalMemberID  uuid.UUID
	ConsenterMemberID  uuid.UUID
	Purpose            Purpose
	Scope              json.RawMessage
	ConsentTextVersion string
	Evidence           json.RawMessage
}
