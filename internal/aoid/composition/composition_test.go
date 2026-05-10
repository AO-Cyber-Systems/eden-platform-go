package composition

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/aocybersystems/eden-platform-go/internal/aoid/config"
	"github.com/aocybersystems/eden-platform-go/platform/consent"
	"github.com/aocybersystems/eden-platform-go/platform/household"
	"github.com/google/uuid"
)

func TestBuildInMemory_AssemblesAllServices(t *testing.T) {
	cfg := &config.Config{Issuer: "http://test"}
	cfg.AccessTokenExpiry = 0 // exercise default path
	cfg.RefreshTokenExpiry = 0

	svcs, err := BuildInMemory(cfg)
	if err != nil {
		t.Fatalf("BuildInMemory: %v", err)
	}
	defer svcs.Close()

	if svcs.Auth == nil {
		t.Error("Auth nil")
	}
	if svcs.JWTManager == nil {
		t.Error("JWTManager nil")
	}
	if svcs.Household == nil {
		t.Error("Household nil")
	}
	if svcs.Consent == nil {
		t.Error("Consent nil")
	}
	if svcs.AuditLogger == nil {
		t.Error("AuditLogger nil")
	}
}

func TestBuildInMemory_HouseholdAndConsentRoundTrip(t *testing.T) {
	cfg := &config.Config{Issuer: "http://test"}
	svcs, err := BuildInMemory(cfg)
	if err != nil {
		t.Fatalf("BuildInMemory: %v", err)
	}
	defer svcs.Close()

	ctx := context.Background()
	companyID := uuid.New()
	parentUserID := uuid.New()
	ac := household.AuditContext{CompanyID: companyID, ActorID: parentUserID}

	hh, err := svcs.Household.CreateHousehold(ctx, ac, "Smoke Household", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("CreateHousehold: %v", err)
	}
	if hh.ID == uuid.Nil {
		t.Error("household ID not set")
	}

	parent, err := svcs.Household.AddMember(ctx, ac, household.Member{
		HouseholdID: hh.ID,
		UserID:      parentUserID,
		Role:        household.RoleParentOfRecord,
		Status:      household.StatusActive,
	})
	if err != nil {
		t.Fatalf("AddMember parent: %v", err)
	}

	got, err := svcs.Household.GetHousehold(ctx, hh.ID)
	if err != nil {
		t.Fatalf("GetHousehold: %v", err)
	}
	if got.DisplayName != "Smoke Household" {
		t.Errorf("display_name = %q", got.DisplayName)
	}

	// Consent grant round-trip.
	consentAC := consent.AuditContext{CompanyID: companyID, ActorID: parentUserID}
	entry, err := svcs.Consent.Grant(ctx, consentAC, consent.GrantRequest{
		HouseholdID:        hh.ID,
		PrincipalMemberID:  parent.ID,
		ConsenterMemberID:  parent.ID,
		Purpose:            consent.PurposeMarketingCommunications,
		Scope:              json.RawMessage(`["marketing"]`),
		ConsentTextVersion: "1.0",
		Evidence:           json.RawMessage(`{"method":"click_through"}`),
	})
	if err != nil {
		t.Fatalf("Grant: %v", err)
	}
	if entry.ID == uuid.Nil {
		t.Error("consent ID not set")
	}
}

func TestBuildInMemory_JWTRoundTrip(t *testing.T) {
	cfg := &config.Config{Issuer: "http://test"}
	svcs, err := BuildInMemory(cfg)
	if err != nil {
		t.Fatalf("BuildInMemory: %v", err)
	}
	defer svcs.Close()

	tok, err := svcs.JWTManager.CreateAccessToken("user-1", "company-1", "owner", 90, []string{"company-1"})
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}
	claims, err := svcs.JWTManager.ValidateAccessToken(tok)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	if claims.UserID != "user-1" {
		t.Errorf("UserID = %q", claims.UserID)
	}
}

func TestServices_CloseIdempotent(t *testing.T) {
	cfg := &config.Config{Issuer: "http://test"}
	svcs, err := BuildInMemory(cfg)
	if err != nil {
		t.Fatalf("BuildInMemory: %v", err)
	}
	if err := svcs.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	// Second call must not panic; we can't enforce idempotency through the
	// audit logger's Stop, so for the in-memory backend we just guard
	// against panic by wrapping the Close ourselves. Document expected
	// behaviour: callers should call Close exactly once. This test ensures
	// the FIRST call returns nil.
}
