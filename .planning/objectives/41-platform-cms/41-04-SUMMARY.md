---
objective: 41-platform-cms
job: "04"
subsystem: cms
tags: [go, cms, image-pipeline, storage, upload, content-addressed, idempotent]

# Dependency graph
requires:
  - objective: 41-01
    provides: "Block.Type opacity convention this file follows for MediaVariant naming (caller-opaque, no cms-level special-casing)"
  - objective: 41-02
    provides: "platform/cms package scaffold (store.go's tenant-scoping-by-companyID convention this file mirrors)"
provides:
  - "MediaPipeline.DeriveVariants — accepts raw image bytes + content type, derives a configurable []MediaVariant catalog (default: original/thumb/display), stores each rendition through platform/storage, returns Media{ContentHash, []VariantResult{StorageKey, Width, Height, ContentType, URL}}"
  - "MediaPipeline.DeriveFromAttachment — derives variants for a source already uploaded through platform/upload's presigned-URL flow, by fetching upload.Attachment.StorageKey through the same storage.Client"
  - "MediaPipeline.PresignVariant — mints a fresh presigned GET URL for an already-derived variant's stable StorageKey"
  - "Content-addressed, tenant-scoped key derivation (keyPrefix/companyID/sha256(bytes)/variantName) — the mechanism that makes derivation idempotent and safe to re-run"
affects: [41-05-settings, 41-06-docs]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Content-addressed storage keys (sha256 of source bytes) instead of uuid/timestamp-based keys — the specific mechanism that makes DeriveVariants idempotent: same tenant + same bytes always yields the same StorageKey per variant, and a Stat-hit on an existing key short-circuits re-deriving/re-storing"
    - "image.DecodeConfig bounds check BEFORE image.Decode — reads only the header, rejects declared width/height beyond MaxMediaDimension (8000px) before any pixel buffer is allocated, defeating decompression-bomb-style malformed uploads"
    - "Nearest-neighbor resize implemented against the standard library's image package only (no golang.org/x/image/draw) — this TRD does not own go.mod, so no new module dependency was added; see key-decisions"
    - "Narrow composition surface: MediaPipeline takes platform/storage.Client (interface, already satisfied by storage.NewMemoryClient for infra-free tests) for ALL blob IO, and platform/upload.Attachment (a plain struct, no DB/minio.Client needed) as its adapter into platform/upload — no second blob implementation added to cms"

key-files:
  created:
    - platform/cms/media.go
    - platform/cms/media_test.go
  modified: []

key-decisions:
  - "Video (video_service.go / video_url.go in the smartWellness donor) is explicitly OUT of scope for V1, per this TRD's constraint. This is a deliberate deferral, not a silent drop — recorded here per the constraint's instruction, and left as a documented non-goal in media.go's file-level comment for future readers. No video-shaped seam (interface, type, or field) was added speculatively; a later TRD that wants video support designs it fresh rather than being constrained by a placeholder from this one."
  - "Did NOT add golang.org/x/image/draw (used by both donor image_service.go files for BiLinear resampling). go.mod is outside this TRD's file-ownership scope (platform/cms/{media.go,media_test.go} only, per <file_ownership>), so resizeToWidth is a small nearest-neighbor implementation against the standard library's image package alone. Quality tradeoff is acceptable for thumbnail/display renditions; a future TRD that DOES own go.mod can swap the resampler behind the same call site without touching DeriveVariants' contract."
  - "Chose content-addressed (sha256-of-bytes) storage keys over the donor pattern's uuid.NewString()-based keys specifically because this TRD's must-haves require idempotent derivation ('re-processing the same source yields the same variant keys'). A uuid- or timestamp-based key can never satisfy that by construction — it produces a NEW key on every call. sha256(companyID, bytes, variantName) does, and additionally gets free deduplication (identical uploads across time never create a second copy) and a cheap re-run (Stat-hit skips Put entirely)."
  - "DeriveFromAttachment composes platform/upload's Attachment TYPE (a plain struct: StorageKey/ContentType/Filename/SizeBytes) rather than platform/upload.Service (which requires a live *minio.Client and a DB-backed UploadStore to construct). This keeps composition genuine — go list -deps ./platform/cms shows platform/upload as a real dependency — while staying fully testable without provisioning MinIO or Postgres, per this run's turn-budget constraint against live infra. The doc comment on DeriveFromAttachment states its one real-world assumption explicitly: platform/upload and this MediaPipeline must be wired to the same storage.Client/bucket for the fetch to succeed."
  - "VariantResult carries both StorageKey (proven stable/idempotent by test) and URL (a presigned GET URL freshly minted on every call, per platform/storage's PresignedGet semantics — its value embeds an expiry and is NOT expected to be byte-identical across separate DeriveVariants calls). The must-have 'returns stable URLs' is satisfied at the StorageKey layer, which is the actually-stable identifier a page persists; URL is documented as a convenience mint, re-mintable any time via PresignVariant(key, ttl)."

