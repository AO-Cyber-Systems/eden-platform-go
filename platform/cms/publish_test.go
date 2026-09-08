package cms

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func newDraftPage() Page {
	return Page{
		ID:        uuid.New(),
		CompanyID: uuid.New(),
		Slug:      "test-page",
		Title:     "Test Page",
		Status:    PageStatusDraft,
	}
}

func TestSchedule_FromDraft_SetsScheduledStateAndUTCInstant(t *testing.T) {
	p := newDraftPage()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// Deliberately pass a NON-UTC located time to prove Schedule normalizes.
	loc := time.FixedZone("UTC-5", -5*60*60)
	at := time.Date(2026, 1, 2, 10, 0, 0, 0, loc) // = 2026-01-02T15:00:00Z

	if err := p.Schedule(at, now); err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if p.Status != PageStatusScheduled {
		t.Fatalf("Status = %v, want %v", p.Status, PageStatusScheduled)
	}
	if p.ScheduledPublishAt == nil {
		t.Fatal("ScheduledPublishAt is nil")
	}
	if p.ScheduledPublishAt.Location() != time.UTC {
		t.Fatalf("ScheduledPublishAt location = %v, want UTC", p.ScheduledPublishAt.Location())
	}
	want := time.Date(2026, 1, 2, 15, 0, 0, 0, time.UTC)
	if !p.ScheduledPublishAt.Equal(want) {
		t.Fatalf("ScheduledPublishAt = %v, want %v (absolute instant must survive zone normalization)", p.ScheduledPublishAt, want)
	}
	if !p.UpdatedAt.Equal(now) {
		t.Fatalf("UpdatedAt = %v, want %v", p.UpdatedAt, now)
	}
}

func TestSchedule_ZeroTime_Rejected(t *testing.T) {
	p := newDraftPage()
	err := p.Schedule(time.Time{}, time.Now())
	if !errors.Is(err, ErrScheduleTimeZero) {
		t.Fatalf("err = %v, want ErrScheduleTimeZero", err)
	}
	if p.Status != PageStatusDraft {
		t.Fatalf("Status changed on rejected Schedule: %v", p.Status)
	}
}

func TestSchedule_FromPublished_Rejected(t *testing.T) {
	p := newDraftPage()
	now := time.Now().UTC()
	if err := p.PublishNow(now); err != nil {
		t.Fatalf("PublishNow: %v", err)
	}
	err := p.Schedule(now.Add(time.Hour), now)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("err = %v, want ErrInvalidTransition", err)
	}
}

func TestSchedule_Reschedule_OverwritesInstant(t *testing.T) {
	p := newDraftPage()
	now := time.Now().UTC()
	first := now.Add(24 * time.Hour)
	second := now.Add(48 * time.Hour)

	if err := p.Schedule(first, now); err != nil {
		t.Fatalf("first Schedule: %v", err)
	}
	if err := p.Schedule(second, now); err != nil {
		t.Fatalf("reschedule: %v", err)
	}
	if !p.ScheduledPublishAt.Equal(second) {
		t.Fatalf("ScheduledPublishAt = %v, want %v", p.ScheduledPublishAt, second)
	}
}

func TestUnschedule_RevertsToDraft(t *testing.T) {
	p := newDraftPage()
	now := time.Now().UTC()
	if err := p.Schedule(now.Add(time.Hour), now); err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if err := p.Unschedule(now); err != nil {
		t.Fatalf("Unschedule: %v", err)
	}
	if p.Status != PageStatusDraft {
		t.Fatalf("Status = %v, want draft", p.Status)
	}
	if p.ScheduledPublishAt != nil {
		t.Fatalf("ScheduledPublishAt = %v, want nil", p.ScheduledPublishAt)
	}
}

func TestUnschedule_FromDraft_Rejected(t *testing.T) {
	p := newDraftPage()
	err := p.Unschedule(time.Now())
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("err = %v, want ErrInvalidTransition", err)
	}
}

