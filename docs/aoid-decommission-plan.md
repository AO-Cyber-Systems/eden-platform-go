# AO ID Per-Product Decommission Plan

**Status:** Draft, accompanying Objective 31 (M8 milestone)
**Owner:** Platform Identity (with per-product cutover owners listed below)
**Last updated:** 2026-05

This document is the canonical reference for migrating consumer
products off their local user/credential tables and onto AO ID
delegation. The *federation surface* (SAML IdP exports, external-IdP
imports, assertion-to-token bridge) shipped in Objective 31; per-product
migrations happen in follow-on objectives owned by each product team.

The exit criterion is §9 of `PORTFOLIO_STANDARDIZATION_PLAN.md`:

> Every human user authenticates exactly once against AO ID.

When every product matrix row below is checked off, M8 is *fully* met.
M8 milestone closure on the federation surface alone is the
Objective 31 acceptance gate.

---

## Per-product matrix

| Product               | Decommissions                                            | Keeps                                                                | Cutover Owner             | Follow-on Objective |
|-----------------------|----------------------------------------------------------|----------------------------------------------------------------------|---------------------------|---------------------|
| AOSentry (human auth) | `users`, `sessions`, OIDC client tables                  | machine-auth (`master_keys`, `ip_allowlists`, `key_rotations`)        | AOSentry team             | TBD (post-M8)       |
| Eden-Biz              | `users`, `sessions`, credential tables                    | `aoid_subject`-keyed `user_profiles`, role mappings                  | Eden-Biz team             | TBD (post-M8)       |
| AOFamily ai           | `users`, `sessions`                                       | family-profile records (POR'd via `platform/household`)              | AOFamily team             | post-Obj 32         |
| AOFamily browser      | `users`, `sessions`                                       | browser device registrations                                          | AOFamily team             | post-Obj 32         |
| AOFamily connect      | `users`, `sessions`                                       | connect-handle records                                                | AOFamily team             | post-Obj 32         |
| AODex                 | (already migrated Obj 30)                                 | —                                                                     | —                         | n/a                 |
| Eden Family           | n/a — greenfield, launches with AO ID                     | —                                                                     | Eden Family team          | Obj 33              |

AODex is the *reference* migration: when a product team starts its
follow-on, the AODex auth provider (`aodex-go/internal/auth/aoid_provider.go`)
and Obj 30 SUMMARY are the canonical models to copy.

---

## Migration pattern

A uniform six-step playbook applies to every product. Each product's
follow-on objective produces its own SUMMARY but should not deviate
from these steps; deviations require explicit Platform-Identity sign-off.

### 1. Register the product as an OIDC client in AO ID

- Use AO ID's `clients.MemoryRegistry` (Phase A) or future pgstore
  registry. Pick a stable `client_id` (e.g. `aosentry-prod`,
  `edenbiz-prod`).
- Generate a long-lived client secret; rotate every 6 months.
- Configure `redirect_uris` to point at the product's auth callback
  endpoint, e.g. `https://aosentry.aocyber.com/auth/aoid/callback`.
- Allowed scopes: `openid profile email offline_access`.
- Allowed grants: `authorization_code refresh_token`.

### 2. Add an OIDC client at the product

- Copy AODex's `aoid_provider.go` shape: token validation against
  AO ID's JWKS, id_token claim extraction, refresh-token storage.
- Mount `/auth/aoid/callback` returning to the product's session
  cookie flow.
- For Connect/RPC products, plumb the AO ID `Bearer` token through
  `platform/auth.RequireAuth` middleware.

### 3. Dual-write phase

- The product accepts BOTH local and AO ID tokens for ≥ 14 days.
- Local login UI continues to work behind a feature flag.
- Every AO ID login creates a `user_profiles` row keyed by
  `aoid_subject` if missing.

### 4. Migrate existing users

- Idempotent script per product:
  - For each `users` row:
    - Look up AO ID user by email.
    - If missing in AO ID: create a placeholder via Bridge JIT or via
      the (forthcoming) admin Connect endpoint.
    - Stamp `user_profiles.aoid_subject` with the AO ID UUID.
- Write a `migration_audit` row recording (old_user_id, aoid_subject,
  migrated_at, migrator_actor).
- Verify: every product `users` row has a non-null `aoid_subject` on
  the matching `user_profiles` row.

### 5. Cutover

- Flip the product's login UI feature flag: now redirects to
  `https://id.aocyber.com/oauth2/authorize` instead of showing local
  email+password.
- Local login endpoint remains active behind a rollback flag for 30
  days.

### 6. Decommission

- Drop the product's `users` and `sessions` tables (and any related
  credential tables).