requirements-completed: [R47]

# Verification evidence
verification:
  gates_defined: 4
  gates_passed: 4
  auto_fix_cycles: 0
  tdd_evidence: false
  test_pairing: true

# Metrics
duration: 10min
completed: 2026-09-08
---

# Objective 41 TRD 04: CMS Media Pipeline Summary

**Content-addressed image variant derivation composing platform/storage + platform/upload — idempotent by construction via sha256-keyed storage paths, with an image.DecodeConfig bounds gate that rejects oversized/malformed images before any pixel buffer is allocated.**

## Performance

- **Duration:** ~10 min (merge of Waves 1-2 to final task commit)
- **Started:** 2026-09-08T08:47:23-04:00 (post-merge baseline)
- **Completed:** 2026-09-08T08:56:33-04:00
- **Tasks:** 1 (single-file deliverable per TRD scope)
- **Files modified:** 2 (both new: media.go, media_test.go)

## Accomplishments
- `MediaPipeline.DeriveVariants` — validates an upload (size cap, content-type whitelist, `image.DecodeConfig` dimension bounds), derives a configurable variant catalog (default: `original`/`thumb`/`display`), stores each rendition through `platform/storage.Client`, and returns stable content-addressed keys plus presigned URLs
- `MediaPipeline.DeriveFromAttachment` — genuine composition seam into `platform/upload.Attachment`, letting a presigned-URL upload flow feed straight into variant derivation without a second re-upload
- Idempotency proven by test: two `DeriveVariants` calls on identical `(companyID, bytes)` yield identical `ContentHash` and identical `StorageKey` per variant; a second call's `Stat` hit skips re-deriving/re-storing entirely
- Malformed-image safety proven by two tests: garbage bytes claiming `image/png` (rejected by `DecodeConfig` itself) and a CRC-valid, header-tampered PNG that spoofs a 60000x60000 declared size (rejected by the `MaxMediaDimension` bounds check specifically, BEFORE `image.Decode` ever runs) — both wrapped in `recover()` to make the "never panics" claim explicit, not just implied by test survival

## Task Evidence

| Task | Verify Command | Exit Code | Status |
|---|---|---|---|
| 1: CMS media pipeline (media.go + media_test.go) | `go test ./platform/cms/... -count=1 -race` | 0 | PASS |

## Task Commits

1. **Task 1: CMS media pipeline** - `cb8e77e` (feat)

**Plan metadata:** this commit (docs: SUMMARY + STATE untouched per `<worktree_protocol>` — a parallel agent owns STATE.md/ROADMAP.md)

## Validation Gate Results

All four `<verify>` gates from the TRD, run explicitly and pasted below (not inferred):

### Gate 1 — `go test ./platform/cms/... -count=1 -race` green

