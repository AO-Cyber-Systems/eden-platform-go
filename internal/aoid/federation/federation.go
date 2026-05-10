package federation

import (
	"context"
	"errors"
	"time"

	platsaml "github.com/aocybersystems/eden-platform-go/platform/auth/saml"
	"github.com/google/uuid"
)

// Errors returned by federation registry / manager operations.
var (
	// ErrTenantNotFound — no IdP config for this tenant ID.
	ErrTenantNotFound = errors.New("federation: tenant IdP config not found")
	// ErrTenantInactive — tenant exists but IsActive == false.
	ErrTenantInactive = errors.New("federation: tenant IdP config inactive")
	// ErrExternalIdPNotFound — no inbound IdP entry for the supplied ID.
	ErrExternalIdPNotFound = errors.New("federation: external IdP not found")
	// ErrDuplicateTenant — Register called with an already-known tenant.
	ErrDuplicateTenant = errors.New("federation: tenant IdP config already registered")
	// ErrDuplicateExternalIdP — Register called with a duplicate external IdP.
	ErrDuplicateExternalIdP = errors.New("federation: external IdP already registered")
	// ErrInvalidConfig — config failed validation.
	ErrInvalidConfig = errors.New("federation: invalid configuration")
)

// TenantIdPConfig is the per-tenant configuration that drives one IdP
// instance. Tenants are usually company IDs but the federation package
// is intentionally agnostic — any opaque UUID works.
type TenantIdPConfig struct {
	// TenantID identifies the AO ID tenant. Required.
	TenantID uuid.UUID

	// DisplayName is the admin-facing label (e.g. "Acme Corp IdP").
	DisplayName string

	// EntityID is the canonical IdP entity URL (typically the metadata
	// URL). SPs use this to look up the IdP in their trust store.
	// Required.
	EntityID string

	// SSOURL is the public SSO endpoint that accepts AuthnRequests
	// (HTTP-Redirect + HTTP-POST bindings). Required.
	SSOURL string

	// MetadataURL is the public URL where SPs fetch this tenant's IdP
	// metadata. Distinct from EntityID for deployments where the IdP
	// publishes metadata at a path different from the entity ID
	// (rare, but supported).
	MetadataURL string

	// AllowedSPs is the set of downstream SPs this IdP will issue
	// assertions to. Keyed by SP entity ID.
	AllowedSPs map[string]SPRegistration

	// AttributeTemplate maps an attribute name (claim) → an array of
	// source claim names that the IdP will pull from the AO ID user
	// record. The first non-empty source wins. Example:
	//   "email"      -> ["email"]
	//   "first_name" -> ["given_name","first_name"]
	AttributeTemplate map[string][]string

	// AssertionLifetime is the validity window of issued assertions.
	// Defaults to 5 minutes when zero.
	AssertionLifetime time.Duration

	// IsActive is the on/off switch. Inactive configs return
	// ErrTenantInactive from IdPManager.Get.
	IsActive bool

	// CreatedAt + UpdatedAt are stamped by the registry.
	CreatedAt time.Time
	UpdatedAt time.Time
}

// SPRegistration is the federation-internal SP record. It carries
// admin-facing metadata (Label) and the runtime fields needed to
// validate AuthnRequests + post assertions to the SP's ACS URL.
type SPRegistration struct {
	// EntityID matches the AuthnRequest's Issuer. Required.
	EntityID string

	// ACSURL is the AssertionConsumerService URL on the SP. Required.
	ACSURL string

	// Audience overrides the assertion audience. Empty = use EntityID.
	Audience string

	// Label is the admin-facing display name (e.g. "Partner Portal").
	Label string

	// SigningCertificatePEM, when non-empty, asks the IdP to verify the
	// AuthnRequest's XML signature against this cert. Otherwise the
	// AuthnRequest is accepted unsigned (per Obj 23 behavior).
	SigningCertificatePEM string

	// CreatedAt is stamped by the registry.
	CreatedAt time.Time
}

// Validate returns ErrInvalidConfig wrapping a description when the
// config is missing required fields. Used by registries before
// persistence.
func (c *TenantIdPConfig) Validate() error {
	if c == nil {
		return ErrInvalidConfig
	}
	if c.TenantID == uuid.Nil {
		return errInvalid("TenantID is required")
	}
	if c.EntityID == "" {
		return errInvalid("EntityID is required")
	}
	if c.SSOURL == "" {
		return errInvalid("SSOURL is required")
	}
	return nil
}

// Validate returns ErrInvalidConfig when the SP entry is missing
// required fields.
func (s *SPRegistration) Validate() error {
	if s == nil {
		return ErrInvalidConfig
	}
	if s.EntityID == "" {
		return errInvalid("SPRegistration.EntityID is required")
	}
	if s.ACSURL == "" {
		return errInvalid("SPRegistration.ACSURL is required")
	}
	return nil
}

// Registry is the outbound IdP config store. The in-memory
// implementation is the Phase A reference; pgstore implementations
// satisfy the same interface in a follow-on.
type Registry interface {
	Register(ctx context.Context, cfg TenantIdPConfig) error
	Get(ctx context.Context, tenantID uuid.UUID) (TenantIdPConfig, error)
	Update(ctx context.Context, cfg TenantIdPConfig) error
	Delete(ctx context.Context, tenantID uuid.UUID) error
	List(ctx context.Context, activeOnly bool) ([]TenantIdPConfig, error)
}

// KeyResolver supplies the signing keys for a tenant's IdP. The Phase
// A SharedKeyResolver returns one key for every tenant; rotation-aware
// implementations land later.
type KeyResolver interface {
	Resolve(ctx context.Context, tenantID uuid.UUID) (current, previous *platsaml.SigningKey, err error)
}

// SharedKeyResolver returns the same `Current` (and optional `Previous`)
// key for every tenant. Fine for Phase A — every AOC tenant federates
// under a single AO-controlled signing identity, which is exactly the
// trust model documented in PORTFOLIO_STANDARDIZATION_PLAN §9.
type SharedKeyResolver struct {
	Current  *platsaml.SigningKey
	Previous *platsaml.SigningKey
}

// Resolve returns the shared key pair regardless of tenant ID. Returns
// an error when Current is nil — the IdP would refuse to construct in
// that case anyway.
func (s *SharedKeyResolver) Resolve(_ context.Context, _ uuid.UUID) (*platsaml.SigningKey, *platsaml.SigningKey, error) {
	if s == nil || s.Current == nil {
		return nil, nil, errors.New("federation: SharedKeyResolver has no Current key")
	}
	return s.Current, s.Previous, nil
}

func errInvalid(msg string) error {
	return &invalidConfigError{msg: msg}
}

type invalidConfigError struct {
	msg string
}

func (e *invalidConfigError) Error() string {
	return "federation: invalid configuration: " + e.msg
}

func (e *invalidConfigError) Is(target error) bool {
	return target == ErrInvalidConfig
}
