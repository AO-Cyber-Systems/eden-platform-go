package cms

import (
	"errors"
	"fmt"
	"time"
)

// This file owns the draft -> scheduled -> published (-> archived)
// transition rules for Page.Status, and the due-at visibility gate the
// public read path uses. Page itself (page.go, TRD 41-01) only carries the
// state — Status, ScheduledPublishAt, PublishedAt — as data; this file is
// where that state is validated and moved.
//
// # Why not platform/statemachine
//
// platform/statemachine.Machine composes transition + persistence: its
// Transition method loads current state from a Store[S,E] keyed by an
// opaque instance ID, evaluates the move, then saves the new state plus a
// history entry back through that SAME Store. That is a good fit for a
// workflow whose state lives ONLY in the machine's own store.
//
// Page.Status does not live there. It is a field on the Page struct that
// 41-02's Store/PostgresStore (platform/cms/store.go, store_pg.go — a
// sibling TRD this file does not own) persists as part of the whole Page
// row in one write. Composing statemachine here would mean either:
//
//   - standing up a SECOND, parallel statemachine.Store (in-memory or
//     Postgres-backed) that tracks PageStatus independently of the Page row
//     itself — two sources of truth for the same fact, and a migration
//     this TRD is explicitly barred from adding (that's 41-02's seam); or
//   - calling Machine.Transition with a Store adapter that shells out to
//     41-02's not-yet-written Store on every transition, coupling this
//     file to a persistence interface it does not own and cannot see yet
//     (41-02 runs in the same wave, not before this TRD).
//
// Five states and five hand-checked transitions do not need a generic
// engine to stay correct, and plain validated mutator methods on *Page
// keep exactly one place (the Page row) as the source of truth. Composing
// statemachine was considered and rejected for this reason — see
// 41-03-SUMMARY.md.
//
// # Why the public read path does not wait on a background publisher
//
// The donor implementations (justinforme, smartWellness) pair this state
// machine with a polling Scheduler that leases and flips due rows from
// scheduled to published. That machinery is deliberately NOT ported here
// (it is not in this TRD's file list) because VisibleAt below makes it
// unnecessary for correctness: the public read path compares `now` against
// ScheduledPublishAt directly, so a page becomes visible the instant it is
// due regardless of whether any worker has run yet, and a page that is not
// yet due is invisible regardless of Status bookkeeping lag. A periodic
// worker MAY still call PublishIfDue to reconcile Status for admin-UI
// display purposes, but the public read path's correctness never depends
// on it having run.

// ErrInvalidTransition is returned when a Page's current Status does not
// permit the requested transition (e.g. archiving a draft, scheduling a
// published page).
var ErrInvalidTransition = errors.New("cms: invalid publication-state transition")

// ErrScheduleTimeZero is returned by Schedule when the requested publish
// instant is the zero time.Time value.
var ErrScheduleTimeZero = errors.New("cms: scheduled publish time must not be zero")

// Schedule transitions p into PageStatusScheduled, to publish automatically
// once `at` passes. Allowed from PageStatusDraft (first schedule) or
// PageStatusScheduled (reschedule to a new instant); any other current
// status returns ErrInvalidTransition.
//
// at is normalized with .UTC() before it is stored — Page.ScheduledPublishAt
// is documented as an absolute UTC instant, and a wall-clock time carried in
// a different zone would compare incorrectly against the UTC `now` that
// VisibleAt and PublishIfDue use, publishing early or late. Callers pass
// whatever zone they have; this method is what makes it absolute.
func (p *Page) Schedule(at, now time.Time) error {
	if at.IsZero() {
		return ErrScheduleTimeZero
	}
	switch p.Status {
	case PageStatusDraft, PageStatusScheduled:
	default:
		return fmt.Errorf("%w: cannot schedule from %s", ErrInvalidTransition, p.Status)
	}
	utcAt := at.UTC()
	p.Status = PageStatusScheduled
	p.ScheduledPublishAt = &utcAt
	p.UpdatedAt = now.UTC()
	return nil
}

