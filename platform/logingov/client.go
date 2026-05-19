package logingov

import (
	"context"
	"crypto/rsa"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/aocybersystems/eden-platform-go/platform/oidcrp"
	oidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// Exported sentinel errors. Callers branch on these via errors.Is.
//
// ErrSigningKeyMissing: Config.SigningKey is nil. The caller (AOID admin
// RPC) must materialize the RP signing key from KMS before constructing
// the Client.
//
// ErrSigningKeyTooShort: Config.SigningKey is shorter than RSA-2048.
// Login.gov rejects shorter keys per its OIDC documentation; we enforce
// the same check at construction time to fail closed early.
//
// ErrNonceMismatch: the nonce claim in the returned ID token does not
// match the storedNonce supplied to Client.Exchange. CRITICAL — indicates
// possible replay or session-hijack; treat as fatal, never issue a
// session. Re-exported as a distinct value (not aliased to oidcrp's) so
// callers can compose tighter switches when both OIDC stacks are in play.
//
// ErrACRMismatch: returned by callers (not this package directly) when
// mapACR yields "none" or a level below the policy-required minimum. We
// expose the sentinel here so the AOID handler in TRD 07-06 can branch on
// it consistently.
//
// ErrTokenEndpointStatus: the Login.gov token endpoint returned a non-200
// HTTP status. The wrapped error carries the status code + body excerpt
// for audit + operator triage.
var (
	ErrSigningKeyMissing   = errors.New("logingov: signing key missing")
	ErrSigningKeyTooShort  = errors.New("logingov: signing key too short (RSA < 2048 bits)")
	ErrNonceMismatch       = errors.New("logingov: nonce mismatch")
	ErrACRMismatch         = errors.New("logingov: acr mismatch")
	ErrTokenEndpointStatus = errors.New("logingov: token endpoint non-200 status")
)

// Config is the caller-supplied configuration for a Login.gov RP client.
//
// All fields are required EXCEPT ACRValues, Scopes, HTTPClient and
// SigningKID, which have safe defaults. Validation runs in NewClient.
type Config struct {
	// TenantID is the AOID tenant identifier; used as part of the cache
	// key passed to platform/oidcrp's ProviderCache + VerifierCache. The
	// concrete value is opaque to this package — just must be stable per
	// logical (tenant, idp) binding.
	TenantID string

	// ClientID is the RP's Login.gov-registered client_id (a UUID-shaped
	// string). It appears as `iss` and `sub` in the client_assertion JWT
	// and as `aud` in the returned ID token.
	ClientID string

	// IssuerURL is the Login.gov OP issuer URL. The two known production
	// values are:
	//   - sandbox: https://idp.int.identitysandbox.gov/
	//   - prod:    https://secure.login.gov/
	// MUST match the `iss` claim in returned ID tokens exactly. Operator
	// config, not hardcoded here.
	IssuerURL string

	// RedirectURL is the RP's callback URL registered with Login.gov. MUST
	// match the redirect_uri sent in the authorization request AND the
	// token-exchange request, otherwise Login.gov rejects with a 400.
	RedirectURL string

	// SigningKey is the RP's RSA private key (≥ 2048 bits) used to sign
	// the client_assertion JWT per RFC 7523 §2.2. The corresponding
	// public key MUST be registered with Login.gov (either via direct
	// key upload in the Partner Portal or via an RP-hosted jwks_uri).
	SigningKey *rsa.PrivateKey

	// SigningKID is the Key ID header value placed in the
	// client_assertion JWT header. Login.gov uses this to look up the
	// registered public key when multiple keys are on file (key rotation
	// window). May be empty if only one key is registered.
	SigningKID string

	// ACRValues overrides the default acr_values sent in the
	// authorization request. If empty, BuildAuthURL defaults to
	// []string{"urn:acr.login.gov:auth-only"} — IAL1, the lowest tier.
	// Callers requesting verified identity or AAL2/3 supply explicit
	// URN values per Login.gov documentation §A.4.
	ACRValues []string

	// Scopes is the OAuth 2.0 scopes requested. If empty, defaults to
	// []string{"openid", "email"}. Common additions are "profile",
	// "phone", "address", "all_emails" — see Login.gov scope docs.
	Scopes []string

	// HTTPClient is the *http.Client used for discovery, JWKS fetch,
	// and the token-endpoint POST. If nil, http.DefaultClient is used.
	// Tests override this to inject a stub OP's transport.
	HTTPClient *http.Client
}

// ID is the post-exchange identity record returned by Client.Exchange.
// All fields except RawClaims are derived from the verified ID token.
//
// AssuranceLevel is the AOID-canonical assurance enum derived by mapACR
// from the raw ACR claim. Callers (AOID federation handler) compare
// AssuranceLevel to the policy-required minimum and audit + reject if
// below threshold.
type ID struct {
	// Sub is the Login.gov per-RP UUID. Identical for the same end user
	// across multiple Exchange calls against the same RP; DIFFERENT for
	// the same end user across different RPs (privacy-by-design).
	Sub string

	// Email is the verified email address from the ID token. Login.gov
	// guarantees this address is verified (EmailVerified == true).
	Email string

	// EmailVerified mirrors the `email_verified` claim. Login.gov always
	// returns true; callers MUST still check this defensively.
	EmailVerified bool

	// ACR is the raw `acr` claim value from the ID token, before mapping.
	// Stored for audit traceability — the AOID audit event includes the
	// raw URN to preserve forensic evidence of what Login.gov asserted.
	ACR string

	// AssuranceLevel is the AOID-canonical assurance enum produced by
	// mapACR. One of: "ial_1", "verified_no_match", "ial_2",
	// "ial_2_preferred", "aal_2", "aal_3", "aal_3_piv", or "none".
	AssuranceLevel string

	// RawClaims is the entire ID token claims map for downstream
	// attribute mapping (oidcrp.ApplyClaimMap or bespoke logic).
	RawClaims map[string]any
}

// defaultACR is the lowest-tier ACR value, used when cfg.ACRValues is
// empty in BuildAuthURL. IAL1 — proofing not required, basic auth only.
const defaultACR = "urn:acr.login.gov:auth-only"

// defaultScopes is Login.gov's minimum acceptable scope set. "openid" is
// mandatory for OIDC; "email" yields the verified email address used as
// the primary identity attribute by the AOID federation handler.
var defaultScopes = []string{oidc.ScopeOpenID, "email"}

// Client is a Login.gov-specialized OIDC RP. Holds the cached
// *oidc.Provider (populated via the shared platform/oidcrp ProviderCache),
// the OAuth2 config derived from discovery, and a reference to the
// VerifierCache used at Exchange time.
//
// Construct via NewClient. Safe for concurrent use across goroutines:
// the embedded provider + verifier cache are themselves concurrent-safe.
type Client struct {
	cfg           Config
	provider      *oidc.Provider
	oauthConfig   *oauth2.Config
	verifierCache *oidcrp.VerifierCache
	httpClient    *http.Client
	tokenEndpoint string
}

// NewClient constructs a Login.gov RP Client. Runs validation on the
// caller-supplied Config, executes OIDC discovery via the shared
// ProviderCache (singleflight-collapsed on cold start), and pre-derives
// the oauth2.Config + token endpoint URL.
//
// Validation:
//
//   - cfg.SigningKey must be non-nil → otherwise ErrSigningKeyMissing.
//   - cfg.SigningKey.N.BitLen() must be ≥ 2048 → otherwise
//     ErrSigningKeyTooShort.
//   - cfg.HTTPClient defaults to http.DefaultClient when nil.
//   - cfg.Scopes defaults to ["openid","email"] when empty.
//
// providerCache + verifierCache should be process-singletons shared
// across all Clients (one set per Eden binary, not one per Config). They
// are passed in explicitly so callers can control invalidation around
// JWKS rotation events.
func NewClient(ctx context.Context, cfg Config, providerCache *oidcrp.ProviderCache, verifierCache *oidcrp.VerifierCache) (*Client, error) {
	if cfg.SigningKey == nil {
		return nil, ErrSigningKeyMissing
	}
	if cfg.SigningKey.N.BitLen() < minRSAKeyBits {
		return nil, ErrSigningKeyTooShort
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = http.DefaultClient
	}
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = defaultScopes
	}
	if providerCache == nil {
		return nil, fmt.Errorf("logingov: NewClient: nil providerCache")
	}
	if verifierCache == nil {
		return nil, fmt.Errorf("logingov: NewClient: nil verifierCache")
	}

	cacheKey := "logingov:" + cfg.TenantID
	p, err := providerCache.Get(ctx, cacheKey, cfg.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("logingov: provider discovery: %w", err)
	}

	return &Client{
		cfg:      cfg,
		provider: p,
		oauthConfig: &oauth2.Config{
			ClientID:    cfg.ClientID,
			RedirectURL: cfg.RedirectURL,
			Scopes:      cfg.Scopes,
			Endpoint:    p.Endpoint(),
		},
		verifierCache: verifierCache,
		httpClient:    cfg.HTTPClient,
		tokenEndpoint: p.Endpoint().TokenURL,
	}, nil
}

