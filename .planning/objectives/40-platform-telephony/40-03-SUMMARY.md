---
objective: 40-platform-telephony
trd: "03"
subsystem: telephony
tags: [postgres, encryption, multi-tenant, webhooks, migrations, pgx]

# Dependency graph
requires:
  - objective: 40-platform-telephony
    provides: "TRD 01 — Provider interface, NoopProvider, Registry, Config/value types, CompanyID rename (platform/telephony/models.go)"
provides:
  - "ConfigStore interface + PostgresConfigStore: per-tenant provider config persisted in tenant_telephony_config (migration 016)"
  - "Encrypter seam (NopEncrypter for tests) + FieldEncrypter production adapter over platform/encryption.FieldEncryptor"
  - "LookupBySendingNumber(provider, number) — the number->tenant resolution inbound webhooks route through"
  - "ErrSendingNumberClaimed — fail-closed conflict semantics carried forward from eden-biz/telephonycreds"
affects: ["40-04", "40-05", "40-02"]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Active-row-with-history config model: Upsert deactivates the prior active row in the same tx, then inserts the new one — never a destructive UPDATE of credentials in place"
    - "Encrypter seam (interface + NopEncrypter) so DB-backed tests can run without a real key; production wiring supplies FieldEncrypter"
    - "Partial unique index (WHERE is_active = true) as the fail-closed guard against a security-sensitive routing ambiguity, mapped from pgconn 23505 to a typed sentinel error"

key-files:
  created:
    - platform/telephony/config_store.go
    - platform/telephony/config_store_test.go
    - platform/telephony/field_encrypter.go
    - platform/telephony/field_encrypter_test.go
    - migrations/platform/016_tenant_telephony_config.up.sql
    - migrations/platform/016_tenant_telephony_config.down.sql
  modified: []

key-decisions:
  - "field_encrypter.go vendors NO crypto — it is a 33-line adapter that calls platform/encryption.New(key, key) and delegates Encrypt/Decrypt straight through; the same 32-byte key is reused for both the encryption and blind-index arguments since telephony credentials are write-only and never blind-indexed."
  - "Carried eden-biz/telephonycreds' from-number conflict semantics forward, even though politihub's source store had no such guard: added a partial unique index on (provider, sending_number) WHERE is_active, and a new ErrSendingNumberClaimed sentinel mapped from the resulting pgconn 23505."
  - "Deliberately did NOT create the telephony_phone_optouts table in migration 016, despite file_ownership permitting it 'if natural' — TRD 03's own file_tree scopes migrations/platform/ to the config table only, politihub's opt-out store depends on a custom SQL function (politihub_normalize_phone) that doesn't exist here and needs its own design, and TRD 40-05 (which depends on 40-03) owns that Go store — better for it to define the schema atomically alongside its own code."
  - "Fixed a test-fixture bug found during final verification: config_store_test.go's sending-number literals were NOT actually unique across repeated runs against the same persistent test database (only company slugs were), so a second run collided on migration 016's own partial unique index. Added uniqueNumber() and replaced every literal."

patterns-established:
  - "DATABASE_URL-gated integration tests for platform/telephony, mirroring platform/pgstore's existing convention — skip cleanly when unset, exercise the real Postgres partial-unique-index behavior when set."

requirements-completed: [R46]

# Verification evidence
verification:
  gates_defined: 4
  gates_passed: 4
  auto_fix_cycles: 1
  tdd_evidence: false
  test_pairing: true

# Metrics
duration: ~30min (commit-span); executed across two agent turns after a mid-task turn-limit interruption
completed: 2026-09-08
---

# Objective 40 TRD 03: Per-Tenant Telephony Config Summary

**Postgres-backed ConfigStore with credentials encrypted at rest via platform/encryption.FieldEncryptor, plus the LookupBySendingNumber tenant resolver that inbound webhooks route through, carrying eden-biz's from-number conflict guard forward as a partial unique index.**

## Performance

- **Duration:** ~30 min across commits (21:22–21:52 local); execution spanned two agent turns due to a mid-task turn-limit interruption between the migration/config_store commits and the field_encrypter/test commits
- **Completed:** 2026-09-08
- **Tasks:** 4 (migration, ConfigStore, FieldEncrypter, config_store_test) + 1 auto-fixed deviation
- **Files modified:** 6 created, 0 modified (pre-existing files)

## Accomplishments

