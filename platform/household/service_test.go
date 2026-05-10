package household

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/aocybersystems/eden-platform-go/platform/audit"
	"github.com/google/uuid"
)

// recorder captures audit.Event values; satisfies the auditEmitter
// interface inside the household package.
type recorder struct {
	mu     sync.Mutex
	events []audit.Event
}

func (r *recorder) Log(e audit.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *recorder) snapshot() []audit.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]audit.Event, len(r.events))
	copy(out, r.events)
	return out
}

func newServiceWithRecorder(t *testing.T) (*Service, *memStore, *recorder) {
	t.Helper()
	store := newMemStore()
	rec := &recorder{}
	svc := &Service{store: store, auditor: rec}
	return svc, store, rec
}

func newAC() AuditContext {
	return AuditContext{
		CompanyID: uuid.New(),
		ActorID:   uuid.New(),
		IPAddress: "127.0.0.1",
	}
}

func TestService_CreateHousehold_EmitsAudit(t *testing.T) {
	svc, _, rec := newServiceWithRecorder(t)
	ctx := context.Background()
	ac := newAC()

	h, err := svc.CreateHousehold(ctx, ac, "Test Family", json.RawMessage(`{"plan":"family"}`))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if h.PrimaryContactUserID != ac.ActorID {
		t.Errorf("primary contact = %s, want %s", h.PrimaryContactUserID, ac.ActorID)
	}
	if h.DisplayName != "Test Family" {
		t.Errorf("display_name = %q", h.DisplayName)
	}

	events := rec.snapshot()
	if len(events) != 1 {
		t.Fatalf("event count = %d, want 1", len(events))
	}
	if events[0].Action != ActionHouseholdCreated {
		t.Errorf("action = %q, want %q", events[0].Action, ActionHouseholdCreated)
	}
	if events[0].Resource != "household" {
		t.Errorf("resource = %q", events[0].Resource)
	}
	if events[0].ResourceID != h.ID.String() {
		t.Errorf("resource_id = %q, want %s", events[0].ResourceID, h.ID)
	}
}

func TestService_AddMember_RejectsChildWithoutBirthdate(t *testing.T) {
	svc, _, _ := newServiceWithRecorder(t)
	ctx := context.Background()
	ac := newAC()

	h, _ := svc.CreateHousehold(ctx, ac, "Test", nil)
	_, err := svc.AddMember(ctx, ac, Member{
		HouseholdID: h.ID,
		UserID:      uuid.New(),
		Role:        RoleChild,
	})
	if !errors.Is(err, ErrChildBirthdateRequired) {
		t.Fatalf("err = %v, want ErrChildBirthdateRequired", err)
	}
}

func TestService_AddMember_RejectsInvalidRole(t *testing.T) {
	svc, _, _ := newServiceWithRecorder(t)
	ctx := context.Background()
	ac := newAC()

	h, _ := svc.CreateHousehold(ctx, ac, "Test", nil)
	_, err := svc.AddMember(ctx, ac, Member{
		HouseholdID: h.ID,
		UserID:      uuid.New(),
		Role:        Role("not-a-role"),
	})
	if !errors.Is(err, ErrInvalidRole) {
		t.Fatalf("err = %v, want ErrInvalidRole", err)
	}
}

func TestService_AddMember_AllowsChildWithBirthdate(t *testing.T) {
	svc, _, rec := newServiceWithRecorder(t)
	ctx := context.Background()
	ac := newAC()

	h, _ := svc.CreateHousehold(ctx, ac, "Family", nil)
	bday := time.Date(2018, 6, 1, 0, 0, 0, 0, time.UTC)
	m, err := svc.AddMember(ctx, ac, Member{
		HouseholdID: h.ID,
		UserID:      uuid.New(),
		Role:        RoleChild,
		Birthdate:   &bday,
	})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if m.Status != StatusActive {
		t.Errorf("status default = %q, want active", m.Status)
	}

	events := rec.snapshot()
	if len(events) != 2 {
		t.Fatalf("event count = %d, want 2", len(events))
	}
	if events[1].Action != ActionMemberAdded {
		t.Errorf("event[1].Action = %q, want %q", events[1].Action, ActionMemberAdded)
	}
}

func TestService_EstablishParentOfRecord_RejectsNonParent(t *testing.T) {
	svc, _, _ := newServiceWithRecorder(t)
	ctx := context.Background()
	ac := newAC()

	h, _ := svc.CreateHousehold(ctx, ac, "Family", nil)
	bday := time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC)

	child, _ := svc.AddMember(ctx, ac, Member{
		HouseholdID: h.ID, UserID: uuid.New(), Role: RoleChild, Birthdate: &bday,
	})
	otherAdult, _ := svc.AddMember(ctx, ac, Member{
		HouseholdID: h.ID, UserID: uuid.New(), Role: RoleAdultNonParent,
	})

	_, err := svc.EstablishParentOfRecord(ctx, ac, child.ID, otherAdult.ID)
	if !errors.Is(err, ErrParentNotEligible) {
		t.Fatalf("err = %v, want ErrParentNotEligible", err)
	}
}

