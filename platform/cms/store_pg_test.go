package cms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"reflect"
	"testing"
	"time"

	edenplatform "github.com/aocybersystems/eden-platform-go"
	"github.com/aocybersystems/eden-platform-go/platform/pgstore"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// ── Unit tests: the JSONB marshal/unmarshal boundary, no database needed ──
//
// These exercise scanPage -- the EXACT function PostgresStore.GetPage /
// GetPageBySlug / ListPages call on a real *pgx.Row -- through a hand-built
// fakeRow, so the round-trip / ordering / unknown-key properties are proven
// against production code, not a reimplementation of it. A live Postgres is
// not required to prove any of this: JSONB storage is, on the read side,
// exactly "the bytes Postgres handed back get json.Unmarshal'd" -- there is
// no server-side transform in play that a fake row scan wouldn't also
// exercise faithfully at the boundary this package owns (encoding/json).

// fakeRow is a minimal rowScanner double. values must be typed EXACTLY as
// scanPage's Scan destinations expect (e.g. a *time.Time for a nullable
// timestamp column, never untyped nil) -- this mirrors how pgx itself hands
// back already-typed Go values to Scan.
type fakeRow struct {
	values []any
	err    error
}

func (f fakeRow) Scan(dest ...any) error {
	if f.err != nil {
		return f.err
	}
	if len(dest) != len(f.values) {
		return fmt.Errorf("fakeRow: %d scan targets, %d values", len(dest), len(f.values))
	}
	for i, d := range dest {
		dv := reflect.ValueOf(d)
		if dv.Kind() != reflect.Ptr {
			return fmt.Errorf("fakeRow: scan target %d is not a pointer: %T", i, d)
		}
		vv := reflect.ValueOf(f.values[i])
		if !vv.IsValid() {
			continue // nil interface value -- leave the destination at its zero value
		}
		if !vv.Type().AssignableTo(dv.Elem().Type()) {
			return fmt.Errorf("fakeRow: value %d of type %T not assignable to %s", i, f.values[i], dv.Elem().Type())
		}
		dv.Elem().Set(vv)
	}
	return nil
}

// newFakeRow builds a fakeRow with the 10 columns scanPage expects, in
// scanPage's exact column order.
func newFakeRow(id, companyID uuid.UUID, slug, title string, blocksJSON []byte, status string, scheduledAt, publishedAt *time.Time, createdAt, updatedAt time.Time) fakeRow {
	return fakeRow{values: []any{id, companyID, slug, title, blocksJSON, status, scheduledAt, publishedAt, createdAt, updatedAt}}
}

// TestScanPage_BlocksRoundTripByteStable proves the must_have: "Blocks
// persist as JSONB and round-trip byte-stable through save/load". It builds
// a []Block containing an UNKNOWN Data key (one no field of Block or any
// type in this package ever names -- exactly the shape a consumer's own
// evolving block schema produces), marshals it the same way
// PostgresStore.CreatePage / UpdatePage do, feeds those raw bytes through
// scanPage exactly as a real Postgres row would, and asserts the result is
// byte-for-byte the same value: nothing dropped, nothing renamed, nothing
// coerced.
func TestScanPage_BlocksRoundTripByteStable(t *testing.T) {
	want := []Block{
		{
			ID:   "b1",
			Type: "splash-banner-from-a-consumer-app", // opaque to this package on purpose
			Data: map[string]any{
				"headline": "Welcome",
				// unknownFutureField is a key NO version of platform/cms has ever
				// defined. A consumer's block schema evolves independently of
				// platform -- dropping this here would silently destroy content
				// platform/cms was never asked to understand in the first place.
				"unknownFutureField": "some-value-platform-cms-has-never-heard-of",
				"nested": map[string]any{
					"alsoUnknown": []any{"x", "y", float64(3)},
				},
			},
		},
	}
	blocksJSON, err := jsonMarshalForTest(want)
	if err != nil {
		t.Fatalf("marshal fixture blocks: %v", err)
	}

	id, companyID := uuid.New(), uuid.New()
	now := time.Date(2027, 3, 4, 5, 6, 7, 0, time.UTC)
	row := newFakeRow(id, companyID, "welcome", "Welcome", blocksJSON, string(PageStatusDraft), nil, nil, now, now)

	got, err := scanPage(row)
	if err != nil {
		t.Fatalf("scanPage: %v", err)
	}
	if !reflect.DeepEqual(got.Blocks, want) {
		t.Fatalf("Blocks round-trip mismatch:\n got  = %#v\n want = %#v", got.Blocks, want)
	}
	// Re-marshal what scanPage produced and diff the raw bytes against the
	// original JSON that was "stored" -- this is the byte-stability half of
	// the must_have, not just Go-struct equality.
	remarshaled, err := jsonMarshalForTest(got.Blocks)
	if err != nil {
		t.Fatalf("remarshal: %v", err)
	}
	if string(remarshaled) != string(blocksJSON) {
		t.Fatalf("re-marshaled JSON differs from stored JSON:\n stored = %s\n reread = %s", blocksJSON, remarshaled)
	}
}

