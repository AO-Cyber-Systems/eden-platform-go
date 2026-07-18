package pgstore_test

import (
	"context"
	"testing"

	"github.com/aocybersystems/eden-platform-go/platform/auth"
	"github.com/aocybersystems/eden-platform-go/platform/pgstore"
	"github.com/google/uuid"
)

// COMPANION-AV05-AOID-JIT TRD-06 — the JIT provisioning policy moved OFF biz env
// vars ONTO the per-company sso_configs row. These DB-backed tests prove (a) the
// three new policy columns round-trip through UpsertSSOConfig/GetSSOConfig/
// ListSSOConfigs and (b) ResolveJITCompanyByIssuerDomain derives the company +
// default role by issuer+domain, denying on no-match / jit_enabled=false and
// FAILING SECURE (ErrAmbiguousJITMatch) on an ambiguous same-issuer match.
//
// DATABASE_URL-gated (setupTestBackend skips when unset — CI sets it).

const jitPolicyIssuer = "https://auth.aocyber.ai"

func seedJITCompany(t *testing.T, store *pgstore.AuthStore, slug string) uuid.UUID {
	t.Helper()
	id, err := store.CreateCompany(context.Background(), "JIT "+slug, slug, "standalone")
	if err != nil {
		t.Fatalf("create company %s: %v", slug, err)
	}
	return id
}

func TestAuthStore_SSOJITPolicy_RoundTrip(t *testing.T) {
	backend := setupTestBackend(t)
	store := backend.AuthStore()
	ctx := context.Background()

	companyID := seedJITCompany(t, store, "sso-jit-rt-"+uuid.NewString()[:8])
	want := auth.SSOConfig{
		CompanyID:            companyID,
		Provider:             "oidc",
		IssuerURL:            jitPolicyIssuer,
		DisplayName:          "AO Cyber",
		IsActive:             true,
		EmailDomainAllowlist: []string{"aocyber.ai"},
		JITDefaultRole:       "manager",
		JITEnabled:           true,
	}
	if err := store.UpsertSSOConfig(ctx, want); err != nil {
		t.Fatalf("upsert sso config: %v", err)
	}

	got, err := store.GetSSOConfig(ctx, companyID, "oidc")
	if err != nil {
		t.Fatalf("get sso config: %v", err)
	}
	if len(got.EmailDomainAllowlist) != 1 || got.EmailDomainAllowlist[0] != "aocyber.ai" {
		t.Errorf("EmailDomainAllowlist = %v, want [aocyber.ai]", got.EmailDomainAllowlist)
	}
	if got.JITDefaultRole != "manager" {
		t.Errorf("JITDefaultRole = %q, want manager", got.JITDefaultRole)
	}
	if !got.JITEnabled {
		t.Errorf("JITEnabled = false, want true")
	}

	// ListSSOConfigs must also carry the policy fields.
	list, err := store.ListSSOConfigs(ctx, companyID)
	if err != nil {
		t.Fatalf("list sso configs: %v", err)
	}
	if len(list) != 1 || list[0].JITDefaultRole != "manager" || !list[0].JITEnabled {
		t.Errorf("ListSSOConfigs policy fields not round-tripped: %+v", list)
	}
}

func TestAuthStore_SSOJITPolicy_ResolveByIssuerDomain_Happy(t *testing.T) {
	backend := setupTestBackend(t)
	store := backend.AuthStore()
	ctx := context.Background()

	companyID := seedJITCompany(t, store, "sso-jit-ok-"+uuid.NewString()[:8])
	if err := store.UpsertSSOConfig(ctx, auth.SSOConfig{
		CompanyID: companyID, Provider: "oidc", IssuerURL: jitPolicyIssuer, IsActive: true,
		EmailDomainAllowlist: []string{"aocyber.ai"}, JITDefaultRole: "manager", JITEnabled: true,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	gotCompany, gotRole, err := store.ResolveJITCompanyByIssuerDomain(ctx, jitPolicyIssuer, "aocyber.ai")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if gotCompany != companyID {
		t.Errorf("company = %s, want %s", gotCompany, companyID)
	}
	if gotRole != "manager" {
		t.Errorf("role = %q, want manager", gotRole)
	}
}

func TestAuthStore_SSOJITPolicy_ResolveByIssuerDomain_NoMatchAndDisabled(t *testing.T) {
	backend := setupTestBackend(t)
	store := backend.AuthStore()
	ctx := context.Background()

	// (a) domain not in allowlist ⇒ ErrNoJITMatch.
	companyID := seedJITCompany(t, store, "sso-jit-nm-"+uuid.NewString()[:8])
	if err := store.UpsertSSOConfig(ctx, auth.SSOConfig{
		CompanyID: companyID, Provider: "oidc", IssuerURL: jitPolicyIssuer, IsActive: true,
		EmailDomainAllowlist: []string{"aocyber.ai"}, JITDefaultRole: "manager", JITEnabled: true,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if _, _, err := store.ResolveJITCompanyByIssuerDomain(ctx, jitPolicyIssuer, "evil.com"); err != auth.ErrNoJITMatch {
		t.Errorf("unknown domain err = %v, want ErrNoJITMatch", err)
	}

	// (b) jit_enabled=false ⇒ ErrNoJITMatch even with a matching domain.
	disabledID := seedJITCompany(t, store, "sso-jit-off-"+uuid.NewString()[:8])
	if err := store.UpsertSSOConfig(ctx, auth.SSOConfig{
		CompanyID: disabledID, Provider: "oidc", IssuerURL: jitPolicyIssuer, IsActive: true,
		EmailDomainAllowlist: []string{"disabled.example"}, JITDefaultRole: "manager", JITEnabled: false,
	}); err != nil {
		t.Fatalf("upsert disabled: %v", err)
	}
	if _, _, err := store.ResolveJITCompanyByIssuerDomain(ctx, jitPolicyIssuer, "disabled.example"); err != auth.ErrNoJITMatch {
		t.Errorf("jit_enabled=false err = %v, want ErrNoJITMatch", err)
	}
}

func TestAuthStore_SSOJITPolicy_ResolveByIssuerDomain_Ambiguous(t *testing.T) {
	backend := setupTestBackend(t)
	store := backend.AuthStore()
	ctx := context.Background()

	// Two DIFFERENT companies, SAME issuer, BOTH allowlist the domain, BOTH enabled.
	dom := "ambiguous-" + uuid.NewString()[:8] + ".example"
	for _, slug := range []string{"sso-jit-amb-a-" + uuid.NewString()[:8], "sso-jit-amb-b-" + uuid.NewString()[:8]} {
		cid := seedJITCompany(t, store, slug)
		if err := store.UpsertSSOConfig(ctx, auth.SSOConfig{
			CompanyID: cid, Provider: "oidc", IssuerURL: jitPolicyIssuer, IsActive: true,
			EmailDomainAllowlist: []string{dom}, JITDefaultRole: "manager", JITEnabled: true,
		}); err != nil {
			t.Fatalf("upsert %s: %v", slug, err)
		}
	}
	if _, _, err := store.ResolveJITCompanyByIssuerDomain(ctx, jitPolicyIssuer, dom); err != auth.ErrAmbiguousJITMatch {
		t.Errorf("ambiguous match err = %v, want ErrAmbiguousJITMatch (fail-secure)", err)
	}
}
