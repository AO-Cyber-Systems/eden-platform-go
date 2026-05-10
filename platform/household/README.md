# platform/household

Net-new platform package providing the family / parent-of-record / child-account
model. Backs AOFamily-AI today and Eden Family at launch (per
`PORTFOLIO_STANDARDIZATION_PLAN.md` §6 N1).

## Overview

`household` models three things:

1. **Household** — a billable / governable group keyed on a primary contact
   user. Eden Family uses households as the family-plan billable entity;
   AOFamily-AI uses them as the COPPA / GDPR-K compliance anchor.
2. **Member** — an individual associated with a household, with a `Role`
   (parent / child / guardian / adult-non-parent / other), a `Status`
   (pending / active / removed), and a `Capabilities` flag bag.
3. **Parent-of-record** — the legally-responsible parent for a child member.
   Multiple parents-of-record per child are supported (split households).
   Only members with `Role = parent | guardian` may be established as
   parent-of-record.

All mutations route through `household.Service`, which wraps a `household.Store`
and emits a `platform/audit` event for every change.

## Quickstart

```go
import (
    "github.com/aocybersystems/eden-platform-go/platform/audit"
    "github.com/aocybersystems/eden-platform-go/platform/household"
    "github.com/aocybersystems/eden-platform-go/platform/pgstore"
)

backend, _ := pgstore.NewBackend(ctx, dbURL, migrationsFS)
auditLogger := audit.NewLogger(backend.AuditStore())
auditLogger.Start()
defer auditLogger.Stop()

svc := household.NewService(backend.HouseholdStore(), auditLogger)

ac := household.AuditContext{
    CompanyID: tenantCompanyID, // billable company for the family
    ActorID:   currentUserID,
    IPAddress: clientIP,
}

// Create the household; the actor becomes its primary contact.
hh, err := svc.CreateHousehold(ctx, ac, "Smith Family", nil)

// Add the actor as parent-of-record member.
parent, err := svc.AddMember(ctx, ac, household.Member{
    HouseholdID:  hh.ID,
    UserID:       currentUserID,
    Role:         household.RoleParentOfRecord,
    Capabilities: household.DefaultCapabilities(household.RoleParentOfRecord),
})

// Add a child (birthdate is required for COPPA logic).
bday := time.Date(2018, 6, 1, 0, 0, 0, 0, time.UTC)
child, err := svc.AddMember(ctx, ac, household.Member{
    HouseholdID: hh.ID,
    UserID:      childUserID,
    Role:        household.RoleChild,
    Birthdate:   &bday,
})

// Establish the legal parent-of-record link.
por, err := svc.EstablishParentOfRecord(ctx, ac, child.ID, parent.ID)
```

## Audit semantics

Every mutating call emits an audit event with `Resource = "household"`. The
action constants are exported (`ActionHouseholdCreated`, `ActionMemberAdded`,
etc.) so callers can filter or assert on them.

The `AuditContext.CompanyID` is required because the platform `audit_logs`
table FK-references `companies(id)`. For Eden Family use the family's billable
company; for AOFamily-AI use the per-tenant company id. Households themselves
are otherwise company-agnostic.

## Roles & capabilities

| Role               | CanGrantConsent | Default capabilities                                         |
|--------------------|-----------------|--------------------------------------------------------------|
| `parent`           | yes             | invite, billing, consent, audit                              |
| `guardian`         | yes             | invite, consent, audit                                       |
| `adult_non_parent` | no              | none by default                                              |
| `child`            | no              | none — capabilities are typically managed by parent          |
| `other`            | no              | none                                                         |

`Capabilities` is a JSONB struct with `omitempty` JSON tags, so adding new
capability fields in the future does not require a migration.

## Integration with platform/consent (Objective 25)

`platform/consent` keys consent records on household members. The eligibility
gate uses `household.Role.CanGrantConsent()` — only `parent` or `guardian`
members may grant consent on behalf of a child member. The package also
exports `ActionParentOfRecordEstablished` so consent flows can audit-correlate.

## Migration story

No existing Eden product has ad-hoc family / parent records today. When such
products appear, the recommended migration is:

1. Create a household per legacy family record, with the most-billable adult
   as primary contact.
2. Insert each adult as `RoleParentOfRecord` with default capabilities.
3. Insert each minor as `RoleChild` with the recorded birthdate.
4. For each adult-child pair recorded as legally responsible, call
   `EstablishParentOfRecord`.
5. Replay the migration audit trail to `audit_logs` so the historical
   relationships are not lost.

A migration tool is **not** in scope for Objective 24; document the pattern
above when the first migration is required.

## Test surface

- `service_test.go` covers all Service-layer business rules with an
  in-memory store (no DB needed).
- `pgstore/household_test.go` covers the PostgreSQL-backed store and an
  end-to-end service + audit-emission integration test.

## Out of scope

- **Consent ledger** — see Objective 25 / `platform/consent`.
- **Billing rail / feature flags** — Objective 27 / `platform/entitlements`.
- **AO ID composition** — Objective 29 / `ao-id` service.
