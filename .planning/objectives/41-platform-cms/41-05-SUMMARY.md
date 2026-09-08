---
objective: 41-platform-cms
job: 05
subsystem: cms
tags: [go, multi-tenant, cms, site-settings, brand-tokens, in-memory-store]

# Dependency graph
requires:
  - objective: 41-platform-cms (TRD 01)
    provides: Page/Block data model conventions (tolerant-parse style, tenant-scoped by companyID)
  - objective: 41-platform-cms (TRD 02)
    provides: Store interface pattern (store.go) this TRD's SettingsStore seam mirrors
provides:
  - "Settings: per-tenant site identity + navigation + brand tokens, as data"
  - "SocialLinks, NavItem, BrandTokens data types"
  - "DefaultSettings(companyID): documented zero-configuration defaults"
  - "SettingsStore interface + MemorySettingsStore reference implementation"
  - "ValidateHexColor / ValidateLogoURL policy-free value guards"
affects: [platform-cms-media (41-04, sibling), any future renderer/admin-API TRD consuming Settings]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "SettingsStore seam defined without a Postgres implementation when migrations/ is out of file-ownership scope (same precedent as preview_token.go's no-store design)"
    - "Open token maps (Colors/TypeRoles/Spacing) instead of fixed named fields, for tenant-extensible brand data"

key-files:
  created: [platform/cms/settings.go, platform/cms/settings_test.go]
  modified: []

key-decisions:
  - "Reconciled three donor shapes (justinforme site_settings.go, smartWellness design.go/nav_items.go, eden-biz brand.go) into one typed Settings struct -- see Reconciliation section below"
  - "No PostgresSettingsStore shipped -- migrations/ is outside this TRD's file-ownership scope; MemorySettingsStore proves the round-trip/defaults contract instead"
  - "Renamed the donor's marketing-tagline field to Tagline (not the donor's more vertical-specific name) to keep the platform package vertical-agnostic per the objective's naming gate"

patterns-established:
  - "Brand tokens as open name->value maps (Colors/TypeRoles/Spacing) rather than fixed struct fields, so any tenant/vertical can define its own token vocabulary without a schema change"

requirements-completed: [R47]

# Verification evidence
verification:
  gates_defined: 3
  gates_passed: 3
  auto_fix_cycles: 0
  tdd_evidence: false
  test_pairing: true

# Metrics
duration: ~35min
completed: 2026-09-08
---

# Objective 41 TRD 05: Site Settings + Brand Tokens Summary

**Per-tenant `Settings` struct (identity, navigation, brand tokens) with `DefaultSettings` covering the no-row case, a `SettingsStore` seam, and an in-memory reference implementation — reconciled from three separate donor codebases, zero CSS emitted.**

## Performance

- **Duration:** ~35 min
- **Started:** 2026-09-08T08:20:00-04:00 (approx.)
- **Completed:** 2026-09-08T08:54:11-04:00
- **Tasks:** 1 (single-file deliverable pair: settings.go + settings_test.go)
- **Files modified:** 2 created

## Accomplishments
- `Settings` type unifying site identity, navigation (`NavItem`), and brand tokens (`BrandTokens`) as pure data, tenant-keyed by `CompanyID uuid.UUID`
- `DefaultSettings(companyID)` — the documented no-row default (empty identity, nil `Nav`, non-nil empty `Brand` token maps) returned by `MemorySettingsStore.GetSettings` on a miss, never an error
- `SettingsStore` interface (small, separate seam — `store.go` untouched) + `MemorySettingsStore`, a concurrency-safe in-memory reference implementation proving the round-trip contract
- `ValidateHexColor` / `ValidateLogoURL` — policy-free value guards, ported/generalized from eden-biz's `brand.go`
- Full doc-comment reconciliation of three donor implementations directly in `settings.go`'s package comment

## Task Evidence

| Task | Verify Command | Exit Code | Status |
|---|---|---|---|
| 1: settings.go + settings_test.go | `go test ./platform/cms/... -count=1 -race` | 0 | PASS |
| 1: documented-defaults proof | `go test ./platform/cms/... -run TestDefaultSettings_TenantWithNoRow -v` | 0 | PASS |
| 1: no-CSS gate | `grep -riE "css\|stylesheet\|<style" platform/cms/settings.go` | 1 (no match) | PASS |
| 1: no-vertical-terms gate | `grep -riE "hero\|practitioner\|accordion\|membership_grid" platform/cms/` | 1 (no match) | PASS |

## Task Commits

1. **Task 1: settings.go + settings_test.go** - `a8b9316` (feat)

**Plan metadata:** this commit (docs: SUMMARY.md)

_Note: single-task TRD — one deliverable pair, one commit._

## Validation Gate Results