// BuildAuthURL composes the Login.gov authorization-code request URL.
// PKCE (S256) and nonce are mandatory (enforced by oidcrp.BuildAuthURL);
// Login.gov-specific acr_values is attached as an extra auth code option.
//
// acr_values resolution:
//   - If cfg.ACRValues was set, it forms the base set.
//   - Otherwise the base set is ["urn:acr.login.gov:auth-only"] (IAL1).
//   - extraACR is appended to the base set. Callers requesting AAL
//     escalation pass values like
//     "http://idmanagement.gov/ns/assurance/aal/2?phishing_resistant=true".
//
// The final acr_values parameter is space-separated per OIDC core §3.1.2.1.
//
// state and nonce MUST be caller-managed opaque values (typically the
// signed-state from oidcrp.SignedState and a cryptographically random
// nonce stored in the InFlightStore). pkceVerifier MUST be ≥ 43 chars
// (oidcrp.BuildAuthURL enforces).
func (c *Client) BuildAuthURL(state, nonce, pkceVerifier string, extraACR []string) (string, error) {
	acrValues := make([]string, 0, len(c.cfg.ACRValues)+len(extraACR)+1)
	if len(c.cfg.ACRValues) == 0 {
		acrValues = append(acrValues, defaultACR)
	} else {
		acrValues = append(acrValues, c.cfg.ACRValues...)
	}
	acrValues = append(acrValues, extraACR...)

	extra := []oauth2.AuthCodeOption{
		oauth2.SetAuthURLParam("acr_values", strings.Join(acrValues, " ")),
	}
	return oidcrp.BuildAuthURL(c.oauthConfig, state, nonce, pkceVerifier, extra)
}

