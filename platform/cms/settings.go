package cms

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Settings is the per-tenant configuration envelope for a company's site:
// its identity (name, contact, social, SEO-ish copy), its navigation, and
// its brand tokens. All three are DATA — this file carries no rendering,
// no template, no visual-output logic. Something above this package turns
// Settings into a rendered page, exactly as Page/Block (page.go, block.go
// — sibling TRDs in this objective) carry content data that a separate
// renderer interprets.
//
// # Three donor implementations reconciled into one shape
//
// This shape is a superset of three prior single-tenant implementations,
// none of which map onto it directly:
//
//   - justinforme's site_settings.go modeled identity as a flat,
//     allowlisted key/value table (site_name, contact_email, ... as
//     string keys) because it extended an EXISTING key/value column the
//     donor app already had. Settings instead uses named, typed fields —
//     this package has no legacy column to extend, and named fields give a
//     renderer compile-time field names instead of stringly-typed key
//     lookups. The KEY SET is taken as the superset (identity + contact +
//     social + a marketing tagline + footer copy + an open-graph image +
//     a description); the SHAPE changed from map[string]string to a
//     struct. The tagline field is named Tagline here, not the donor's
//     more marketing-specific name — this package stays vertical-agnostic
//     (see the objective's own naming gate).
//
//   - smartWellness's nav_items.go owns a full two-level navigation
//     CRUD surface — Manager, Store, hierarchy-depth validation, a
//     transactional Reorder. Settings takes only the DATA SHAPE (NavItem:
//     id, optional parent, label, url, position, open-in-new-tab,
//     published) as its Nav field. The Manager's business rules (parent
//     must itself be top-level, no grandchildren, refuse to demote a row
//     that has children) are NOT ported — those are edit-time policy for
//     an admin surface, not data this package owns, mirroring how
//     block.go carries Block as data while a separate layer owns what
//     block types mean.
//
//   - smartWellness's design.go modeled brand tokens as a flat list of
//     individually-addressable rows (token_set, key, value, description,
//     updated-by, updated-at) — good for a generic admin table, but the
//     per-token audit metadata (who changed THIS ONE token and when) is an
//     audit-log concern (see eden-biz's separate audit.Append pattern),
//     not something Settings itself should carry. BrandTokens groups the
//     same underlying data into three named buckets — Colors, TypeRoles,
//     Spacing — because the objective's own truths name exactly those
//     three categories; Settings carries one UpdatedAt for the row, not
//     one per token.
//
//   - eden-biz's brand.go modeled brand tokens as four fixed named
//     fields (a primary color, an accent color, a logo URL, a display
//     font constrained to a curated four-font allowlist) with strict
//     validation. The fixed-field shape does not generalize — a tenant
//     might want more than two named colors, or a type-role scheme with
//     no analog to "display font" at all — so Colors/TypeRoles are open
//     maps instead. LogoURL is kept as its own named field (a logo is
//     singular, not a token bucket). The curated-font ALLOWLIST was
//     deliberately NOT ported: which fonts a renderer has licensed is
//     consumer policy, not platform data shape. The hex-color format
//     guard (ValidateHexColor below) WAS ported, and the safe-URL check
//     for LogoURL (ValidateLogoURL below) was ported too, generalized —
//     both are policy-free "is this a well-formed value" guards, useful
//     to any caller regardless of which fonts or colors it allows.
//
// # Documented defaults for a tenant with no row
//
// A tenant that has never saved settings gets DefaultSettings(companyID),
// never an error: a zero-configuration tenant must still be renderable.
// The defaults are: CompanyID set, every string field empty, Nav nil (no
// navigation), and Brand holding three non-nil-but-empty maps (Colors,
// TypeRoles, Spacing) plus an empty LogoURL — never a nil BrandTokens, so
// a caller can range over Brand.Colors etc. without a nil check. See
// DefaultSettings and MemorySettingsStore.GetSettings.
//
// # No Postgres-backed store in this file
//
// This TRD owns only settings.go/settings_test.go — not store.go,
// store_pg.go, or migrations/ (those belong to a sibling TRD in this
// objective, and migrations/ specifically is off-limits per this TRD's
// file-ownership scope). SettingsStore is defined here as the persistence
// seam a Postgres implementation would satisfy, and MemorySettingsStore is
// shipped as the reference implementation that proves the round-trip and
// documented-defaults contracts today. A cms_site_settings-backed
// PostgresSettingsStore is a natural, small follow-up once a migration
// exists — this file makes that gap visible rather than papering over it
// with a fabricated table this TRD is not allowed to create, following the
// same file-ownership precedent this package's preview-token file
// documents for its own no-store constraint.

