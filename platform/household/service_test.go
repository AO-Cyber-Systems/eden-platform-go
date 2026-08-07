package household

// Behavior checklist (test list) for the re-homed household model (Phase 0).
// Written before the tests below; each item maps to a Test* function.
//
// Households
//   - CreateHousehold sets primary_contact = acting identity and emits audit.
//
// Members / role validation
//   - AddMember rejects an unknown role.
//   - AddMember rejects a child with no birthdate.
//   - AddMember allows a child with a birthdate; child holds no capability.
//
// Capability invariants
//   - A guardian may be manager + account_owner (happy path).
//   - AddMember rejects a child as manager.
//   - AddMember rejects a child as account_owner.
//   - UpdateMemberRole rejects giving a child a capability.
//   - account_owner must be an adult (non-child); an adult may be owner.
//   - Exactly one account_owner: adding a second active owner is rejected.
//
// Read path
//   - GetHouseholdForIdentity returns the identity's household + role/caps.
//
// Wrong-household isolation
//   - GetMemberByIdentity is household-scoped: a lookup in household A never
//     returns household B's member, even for the same identity.
//   - ListMembers(A) never contains a household B member.
//   - SetAccountOwner(A, B's member) is rejected (cross-household).
//
// account_owner transfer
//   - SetAccountOwner demotes the old owner and promotes the new one.
//   - SetAccountOwner rejects a child target.
//
// Removal guards
//   - RemoveMember refuses to remove the account_owner (transfer first).
//   - RemoveMember refuses to remove the last manager.
//   - RemoveMember succeeds for a non-last manager; list excludes removed.
//
// Consent eligibility / parent-of-record
//   - Role.CanGrantConsent: guardian true, adult false, child false.
//   - EstablishParentOfRecord accepts a guardian, rejects a non-guardian adult.

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

// recorder captures audit.Event values; satisfies the auditEmitter interface.
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
	return &Service{store: store, auditor: rec}, store, rec
}

func newAC() AuditContext {
	return AuditContext{CompanyID: uuid.New(), ActorID: uuid.New(), IPAddress: "127.0.0.1"}
}

func childDOB() *time.Time {
	b := time.Date(2018, 6, 1, 0, 0, 0, 0, time.UTC)
	return &b
}

// addGuardianOwner adds a guardian who is manager + account_owner — the
// canonical "first member" a provisioning seam creates.
func addGuardianOwner(t *testing.T, svc *Service, ac AuditContext, hhID, identityID uuid.UUID) Member {
	t.Helper()
	m, err := svc.AddMember(context.Background(), ac, Member{
		HouseholdID: hhID, IdentityID: identityID, Role: RoleGuardian,
		IsManager: true, IsAccountOwner: true,
	})
	if err != nil {
		t.Fatalf("add guardian owner: %v", err)
	}
	return m
}

func TestService_CreateHousehold_EmitsAudit(t *testing.T) {
	svc, _, rec := newServiceWithRecorder(t)
	ac := newAC()

	h, err := svc.CreateHousehold(context.Background(), ac, "Test Family", json.RawMessage(`{"plan":"family"}`))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if h.PrimaryContactIdentityID != ac.ActorID {
		t.Errorf("primary contact = %s, want %s", h.PrimaryContactIdentityID, ac.ActorID)
	}
	events := rec.snapshot()
	if len(events) != 1 || events[0].Action != ActionHouseholdCreated {
		t.Fatalf("events = %+v, want one household.created", events)
	}
	if events[0].ResourceID != h.ID.String() {
		t.Errorf("resource_id = %q, want %s", events[0].ResourceID, h.ID)
	}
}

func TestService_AddMember_RejectsInvalidRole(t *testing.T) {
	svc, _, _ := newServiceWithRecorder(t)
	ac := newAC()
	h, _ := svc.CreateHousehold(context.Background(), ac, "T", nil)
	_, err := svc.AddMember(context.Background(), ac, Member{HouseholdID: h.ID, IdentityID: uuid.New(), Role: Role("nope")})
	if !errors.Is(err, ErrInvalidRole) {
		t.Fatalf("err = %v, want ErrInvalidRole", err)
	}
}

func TestService_AddMember_RejectsChildWithoutBirthdate(t *testing.T) {
	svc, _, _ := newServiceWithRecorder(t)
	ac := newAC()
	h, _ := svc.CreateHousehold(context.Background(), ac, "T", nil)
	_, err := svc.AddMember(context.Background(), ac, Member{HouseholdID: h.ID, IdentityID: uuid.New(), Role: RoleChild})
	if !errors.Is(err, ErrChildBirthdateRequired) {
		t.Fatalf("err = %v, want ErrChildBirthdateRequired", err)
	}
}