// TestScanPage_OrderAndDuplicateTypesSurvive proves the must_have:
// "Ordering survives persistence -- a reordered page reloads in the new
// order", plus (per ParseBlocks' own contract) that two blocks sharing one
// Type but distinct IDs stay distinct and in-order across the round trip.
func TestScanPage_OrderAndDuplicateTypesSurvive(t *testing.T) {
	// Deliberately reordered vs. how a naive implementation might sort
	// (alphabetically by ID, or grouped by Type): banner, markdown, banner.
	// If storage/scan reordered or grouped, this would fail.
	want := []Block{
		{ID: "z-last-alphabetically", Type: "banner", Data: map[string]any{"slot": float64(1)}},
		{ID: "a-first-alphabetically", Type: "markdown", Data: map[string]any{"source": "# hi"}},
		{ID: "m-middle", Type: "banner", Data: map[string]any{"slot": float64(2)}}, // duplicate Type, distinct ID
	}
	blocksJSON, err := jsonMarshalForTest(want)
	if err != nil {
		t.Fatalf("marshal fixture blocks: %v", err)
	}

	id, companyID := uuid.New(), uuid.New()
	now := time.Now().UTC()
	row := newFakeRow(id, companyID, "ordered", "Ordered", blocksJSON, string(PageStatusPublished), nil, nil, now, now)

	got, err := scanPage(row)
	if err != nil {
		t.Fatalf("scanPage: %v", err)
	}
	if len(got.Blocks) != 3 {
		t.Fatalf("got %d blocks, want 3", len(got.Blocks))
	}
	for i, wb := range want {
		if got.Blocks[i].ID != wb.ID || got.Blocks[i].Type != wb.Type {
			t.Fatalf("block[%d] = {ID:%q Type:%q}, want {ID:%q Type:%q} -- order not preserved",
				i, got.Blocks[i].ID, got.Blocks[i].Type, wb.ID, wb.Type)
		}
	}
	if got.Blocks[0].ID == got.Blocks[2].ID {
		t.Fatal("two distinct banner blocks collapsed to the same ID")
	}
}

// TestScanPage_EmptyAndNullBlocks proves an empty/absent blocks array
// collapses to a nil (not panicking, not erroring) []Block, matching
// migration 018's `DEFAULT '[]'::jsonb`.
func TestScanPage_EmptyAndNullBlocks(t *testing.T) {
	id, companyID := uuid.New(), uuid.New()
	now := time.Now().UTC()

	row := newFakeRow(id, companyID, "empty", "Empty", []byte(`[]`), string(PageStatusDraft), nil, nil, now, now)
	got, err := scanPage(row)
	if err != nil {
		t.Fatalf("scanPage: %v", err)
	}
	if len(got.Blocks) != 0 {
		t.Fatalf("Blocks = %#v, want empty", got.Blocks)
	}

	rowNil := newFakeRow(id, companyID, "nilblocks", "Nil", nil, string(PageStatusDraft), nil, nil, now, now)
	gotNil, err := scanPage(rowNil)
	if err != nil {
		t.Fatalf("scanPage with nil blocksRaw: %v", err)
	}
	if gotNil.Blocks != nil {
		t.Fatalf("Blocks = %#v, want nil for a nil/absent JSONB value", gotNil.Blocks)
	}
}

