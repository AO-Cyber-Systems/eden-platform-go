-- Tenant-scoped CMS pages. blocks is the page's ordered content, stored
-- whole as a single JSONB array -- platform/cms.Block{ID,Type,Data} entries
-- in render order, marshaled/unmarshaled Go-side by
-- platform/cms/store_pg.go's PostgresStore (never validated or
-- special-cased here; Type and Data are opaque to this table). Storing the
-- array atomically, rather than one row per block, is what makes a reorder
-- a single UPDATE and keeps index position == render order without a
-- separate sort column to drift out of sync.
CREATE TABLE IF NOT EXISTS cms_pages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    slug TEXT NOT NULL,
    title TEXT NOT NULL,
    blocks JSONB NOT NULL DEFAULT '[]'::jsonb,
    status TEXT NOT NULL DEFAULT 'draft',
    scheduled_publish_at TIMESTAMPTZ,
    published_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_cms_pages_company
    ON cms_pages (company_id);

-- A slug is unique per tenant, not globally -- two companies may each run
-- their own "about" page. PostgresStore.CreatePage / UpdatePage map the
-- resulting 23505 to ErrSlugTaken.
CREATE UNIQUE INDEX uq_cms_pages_company_slug
    ON cms_pages (company_id, slug);
