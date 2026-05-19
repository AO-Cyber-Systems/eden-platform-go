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
//
// # Obj 7 (Federal-identity integrations)
//
// AOID Obj 7 emits audit events for DoD CAC / PIV mTLS federation, Login.gov
// OIDC federation, and the assurance / IAL / AAL policy enforcer that gates
// access on credential strength. Constants below extend the federation
// surface above; downstream SIEM rules distinguish federal-civilian
// (logingov.*) and DoD (cac.*) flows from the generic federation.* events.
//
//	Constant                          | String value                | Use site
//	----------------------------------|-----------------------------|--------------------------------
//	ActionAssurancePolicyViolation    | auth.policy_violation       | Assurance / IAL / AAL policy enforcer (TRD 07-07) rejection
//	ActionCACCertValidated            | cac.cert_validated          | CAC handler (TRD 07-05) — DoD CA chain + OCSP/CRL check passed
//	ActionCACEdipiExtracted           | cac.edipi_extracted         | CAC handler — EDIPI parsed from PIV NACI / FASC-N
//	ActionCACSessionStart             | cac.session_start           | CAC handler — mTLS client-cert session opened
//	ActionCACValidationFailed         | cac.validation_failed       | CAC handler — chain build, OCSP, EDIPI extraction, or policy fail
//	ActionLoginGovCallback            | logingov.callback           | Login.gov handler (TRD 07-06) — OIDC callback receipt
//	ActionLoginGovJITProvisioned      | logingov.jit_provisioned    | Login.gov handler — JIT account creation (IAL2-attested)
//	ActionLoginGovSessionStart        | logingov.session_start      | Login.gov handler — authorize request begin
//
// Naming note: ActionLoginGovJITProvisioned is intentionally a sibling of —
// not an alias for — Obj 6's ActionAccountCreatedJIT. SIEM rules and Obj 9
// dashboards differentiate IAL2-attested federal civilian provisioning from
// the generic SAML / OIDC JIT event so each can be audited independently
// against the FedRAMP High / DoD IL5 evidence requirements.
//
// AC-2 evidence: the cac.* + logingov.* events are the federal-identity arm
// of the AC-2 account-management story. ActionAssurancePolicyViolation is
// the IA-2 / IA-8 enforcement record — the policy enforcer emits it on every
// credential rejection so auditors can review denials by tenant and reason.

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

// Obj 7 — Federal-identity Action constants (TRD 07-03). Alphabetical by
// Go identifier. String values use the "auth.policy_violation" / "cac.*" /
// "logingov.*" prefixes; downstream consumers parse on the wire strings.
const (
	// ActionAssurancePolicyViolation is emitted by the Obj 7 assurance /
	// IAL / AAL policy enforcer (TRD 07-07) when a presented credential
	// does not satisfy the tenant's configured assurance policy (e.g.
	// password-only authn against a tenant requiring CAC or IAL2). Details
	// carry tenant_id, account_id (if known), required_level, actual_level,
	// credential_type, reason.
	ActionAssurancePolicyViolation Action = "auth.policy_violation"

	// ActionCACCertValidated is emitted by the AOID CAC federation handler
	// (TRD 07-05) after the presented client certificate chains to a DoD
	// trusted root, OCSP / CRL status is good, and EKU / NotAfter checks
	// pass. Details carry tenant_id, serial_number, issuer_dn, subject_dn.
	ActionCACCertValidated Action = "cac.cert_validated"

	// ActionCACEdipiExtracted is emitted by the AOID CAC federation handler
	// after EDIPI is parsed from the validated certificate (PIV NACI OID or
	// FASC-N otherName). Details carry tenant_id, edipi (hashed for log
	// hygiene), subject_dn.
	ActionCACEdipiExtracted Action = "cac.edipi_extracted"

	// ActionCACSessionStart is emitted by the AOID CAC federation handler at
	// the start of an mTLS PIV / CAC session — before chain validation —
	// so the audit record exists even if downstream validation fails.
	// Details carry tenant_id, remote_addr, sni, cert_serial (if presented).
	ActionCACSessionStart Action = "cac.session_start"

	// ActionCACValidationFailed is emitted by the AOID CAC federation handler
	// when any step of CAC validation fails: chain-build error, OCSP / CRL
	// denial, EKU mismatch, EDIPI extraction failure, or policy rejection.
	// Details carry tenant_id, reason, and (when known) serial_number,
	// issuer_dn. Pairs with ActionCACSessionStart for the failed session.
	ActionCACValidationFailed Action = "cac.validation_failed"

	// ActionLoginGovCallback is emitted by the AOID Login.gov federation
	// handler (TRD 07-06) on receipt of the OIDC authorization-code callback,
	// before token exchange. Details carry tenant_id, state, has_code,
	// has_error. Pairs with ActionLoginGovSessionStart by state value for
	// per-session correlation.
	ActionLoginGovCallback Action = "logingov.callback"

	// ActionLoginGovJITProvisioned is emitted by the AOID Login.gov
	// federation handler when an IAL2-attested userinfo response leads to
	// just-in-time creation of a local account. Distinct from Obj 6's
	// ActionAccountCreatedJIT so SIEM rules and Obj 9 dashboards can
	// differentiate federal-civilian provisioning. Details carry tenant_id,
	// account_id, ial_level, aal_level, sub (Login.gov pairwise subject).
	ActionLoginGovJITProvisioned Action = "logingov.jit_provisioned"

	// ActionLoginGovSessionStart is emitted by the AOID Login.gov federation
	// handler when an authorize request is constructed (before the redirect
	// to Login.gov). Details carry tenant_id, state, nonce, requested_ial,
	// requested_aal.
	ActionLoginGovSessionStart Action = "logingov.session_start"
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
