package social

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

// Facebook OAuth2 / Graph endpoints (v20.0). Facebook is NOT OIDC — there is no
// id_token to JWKS-verify; trust comes from the TLS code exchange + the Graph
// userinfo call with the resulting access token.
const (
	facebookAuthURL   = "https://www.facebook.com/v20.0/dialog/oauth"
	facebookTokenURL  = "https://graph.facebook.com/v20.0/oauth/access_token"
	facebookGraphMeURL = "https://graph.facebook.com/v20.0/me"
)

// facebookScopes request the public profile and (optional) email. Email may be
// absent if the user declines — that is NOT an error (Provision takes the
// email-less path).
var facebookScopes = []string{"public_profile", "email"}

// facebookOAuthConfig builds the oauth2.Config for Facebook's code exchange.
func (s *SocialAuthService) facebookOAuthConfig() oauth2.Config {
	pc := s.providers["facebook"]
	return oauth2.Config{
		ClientID:     pc.ClientID,
		ClientSecret: pc.ClientSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:   facebookAuthURL,
			TokenURL:  facebookTokenURL,
			AuthStyle: oauth2.AuthStyleInParams,
		},
		RedirectURL: s.baseURL + "/auth/social/callback",
		Scopes:      facebookScopes,
	}
}

// exchangeFacebook exchanges the authorization code for a Graph access token.
func exchangeFacebook(ctx context.Context, cfg oauth2.Config, code string) (string, error) {
	token, err := cfg.Exchange(ctx, code)
	if err != nil {
		return "", fmt.Errorf("exchange facebook code: %w", err)
	}
	if token.AccessToken == "" {
		return "", fmt.Errorf("facebook token response missing access_token")
	}
	return token.AccessToken, nil
}

// fetchFacebookUser calls Graph /me?fields=id,name,email with the access token.
// The email is OPTIONAL — an empty string is returned (not an error) when the
// user declined the email permission.
func fetchFacebookUser(ctx context.Context, accessToken string) (id, name, email string, err error) {
	q := url.Values{}
	q.Set("fields", "id,name,email")
	q.Set("access_token", accessToken)
	reqURL := facebookGraphMeURL + "?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", "", "", fmt.Errorf("create facebook me request: %w", err)
	}
	resp, err := socialHTTPClient().Do(req)
	if err != nil {
		return "", "", "", fmt.Errorf("call facebook graph: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", "", "", fmt.Errorf("facebook graph returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", "", fmt.Errorf("decode facebook response: %w", err)
	}
	return result.ID, result.Name, result.Email, nil
}

// facebookToIdentity maps a Graph response into an Identity. EmailVerified is
// ALWAYS false: the Graph API does not assert email ownership, so Provision must
// never auto-link by email (Pitfall 3). A missing email yields Email "" and the
// email-less Provision path.
func facebookToIdentity(id, name, email string) Identity {
	return Identity{
		Provider:      "facebook",
		ProviderSub:   id,
		Email:         strings.ToLower(strings.TrimSpace(email)),
		EmailVerified: false,
		DisplayName:   name,
	}
}

// socialHTTPClient returns the HTTP client used for provider userinfo calls.
func socialHTTPClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
}