- `platform/telephony/config_store.go`: `ConfigStore` interface (`Get`, `LookupBySendingNumber`, `Upsert`, `Deactivate`, `List`), `PostgresConfigStore` implementation, `Encrypter` seam, `NopEncrypter`
- `platform/telephony/field_encrypter.go`: thin production adapter delegating 100% of crypto to `platform/encryption.FieldEncryptor`
- Migration 016: `tenant_telephony_config` table with a partial unique index enforcing one active row per company AND one active row per (provider, sending_number)
- Full test coverage proving ciphertext-at-rest, round-trip decryption, correct tenant resolution by sending number, and fail-closed conflict rejection

## Task Evidence

| Task | Verify Command | Exit Code | Status |
|---|---|---|---|
| 1: Migration 016 (up/down) | `migrate -path migrations/platform -database ... up` then `down 1` then `up` | 0 | PASS |
| 2: ConfigStore + PostgresConfigStore | `go build ./platform/telephony/...` | 0 | PASS |
| 3: FieldEncrypter adapter | `go test ./platform/telephony/... -run TestFieldEncrypter -v` | 0 | PASS |
| 4: config_store_test.go (DB-gated) | `DATABASE_URL=... go test ./platform/telephony/... -count=1 -race` | 0 | PASS |

## Task Commits

Each task was committed atomically:

1. **Task 1: tenant_telephony_config migration (016)** — `a10148c` (chore)
2. **Task 2: ConfigStore interface + PostgresConfigStore** — `581128d` (feat)
3. **Task 3: FieldEncrypter adapter + test** — `34720ec` (feat)
4. **Task 4: config_store_test.go** — `b837a0d` (test)
5. **Deviation fix: unique sending numbers in test fixtures** — `5d238c7` (fix, Rule 1)

**Plan metadata:** this commit (docs: complete TRD)

## Validation Gate Results

| Gate | Command | Exit Code | Status |
|---|---|---|---|
| build (package) | `go build ./platform/telephony/...` | 0 | PASS |
| build (repo-wide) | `go build ./...` | 0 | PASS |
| vet | `go vet ./platform/telephony/...` | 0 | PASS |
| fmt | `gofmt -l platform/telephony/` | 0 (empty output) | PASS |
| test -race (DB-backed) | `DATABASE_URL=... go test ./platform/telephony/... -count=1 -race` | 0 | PASS (21/21, run twice consecutively) |
| test (no DB) | `go test ./platform/telephony/... -count=1 -race` | 0 | PASS (15 run, 6 skip cleanly) |

## Post-TRD Verification

- **Auto-fix cycles used:** 1 (test-fixture sending-number uniqueness bug, Rule 1)
- **Must-haves verified:** 4/4 — all four `<verify>` items from the TRD
- **Gate failures:** None (the one failure encountered — 4 tests failing on a second consecutive run — was diagnosed and fixed before being counted as a gate result; see Deviations)

### Full verify-check evidence (as required by the coordinator's continuation message)

**1. `go test ./platform/telephony/... -count=1 -race` (green, run twice consecutively against the same live Postgres to prove no fixture-collision regression):**

```
=== RUN   TestPostgresConfigStore_GetNotConfigured
--- PASS: TestPostgresConfigStore_GetNotConfigured (0.18s)
=== RUN   TestPostgresConfigStore_UpsertGetRoundTrip_CiphertextAtRest
--- PASS: TestPostgresConfigStore_UpsertGetRoundTrip_CiphertextAtRest (0.19s)
=== RUN   TestPostgresConfigStore_UpsertDeactivatesPriorRow
--- PASS: TestPostgresConfigStore_UpsertDeactivatesPriorRow (0.19s)
=== RUN   TestPostgresConfigStore_LookupBySendingNumber
--- PASS: TestPostgresConfigStore_LookupBySendingNumber (0.20s)
=== RUN   TestPostgresConfigStore_FromNumberConflictIsRejected
--- PASS: TestPostgresConfigStore_FromNumberConflictIsRejected (0.21s)
=== RUN   TestPostgresConfigStore_DeactivateAndList
--- PASS: TestPostgresConfigStore_DeactivateAndList (0.20s)
=== RUN   TestNopEncrypter_PassesThrough
--- PASS: TestNopEncrypter_PassesThrough (0.00s)
=== RUN   TestFieldEncrypterValidKey
--- PASS: TestFieldEncrypterValidKey (0.00s)
=== RUN   TestFieldEncrypterBadKeyLength
--- PASS: TestFieldEncrypterBadKeyLength (0.00s)
=== RUN   TestFieldEncrypterRoundTrip
--- PASS: TestFieldEncrypterRoundTrip (0.00s)
=== RUN   TestFieldEncrypterNonceUniqueness
--- PASS: TestFieldEncrypterNonceUniqueness (0.00s)
=== RUN   TestFieldEncrypterEmptyString
--- PASS: TestFieldEncrypterEmptyString (0.00s)
=== RUN   TestConfig_SignatureToken
--- PASS: TestConfig_SignatureToken (0.00s)
=== RUN   TestReconstructURL_Direct
--- PASS: TestReconstructURL_Direct (0.00s)
=== RUN   TestReconstructURL_HonoursForwardedHeaders
--- PASS: TestReconstructURL_HonoursForwardedHeaders (0.00s)
=== RUN   TestMapStatus
--- PASS: TestMapStatus (0.00s)
=== RUN   TestRegistry_RegisterAndLookup
--- PASS: TestRegistry_RegisterAndLookup (0.00s)
=== RUN   TestRegistry_Unregistered
--- PASS: TestRegistry_Unregistered (0.00s)
=== RUN   TestRegistry_Override
--- PASS: TestRegistry_Override (0.00s)
=== RUN   TestRegistry_NilFactoryPanics
--- PASS: TestRegistry_NilFactoryPanics (0.00s)
=== RUN   TestNoopProvider_AllErrPaths
--- PASS: TestNoopProvider_AllErrPaths (0.00s)
=== RUN   TestNoopProvider_Type
--- PASS: TestNoopProvider_Type (0.00s)
PASS
ok  	github.com/aocybersystems/eden-platform-go/platform/telephony	3.900s
```

