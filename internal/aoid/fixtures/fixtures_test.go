package fixtures

import (
	"context"
	"testing"

	"github.com/aocybersystems/eden-platform-go/internal/aoid/composition"
	"github.com/aocybersystems/eden-platform-go/internal/aoid/config"
	"github.com/aocybersystems/eden-platform-go/platform/consent"
)

func TestSeed_PopulatesHouseholdParentChildAndConsent(t *testing.T) {
	cfg := &config.Config{Issuer: "http://test"}
	svcs, err := composition.BuildInMemory(cfg)
	if err != nil {
		t.Fatalf("BuildInMemory: %v", err)
	}
	defer svcs.Close()

	ctx := context.Background()
	fix, err := Seed(ctx, svcs)
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}

	// Household has parent + child member.
	members, err := svcs.Household.ListMembers(ctx, fix.HouseholdID)
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(members) != 2 {
		t.Errorf("expected 2 members, got %d", len(members))
	}

	// Parent-of-record link present and not revoked.
	pors, err := svcs.Household.ListParentsOfRecord(ctx, fix.ChildMemberID)
	if err != nil {
		t.Fatalf("ListParentsOfRecord: %v", err)
	}
	if len(pors) != 1 {
		t.Fatalf("expected 1 POR, got %d", len(pors))
	}
	if pors[0].ParentMemberID != fix.ParentMemberID {
		t.Errorf("POR parent member = %s want %s", pors[0].ParentMemberID, fix.ParentMemberID)
	}

	// Consent ledger has the grant.
	entries, err := svcs.Consent.ListForPrincipal(ctx, consent.AuditContext{}, fix.ChildMemberID, 10, 0)
	if err != nil {
		t.Fatalf("ListForPrincipal: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 consent entry, got %d", len(entries))
	}
	if entries[0].Purpose != consent.PurposeChildAccountCreation {
		t.Errorf("purpose = %q", entries[0].Purpose)
	}
	if entries[0].ID != fix.ConsentEntryID {
		t.Errorf("consent ID mismatch")
	}
}

func TestSeed_NilServicesError(t *testing.T) {
	if _, err := Seed(context.Background(), nil); err == nil {
		t.Error("expected error for nil services")
	}
}

func TestSeed_LoginRoundTripWithSeededParent(t *testing.T) {
	cfg := &config.Config{Issuer: "http://test"}
	svcs, err := composition.BuildInMemory(cfg)
	if err != nil {
		t.Fatalf("BuildInMemory: %v", err)
	}
	defer svcs.Close()

	ctx := context.Background()
	fix, err := Seed(ctx, svcs)
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}

	// The seeded parent should be able to log in with the documented password.
	resp, err := svcs.Auth.Login(ctx, fix.ParentEmail, fix.ParentPassword)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if resp.AccessToken == "" {
		t.Error("AccessToken empty")
	}
	if resp.User.ID != fix.ParentUserID {
		t.Errorf("user ID mismatch")
	}
}
