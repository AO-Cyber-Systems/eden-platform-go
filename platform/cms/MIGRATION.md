# Migrating onto platform/cms

This objective surveyed four products that each ship their own block-based CMS, to build
one shared engine underneath all of them. **This document does not perform any of those
migrations.** No consumer's code changes as a result of this objective; nothing here has
been cut over. What follows is what each source looks like today, what maps directly onto
`platform/cms`, and what stays outside this package on purpose.

## The four sources

| Source | Impl LOC | What it ships |
|---|---:|---|
| `eden-biz/go/internal/website` | 46,097 | Full website builder: block schema, brand CSS, catalog blocks, build artifacts, a Cloudflare Pages client, analytics, golden render tests |
| `justinforme/app/cms` | 8,772 | Complete block CMS: blocks, render, icons, image pipeline, preview tokens, a scheduler, site settings |
| `smartWellness/app/cms` | 4,288 | Same machinery as justinforme, plus design tokens, membership, `nav_items`, practitioner blocks, video |
| `politihub/go/internal/website_builder` | 681 | Thin: an AI client, a handler, models, a service, a store |

~60,000 LOC total across all four. None of them is migrated by this objective. What this
objective did was read all four, take the *shared* model and lifecycle out of them, and
leave every consumer's *own* catalog and product-specific machinery exactly where it is.

## What came from where

- **Block model** — `eden-biz/go/internal/website/block_schema.go`'s `Block{ID, Type,
  Data}` and its tolerant `ParseBlocks` (skip malformed elements, never fail the whole
  parse) is the most advanced of the four and was ported close to verbatim as
  `platform/cms.Block` / `platform/cms.ParseBlocks`.
- **Content/lifecycle machinery** — justinforme's `app/cms` is the most complete
  self-contained implementation (blocks + render + icons + image pipeline + preview tokens
  + scheduler + site settings, all in one product) and was the primary shape reference for
  `Page`, the publish state machine, and preview tokens — see README.md's per-file
  breakdown for exactly what was kept and what was deliberately changed (e.g. preview
  tokens gained a `crypto/rand` nonce that justinforme's deterministic-signature design
  didn't have).
- **Settings reconciliation** — `Settings` (identity + `Nav` + `BrandTokens`) is a genuine
  three-way merge of justinforme's `site_settings.go` (flat key/value identity fields),
  smartWellness's `nav_items.go` (navigation data shape, not its CRUD business rules) and
  `design.go` (per-token design rows, regrouped into three named buckets), and eden-biz's
  `brand.go` (the hex-color and logo-URL validators, generalized; its curated four-font
  allowlist was explicitly *not* ported — see 41-05-SUMMARY.md's full reconciliation
  writeup for the disagreement-by-disagreement account).
- **eden-biz is the most advanced source and the worst starting point.** Its 46k LOC is the
  richest model of the four, but it is fused end-to-end to its own builder UI and its own
  Cloudflare Pages deploy pipeline — pulling any of that machinery in would have pulled the
  deploy pipeline in with it. `platform/cms` took eden-biz's *block model*, not its
  *machinery*.
- **politihub** contributed the least code (681 LOC, the thinnest of the four) but is named
  explicitly below because its AI-generation client is a deferred piece, not because its
  CMS machinery informed this package's shape.

## What each product still has to build or keep

Nothing here is ported. This is a map of where each capability lives *today*, for a future
migration to consult — not a to-do list this objective executed.

| Deferred capability | Currently lives in |
|---|---|
| Builder / admin UI | `eden-biz` `builder.go`, `builder_statics` |
| Static build artifacts + Cloudflare Pages deploy | `eden-biz` `build_artifact.go`, `cf_pages_client` |
| AI content generation | `politihub` `ai_client.go` |
| Analytics | `eden-biz` `analytics.go` |
| Video derivation | `smartWellness` `video_service.go` |
| Each product's own block catalog | stays with that product — `platform/cms.Block.Type` is opaque by design, see README.md |
| Rendering (mapping `Type` → a component) | `eden-web` (eden-libs, already shipped as Obj 34) — the recommended renderer, wired by the consumer, never imported by `platform/cms` itself |

None of these six items is a bug or an oversight in this package. Each is either (a)
product-specific policy that has no single correct shared shape (the block catalog, AI
generation, analytics), (b) deploy/build tooling that is a different concern from content
and lifecycle (the builder UI, CF Pages), or (c) capability explicitly out of this
objective's V1 scope and named as such from the start (video — smartWellness's
`video_service.go` derives video renditions the way `platform/cms/media.go` derives image
renditions, but this package's `MediaPipeline` handles images only; a consumer needing
video derivation builds its own pipeline or a later TRD extends this one deliberately).

## What this unblocks — without performing it

`eden-biz/go/go.mod` has no dependency on eden-web today. Objective 35 — "cut
`eden-biz/internal/website` over to render through eden-web instead of its own 46k-LOC
builder" — was scoped as a follow-up to Objective 34 (the objective that shipped eden-web)
and **was never planned**. It does not appear as a completed or in-progress objective
anywhere in eden-libs' own state tracking.

**This objective (41) is a prerequisite for Obj 35, and does not perform it.** Before this
package existed, cutting eden-biz over to eden-web meant rewriting all 46,097 lines of
`internal/website` at once — content model, lifecycle, storage, and rendering all
entangled together in one migration. With `platform/cms` now owning the content model and
lifecycle as a separate, shared, already-tested package, a future Obj 35 becomes
materially smaller: point eden-biz's *rendering* at eden-web while eden-biz keeps (or
migrates onto `platform/cms`, if it also chooses to adopt the content/lifecycle layer)
whatever content model it's running. Nothing about that migration — full or partial — has
happened here. No eden-biz file was modified by this objective. No eden-biz code imports
`platform/cms`. This document exists to make the unblocked-but-unperformed state of Obj 35
explicit, not to claim credit for a migration that has not started.

## What "adopting platform/cms" would actually involve, for a future migration

Not performed here — recorded so a future TRD has a starting checklist:

1. Map the product's existing block rows onto `platform/cms.Block{ID, Type, Data}` — for
   all four sources this is a near-direct fit, since `platform/cms.Block` was modeled
   directly on eden-biz's shape.
2. Point page CRUD at `platform/cms.Store` (apply migration `018_cms_pages`, or supply a
   different `Store` implementation) — replacing whatever the product's own storage layer
   does today.
3. Replace any home-grown draft/scheduled/published logic with `Page`'s `Schedule` /
   `PublishNow` / `Archive` / `VisibleAt` methods (publish.go).
4. Replace any home-grown preview-link mechanism with `MintPreviewToken` / `Authorize`.
5. Keep the product's own block *catalog* (its `Type` vocabulary and its renderer) —
   `platform/cms` never defines one, so there is nothing to migrate here except wherever the
   renderer currently reads page/block data from.
6. Keep whatever the product currently uses for the six deferred capabilities in the table
   above — none of them move.
7. **Do not assume `Settings` persists** if adopting `settings.go` — see README.md's open
   gap #1 (no `PostgresSettingsStore` ships in this package yet).
8. **Do not assume migration `018_cms_pages` has been proven against a live database** — see
   README.md's open gap #2. Verify it in the target environment before depending on it in
   production.
