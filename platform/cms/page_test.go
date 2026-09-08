package cms

import (
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestPageStatus_WireValues locks the exact string values of PageStatus.
// Downstream TRDs in this objective (store.go's persistence, publish.go's
// state machine) depend on these exact strings — a silent rename here would
// break a Postgres column value or a state-machine comparison elsewhere in
// the package without the compiler ever noticing.
func TestPageStatus_WireValues(t *testing.T) {
	cases := map[PageStatus]string{
		PageStatusDraft:     "draft",
		PageStatusScheduled: "scheduled",
		PageStatusPublished: "published",
		PageStatusArchived:  "archived",
	}
	for status, want := range cases {
		if string(status) != want {
			t.Errorf("PageStatus %v = %q, want %q", status, string(status), want)
		}
	}
}

// TestPage_BlocksOrderSurvivesAssignment proves Page is a pure data carrier
// for its Blocks — assigning the output of ParseBlocks onto a Page preserves
// order and content exactly, with no reordering, filtering, or rendering
// applied by Page itself.
func TestPage_BlocksOrderSurvivesAssignment(t *testing.T) {
	extra := map[string]any{
		"blocks": []any{
			map[string]any{"id": "1", "type": "banner", "data": map[string]any{}},
			map[string]any{"id": "2", "type": "markdown", "data": map[string]any{"source": "x"}},
			map[string]any{"id": "3", "type": "banner", "data": map[string]any{}},
		},
	}
	blocks := ParseBlocks(extra)

	p := Page{
		ID:        uuid.New(),
		CompanyID: uuid.New(),
		Slug:      "about",
		Title:     "About",
		Blocks:    blocks,
		Status:    PageStatusDraft,
	}

	want := []Block{
		{ID: "1", Type: "banner", Data: map[string]any{}},
		{ID: "2", Type: "markdown", Data: map[string]any{"source": "x"}},
		{ID: "3", Type: "banner", Data: map[string]any{}},
	}
	if !reflect.DeepEqual(p.Blocks, want) {
		t.Errorf("Page.Blocks = %#v, want %#v", p.Blocks, want)
	}
}

// TestPage_ZeroValueCarriesNoImpliedState documents that a zero-value Page
// has an empty Status (not an implicit PageStatusDraft) and nil Blocks —
// Page does not manufacture defaults; a caller (store.go / publish.go) is
// responsible for that.
func TestPage_ZeroValueCarriesNoImpliedState(t *testing.T) {
	var p Page
	if p.Status != "" {
		t.Errorf("zero-value Page.Status = %q, want empty", p.Status)
	}
	if p.Blocks != nil {
		t.Errorf("zero-value Page.Blocks = %#v, want nil", p.Blocks)
	}
	if p.ScheduledPublishAt != nil {
		t.Errorf("zero-value Page.ScheduledPublishAt = %v, want nil", p.ScheduledPublishAt)
	}
	if p.PublishedAt != nil {
		t.Errorf("zero-value Page.PublishedAt = %v, want nil", p.PublishedAt)
	}
}

// TestPage_ScheduledPublishAtIsAbsoluteUTCCarrier proves Page merely carries
// whatever instant it is given — it neither normalizes nor validates the
// zone. That guard belongs to publish.go; this test only pins down that the
// field is a *time.Time capable of holding an absolute UTC instant intact.
func TestPage_ScheduledPublishAtIsAbsoluteUTCCarrier(t *testing.T) {
	when := time.Date(2027, time.January, 2, 15, 4, 5, 0, time.UTC)
	p := Page{Status: PageStatusScheduled, ScheduledPublishAt: &when}
	if p.ScheduledPublishAt == nil {
		t.Fatal("ScheduledPublishAt is nil, want set")
	}
	if !p.ScheduledPublishAt.Equal(when) {
		t.Errorf("ScheduledPublishAt = %v, want %v", *p.ScheduledPublishAt, when)
	}
	if p.ScheduledPublishAt.Location() != time.UTC {
		t.Errorf("ScheduledPublishAt location = %v, want UTC", p.ScheduledPublishAt.Location())
	}
}

// TestPage_TenantFieldIsUUID pins CompanyID as the tenant key, consistent
// with platform/company.Company.ID and platform/telephony.Config.CompanyID.
func TestPage_TenantFieldIsUUID(t *testing.T) {
	id := uuid.New()
	p := Page{CompanyID: id}
	if p.CompanyID != id {
		t.Errorf("CompanyID = %v, want %v", p.CompanyID, id)
	}
}