| Gate | Command | Exit Code | Status |
|---|---|---|---|
| test (full package, race) | `go test ./platform/cms/... -count=1 -race` | 0 | PASS |
| defaults proof | `go test ./platform/cms/... -run TestDefaultSettings_TenantWithNoRow` | 0 | PASS |
| no-CSS grep | `grep -riE "css\|stylesheet\|<style" platform/cms/settings.go` | 1 | PASS (no match) |
| build | `go build ./platform/cms/...` | 0 | PASS |
| vet | `go vet ./platform/cms/...` | 0 | PASS |
| gofmt | `gofmt -l platform/cms/settings.go platform/cms/settings_test.go` | 0 | PASS (no output) |

## Post-TRD Verification

- **Auto-fix cycles used:** 0
- **Must-haves verified:** 4/4
  - Site settings are per-tenant: site identity, navigation, and brand tokens — YES (`Settings.CompanyID`, `Settings.Social`/`Nav`/`Brand`)
  - Brand tokens are DATA, package emits no CSS — YES (grep gate clean; `BrandTokens` is name->value maps only)
  - Settings round-trip through the store, readable by a renderer without cms internals — YES (`TestMemorySettingsStore_RoundTrip`, `TestSettings_JSONRoundTrip`)
  - Tenant with no settings row gets documented defaults, not an error — YES (`TestDefaultSettings_TenantWithNoRow`, doc comment on `DefaultSettings`)
- **Gate failures:** None

## Full Suite Result

```
$ go test ./platform/cms/... -count=1 -race
ok  	github.com/aocybersystems/eden-platform-go/platform/cms	1.316s
```

Test count: 56 pre-existing (Waves 1-2, merged from `plan/obj-40-platform-telephony` before coding began) + 8 new (`TestDefaultSettings_TenantWithNoRow`, `TestMemorySettingsStore_RoundTrip`, `TestMemorySettingsStore_UpsertRequiresCompanyID`, `TestMemorySettingsStore_TenantIsolation`, `TestMemorySettingsStore_ConcurrentAccess`, `TestSettings_JSONRoundTrip`, `TestValidateHexColor`, `TestValidateLogoURL`) = 64 total `--- PASS` lines under `-v`.

## Files Created/Modified
- `platform/cms/settings.go` — `Settings`, `SocialLinks`, `NavItem`, `BrandTokens`, `DefaultSettings`, `SettingsStore`, `MemorySettingsStore`, `ValidateHexColor`, `ValidateLogoURL`
- `platform/cms/settings_test.go` — 8 test functions (including 2 table-driven with named subtests) covering defaults, round-trip, tenant isolation, concurrency (`-race`), JSON round-trip, and both value guards

## Three-Way Reconciliation (source_material)

The TRD required reading three donor implementations, each in a different repo, and taking the superset of DATA while documenting where they disagreed:

### 1. `justinforme/app/cms/site_settings.go` — site identity
Donor shape: a flat, **allowlisted key/value table** (`site_name`, `contact_email`, `contact_phone`, `social_twitter`, `social_facebook`, `social_instagram`, plus CMS-added `hero_tagline`, `footer_text`, `og_image_url`, `site_description`), because it extended an *existing* key/value column the donor app already had (`SettingKey*` constants + `AllCMSSiteSettingKeys` allowlist).

**Decision:** kept the *key set* as the superset (identity + contact + social + a marketing tagline + footer copy + an OG image + a description) but changed the *shape* to a typed struct (`Settings` fields + `SocialLinks`) — this package has no legacy key/value column to extend, and named fields give a renderer compile-time field access instead of stringly-typed lookups. The donor's social-URL normalization logic (`NormalizeSocialValue`, handle-vs-URL canonicalization) was **not ported** — that's edit-time input normalization for an admin API, not data shape.

**Naming note:** the donor's tagline field name contains a word this objective's success-criteria gate explicitly forbids repo-wide (`grep -riE "hero|practitioner|accordion|membership_grid" platform/cms/` must return nothing). Renamed to `Tagline` — same semantic (a short marketing line distinct from `SiteDescription`), vertical-agnostic name. This is a real content-firewall decision, not just gate-avoidance: the objective's platform package should not carry any single vertical's product vocabulary.

### 2. `smartWellness/app/cms/nav_items.go` — navigation
Donor shape: a full two-level navigation **CRUD surface** — `Manager`, `NavItemStore` (Create/Update/Get/List/ListPublished/NextPositionFor/HasChildren/Delete/Reorder), with hierarchy-depth validation (a parent must itself be top-level; no grandchildren; refuse to demote a row that has children).

**Decision:** took only the **data shape** — `NavItem{ID, ParentID, Label, URL, Position, OpenInNewTab, IsPublished}` — as `Settings.Nav`. The Manager's business rules (depth validation, position computation, transactional reorder) were **not ported**: those are edit-time policy for a higher-level admin surface, exactly mirroring how this package's existing `block.go` carries `Block` as inert data while a separate layer owns what block types mean and how they're edited. `platform/cms/settings.go` doc-comments this gap explicitly rather than silently dropping the capability.

