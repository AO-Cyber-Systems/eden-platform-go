package social

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"golang.org/x/oauth2"
)

// X (Twitter) OAuth2 endpoints. X uses OAuth2 with PKCE as a PUBLIC client (no
// client secret). It is NOT OIDC — there is no id_token; trust comes from the
// PKCE-protected TLS code exchange + the users/me call. X API v2 NEVER returns an
// email, so the X path is ALWAYS email-less (Pitfall 5).
const (
	xAuthURL    = "https://twitter.com/i/oauth2/authorize"
	xTokenURL   = "https://api.twitter.com/2/oauth2/token"
	xUsersMeURL = "https://api.twitter.com/2/users/me"
)

// xScopes are the minimum scopes for reading the authenticated user. NO email
// scope exists on X — none is requested. offline.access yields a refresh token.
var xScopes = []string{"tweet.read", "users.read", "offline.access"}

// xOAuthConfig builds the oauth2.Config for X's PKCE flow. AuthStyleInHeader is
// required by X's token endpoint for public clients.
func (s *SocialAuthService) xOAuthConfig() oauth2.Config {
	pc := s.providers["x"]
	return oauth2.Config{
		ClientID: pc.ClientID,
		Endpoint: oauth2.Endpoint{
			AuthURL:   xAuthURL,
			TokenURL:  xTokenURL,
			AuthStyle: oauth2.AuthStyleInHeader,
		},
		RedirectURL: s.baseURL + "/auth/social/callback",
		Scopes:      xScopes,
	}
}

// initiateX starts X's server-side PKCE flow. The server generates the PKCE
// verifier, stores it in the signed state JWT (so the callback can recover it for
// the token Exchange — Pitfall 9), and builds the authorization URL carrying an
// S256 code_challenge. The redirect_uri is validated against the allowlist first
// (Pitfall 4).
func (s *SocialAuthService) initiateX(redirectURI string) (authURL, state string, err error) {
	if !s.isAllowedRedirectURI(redirectURI) {
		return "", "", fmt.Errorf("redirect_uri not allowed")
	}

	verifier := oauth2.GenerateVerifier()
	nonce, err := randomNonce()
	if err != nil {
		return "", "", fmt.Errorf("generate nonce: %w", err)
	}
	state, err = s.createStateJWT("x", redirectURI, verifier, nonce)
	if err != nil {
		return "", "", fmt.Errorf("create state: %w", err)
	}

	cfg := s.xOAuthConfig()
	authURL = cfg.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))
	return authURL, state, nil
}

// exchangeX exchanges the authorization code for an access token, passing the
// PKCE verifier recovered from the state JWT. A 401 here usually means the
// verifier did not match the challenge sent at initiate.
func exchangeX(ctx context.Context, cfg oauth2.Config, code, verifier string) (string, error) {
	token, err := cfg.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return "", fmt.Errorf("exchange x code: %w", err)
	}
	if token.AccessToken == "" {
		return "", fmt.Errorf("x token response missing access_token")
	}
	return token.AccessToken, nil
}

// fetchXUser calls users/me with the access token. NO email is requested or
// expected — X API v2 never returns one.
func fetchXUser(ctx context.Context, accessToken string) (id, name string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, xUsersMeURL+"?user.fields=id,name,username", nil)
	if err != nil {
		return "", "", fmt.Errorf("create x users/me request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := socialHTTPClient().Do(req)
	if err != nil {
		return "", "", fmt.Errorf("call x users/me: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", "", fmt.Errorf("x users/me returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", fmt.Errorf("decode x response: %w", err)
	}
	return result.Data.ID, result.Data.Name, nil
}

// xToIdentity maps a users/me response into an Identity. X is ALWAYS email-less:
// Email "" and EmailVerified false force Provision's placeholder path.
func xToIdentity(id, name string) Identity {
	return Identity{
		Provider:      "x",
		ProviderSub:   id,
		Email:         "",
		EmailVerified: false,
		DisplayName:   name,
	}
}