func TestPublishNow_FromDraft_SetsPublishedAtOnce(t *testing.T) {
	p := newDraftPage()
	now := time.Now().UTC()
	if err := p.PublishNow(now); err != nil {
		t.Fatalf("PublishNow: %v", err)
	}
	if p.Status != PageStatusPublished {
		t.Fatalf("Status = %v, want published", p.Status)
	}
	if p.PublishedAt == nil || !p.PublishedAt.Equal(now) {
		t.Fatalf("PublishedAt = %v, want %v", p.PublishedAt, now)
	}

	// Archive then republish: PublishedAt must NOT move (page.go's contract:
	// "set the first time", "left unchanged by a later Archive").
	firstPublishedAt := *p.PublishedAt
	if err := p.Archive(now.Add(time.Hour)); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	// Archived pages can't PublishNow directly per this file's state graph
	// (only draft/scheduled -> published); simulate an admin re-drafting it.
	p.Status = PageStatusDraft
	republishAt := now.Add(2 * time.Hour)
	if err := p.PublishNow(republishAt); err != nil {
		t.Fatalf("republish: %v", err)
	}
	if !p.PublishedAt.Equal(firstPublishedAt) {
		t.Fatalf("PublishedAt moved on republish: got %v, want unchanged %v", p.PublishedAt, firstPublishedAt)
	}
}

func TestPublishNow_ClearsScheduledPublishAt(t *testing.T) {
	p := newDraftPage()
	now := time.Now().UTC()
	if err := p.Schedule(now.Add(time.Hour), now); err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if err := p.PublishNow(now); err != nil {
		t.Fatalf("PublishNow: %v", err)
	}
	if p.ScheduledPublishAt != nil {
		t.Fatalf("ScheduledPublishAt = %v, want nil after PublishNow", p.ScheduledPublishAt)
	}
}

func TestPublishNow_FromArchived_Rejected(t *testing.T) {
	p := newDraftPage()
	now := time.Now().UTC()
	if err := p.PublishNow(now); err != nil {
		t.Fatalf("PublishNow: %v", err)
	}
	if err := p.Archive(now); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	err := p.PublishNow(now)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("err = %v, want ErrInvalidTransition", err)
	}
}

func TestArchive_FromDraft_Rejected(t *testing.T) {
	p := newDraftPage()
	err := p.Archive(time.Now())
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("err = %v, want ErrInvalidTransition", err)
	}
}

