---
objective: 41
name: platform/cms
date: 2026-09-07
status: complete
---

# Objective 41 — Research: `platform/cms`

## Question

Four products ship a block CMS. What is extractable, and what is not?

## Source inventory — and why this is NOT a telephony-style lift

| Source | Impl LOC | Character |
|---|---|---|
| `eden-biz/go/internal/website` | **46,097** | Full website builder: block schema, brand CSS, catalog blocks, build artifacts, Cloudflare-Pages client, analytics, golden render tests |
| `justinforme/app/cms` | 8,772 | Complete block CMS: blocks, render, icons, image pipeline, preview tokens, scheduler, site settings |
| `smartWellness/app/cms` | 4,288 | Same machinery + design tokens, membership, nav_items, practitioner, video |
| `politihub/go/internal/website_builder` | 681 | Thin: AI client, handler, models, service, store |

**~60,000 LOC total.** Objective 40 could say "lift politihub wholesale" because one source
was complete and small. Nothing of the sort is true here. This objective must **scope by
layer**, not by source.

## The finding that makes this tractable

`eden-biz/go/internal/website/block_schema.go:47` already has the correct model:

```go
type Block struct {
    ID   string         `json:"id"`
    Type string         `json:"type"`
    Data map[string]any `json:"data"`
}
```

`ParseBlocks` is **tolerant by construction**: absent/non-array blocks → empty, a block
with no `type` → skipped, malformed `data` → nil map. Order preserved, duplicate types
with distinct ids all survive. One bad block never takes down its siblings.

`Type` is an opaque string and `Data` is opaque JSON. That single decision dissolves the
problem below.

## Why §18's "nearly identical" needed qualifying

CAPABILITIES.md §18 says justinforme and smartWellness are "nearly identical". Verified:
that is true of the **machinery** — both ship `blocks.go`, `image_service.go`,
`preview_token.go`, `public_handler.go`, `scheduler.go`, `service.go`, `store.go` almost
file-for-file.

It is **not** true of the block vocabulary:

| justinforme kinds | smartWellness kinds |
|---|---|
| accordion, announcement_bar, bio, blog, bullet_list, button_row, callout | cta_strip, event_list, faq, gallery, hero, markdown, membership_grid, pillar_grid, practitioner_card, practitioner_grid |

These are different products' domain vocabularies and will never converge. With eden-biz's
opaque-`Type` model they don't have to: **the engine is shared, the catalog is not.**

That is the central scoping decision of this objective.

## Layering — and the eden-web relationship

eden-libs already owns the presentation layer: **`eden-web`** (Obj 34) with `render/`,
`engine/`, `ssg/`, and a `stories` component `Registry` (`Register`/`All`/`ByComponent`).

Two distinct layers, and they must stay distinct:

- **Content model + lifecycle** (this objective) — blocks, pages, ordering, scheduled
  publish, preview tokens, media, brand settings.
- **Presentation** (`eden-web`, already shipped) — components, themes, SSG.

**Hard constraint:** `platform/cms` lives in eden-platform-go; `eden-web` lives in
eden-libs as a *separate Go module*. `platform/cms` MUST NOT import eden-web — that would
create a cross-repo module dependency between two independently released repos. The CMS
**emits `[]Block`**; the consumer chooses a renderer, with eden-web the recommended one.
The seam is `Type string` → component name.

## Verified: Obj 35 never happened

`eden-biz/go/go.mod` has **no eden-web dependency**. Objective 35 ("eden-biz
`internal/website` cutover onto eden-web") was scoped in eden-libs STATE.md as a follow-up
to Obj 34 and never planned. eden-biz's 46k-LOC builder is still entirely its own.

This objective does **not** do that cutover, but it is a prerequisite for doing it sanely:
once the block model and lifecycle are platform-owned, Obj 35 becomes "point eden-biz's
renderer at eden-web" rather than "rewrite 46k LOC".

## Scope decision

**V1 — the engine and the lifecycle:**
block model (eden-biz's tolerant `{ID,Type,Data}`), page/content model with ordering,
store seam + Postgres implementation, scheduled publishing, preview tokens, media/image
pipeline, site/brand settings.

**Explicitly deferred — and where each currently lives:**

| Deferred | Lives in |
|---|---|
| Builder / admin UI | eden-biz `builder.go`, `builder_statics` |
| Build artifacts + Cloudflare-Pages deploy | eden-biz `build_artifact.go`, `cf_pages_client` |
| AI content generation | politihub `ai_client.go` (see §18 `eden-ai-content`) |
| Analytics | eden-biz `analytics.go` |
| Domain block catalogs | each consumer keeps its own |
| Rendering | `eden-web`, already shipped |

## Pitfalls

- **Do not unify the block catalog.** It is the one thing guaranteed to diverge. Keep
  `Type` opaque.
- **Preserve tolerant parsing.** eden-biz's skip-don't-fail behaviour is a published-site
  availability property: one malformed block must not blank a page.
- **Preview tokens are an auth surface.** A preview token grants read access to unpublished
  content — it needs expiry and scoping, and it must not be guessable.
- **Scheduled publish is a correctness trap.** Timezone handling and "publish at" vs
  "published" state must be explicit, or content leaks early or never appears.
- **eden-biz is the most advanced source but the worst starting point** — its 46k LOC is
  fused to its builder and deploy pipeline. Take its *model*, take justinforme's
  *machinery*.
