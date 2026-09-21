package household

// Behavior checklist (test list) for the re-homed household model (Phase 0).
// Written before the tests below; each item maps to a Test* function.
//
// Households
//   - CreateHousehold sets primary_contact = acting identity and emits audit.
//
// Atomic provisioning seam (Phase 0.4)
//   - CreateHouseholdWithOwner (guardian) persists the household + exactly one
//     member who is guardian + manager + account_owner; primary_contact is the
//     owner's identity; GetHouseholdForIdentity(owner) returns it; a
//     household.created and a member_added audit row carry the identity actor.
//   - CreateHouseholdWithOwner (adult) is allowed.
//   - CreateHouseholdWithOwner (child) is rejected; nothing is persisted and no
//     audit is emitted.
//   - Atomicity: when the store's atomic insert fails, the service persists no
//     household (it never creates one independently of the atomic call).
//   - Resulting invariants: exactly one account_owner, >=1 manager.
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

// failOwnerInsertStore wraps memStore but makes the atomic
// CreateHouseholdWithOwner fail as if the member insert (the second statement in
// the transaction) errored — persisting NOTHING. It proves the service
// delegates to a single atomic store call rather than creating the household
// independently (which would leave an orphan household on member failure).
type failOwnerInsertStore struct {
	*memStore
	called bool
}

func (s *failOwnerInsertStore) CreateHouseholdWithOwner(context.Context, Household, Member) (Household, Member, error) {
	s.called = true
	return Household{}, Member{}, errors.New("simulated owner insert failure")
}

func TestService_CreateHouseholdWithOwner_HappyPath(t *testing.T) {
	svc, store, rec := newServiceWithRecorder(t)
	ac := newAC()
	ctx := context.Background()
	ownerID := uuid.New()

	hh, owner, err := svc.CreateHouseholdWithOwner(ctx, ac, "Provisioned Fam",
		OwnerInput{IdentityID: ownerID, Role: RoleGuardian}, json.RawMessage(`{"plan":"family"}`))
	if err != nil {
		t.Fatalf("create with owner: %v", err)
	}
	if hh.PrimaryContactIdentityID != ownerID {
		t.Errorf("primary contact = %s, want owner identity %s", hh.PrimaryContactIdentityID, ownerID)
	}
	if owner.Role != RoleGuardian || !owner.IsManager || !owner.IsAccountOwner || owner.Status != StatusActive {
		t.Errorf("owner = %+v, want active guardian manager+owner", owner)
	}

	// Exactly one member, and it's the owner.
	members, _ := svc.ListMembers(ctx, hh.ID)
	if len(members) != 1 || members[0].ID != owner.ID {
		t.Fatalf("members = %+v, want exactly the owner", members)
	}
	// Invariants: exactly one account_owner, >=1 manager.
	owners := 0
	for _, m := range members {
		if m.IsAccountOwner {
			owners++
		}
	}
	if owners != 1 {
		t.Errorf("account_owners = %d, want exactly 1", owners)
	}
	if n, _ := store.CountManagers(ctx, hh.ID); n < 1 {
		t.Errorf("managers = %d, want >= 1", n)
	}

	// Read path returns the household + the owner's membership.
	gotHH, gotMember, err := svc.GetHouseholdForIdentity(ctx, ownerID)
	if err != nil {
		t.Fatalf("get for identity: %v", err)
	}
	if gotHH.ID != hh.ID || gotMember.ID != owner.ID {
		t.Errorf("for-identity = (%s,%s), want (%s,%s)", gotHH.ID, gotMember.ID, hh.ID, owner.ID)
	}

	// Audit: a household.created and a member_added row, both carrying the
	// identity actor from the AuditContext.
	var created, added bool
	for _, e := range rec.snapshot() {
		if e.ActorID != ac.ActorID.String() {
			t.Errorf("audit actor = %q, want identity %q", e.ActorID, ac.ActorID)
		}
		switch e.Action {
		case ActionHouseholdCreated:
			created = true
		case ActionMemberAdded:
			added = true
		}
	}
	if !created || !added {
		t.Errorf("audit events = %+v, want household.created + member_added", rec.snapshot())
	}
}

func TestService_CreateHouseholdWithOwner_AdultAllowed(t *testing.T) {
	svc, _, _ := newServiceWithRecorder(t)
	ac := newAC()
	_, owner, err := svc.CreateHouseholdWithOwner(context.Background(), ac, "Adult Fam",
		OwnerInput{IdentityID: uuid.New(), Role: RoleAdult}, nil)
	if err != nil {
		t.Fatalf("adult owner: %v", err)
	}
	if owner.Role != RoleAdult || !owner.IsAccountOwner || !owner.IsManager {
		t.Errorf("owner = %+v, want adult manager+account_owner", owner)
	}
}

func TestService_CreateHouseholdWithOwner_RejectsChild(t *testing.T) {
	svc, store, rec := newServiceWithRecorder(t)
	ac := newAC()
	ctx := context.Background()
	ownerID := uuid.New()

	_, _, err := svc.CreateHouseholdWithOwner(ctx, ac, "Kid",
		OwnerInput{IdentityID: ownerID, Role: RoleChild, Birthdate: childDOB()}, nil)
	if !errors.Is(err, ErrChildCannotHoldCapability) {
		t.Fatalf("err = %v, want ErrChildCannotHoldCapability", err)
	}
	// Nothing persisted, nothing emitted.
	if _, _, err := svc.GetHouseholdForIdentity(ctx, ownerID); !errors.Is(err, ErrNotFound) {
		t.Errorf("household persisted for rejected child owner: err = %v, want ErrNotFound", err)
	}
	if len(store.households) != 0 {
		t.Errorf("household rows = %d, want 0", len(store.households))
	}
	if evs := rec.snapshot(); len(evs) != 0 {
		t.Errorf("audit events for rejected create = %+v, want none", evs)
	}
}

