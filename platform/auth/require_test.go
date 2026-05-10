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

// TestRequireHousehold covers the fail-closed paths for the household-
// scoped require helper (Obj 24a). Mirrors TestRequireCompany's shape so
// the household consumers (AOFamily, Eden Family) get the same audit
// guarantees as B2B consumers.
func TestRequireHousehold(t *testing.T) {
	const (
		userID      = "11111111-1111-1111-1111-111111111111"
		householdID = "33333333-3333-3333-3333-333333333333"
	)

	t.Run("happy_path", func(t *testing.T) {
		claims := &auth.Claims{UserID: userID, HouseholdID: householdID}
		ctx := auth.WithClaims(context.Background(), claims)

		got, err := auth.RequireHousehold(ctx)
		if err != nil {
			t.Fatalf("RequireHousehold: unexpected err: %v", err)
		}
		if got.String() != householdID {
			t.Fatalf("RequireHousehold: got %q, want %q", got, householdID)
		}
	})

	t.Run("no_claims", func(t *testing.T) {
		got, err := auth.RequireHousehold(context.Background())
		if !errors.Is(err, auth.ErrNoHousehold) {
			t.Fatalf("RequireHousehold: want ErrNoHousehold, got %v", err)
		}
		if got.String() != "00000000-0000-0000-0000-000000000000" {
			t.Fatalf("RequireHousehold: want uuid.Nil, got %s", got)
		}
	})

	t.Run("claims_present_empty_household_id", func(t *testing.T) {
		claims := &auth.Claims{UserID: userID, HouseholdID: ""}
		ctx := auth.WithClaims(context.Background(), claims)

		_, err := auth.RequireHousehold(ctx)
		if !errors.Is(err, auth.ErrNoHousehold) {
			t.Fatalf("RequireHousehold: want ErrNoHousehold, got %v", err)
		}
	})

	t.Run("claims_present_invalid_uuid", func(t *testing.T) {
		claims := &auth.Claims{UserID: userID, HouseholdID: "not-a-uuid"}
		ctx := auth.WithClaims(context.Background(), claims)

		_, err := auth.RequireHousehold(ctx)
		if !errors.Is(err, auth.ErrNoHousehold) {
			t.Fatalf("RequireHousehold: want ErrNoHousehold, got %v", err)
		}
	})

	// B2B claims (HouseholdID empty) must NOT pass RequireHousehold —
	// that's the whole point of fail-closed.
	t.Run("b2b_claims_rejected", func(t *testing.T) {
		claims := &auth.Claims{UserID: userID, CompanyID: "22222222-2222-2222-2222-222222222222", Role: "admin", RoleLevel: 80}
		ctx := auth.WithClaims(context.Background(), claims)

		_, err := auth.RequireHousehold(ctx)
		if !errors.Is(err, auth.ErrNoHousehold) {
			t.Fatalf("RequireHousehold: B2B claims should fail, got %v", err)
		}
	})
}