func TestService_AddMember_AllowsChildWithBirthdate(t *testing.T) {
	svc, _, _ := newServiceWithRecorder(t)
	ac := newAC()
	h, _ := svc.CreateHousehold(context.Background(), ac, "Fam", nil)
	m, err := svc.AddMember(context.Background(), ac, Member{HouseholdID: h.ID, IdentityID: uuid.New(), Role: RoleChild, Birthdate: childDOB()})
	if err != nil {
		t.Fatalf("add child: %v", err)
	}
	if m.Status != StatusActive {
		t.Errorf("status = %q, want active", m.Status)
	}
	if m.IsManager || m.IsAccountOwner {
		t.Error("child must hold no capability")
	}
}

func TestService_AddMember_GuardianManagerOwner_HappyPath(t *testing.T) {
	svc, _, _ := newServiceWithRecorder(t)
	ac := newAC()
	h, _ := svc.CreateHousehold(context.Background(), ac, "Fam", nil)
	m := addGuardianOwner(t, svc, ac, h.ID, uuid.New())
	if !m.IsManager || !m.IsAccountOwner || m.Role != RoleGuardian {
		t.Errorf("member = %+v, want guardian manager+owner", m)
	}
}

func TestService_AddMember_RejectsChildAsManager(t *testing.T) {
	svc, _, _ := newServiceWithRecorder(t)
	ac := newAC()
	h, _ := svc.CreateHousehold(context.Background(), ac, "Fam", nil)
	_, err := svc.AddMember(context.Background(), ac, Member{HouseholdID: h.ID, IdentityID: uuid.New(), Role: RoleChild, Birthdate: childDOB(), IsManager: true})
	if !errors.Is(err, ErrChildCannotHoldCapability) {
		t.Fatalf("err = %v, want ErrChildCannotHoldCapability", err)
	}
}

func TestService_AddMember_RejectsChildAsAccountOwner(t *testing.T) {
	svc, _, _ := newServiceWithRecorder(t)
	ac := newAC()
	h, _ := svc.CreateHousehold(context.Background(), ac, "Fam", nil)
	_, err := svc.AddMember(context.Background(), ac, Member{HouseholdID: h.ID, IdentityID: uuid.New(), Role: RoleChild, Birthdate: childDOB(), IsAccountOwner: true})
	if !errors.Is(err, ErrChildCannotHoldCapability) {
		t.Fatalf("err = %v, want ErrChildCannotHoldCapability", err)
	}
}

func TestService_UpdateMemberRole_RejectsChildCapability(t *testing.T) {
	svc, _, _ := newServiceWithRecorder(t)
	ac := newAC()
	h, _ := svc.CreateHousehold(context.Background(), ac, "Fam", nil)
	child, _ := svc.AddMember(context.Background(), ac, Member{HouseholdID: h.ID, IdentityID: uuid.New(), Role: RoleChild, Birthdate: childDOB()})
	_, err := svc.UpdateMemberRole(context.Background(), ac, child.ID, RoleChild, true, false, nil)
	if !errors.Is(err, ErrChildCannotHoldCapability) {
		t.Fatalf("err = %v, want ErrChildCannotHoldCapability", err)
	}
}

func TestService_AddMember_RejectsSecondAccountOwner(t *testing.T) {
	svc, _, _ := newServiceWithRecorder(t)
	ac := newAC()
	h, _ := svc.CreateHousehold(context.Background(), ac, "Fam", nil)
	addGuardianOwner(t, svc, ac, h.ID, uuid.New())
	_, err := svc.AddMember(context.Background(), ac, Member{HouseholdID: h.ID, IdentityID: uuid.New(), Role: RoleAdult, IsAccountOwner: true})
	if !errors.Is(err, ErrAccountOwnerExists) {
		t.Fatalf("err = %v, want ErrAccountOwnerExists", err)
	}
}

func TestService_AddMember_AdultCanBeOwner(t *testing.T) {
	svc, _, _ := newServiceWithRecorder(t)
	ac := newAC()
	h, _ := svc.CreateHousehold(context.Background(), ac, "Fam", nil)
	m, err := svc.AddMember(context.Background(), ac, Member{HouseholdID: h.ID, IdentityID: uuid.New(), Role: RoleAdult, IsManager: true, IsAccountOwner: true})
	if err != nil {
		t.Fatalf("adult owner: %v", err)
	}
	if !m.IsAccountOwner {
		t.Error("adult should be account_owner")
	}
}

func TestService_GetHouseholdForIdentity(t *testing.T) {
	svc, _, _ := newServiceWithRecorder(t)
	ac := newAC()
	h, _ := svc.CreateHousehold(context.Background(), ac, "Fam", nil)
	id := uuid.New()
	addGuardianOwner(t, svc, ac, h.ID, id)

	gotHH, gotMember, err := svc.GetHouseholdForIdentity(context.Background(), id)
	if err != nil {
		t.Fatalf("get for identity: %v", err)
	}
	if gotHH.ID != h.ID {
		t.Errorf("household = %s, want %s", gotHH.ID, h.ID)
	}
	if gotMember.IdentityID != id || gotMember.Role != RoleGuardian || !gotMember.IsAccountOwner {
		t.Errorf("member = %+v, want guardian owner for %s", gotMember, id)
	}
}

