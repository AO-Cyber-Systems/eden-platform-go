package pgstore_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/aocybersystems/eden-platform-go/platform/audit"
	"github.com/aocybersystems/eden-platform-go/platform/company"
	"github.com/aocybersystems/eden-platform-go/platform/household"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// TestHouseholdService_AuditPersists_NoCompany proves the real production shape
// of household / COPPA audit: a household is NOT a platform company, so the
// acting AuditContext carries NO company (CompanyID = uuid.Nil). Driven through
// the real household.Service + real async audit.Logger + real Postgres store,
// the household.created / member_added events MUST persist to audit_logs with a
// NULL company_id and the acting AOID identity as the actor.
//
// Before migration 018 (audit_logs.company_id was NOT NULL REFERENCES
// companies(id)) this insert FK-failed on audit_logs_company_id_fkey — a
// household has no companies(id) — and the async logger swallowed the error
// (slog.Warn in platform/audit/logger.go), so the COPPA audit trail was
// SILENTLY DROPPED. That these rows persist proves the fix.
//
// NOTE: the pre-existing TestHouseholdService_*_AuditEmitted tests inject a
// REAL company id into the AuditContext, which masks this gap; this test uses
// the true no-company shape.
func TestHouseholdService_AuditPersists_NoCompany(t *testing.T) {
	backend := setupTestBackend(t)
	hhStore := backend.HouseholdStore()
	auditStore := backend.AuditStore()
	ctx := context.Background()

	actorIdentityID := uuid.New() // an aoid.identities(id), NOT a platform.users row
	guardianID := uuid.New()

	logger := audit.NewLogger(auditStore)
	logger.Start()
	svc := household.NewService(hhStore, logger)

	// The real production contract: no company scope for a household.
	ac := household.AuditContext{CompanyID: uuid.Nil, ActorID: actorIdentityID, IPAddress: "10.0.0.9"}

	h, err := svc.CreateHousehold(ctx, ac, "No Company Fam", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("create household: %v", err)
	}
	if _, err := svc.AddMember(ctx, ac, household.Member{
		HouseholdID: h.ID, IdentityID: guardianID, Role: household.RoleGuardian,
		IsManager: true, IsAccountOwner: true,
	}); err != nil {
		t.Fatalf("add member: %v", err)
	}
	logger.Stop() // drains the async buffer

	// Query the identity actor's audit rows directly (QueryAuditLogs is
	// company-scoped and cannot see NULL-company rows). Assert the rows
	// persisted with a NULL company_id.
	rows, err := backend.Pool().Query(ctx,
		`SELECT company_id, actor_id, action FROM audit_logs WHERE actor_id = $1 ORDER BY action`,
		actorIdentityID)
	if err != nil {
		t.Fatalf("query audit_logs: %v", err)
	}
	defer rows.Close()

	seen := map[string]bool{}
	count := 0
	for rows.Next() {
		var companyID pgtype.UUID
		var actorID uuid.UUID
		var action string
		if err := rows.Scan(&companyID, &actorID, &action); err != nil {
			t.Fatalf("scan: %v", err)
		}
		count++
		if companyID.Valid {
			t.Errorf("action %q: company_id = %x, want NULL (household has no company)", action, companyID.Bytes)
		}
		if actorID != actorIdentityID {
			t.Errorf("action %q: actor_id = %s, want identity %s", action, actorID, actorIdentityID)
		}
		seen[action] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows err: %v", err)
	}

	if count == 0 {
		t.Fatal("no household audit rows persisted — no-company inserts were dropped (migration 018 missing?)")
	}
	if !seen[household.ActionHouseholdCreated] {
		t.Errorf("missing %q audit row", household.ActionHouseholdCreated)
	}
	if !seen[household.ActionMemberAdded] {
		t.Errorf("missing %q audit row", household.ActionMemberAdded)
	}
}

