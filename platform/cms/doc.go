// Package cms owns block-based content and its lifecycle for a
// multi-tenant platform: an ordered list of opaque [Block] values that make
// up a [Page], persisted per tenant through [Store], carried through
// draft -> scheduled -> published -> archived by the methods on *Page in
// publish.go, gated pre-publish by a bearer [PreviewGrant] token, plus a
// per-tenant [Settings] envelope and a [MediaPipeline] for derived image
// renditions.
//
// # The one decision this package is built around
//
// [Block.Type] is a plain string, and this package never inspects,
// validates, or special-cases its value. The catalog of what a Type MEANS
// -- "bio", "callout", "practitioner_grid", "membership_grid", whatever a
// given product's editor and renderer agree on -- belongs entirely to the
// consumer, not to this package.
//
// This is not an oversight; it is the load-bearing design call the whole
// objective rests on. Four products were surveyed while building this
// package (see MIGRATION.md), and their block vocabularies do not
// converge: justinforme ships "bio" and "blog" and "callout" blocks;
// smartWellness ships "practitioner_grid" and "membership_grid" blocks with
// no analog in justinforme's catalog, and vice versa. Unifying the catalog
// was considered and rejected -- it is the one thing guaranteed to keep
// diverging as new products and new block kinds show up, so baking any
// vocabulary into this shared package would either constrain every future
// consumer to today's catalog or force an every-vertical superset that
// serves no one well. Keeping Type opaque is what lets ONE engine -- this
// package -- serve every one of them: the engine is shared, the catalog is
// not.
//
// # Layering: this package stops at []Block
//
// This package's job ends at producing an ordered []Block per Page. It
// carries NO rendering concerns anywhere in it -- no HTML, no templates, no
// component registry, no CSS (see settings.go's BrandTokens, which is data,
// never emitted style). Turning a []Block into a rendered page is a
// separate layer's job entirely: a renderer maps each Block.Type to a
// component and interprets that Block's Data. eden-web (a sibling module in
// the eden-libs repo) is the recommended renderer for that job today, via
// its `stories` Registry keyed by component name -- but this package has no
// import of eden-web, does not know it exists, and never will: see
// README.md's "Layering" section and this TRD's own must-haves for why that
// dependency arrow is deliberately never drawn. The CONSUMER wires a
// []Block producer (this package) to a renderer (eden-web, or anything
// else) -- this package supplies one side of that seam only.
//
// # What is NOT in this package
//
// Deliberately out of scope for V1, and not silently missing -- named here
// and in MIGRATION.md so a future TRD extends this package on purpose
// rather than by accident: a builder/admin UI, static-build-and-deploy
// (e.g. a Cloudflare Pages pipeline), AI content generation, analytics, and
// any one consumer's own block catalog. See MIGRATION.md for exactly where
// each of those currently lives.
//
// # Two open gaps, recorded rather than hidden
//
// Settings (settings.go) has no Postgres-backed store in this package as
// shipped -- [SettingsStore] is the seam; [MemorySettingsStore] is the only
// implementation that exists today, and it does not survive a process
// restart. Migration 018 (cms_pages, the table [PostgresStore] reads and
// writes) has never been applied against a live Postgres in this
// objective's own execution history -- its apply/rollback correctness rests
// on hand-review against migrations 016/017's style plus DATABASE_URL-gated
// integration tests that report SKIP, not PASS, whenever no database is
// reachable. Both gaps are detailed in README.md and MIGRATION.md; neither
// should be assumed closed by this package's presence.
package cms