Second consecutive run (same live container, no restart, no truncate) — proves the earlier fixture bug is actually fixed and not a fluke of a fresh database:

```
ok  	github.com/aocybersystems/eden-platform-go/platform/telephony	3.755s
```

**2. Ciphertext-at-rest + round-trip through `platform/encryption`:** `TestPostgresConfigStore_UpsertGetRoundTrip_CiphertextAtRest` (included in the run above) — writes with a real `FieldEncrypter`, reads the raw `auth_token_encrypted`/`webhook_secret_encrypted` columns with a bypass SQL query, asserts the raw bytes differ from and are longer than the plaintext, then separately proves (a) `store.Get()` decrypts back to the original plaintext and (b) calling `enc.Decrypt()` directly on the same raw bytes independently reproduces the plaintext — the second assertion is what proves genuine delegation to `platform/encryption`, not a store-internal shortcut. PASS.

**3. `LookupBySendingNumber` correctness:** `TestPostgresConfigStore_LookupBySendingNumber` (included in the run above) — two companies with distinct numbers each resolve to their own `Config`; wrong-provider and unclaimed-number lookups both collapse to `ErrTenantNotConfigured` with no cross-tenant existence leak. PASS.

**4. Migration 016 applies and rolls back cleanly:**

```
$ migrate -database "postgres://...:5545/telephony_test?sslmode=disable" -path migrations/platform down 1
16/d tenant_telephony_config (40.507459ms)

$ docker exec telephony-trd03-pg psql -U postgres -d telephony_test -c "\dt tenant_telephony_config"
Did not find any relation named "tenant_telephony_config".

$ migrate -database "postgres://...:5545/telephony_test?sslmode=disable" -path migrations/platform up
16/u tenant_telephony_config (44.240666ms)
```

## Files Created/Modified

- `platform/telephony/config_store.go` — `ConfigStore` interface, `PostgresConfigStore`, `Encrypter`, `NopEncrypter`, `ErrSendingNumberClaimed`
- `platform/telephony/config_store_test.go` — net-new DB-gated integration coverage (no politihub equivalent exists)
- `platform/telephony/field_encrypter.go` — thin adapter over `platform/encryption.FieldEncryptor`
- `platform/telephony/field_encrypter_test.go` — near-verbatim port of politihub's test
- `migrations/platform/016_tenant_telephony_config.up.sql` / `.down.sql` — new table + two partial unique indexes

## Decisions Made

1. **`field_encrypter.go` delegates 100% to `platform/encryption.FieldEncryptor` — no crypto vendored or reimplemented.** The entire file is a 33-line adapter: `NewFieldEncrypter(key)` calls `encryption.New(key, key)` (reusing the single 32-byte key for both the encryption and blind-index arguments, since telephony credentials are write-only and never blind-indexed), and `Encrypt`/`Decrypt` are one-line pass-throughs to `platform/encryption`'s methods. `TestPostgresConfigStore_UpsertGetRoundTrip_CiphertextAtRest` independently confirms this by decrypting the raw on-disk bytes directly through the same `FieldEncrypter`, outside the store, and getting back the original plaintext.