- Keep `user_profiles` keyed on `aoid_subject`.
- Archive credential columns per the policy below.

---

## Compliance considerations

### PII at rest

Credentials per se are not PII but adjacent metadata (email,
display_name) is. Archive policy varies by product:

| Product   | Retention            | Storage                       |
|-----------|----------------------|-------------------------------|
| AOSentry  | 1 year               | S3 Glacier (encrypted dump)   |
| Eden-Biz  | 7 years              | S3 Glacier (encrypted dump)   |
| AOFamily  | Per-region (see Obj 32 doc) | S3 Glacier (encrypted dump) |
| AODex     | (already retired)    | (archived during Obj 30)      |

The encrypted dump is symmetric-encrypted with a KMS-managed key.
Access requires Platform-Security + product-owner dual approval.

### Audit trail

A `migration_audit` table per product survives the credential-table
drop. Schema:

```sql
CREATE TABLE migration_audit (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  old_user_id     UUID NOT NULL,
  aoid_subject    UUID NOT NULL,
  migrated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  migrator_actor  TEXT NOT NULL,   -- "auto-script" or "admin@example.com"
  CONSTRAINT      uniq_old_user UNIQUE (old_user_id)
);
```

### Right-to-deletion (GDPR / CCPA)

User deletion via AO ID must cascade to product `user_profiles`. The
mechanism:

- AO ID emits a `subject_deletion` webhook on user delete (spec TBD;
  out of scope for Obj 31 but documented here so per-product
  follow-ons plan for it).
- Each product subscribes; on receipt, marks the matching
  `user_profiles` row for soft-deletion + cascades to derived data per
  product policy.

### COPPA / GDPR-K (AOFamily-specific)

AOFamily's three backends carry minor (child) accounts. Decommissioning
their local user tables happens AFTER Obj 32 (which sits between this
work and AOFamily's existing local auth). Child accounts route through
`platform/household` POR (parent-of-record) for AO ID claim issuance,
documented in Obj 24 + 25 SUMMARYs.

---

## Rollback strategy

Each product retains the dual-write code path behind a feature flag
for ≥30 days post-cutover. If AO ID has a regional outage during that
window, the product flips the flag back and accepts local credentials.

Rollback procedure:
1. Set the product's `AUTH_BACKEND` env var (or feature flag) to
   `local-fallback`.
2. Product re-enables its local login route.
3. AO ID tokens continue to work in parallel (the validator does not
   require AO ID issuer reachability when the local route is enabled).
4. Once AO ID is healthy: flip back to `aoid`, run the resync script
   to capture any new local logins, drop the local-fallback gate.

After 30 days the flag is removed and the old login code path deleted.

---

## §9 exit-criteria verification (R41.8 + R41.9)

`eden status --all` is extended (out-of-scope follow-on) with a
`--federation` flag that queries each registered product's database
and reports row counts in deprecated tables. The exit criterion
**every product reports zero rows** in its decommissioned credential
tables.

Until that flag lands, the audit is manual: each product's follow-on
SUMMARY records the row count at cutover and at the 30-day mark.

---

## What's in vs out of scope for Objective 31

**In scope (shipped):**
- SAML IdP exports per tenant (outbound federation)
- External IdP onboarding (inbound federation, SAML + OIDC)
- Assertion-to-token bridge with JIT provisioning
- HTTP surface mounted on cmd/aoid for federation runtime
- This decommission plan document

**Deferred to follow-ons:**
- Per-product code changes (the migrations themselves)
- Pgstore-backed federation registries (Phase A is in-memory)
- Admin Connect-RPC handlers for federation CRUD
- Flutter admin UI for federation config management
- `subject_deletion` webhook contract + implementation
- `eden status --federation` audit flag

---

## Per-product TRD references

| Product            | Follow-on TRD          | Expected ETA |
|--------------------|------------------------|--------------|
| AOSentry human auth| (TBD)                  | post-M8      |
| Eden-Biz           | (TBD)                  | post-M8      |
| AOFamily ai        | (TBD, after Obj 32)    | post-Obj 32  |
| AOFamily browser   | (TBD, after Obj 32)    | post-Obj 32  |
| AOFamily connect   | (TBD, after Obj 32)    | post-Obj 32  |
| Eden Family        | Obj 33                 | M9           |

Each follow-on opens its own DevFlow objective, generates its own TRDs,
and ships its own SUMMARY citing this plan as the source of truth.
