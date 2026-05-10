// Package discovery serves the OIDC discovery document at
// /.well-known/openid-configuration and the issuer-not-active stubs at
// /oauth2/{token,authorize,userinfo}.
//
// During the Objective 29 scaffolding milestone the discovery document
// returns 200 with the standard fields populated AND a non-standard
// `service_status` claim of "scaffold" so federation tooling can probe
// the service while issuance is still off. The actual token / authorize
// / userinfo endpoints reply 503 with a documented JSON error body.
package discovery

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/aocybersystems/eden-platform-go/internal/aoid/config"
)

// ServiceStatusScaffold is the value of the non-standard service_status
// field while the issuer is not yet active. Once Objective 30 turns the
// issuer on, this becomes "active".
const ServiceStatusScaffold = "scaffold"

// Doc is the OpenID Connect discovery document, augmented with a
// non-standard `service_status` claim. Field names match the spec —
// keep snake_case JSON tags or relying parties will fail to parse.
//
// Reference: https://openid.net/specs/openid-connect-discovery-1_0.html#ProviderMetadata
type Doc struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	UserinfoEndpoint                  string   `json:"userinfo_endpoint"`
	JWKSURI                           string   `json:"jwks_uri"`
	RegistrationEndpoint              string   `json:"registration_endpoint,omitempty"`
	ScopesSupported                   []string `json:"scopes_supported"`
	ResponseTypesSupported            []string `json:"response_types_supported"`
	ResponseModesSupported            []string `json:"response_modes_supported,omitempty"`
	GrantTypesSupported               []string `json:"grant_types_supported"`
	SubjectTypesSupported             []string `json:"subject_types_supported"`
	IDTokenSigningAlgValuesSupported  []string `json:"id_token_signing_alg_values_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported,omitempty"`
	ClaimsSupported                   []string `json:"claims_supported,omitempty"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported,omitempty"`

	// ServiceStatus is a non-standard extension field. While the value is
	// "scaffold" the issuer endpoints reply 503; relying parties should
	// not treat the discovery document's existence as a contract that
	// tokens can be issued.
	ServiceStatus string `json:"service_status"`
}

// BuildDoc returns the discovery document for the given config.
func BuildDoc(cfg *config.Config) Doc {
	issuer := strings.TrimRight(cfg.Issuer, "/")
	return Doc{
		Issuer:                issuer,
		AuthorizationEndpoint: issuer + "/oauth2/authorize",
		TokenEndpoint:         issuer + "/oauth2/token",
		UserinfoEndpoint:      issuer + "/oauth2/userinfo",
		JWKSURI:               issuer + "/.well-known/jwks.json",
		ScopesSupported:       []string{"openid", "profile", "email", "offline_access"},
		ResponseTypesSupported: []string{
			"code",
			"id_token",
			"code id_token",
		},
		ResponseModesSupported: []string{"query", "fragment", "form_post"},
		GrantTypesSupported: []string{
			"authorization_code",
			"refresh_token",
		},
		SubjectTypesSupported:             []string{"public"},
		IDTokenSigningAlgValuesSupported:  []string{"ML-DSA-65"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_basic", "client_secret_post", "none"},
		ClaimsSupported: []string{
			"sub", "iss", "aud", "exp", "iat", "auth_time",
			"email", "email_verified", "name", "preferred_username",
		},
		CodeChallengeMethodsSupported: []string{"S256"},
		ServiceStatus:                 ServiceStatusScaffold,
	}
}

// Handler returns an http.HandlerFunc that emits the OIDC discovery
// document built from cfg. The response is fresh on every request — the
// document is small enough that caching would buy little; future TRDs
// can layer a Cache-Control front-door if probes get aggressive.
func Handler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		doc := BuildDoc(cfg)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=300")
		_ = json.NewEncoder(w).Encode(doc)
	}
}

// IssuerNotActive is the http.HandlerFunc used by the token / authorize /
// userinfo paths while the issuer is in scaffold state. Returns 503 with
// a documented JSON body so relying parties can distinguish "issuer
// off" from generic infrastructure errors.
func IssuerNotActive(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusServiceUnavailable)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":             "issuer_not_active",
		"error_description": "AO ID is in scaffold mode; token issuance activates in objective 30",
	})
}
