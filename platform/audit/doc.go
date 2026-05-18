// Package audit emits canonical audit events across the AOCyber portfolio.
//
// Events flow through the Logger to a Store implementation; the package owns
// the typed Action surface (event.go), HTTP-derived metadata helpers, and the
// standard Details keys (DetailBefore/DetailAfter/etc).
//
// # Identity Lifecycle (added in AOID Obj 2 — TRD 02-03)
//
// AOID emits the following Action constants for identity lifecycle events.
// Each event is AC-2 evidence for FedRAMP / IL5 compliance.
//
//	Constant                  | NIST AC-2 Sub-control | Evidence
//	--------------------------|-----------------------|--------------------------------
//	ActionAccountCreate       | AC-2(a), AC-2(d)      | Account provisioning records
//	ActionAccountUpdate       | AC-2(d)               | Attribute changes
//	ActionAccountSuspend      | AC-2(c), AC-2(f)      | Suspension for recertification
//	ActionAccountRecover      | AC-2(c)               | Recovery from suspension
//	ActionAccountDelete       | AC-2(h), AC-2(i)      | Deprovisioning with retention
//	ActionAccountExpire       | AC-2(2), AC-2(3)      | Automated expiration (LIFE-08)
//	ActionGroupCreate         | AC-2(j), AC-3         | Group definition
//	ActionGroupDelete         | AC-2(j)               | Group removal
//	ActionGroupMemberAdd      | AC-2(j), AC-6         | Membership grant
//	ActionGroupMemberRemove   | AC-2(j), AC-6         | Membership revoke
//	ActionIdentityRoleCreate  | AC-6                  | Role definition (tenant-scoped)
//	ActionIdentityRoleAssign  | AC-6, AC-6(5)         | Role grant to account
//	ActionIdentityRoleRevoke  | AC-6                  | Role revocation
//	ActionEntitlementSet      | AC-3, AC-6            | Per-account entitlement set
//	ActionEntitlementDelete   | AC-3                  | Entitlement removal
//	ActionTenantCreate        | AC-2(a)               | Tenant provisioning (super-admin)
//
// MGMT-05 evidence convention: ActionIdentityRoleAssign emissions populate
// Details["role_name"] with the role name. Auditors filter
// `Action == "identity.role.assign" AND Details.role_name == "tenant_admin"`
// to surface tenant-admin grants; the AOID AssignRole handler is responsible
// for populating that field.
//
// All identity events carry tenant_id in Event.CompanyID — the audit package's
// pre-existing field. AOID maps the conceptual "tenant" onto "company" because
// the underlying data shape (a UUID scoping every event) is identical.
//
// # Naming Conventions
//
// Action strings follow "domain.entity.verb" (e.g. "auth.user.login",
// "identity.account.suspend"). The Go identifier for an Action constant uses
// PascalCase ("ActionUserLogin"); when a domain reuses a noun that another
// domain already claimed (the "role" case across rbac.* and identity.*),
// disambiguate the Go identifier by adding the domain as a prefix
// ("ActionIdentityRoleAssign") rather than dropping into a different package.
// Two Action constants must never share a string value — see
// TestActionConstants_AllUnique for the regression guard.
package audit