// SocialLinks is the set of social profile URLs a tenant's site surfaces
// in its footer/header. All three fields are optional; an empty string
// means "not set", not "unset intentionally" vs "never configured" — this
// package does not distinguish those two cases.
type SocialLinks struct {
	Twitter   string `json:"twitter,omitempty"`
	Facebook  string `json:"facebook,omitempty"`
	Instagram string `json:"instagram,omitempty"`
}

// NavItem is one entry in a tenant's site navigation. ParentID nil means a
// top-level entry; a non-nil ParentID references another NavItem's ID in
// the same tenant's Nav slice. Position IS render order within a sibling
// group (top-level items share one ordering; each parent's children share
// their own) — this package does not compute or validate Position, it
// only carries whatever the caller assigned, exactly as Block.ID/Type/Data
// are carried without interpretation in block.go.
//
// This package does not enforce navigation depth, uniqueness, or
// parent/child consistency (e.g. a ParentID that points at a non-existent
// or deleted item) — those are edit-time policy for a higher-level admin
// surface, not this data-only type.
type NavItem struct {
	ID           uuid.UUID  `json:"id"`
	ParentID     *uuid.UUID `json:"parent_id,omitempty"`
	Label        string     `json:"label"`
	URL          string     `json:"url"`
	Position     int        `json:"position"`
	OpenInNewTab bool       `json:"open_in_new_tab,omitempty"`
	IsPublished  bool       `json:"is_published,omitempty"`
}

// BrandTokens is a tenant's visual design vocabulary as pure data: named
// colors, named type roles, and named spacing values, plus a single logo
// URL. Colors/TypeRoles/Spacing are open maps (token name -> token value)
// rather than fixed fields, so a tenant can define as many or as few
// named tokens as its renderer understands — this package assigns no
// meaning to any particular key (not even "primary" or "accent") and
// performs no validation of map contents beyond what a caller explicitly
// runs through ValidateHexColor / ValidateLogoURL.
//
// Nothing in this type, or anywhere else in this file, turns these values
// into a rendered visual output. That is a renderer's job — see this
// file's package-level doc comment.
type BrandTokens struct {
	Colors    map[string]string `json:"colors,omitempty"`
	TypeRoles map[string]string `json:"type_roles,omitempty"`
	Spacing   map[string]string `json:"spacing,omitempty"`
	LogoURL   string            `json:"logo_url,omitempty"`
}

// Settings is one tenant's full site configuration: identity, navigation,
// and brand tokens. CompanyID is the tenant key, matching Page.CompanyID
// and platform/company.Company.ID.
type Settings struct {
	CompanyID uuid.UUID `json:"company_id"`

	// Identity
	SiteName        string      `json:"site_name"`
	SiteDescription string      `json:"site_description"`
	ContactEmail    string      `json:"contact_email"`
	ContactPhone    string      `json:"contact_phone"`
	Tagline         string      `json:"tagline"`
	FooterText      string      `json:"footer_text"`
	OGImageURL      string      `json:"og_image_url"`
	Social          SocialLinks `json:"social"`

	// Navigation. Nil means no navigation configured, not an error --
	// matches Block/ParseBlocks' "absent means empty list" convention.
	Nav []NavItem `json:"nav,omitempty"`

	// Brand tokens -- see BrandTokens.
	Brand BrandTokens `json:"brand"`

	UpdatedAt time.Time `json:"updated_at"`
}

// DefaultSettings returns the documented defaults for a tenant with no
// saved settings row: CompanyID set, every identity field empty, Nav nil,
// and Brand holding three non-nil empty maps plus an empty LogoURL. This
// is what MemorySettingsStore.GetSettings returns on a miss -- callers
// (including a renderer) never need a separate "no settings yet" branch;
// DefaultSettings IS the fallback content.
func DefaultSettings(companyID uuid.UUID) Settings {
	return Settings{
		CompanyID: companyID,
		Brand: BrandTokens{
			Colors:    map[string]string{},
			TypeRoles: map[string]string{},
			Spacing:   map[string]string{},
		},
	}
}

// SettingsStore persists one Settings row per tenant, keyed by CompanyID
// -- consistent with this package's Store interface (store.go), which
// keys Page the same way. A caller can never read or overwrite another
// tenant's settings by any means this interface exposes.
type SettingsStore interface {
	// GetSettings fetches companyID's settings row. If no row has ever
	// been saved for this tenant, it returns DefaultSettings(companyID)
	// and a nil error -- never ErrNotFound. A tenant with no configured
	// settings is a normal, renderable state, not a failure.
	GetSettings(ctx context.Context, companyID uuid.UUID) (Settings, error)

	// UpsertSettings creates or fully replaces the settings row for
	// s.CompanyID and returns the stored value (with UpdatedAt set to the
	// write time). Returns an error if s.CompanyID is uuid.Nil.
	UpsertSettings(ctx context.Context, s Settings) (Settings, error)
}

