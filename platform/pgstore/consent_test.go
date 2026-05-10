package pgstore_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/aocybersystems/eden-platform-go/platform/audit"
	"github.com/aocybersystems/eden-platform-go/platform/company"
	"github.com/aocybersystems/eden-platform-go/platform/consent"
	"github.com/aocybersystems/eden-platform-go/platform/household"
	"github.com/aocybersystems/eden-platform-go/platform/pgstore"
	"github.com/google/uuid"
)

type consentSeed struct {
	companyID    uuid.UUID
	parentUserID uuid.UUID
	hh           household.Household
	parent       household.Member
	child        household.Member
}

func seedConsentFamily(t *testing.T, be *pgstore.Backend) consentSeed {
	t.Helper()
	authStore := be.AuthStore()
	companyStore := be.CompanyStore()
	hhStore := be.HouseholdStore()
	ctx := context.Background()

	prefix := strings.ToLower(strings.NewReplacer("/", "-", "_", "-").Replace(t.Name()))

	parentUser, err := authStore.CreateUser(ctx, prefix+"-parent@example.com", "h", "Parent")
	if err != nil {
		t.Fatalf("create parent user: %v", err)
	}
	childUser, err := authStore.CreateUser(ctx, prefix+"-child@example.com", "h", "Child")
	if err != nil {
		t.Fatalf("create child user: %v", err)
	}
	co, err := companyStore.CreateCompany(ctx, company.Company{Name: t.Name(), Slug: prefix})
	if err != nil {
		t.Fatalf("create company: %v", err)
	}

	hh, err := hhStore.CreateHousehold(ctx, household.Household{
		PrimaryContactUserID: parentUser.ID, DisplayName: "Consent Family",
	})
	if err != nil {
		t.Fatalf("create household: %v", err)
	}
	parent, err := hhStore.AddMember(ctx, household.Member{
		HouseholdID: hh.ID, UserID: parentUser.ID, Role: household.RoleParentOfRecord,
		Capabilities: household.DefaultCapabilities(household.RoleParentOfRecord),
	})
	if err != nil {
		t.Fatalf("add parent: %v", err)
	}
	bday := time.Date(2018, 6, 1, 0, 0, 0, 0, time.UTC)
	child, err := hhStore.AddMember(ctx, household.Member{
		HouseholdID: hh.ID, UserID: childUser.ID, Role: household.RoleChild, Birthdate: &bday,
	})
	if err != nil {
		t.Fatalf("add child: %v", err)
	}

	return consentSeed{
		companyID:    co.ID,
		parentUserID: parentUser.ID,
		hh:           hh,
		parent:       parent,
		child:        child,
	}
}

func TestConsentStore_AppendOnly(t *testing.T) {
	be := setupTestBackend(t)
	seed := seedConsentFamily(t, be)
	ctx := context.Background()

	cs := be.ConsentStore()
	g, err := cs.InsertEntry(ctx, consent.Entry{
		HouseholdID:        seed.hh.ID,
		PrincipalMemberID:  seed.child.ID,
		ConsenterMemberID:  seed.parent.ID,
		Purpose:            consent.PurposeChildAccountCreation,
		Scope:              json.RawMessage(`{"all":true}`),
		ConsentTextVersion: "v1.0",
		Evidence:           json.RawMessage(`{"method":"click_through"}`),
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	// UPDATE should fail with the trigger error.
	_, err = be.Pool().Exec(ctx, "UPDATE consent_ledger SET purpose = 'mutated' WHERE id = $1", g.ID)
	if err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Errorf("UPDATE expected append-only error, got %v", err)
	}

	// DELETE should fail too.
	_, err = be.Pool().Exec(ctx, "DELETE FROM consent_ledger WHERE id = $1", g.ID)
	if err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Errorf("DELETE expected append-only error, got %v", err)
	}
}

