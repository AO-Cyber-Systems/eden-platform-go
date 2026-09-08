package telephony

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"testing"

	edenplatform "github.com/aocybersystems/eden-platform-go"
	"github.com/aocybersystems/eden-platform-go/platform/pgstore"
	"github.com/google/uuid"
)

// config_store_test.go has NO source equivalent -- politihub's telephony
// package has a config_store.go but no config_store_test.go (verified: `find
// politihub/go/internal/telephony -iname '*config_store*'` returns only the
// implementation file). This file is therefore net-new coverage written for
// this TRD, not a port.
//
// DATABASE_URL-gated (skipped when unset), matching platform/pgstore's own
// integration-test convention (see pgstore_test.go / sso_jit_policy_test.go).
// Slugs and sending numbers are suffixed with a fresh uuid per test so tests
// can share a persistent database without a truncate-between-tests step.

func setupConfigStoreTest(t *testing.T) (*pgstore.Backend, *pgstore.AuthStore) {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping integration tests")
	}
	migrationsFS, err := fs.Sub(edenplatform.MigrationsFS, "migrations/platform")
	if err != nil {
		t.Fatalf("sub migrations fs: %v", err)
	}
	backend, err := pgstore.NewBackend(context.Background(), dbURL, migrationsFS)
	if err != nil {
		t.Fatalf("create backend: %v", err)
	}
	t.Cleanup(backend.Close)
	return backend, backend.AuthStore()
}

func mustCreateCompany(t *testing.T, authStore *pgstore.AuthStore, namePrefix string) uuid.UUID {
	t.Helper()
	slug := namePrefix + "-" + uuid.NewString()[:8]
	id, err := authStore.CreateCompany(context.Background(), slug, slug, "standalone")
	if err != nil {
		t.Fatalf("create company %s: %v", slug, err)
	}
	return id
}

// TestPostgresConfigStore_GetNotConfigured proves a company with no row at
// all collapses to ErrTenantNotConfigured, not a raw scan error.
func TestPostgresConfigStore_GetNotConfigured(t *testing.T) {
	backend, authStore := setupConfigStoreTest(t)
	store := NewPostgresConfigStore(backend.Pool(), nil)
	ctx := context.Background()

	companyID := mustCreateCompany(t, authStore, "cfg-none")
	_, err := store.Get(ctx, companyID)
	if !errors.Is(err, ErrTenantNotConfigured) {
		t.Fatalf("Get on unconfigured company: got %v, want ErrTenantNotConfigured", err)
	}
}