// TestScanPage_TenantAndTimestampFieldsPreserved proves scanPage carries
// CompanyID, Status, and the nullable schedule/publish instants through
// untouched -- the must_have "Content is tenant-scoped by companyID
// uuid.UUID" depends on CompanyID never being dropped or swapped on read.
func TestScanPage_TenantAndTimestampFieldsPreserved(t *testing.T) {
	id, companyID := uuid.New(), uuid.New()
	created := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	updated := time.Date(2027, 1, 2, 0, 0, 0, 0, time.UTC)
	scheduled := time.Date(2027, 1, 3, 12, 0, 0, 0, time.UTC)

	row := newFakeRow(id, companyID, "sched", "Sched", []byte(`[]`), string(PageStatusScheduled), &scheduled, nil, created, updated)
	got, err := scanPage(row)
	if err != nil {
		t.Fatalf("scanPage: %v", err)
	}
	if got.ID != id {
		t.Errorf("ID = %v, want %v", got.ID, id)
	}
	if got.CompanyID != companyID {
		t.Errorf("CompanyID = %v, want %v", got.CompanyID, companyID)
	}
	if got.Status != PageStatusScheduled {
		t.Errorf("Status = %q, want %q", got.Status, PageStatusScheduled)
	}
	if got.ScheduledPublishAt == nil || !got.ScheduledPublishAt.Equal(scheduled) {
		t.Errorf("ScheduledPublishAt = %v, want %v", got.ScheduledPublishAt, scheduled)
	}
	if got.PublishedAt != nil {
		t.Errorf("PublishedAt = %v, want nil", got.PublishedAt)
	}
	if !got.CreatedAt.Equal(created) || !got.UpdatedAt.Equal(updated) {
		t.Errorf("CreatedAt/UpdatedAt = %v/%v, want %v/%v", got.CreatedAt, got.UpdatedAt, created, updated)
	}
}

// TestScanPage_ScanErrorPropagates proves a Scan failure (e.g. pgx.ErrNoRows
// on a genuinely missing row) surfaces to the caller rather than being
// swallowed -- GetPage/GetPageBySlug rely on this to map it to ErrNotFound.
func TestScanPage_ScanErrorPropagates(t *testing.T) {
	wantErr := errors.New("boom")
	_, err := scanPage(fakeRow{err: wantErr})
	if !errors.Is(err, wantErr) {
		t.Fatalf("scanPage error = %v, want %v", err, wantErr)
	}
}

// TestIsSlugConflict proves the Postgres 23505 (unique_violation) mapping
// PostgresStore.CreatePage / UpdatePage rely on to turn uq_cms_pages_company_slug
// into ErrSlugTaken, and that it does NOT fire on an unrelated Postgres error
// or a non-Postgres error.
func TestIsSlugConflict(t *testing.T) {
	if !isSlugConflict(&pgconn.PgError{Code: "23505", ConstraintName: "uq_cms_pages_company_slug"}) {
		t.Error("isSlugConflict(23505) = false, want true")
	}
	if isSlugConflict(&pgconn.PgError{Code: "23503"}) { // foreign_key_violation
		t.Error("isSlugConflict(23503) = true, want false")
	}
	if isSlugConflict(errors.New("not a pg error")) {
		t.Error("isSlugConflict(generic error) = true, want false")
	}
	if isSlugConflict(nil) {
		t.Error("isSlugConflict(nil) = true, want false")
	}
}

// TestPostgresStore_SatisfiesStore is a compile-time-adjacent runtime check
// that the must_have "Store is an interface; PostgresStore is one
// implementation" actually holds -- var _ Store = (*PostgresStore)(nil) in
// store_pg.go already enforces this at compile time; this test additionally
// documents it in the test output.
func TestPostgresStore_SatisfiesStore(t *testing.T) {
	var s Store = &PostgresStore{}
	if s == nil {
		t.Fatal("PostgresStore{} does not satisfy Store")
	}
}

// jsonMarshalForTest is a tiny indirection so the fixture-building code
// above reads plainly; it is exactly encoding/json.Marshal.
func jsonMarshalForTest(v any) ([]byte, error) {
	return json.Marshal(v)
}

// ── Integration tests: real Postgres, DATABASE_URL-gated ──────────────────
//
// These exercise the full CreatePage -> GetPage/GetPageBySlug/ListPages ->
// UpdatePage -> DeletePage path against an actual cms_pages table (migration
// 018), through an actual *pgxpool.Pool -- the unit tests above cannot
// reach the SQL text, the unique index, or RunMigrations itself. They are
// skipped (not failed) when DATABASE_URL is unset, matching
// platform/pgstore's own integration-test convention (see
// platform/telephony/config_store_test.go, pgstore_test.go).
//
// NOT independently verified in this TRD's execution run: no DATABASE_URL
// was available in the executing environment, so this suite has not
// actually been run against a live database as part of this change. See
// 41-02-SUMMARY.md.

