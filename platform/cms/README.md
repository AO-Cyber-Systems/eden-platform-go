# platform/cms

Block-based content and its lifecycle, for any tenant, for any product's own catalog of
block kinds. This package owns content and lifecycle; it does not own rendering.

## The decision everything else here follows from

`Block.Type` is a plain string, and this package never looks inside it. The catalog of
what a `Type` *means* — `"bio"`, `"callout"`, `"practitioner_grid"`, `"membership_grid"`,
whatever a product's editor and renderer agree on — belongs to the consumer, not to this
package.

That is not a gap. Four products were read while building this package (see
`MIGRATION.md`): justinforme ships `bio` / `blog` / `callout` blocks; smartWellness ships
`practitioner_grid` / `membership_grid` blocks with no equivalent in justinforme's catalog,
and vice versa. Those vocabularies do not converge, and they never will — a wellness
practice and a personal blog do not want the same block kinds, and a fifth product adopting
this package tomorrow will want a catalog neither of those two anticipated. Unifying the
catalog into this package was considered and rejected for exactly that reason: it is the
one thing guaranteed to keep diverging.

So the catalog stays with the consumer, and this package stays generic:

- **The engine is shared.** Content model (`Block`, `Page`), ordering, per-tenant storage,
  the draft → scheduled → published → archived lifecycle, preview tokens, media
  derivation, and site/brand settings — one implementation, used by every consumer.
- **The catalog is not.** What `bio` renders as, what `practitioner_grid`'s `Data` shape
  is, how many block kinds a product ships — each consumer's own concern, and this package
  has an explicit tolerant-parse contract (`ParseBlocks`, see below) specifically so an
  unrecognized or malformed `Type` never takes a whole page down.

## Layering: platform/cms stops at `[]Block`

This package's job ends the moment it hands back an ordered `[]Block`. It carries no
rendering concerns anywhere in it — no HTML, no templates, no component registry. Turning
`[]Block` into an actual rendered page is a **separate layer's job**: a renderer maps each
`Block.Type` to a component and interprets that block's `Data`.

```
platform/cms  →  []Block  →  (the seam: Type string → component name)  →  a renderer
```

**eden-web** (a sibling package in the `eden-libs` repo) is the recommended renderer for
that job — it already ships a `stories` component `Registry` keyed by component name, which
is exactly the shape this seam wants. But that recommendation is a documentation fact, not
a code fact:

> **`platform/cms` has NO dependency on eden-web.** This package lives in `eden-platform-go`
> and is versioned/released independently of `eden-libs`, where eden-web lives as its own Go
> module. Importing eden-web from here would couple two separately-released repos'
> release cycles together — a change to eden-web's `stories` package could not land without
> also bumping and re-vendoring it into this repo, and vice versa. **The dependency arrow is
> never drawn.** The CONSUMER — the service or app that has both `platform/cms` and a
> renderer in its dependency graph — is the only place that wire the two together.

Verify this holds at any time with:

```bash
go list -deps ./platform/cms | grep eden-web   # must print nothing
```

(`go.mod` for this module has no `eden-libs` or `eden-web` entry at all — there is nothing
to accidentally import.)

## What's in this package

| File | Owns |
|---|---|
| `block.go` | `Block{ID, Type, Data}`; `ParseBlocks` — tolerant projection of a decoded JSON `"blocks"` array into `[]Block` |
| `page.go` | `Page`; `PageStatus` (draft / scheduled / published / archived) |
| `store.go` | `Store` — the tenant-scoped persistence interface (`ErrNotFound`, `ErrSlugTaken`) |
| `store_pg.go` | `PostgresStore` — the production `Store`, backed by migration `018_cms_pages` |
| `publish.go` | The lifecycle: `Page.Schedule` / `Unschedule` / `PublishNow` / `Archive` / `DueForPublish` / `PublishIfDue` / `VisibleAt`; `FilterVisible` |
| `preview_token.go` | `MintPreviewToken` / `VerifyPreviewToken` / `Authorize` — a self-verifying, no-storage bearer credential scoped to one page and one tenant |
| `settings.go` | `Settings` (identity, `Nav`, `BrandTokens`) — per-tenant site config as data; `SettingsStore` + `MemorySettingsStore` |
| `media.go` | `MediaPipeline` — derives size-bounded image renditions through `platform/storage` |

## Content: `Block` and tolerant parsing

```go
type Block struct {
	ID   string
	Type string         // opaque — see above
	Data map[string]any  // opaque — block-specific payload
}
```