// TestPostgresConfigStore_UpsertGetRoundTrip_CiphertextAtRest is the
// must_have: "a stored credential is encrypted at rest via
// platform/encryption.FieldEncryptor -- NOT vendored crypto" AND that it
// round-trips. It writes with the real FieldEncrypter (not NopEncrypter),
// reads the raw column back out with a plain SQL query bypassing the store
// entirely, and asserts the on-disk bytes are neither the plaintext nor
// human-readable, before proving Get() decrypts it back to the original
// plaintext through platform/encryption.
func TestPostgresConfigStore_UpsertGetRoundTrip_CiphertextAtRest(t *testing.T) {
	backend, authStore := setupConfigStoreTest(t)
	ctx := context.Background()

	enc, err := NewFieldEncrypter(testKey())
	if err != nil {
		t.Fatalf("NewFieldEncrypter: %v", err)
	}
	store := NewPostgresConfigStore(backend.Pool(), enc)

	companyID := mustCreateCompany(t, authStore, "cfg-ciphertext")
	const plainAuth = "AC_super_secret_auth_token"
	const plainSecret = "whsec_super_secret_webhook_key"
	number := "+15550001111"

	cfg := Config{
		CompanyID:     companyID,
		Provider:      ProviderSignalWire,
		AccountSID:    "AC_test",
		AuthToken:     plainAuth,
		WebhookSecret: plainSecret,
		SpaceURL:      "https://example.signalwire.com",
		SendingNumber: number,
	}
	if err := store.Upsert(ctx, cfg); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	// Bypass the store: read the raw encrypted columns directly.
	var authRaw, secretRaw []byte
	err = backend.Pool().QueryRow(ctx, `
		SELECT auth_token_encrypted, webhook_secret_encrypted
		  FROM tenant_telephony_config
		 WHERE company_id = $1 AND is_active = true`, companyID,
	).Scan(&authRaw, &secretRaw)
	if err != nil {
		t.Fatalf("raw select: %v", err)
	}
	if string(authRaw) == plainAuth {
		t.Error("auth_token_encrypted equals the plaintext -- credential is NOT encrypted at rest")
	}
	if string(secretRaw) == plainSecret {
		t.Error("webhook_secret_encrypted equals the plaintext -- credential is NOT encrypted at rest")
	}
	// AES-GCM ciphertext (nonce || sealed box) is materially longer than the
	// plaintext it wraps -- confirm this isn't merely re-encoded plaintext.
	if len(authRaw) <= len(plainAuth) {
		t.Errorf("auth_token_encrypted length %d <= plaintext length %d -- does not look like AES-GCM output", len(authRaw), len(plainAuth))
	}

	// Now prove the round trip through platform/encryption via Get().
	got, err := store.Get(ctx, companyID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AuthToken != plainAuth {
		t.Errorf("AuthToken round-trip: got %q, want %q", got.AuthToken, plainAuth)
	}
	if got.WebhookSecret != plainSecret {
		t.Errorf("WebhookSecret round-trip: got %q, want %q", got.WebhookSecret, plainSecret)
	}
	if got.Provider != ProviderSignalWire {
		t.Errorf("Provider = %q, want %q", got.Provider, ProviderSignalWire)
	}
	if got.SendingNumber != number {
		t.Errorf("SendingNumber = %q, want %q", got.SendingNumber, number)
	}
	if !got.IsActive {
		t.Error("IsActive = false, want true")
	}

	// Decrypting the SAME raw bytes directly through the FieldEncrypter used
	// to write them must also reproduce the plaintext -- proves the store
	// genuinely delegates to platform/encryption rather than something
	// store-internal.
	directlyDecrypted, err := enc.Decrypt(authRaw)
	if err != nil {
		t.Fatalf("direct FieldEncrypter.Decrypt of the on-disk bytes: %v", err)
	}
	if directlyDecrypted != plainAuth {
		t.Errorf("direct platform/encryption decrypt: got %q, want %q", directlyDecrypted, plainAuth)
	}
}

// TestPostgresConfigStore_UpsertDeactivatesPriorRow proves only one row per
// company is ever active -- a second Upsert supersedes, not duplicates.
func TestPostgresConfigStore_UpsertDeactivatesPriorRow(t *testing.T) {
	backend, authStore := setupConfigStoreTest(t)
	ctx := context.Background()
	store := NewPostgresConfigStore(backend.Pool(), NopEncrypter{})

	companyID := mustCreateCompany(t, authStore, "cfg-supersede")
	first := Config{
		CompanyID: companyID, Provider: ProviderTwilio, AccountSID: "AC_first",
		AuthToken: "tok1", SendingNumber: "+15550002222",
	}
	if err := store.Upsert(ctx, first); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	second := Config{
		CompanyID: companyID, Provider: ProviderTwilio, AccountSID: "AC_second",
		AuthToken: "tok2", SendingNumber: "+15550003333",
	}
	if err := store.Upsert(ctx, second); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	got, err := store.Get(ctx, companyID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AccountSID != "AC_second" {
		t.Errorf("AccountSID = %q, want %q (second upsert should supersede first)", got.AccountSID, "AC_second")
	}

	var activeCount int
	err = backend.Pool().QueryRow(ctx, `
		SELECT count(*) FROM tenant_telephony_config
		 WHERE company_id = $1 AND is_active = true`, companyID,
	).Scan(&activeCount)
	if err != nil {
		t.Fatalf("count active rows: %v", err)
	}
	if activeCount != 1 {
		t.Errorf("active row count = %d, want 1", activeCount)
	}
}

// TestPostgresConfigStore_LookupBySendingNumber is the must_have:
// "LookupBySendingNumber(provider, number) maps an inbound webhook back to
// its tenant". Two companies with DIFFERENT numbers must each resolve to
// their OWN Config, and an unclaimed number must collapse to
// ErrTenantNotConfigured (no cross-tenant existence leak).
func TestPostgresConfigStore_LookupBySendingNumber(t *testing.T) {
	backend, authStore := setupConfigStoreTest(t)
	ctx := context.Background()
	store := NewPostgresConfigStore(backend.Pool(), NopEncrypter{})

	companyA := mustCreateCompany(t, authStore, "cfg-lookup-a")
	companyB := mustCreateCompany(t, authStore, "cfg-lookup-b")
	numberA := "+15550004444"
	numberB := "+15550005555"

	if err := store.Upsert(ctx, Config{
		CompanyID: companyA, Provider: ProviderSignalWire, AccountSID: "AC_a",
		AuthToken: "tok-a", SendingNumber: numberA,
	}); err != nil {
		t.Fatalf("upsert company A: %v", err)
	}
	if err := store.Upsert(ctx, Config{
		CompanyID: companyB, Provider: ProviderSignalWire, AccountSID: "AC_b",
		AuthToken: "tok-b", SendingNumber: numberB,
	}); err != nil {
		t.Fatalf("upsert company B: %v", err)
	}

	got, err := store.LookupBySendingNumber(ctx, ProviderSignalWire, numberA)
	if err != nil {
		t.Fatalf("LookupBySendingNumber(numberA): %v", err)
	}
	if got.CompanyID != companyA {
		t.Fatalf("LookupBySendingNumber(numberA) resolved to company %v, want %v", got.CompanyID, companyA)
	}
	if got.AccountSID != "AC_a" {
		t.Errorf("AccountSID = %q, want %q", got.AccountSID, "AC_a")
	}

	got2, err := store.LookupBySendingNumber(ctx, ProviderSignalWire, numberB)
	if err != nil {
		t.Fatalf("LookupBySendingNumber(numberB): %v", err)
	}
	if got2.CompanyID != companyB {
		t.Fatalf("LookupBySendingNumber(numberB) resolved to company %v, want %v", got2.CompanyID, companyB)
	}

	// Wrong provider for a real number -> not found (provider is part of the key).
	if _, err := store.LookupBySendingNumber(ctx, ProviderTwilio, numberA); !errors.Is(err, ErrTenantNotConfigured) {
		t.Errorf("LookupBySendingNumber(wrong provider): got %v, want ErrTenantNotConfigured", err)
	}

	// Unclaimed number -> ErrTenantNotConfigured, no existence leak.
	if _, err := store.LookupBySendingNumber(ctx, ProviderSignalWire, "+15559998888"); !errors.Is(err, ErrTenantNotConfigured) {
		t.Errorf("LookupBySendingNumber(unclaimed): got %v, want ErrTenantNotConfigured", err)
	}
}

// TestPostgresConfigStore_FromNumberConflictIsRejected carries over the
// from-number conflict semantics reconciled from
// eden-biz/telephonycreds/from_number_conflict_test.go
// (TestSetTelephonyCreds_FromNumberClaimedByAnotherCompanyIsRejected):
// politihub's original config_store.go had NO such guard -- a second
// committee could silently claim a sending_number another committee already
// used, and LookupBySendingNumber's "LIMIT 1" with no ORDER BY would then
// resolve inbound traffic to whichever row Postgres happened to return.
// Migration 016's partial unique index closes that gap, and Upsert maps the
// resulting 23505 to ErrSendingNumberClaimed so the second company's write
// fails closed instead of silently corrupting inbound routing.
func TestPostgresConfigStore_FromNumberConflictIsRejected(t *testing.T) {
	backend, authStore := setupConfigStoreTest(t)
	ctx := context.Background()
	store := NewPostgresConfigStore(backend.Pool(), NopEncrypter{})

	companyA := mustCreateCompany(t, authStore, "cfg-conflict-a")
	companyB := mustCreateCompany(t, authStore, "cfg-conflict-b")
	sharedNumber := "+15550006666"

	if err := store.Upsert(ctx, Config{
		CompanyID: companyA, Provider: ProviderTwilio, AccountSID: "AC_owner",
		AuthToken: "tok-owner", SendingNumber: sharedNumber,
	}); err != nil {
		t.Fatalf("company A claims the number: %v", err)
	}

	err := store.Upsert(ctx, Config{
		CompanyID: companyB, Provider: ProviderTwilio, AccountSID: "AC_claimer",
		AuthToken: "tok-claimer", SendingNumber: sharedNumber,
	})
	if !errors.Is(err, ErrSendingNumberClaimed) {
		t.Fatalf("company B claiming A's number: got %v, want ErrSendingNumberClaimed", err)
	}

	// The inbound resolver must still map the number to company A ONLY --
	// deterministic, never ambiguous -- and company B's rejected write must
	// not have altered company A's row.
	got, err := store.LookupBySendingNumber(ctx, ProviderTwilio, sharedNumber)
	if err != nil {
		t.Fatalf("LookupBySendingNumber after rejected conflict: %v", err)
	}
	if got.CompanyID != companyA {
		t.Fatalf("shared number resolved to company %v, want the OWNING company A %v", got.CompanyID, companyA)
	}
	if got.AccountSID != "AC_owner" {
		t.Errorf("AccountSID = %q, want %q (company A's row must be untouched)", got.AccountSID, "AC_owner")
	}

	// Company B must remain unconfigured -- its rejected Upsert must not
	// have left a dangling active row under a different number, and it
	// certainly must not have claimed A's number.
	if _, err := store.Get(ctx, companyB); !errors.Is(err, ErrTenantNotConfigured) {
		t.Errorf("company B Get after rejected conflict: got %v, want ErrTenantNotConfigured", err)
	}

	// Deactivating company A and having company B retry must now succeed --
	// the conflict is about ACTIVE ownership, not a permanent number lock.
	if err := store.Deactivate(ctx, companyA); err != nil {
		t.Fatalf("deactivate company A: %v", err)
	}
	if err := store.Upsert(ctx, Config{
		CompanyID: companyB, Provider: ProviderTwilio, AccountSID: "AC_claimer",
		AuthToken: "tok-claimer", SendingNumber: sharedNumber,
	}); err != nil {
		t.Fatalf("company B claims the number after A deactivates: %v", err)
	}
	got2, err := store.LookupBySendingNumber(ctx, ProviderTwilio, sharedNumber)
	if err != nil {
		t.Fatalf("LookupBySendingNumber after B claims freed number: %v", err)
	}
	if got2.CompanyID != companyB {
		t.Fatalf("shared number resolved to company %v, want company B %v now that A is deactivated", got2.CompanyID, companyB)
	}
}

// TestPostgresConfigStore_DeactivateAndList proves Deactivate removes a
// company from both Get and List, and that List never decrypts secrets.
func TestPostgresConfigStore_DeactivateAndList(t *testing.T) {
	backend, authStore := setupConfigStoreTest(t)
	ctx := context.Background()
	store := NewPostgresConfigStore(backend.Pool(), NopEncrypter{})

	companyID := mustCreateCompany(t, authStore, "cfg-deactivate")
	number := "+15550007777"
	if err := store.Upsert(ctx, Config{
		CompanyID: companyID, Provider: ProviderTwilio, AccountSID: "AC_deact",
		AuthToken: "secret-tok", WebhookSecret: "secret-whsec", SendingNumber: number,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	all, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	found := false
	for _, c := range all {
		if c.CompanyID == companyID {
			found = true
			if c.AuthToken != "" {
				t.Errorf("List() decrypted AuthToken (got %q); List must never decrypt secrets", c.AuthToken)
			}
			if c.WebhookSecret != "" {
				t.Errorf("List() decrypted WebhookSecret (got %q); List must never decrypt secrets", c.WebhookSecret)
			}
		}
	}
	if !found {
		t.Fatalf("List() did not include the newly upserted active company %v", companyID)
	}

	if err := store.Deactivate(ctx, companyID); err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	if _, err := store.Get(ctx, companyID); !errors.Is(err, ErrTenantNotConfigured) {
		t.Errorf("Get after deactivate: got %v, want ErrTenantNotConfigured", err)
	}
	allAfter, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list after deactivate: %v", err)
	}
	for _, c := range allAfter {
		if c.CompanyID == companyID {
			t.Fatalf("List() after Deactivate still includes company %v", companyID)
		}
	}
}

// TestNopEncrypter_PassesThrough documents NopEncrypter's contract directly
// (the dev/test-only path where "encryption" is the identity function).
func TestNopEncrypter_PassesThrough(t *testing.T) {
	var enc Encrypter = NopEncrypter{}
	ciphertext, err := enc.Encrypt("plain")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if string(ciphertext) != "plain" {
		t.Errorf("NopEncrypter.Encrypt = %q, want %q (pass-through)", ciphertext, "plain")
	}
	got, err := enc.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if got != "plain" {
		t.Errorf("NopEncrypter.Decrypt = %q, want %q", got, "plain")
	}
}
