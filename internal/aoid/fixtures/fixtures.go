// Package fixtures seeds dev / smoke-test data into a freshly composed
// aoid Services bundle. Production callers must NOT invoke Seed — it
// creates a deterministic test household, a parent + child member, a
// parent-of-record link, and a child_account_creation consent grant.
//
// Determinism: for tests that need stable IDs, callers can compose with
// SeedDeterministic which uses fixed UUIDs. Plain Seed picks fresh IDs
// each run so dev re-runs don't collide with prior data when the
// underlying backend persists.
package fixtures

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/aocybersystems/eden-platform-go/internal/aoid/composition"
	"github.com/aocybersystems/eden-platform-go/platform/consent"
	"github.com/aocybersystems/eden-platform-go/platform/household"
	"github.com/google/uuid"
)

// Fixture is the set of objects Seed creates. Returned for tests that
// want to assert on the seeded ids.
type Fixture struct {
	ParentUserID    uuid.UUID
	ChildUserID     uuid.UUID
	HouseholdID     uuid.UUID
	ParentMemberID  uuid.UUID
	ChildMemberID   uuid.UUID
	ParentOfRecord  household.ParentOfRecord
	ConsentEntryID  uuid.UUID
	ParentEmail     string
	ParentPassword  string
}

// Seed creates one household with a parent + child member, parent-of-
// record, and a child_account_creation consent grant. The parent is
// signed up via auth.Service so a real password credential round-trips.
//
// Returned Fixture exposes the created ids so smoke tests can assert
// against them.
func Seed(ctx context.Context, svcs *composition.Services) (*Fixture, error) {
	if svcs == nil {
		return nil, fmt.Errorf("fixtures: nil services")
	}

	// 1. Sign up the parent. This goes through the real auth pipeline
	//    (creates user + default company + owner membership + tokens).
	const parentEmail = "parent@aoid.local"
	const parentPassword = "fixtures-pw-1234"
	signup, err := svcs.Auth.SignUp(ctx, parentEmail, parentPassword, "Fixture Parent")
	if err != nil {
		return nil, fmt.Errorf("fixtures: signup parent: %w", err)
	}

	parentUserID := signup.User.ID
	companyID := uuid.New() // synthetic; signup.User.ID company linkage is opaque here

	// 2. Create a household with the parent as primary contact.
	ac := household.AuditContext{
		CompanyID: companyID,
		ActorID:   parentUserID,
		IPAddress: "127.0.0.1",
	}
	hh, err := svcs.Household.CreateHousehold(ctx, ac, "Fixture Household", json.RawMessage(`{"seeded":true}`))
	if err != nil {
		return nil, fmt.Errorf("fixtures: create household: %w", err)
	}

	parentMember, err := svcs.Household.AddMember(ctx, ac, household.Member{
		HouseholdID:  hh.ID,
		UserID:       parentUserID,
		Role:         household.RoleParentOfRecord,
		Status:       household.StatusActive,
		Capabilities: household.DefaultCapabilities(household.RoleParentOfRecord),
	})
	if err != nil {
		return nil, fmt.Errorf("fixtures: add parent member: %w", err)
	}

	// 3. Add a child member with a deterministic 8-year-old DOB so
	//    COPPA / GDPR-K logic exercises the under-13 path.
	childDOB := time.Now().UTC().AddDate(-8, 0, 0)
	childUserID := uuid.New()
	childMember, err := svcs.Household.AddMember(ctx, ac, household.Member{
		HouseholdID:  hh.ID,
		UserID:       childUserID,
		Role:         household.RoleChild,
		Status:       household.StatusActive,
		Birthdate:    &childDOB,
		Capabilities: household.DefaultCapabilities(household.RoleChild),
	})
	if err != nil {
		return nil, fmt.Errorf("fixtures: add child member: %w", err)
	}

	// 4. Establish parent-of-record link.
	por, err := svcs.Household.EstablishParentOfRecord(ctx, ac, childMember.ID, parentMember.ID)
	if err != nil {
		return nil, fmt.Errorf("fixtures: establish POR: %w", err)
	}

	// 5. Grant a child_account_creation consent so the consent ledger has
	//    a representative row.
	consentAC := consent.AuditContext{
		CompanyID: companyID,
		ActorID:   parentUserID,
		IPAddress: "127.0.0.1",
	}
	consentEntry, err := svcs.Consent.Grant(ctx, consentAC, consent.GrantRequest{
		HouseholdID:        hh.ID,
		PrincipalMemberID:  childMember.ID,
		ConsenterMemberID:  parentMember.ID,
		Purpose:            consent.PurposeChildAccountCreation,
		Scope:              json.RawMessage(`["account.create"]`),
		ConsentTextVersion: "1.0",
		Evidence:           json.RawMessage(`{"method":"click_through","reference":"fixture"}`),
	})
	if err != nil {
		return nil, fmt.Errorf("fixtures: grant consent: %w", err)
	}

	return &Fixture{
		ParentUserID:   parentUserID,
		ChildUserID:    childUserID,
		HouseholdID:    hh.ID,
		ParentMemberID: parentMember.ID,
		ChildMemberID:  childMember.ID,
		ParentOfRecord: por,
		ConsentEntryID: consentEntry.ID,
		ParentEmail:    parentEmail,
		ParentPassword: parentPassword,
	}, nil
}
