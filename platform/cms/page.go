package cms

import (
	"time"

	"github.com/google/uuid"
)

// PageStatus is the publication state of a Page. It is a closed string enum
// so it round-trips human-readably through JSON and Postgres text/enum
// columns, and so the state machine in this package's publish.go (a later
// TRD in this objective) has a fixed set of states to transition between.
type PageStatus string

const (
	// PageStatusDraft is a page's default state: content that exists but has
	// never been made visible on the public read path.
	PageStatusDraft PageStatus = "draft"

	// PageStatusScheduled is content queued to become PageStatusPublished at
	// ScheduledPublishAt. It remains invisible on the public read path until
	// that instant passes — see this package's publish.go.
	PageStatusScheduled PageStatus = "scheduled"

	// PageStatusPublished is live content, visible on the public read path.
	PageStatusPublished PageStatus = "published"

	// PageStatusArchived is content that was published at some point and has
	// since been deliberately withdrawn. It stays off the public read path
	// but is not deleted.
	PageStatusArchived PageStatus = "archived"
)

// Page is the ordered content of one addressable page: its identity, its
// owning tenant, its ordered Blocks, and its publication state.
//
// Page carries NO rendering concerns — no HTML, no templates, no knowledge
// of how a Block.Type maps to a component. It is a data carrier only.
//
//   - Persistence lives behind this package's Store interface (a later TRD).
//   - The draft -> scheduled -> published state machine, due-at evaluation,
//     and preview-token gate live in publish.go / preview_token.go (also
//     later TRDs). This struct only carries the state those own; it does
//     not validate or transition it.
type Page struct {
	ID        uuid.UUID
	CompanyID uuid.UUID // tenant; matches platform/company and platform/telephony's Config.CompanyID
	Slug      string
	Title     string

	// Blocks is the page's ordered content. Index IS render order — see
	// Block and ParseBlocks for the tolerant-parse contract that produces
	// this slice from stored/decoded JSON.
	Blocks []Block

	Status PageStatus

	// ScheduledPublishAt is the absolute UTC instant a scheduled Page
	// becomes published. Meaningful only when Status == PageStatusScheduled;
	// nil otherwise. Never a wall-clock time without a zone — an ambiguous
	// instant here is a content-leak risk (see publish.go).
	ScheduledPublishAt *time.Time

	// PublishedAt is set the first time a Page transitions into
	// PageStatusPublished, and is left unchanged by a later Archive — it
	// answers "has this ever been public", not "is it public now" (that's
	// Status).
	PublishedAt *time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}
