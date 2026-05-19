package audit

// This file extends the Action surface declared in event.go with constants for
// the AOID Objective 5 credential-issuance domain. Keeping a separate file
// per objective reduces merge conflicts when multiple objectives are in
// flight against the audit package in parallel.
//
// # Obj 5 (Credential issuance beyond OAuth)
//
// AOID Obj 5 emits audit events for API-key issuance + validation
// (CRED-05, VAL-03), mTLS client/server/workload certificate issuance
// (CRED-06), and SPIFFE workload SVID issuance (CRED-07). The constants
// are exported as audit.Action so callers get type-safe usage and
// downstream consumers (Obj 9 audit dashboards, AOAudit aggregation)
// can pin on the string values.
//
//	Constant                          | String value                  | Use site
//	----------------------------------|-------------------------------|--------------------------------
//	ActionApiKeyMinted                | credential.api_key.minted     | apikey.Service.Mint
//	ActionApiKeyValidated             | credential.api_key.validated  | apikey.Service.Validate (ok)
//	ActionApiKeyValidationDenied      | credential.api_key.validation_denied | apikey.Service.Validate (fail)
//	ActionApiKeyRevoked               | credential.api_key.revoked    | apikey.Service.Revoke
//	ActionApiKeyRotated               | credential.api_key.rotated    | apikey.Service.Rotate
//	ActionCertificateIssued           | credential.certificate.issued | pki.Service.Issue
//	ActionCertificateRenewed          | credential.certificate.renewed| pki.Service.Renew
//	ActionCertificateRevoked          | credential.certificate.revoked| pki.Service.Revoke
//	ActionSVIDIssued                  | credential.svid.issued        | svid.Service.Issue (ok)
//	ActionSVIDIssuedFailure           | credential.svid.issued.failure| svid.Service.Issue (fail)
//	ActionSVIDRevoked                 | credential.svid.revoked       | svid.Service.Revoke
//
// Naming note: these constants live in the audit.* Go identifier space
// alongside the pre-existing auth.apikey.* constants (ActionAPIKeyCreate,
// ActionAPIKeyRevoke, ActionAPIKeyRotate) declared in event.go. Those
// pre-existing constants are GENERIC (no credential subsystem prefix);
// the new ones below use the "credential.*" prefix to mark them as
// belonging to AOID's credential-issuance subsystem (CRED-05 / CRED-06 /
// CRED-07). Downstream consumers parse on the string value, so the two
// surface independently — pin on ActionAPIKeyCreate for the legacy
// generic emit path and on ActionApiKeyMinted for the credential-
// issuance path.
//
// AC-2 evidence: the credential.* events are AOID's credential-lifecycle
// arm of the AC-2 account-management story (parallel to identity.*
// events for accounts/groups/roles/entitlements/tenants).

// Obj 5 — Credential Action constants. Alphabetical by Go identifier.
const (
	// ActionApiKeyMinted is emitted when apikey.Service.Mint successfully
	// creates a new API key. Details carry tenant_id, owner_account_id,
	// key_prefix, key_id, name, scopes.
	ActionApiKeyMinted Action = "credential.api_key.minted"

	// ActionApiKeyRevoked is emitted when apikey.Service.Revoke successfully
	// marks an API key revoked. Details carry tenant_id, key_id, key_prefix,
	// reason.
	ActionApiKeyRevoked Action = "credential.api_key.revoked"

	// ActionApiKeyRotated is emitted when apikey.Service.Rotate completes
	// (atomic revoke-old + mint-new). Details carry tenant_id, old_key_id,
	// new_key_id, key_prefix (new).
	ActionApiKeyRotated Action = "credential.api_key.rotated"

	// ActionApiKeyValidated is emitted on every successful
	// apikey.Service.Validate call (validation API hot path). Details
	// carry tenant_id, key_id, key_prefix.
	ActionApiKeyValidated Action = "credential.api_key.validated"

	// ActionApiKeyValidationDenied is emitted on every failed
	// apikey.Service.Validate call. Details carry tenant_id, key_prefix
	// (or empty for malformed input), reason. reason is one of:
	// "unknown" | "expired" | "invalid_format".
	ActionApiKeyValidationDenied Action = "credential.api_key.validation_denied"

	// ActionCertificateIssued is emitted when pki.Service.Issue successfully
	// signs a leaf X.509 certificate. Details carry tenant_id, serial_number,
	// subject_dn, kind, owner_account_id (if any).
	ActionCertificateIssued Action = "credential.certificate.issued"

	// ActionCertificateRenewed is emitted when pki.Service.Renew issues a
	// replacement cert. Details carry tenant_id, old_serial, new_serial,
	// subject_dn.
	ActionCertificateRenewed Action = "credential.certificate.renewed"

	// ActionCertificateRevoked is emitted when pki.Service.Revoke flips the
	// cert to revoked + appends to aoid.revocations. Details carry
	// tenant_id, serial_number, revoked_reason_code (RFC 5280 §5.3.1).
	ActionCertificateRevoked Action = "credential.certificate.revoked"

	// ActionSVIDIssued is emitted when svid.Service.Issue successfully
	// produces a workload SVID. Details carry tenant_id, spiffe_id,
	// serial_number, expires_at.
	ActionSVIDIssued Action = "credential.svid.issued"

	// ActionSVIDIssuedFailure is emitted when svid.Service.Issue fails
	// (policy denial, KMS-sign failure, invalid input). Details carry
	// tenant_id, spiffe_id (if known), reason.
	ActionSVIDIssuedFailure Action = "credential.svid.issued.failure"

	// ActionSVIDRevoked is emitted when svid.Service.Revoke marks the SVID
	// row revoked + appends to aoid.revocations. Details carry tenant_id,
	// spiffe_id, serial_number.
	ActionSVIDRevoked Action = "credential.svid.revoked"
)