2. **From-number conflict semantics: CARRIED forward from eden-biz/telephonycreds.** Politihub's original `config_store.go` had no guard at all — any tenant could silently claim a sending_number already owned by another tenant's active config, and `LookupBySendingNumber`'s unordered `LIMIT 1` would then resolve inbound webhook traffic to whichever row Postgres happened to return — a live security/correctness gap for a webhook-routing chokepoint. Migration 016 adds `uq_tenant_telephony_config_active_number`, a partial unique index on `(provider, sending_number) WHERE is_active = true`, matching eden-biz's `uq_telephony_credentials_from_number` invariant. `PostgresConfigStore.Upsert` maps the resulting `pgconn.PgError` code `23505` to a new sentinel `ErrSendingNumberClaimed`. `TestPostgresConfigStore_FromNumberConflictIsRejected` proves the full lifecycle: company B's claim on A's active number is rejected, A's row is untouched, B stays unconfigured, and B's retry succeeds only after A deactivates.

3. **The `telephony_phone_optouts` table was deliberately NOT created in migration 016**, despite `file_ownership` permitting it "if natural." Reasons: (a) TRD 03's own `file_tree` scopes `migrations/platform/` to the tenant config table only — "ADD telephony config table (016)" is the actual scope; (b) politihub's opt-out store depends on a custom SQL function (`politihub_normalize_phone`) that does not exist in this repo and would need its own design decision, not a mechanical lift; (c) TRD 40-05 (which explicitly depends on `["01","03"]`) owns the opt-out Go store and is better positioned to define that schema atomically alongside its own code rather than inheriting a table shape from an unrelated TRD.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] config_store_test.go's sending-number literals were not actually unique across repeated test runs**
- **Found during:** final verification (re-running the full suite a second time against the same persistent test Postgres, per the coordinator's request to paste real, freshly-run verify output)
- **Issue:** The file's own comment claimed "slugs and sending numbers are suffixed with a fresh uuid per test," but only company slugs (via `mustCreateCompany`) actually got one — sending numbers were fixed literals (`+15550001111`, etc). Migration 016's partial unique index on `(provider, sending_number) WHERE is_active` means a second consecutive run against the same (non-truncated) database collided with rows the first run left active, failing 4 of 6 DB-gated tests with `ErrSendingNumberClaimed` — not a real bug in the store, but a broken test fixture masquerading as one.
- **Fix:** Added a `uniqueNumber()` helper (uuid-derived, called fresh per use) and replaced every hardcoded number literal, including the "unclaimed number" literal used to prove a negative lookup.
- **Files modified:** `platform/telephony/config_store_test.go`
- **Verification:** Ran the full suite twice consecutively against the same live container with no restart/truncate between runs; both runs pass 21/21.
- **Committed in:** `5d238c7`

---

**Total deviations:** 1 auto-fixed (1 bug, Rule 1)
**Impact on plan:** Test-infrastructure-only; no production code changed as a result. Necessary to trust the `<verify>` evidence pasted above rather than a one-shot lucky pass.

## Issues Encountered

Execution spanned two agent turns: a turn-limit interruption occurred after committing the migration (`a10148c`) and `config_store.go` (`581128d`) but before `field_encrypter.go`/`field_encrypter_test.go` were committed, even though both files were already written to disk. The coordinator's continuation message confirmed the exact on-disk/git state and directed resumption without redoing work; verified via `git status --short` before proceeding, then committed the two files (`34720ec`), wrote `config_store_test.go` (`b837a0d`), and completed the deviation fix (`5d238c7`) described above.

## User Setup Required

None — no external service configuration required. Local verification used an ephemeral `postgres:17-alpine` Docker container (`telephony-trd03-pg`, host port 5545) for the DATABASE_URL-gated integration tests; this container is not part of the shipped code and can be discarded.

## Next Objective Readiness

- `ConfigStore`/`PostgresConfigStore`/`Encrypter`/`FieldEncrypter` are ready for TRD 40-04 (webhook handlers, which need `LookupBySendingNumber` for inbound routing) and TRD 40-02 (concrete provider adapters, which need `Get`/`Upsert` for tenant credentials).
- TRD 40-05 (opt-out store) can now depend on migration 016 being present but still owns its own opt-out table migration and Go store — not created here, as documented above.
- `go.mod` untouched; no new dependency introduced by this TRD.

## Self-Check: PASSED

All 7 claimed files confirmed present on disk (`config_store.go`, `config_store_test.go`, `field_encrypter.go`, `field_encrypter_test.go`, `016_tenant_telephony_config.up.sql`, `016_tenant_telephony_config.down.sql`, this SUMMARY.md). All 5 claimed commit hashes (`a10148c`, `581128d`, `34720ec`, `b837a0d`, `5d238c7`) confirmed present via `git cat-file -e`.

---
*Objective: 40-platform-telephony*
*TRD: 03*
*Completed: 2026-09-08*