func TestService_EstablishParentOfRecord_HappyPath(t *testing.T) {
	svc, _, rec := newServiceWithRecorder(t)
	ctx := context.Background()
	ac := newAC()

	h, _ := svc.CreateHousehold(ctx, ac, "Family", nil)
	bday := time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC)

	parent, _ := svc.AddMember(ctx, ac, Member{
		HouseholdID: h.ID, UserID: uuid.New(), Role: RoleParentOfRecord,
		Capabilities: DefaultCapabilities(RoleParentOfRecord),
	})
	child, _ := svc.AddMember(ctx, ac, Member{
		HouseholdID: h.ID, UserID: uuid.New(), Role: RoleChild, Birthdate: &bday,
	})

	por, err := svc.EstablishParentOfRecord(ctx, ac, child.ID, parent.ID)
	if err != nil {
		t.Fatalf("establish: %v", err)
	}
	if por.ChildMemberID != child.ID || por.ParentMemberID != parent.ID {
		t.Errorf("por mismatch: %+v", por)
	}

	parents, err := svc.ListParentsOfRecord(ctx, child.ID)
	if err != nil {
		t.Fatalf("list parents: %v", err)
	}
	if len(parents) != 1 {
		t.Errorf("parent count = %d, want 1", len(parents))
	}

	// audit: create + 2 add_member + 1 establish_por = 4
	events := rec.snapshot()
	if len(events) != 4 {
		t.Fatalf("event count = %d, want 4", len(events))
	}
	if events[3].Action != ActionParentOfRecordEstablished {
		t.Errorf("last event = %q", events[3].Action)
	}
}

func TestService_RemoveMember_AndList(t *testing.T) {
	svc, _, _ := newServiceWithRecorder(t)
	ctx := context.Background()
	ac := newAC()

	h, _ := svc.CreateHousehold(ctx, ac, "Family", nil)
	parent, _ := svc.AddMember(ctx, ac, Member{
		HouseholdID: h.ID, UserID: uuid.New(), Role: RoleParentOfRecord,
	})
	bday := time.Date(2017, 1, 1, 0, 0, 0, 0, time.UTC)
	child, _ := svc.AddMember(ctx, ac, Member{
		HouseholdID: h.ID, UserID: uuid.New(), Role: RoleChild, Birthdate: &bday,
	})

	// 2 active members
	members, _ := svc.ListMembers(ctx, h.ID)
	if len(members) != 2 {
		t.Fatalf("active count = %d, want 2", len(members))
	}

	// remove child
	if err := svc.RemoveMember(ctx, ac, child.ID); err != nil {
		t.Fatalf("remove: %v", err)
	}

	members, _ = svc.ListMembers(ctx, h.ID)
	if len(members) != 1 {
		t.Errorf("after remove = %d, want 1", len(members))
	}
	if members[0].ID != parent.ID {
		t.Errorf("remaining member = %s, want parent %s", members[0].ID, parent.ID)
	}
}

func TestService_ListHouseholdsForUser(t *testing.T) {
	svc, _, _ := newServiceWithRecorder(t)
	ctx := context.Background()

	user := uuid.New()
	ac := AuditContext{CompanyID: uuid.New(), ActorID: user, IPAddress: "::1"}

	h1, _ := svc.CreateHousehold(ctx, ac, "Primary", nil)
	h2, _ := svc.CreateHousehold(ctx, ac, "Secondary", nil)
	// CreateHousehold doesn't auto-add the contact as a member; emulate it.
	if _, err := svc.AddMember(ctx, ac, Member{
		HouseholdID: h1.ID, UserID: user, Role: RoleParentOfRecord,
	}); err != nil {
		t.Fatalf("add member h1: %v", err)
	}
	if _, err := svc.AddMember(ctx, ac, Member{
		HouseholdID: h2.ID, UserID: user, Role: RoleAdultNonParent,
	}); err != nil {
		t.Fatalf("add member h2: %v", err)
	}

	got, err := svc.ListHouseholdsForUser(ctx, user)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("count = %d, want 2", len(got))
	}
}

func TestRole_CanGrantConsent(t *testing.T) {
	cases := []struct {
		role Role
		want bool
	}{
		{RoleParentOfRecord, true},
		{RoleGuardian, true},
		{RoleAdultNonParent, false},
		{RoleChild, false},
		{RoleOther, false},
	}
	for _, c := range cases {
		if got := c.role.CanGrantConsent(); got != c.want {
			t.Errorf("%s.CanGrantConsent() = %v, want %v", c.role, got, c.want)
		}
	}
}

func TestDefaultCapabilities(t *testing.T) {
	if !DefaultCapabilities(RoleParentOfRecord).CanManageBilling {
		t.Error("parent should manage billing")
	}
	if DefaultCapabilities(RoleChild).CanGrantConsent {
		t.Error("child must not grant consent")
	}
	if DefaultCapabilities(RoleGuardian).CanManageBilling {
		t.Error("guardian default should not manage billing")
	}
}
