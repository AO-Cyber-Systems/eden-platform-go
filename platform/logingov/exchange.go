package logingov

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	oidc "github.com/coreos/go-oidc/v3/oidc"
)

// tokenResponse is the subset of the OIDC token-endpoint JSON body we
// consume. Login.gov returns additional fields (e.g., scope, token_type
// always = "Bearer") which we ignore — letting the JSON decoder skip
// unknowns is the safest forward-compat posture.
type tokenResponse struct {
	IDToken     string `json:"id_token"`
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

// Exchange completes the Login.gov authorization-code flow.
//
// Implementation flow:
//
//  1. SignClientAssertion — produce an RFC 7523 §2.2 client_assertion
//     JWT signed with the RP's private key. The assertion's aud claim
//     is the token endpoint URL exactly (NOT the issuer URL — Login.gov
//     enforces the distinction).
//  2. POST to the token endpoint manually. We do NOT use
//     oauth2.Config.Exchange or oidcrp.ExchangeAndVerify because both
//     internally use Config.Exchange, which only supports
//     client_secret_basic / client_secret_post. Login.gov requires
//     private_key_jwt.
//  3. Decode the token response JSON. Reject non-200 responses with
//     ErrTokenEndpointStatus (status code preserved for caller audit).
//  4. Verify the ID token via the cached *oidc.IDTokenVerifier (pinned
//     to RS256 — Login.gov tokens are always RS256-signed).
//  5. Extract claims into a map; compare the nonce claim to storedNonce.
//     Mismatch → ErrNonceMismatch (CRITICAL: indicates possible replay).
//  6. Run mapACR over the raw acr claim → AOID assurance enum.
//  7. Return the populated *ID.
//
// Parameters:
//
//   - ctx: cancellation + deadline for both the token POST and ID
//     token verification (the verifier may fetch JWKS).
//   - code: the authorization code from the /federate/logingov/callback
//     query string.
//   - pkceVerifier: the original code_verifier the caller passed to
//     BuildAuthURL (retrieved from the InFlightStore by nonce).
//   - storedNonce: the nonce the caller passed to BuildAuthURL (also
//     retrieved from the InFlightStore).
//
// Errors:
//   - ErrNonceMismatch — nonce in ID token differs from storedNonce.
//   - ErrTokenEndpointStatus — token endpoint returned non-200; the
//     wrapped error preserves the status code + body excerpt.
//   - Generic wrapped errors for assertion signing failure, network
//     errors, ID token verification failure, JSON decode failure, etc.
func (c *Client) Exchange(ctx context.Context, code, pkceVerifier, storedNonce string) (*ID, error) {
	assertion, assertionType, err := SignClientAssertion(c.cfg.ClientID, c.tokenEndpoint, c.cfg.SigningKey, c.cfg.SigningKID)
	if err != nil {
		return nil, fmt.Errorf("logingov.Exchange: sign client_assertion: %w", err)
	}

	form := url.Values{
		"grant_type":            []string{"authorization_code"},
		"code":                  []string{code},
		"redirect_uri":          []string{c.cfg.RedirectURL},
		"code_verifier":         []string{pkceVerifier},
		"client_assertion_type": []string{assertionType},
		"client_assertion":      []string{assertion},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("logingov.Exchange: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("logingov.Exchange: POST token endpoint: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		// Read up to 4 KiB for the audit excerpt — enough to capture the
		// error body without unbounded memory growth on a hostile server.
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("%w: status=%d body=%s",
			ErrTokenEndpointStatus, resp.StatusCode, string(body))
	}

	var tr tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, fmt.Errorf("logingov.Exchange: decode token response: %w", err)
	}
	if tr.IDToken == "" {
		return nil, fmt.Errorf("logingov.Exchange: response missing id_token")
	}

	// Verify the ID token via the shared verifier cache (RS256-pinned
	// because Login.gov only signs with RS256).
	verifier := c.verifierCache.Get(
		"logingov:"+c.cfg.TenantID,
		c.provider,
		c.cfg.ClientID,
		[]string{oidc.RS256},
	)
	idt, err := verifier.Verify(ctx, tr.IDToken)
	if err != nil {
		return nil, fmt.Errorf("logingov.Exchange: verify id_token: %w", err)
	}

	var claims map[string]any
	if err := idt.Claims(&claims); err != nil {
		return nil, fmt.Errorf("logingov.Exchange: decode id_token claims: %w", err)
	}

	// Nonce binding. CRITICAL: do not skip — a missing or mismatched
	// nonce indicates possible replay or session-hijack.
	gotNonce, _ := claims["nonce"].(string)
	if gotNonce != storedNonce {
		return nil, ErrNonceMismatch
	}

	id := &ID{
		Sub:           toString(claims["sub"]),
		Email:         toString(claims["email"]),
		EmailVerified: toBool(claims["email_verified"]),
		ACR:           toString(claims["acr"]),
		RawClaims:     claims,
	}
	id.AssuranceLevel = mapACR(id.ACR)
	return id, nil
}

// toString returns v as a string if it is one, else "".
func toString(v any) string {
	s, _ := v.(string)
	return s
}

// toBool returns v as a bool if it is one, else false.
func toBool(v any) bool {
	b, _ := v.(bool)
	return b
}
