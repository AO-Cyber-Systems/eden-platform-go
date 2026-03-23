package server

import (
	"context"
	"testing"

	"github.com/aocybersystems/eden-platform-go/platform/auth"
)

func TestWithClaims_ClaimsFromContext(t *testing.T) {
	claims := &auth.Claims{
		UserID:    "user-1",
		CompanyID: "company-1",
		Role:      "admin",
		RoleLevel: 80,
	}

	ctx := WithClaims(context.Background(), claims)
	retrieved := ClaimsFromContext(ctx)

	if retrieved == nil {
		t.Fatalf("ClaimsFromContext() returned nil")
	}
	if retrieved.UserID != "user-1" {
		t.Errorf("UserID = %q, want %q", retrieved.UserID, "user-1")
	}
	if retrieved.CompanyID != "company-1" {
		t.Errorf("CompanyID = %q, want %q", retrieved.CompanyID, "company-1")
	}
	if retrieved.Role != "admin" {
		t.Errorf("Role = %q, want %q", retrieved.Role, "admin")
	}
}

func TestExtractClaims_NoClaims(t *testing.T) {
	userID, companyID, role := ExtractClaims(context.Background())
	if userID != "" || companyID != "" || role != "" {
		t.Errorf("ExtractClaims() with no claims = (%q, %q, %q), want empty strings", userID, companyID, role)
	}
}

func TestExtractClaims_WithClaims(t *testing.T) {
	claims := &auth.Claims{
		UserID:    "u1",
		CompanyID: "c1",
		Role:      "owner",
	}
	ctx := WithClaims(context.Background(), claims)

	userID, companyID, role := ExtractClaims(ctx)
	if userID != "u1" {
		t.Errorf("userID = %q, want %q", userID, "u1")
	}
	if companyID != "c1" {
		t.Errorf("companyID = %q, want %q", companyID, "c1")
	}
	if role != "owner" {
		t.Errorf("role = %q, want %q", role, "owner")
	}
}