// TestAuditLog_ZeroCompanyFK_DifferentialControl is the deterministic
// differential control for the migration-018 fix. It proves both halves on the
// CURRENT (post-018) schema against the same table:
//
//   - RED (pre-fix behavior): inserting an audit row with the all-zero UUID as
//     a NON-NULL company_id still violates audit_logs_company_id_fkey — exactly
//     the failure that silently dropped household audit before the fix. This is
//     what the old code path did (it passed uuid.Nil straight through as a
//     non-null value).
//   - GREEN (post-fix behavior): inserting the same row with company_id = NULL
//     (what pgstore now maps a zero company to) succeeds — the FK exempts NULL.
//
// The FK is deliberately left in place by 018, so a real (non-existent) company
// id is still rejected; only NULL is allowed through.
func TestAuditLog_ZeroCompanyFK_DifferentialControl(t *testing.T) {
	backend := setupTestBackend(t)
	ctx := context.Background()
	actorID := uuid.New()

	// RED: the pre-fix value (all-zero UUID, NON-NULL) must FK-fail.
	_, err := backend.Pool().Exec(ctx,
		`INSERT INTO audit_logs (company_id, actor_id, action, resource, resource_id, details, ip_address)
		 VALUES ('00000000-0000-0000-0000-000000000000'::uuid, $1, 'household.created', 'household', '', '{}', '')`,
		actorID)
	if err == nil {
		t.Fatal("expected zero-UUID company_id insert to violate audit_logs_company_id_fkey, but it succeeded")
	}
	if !strings.Contains(err.Error(), "audit_logs_company_id_fkey") {
		t.Fatalf("expected FK violation on audit_logs_company_id_fkey, got: %v", err)
	}

	// GREEN: the post-fix value (NULL company_id) must insert cleanly.
	_, err = backend.Pool().Exec(ctx,
		`INSERT INTO audit_logs (company_id, actor_id, action, resource, resource_id, details, ip_address)
		 VALUES (NULL, $1, 'household.created', 'household', '', '{}', '')`,
		actorID)
	if err != nil {
		t.Fatalf("NULL company_id insert should succeed post-018, got: %v", err)
	}

	// Confirm the persisted row carries a NULL company_id.
	var companyID pgtype.UUID
	if err := backend.Pool().QueryRow(ctx,
		`SELECT company_id FROM audit_logs WHERE actor_id = $1`, actorID).Scan(&companyID); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if companyID.Valid {
		t.Errorf("company_id = %x, want NULL", companyID.Bytes)
	}
}

// TestAuditLog_CompanyScoped_StillPersists is the no-regression half: a normal
// COMPANY-scoped audit event (a real companies(id)) must still persist and be
// readable through the company-scoped query path, unchanged by migration 018.
func TestAuditLog_CompanyScoped_StillPersists(t *testing.T) {
	backend := setupTestBackend(t)
	auditStore := backend.AuditStore()
	companyStore := backend.CompanyStore()
	ctx := context.Background()

	co, err := companyStore.CreateCompany(ctx, company.Company{Name: "Scoped Co", Slug: "scoped-co"})
	if err != nil {
		t.Fatalf("create company: %v", err)
	}
	actorID := uuid.New()

	if err := auditStore.CreateAuditLog(ctx, co.ID, actorID, "settings.updated", "settings", "", "127.0.0.1", []byte(`{}`)); err != nil {
		t.Fatalf("create company-scoped audit: %v", err)
	}

	entries, total, err := auditStore.QueryAuditLogs(ctx, co.ID, 10, 0, nil, nil, nil)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if total != 1 || len(entries) != 1 {
		t.Fatalf("company-scoped audit total=%d entries=%d, want 1/1", total, len(entries))
	}
	if entries[0].GetCompanyId() != co.ID.String() {
		t.Errorf("company_id = %q, want %q", entries[0].GetCompanyId(), co.ID.String())
	}
}