A `Block`'s position in its containing `[]Block` **is** its render order. `ParseBlocks`
projects a decoded `extra["blocks"]` JSON value into `[]Block` and is **tolerant by
construction**, ported from eden-biz's `block_schema.go` semantics: an absent/non-array
`"blocks"` key yields an empty list (never an error); a non-object array element is
skipped, never panics; an object with no (or non-string) `"type"` is skipped, since an
untyped block is unrenderable. **One malformed block is dropped so its siblings still
render.**

This is deliberately an availability property, not a style choice: a bad row in a database,
or a stray element a future editor UI wrote incorrectly, must never blank an entire
published page for every visitor. `FilterVisible` and the rest of the public read path
build on top of blocks that already survived this filter.

## Lifecycle: draft → scheduled → published → archived

`Page.Status` moves through four states, validated and transitioned by plain methods on
`*Page` (not a generic state-machine engine — see publish.go's own doc comment for why
`platform/statemachine` was considered and rejected: `Page.Status` is one field on the
`Page` row `Store` already owns as a whole, and composing `statemachine.Machine` here would
mean a second, parallel store tracking the same fact).

- **`Schedule(at, now)`** — draft or scheduled → scheduled. `at` is normalized to UTC before
  it's stored; `ScheduledPublishAt` is always an absolute UTC instant, never a wall-clock
  time in an arbitrary zone (a zone mismatch here is a content-leak risk).
- **`PublishNow(now)`** — draft or scheduled → published immediately. `PublishedAt` is set
  only the *first* time a page is ever published; a later archive-then-republish keeps the
  original `PublishedAt`.
- **`Archive(now)`** — published → archived. Withdraws from the public read path without
  deleting the row.
- **`DueForPublish(now)` / `PublishIfDue(now)`** — has a scheduled page's instant passed?
  An optional reconciliation worker MAY call `PublishIfDue` to flip `Status` for admin-UI
  display, but nothing about correctness depends on that worker having run.
- **`VisibleAt(now)`** — the actual public-read-path gate. Compares `now` to
  `ScheduledPublishAt` directly rather than trusting `Status` alone, so a page becomes
  visible the *instant* it is due even if no worker has run yet, and a not-yet-due
  scheduled page stays invisible even if `Status` bookkeeping is stale.

This design deliberately does **not** port the donor implementations' polling `Scheduler`
that leases and flips due rows in the background — `VisibleAt`'s direct time comparison
makes that machinery unnecessary for correctness, and it is out of this package's file
list.

## Preview tokens are a bearer credential

A preview token grants **time-bounded, unauthenticated read access to exactly one page in
exactly one tenant** — the way an editor previews a draft or scheduled page before it's
public. Treat it exactly like any other bearer credential:

- `MintPreviewToken(secret, pageID, companyID)` — HMAC-SHA256-signed over a
  `crypto/rand` nonce plus `(pageID, companyID, expiresAt)`. The nonce means two tokens for
  the *same* page/tenant/expiry are never identical — this package's tokens are not a pure
  deterministic function of secret+payload the way the donor implementations' were.
  Default TTL is `PreviewTokenTTL` (30 minutes), short by design: a preview URL pasted into
  a chat or a Slack link is now a bounded-lifetime leak, not a permanent one.
- `Authorize(secret, token, pageID, companyID)` — verifies signature (constant-time
  compare, `crypto/subtle`) and expiry, **and** enforces that the token was minted for
  exactly this page and tenant. A token scoped to page A presented for page B — or a token
  for tenant X presented against tenant Y's page — is refused with `ErrPreviewTokenScope`,
  not silently honored.
- **No server-side storage.** The token is self-verifying; there is no revocation-before-
  expiry and no way to list currently-live preview links, because there is no store backing
  it. If a consumer needs either of those, that requires a persisted grants table this
  package does not define today — a deliberate, visible gap (see preview_token.go's own doc
  comment), not a silent one.
- **Consumer's responsibility:** transport the token safely. This package makes the token
  itself hard to forge or reuse past its expiry; it does nothing to stop a consumer from,
  say, logging the full preview URL including the token in a query string. Treat a minted
  preview token exactly as you would any other bearer credential in transit.

## Storage: `Store` is the seam, not the destination

```go
type Store interface {
	CreatePage(ctx, p Page) (Page, error)
	GetPage(ctx, companyID, id uuid.UUID) (Page, error)
	GetPageBySlug(ctx, companyID uuid.UUID, slug string) (Page, error)
	ListPages(ctx, companyID uuid.UUID) ([]Page, error)
	UpdatePage(ctx, p Page) (Page, error)
	DeletePage(ctx, companyID, id uuid.UUID) error
}
```