func setupStoreTest(t *testing.T) (*pgstore.Backend, *pgstore.AuthStore) {
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

func mustCreateCompanyForCMS(t *testing.T, authStore *pgstore.AuthStore, namePrefix string) uuid.UUID {
	t.Helper()
	slug := namePrefix + "-" + uuid.NewString()[:8]
	id, err := authStore.CreateCompany(context.Background(), slug, slug, "standalone")
	if err != nil {
		t.Fatalf("create company %s: %v", slug, err)
	}
	return id
}

// TestPostgresStore_CreateGetRoundTrip_Integration proves the full save ->
// load path through real Postgres, including the unknown-Data-key
// preservation property, end to end.
func TestPostgresStore_CreateGetRoundTrip_Integration(t *testing.T) {
	backend, authStore := setupStoreTest(t)
	ctx := context.Background()
	store := NewPostgresStore(backend.Pool())

	companyID := mustCreateCompanyForCMS(t, authStore, "cms-roundtrip")
	blocks := []Block{
		{ID: "1", Type: "banner", Data: map[string]any{"unknownKey": "still here"}},
		{ID: "2", Type: "markdown", Data: map[string]any{"source": "# hi"}},
	}
	created, err := store.CreatePage(ctx, Page{
		CompanyID: companyID,
		Slug:      "about-" + uuid.NewString()[:8],
		Title:     "About",
		Blocks:    blocks,
	})
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	got, err := store.GetPage(ctx, companyID, created.ID)
	if err != nil {
		t.Fatalf("GetPage: %v", err)
	}
	if !reflect.DeepEqual(got.Blocks, blocks) {
		t.Fatalf("Blocks after save/load = %#v, want %#v", got.Blocks, blocks)
	}
}

// TestPostgresStore_ReorderPersists_Integration proves the must_have
// "Ordering survives persistence" through a real UpdatePage -> GetPage
// cycle.
func TestPostgresStore_ReorderPersists_Integration(t *testing.T) {
	backend, authStore := setupStoreTest(t)
	ctx := context.Background()
	store := NewPostgresStore(backend.Pool())

	companyID := mustCreateCompanyForCMS(t, authStore, "cms-reorder")
	original := []Block{
		{ID: "a", Type: "banner", Data: map[string]any{}},
		{ID: "b", Type: "markdown", Data: map[string]any{}},
	}
	created, err := store.CreatePage(ctx, Page{
		CompanyID: companyID,
		Slug:      "reorder-" + uuid.NewString()[:8],
		Title:     "Reorder",
		Blocks:    original,
	})
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	reordered := []Block{original[1], original[0]}
	created.Blocks = reordered
	if _, err := store.UpdatePage(ctx, created); err != nil {
		t.Fatalf("UpdatePage: %v", err)
	}

	got, err := store.GetPage(ctx, companyID, created.ID)
	if err != nil {
		t.Fatalf("GetPage after reorder: %v", err)
	}
	if got.Blocks[0].ID != "b" || got.Blocks[1].ID != "a" {
		t.Fatalf("Blocks after reorder = %#v, want [b, a]", got.Blocks)
	}
}

// TestPostgresStore_TenantScoping_Integration proves a page created under
// one company is invisible (ErrNotFound, not a cross-tenant read) to
// another company.
func TestPostgresStore_TenantScoping_Integration(t *testing.T) {
	backend, authStore := setupStoreTest(t)
	ctx := context.Background()
	store := NewPostgresStore(backend.Pool())

	companyA := mustCreateCompanyForCMS(t, authStore, "cms-tenant-a")
	companyB := mustCreateCompanyForCMS(t, authStore, "cms-tenant-b")

	page, err := store.CreatePage(ctx, Page{CompanyID: companyA, Slug: "secret-" + uuid.NewString()[:8], Title: "Secret"})
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}
	if _, err := store.GetPage(ctx, companyB, page.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetPage across tenants: got %v, want ErrNotFound", err)
	}
	if err := store.DeletePage(ctx, companyB, page.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeletePage across tenants: got %v, want ErrNotFound", err)
	}
	// The page must still exist for its real owner.
	if _, err := store.GetPage(ctx, companyA, page.ID); err != nil {
		t.Fatalf("owner GetPage after cross-tenant no-op attempts: %v", err)
	}
}