func TestService_WrongHouseholdIsolation(t *testing.T) {
	svc, store, _ := newServiceWithRecorder(t)
	ac := newAC()
	ctx := context.Background()

	hhA, _ := svc.CreateHousehold(ctx, ac, "A", nil)
	hhB, _ := svc.CreateHousehold(ctx, ac, "B", nil)

	idShared := uuid.New() // same identity is a member of BOTH households
	memberA := addGuardianOwner(t, svc, ac, hhA.ID, idShared)
	memberB := addGuardianOwner(t, svc, ac, hhB.ID, idShared)
	idOnlyB := uuid.New()
	bOnly, _ := svc.AddMember(ctx, ac, Member{HouseholdID: hhB.ID, IdentityID: idOnlyB, Role: RoleAdult, IsManager: true})

	// GetMemberByIdentity is household-scoped: A's lookup returns A's row.
	gotA, err := store.GetMemberByIdentity(ctx, hhA.ID, idShared)
	if err != nil {
		t.Fatalf("member by identity A: %v", err)
	}
	if gotA.ID != memberA.ID || gotA.HouseholdID != hhA.ID {
		t.Errorf("scoped lookup returned %s (hh %s), want A's member %s", gotA.ID, gotA.HouseholdID, memberA.ID)
	}
	gotB, _ := store.GetMemberByIdentity(ctx, hhB.ID, idShared)
	if gotB.ID != memberB.ID {
		t.Errorf("scoped lookup B returned %s, want %s", gotB.ID, memberB.ID)
	}

	// An identity that only belongs to B is invisible from A.
	if _, err := store.GetMemberByIdentity(ctx, hhA.ID, idOnlyB); !errors.Is(err, ErrNotFound) {
		t.Errorf("A lookup of B-only identity err = %v, want ErrNotFound", err)
	}

	// ListMembers(A) never contains a B member.
	membersA, _ := svc.ListMembers(ctx, hhA.ID)
	for _, m := range membersA {
		if m.HouseholdID != hhA.ID {
			t.Errorf("ListMembers(A) leaked household %s", m.HouseholdID)
		}
		if m.ID == bOnly.ID || m.ID == memberB.ID {
			t.Errorf("ListMembers(A) leaked B member %s", m.ID)
		}
	}

	// SetAccountOwner(A, B's member) must not cross households.
	if _, err := svc.SetAccountOwner(ctx, ac, hhA.ID, memberB.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-household SetAccountOwner err = %v, want ErrNotFound", err)
	}
	// B's owner is unchanged.
	stillB, _ := store.GetMember(ctx, memberB.ID)
	if !stillB.IsAccountOwner {
		t.Error("cross-household transfer wrongly demoted B's owner")
	}
}

func TestService_SetAccountOwner_Transfers(t *testing.T) {
	svc, store, _ := newServiceWithRecorder(t)
	ac := newAC()
	ctx := context.Background()
	h, _ := svc.CreateHousehold(ctx, ac, "Fam", nil)
	owner := addGuardianOwner(t, svc, ac, h.ID, uuid.New())
	other, _ := svc.AddMember(ctx, ac, Member{HouseholdID: h.ID, IdentityID: uuid.New(), Role: RoleAdult, IsManager: true})

	newOwner, err := svc.SetAccountOwner(ctx, ac, h.ID, other.ID)
	if err != nil {
		t.Fatalf("transfer: %v", err)
	}
	if !newOwner.IsAccountOwner {
		t.Error("new owner not marked account_owner")
	}
	demoted, _ := store.GetMember(ctx, owner.ID)
	if demoted.IsAccountOwner {
		t.Error("old owner still account_owner after transfer")
	}
}

func TestService_SetAccountOwner_RejectsChild(t *testing.T) {
	svc, _, _ := newServiceWithRecorder(t)
	ac := newAC()
	ctx := context.Background()
	h, _ := svc.CreateHousehold(ctx, ac, "Fam", nil)
	addGuardianOwner(t, svc, ac, h.ID, uuid.New())
	child, _ := svc.AddMember(ctx, ac, Member{HouseholdID: h.ID, IdentityID: uuid.New(), Role: RoleChild, Birthdate: childDOB()})
	if _, err := svc.SetAccountOwner(ctx, ac, h.ID, child.ID); !errors.Is(err, ErrAccountOwnerMustBeAdult) {
		t.Fatalf("err = %v, want ErrAccountOwnerMustBeAdult", err)
	}
}