// Unschedule cancels a pending scheduled publish and reverts p to
// PageStatusDraft. Allowed only from PageStatusScheduled.
func (p *Page) Unschedule(now time.Time) error {
	if p.Status != PageStatusScheduled {
		return fmt.Errorf("%w: cannot unschedule from %s", ErrInvalidTransition, p.Status)
	}
	p.Status = PageStatusDraft
	p.ScheduledPublishAt = nil
	p.UpdatedAt = now.UTC()
	return nil
}

// PublishNow immediately publishes p, overriding any pending schedule.
// Allowed from PageStatusDraft or PageStatusScheduled; any other current
// status (already published, archived) returns ErrInvalidTransition.
//
// Per page.go's contract, PublishedAt is set only the FIRST time a page is
// ever published — a page that is archived and later republished keeps its
// original PublishedAt.
func (p *Page) PublishNow(now time.Time) error {
	switch p.Status {
	case PageStatusDraft, PageStatusScheduled:
	default:
		return fmt.Errorf("%w: cannot publish from %s", ErrInvalidTransition, p.Status)
	}
	nowUTC := now.UTC()
	p.Status = PageStatusPublished
	p.ScheduledPublishAt = nil
	if p.PublishedAt == nil {
		p.PublishedAt = &nowUTC
	}
	p.UpdatedAt = nowUTC
	return nil
}

// Archive withdraws a published page from the public read path without
// deleting it. Allowed only from PageStatusPublished.
func (p *Page) Archive(now time.Time) error {
	if p.Status != PageStatusPublished {
		return fmt.Errorf("%w: cannot archive from %s", ErrInvalidTransition, p.Status)
	}
	p.Status = PageStatusArchived
	p.UpdatedAt = now.UTC()
	return nil
}

// DueForPublish reports whether p is scheduled AND its ScheduledPublishAt
// instant has passed as of now. It does not mutate p — see VisibleAt for
// the read-path visibility gate this backs, and PublishIfDue for the
// mutating equivalent a reconciliation worker can use.
func (p *Page) DueForPublish(now time.Time) bool {
	return p.Status == PageStatusScheduled &&
		p.ScheduledPublishAt != nil &&
		!now.UTC().Before(*p.ScheduledPublishAt)
}

// PublishIfDue publishes p if, and only if, DueForPublish(now) is true.
// Returns whether a transition happened. Intended for an out-of-band
// worker that reconciles Status with ScheduledPublishAt for admin-facing
// display; the public read path's correctness does NOT depend on this
// having been called — see VisibleAt.
func (p *Page) PublishIfDue(now time.Time) (bool, error) {
	if !p.DueForPublish(now) {
		return false, nil
	}
	return true, p.PublishNow(now)
}

// VisibleAt is the public-read-path visibility gate: it reports whether p
// should be served to an unauthenticated visitor at instant now.
//
// This is the security-relevant check named in this TRD's constraints: a
// scheduled-but-not-due page returns false here even if some worker has
// not yet run, and a due scheduled page returns true here even if no
// worker has flipped its Status yet. Visibility is decided by comparing
// `now` to ScheduledPublishAt directly, not by trusting Status alone.
func (p *Page) VisibleAt(now time.Time) bool {
	switch p.Status {
	case PageStatusPublished:
		return true
	case PageStatusScheduled:
		return p.DueForPublish(now)
	default: // draft, archived, or any future status value
		return false
	}
}

// FilterVisible returns the subset of pages visible at instant now,
// preserving relative order. A thin convenience wrapper around VisibleAt
// for a public list/read-path handler.
func FilterVisible(pages []Page, now time.Time) []Page {
	out := make([]Page, 0, len(pages))
	for _, p := range pages {
		if p.VisibleAt(now) {
			out = append(out, p)
		}
	}
	return out
}
