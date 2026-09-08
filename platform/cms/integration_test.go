package cms

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// This file is this objective's end-to-end proof that platform/cms's
// pieces -- Store (store.go), the publish lifecycle (publish.go), and
// preview tokens (preview_token.go) -- compose into the flow the whole
// package exists to support: a page is drafted, scheduled, readable
// through a preview token while still invisible to the public, becomes
// due, and finally readable on the public path with no token at all.
//
// It runs against memStore below, an in-memory Store, deliberately NOT
// PostgresStore. That is not a shortcut around a database: it is the
// direct proof of store.go's own claim that Store is an interface any
// consumer can implement, and that publish.go / preview_token.go never
// need to know which implementation they're composed with. See
// README.md's open gap on migration 018_cms_pages: it has never been
// applied against a live Postgres in this objective's execution history,
// and this test does not change that -- it proves the LIFECYCLE composes,
// not that PostgresStore's SQL is sound. That is store_pg_test.go's
// DATABASE_URL-gated job, not this file's.

// memStore is a minimal, tenant-scoped, in-memory Store implementation
// used ONLY by this test file. It honors the same contracts store.go
// documents for every method (default Status on create, ErrNotFound
// collapsing cross-tenant lookups, ErrSlugTaken on a per-tenant slug
// collision) so a test run against it exercises the real Store contract,
// not a loosened stand-in.
type memStore struct {
	mu    sync.Mutex
	pages map[uuid.UUID]Page
}

func newMemStore() *memStore {
	return &memStore{pages: make(map[uuid.UUID]Page)}
}

// Compile-time assertion that memStore satisfies Store, exactly like
// store_pg.go's assertion for PostgresStore.
var _ Store = (*memStore)(nil)

func (m *memStore) CreatePage(_ context.Context, p Page) (Page, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	if p.Status == "" {
		p.Status = PageStatusDraft
	}
	for _, existing := range m.pages {
		if existing.CompanyID == p.CompanyID && existing.Slug == p.Slug {
			return Page{}, ErrSlugTaken
		}
	}
	now := time.Now().UTC()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now
	m.pages[p.ID] = p
	return p, nil
}

func (m *memStore) GetPage(_ context.Context, companyID, id uuid.UUID) (Page, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.pages[id]
	if !ok || p.CompanyID != companyID {
		return Page{}, ErrNotFound
	}
	return p, nil
}

func (m *memStore) GetPageBySlug(_ context.Context, companyID uuid.UUID, slug string) (Page, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.pages {
		if p.CompanyID == companyID && p.Slug == slug {
			return p, nil
		}
	}
	return Page{}, ErrNotFound
}