// ErrSettingsCompanyRequired is returned by UpsertSettings when the given
// Settings has no CompanyID -- an ownerless settings row is not a valid
// state for a tenant-keyed store.
var ErrSettingsCompanyRequired = errors.New("cms: settings.CompanyID is required")

// MemorySettingsStore is an in-memory SettingsStore, safe for concurrent
// use. It is the reference implementation that proves Settings round-trips
// through a store and that a tenant with no row gets documented defaults
// -- see this file's package doc comment for why no Postgres-backed store
// ships in this TRD (file-ownership boundary: migrations/ belongs to a
// sibling TRD in this objective).
type MemorySettingsStore struct {
	mu   sync.RWMutex
	rows map[uuid.UUID]Settings
}

// NewMemorySettingsStore constructs an empty MemorySettingsStore.
func NewMemorySettingsStore() *MemorySettingsStore {
	return &MemorySettingsStore{rows: make(map[uuid.UUID]Settings)}
}

// Compile-time assertion that MemorySettingsStore satisfies SettingsStore.
var _ SettingsStore = (*MemorySettingsStore)(nil)

// GetSettings implements SettingsStore.
func (m *MemorySettingsStore) GetSettings(_ context.Context, companyID uuid.UUID) (Settings, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if row, ok := m.rows[companyID]; ok {
		return row, nil
	}
	return DefaultSettings(companyID), nil
}

// UpsertSettings implements SettingsStore.
func (m *MemorySettingsStore) UpsertSettings(_ context.Context, s Settings) (Settings, error) {
	if s.CompanyID == uuid.Nil {
		return Settings{}, ErrSettingsCompanyRequired
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s.UpdatedAt = time.Now().UTC()
	m.rows[s.CompanyID] = s
	return s, nil
}

// ── Value guards ────────────────────────────────────────────────────────
//
// These two functions validate that a token VALUE is well-formed enough to
// be safe data for whatever consumes it later (a renderer building an
// <img> src or interpolating a color into a visual-output format). They
// are policy-free: ValidateHexColor does not know or care what a caller
// names its color tokens, and ValidateLogoURL does not know or care what
// image formats a renderer accepts. Callers decide whether/when to run
// them -- BrandTokens itself never calls these; nothing in this package
// rejects an unvalidated value on write.

// reHexColor matches a #rrggbb hex color: exactly six hex digits, anchored
// so no leading/trailing junk can ride along inside an otherwise-valid
// prefix.
var reHexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// ErrInvalidColorToken is returned by ValidateHexColor when v is not a
// well-formed #rrggbb string.
var ErrInvalidColorToken = errors.New("cms: color token must be a #rrggbb hex string")

// ValidateHexColor reports whether v is a well-formed #rrggbb hex color.
// Ported from eden-biz's brand-token validator (the "a stored color is
// re-validated against a hex pattern before it reaches presentation
// output" lesson) generalized to any color-named token, not just a fixed
// primary/accent pair.
func ValidateHexColor(v string) error {
	if !reHexColor.MatchString(v) {
		return ErrInvalidColorToken
	}
	return nil
}

// ErrInvalidLogoURL is returned by ValidateLogoURL when v is neither an
// absolute http(s) URL nor a rooted relative path.
var ErrInvalidLogoURL = errors.New("cms: logo url must be an http(s) URL or a rooted path")

// ValidateLogoURL reports whether v is safe to treat as a logo URL: an
// absolute http(s) URL with a host, or a rooted relative path ("/media/
// ..."), and free of whitespace/angle-bracket/quote characters that could
// break out of a src/href attribute downstream. An empty string is valid
// (it clears the logo). Scheme-relative ("//host/...") and any other
// scheme (javascript:, data:, ...) are rejected.
func ValidateLogoURL(v string) error {
	if v == "" {
		return nil
	}
	if strings.ContainsAny(v, " \t\r\n<>\"'`") {
		return ErrInvalidLogoURL
	}
	u, err := url.Parse(v)
	if err != nil {
		return ErrInvalidLogoURL
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		if u.Host == "" {
			return ErrInvalidLogoURL
		}
		return nil
	case "":
		if strings.HasPrefix(v, "/") && !strings.HasPrefix(v, "//") {
			return nil
		}
		return ErrInvalidLogoURL
	default:
		return ErrInvalidLogoURL
	}
}
