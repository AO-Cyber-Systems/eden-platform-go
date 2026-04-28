package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/aocybersystems/eden-platform-go/platform/auth"
	platformserver "github.com/aocybersystems/eden-platform-go/platform/server"
)

// TestRequireCompany covers the canonical happy + failure paths for the
// fail-closed RequireCompany helper that handlers will call as their first
// line of defense against tenant escape.
func TestRequireCompany(t *testing.T) {
	const (
		userID    = "11111111-1111-1111-1111-111111111111"
		companyID = "22222222-2222-2222-2222-222222222222"
	)

	t.Run("happy_path", func(t *testing.T) {
		claims := &auth.Claims{UserID: userID, CompanyID: companyID, Role: "owner", RoleLevel: 90}
		ctx := auth.WithClaims(context.Background(), claims)

		got, err := auth.RequireCompany(ctx)
		if err != nil {
			t.Fatalf("RequireCompany: unexpected err: %v", err)
		}
		if got.String() != companyID {
			t.Fatalf("RequireCompany: got %q, want %q", got, companyID)
		}
	})

	t.Run("no_claims", func(t *testing.T) {
		got, err := auth.RequireCompany(context.Background())
		if !errors.Is(err, auth.ErrNoCompany) {
			t.Fatalf("RequireCompany: want ErrNoCompany, got %v", err)
		}
		if got.String() != "00000000-0000-0000-0000-000000000000" {
			t.Fatalf("RequireCompany: want uuid.Nil, got %s", got)
		}
	})

	t.Run("claims_present_empty_company_id", func(t *testing.T) {
		claims := &auth.Claims{UserID: userID, CompanyID: ""}
		ctx := auth.WithClaims(context.Background(), claims)

		got, err := auth.RequireCompany(ctx)
		if !errors.Is(err, auth.ErrNoCompany) {
			t.Fatalf("RequireCompany: want ErrNoCompany, got %v", err)
		}
		if got.String() != "00000000-0000-0000-0000-000000000000" {
			t.Fatalf("RequireCompany: want uuid.Nil, got %s", got)
		}
	})

	t.Run("claims_present_invalid_uuid", func(t *testing.T) {
		claims := &auth.Claims{UserID: userID, CompanyID: "not-a-uuid"}
		ctx := auth.WithClaims(context.Background(), claims)

		got, err := auth.RequireCompany(ctx)
		if !errors.Is(err, auth.ErrNoCompany) {
			t.Fatalf("RequireCompany: want ErrNoCompany, got %v", err)
		}
		if got.String() != "00000000-0000-0000-0000-000000000000" {
			t.Fatalf("RequireCompany: want uuid.Nil, got %s", got)
		}
	})

	t.Run("happy_path_RequireUser", func(t *testing.T) {
		claims := &auth.Claims{UserID: userID, CompanyID: companyID}
		ctx := auth.WithClaims(context.Background(), claims)

		got, err := auth.RequireUser(ctx)
		if err != nil {
			t.Fatalf("RequireUser: unexpected err: %v", err)
		}
		if got.String() != userID {
			t.Fatalf("RequireUser: got %q, want %q", got, userID)
		}
	})

	// Bridge proof: claims written via the platform/server re-export are
	// readable by the canonical platform/auth.RequireCompany. This guards
	// the most likely regression — a future refactor that drifts the two
	// context keys apart again.
	t.Run("roundtrip_via_platform_server_re_export", func(t *testing.T) {
		claims := &auth.Claims{UserID: userID, CompanyID: companyID}
		ctx := platformserver.WithClaims(context.Background(), claims)

		got, err := auth.RequireCompany(ctx)
		if err != nil {
			t.Fatalf("roundtrip: unexpected err: %v", err)
		}
		if got.String() != companyID {
			t.Fatalf("roundtrip: got %q, want %q", got, companyID)
		}
	})
}