Every method is tenant-scoped by `companyID` **in the query predicate itself**, not as a
post-hoc filter — a lookup for a real page ID belonging to a *different* tenant collapses to
the same `ErrNotFound` a nonexistent ID gets, so a caller can never confirm another tenant's
page exists by probing IDs. `PostgresStore` (migration `018_cms_pages`) is the production
implementation this package ships; blocks persist as one JSONB array column per page (not
one row per block), so a reorder is a single `UPDATE` and array index stays the single
source of render order with no separate sort column to drift.

`Store` being an interface is not an abstraction exercise — a consumer can substitute an
in-memory fake for tests (`platform/cms/integration_test.go` in this package does exactly
that to prove the lifecycle without a live database) or an entirely different backend,
without `publish.go` / `preview_token.go` ever needing to know which.

## Settings: per-tenant identity, navigation, and brand tokens — as data

`Settings` carries a tenant's site identity (name, contact, social links, tagline, footer
copy), navigation tree (`NavItem`), and brand tokens (`BrandTokens`: open `Colors` /
`TypeRoles` / `Spacing` maps plus a `LogoURL`). All of it is **data**. This file emits no
CSS, no HTML, nothing visual — turning `BrandTokens` into an actual stylesheet or turning
`Nav` into an actual `<nav>` element is, again, a renderer's job, not this package's.
`DefaultSettings(companyID)` is what a tenant with no saved row gets — never an error, so a
zero-configuration tenant is still fully renderable.

`ValidateHexColor` / `ValidateLogoURL` are policy-free value guards a caller may run before
persisting a token value; `Settings`/`BrandTokens` themselves never call them, so writing an
unvalidated value is not itself blocked by this package.

## Media: bounded image derivation, one blob path

`MediaPipeline.DeriveVariants` turns one uploaded image into a configurable set of
size-bounded renditions (`DefaultVariants`: `original` / `thumb` / `display`), storing each
through an injected `platform/storage.Client` — this package never talks to an object-store
SDK or a filesystem directly, and never derives video (donor repos' `video_service.go` is
explicitly not ported; see MIGRATION.md). Untrusted bytes are checked — size, then content
type, then `image.DecodeConfig`'s *reported* dimensions against `MaxMediaDimension` — before
the memory-allocating `image.Decode` call ever runs, so a crafted "decompression bomb"
header fails before it can exhaust memory. Derivation is idempotent and content-addressed:
the same `(companyID, sha256(source bytes), variant name)` always yields the same storage
key, so re-processing the same upload never creates a second copy.

## Two open gaps — read before you assume either is closed

**1. Settings has no Postgres-backed store.** `SettingsStore` is the seam (`GetSettings` /
`UpsertSettings`), and `MemorySettingsStore` is the only implementation shipped in this
package. It works correctly and is fully tested — but it is **in-process memory**: nothing
written through it survives a restart, and nothing written on one process instance is
visible to another. This is not an oversight of this final TRD; the TRD that shipped
`settings.go` (41-05) did not own `migrations/`, so it had no seam to add a
`cms_site_settings` table. **Do not assume settings persist** until a
`PostgresSettingsStore` and its migration exist. See MIGRATION.md.

**2. Migration `018_cms_pages` has never been applied against a live Postgres in this
objective's history.** The TRD that authored it (41-02) had a live Postgres torn down
mid-task before the apply/rollback check ran, and every later TRD in this objective
(including this one) was explicitly told not to provision database infrastructure. What
exists instead: the SQL was hand-reviewed line-by-line against migrations `016`/`017`'s
already-applied style, and `store_pg_test.go` ships `DATABASE_URL`-gated integration tests
(`TestPostgresStore_*_Integration`) that exercise the full CRUD + reorder + tenant-isolation
path against a real table — but with `DATABASE_URL` unset, as it is in every run of this
objective so far, those tests report **SKIP, not PASS**. Say "not verified against a live
database" — do not read a green `go test` run as proof the migration applies and rolls back
cleanly. The next time a `DATABASE_URL` pointed at a disposable Postgres is available,
running `go test ./platform/cms/... -count=1` with it set exercises this for real with no
code changes required.

## Adopting this package

1. Wire a `Store` — `PostgresStore` against a Postgres with migration `018_cms_pages`
   applied, or your own implementation.
2. Define your own block catalog (the `Type` strings your editor and renderer agree on) —
   this package will never define one for you.
3. Wire a renderer that maps `Block.Type` → component and interprets `Block.Data`. eden-web
   is the recommended one; it is not a dependency of this package and must be wired by you.
4. If you need previews, pick a signing secret and call `MintPreviewToken` /
   `Authorize` around your preview route.
5. If you need persisted settings today, you'll need to add a `PostgresSettingsStore` — see
   the open gap above — or accept `MemorySettingsStore`'s in-process-only semantics for now.

See `MIGRATION.md` for how each of the four surveyed products maps onto this package, and
what each one still has to build or keep on its own.