### 3. `smartWellness/app/cms/design.go` — brand tokens (design system)
Donor shape: `DesignToken{TokenSet, Key, Value, Description, UpdatedBy, UpdatedAt}` — a flat, individually-addressable row per token, with its own `DesignStore` (List/Upsert/Delete), intended to build a dynamic `theme.css` at request time (explicitly a CSS-emitting consumer this package must NOT replicate).

**Decision:** grouped the same underlying data into three **named buckets** — `Colors`, `TypeRoles`, `Spacing` (open `map[string]string` each) — because the TRD's own must-haves name exactly those three categories. Per-token audit metadata (`UpdatedBy`, per-token `UpdatedAt`) was **dropped**: `Settings` carries one `UpdatedAt` for the whole row (matching `Page`'s convention in this package), and per-token audit trail is an audit-log concern (see eden-biz's separate `audit.Append` pattern in `site_settings.go`), not something a data-only settings row should carry itself.

### 4. `eden-biz/go/internal/website/brand.go` — brand-token validation
Donor shape: **four fixed named fields** (`PrimaryColor`, `AccentColor`, `LogoURL`, `DisplayFont`) with strict validation — a `#rrggbb` hex regex for colors, a **curated four-font allowlist** (`proxima-nova`, `petersburg-web`, `parabolica`, `warnock-pro-display` — eden-biz's own licensed Adobe Typekit fonts) for `DisplayFont`, and a safe-URL check for `LogoURL` (rejects `javascript:`/scheme-relative/whitespace/angle-brackets).

**Decision, and the most consequential disagreement of the three:** the fixed-field shape does **not** generalize to a multi-tenant, multi-vertical platform — a tenant might want more than two named colors, or a type-role scheme with no analog to "display font" at all. `Colors`/`TypeRoles` are open maps instead of fixed fields. `LogoURL` was kept as its own named field (a logo is singular, not a token bucket).

The curated-font **allowlist itself was deliberately NOT ported** — which fonts a renderer has licensed is consumer/tenant policy, not platform data shape, and hard-coding eden-biz's four Adobe fonts into a shared platform package would leak one tenant's licensing situation into everyone's data model. The **hex-color format guard** (`ValidateHexColor`) and a **generalized safe-URL guard** (`ValidateLogoURL`) *were* ported, since both are policy-free "is this well-formed" checks useful to any caller regardless of which colors/fonts it permits — callers opt in to running them; `BrandTokens` itself never calls them on write.

`eden-biz`'s companion `brand_css.go` (the actual CSS-emitting renderer) was **never read and nothing from it was ported** — out of scope by the TRD's explicit instruction, and confirmed absent by the `no-CSS` grep gate on `settings.go`.

## Decisions Made
- No `PostgresSettingsStore` in this TRD: file ownership excludes `migrations/`, so a Postgres-backed implementation has no table to write to yet. `SettingsStore` is defined as the seam a future migration-bearing TRD would satisfy; `MemorySettingsStore` is the reference implementation proving round-trip + documented-defaults today. This mirrors `preview_token.go`'s existing, already-merged precedent for the identical file-ownership constraint (see that file's own doc comment on why it ships no store).
- `Settings.Nav` defaults to `nil` (not an empty slice) for "no navigation configured", matching this package's existing `Block`/`ParseBlocks` convention of nil-means-empty rather than introducing a second empty-collection convention.

## Deviations from Plan

None — TRD executed exactly as written. The only judgment call beyond the TRD's literal text was the `Tagline` rename (see Reconciliation §1), which the TRD's own success criteria anticipated and required (the vertical-specific-terms grep gate).

## Issues Encountered

None. Waves 1-2 (`plan/obj-40-platform-telephony`) merged cleanly on the first attempt; `platform/cms/{block,page,store,store_pg,publish,preview_token}.go` were present as expected and the pre-existing 56-test suite passed before any new code was written, per the worktree-base warning.

## User Setup Required

None — no external service configuration required. (No live database was provisioned or required; `MemorySettingsStore` needs none, and the deferred Postgres implementation is explicitly out of scope for this TRD.)

## Next Objective Readiness

- `Settings`/`SettingsStore` are ready for a renderer or admin-API layer to consume directly — no cms-internal knowledge required beyond the exported struct fields (proven by `TestSettings_JSONRoundTrip`).
- **Open follow-up, flagged rather than silently gapped:** a `cms_site_settings`-backed `PostgresSettingsStore` needs a migration (`migrations/platform/`) that this TRD was not permitted to add. A future TRD in this objective (or a dedicated follow-up) should add that migration and a `PostgresSettingsStore` satisfying the `SettingsStore` interface already defined here — the interface shape should not need to change to accommodate it.
- No blockers for the parallel `41-04` (media) TRD — no shared files touched, no interface it depends on was changed.

## Self-Check: PASSED

- FOUND: `platform/cms/settings.go`
- FOUND: `platform/cms/settings_test.go`
- FOUND commit: `a8b9316`

---
*Objective: 41-platform-cms*
*Completed: 2026-09-08*
