package social

import (
	"context"
	"fmt"
	"os"
)

// customProviders is the set of non-OIDC providers handled by 09-03. Apple has a
// verifiable id_token (via Apple's JWKS) but uses a generated ES256 client secret
// rather than a static secret, so it is registered here too. Facebook and X are
// plain OAuth2 (no id_token).
var customProviders = map[string]bool{
	"apple":    true,
	"facebook": true,
	"x":        true,
}

// RegisterCustomProvider registers an Apple/Facebook/X provider with its client
// credentials. For Apple, clientID is the Services ID and the ES256 client secret
// is generated at callback time from env (.p8 key + team/key IDs). For X (a
// public PKCE client) clientSecret is empty. Like RegisterOIDCProvider, the entry
// is inert config — behavior lives in the per-provider exchange logic.
func (s *SocialAuthService) RegisterCustomProvider(provider, clientID, clientSecret string) {
	s.providers[provider] = ProviderConfig{
		Provider:     provider,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		UsesPKCE:     provider == "x",
	}
}

// Initiate is the single entry point the InitiateSocialLogin RPC calls for ALL
// five providers. It dispatches OIDC providers (google/microsoft) to InitiateOIDC
// and the custom providers (apple/facebook/x) to their initiate logic. Apple and
// Facebook use the standard authorization-code AuthCodeURL with a state JWT; X
// uses server-side PKCE (initiateX).
func (s *SocialAuthService) Initiate(ctx context.Context, provider, redirectURI string) (authURL, state string, err error) {
	if _, ok := oidcPresets[provider]; ok {
		return s.InitiateOIDC(ctx, provider, redirectURI)
	}
	if customProviders[provider] {
		return s.initiateCustom(ctx, provider, redirectURI)
	}
	return "", "", fmt.Errorf("unknown social provider %q", provider)
}

// initiateCustom builds the authorization URL for a custom (non-OIDC) provider.
// X takes the PKCE path; Apple and Facebook take the plain authorization-code
// path with the app redirect carried in the state JWT.
func (s *SocialAuthService) initiateCustom(_ context.Context, provider, redirectURI string) (authURL, state string, err error) {
	if _, ok := s.providers[provider]; !ok {
		return "", "", fmt.Errorf("provider %q not registered", provider)
	}
	if !s.isAllowedRedirectURI(redirectURI) {
		return "", "", fmt.Errorf("redirect_uri not allowed")
	}

	if provider == "x" {
		return s.initiateX(redirectURI)
	}

	nonce, err := randomNonce()
	if err != nil {
		return "", "", fmt.Errorf("generate nonce: %w", err)
	}
	// No PKCE verifier for apple/facebook — the pkce field stays empty.
	state, err = s.createStateJWT(provider, redirectURI, "", nonce)
	if err != nil {
		return "", "", fmt.Errorf("create state: %w", err)
	}

	switch provider {
	case "apple":
		// Client secret is not needed to BUILD the auth URL (only at Exchange),
		// so an empty secret is fine here.
		cfg := s.appleOAuthConfig("")
		authURL = cfg.AuthCodeURL(state)
		// Apple requires response_mode=form_post when name/email scopes are
		// requested so the one-time `user` field is delivered as a form POST.
		authURL += "&response_mode=form_post"
	case "facebook":
		cfg := s.facebookOAuthConfig()
		authURL = cfg.AuthCodeURL(state)
	default:
		return "", "", fmt.Errorf("unsupported custom provider %q", provider)
	}
	return authURL, state, nil
}

// handleCustomCallback completes an Apple/Facebook/X flow: it exchanges the code,
// builds the provider Identity (verifying Apple's id_token via JWKS), and runs
// the shared Provision pipeline. formUserField carries Apple's one-time name POST
// (empty for the other providers). The caller (HandleCallback) has already
// re-validated the redirect allowlist.
func (s *SocialAuthService) handleCustomCallback(ctx context.Context, provider, code, formUserField string) (Identity, error) {
	switch provider {
	case "apple":
		clientSecret, err := s.appleClientSecret()
		if err != nil {
			return Identity{}, fmt.Errorf("apple client secret: %w", err)
		}
		cfg := s.appleOAuthConfig(clientSecret)
		rawIDToken, err := exchangeApple(ctx, cfg, code)
		if err != nil {
			return Identity{}, err
		}
		claims, err := verifyAppleIDToken(ctx, rawIDToken, s.providers["apple"].ClientID)
		if err != nil {
			return Identity{}, err
		}
		return appleClaimsToIdentity(claims, formUserField), nil

	case "facebook":
		cfg := s.facebookOAuthConfig()
		accessToken, err := exchangeFacebook(ctx, cfg, code)
		if err != nil {
			return Identity{}, err
		}
		id, name, email, err := fetchFacebookUser(ctx, accessToken)
		if err != nil {
			return Identity{}, err
		}
		return facebookToIdentity(id, name, email), nil

	case "x":
		return Identity{}, fmt.Errorf("x callback must be handled with its PKCE verifier")

	default:
		return Identity{}, fmt.Errorf("unsupported custom provider %q", provider)
	}
}

// handleXCallback completes X's PKCE flow using the verifier recovered from the
// state JWT, then maps users/me into the (always email-less) Identity.
func (s *SocialAuthService) handleXCallback(ctx context.Context, code, verifier string) (Identity, error) {
	cfg := s.xOAuthConfig()
	accessToken, err := exchangeX(ctx, cfg, code, verifier)
	if err != nil {
		return Identity{}, err
	}
	id, name, err := fetchXUser(ctx, accessToken)
	if err != nil {
		return Identity{}, err
	}
	return xToIdentity(id, name), nil
}

// appleClientSecret generates the ES256 client-secret JWT from environment. The
// .p8 key is read from APPLE_PRIVATE_KEY (PEM inline) or APPLE_PRIVATE_KEY_PATH.
// A future rotation cron is out of scope (Pitfall 1) — it is regenerated per
// callback, which is cheap and always well within Apple's 6-month cap.
func (s *SocialAuthService) appleClientSecret() (string, error) {
	teamID := os.Getenv("APPLE_TEAM_ID")
	keyID := os.Getenv("APPLE_KEY_ID")
	servicesID := s.providers["apple"].ClientID
	if servicesID == "" {
		servicesID = os.Getenv("APPLE_SERVICES_ID")
	}

	keyPEM := os.Getenv("APPLE_PRIVATE_KEY")
	if keyPEM == "" {
		if path := os.Getenv("APPLE_PRIVATE_KEY_PATH"); path != "" {
			b, err := os.ReadFile(path)
			if err != nil {
				return "", fmt.Errorf("read apple private key file: %w", err)
			}
			keyPEM = string(b)
		}
	}
	if keyPEM == "" {
		return "", fmt.Errorf("APPLE_PRIVATE_KEY or APPLE_PRIVATE_KEY_PATH not set")
	}
	return GenerateAppleClientSecret(keyPEM, teamID, servicesID, keyID)
}