func TestDueForPublish(t *testing.T) {
	base := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		status PageStatus
		at     *time.Time
		now    time.Time
		want   bool
	}{
		{"not scheduled", PageStatusDraft, nil, base, false},
		{"scheduled, not due", PageStatusScheduled, tPtr(base.Add(time.Hour)), base, false},
		{"scheduled, exactly due", PageStatusScheduled, tPtr(base), base, true},
		{"scheduled, past due", PageStatusScheduled, tPtr(base.Add(-time.Hour)), base, true},
		{"published, has instant (ignored)", PageStatusPublished, tPtr(base.Add(-time.Hour)), base, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Page{Status: tt.status, ScheduledPublishAt: tt.at}
			if got := p.DueForPublish(tt.now); got != tt.want {
				t.Fatalf("DueForPublish = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPublishIfDue_NotDue_NoOp(t *testing.T) {
	p := newDraftPage()
	now := time.Now().UTC()
	if err := p.Schedule(now.Add(time.Hour), now); err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	transitioned, err := p.PublishIfDue(now)
	if err != nil {
		t.Fatalf("PublishIfDue: %v", err)
	}
	if transitioned {
		t.Fatal("PublishIfDue transitioned a not-yet-due page")
	}
	if p.Status != PageStatusScheduled {
		t.Fatalf("Status = %v, want scheduled (unchanged)", p.Status)
	}
}

func TestPublishIfDue_Due_Publishes(t *testing.T) {
	p := newDraftPage()
	now := time.Now().UTC()
	if err := p.Schedule(now.Add(time.Hour), now); err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	later := now.Add(2 * time.Hour)
	transitioned, err := p.PublishIfDue(later)
	if err != nil {
		t.Fatalf("PublishIfDue: %v", err)
	}
	if !transitioned {
		t.Fatal("PublishIfDue did not transition a due page")
	}
	if p.Status != PageStatusPublished {
		t.Fatalf("Status = %v, want published", p.Status)
	}
}

// TestVisibleAt_ScheduledNotDue_NotServed proves the must_have: "Content
// that is scheduled but not yet due is NOT served by the public path" —
// and specifically that this holds WITHOUT PublishIfDue (or any worker)
// having run, since VisibleAt is the actual read-path gate.
func TestVisibleAt_ScheduledNotDue_NotServed(t *testing.T) {
	p := newDraftPage()
	now := time.Now().UTC()
	if err := p.Schedule(now.Add(time.Hour), now); err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if p.VisibleAt(now) {
		t.Fatal("scheduled-but-not-due page is visible; must be invisible")
	}
}

// TestVisibleAt_ScheduledDue_ServedWithoutWorker proves the read path does
// NOT depend on a background publisher having flipped Status yet — the
// due-at comparison alone makes it visible.
func TestVisibleAt_ScheduledDue_ServedWithoutWorker(t *testing.T) {
	p := newDraftPage()
	now := time.Now().UTC()
	if err := p.Schedule(now.Add(time.Hour), now); err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	// Status is still "scheduled" here — PublishIfDue was never called.
	later := now.Add(2 * time.Hour)
	if !p.VisibleAt(later) {
		t.Fatal("due scheduled page is invisible without a worker flip; must be visible")
	}
}

func TestVisibleAt_Published_Served(t *testing.T) {
	p := newDraftPage()
	now := time.Now().UTC()
	if err := p.PublishNow(now); err != nil {
		t.Fatalf("PublishNow: %v", err)
	}
	if !p.VisibleAt(now) {
		t.Fatal("published page is not visible")
	}
}

func TestVisibleAt_Draft_NotServed(t *testing.T) {
	p := newDraftPage()
	if p.VisibleAt(time.Now()) {
		t.Fatal("draft page is visible")
	}
}

func TestVisibleAt_Archived_NotServed(t *testing.T) {
	p := newDraftPage()
	now := time.Now().UTC()
	if err := p.PublishNow(now); err != nil {
		t.Fatalf("PublishNow: %v", err)
	}
	if err := p.Archive(now); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if p.VisibleAt(now) {
		t.Fatal("archived page is visible")
	}
}

func TestFilterVisible_MixedStatuses(t *testing.T) {
	now := time.Now().UTC()

	draft := newDraftPage()

	published := newDraftPage()
	if err := published.PublishNow(now); err != nil {
		t.Fatalf("PublishNow: %v", err)
	}

	scheduledNotDue := newDraftPage()
	if err := scheduledNotDue.Schedule(now.Add(time.Hour), now); err != nil {
		t.Fatalf("Schedule: %v", err)
	}

	scheduledDue := newDraftPage()
	if err := scheduledDue.Schedule(now.Add(-time.Hour), now); err != nil {
		t.Fatalf("Schedule: %v", err)
	}

	archived := newDraftPage()
	if err := archived.PublishNow(now); err != nil {
		t.Fatalf("PublishNow: %v", err)
	}
	if err := archived.Archive(now); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	pages := []Page{draft, published, scheduledNotDue, scheduledDue, archived}
	got := FilterVisible(pages, now)

	if len(got) != 2 {
		t.Fatalf("FilterVisible returned %d pages, want 2 (published + due-scheduled): %+v", len(got), got)
	}
	gotIDs := map[uuid.UUID]bool{got[0].ID: true, got[1].ID: true}
	if !gotIDs[published.ID] {
		t.Error("published page missing from FilterVisible result")
	}
	if !gotIDs[scheduledDue.ID] {
		t.Error("due-scheduled page missing from FilterVisible result")
	}
	if gotIDs[draft.ID] || gotIDs[scheduledNotDue.ID] || gotIDs[archived.ID] {
		t.Error("FilterVisible leaked a non-visible page")
	}
}

func tPtr(t time.Time) *time.Time { return &t }