// mapACR is the authoritative Login.gov ACR -> AOID assurance enum
// translator. The mapping table here is the single source of truth across
// Eden + AOID + AOSentry. Adding new ACR values requires updating this
// function + its exhaustive test in client_test.go.
//
// Unknown values map to "none". The caller (AOID federation handler)
// checks AssuranceLevel against the policy-required minimum and rejects
// with ErrACRMismatch if "none" or below threshold.
//
// References:
//   - 07-RESEARCH.md §A.4 — full mapping table with citations
//   - Login.gov "Authentication Context Class Reference values" docs
func mapACR(rawACR string) string {
	switch rawACR {
	case "urn:acr.login.gov:auth-only":
		return "ial_1"
	case "urn:acr.login.gov:verified":
		return "verified_no_match"
	case "urn:acr.login.gov:verified-facial-match-required":
		return "ial_2"
	case "urn:acr.login.gov:verified-facial-match-preferred":
		return "ial_2_preferred"
	case "http://idmanagement.gov/ns/assurance/aal/2":
		return "aal_2"
	case "http://idmanagement.gov/ns/assurance/aal/2?phishing_resistant=true":
		return "aal_3"
	case "http://idmanagement.gov/ns/assurance/aal/2?hspd12=true":
		return "aal_3_piv"
	default:
		return "none"
	}
}