// TestPostgresStore_SlugConflict_Integration proves ErrSlugTaken fires on a
// same-tenant slug collision and NOT across tenants.
func TestPostgresStore_SlugConflict_Integration(t *testing.T) {
	backend, authStore := setupStoreTest(t)
	ctx := context.Background()
	store := NewPostgresStore(backend.Pool())

	companyA := mustCreateCompanyForCMS(t, authStore, "cms-slug-a")
	companyB := mustCreateCompanyForCMS(t, authStore, "cms-slug-b")
	slug := "dup-" + uuid.NewString()[:8]

	if _, err := store.CreatePage(ctx, Page{CompanyID: companyA, Slug: slug, Title: "First"}); err != nil {
		t.Fatalf("first CreatePage: %v", err)
	}
	if _, err := store.CreatePage(ctx, Page{CompanyID: companyA, Slug: slug, Title: "Second"}); !errors.Is(err, ErrSlugTaken) {
		t.Fatalf("same-tenant duplicate slug: got %v, want ErrSlugTaken", err)
	}
	// A DIFFERENT tenant may use the same slug -- slugs are unique per
	// company, not globally.
	if _, err := store.CreatePage(ctx, Page{CompanyID: companyB, Slug: slug, Title: "Other tenant"}); err != nil {
		t.Fatalf("cross-tenant same slug should succeed: %v", err)
	}
}

// TestPostgresStore_DeletePage_Integration proves Delete removes the row
// and a subsequent Get collapses to ErrNotFound.
func TestPostgresStore_DeletePage_Integration(t *testing.T) {
	backend, authStore := setupStoreTest(t)
	ctx := context.Background()
	store := NewPostgresStore(backend.Pool())

	companyID := mustCreateCompanyForCMS(t, authStore, "cms-delete")
	page, err := store.CreatePage(ctx, Page{CompanyID: companyID, Slug: "gone-" + uuid.NewString()[:8], Title: "Gone"})
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}
	if err := store.DeletePage(ctx, companyID, page.ID); err != nil {
		t.Fatalf("DeletePage: %v", err)
	}
	if _, err := store.GetPage(ctx, companyID, page.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetPage after delete: got %v, want ErrNotFound", err)
	}
	if err := store.DeletePage(ctx, companyID, page.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second DeletePage: got %v, want ErrNotFound", err)
	}
}

// TestPostgresStore_ListPages_Integration proves ListPages returns only the
// requesting tenant's pages.
func TestPostgresStore_ListPages_Integration(t *testing.T) {
	backend, authStore := setupStoreTest(t)
	ctx := context.Background()
	store := NewPostgresStore(backend.Pool())

	companyA := mustCreateCompanyForCMS(t, authStore, "cms-list-a")
	companyB := mustCreateCompanyForCMS(t, authStore, "cms-list-b")

	if _, err := store.CreatePage(ctx, Page{CompanyID: companyA, Slug: "one-" + uuid.NewString()[:8], Title: "One"}); err != nil {
		t.Fatalf("CreatePage A1: %v", err)
	}
	if _, err := store.CreatePage(ctx, Page{CompanyID: companyA, Slug: "two-" + uuid.NewString()[:8], Title: "Two"}); err != nil {
		t.Fatalf("CreatePage A2: %v", err)
	}
	if _, err := store.CreatePage(ctx, Page{CompanyID: companyB, Slug: "b-only-" + uuid.NewString()[:8], Title: "B only"}); err != nil {
		t.Fatalf("CreatePage B1: %v", err)
	}

	pagesA, err := store.ListPages(ctx, companyA)
	if err != nil {
		t.Fatalf("ListPages A: %v", err)
	}
	if len(pagesA) != 2 {
		t.Fatalf("ListPages A returned %d pages, want 2", len(pagesA))
	}
	for _, p := range pagesA {
		if p.CompanyID != companyA {
			t.Fatalf("ListPages A leaked a page from company %v", p.CompanyID)
		}
	}
}