// TestRequireParentMode covers the parental-control middleware helper.
// AOFamily backends use this to gate parent-only actions (managing children,
// granting consent, etc.).
func TestRequireParentMode(t *testing.T) {
	const (
		userID      = "11111111-1111-1111-1111-111111111111"
		householdID = "33333333-3333-3333-3333-333333333333"
	)

	t.Run("happy_path_household_parent", func(t *testing.T) {
		claims := &auth.Claims{UserID: userID, HouseholdID: householdID, ChildMode: false}
		ctx := auth.WithClaims(context.Background(), claims)

		got, err := auth.RequireParentMode(ctx)
		if err != nil {
			t.Fatalf("RequireParentMode: unexpected err: %v", err)
		}
		if got.HouseholdID != householdID {
			t.Fatalf("RequireParentMode: HouseholdID = %q, want %q", got.HouseholdID, householdID)
		}
	})

	t.Run("child_mode_rejected", func(t *testing.T) {
		claims := &auth.Claims{UserID: userID, HouseholdID: householdID, ChildID: "44444444-4444-4444-4444-444444444444", ChildMode: true}
		ctx := auth.WithClaims(context.Background(), claims)

		_, err := auth.RequireParentMode(ctx)
		if !errors.Is(err, auth.ErrNotParentMode) {
			t.Fatalf("RequireParentMode: want ErrNotParentMode, got %v", err)
		}
	})

	t.Run("no_claims", func(t *testing.T) {
		_, err := auth.RequireParentMode(context.Background())
		if !errors.Is(err, auth.ErrNoHousehold) {
			t.Fatalf("RequireParentMode: want ErrNoHousehold (no claims), got %v", err)
		}
	})

	// B2B claims (no HouseholdID, ChildMode defaults to false) pass
	// RequireParentMode. The helper's semantic is "not in child mode,"
	// not "has household." Mixed-tenancy products that want B2B claims
	// to pass parent-mode checks are explicitly supported.
	t.Run("b2b_claims_pass", func(t *testing.T) {
		claims := &auth.Claims{UserID: userID, CompanyID: "22222222-2222-2222-2222-222222222222", Role: "admin"}
		ctx := auth.WithClaims(context.Background(), claims)

		got, err := auth.RequireParentMode(ctx)
		if err != nil {
			t.Fatalf("RequireParentMode: B2B claims should pass (ChildMode defaults to false), got %v", err)
		}
		if got.UserID != userID {
			t.Fatalf("RequireParentMode: UserID = %q, want %q", got.UserID, userID)
		}
	})
}

// TestRequireChildMode covers the inverse helper used by child-mode
// endpoints (e.g., the AOFamily-AI chat endpoint when a child is using
// the device).
func TestRequireChildMode(t *testing.T) {
	const (
		userID      = "11111111-1111-1111-1111-111111111111"
		householdID = "33333333-3333-3333-3333-333333333333"
		childID     = "44444444-4444-4444-4444-444444444444"
	)

	t.Run("happy_path_child_mode", func(t *testing.T) {
		claims := &auth.Claims{UserID: userID, HouseholdID: householdID, ChildID: childID, ChildMode: true}
		ctx := auth.WithClaims(context.Background(), claims)

		got, err := auth.RequireChildMode(ctx)
		if err != nil {
			t.Fatalf("RequireChildMode: unexpected err: %v", err)
		}
		if got.ChildID != childID {
			t.Fatalf("RequireChildMode: ChildID = %q, want %q", got.ChildID, childID)
		}
		if !got.ChildMode {
			t.Fatalf("RequireChildMode: ChildMode = false, want true")
		}
	})

	t.Run("parent_mode_rejected", func(t *testing.T) {
		claims := &auth.Claims{UserID: userID, HouseholdID: householdID, ChildMode: false}
		ctx := auth.WithClaims(context.Background(), claims)

		_, err := auth.RequireChildMode(ctx)
		if !errors.Is(err, auth.ErrNotChildMode) {
			t.Fatalf("RequireChildMode: want ErrNotChildMode, got %v", err)
		}
	})

	t.Run("no_claims", func(t *testing.T) {
		_, err := auth.RequireChildMode(context.Background())
		if !errors.Is(err, auth.ErrNoHousehold) {
			t.Fatalf("RequireChildMode: want ErrNoHousehold (no claims), got %v", err)
		}
	})

	// B2B claims must NOT pass RequireChildMode — they have ChildMode=false.
	t.Run("b2b_claims_rejected", func(t *testing.T) {
		claims := &auth.Claims{UserID: userID, CompanyID: "22222222-2222-2222-2222-222222222222", Role: "admin"}
		ctx := auth.WithClaims(context.Background(), claims)

		_, err := auth.RequireChildMode(ctx)
		if !errors.Is(err, auth.ErrNotChildMode) {
			t.Fatalf("RequireChildMode: B2B claims should fail, got %v", err)
		}
	})
}
