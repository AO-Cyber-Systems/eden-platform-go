// Package federation implements the AO ID federation surface: the
// outbound SAML/OIDC Identity Provider exports that downstream Service
// Providers consume, and the inbound external-IdP imports that let AOC
// tenants delegate authentication to their corporate Okta / Azure AD /
// Google Workspace IdPs.
//
// # Outbound (IdP exports)
//
//   - Each tenant (TenantID) configures a SAML IdP entry: entity ID,
//     SSO URL, allowed SPs, attribute template.
//   - `Registry` (in-memory Phase A; pgstore-backed in a follow-on)
//     persists `TenantIdPConfig` rows.
//   - `IdPManager` lazily constructs `platform/auth/saml/idp.IdentityProvider`
//     instances per tenant, sharing the AO ID signing key via a
//     `KeyResolver`.
//   - Downstream SPs fetch /saml/idp/{tenant}/metadata, post AuthnRequests
//     to /saml/idp/{tenant}/sso, and receive signed assertions.
//
// # Inbound (external-IdP imports)
//
//   - Each tenant may register one or more `TenantExternalIdP` entries
//     (the customer's Okta, Azure AD, etc.).
//   - `ExternalIdP` wraps the IdP per-tenant, knows how to build
//     AuthnRequests / OIDC authorization URLs and parse the responses.
//   - `Bridge.HandleAssertion` translates a validated external assertion
//     into an AO ID native token pair via JIT provisioning.
//
// # Lifetime
//
// All structures here are constructed by `internal/aoid/composition`
// and stored on `composition.Services.Federation`. The HTTP surface in
// `handlers.go` mounts under cmd/aoid's mux.
//
// # Status
//
// Phase A (objective 31, M8 milestone): in-memory registries; shared
// signing key across tenants; per-product decommissions documented but
// not executed. Per-tenant signing-key rotation, pgstore-backed
// registries, and Flutter admin UI are deferred to follow-on objectives.
package federation
