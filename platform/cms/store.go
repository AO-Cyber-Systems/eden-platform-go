package cms

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// ErrNotFound is returned when a page lookup (by ID or slug) finds no
// matching row for the given tenant. A page that exists for a DIFFERENT
// company collapses to this same error -- Store methods never leak
// cross-tenant existence.
var ErrNotFound = errors.New("cms: page not found")

// ErrSlugTaken is returned by CreatePage / UpdatePage when the requested
// slug is already in use by another page for the same tenant. Slugs are
// unique per company, not globally -- two tenants may each run an "about"
// page.
var ErrSlugTaken = errors.New("cms: slug already in use for this tenant")

// Store persists Pages -- each carrying its ordered Blocks -- for one
// tenant's content library.
//
// Store is an INTERFACE, not just as an abstraction exercise: it is the
// seam that lets any consumer supply its own persistence. PostgresStore
// (store_pg.go) is the production implementation this package ships, but a
// consumer is free to substitute an in-memory fake for tests, or an
// entirely different backend, without this package's higher-level logic
// (publish.go, preview_token.go -- later TRDs in this objective) ever
// needing to know which.
//
// Every method is tenant-scoped by companyID -- consistent with
// platform/company.Company.ID and platform/telephony.Config.CompanyID. A
// caller can never read or mutate another tenant's page by guessing an ID:
// the companyID predicate is part of the lookup itself, not a
// post-hoc filter.
type Store interface {
	// CreatePage inserts a new page for companyID. If p.ID is uuid.Nil, a
	// new ID is generated. If p.Status is empty, it defaults to
	// PageStatusDraft. Returns ErrSlugTaken if p.Slug already exists for
	// this tenant.
	CreatePage(ctx context.Context, p Page) (Page, error)

	// GetPage fetches one page by (companyID, id). Returns ErrNotFound if
	// no such page exists for this tenant -- including when the id exists
	// under a DIFFERENT tenant.
	GetPage(ctx context.Context, companyID, id uuid.UUID) (Page, error)

	// GetPageBySlug fetches one page by (companyID, slug). Returns
	// ErrNotFound on miss, identically to GetPage.
	GetPageBySlug(ctx context.Context, companyID uuid.UUID, slug string) (Page, error)

	// ListPages returns every page belonging to companyID, ordered by
	// creation time (newest first). Never returns another tenant's pages.
	ListPages(ctx context.Context, companyID uuid.UUID) ([]Page, error)

	// UpdatePage persists a full replacement of an existing page's mutable
	// fields (slug, title, blocks, status, scheduled/published instants),
	// keyed by (p.CompanyID, p.ID). This is also the reorder path: a
	// caller that reorders p.Blocks and calls UpdatePage gets that new
	// order back from a subsequent GetPage / GetPageBySlug / ListPages --
	// Blocks round-trips as JSONB and index position IS render order (see
	// Block, ParseBlocks). Returns ErrNotFound if no page matches
	// (p.CompanyID, p.ID); returns ErrSlugTaken if the new slug collides
	// with a DIFFERENT page in the same tenant.
	UpdatePage(ctx context.Context, p Page) (Page, error)

	// DeletePage removes a page, keyed by (companyID, id). Returns
	// ErrNotFound if no such page exists for this tenant.
	DeletePage(ctx context.Context, companyID, id uuid.UUID) error
}