func (m *memStore) ListPages(_ context.Context, companyID uuid.UUID) ([]Page, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Page, 0)
	for _, p := range m.pages {
		if p.CompanyID == companyID {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (m *memStore) UpdatePage(_ context.Context, p Page) (Page, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.pages[p.ID]
	if !ok || existing.CompanyID != p.CompanyID {
		return Page{}, ErrNotFound
	}
	for id, other := range m.pages {
		if id != p.ID && other.CompanyID == p.CompanyID && other.Slug == p.Slug {
			return Page{}, ErrSlugTaken
		}
	}
	p.CreatedAt = existing.CreatedAt
	p.UpdatedAt = time.Now().UTC()
	m.pages[p.ID] = p
	return p, nil
}

func (m *memStore) DeletePage(_ context.Context, companyID, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.pages[id]
	if !ok || p.CompanyID != companyID {
		return ErrNotFound
	}
	delete(m.pages, id)
	return nil
}

// TestLifecycle_DraftScheduleDuePreviewPublicRead is the required
// draft -> schedule -> preview-token read -> due -> public read proof.
func TestLifecycle_DraftScheduleDuePreviewPublicRead(t *testing.T) {
	ctx := context.Background()
	store := newMemStore()
	secret := []byte("integration-test-preview-secret")
	companyID := uuid.New()

	// ── draft ──────────────────────────────────────────────────────
	created, err := store.CreatePage(ctx, Page{
		CompanyID: companyID,
		Slug:      "about",
		Title:     "About Us",
		Blocks: []Block{
			{ID: "b1", Type: "bio", Data: map[string]any{"headline": "Hello"}},
		},
	})
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}
	if created.Status != PageStatusDraft {
		t.Fatalf("Status = %v, want %v (default)", created.Status, PageStatusDraft)
	}

	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	if created.VisibleAt(now) {
		t.Fatal("a draft page must not be publicly visible")
	}

	// ── schedule ───────────────────────────────────────────────────
	future := now.Add(2 * time.Hour)
	page := created
	if err := page.Schedule(future, now); err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if page.Status != PageStatusScheduled {
		t.Fatalf("Status = %v, want %v", page.Status, PageStatusScheduled)
	}

	scheduled, err := store.UpdatePage(ctx, page)
	if err != nil {
		t.Fatalf("UpdatePage (schedule): %v", err)
	}
	if scheduled.VisibleAt(now) {
		t.Fatal("a scheduled-but-not-due page must not be publicly visible")
	}

	// ── preview-token read (page is still not due) ────────────────
	token, expiresAt, err := MintPreviewToken(secret, scheduled.ID, companyID)
	if err != nil {
		t.Fatalf("MintPreviewToken: %v", err)
	}
	if !expiresAt.After(now) {
		t.Fatalf("expiresAt %v is not after mint time %v", expiresAt, now)
	}

	grant, err := Authorize(secret, token, scheduled.ID, companyID)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if grant.PageID != scheduled.ID || grant.CompanyID != companyID {
		t.Fatalf("grant scoped to (%s,%s), want (%s,%s)", grant.PageID, grant.CompanyID, scheduled.ID, companyID)
	}

	// A preview grant authorizes the VIEWER; the actual content read still
	// goes through the same tenant-scoped Store any other read would use.
	previewRead, err := store.GetPage(ctx, companyID, scheduled.ID)
	if err != nil {
		t.Fatalf("GetPage (preview): %v", err)
	}
	if previewRead.Title != "About Us" || len(previewRead.Blocks) != 1 || previewRead.Blocks[0].Type != "bio" {
		t.Fatalf("preview read returned unexpected content: %+v", previewRead)
	}

	// Wrong page/tenant scope must be refused, not silently widened.
	if _, err := Authorize(secret, token, uuid.New(), companyID); !errors.Is(err, ErrPreviewTokenScope) {
		t.Fatalf("Authorize wrong-page error = %v, want ErrPreviewTokenScope", err)
	}
	if _, err := Authorize(secret, token, scheduled.ID, uuid.New()); !errors.Is(err, ErrPreviewTokenScope) {
		t.Fatalf("Authorize wrong-tenant error = %v, want ErrPreviewTokenScope", err)
	}

	// ── due ────────────────────────────────────────────────────────
	dueNow := future.Add(1 * time.Minute)
	if !previewRead.DueForPublish(dueNow) {
		t.Fatal("page should be due for publish at dueNow")
	}
	didPublish, err := previewRead.PublishIfDue(dueNow)
	if err != nil {
		t.Fatalf("PublishIfDue: %v", err)
	}
	if !didPublish {
		t.Fatal("PublishIfDue returned false for a due page")
	}
	if previewRead.Status != PageStatusPublished {
		t.Fatalf("Status = %v, want %v", previewRead.Status, PageStatusPublished)
	}
	if previewRead.PublishedAt == nil {
		t.Fatal("PublishedAt was not set on first publish")
	}

	published, err := store.UpdatePage(ctx, previewRead)
	if err != nil {
		t.Fatalf("UpdatePage (publish): %v", err)
	}

	// ── public read (no token at all) ─────────────────────────────
	byID, err := store.GetPage(ctx, companyID, published.ID)
	if err != nil {
		t.Fatalf("GetPage (public, by id): %v", err)
	}
	if !byID.VisibleAt(dueNow) {
		t.Fatal("a due, published page must be publicly visible with no token")
	}

	bySlug, err := store.GetPageBySlug(ctx, companyID, "about")
	if err != nil {
		t.Fatalf("GetPageBySlug (public): %v", err)
	}
	if !bySlug.VisibleAt(dueNow) {
		t.Fatal("public read by slug must also report visible")
	}

	visible := FilterVisible([]Page{bySlug}, dueNow)
	if len(visible) != 1 {
		t.Fatalf("FilterVisible returned %d pages, want 1", len(visible))
	}
}

// TestLifecycle_CrossTenantReadCollapsesToNotFound proves the tenant-
// isolation property this package's docs repeat throughout: a page ID
// that is real, just under a DIFFERENT tenant, is indistinguishable from
// a nonexistent one -- both return ErrNotFound, never a leak.
func TestLifecycle_CrossTenantReadCollapsesToNotFound(t *testing.T) {
	ctx := context.Background()
	store := newMemStore()
	companyA := uuid.New()
	companyB := uuid.New()

	page, err := store.CreatePage(ctx, Page{CompanyID: companyA, Slug: "secret", Title: "Secret"})
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	if _, err := store.GetPage(ctx, companyB, page.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant GetPage error = %v, want ErrNotFound", err)
	}
}