func TestService_CreateHouseholdWithOwner_AtomicRollback(t *testing.T) {
	base := newMemStore()
	store := &failOwnerInsertStore{memStore: base}
	svc := &Service{store: store, auditor: &recorder{}}
	ac := newAC()

	_, _, err := svc.CreateHouseholdWithOwner(context.Background(), ac, "Rollback",
		OwnerInput{IdentityID: uuid.New(), Role: RoleGuardian}, nil)
	if err == nil {
		t.Fatal("expected error from failing owner insert")
	}
	if !store.called {
		t.Error("service did not delegate to the atomic store method")
	}
	// The service must NOT have created a household independently of the atomic
	// call — no orphan household or member is persisted.
	if len(base.households) != 0 {
		t.Errorf("orphan household rows = %d, want 0 (service must create atomically)", len(base.households))
	}
	if len(base.members) != 0 {
		t.Errorf("orphan member rows = %d, want 0", len(base.members))
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

// ---- UpdateMemberRole must enforce the same lower-bound invariants as
// RemoveMember (review finding on #56): demoting the sole owner / last manager
// through the role path left the household with no owner and no manager.

func TestService_UpdateMemberRole_RefusesDemotingAccountOwner(t *testing.T) {
	svc, _, _ := newServiceWithRecorder(t)
	ac := newAC()
	ctx := context.Background()
	h, _ := svc.CreateHousehold(ctx, ac, "Fam", nil)
	owner := addGuardianOwner(t, svc, ac, h.ID, uuid.New())

	_, err := svc.UpdateMemberRole(ctx, ac, owner.ID, RoleGuardian, true, false, nil)
	if !errors.Is(err, ErrCannotRemoveAccountOwner) {
		t.Fatalf("err = %v, want ErrCannotRemoveAccountOwner (owner must be transferred via SetAccountOwner)", err)
	}
	got, _ := svc.store.GetMember(ctx, owner.ID)
	if !got.IsAccountOwner {
		t.Fatal("account_owner capability was stripped despite the error")
	}
}

func TestService_UpdateMemberRole_RefusesDemotingLastManager(t *testing.T) {
	svc, _, _ := newServiceWithRecorder(t)
	ac := newAC()
	ctx := context.Background()
	h, _ := svc.CreateHousehold(ctx, ac, "Fam", nil)
	// Owner is NOT a manager; a separate adult is the sole manager.
	if _, err := svc.AddMember(ctx, ac, Member{HouseholdID: h.ID, IdentityID: uuid.New(), Role: RoleAdult, IsAccountOwner: true}); err != nil {
		t.Fatalf("add owner: %v", err)
	}
	mgr, _ := svc.AddMember(ctx, ac, Member{HouseholdID: h.ID, IdentityID: uuid.New(), Role: RoleGuardian, IsManager: true})

	_, err := svc.UpdateMemberRole(ctx, ac, mgr.ID, RoleGuardian, false, false, nil)
	if !errors.Is(err, ErrLastManager) {
		t.Fatalf("err = %v, want ErrLastManager", err)
	}
	if n, _ := svc.store.CountManagers(ctx, h.ID); n != 1 {
		t.Fatalf("managers = %d after refused demotion, want 1", n)
	}

	// With a second manager present the demotion is allowed.
	if _, err := svc.AddMember(ctx, ac, Member{HouseholdID: h.ID, IdentityID: uuid.New(), Role: RoleAdult, IsManager: true}); err != nil {
		t.Fatalf("add second manager: %v", err)
	}
	if _, err := svc.UpdateMemberRole(ctx, ac, mgr.ID, RoleGuardian, false, false, nil); err != nil {
		t.Fatalf("demote non-last manager: %v", err)
	}
}

func TestService_UpdateMemberRole_RefusesChildWithoutBirthdate(t *testing.T) {
	svc, _, _ := newServiceWithRecorder(t)
	ac := newAC()
	ctx := context.Background()
	h, _ := svc.CreateHousehold(ctx, ac, "Fam", nil)
	addGuardianOwner(t, svc, ac, h.ID, uuid.New())
	adult, _ := svc.AddMember(ctx, ac, Member{HouseholdID: h.ID, IdentityID: uuid.New(), Role: RoleAdult})

	_, err := svc.UpdateMemberRole(ctx, ac, adult.ID, RoleChild, false, false, nil)
	if !errors.Is(err, ErrChildBirthdateRequired) {
		t.Fatalf("err = %v, want ErrChildBirthdateRequired (COPPA age test needs a birthdate)", err)
	}
}

func TestService_UpdateMemberRole_RemovedMemberIsNotFound(t *testing.T) {
	svc, _, _ := newServiceWithRecorder(t)
	ac := newAC()
	ctx := context.Background()
	h, _ := svc.CreateHousehold(ctx, ac, "Fam", nil)
	addGuardianOwner(t, svc, ac, h.ID, uuid.New())
	adult, _ := svc.AddMember(ctx, ac, Member{HouseholdID: h.ID, IdentityID: uuid.New(), Role: RoleAdult})
	if err := svc.RemoveMember(ctx, ac, adult.ID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := svc.UpdateMemberRole(ctx, ac, adult.ID, RoleGuardian, true, false, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound for a removed member", err)
	}
}