func TestConsentStore_GrantAndValidity(t *testing.T) {
	be := setupTestBackend(t)
	seed := seedConsentFamily(t, be)
	ctx := context.Background()

	logger := audit.NewLogger(be.AuditStore())
	logger.Start()

	svc := consent.NewService(be.ConsentStore(), logger)
	ac := consent.AuditContext{CompanyID: seed.companyID, ActorID: seed.parentUserID, IPAddress: "10.0.0.2"}

	g, err := svc.Grant(ctx, ac, consent.GrantRequest{
		HouseholdID:        seed.hh.ID,
		PrincipalMemberID:  seed.child.ID,
		ConsenterMemberID:  seed.parent.ID,
		Purpose:            consent.PurposeAITutorInteraction,
		ConsentTextVersion: "v1.0",
		Evidence:           json.RawMessage(`{"method":"click_through"}`),
	})
	if err != nil {
		t.Fatalf("grant: %v", err)
	}

	v, err := svc.IsValid(ctx, ac, seed.child.ID, consent.PurposeAITutorInteraction, time.Now())
	if err != nil {
		t.Fatalf("isvalid: %v", err)
	}
	if !v.Valid {
		t.Errorf("post-grant: expected valid")
	}

	rev, err := svc.Revoke(ctx, ac, g.ID, seed.parent.ID, json.RawMessage(`{"method":"click_through","reason":"settings_toggle"}`))
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if !rev.IsRevocation() {
		t.Error("revocation entry should have RevokesID set")
	}

	v, err = svc.IsValid(ctx, ac, seed.child.ID, consent.PurposeAITutorInteraction, time.Now())
	if err != nil {
		t.Fatalf("isvalid post-revoke: %v", err)
	}
	if v.Valid {
		t.Errorf("post-revoke: expected invalid, got %+v", v)
	}

	// History contains both rows.
	all, err := svc.ListForPrincipal(ctx, ac, seed.child.ID, 10, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("history = %d, want 2 (grant+revoke)", len(all))
	}

	logger.Stop()

	entries, _, err := be.AuditStore().QueryAuditLogs(ctx, seed.companyID, 50, 0, nil, nil, nil)
	if err != nil {
		t.Fatalf("query audit: %v", err)
	}
	expect := map[string]bool{
		consent.ActionConsentGranted: false,
		consent.ActionConsentRevoked: false,
		consent.ActionConsentRead:    false,
	}
	for _, e := range entries {
		if e.Resource == "consent" {
			if _, ok := expect[e.Action]; ok {
				expect[e.Action] = true
			}
		}
	}
	for action, seen := range expect {
		if !seen {
			t.Errorf("missing audit action %q", action)
		}
	}
}

func TestConsentStore_Renewal(t *testing.T) {
	be := setupTestBackend(t)
	seed := seedConsentFamily(t, be)
	ctx := context.Background()

	cs := be.ConsentStore()

	// v1.0 grant
	v1, err := cs.InsertEntry(ctx, consent.Entry{
		HouseholdID:        seed.hh.ID,
		PrincipalMemberID:  seed.child.ID,
		ConsenterMemberID:  seed.parent.ID,
		Purpose:            consent.PurposeMarketingCommunications,
		ConsentTextVersion: "v1.0",
	})
	if err != nil {
		t.Fatalf("v1: %v", err)
	}
	time.Sleep(5 * time.Millisecond)

	// v1.1 grant (renewal)
	v11, err := cs.InsertEntry(ctx, consent.Entry{
		HouseholdID:        seed.hh.ID,
		PrincipalMemberID:  seed.child.ID,
		ConsenterMemberID:  seed.parent.ID,
		Purpose:            consent.PurposeMarketingCommunications,
		ConsentTextVersion: "v1.1",
	})
	if err != nil {
		t.Fatalf("v1.1: %v", err)
	}

	latest, err := cs.LatestForPurpose(ctx, seed.child.ID, consent.PurposeMarketingCommunications)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if latest.ID != v11.ID {
		t.Errorf("latest = %s, want %s (v1.1)", latest.ID, v11.ID)
	}
	if latest.ConsentTextVersion != "v1.1" {
		t.Errorf("latest version = %q", latest.ConsentTextVersion)
	}

	history, err := cs.ListForPrincipal(ctx, seed.child.ID, 10, 0)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(history) != 2 {
		t.Errorf("history = %d, want 2", len(history))
	}
	_ = v1
}