func TestService_RemoveMember_RefusesAccountOwner(t *testing.T) {
	svc, _, _ := newServiceWithRecorder(t)
	ac := newAC()
	ctx := context.Background()
	h, _ := svc.CreateHousehold(ctx, ac, "Fam", nil)
	owner := addGuardianOwner(t, svc, ac, h.ID, uuid.New())
	if err := svc.RemoveMember(ctx, ac, owner.ID); !errors.Is(err, ErrCannotRemoveAccountOwner) {
		t.Fatalf("err = %v, want ErrCannotRemoveAccountOwner", err)
	}
}

func TestService_RemoveMember_RefusesLastManager(t *testing.T) {
	svc, _, _ := newServiceWithRecorder(t)
	ac := newAC()
	ctx := context.Background()
	h, _ := svc.CreateHousehold(ctx, ac, "Fam", nil)
	// Sole manager is also the owner; make a second adult manager, then a
	// manager-only member to test the last-manager guard cleanly.
	addGuardianOwner(t, svc, ac, h.ID, uuid.New())
	mgr, _ := svc.AddMember(ctx, ac, Member{HouseholdID: h.ID, IdentityID: uuid.New(), Role: RoleAdult, IsManager: true})

	// Two managers now (owner + mgr): removing mgr is allowed.
	if err := svc.RemoveMember(ctx, ac, mgr.ID); err != nil {
		t.Fatalf("remove non-last manager: %v", err)
	}
	members, _ := svc.ListMembers(ctx, h.ID)
	for _, m := range members {
		if m.ID == mgr.ID {
			t.Error("removed manager still listed")
		}
	}

	// Only the owner-manager remains; it can't be removed (it's the account
	// owner AND the last manager — the owner guard fires first).
	owner := members[0]
	if err := svc.RemoveMember(ctx, ac, owner.ID); err == nil {
		t.Error("expected removal of the sole owner/manager to fail")
	}
}

func TestService_RemoveMember_LastManagerGuard_NonOwner(t *testing.T) {
	svc, _, _ := newServiceWithRecorder(t)
	ac := newAC()
	ctx := context.Background()
	h, _ := svc.CreateHousehold(ctx, ac, "Fam", nil)
	// Owner is an adult but NOT a manager; a separate adult is the sole manager.
	if _, err := svc.AddMember(ctx, ac, Member{HouseholdID: h.ID, IdentityID: uuid.New(), Role: RoleAdult, IsAccountOwner: true}); err != nil {
		t.Fatalf("add owner: %v", err)
	}
	mgr, _ := svc.AddMember(ctx, ac, Member{HouseholdID: h.ID, IdentityID: uuid.New(), Role: RoleGuardian, IsManager: true})
	if err := svc.RemoveMember(ctx, ac, mgr.ID); !errors.Is(err, ErrLastManager) {
		t.Fatalf("err = %v, want ErrLastManager", err)
	}
}

func TestRole_CanGrantConsent(t *testing.T) {
	cases := []struct {
		role Role
		want bool
	}{
		{RoleGuardian, true},
		{RoleAdult, false},
		{RoleChild, false},
	}
	for _, c := range cases {
		if got := c.role.CanGrantConsent(); got != c.want {
			t.Errorf("%s.CanGrantConsent() = %v, want %v", c.role, got, c.want)
		}
	}
}

func TestService_EstablishParentOfRecord(t *testing.T) {
	svc, _, _ := newServiceWithRecorder(t)
	ac := newAC()
	ctx := context.Background()
	h, _ := svc.CreateHousehold(ctx, ac, "Fam", nil)
	guardian := addGuardianOwner(t, svc, ac, h.ID, uuid.New())
	adult, _ := svc.AddMember(ctx, ac, Member{HouseholdID: h.ID, IdentityID: uuid.New(), Role: RoleAdult})
	child, _ := svc.AddMember(ctx, ac, Member{HouseholdID: h.ID, IdentityID: uuid.New(), Role: RoleChild, Birthdate: childDOB()})

	// A non-guardian adult is not eligible.
	if _, err := svc.EstablishParentOfRecord(ctx, ac, child.ID, adult.ID); !errors.Is(err, ErrParentNotEligible) {
		t.Fatalf("adult POR err = %v, want ErrParentNotEligible", err)
	}
	// A guardian is eligible.
	por, err := svc.EstablishParentOfRecord(ctx, ac, child.ID, guardian.ID)
	if err != nil {
		t.Fatalf("guardian POR: %v", err)
	}
	parents, _ := svc.ListParentsOfRecord(ctx, child.ID)
	if len(parents) != 1 || parents[0].ID != por.ID {
		t.Errorf("parents = %+v, want the one POR %s", parents, por.ID)
	}
}
