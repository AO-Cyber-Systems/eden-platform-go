package pgstore_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/aocybersystems/eden-platform-go/platform/audit"
	"github.com/aocybersystems/eden-platform-go/platform/company"
	"github.com/aocybersystems/eden-platform-go/platform/household"
	"github.com/google/uuid"
)

func TestHouseholdStore_CreateAndQuery(t *testing.T) {
	backend := setupTestBackend(t)
	hhStore := backend.HouseholdStore()
	ctx := context.Background()

	// identity_id is a logical AOID reference (no FK); a fresh UUID is valid.
	primary := uuid.New()
	h, err := hhStore.CreateHousehold(ctx, household.Household{
		PrimaryContactIdentityID: primary,
		DisplayName:              "Smith Family",
		Metadata:                 json.RawMessage(`{"plan":"family"}`),
	})
	if err != nil {
		t.Fatalf("create household: %v", err)
	}
	if h.ID == uuid.Nil {
		t.Error("household ID is nil")
	}

	got, err := hhStore.GetHouseholdByID(ctx, h.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.PrimaryContactIdentityID != primary {
		t.Errorf("primary contact = %s, want %s", got.PrimaryContactIdentityID, primary)
	}

	members, err := hhStore.ListMembers(ctx, h.ID)
	if err != nil {
		t.Fatalf("list members: %v", err)
	}
	if len(members) != 0 {
		t.Errorf("members = %d, want 0", len(members))
	}
}

func TestHouseholdStore_MemberLifecycle(t *testing.T) {
	backend := setupTestBackend(t)
	hhStore := backend.HouseholdStore()
	ctx := context.Background()

	guardianID := uuid.New()
	childID := uuid.New()
	h, _ := hhStore.CreateHousehold(ctx, household.Household{PrimaryContactIdentityID: guardianID, DisplayName: "Lifecycle"})

	guardian, err := hhStore.AddMember(ctx, household.Member{
		HouseholdID: h.ID, IdentityID: guardianID, Role: household.RoleGuardian,
		Status: household.StatusActive, IsManager: true, IsAccountOwner: true,
	})
	if err != nil {
		t.Fatalf("add guardian: %v", err)
	}
	if !guardian.IsManager || !guardian.IsAccountOwner {
		t.Error("guardian capabilities not persisted")
	}

	bday := time.Date(2018, 3, 14, 0, 0, 0, 0, time.UTC)
	child, err := hhStore.AddMember(ctx, household.Member{
		HouseholdID: h.ID, IdentityID: childID, Role: household.RoleChild,
		Status: household.StatusActive, Birthdate: &bday,
	})
	if err != nil {
		t.Fatalf("add child: %v", err)
	}
	if child.Birthdate == nil || !child.Birthdate.Equal(bday) {
		t.Errorf("birthdate = %v, want %v", child.Birthdate, bday)
	}

	members, _ := hhStore.ListMembers(ctx, h.ID)
	if len(members) != 2 {
		t.Errorf("members = %d, want 2", len(members))
	}

	// GetHouseholdForIdentity returns household + the identity's membership.
	gotHH, gotMember, err := hhStore.GetHouseholdForIdentity(ctx, guardianID)
	if err != nil {
		t.Fatalf("get for identity: %v", err)
	}
	if gotHH.ID != h.ID || gotMember.ID != guardian.ID {
		t.Errorf("for-identity = (%s,%s), want (%s,%s)", gotHH.ID, gotMember.ID, h.ID, guardian.ID)
	}

	if err := hhStore.RemoveMember(ctx, child.ID); err != nil {
		t.Fatalf("remove member: %v", err)
	}
	members, _ = hhStore.ListMembers(ctx, h.ID)
	if len(members) != 1 {
		t.Errorf("after remove = %d, want 1", len(members))
	}
	if _, _, err := hhStore.GetHouseholdForIdentity(ctx, childID); !errors.Is(err, household.ErrNotFound) {
		t.Errorf("removed child for-identity err = %v, want ErrNotFound", err)
	}
}

// TestHouseholdStore_CreateWithOwner asserts the atomic provisioning seam
// persists a household AND its first member (guardian + manager + account_owner)
// so the existence lower-bound holds from creation.
func TestHouseholdStore_CreateWithOwner(t *testing.T) {
	backend := setupTestBackend(t)
	hhStore := backend.HouseholdStore()
	ctx := context.Background()

	ownerID := uuid.New()
	hh, owner, err := hhStore.CreateHouseholdWithOwner(ctx,
		household.Household{PrimaryContactIdentityID: ownerID, DisplayName: "Seeded"},
		household.Member{IdentityID: ownerID, Role: household.RoleGuardian, IsManager: true, IsAccountOwner: true, Status: household.StatusActive})
	if err != nil {
		t.Fatalf("create with owner: %v", err)
	}
	if hh.PrimaryContactIdentityID != ownerID {
		t.Errorf("primary contact = %s, want %s", hh.PrimaryContactIdentityID, ownerID)
	}
	if owner.HouseholdID != hh.ID || !owner.IsManager || !owner.IsAccountOwner || owner.Role != household.RoleGuardian {
		t.Errorf("owner = %+v, want guardian manager+owner of %s", owner, hh.ID)
	}

	members, _ := hhStore.ListMembers(ctx, hh.ID)
	if len(members) != 1 {
		t.Errorf("members = %d, want exactly 1", len(members))
	}
	gotHH, gotMember, err := hhStore.GetHouseholdForIdentity(ctx, ownerID)
	if err != nil {
		t.Fatalf("for identity: %v", err)
	}
	if gotHH.ID != hh.ID || gotMember.ID != owner.ID {
		t.Errorf("for-identity = (%s,%s), want (%s,%s)", gotHH.ID, gotMember.ID, hh.ID, owner.ID)
	}
	if n, _ := hhStore.CountManagers(ctx, hh.ID); n != 1 {
		t.Errorf("managers = %d, want 1", n)
	}
}

// TestHouseholdStore_CreateWithOwner_AtomicRollback forces the member insert to
// fail (an invalid role violates chk_household_member_role) AFTER the household
// insert in the same transaction; the whole tx must roll back, leaving zero
// household rows — proving no orphan household is persisted.
func TestHouseholdStore_CreateWithOwner_AtomicRollback(t *testing.T) {
	backend := setupTestBackend(t)
	hhStore := backend.HouseholdStore()
	ctx := context.Background()

	ownerID := uuid.New()
	_, _, err := hhStore.CreateHouseholdWithOwner(ctx,
		household.Household{PrimaryContactIdentityID: ownerID, DisplayName: "ShouldRollBack"},
		household.Member{IdentityID: ownerID, Role: household.Role("bogus"), IsManager: true, IsAccountOwner: true, Status: household.StatusActive})
	if err == nil {
		t.Fatal("expected member insert to fail on the role CHECK constraint")
	}
	var count int
	if err := backend.Pool().QueryRow(ctx, "SELECT count(*) FROM platform_households").Scan(&count); err != nil {
		t.Fatalf("count households: %v", err)
	}
	if count != 0 {
		t.Errorf("household rows = %d, want 0 — tx did not roll back", count)
	}
}

// TestHouseholdService_CreateWithOwner_AuditEmitted proves the provisioning seam
// writes household.created + member_added audit rows carrying the acting AOID
// identity (not a platform.users row), through the real logger + DB.
func TestHouseholdService_CreateWithOwner_AuditEmitted(t *testing.T) {
	backend := setupTestBackend(t)
	companyStore := backend.CompanyStore()
	hhStore := backend.HouseholdStore()
	auditStore := backend.AuditStore()
	ctx := context.Background()

	actorIdentityID := uuid.New()
	ownerID := uuid.New()
	co, _ := companyStore.CreateCompany(ctx, company.Company{Name: "Prov Fam", Slug: "prov-fam"})

	logger := audit.NewLogger(auditStore)
	logger.Start()
	svc := household.NewService(hhStore, logger)
	ac := household.AuditContext{CompanyID: co.ID, ActorID: actorIdentityID, IPAddress: "10.0.0.2"}

	hh, owner, err := svc.CreateHouseholdWithOwner(ctx, ac, "Provisioned",
		household.OwnerInput{IdentityID: ownerID, Role: household.RoleGuardian}, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("create with owner: %v", err)
	}
	logger.Stop()

	if owner.HouseholdID != hh.ID {
		t.Errorf("owner household = %s, want %s", owner.HouseholdID, hh.ID)
	}

	entries, total, err := auditStore.QueryAuditLogs(ctx, co.ID, 20, 0, nil, nil, nil)
	if err != nil {
		t.Fatalf("query audit: %v", err)
	}
	if total < 2 {
		t.Errorf("audit total = %d, want >= 2 (created + member_added)", total)
	}
	var created, added bool
	for _, e := range entries {
		if e.Resource != "household" {
			continue
		}
		if e.GetActorId() != actorIdentityID.String() {
			t.Errorf("audit actor_id = %q, want identity %q", e.GetActorId(), actorIdentityID)
		}
		switch e.Action {
		case household.ActionHouseholdCreated:
			created = true
		case household.ActionMemberAdded:
			added = true
		}
	}
	if !created || !added {
		t.Errorf("audit rows: created=%v added=%v, want both", created, added)
	}
}

// TestHouseholdStore_OneAccountOwnerIndex asserts the partial unique index
// rejects a second active account_owner in the same household.
func TestHouseholdStore_OneAccountOwnerIndex(t *testing.T) {
	backend := setupTestBackend(t)
	hhStore := backend.HouseholdStore()
	ctx := context.Background()

	h, _ := hhStore.CreateHousehold(ctx, household.Household{PrimaryContactIdentityID: uuid.New(), DisplayName: "Owners"})
	if _, err := hhStore.AddMember(ctx, household.Member{
		HouseholdID: h.ID, IdentityID: uuid.New(), Role: household.RoleGuardian, IsManager: true, IsAccountOwner: true,
	}); err != nil {
		t.Fatalf("first owner: %v", err)
	}
	_, err := hhStore.AddMember(ctx, household.Member{
		HouseholdID: h.ID, IdentityID: uuid.New(), Role: household.RoleAdult, IsAccountOwner: true,
	})
	if !errors.Is(err, household.ErrAccountOwnerExists) {
		t.Fatalf("second owner err = %v, want ErrAccountOwnerExists", err)
	}
}

// TestHouseholdStore_TransferAccountOwner exercises the atomic transfer.
func TestHouseholdStore_TransferAccountOwner(t *testing.T) {
	backend := setupTestBackend(t)
	hhStore := backend.HouseholdStore()
	ctx := context.Background()

	h, _ := hhStore.CreateHousehold(ctx, household.Household{PrimaryContactIdentityID: uuid.New(), DisplayName: "Transfer"})
	owner, _ := hhStore.AddMember(ctx, household.Member{HouseholdID: h.ID, IdentityID: uuid.New(), Role: household.RoleGuardian, IsManager: true, IsAccountOwner: true})
	other, _ := hhStore.AddMember(ctx, household.Member{HouseholdID: h.ID, IdentityID: uuid.New(), Role: household.RoleAdult, IsManager: true})

	newOwner, err := hhStore.SetAccountOwner(ctx, h.ID, other.ID)
	if err != nil {
		t.Fatalf("transfer: %v", err)
	}
	if !newOwner.IsAccountOwner {
		t.Error("new owner not account_owner")
	}
	demoted, _ := hhStore.GetMember(ctx, owner.ID)
	if demoted.IsAccountOwner {
		t.Error("old owner still account_owner")
	}
}

// TestHouseholdStore_WrongHouseholdIsolation asserts household-scoped reads
// never cross households, at the DB layer.
func TestHouseholdStore_WrongHouseholdIsolation(t *testing.T) {
	backend := setupTestBackend(t)
	hhStore := backend.HouseholdStore()
	ctx := context.Background()

	hhA, _ := hhStore.CreateHousehold(ctx, household.Household{PrimaryContactIdentityID: uuid.New(), DisplayName: "A"})
	hhB, _ := hhStore.CreateHousehold(ctx, household.Household{PrimaryContactIdentityID: uuid.New(), DisplayName: "B"})

	shared := uuid.New()
	mA, _ := hhStore.AddMember(ctx, household.Member{HouseholdID: hhA.ID, IdentityID: shared, Role: household.RoleGuardian, IsManager: true, IsAccountOwner: true})
	mB, _ := hhStore.AddMember(ctx, household.Member{HouseholdID: hhB.ID, IdentityID: shared, Role: household.RoleGuardian, IsManager: true, IsAccountOwner: true})

	gotA, err := hhStore.GetMemberByIdentity(ctx, hhA.ID, shared)
	if err != nil {
		t.Fatalf("member by identity A: %v", err)
	}
	if gotA.ID != mA.ID {
		t.Errorf("scoped lookup A = %s, want %s (not B's %s)", gotA.ID, mA.ID, mB.ID)
	}

	membersA, _ := hhStore.ListMembers(ctx, hhA.ID)
	for _, m := range membersA {
		if m.HouseholdID != hhA.ID {
			t.Errorf("ListMembers(A) leaked household %s", m.HouseholdID)
		}
	}

	// A cross-household transfer target must not be found via A.
	if _, err := hhStore.SetAccountOwner(ctx, hhA.ID, mB.ID); !errors.Is(err, household.ErrNotFound) {
		t.Errorf("cross-household transfer err = %v, want ErrNotFound", err)
	}
}

func TestHouseholdStore_ParentOfRecord(t *testing.T) {
	backend := setupTestBackend(t)
	hhStore := backend.HouseholdStore()
	ctx := context.Background()

	h, _ := hhStore.CreateHousehold(ctx, household.Household{PrimaryContactIdentityID: uuid.New(), DisplayName: "POR"})
	bday := time.Date(2017, 1, 1, 0, 0, 0, 0, time.UTC)
	guardian, _ := hhStore.AddMember(ctx, household.Member{
		HouseholdID: h.ID, IdentityID: uuid.New(), Role: household.RoleGuardian, IsManager: true, IsAccountOwner: true,
	})
	child, _ := hhStore.AddMember(ctx, household.Member{
		HouseholdID: h.ID, IdentityID: uuid.New(), Role: household.RoleChild, Birthdate: &bday,
	})

	por, err := hhStore.EstablishParentOfRecord(ctx, child.ID, guardian.ID)
	if err != nil {
		t.Fatalf("establish: %v", err)
	}
	parents, _ := hhStore.ListParentsOfRecord(ctx, child.ID)
	if len(parents) != 1 {
		t.Errorf("parents = %d, want 1", len(parents))
	}
	children, _ := hhStore.ListChildrenForParent(ctx, guardian.ID)
	if len(children) != 1 {
		t.Errorf("children = %d, want 1", len(children))
	}

	if err := hhStore.RevokeParentOfRecord(ctx, por.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	parents, _ = hhStore.ListParentsOfRecord(ctx, child.ID)
	if len(parents) != 0 {
		t.Errorf("after revoke = %d, want 0", len(parents))
	}
}

func TestHouseholdService_EndToEnd_AuditEmitted(t *testing.T) {
	backend := setupTestBackend(t)
	companyStore := backend.CompanyStore()
	hhStore := backend.HouseholdStore()
	auditStore := backend.AuditStore()
	ctx := context.Background()

	// Prove the REAL production contract: the audit actor is the acting AOID
	// identity (aoid.identities(id)), NOT a platform.users row. No throwaway
	// user is created. Before migration 017 dropped the audit_logs.actor_id ->
	// users(id) FK, this identity UUID would FK-fail on insert and be silently
	// swallowed by the audit logger, so no rows would persist and the
	// assertions below would fail. That the rows ARE written proves the fix.
	actorIdentityID := uuid.New()
	guardianID := uuid.New()
	childID := uuid.New()
	co, _ := companyStore.CreateCompany(ctx, company.Company{Name: "E2E Fam", Slug: "e2e-fam"})

	logger := audit.NewLogger(auditStore)
	logger.Start()

	svc := household.NewService(hhStore, logger)
	ac := household.AuditContext{CompanyID: co.ID, ActorID: actorIdentityID, IPAddress: "10.0.0.1"}

	h, err := svc.CreateHousehold(ctx, ac, "End To End", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	guardian, err := svc.AddMember(ctx, ac, household.Member{
		HouseholdID: h.ID, IdentityID: guardianID, Role: household.RoleGuardian,
		IsManager: true, IsAccountOwner: true,
	})
	if err != nil {
		t.Fatalf("add guardian: %v", err)
	}
	bday := time.Date(2019, 5, 1, 0, 0, 0, 0, time.UTC)
	child, err := svc.AddMember(ctx, ac, household.Member{
		HouseholdID: h.ID, IdentityID: childID, Role: household.RoleChild, Birthdate: &bday,
	})
	if err != nil {
		t.Fatalf("add child: %v", err)
	}
	if _, err := svc.EstablishParentOfRecord(ctx, ac, child.ID, guardian.ID); err != nil {
		t.Fatalf("establish por: %v", err)
	}

	logger.Stop()

	entries, total, err := auditStore.QueryAuditLogs(ctx, co.ID, 20, 0, nil, nil, nil)
	if err != nil {
		t.Fatalf("query audit: %v", err)
	}
	if total < 4 {
		t.Errorf("audit total = %d, want >= 4", total)
	}
	expectActions := map[string]bool{
		household.ActionHouseholdCreated:          false,
		household.ActionMemberAdded:               false,
		household.ActionParentOfRecordEstablished: false,
	}
	householdRows := 0
	for _, e := range entries {
		if e.Resource != "household" {
			continue
		}
		householdRows++
		// The persisted row must carry the AOID identity as its actor — proof
		// the identity-space actor survived the insert (would have FK-failed
		// and been dropped before migration 017).
		if e.GetActorId() != actorIdentityID.String() {
			t.Errorf("audit actor_id = %q, want identity %q", e.GetActorId(), actorIdentityID.String())
		}
		if _, ok := expectActions[e.Action]; ok {
			expectActions[e.Action] = true
		}
	}
	if householdRows == 0 {
		t.Fatal("no household audit rows persisted — identity-actor inserts were dropped")
	}
	for action, seen := range expectActions {
		if !seen {
			t.Errorf("expected audit action %q not found", action)
		}
	}
}
