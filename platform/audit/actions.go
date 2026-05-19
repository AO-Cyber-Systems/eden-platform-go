package audit

// This file extends the Action surface declared in event.go with constants and
// supporting data added by AOID objectives that ship under their own TRDs.
// Keeping a separate file per objective reduces merge conflicts when multiple
// objectives are in flight against the audit package in parallel.
//
// # Obj 6 (Federation)
//
// AOID Obj 6 emits audit events for inbound federation assertion decisions,
// just-in-time (JIT) account provisioning, and the admin RPCs that configure
// IdPs / SPs / clients / federation policies. The constants are exported as
// audit.Action so callers get type-safe usage and downstream consumers
// (Obj 9 audit dashboards, AOAudit aggregation) can pin on the string values.
//
//	Constant                          | String value                | Use site
//	----------------------------------|-----------------------------|--------------------------------
//	ActionAccountCreatedJIT           | account.create.jit          | JIT provisioning on inbound
//	ActionDownstreamClientRegistered  | federation.client.register  | OAuth/OIDC client admin RPC
//	ActionDownstreamSPRegistered      | federation.sp.register      | SAML SP admin RPC
//	ActionFederationAccept            | federation.accept           | Inbound assertion accepted
//	ActionFederationIdPConfigured     | federation.idp.configure    | IdP create/update admin RPC
//	ActionFederationIdPRevoked        | federation.idp.revoke       | IdP revocation admin RPC
//	ActionFederationPolicyChanged     | federation.policy.change    | Federation policy admin RPC
//	ActionFederationReject            | federation.reject           | Inbound assertion rejected
//
// ActionFederationReject emissions populate Details["reject_reason"] with one
// of the codes in RejectReasons below. The finite, vetted set prevents drift
// across consumers — auditors filter on these exact strings.
//
// AC-2 evidence: ActionFederationAccept + ActionFederationReject + the
// ActionFederation*Configured/Revoked admin events are the federation arm of
// the AC-2 account-management story. ActionAccountCreatedJIT is the LIFE-09
// just-in-time provisioning record.

// Obj 6 — Federation Action constants. Alphabetical by Go identifier.
const (
	// ActionAccountCreatedJIT is emitted when an inbound federation assertion
	// is accepted and no local account exists for the (idp_id, subject) pair,
	// triggering just-in-time account creation. LIFE-09 evidence.
	ActionAccountCreatedJIT Action = "account.create.jit"

	// ActionDownstreamClientRegistered is emitted by the federation admin RPC
	// when a downstream OAuth/OIDC client is registered.
	ActionDownstreamClientRegistered Action = "federation.client.register"

	// ActionDownstreamSPRegistered is emitted by the federation admin RPC
	// when a downstream SAML service provider is registered.
	ActionDownstreamSPRegistered Action = "federation.sp.register"

	// ActionFederationAccept is emitted on every inbound federation assertion
	// that passes signature, audience, replay, and policy checks (whether or
	// not the local account already existed). Pairs with ActionAccountCreatedJIT
	// when JIT provisioning fires.
	ActionFederationAccept Action = "federation.accept"

	// ActionFederationIdPConfigured is emitted when an upstream IdP is created
	// or updated via the federation admin RPC. Details carry the changed fields.
	ActionFederationIdPConfigured Action = "federation.idp.configure"

	// ActionFederationIdPRevoked is emitted when an upstream IdP is removed
	// via the federation admin RPC. Subsequent inbound assertions from that
	// IdP will reject with reason "idp_not_allowlisted".
	ActionFederationIdPRevoked Action = "federation.idp.revoke"

	// ActionFederationPolicyChanged is emitted when the federation policy
	// (allow-list, attribute-mapping rules, JIT toggles) is mutated.
	ActionFederationPolicyChanged Action = "federation.policy.change"

	// ActionFederationReject is emitted when an inbound federation assertion
	// fails verification or policy. Details["reject_reason"] carries one of
	// the codes in RejectReasons.
	ActionFederationReject Action = "federation.reject"
)

// RejectReasons is the vetted finite set of reason codes that AOID's
// federation service uses when emitting ActionFederationReject. Each code is
// stable across releases — downstream audit dashboards (Obj 9) join on these
// strings to compute per-IdP failure breakdowns, so adding or renaming a
// code is a deliberate cross-repo event.
//
// Slice order is the canonical declaration order used in tests for exact-
// match regression assertions; consumers should treat it as an unordered set.
var RejectReasons = []string{
	"idp_not_allowlisted",          // IdP not in tenant allow-list (or revoked)
	"signature_invalid",            // SAML / OIDC signature did not verify
	"audience_mismatch",            // Audience / aud claim does not match SP
	"nonce_mismatch",               // OIDC nonce did not match issued value
	"expired",                      // NotOnOrAfter / exp in the past
	"replay_detected",              // Assertion ID / nonce already consumed
	"attribute_missing",            // Required SAML attribute / OIDC claim absent
	"email_domain_not_allowed",     // Email domain outside tenant allow-list
	"email_conflict_different_idp", // Email already bound to a different IdP
	"policy_denied",                // Tenant policy rejected the assertion
	"jit_disabled",                 // No local account and JIT off for tenant
	"xsw_detected",                 // SAML XML Signature Wrapping attempt
	"xml_roundtrip_mismatch",       // SAML signed-bytes do not match parsed tree
}
