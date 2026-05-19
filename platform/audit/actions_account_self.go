package audit

// This file extends the Action surface declared in event.go with constants
// for the AOID Objective 8 self-service portal domain.
//
// # Obj 8 (Self-service portal — MGMT-01, MGMT-02, MGMT-04)
//
// AOID Obj 8 emits audit events for the end-user self-service portal:
// profile edits, MFA removal (including the policy-gated last-factor
// denial), OAuth-grant revocation, and self-revocation of API keys.
// The constants are exported as audit.Action so callers get type-safe
// usage and downstream consumers (Obj 9 audit dashboards, AOAudit
// aggregation) can pin on the string values.
//
//	Constant                              | String value                              | Use site
//	--------------------------------------|-------------------------------------------|--------------------------------
//	ActionProfileUpdated                  | profile.updated                           | account_self.Service.UpdateMyProfile
//	ActionMFARemoved                      | auth.mfa.removed                          | account_self.Service.RemoveMyMFA  (success)
//	ActionMFARemovalBlockedLastFactor     | auth.mfa.removal_blocked_last_factor      | account_self.Service.RemoveMyMFA  (policy-gated denial)
//	ActionOAuthGrantRevoked               | auth.oauth_grant.revoked                  | account_self.Service.RevokeMyOAuthGrant
//
// Naming note: these constants live in the audit.* Go identifier space.
// String values follow the project's "domain.entity.verb" convention.
// The pre-existing ActionApiKeyRevoked from actions_credentials.go is
// REUSED (not duplicated) for self-service API-key revocation — callers
// distinguish admin-initiated vs self-initiated via
// Event.Details["initiator"] = "self".

// Obj 8 — Account self-service Action constants. Alphabetical by Go identifier.
const (
	// ActionMFARemovalBlockedLastFactor is emitted when
	// account_self.Service.RemoveMyMFA refuses to revoke the caller's last
	// active MFA factor because the tenant's effective policy requires MFA.
	// Details carry account_id, tenant_id, credential_id.
	ActionMFARemovalBlockedLastFactor Action = "auth.mfa.removal_blocked_last_factor"

	// ActionMFARemoved is emitted when account_self.Service.RemoveMyMFA
	// successfully soft-revokes an MFA factor (TOTP, WebAuthn, or PIV).
	// Details carry account_id, tenant_id, credential_id, kind.
	ActionMFARemoved Action = "auth.mfa.removed"

	// ActionOAuthGrantRevoked is emitted when
	// account_self.Service.RevokeMyOAuthGrant burns every active refresh
	// token for (account_id, client_id). Details carry account_id, client_id,
	// refresh_tokens_burned.
	ActionOAuthGrantRevoked Action = "auth.oauth_grant.revoked"

	// ActionProfileUpdated is emitted when
	// account_self.Service.UpdateMyProfile persists a profile patch. Details
	// carry account_id plus boolean flags indicating which optional fields
	// were present in the patch (display_name_set, contact_email_set,
	// comm_updates_set, comm_marketing_set).
	ActionProfileUpdated Action = "profile.updated"
)