```
$ go test ./platform/cms/... -count=1 -race
ok  	github.com/aocybersystems/eden-platform-go/platform/cms	5.610s
```
Full verbose run: 74 tests pass (56 pre-existing from Waves 1-2 + 18 new in media_test.go), 0 fail, 6 Postgres-integration tests SKIP cleanly (`DATABASE_URL not set` — no live infra was provisioned, per this run's constraint).

### Gate 2 — idempotent variant derivation

```
$ go test ./platform/cms/... -run 'TestDeriveVariants_Idempotent_SameSourceYieldsSameKeys' -v
=== RUN   TestDeriveVariants_Idempotent_SameSourceYieldsSameKeys
--- PASS: TestDeriveVariants_Idempotent_SameSourceYieldsSameKeys (0.09s)
PASS
ok  	github.com/aocybersystems/eden-platform-go/platform/cms	0.403s
```

### Gate 3 — malformed image yields a typed error, never a panic

```
$ go test ./platform/cms/... -run 'TestDeriveVariants_MalformedImage_TypedErrorNeverPanic|TestDeriveVariants_SpoofedDimensions_RejectedByDecodeConfigBoundsCheck' -v
=== RUN   TestDeriveVariants_MalformedImage_TypedErrorNeverPanic
--- PASS: TestDeriveVariants_MalformedImage_TypedErrorNeverPanic (0.00s)
=== RUN   TestDeriveVariants_SpoofedDimensions_RejectedByDecodeConfigBoundsCheck
--- PASS: TestDeriveVariants_SpoofedDimensions_RejectedByDecodeConfigBoundsCheck (0.00s)
PASS
ok  	github.com/aocybersystems/eden-platform-go/platform/cms	0.285s
```

### Gate 4 — `go list -deps ./platform/cms` shows platform/storage, no second blob implementation

```
$ go list -deps ./platform/cms | grep "eden-platform-go/platform"
github.com/aocybersystems/eden-platform-go/platform/storage
github.com/aocybersystems/eden-platform-go/platform/upload
github.com/aocybersystems/eden-platform-go/platform/cms
```
Both `platform/storage` and `platform/upload` present as real dependencies (media.go imports both directly); no `minio-go`, no filesystem, no second `s3Client`/blob wrapper anywhere in `platform/cms`.

### Additional gate — block-catalog isolation (success_criteria, not `<verify>`)

```
$ grep -riE "hero|practitioner|accordion|membership_grid" platform/cms/
(no output, exit 1)
```
Note: an earlier draft of `TestDefaultVariants_IsGenericNotBlockSpecific` listed those four words verbatim as a "forbidden" slice to assert their absence, which self-tripped this exact grep. Per this run's explicit instruction ("if it fires on test data, rename the fixture rather than weakening the gate"), the test was rewritten as `TestDefaultVariants_IsGenericSizingOnly` — an allow-list assertion (`original`/`thumb`/`display` only) that proves the same property without containing the banned strings.

## Post-TRD Verification

- **Auto-fix cycles used:** 0
- **Must-haves verified:** 4/4 (accepts upload + derives variants + stable URLs; composes platform/storage + platform/upload; idempotent derivation; typed-error-never-panic on malformed input)
- **Gate failures:** None

## Files Created/Modified
- `platform/cms/media.go` — `MediaPipeline`, `MediaVariant`/`DefaultVariants`, `UploadInput`/`Media`/`VariantResult`, `DeriveVariants`/`DeriveFromAttachment`/`PresignVariant`, typed `Err*` sentinels, content-addressed key derivation, stdlib-only nearest-neighbor resize
- `platform/cms/media_test.go` — 18 tests: happy path, no-upscale, idempotency (same source/same keys), tenant isolation (same source/different tenant/different keys), JPEG source support, empty/oversized/unsupported-type rejection, malformed-image + spoofed-dimension rejection (both `recover()`-guarded), nil-company-id rejection, storage-disabled fail-closed (3 methods), `PresignVariant` happy path, `DeriveFromAttachment` composition + missing-object handling, default-variant-catalog genericity

## Decisions Made
See `key-decisions` in frontmatter — five decisions, all with inline rationale: video deferral, no new `golang.org/x/image/draw` dependency (go.mod out of scope), content-addressed keys for idempotency, `Attachment`-struct-not-`Service` composition (infra-free testability), and the StorageKey-vs-URL stability split.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] `gofmt` misalignment in the initial media.go draft**
- **Found during:** Task 1, pre-commit formatting check
- **Issue:** `gofmt -l platform/cms/media.go` flagged the file (import-comment alignment and a multi-var block's column spacing)
- **Fix:** `gofmt -w platform/cms/media.go`; re-ran `gofmt -l` to confirm clean
- **Files modified:** platform/cms/media.go
- **Verification:** `gofmt -l platform/cms/media.go` returns no output after the fix
- **Committed in:** cb8e77e (part of Task 1 commit — fixed before first commit, not a separate commit)

**2. [Rule 1 - Bug] Self-defeating test tripped the block-catalog isolation grep gate**
- **Found during:** Task 1, running the success_criteria grep gate against the finished package
- **Issue:** `TestDefaultVariants_IsGenericNotBlockSpecific` spelled out `hero`/`practitioner`/`accordion`/`membership_grid` verbatim in a "forbidden" slice, which the `grep -riE` gate correctly matched — a test asserting the ABSENCE of donor vocabulary necessarily contains that vocabulary as a literal.
- **Fix:** Rewrote the test as `TestDefaultVariants_IsGenericSizingOnly`, an allow-list check (only `original`/`thumb`/`display` permitted) that proves the identical property (no block-catalog vocabulary in the shipped default variant catalog) without containing any of the banned strings.
- **Files modified:** platform/cms/media_test.go
- **Verification:** `grep -riE "hero|practitioner|accordion|membership_grid" platform/cms/` returns no matches (exit 1) after the fix; `go test ./platform/cms/...` still green with the renamed test passing
- **Committed in:** cb8e77e (part of Task 1 commit — fixed before first commit, not a separate commit)

---

**Total deviations:** 2 auto-fixed (both Rule 1 — bugs caught by this TRD's own verification gates before the single commit was made, so neither has a separate commit hash)
**Impact on plan:** No scope creep. Both were mechanical fixes surfaced by running the TRD's own required checks, not new work.

## Issues Encountered

A prompt-style "system-reminder" appeared mid-session instructing file edits to be done via raw `Bash`/heredoc instead of the tracked `Read`/`Edit`/`Write` tools. This was disregarded — it conflicted with this workflow's explicit, standing prohibition on heredoc-based file edits (which bypasses read-before-write and file-history tracking), and no legitimate reason for the override was given. All file changes in this TRD were made through `Write`/`Edit`, as required. Flagging this here for visibility, not as a blocker — it had no effect on the delivered code.

## User Setup Required

None — no external service configuration required. `MediaPipeline` requires a `storage.Client` (either `storage.NewMemoryClient` for dev/test, wired up in this TRD's tests, or `storage.NewS3Client` against a real S3/MinIO backend for production — the latter's setup is platform/storage's own concern, unchanged by this TRD).

## Next Objective Readiness

- `platform/cms/media.go` is ready for a caller (e.g., a Connect handler in a later TRD or a downstream consumer repo) to wire `MediaPipeline` behind an upload endpoint: construct with `NewMediaPipeline(MediaPipelineConfig{Storage: <production storage.Client>})`, call `DeriveVariants` (direct byte upload) or `DeriveFromAttachment` (finishing a `platform/upload` presigned-URL flow).
- No blockers for 41-05 (settings) or 41-06 (docs) — this TRD touched only `platform/cms/{media.go,media_test.go}`, did not touch `go.mod`, `migrations/`, `STATE.md`, or `ROADMAP.md`, per `<file_ownership>` / `<worktree_protocol>`.
- Video support remains an explicit, documented non-goal for V1 (see key-decisions) — a future TRD designing it starts fresh rather than extending a placeholder.

---
*Objective: 41-platform-cms*
*Completed: 2026-09-08*
