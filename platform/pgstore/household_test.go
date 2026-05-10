package pgstore_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/aocybersystems/eden-platform-go/platform/audit"
	"github.com/aocybersystems/eden-platform-go/platform/company"
	"github.com/aocybersystems/eden-platform-go/platform/household"
	"github.com/google/uuid"
)

func TestHouseholdStore_CreateAndQuery(t *testing.T) {
	backend := setupTestBackend(t)
	authStore := backend.AuthStore()
	hhStore := backend.HouseholdStore()
	ctx := context.Background()

	user, err := authStore.CreateUser(ctx, "household-primary@example.com", "h", "Primary")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	h, err := hhStore.CreateHousehold(ctx, household.Household{
		PrimaryContactUserID: user.ID,
		DisplayName:          "Smith Family",
		Metadata:             json.RawMessage(`{"plan":"family"}`),
	})
	if err != nil {
		t.Fatalf("create household: %v", err)
	}
	if h.ID == uuid.Nil {
		t.Error("household ID is nil")
	}
	if h.DisplayName != "Smith Family" {
		t.Errorf("display_name = %q, want %q", h.DisplayName, "Smith Family")
	}

	got, err := hhStore.GetHousehold(ctx, h.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.PrimaryContactUserID != user.ID {
		t.Errorf("primary contact = %s, want %s", got.PrimaryContactUserID, user.ID)
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
	authStore := backend.AuthStore()
	hhStore := backend.HouseholdStore()
	ctx := context.Background()

	parent, _ := authStore.CreateUser(ctx, "parent@example.com", "h", "Parent")
	child, _ := authStore.CreateUser(ctx, "child@example.com", "h", "Child")

	h, _ := hhStore.CreateHousehold(ctx, household.Household{PrimaryContactUserID: parent.ID, DisplayName: "Lifecycle"})

	parentMember, err := hhStore.AddMember(ctx, household.Member{
		HouseholdID:  h.ID,
		UserID:       parent.ID,
		Role:         household.RoleParentOfRecord,
		Status:       household.StatusActive,
		Capabilities: household.DefaultCapabilities(household.RoleParentOfRecord),
	})
	if err != nil {
		t.Fatalf("add parent: %v", err)
	}
	if parentMember.Role != household.RoleParentOfRecord {
		t.Errorf("role = %q", parentMember.Role)
	}
	if !parentMember.Capabilities.CanGrantConsent {
		t.Error("parent default capabilities missing CanGrantConsent")
	}

	bday := time.Date(2018, 3, 14, 0, 0, 0, 0, time.UTC)
	childMember, err := hhStore.AddMember(ctx, household.Member{
		HouseholdID: h.ID,
		UserID:      child.ID,
		Role:        household.RoleChild,
		Status:      household.StatusActive,
		Birthdate:   &bday,
	})
	if err != nil {
		t.Fatalf("add child: %v", err)
	}
	if childMember.Birthdate == nil || !childMember.Birthdate.Equal(bday) {
		t.Errorf("birthdate = %v, want %v", childMember.Birthdate, bday)
	}

	members, _ := hhStore.ListMembers(ctx, h.ID)
	if len(members) != 2 {
		t.Errorf("members = %d, want 2", len(members))
	}

	// Update role
	updated, err := hhStore.UpdateMemberRole(ctx, parentMember.ID, household.RoleGuardian, household.DefaultCapabilities(household.RoleGuardian))
	if err != nil {
		t.Fatalf("update role: %v", err)
	}
	if updated.Role != household.RoleGuardian {
		t.Errorf("updated role = %q", updated.Role)
	}

	// Remove
	if err := hhStore.RemoveMember(ctx, childMember.ID); err != nil {
		t.Fatalf("remove member: %v", err)
	}
	members, _ = hhStore.ListMembers(ctx, h.ID)
	if len(members) != 1 {
		t.Errorf("after remove = %d, want 1", len(members))
	}

	// ListHouseholdsForUser excludes removed members
	parentHHs, _ := hhStore.ListHouseholdsForUser(ctx, parent.ID)
	if len(parentHHs) != 1 {
		t.Errorf("parent households = %d, want 1", len(parentHHs))
	}
	childHHs, _ := hhStore.ListHouseholdsForUser(ctx, child.ID)
	if len(childHHs) != 0 {
		t.Errorf("removed child households = %d, want 0", len(childHHs))
	}
}

func TestHouseholdStore_ParentOfRecord(t *testing.T) {
	backend := setupTestBackend(t)
	authStore := backend.AuthStore()
	hhStore := backend.HouseholdStore()
	ctx := context.Background()

	parentUser, _ := authStore.CreateUser(ctx, "por-parent@example.com", "h", "Parent")
	childUser, _ := authStore.CreateUser(ctx, "por-child@example.com", "h", "Child")

	h, _ := hhStore.CreateHousehold(ctx, household.Household{PrimaryContactUserID: parentUser.ID, DisplayName: "POR"})
	bday := time.Date(2017, 1, 1, 0, 0, 0, 0, time.UTC)
	parentMember, _ := hhStore.AddMember(ctx, household.Member{
		HouseholdID: h.ID, UserID: parentUser.ID, Role: household.RoleParentOfRecord,
		Capabilities: household.DefaultCapabilities(household.RoleParentOfRecord),
	})
	childMember, _ := hhStore.AddMember(ctx, household.Member{
		HouseholdID: h.ID, UserID: childUser.ID, Role: household.RoleChild, Birthdate: &bday,
	})

	por, err := hhStore.EstablishParentOfRecord(ctx, childMember.ID, parentMember.ID)
	if err != nil {
		t.Fatalf("establish: %v", err)
	}
	if por.ChildMemberID != childMember.ID {
		t.Errorf("por.child = %s, want %s", por.ChildMemberID, childMember.ID)
	}

	parents, _ := hhStore.ListParentsOfRecord(ctx, childMember.ID)
	if len(parents) != 1 {
		t.Errorf("parents = %d, want 1", len(parents))
	}
	children, _ := hhStore.ListChildrenForParent(ctx, parentMember.ID)
	if len(children) != 1 {
		t.Errorf("children = %d, want 1", len(children))
	}

	// Revoke
	if err := hhStore.RevokeParentOfRecord(ctx, por.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	parents, _ = hhStore.ListParentsOfRecord(ctx, childMember.ID)
	if len(parents) != 0 {
		t.Errorf("after revoke parents = %d, want 0", len(parents))
	}
}

func TestHouseholdService_EndToEnd_AuditEmitted(t *testing.T) {
	backend := setupTestBackend(t)
	authStore := backend.AuthStore()
	companyStore := backend.CompanyStore()
	hhStore := backend.HouseholdStore()
	auditStore := backend.AuditStore()
	ctx := context.Background()

	parentUser, _ := authStore.CreateUser(ctx, "e2e-parent@example.com", "h", "Parent")
	childUser, _ := authStore.CreateUser(ctx, "e2e-child@example.com", "h", "Child")
	co, _ := companyStore.CreateCompany(ctx, company.Company{Name: "E2E Fam", Slug: "e2e-fam"})

	logger := audit.NewLogger(auditStore)
	logger.Start()

	svc := household.NewService(hhStore, logger)
	ac := household.AuditContext{
		CompanyID: co.ID,
		ActorID:   parentUser.ID,
		IPAddress: "10.0.0.1",
	}

	h, err := svc.CreateHousehold(ctx, ac, "End To End", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	parentMember, err := svc.AddMember(ctx, ac, household.Member{
		HouseholdID:  h.ID,
		UserID:       parentUser.ID,
		Role:         household.RoleParentOfRecord,
		Capabilities: household.DefaultCapabilities(household.RoleParentOfRecord),
	})
	if err != nil {
		t.Fatalf("add parent: %v", err)
	}

	bday := time.Date(2019, 5, 1, 0, 0, 0, 0, time.UTC)
	childMember, err := svc.AddMember(ctx, ac, household.Member{
		HouseholdID: h.ID,
		UserID:      childUser.ID,
		Role:        household.RoleChild,
		Birthdate:   &bday,
	})
	if err != nil {
		t.Fatalf("add child: %v", err)
	}

	if _, err := svc.EstablishParentOfRecord(ctx, ac, childMember.ID, parentMember.ID); err != nil {
		t.Fatalf("establish por: %v", err)
	}

	// Drain audit logger so writes flush.
	logger.Stop()

	// Audit log should now contain at least 4 events for our company.
	entries, total, err := auditStore.QueryAuditLogs(ctx, co.ID, 20, 0, nil, nil, nil)
	if err != nil {
		t.Fatalf("query audit: %v", err)
	}
	if total < 4 {
		t.Errorf("audit total = %d, want >= 4 (create + add x2 + establish_por)", total)
	}
	householdResource := "household"
	expectActions := map[string]bool{
		household.ActionHouseholdCreated:          false,
		household.ActionMemberAdded:               false,
		household.ActionParentOfRecordEstablished: false,
	}
	for _, e := range entries {
		if e.Resource != householdResource {
			continue
		}
		if _, ok := expectActions[e.Action]; ok {
			expectActions[e.Action] = true
		}
	}
	for action, seen := range expectActions {
		if !seen {
			t.Errorf("expected audit action %q not found", action)
		}
	}
}
