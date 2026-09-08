package telephony

// optout_store_test.go has no source equivalent in politihub — its
// phone_optout_store.go likewise has no _test.go file (matching
// config_store_test.go's own header note on the same point). This file is
// therefore net-new coverage for this TRD, written against the DATABASE_URL
// / pgstore.NewBackend convention config_store_test.go already established
// in this package: setupConfigStoreTest and mustCreateCompany are reused
// unchanged.
//
// NOTE for whoever writes the next DB-backed test in this package: the
// DATABASE_URL these tests read must use the "pgx5://" scheme, not
// "postgres://" — platform/pgstore/migrate.go registers ONLY the pgx/v5
// golang-migrate driver via blank import. A "postgres://" URL fails with
// "unknown driver postgres (forgotten import?)" at backend construction,
// which reads like a missing dependency rather than a URL scheme mismatch.

import (
	"context"
	"testing"
)

// --- normalizePhone: pure unit tests, no DB required -------------------

func TestNormalizePhone(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"+12075550142", "+12075550142"},
		{"(207) 555-0142", "+12075550142"},
		{"207-555-0142", "+12075550142"},
		{"207.555.0142", "+12075550142"},
		{"2075550142", "+12075550142"},
		{"+1 207 555 0142", "+12075550142"},
		{"", ""},
		// Not a NANP-shaped 10-digit number and no leading '+' -- passed
		// through as bare digits rather than guessed at. This is the
		// documented limitation in optout_store.go's normalizePhone comment:
		// idempotent against itself, not a validator.
		{"5550142", "5550142"},
		// A '+' that isn't in the leading position is stripped, not
		// preserved -- only a leading '+' survives. The remaining 10 digits
		// still hit the bare-10-digit NANP branch, so this normalizes
		// identically to the other formattings of the same number above.
		{"207+555+0142", "+12075550142"},
	}
	for _, c := range cases {
		got := normalizePhone(c.in)
		if got != c.want {
			t.Errorf("normalizePhone(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestNormalizePhone_FormattingVariantsCollide is the regression politihub's
// phone_optout_store.go documented as a production incident: a phone stored
// in one format and queried in another must still match. This is exactly
// the bug class Go-side, single-definition normalization (see
// optout_store.go's header comment) is meant to close.
func TestNormalizePhone_FormattingVariantsCollide(t *testing.T) {
	variants := []string{"+12075550142", "(207) 555-0142", "207-555-0142", "2075550142"}
	first := normalizePhone(variants[0])
	for _, v := range variants[1:] {
		if got := normalizePhone(v); got != first {
			t.Errorf("normalizePhone(%q) = %q, want %q (same number as %q)", v, got, first, variants[0])
		}
	}
}

// --- PostgresPhoneOptOutStore: DATABASE_URL-gated integration tests ----

func TestPostgresPhoneOptOutStore_UpsertIsOptedOutRoundTrip(t *testing.T) {
	backend, authStore := setupConfigStoreTest(t)
	store := NewPostgresPhoneOptOutStore(backend.Pool())
	ctx := context.Background()

	company := mustCreateCompany(t, authStore, "optout-roundtrip")
	phone := "+12075550101"

	if optedOut, err := store.IsOptedOut(ctx, company, phone); err != nil {
		t.Fatalf("IsOptedOut before Upsert: %v", err)
	} else if optedOut {
		t.Fatal("expected not opted out before any Upsert")
	}

	if err := store.Upsert(ctx, company, phone, string(MatchReasonExactKeyword), string(ProviderTwilio), "STOP"); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	optedOut, err := store.IsOptedOut(ctx, company, phone)
	if err != nil {
		t.Fatalf("IsOptedOut after Upsert: %v", err)
	}
	if !optedOut {
		t.Fatal("expected opted out after Upsert")
	}
}

// TestPostgresPhoneOptOutStore_UpsertIsIdempotent proves a second STOP from
// a different spelling updates the same row rather than erroring or
// duplicating -- the unique index is on (company_id, phone_normalized), not
// a raw phone string.
func TestPostgresPhoneOptOutStore_UpsertIsIdempotent(t *testing.T) {
	backend, authStore := setupConfigStoreTest(t)
	store := NewPostgresPhoneOptOutStore(backend.Pool())
	ctx := context.Background()

	company := mustCreateCompany(t, authStore, "optout-idempotent")

	if err := store.Upsert(ctx, company, "+12075550102", string(MatchReasonExactKeyword), string(ProviderTwilio), "STOP"); err != nil {
		t.Fatalf("first Upsert: %v", err)
	}
	// Same number, differently formatted, different reason -- must update
	// the SAME row, not insert a second one (which would trip the unique
	// index and fail this test outright).
	if err := store.Upsert(ctx, company, "(207) 555-0102", string(MatchReasonFuzzyPhrase), string(ProviderSignalWire), "please stop"); err != nil {
		t.Fatalf("second Upsert (re-upsert): %v", err)
	}

	optedOut, err := store.IsOptedOut(ctx, company, "2075550102")
	if err != nil {
		t.Fatalf("IsOptedOut: %v", err)
	}
	if !optedOut {
		t.Fatal("expected opted out after re-upsert with a differently-formatted phone")
	}
}

// TestPostgresPhoneOptOutStore_NormalizationAcrossFormats is the direct
// Postgres-store analogue of politihub's measured production bug: Upsert
// stores one format, IsOptedOut is queried with another, and the row must
// still be found. politihub's version depended on a SQL function shipping
// in a separate migration; this asserts the Go-side normalizePhone (used on
// both the write and read path) closes the same gap without that
// dependency.
func TestPostgresPhoneOptOutStore_NormalizationAcrossFormats(t *testing.T) {
	backend, authStore := setupConfigStoreTest(t)
	store := NewPostgresPhoneOptOutStore(backend.Pool())
	ctx := context.Background()

	company := mustCreateCompany(t, authStore, "optout-normalize")

	if err := store.Upsert(ctx, company, "+12075550103", string(MatchReasonExactKeyword), string(ProviderTwilio), "STOP"); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	for _, queryPhone := range []string{"(207) 555-0103", "207-555-0103", "2075550103", "207.555.0103"} {
		optedOut, err := store.IsOptedOut(ctx, company, queryPhone)
		if err != nil {
			t.Fatalf("IsOptedOut(%q): %v", queryPhone, err)
		}
		if !optedOut {
			t.Errorf("IsOptedOut(%q) = false, want true (same number as the +12075550103 that was upserted)", queryPhone)
		}
	}
}

// TestPostgresPhoneOptOutStore_Remove proves the START / UNSTOP path: a
// phone opted out via Upsert is no longer opted out after Remove, and
// Remove on a phone with no opt-out row is a no-op, not an error.
func TestPostgresPhoneOptOutStore_Remove(t *testing.T) {
	backend, authStore := setupConfigStoreTest(t)
	store := NewPostgresPhoneOptOutStore(backend.Pool())
	ctx := context.Background()

	company := mustCreateCompany(t, authStore, "optout-remove")
	phone := "+12075550104"

	// Remove with no existing row: no-op, no error.
	if err := store.Remove(ctx, company, phone); err != nil {
		t.Fatalf("Remove on a phone with no opt-out row: %v", err)
	}

	if err := store.Upsert(ctx, company, phone, string(MatchReasonExactKeyword), string(ProviderTwilio), "STOP"); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if optedOut, _ := store.IsOptedOut(ctx, company, phone); !optedOut {
		t.Fatal("expected opted out after Upsert")
	}

	if err := store.Remove(ctx, company, phone); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if optedOut, err := store.IsOptedOut(ctx, company, phone); err != nil {
		t.Fatalf("IsOptedOut after Remove: %v", err)
	} else if optedOut {
		t.Fatal("expected not opted out after Remove")
	}

	// The row must be re-insertable after Remove (proves Remove actually
	// DELETEs rather than merely masking the row some other way -- a
	// leftover row would trip the unique index on the next Upsert).
	if err := store.Upsert(ctx, company, phone, string(MatchReasonExactKeyword), string(ProviderTwilio), "STOP"); err != nil {
		t.Fatalf("Upsert after Remove: %v", err)
	}
	if optedOut, _ := store.IsOptedOut(ctx, company, phone); !optedOut {
		t.Fatal("expected opted out again after re-upsert following Remove")
	}
}

// TestPostgresPhoneOptOutStore_ScopedPerCompany proves an opt-out in one
// company does not leak into another -- the unique index and every query
// are scoped on (company_id, phone_normalized).
func TestPostgresPhoneOptOutStore_ScopedPerCompany(t *testing.T) {
	backend, authStore := setupConfigStoreTest(t)
	store := NewPostgresPhoneOptOutStore(backend.Pool())
	ctx := context.Background()

	companyA := mustCreateCompany(t, authStore, "optout-scope-a")
	companyB := mustCreateCompany(t, authStore, "optout-scope-b")
	phone := "+12075550105"

	if err := store.Upsert(ctx, companyA, phone, string(MatchReasonExactKeyword), string(ProviderTwilio), "STOP"); err != nil {
		t.Fatalf("Upsert companyA: %v", err)
	}

	if optedOut, err := store.IsOptedOut(ctx, companyA, phone); err != nil || !optedOut {
		t.Fatalf("companyA IsOptedOut: optedOut=%v err=%v, want true/nil", optedOut, err)
	}
	if optedOut, err := store.IsOptedOut(ctx, companyB, phone); err != nil || optedOut {
		t.Fatalf("companyB IsOptedOut: optedOut=%v err=%v, want false/nil (opt-out must not leak across companies)", optedOut, err)
	}
}

// TestPostgresPhoneOptOutStore_EmptyPhoneIsNoop matches Upsert's documented
// zero-value guard: an empty phone must never be written.
func TestPostgresPhoneOptOutStore_EmptyPhoneIsNoop(t *testing.T) {
	backend, authStore := setupConfigStoreTest(t)
	store := NewPostgresPhoneOptOutStore(backend.Pool())
	ctx := context.Background()

	company := mustCreateCompany(t, authStore, "optout-empty")

	if err := store.Upsert(ctx, company, "", string(MatchReasonExactKeyword), string(ProviderTwilio), "STOP"); err != nil {
		t.Fatalf("Upsert with empty phone: %v", err)
	}
	if optedOut, err := store.IsOptedOut(ctx, company, ""); err != nil || optedOut {
		t.Fatalf("IsOptedOut with empty phone: optedOut=%v err=%v, want false/nil", optedOut, err)
	}
}
