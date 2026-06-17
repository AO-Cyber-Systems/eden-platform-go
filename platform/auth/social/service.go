package social

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/aocybersystems/eden-platform-go/platform/auth"
)

// stateTTL bounds how long a social-login state JWT is valid (CSRF + PKCE
// verifier carrier). 10 minutes mirrors the SSO state TTL.
const stateTTL = 10 * time.Minute

// placeholderDomain is the non-routable domain used for email-less identities
// (X always; Facebook when declined). The local part is deterministic per
// identity so retries don't collide on the users.email UNIQUE constraint.
const placeholderDomain = "social.placeholder"

// ProviderConfig holds a provider's OAuth2/OIDC settings. It is intentionally
// inert in this TRD — the registry is keyed and loaded, but behavior
// (issuer/client/scopes wiring and code exchange) is populated by the OIDC
// (09-02) and custom-OAuth2 (09-03) provider TRDs.
type ProviderConfig struct {
	Provider     string
	IssuerURL    string
	ClientID     string
	ClientSecret string
	Scopes       []string
	UsesPKCE     bool
}

// SocialAuthService owns the provider registry and the security-critical
// provision/linking decision for consumer social login. It is NOT
// company-scoped — there is no company_id in any of its operations. Token
// issuance reuses the platform JWTManager + refresh_tokens rotation.
type SocialAuthService struct {
	social            auth.SocialStore
	users             auth.AuthStore
	jwt               *auth.JWTManager
	baseURL           string
	redirectAllowlist []string
	providers         map[string]ProviderConfig

	// callback is the code→tokens completion used by the HTTP callback handler.
	// It defaults to s.HandleCallback; tests inject a canned result to avoid
	// real OIDC discovery/network. formUserField carries Apple's one-time `user`
	// (name) form POST; it is "" for every other provider.
	callback func(ctx context.Context, code, stateJWT, formUserField string) (*auth.AuthResponse, string, error)
}

// NewSocialAuthService constructs a SocialAuthService. The provider registry is
// initialized empty; provider TRDs register inert ProviderConfig entries keyed
// by "google"|"apple"|"microsoft"|"facebook"|"x".
func NewSocialAuthService(social auth.SocialStore, users auth.AuthStore, jwt *auth.JWTManager, baseURL string, allowlist []string) *SocialAuthService {
	s := &SocialAuthService{
		social:            social,
		users:             users,
		jwt:               jwt,
		baseURL:           strings.TrimRight(baseURL, "/"),
		redirectAllowlist: allowlist,
		providers:         make(map[string]ProviderConfig),
	}
	s.callback = s.HandleCallback
	return s
}

// Provision resolves a verified provider Identity to a platform user and issues
// a B2C ML-DSA-65 token pair. It implements the account-linking decision:
//
//  1. Known provider_sub → existing user (repeat sign-in, fast path).
//  2. New identity, verified email, matching user → LINK (no new user).
//  3. New identity, verified email, no user → CREATE user + identity.
//  4. New identity, UNVERIFIED email, matching user → do NOT link; CREATE a new
//     user (account-takeover prevention — Pitfall 3).
//  5. Email-less (X always; Facebook declined) → CREATE user with a
//     deterministic placeholder email, identity email NULL.
//
// Every success path mints a non-empty access token and records a refresh-token
// hash via the auth store.
func (s *SocialAuthService) Provision(ctx context.Context, in Identity) (*auth.AuthResponse, error) {
	if in.Provider == "" || in.ProviderSub == "" {
		return nil, fmt.Errorf("provider and provider_sub are required")
	}

	// 1. Fast path: known provider identity.
	if existing, err := s.social.GetUserIdentityByProviderSub(ctx, in.Provider, in.ProviderSub); err == nil {
		user, err := s.users.GetUserByID(ctx, existing.UserID)
		if err != nil {
			return nil, fmt.Errorf("load user for known identity: %w", err)
		}
		return s.issueTokens(ctx, user)
	}

	email := strings.ToLower(strings.TrimSpace(in.Email))
	canLink := email != "" && in.EmailVerified

	var user auth.User
	var identityEmail *string

	switch {
	case canLink:
		// 2/3. Verified email — link to existing user, else create.
		if existingUser, err := s.users.GetUserByEmail(ctx, email); err == nil {
			user = existingUser // link
		} else {
			created, err := s.users.CreateUser(ctx, email, unusablePasswordHash(), s.displayName(in, email))
			if err != nil {
				return nil, fmt.Errorf("create user: %w", err)
			}
			user = created
		}
		identityEmail = &email

	default:
		// 4/5. Unverified or email-less — never link; create a fresh user with a
		// deterministic placeholder email. The identity email is stored as-is
		// (NULL when the provider gave none) so the audit row is truthful.
		placeholder := s.placeholderEmail(in.Provider, in.ProviderSub)
		created, err := s.users.CreateUser(ctx, placeholder, unusablePasswordHash(), s.displayName(in, placeholder))
		if err != nil {
			return nil, fmt.Errorf("create placeholder user: %w", err)
		}
		user = created
		if email != "" {
			identityEmail = &email
		} // else leave nil → identity email NULL
	}

	if _, err := s.social.UpsertUserIdentity(ctx, auth.UserIdentity{
		UserID:      user.ID,
		Provider:    in.Provider,
		ProviderSub: in.ProviderSub,
		Email:       identityEmail,
		IsVerified:  in.EmailVerified,
		DisplayName: in.DisplayName,
		AvatarURL:   in.AvatarURL,
		RawClaims:   in.RawClaims,
	}); err != nil {
		return nil, fmt.Errorf("upsert user identity: %w", err)
	}

	return s.issueTokens(ctx, user)
}

// issueTokens mints a B2C ML-DSA-65 access+refresh pair (empty company/role,
// like SignInNavigators) and records the refresh-token hash for rotation.
func (s *SocialAuthService) issueTokens(ctx context.Context, user auth.User) (*auth.AuthResponse, error) {
	accessToken, err := s.jwt.CreateAccessToken(user.ID.String(), "", "", 0, nil)
	if err != nil {
		return nil, fmt.Errorf("create access token: %w", err)
	}
	refreshToken, err := s.jwt.CreateRefreshToken(user.ID.String())
	if err != nil {
		return nil, fmt.Errorf("create refresh token: %w", err)
	}
	tokenHash := auth.HashToken(refreshToken)
	if err := s.users.CreateRefreshToken(ctx, user.ID, tokenHash, time.Now().Add(s.refreshTokenExpiry())); err != nil {
		return nil, fmt.Errorf("store refresh token: %w", err)
	}
	return &auth.AuthResponse{AccessToken: accessToken, RefreshToken: refreshToken, User: user}, nil
}

// placeholderEmail builds a deterministic, non-routable email for an email-less
// or unverified identity: noemail+<provider>-<short-hash(sub)>@social.placeholder.
// Stable per identity so retries don't collide on users.email UNIQUE.
func (s *SocialAuthService) placeholderEmail(provider, sub string) string {
	h := sha256.Sum256([]byte(provider + "|" + sub))
	short := hex.EncodeToString(h[:])[:12]
	return fmt.Sprintf("noemail+%s-%s@%s", provider, short, placeholderDomain)
}

// displayName resolves a display name for a newly created user, falling back to
// the local part of the email when the provider supplied none.
func (s *SocialAuthService) displayName(in Identity, email string) string {
	if dn := strings.TrimSpace(in.DisplayName); dn != "" {
		return dn
	}
	return strings.SplitN(email, "@", 2)[0]
}

func (s *SocialAuthService) refreshTokenExpiry() time.Duration {
	// Mirror the platform default; the JWTManager owns the authoritative value
	// but does not expose it, so use the same 7-day window as password login.
	return 7 * 24 * time.Hour
}

// createStateJWT signs the social-login flow context into a short-lived token.
// Subject shape: provider|redirectURI|pkceVerifier|nonce — NO company_id.
func (s *SocialAuthService) createStateJWT(provider, redirectURI, pkceVerifier, nonce string) (string, error) {
	subject := strings.Join([]string{provider, redirectURI, pkceVerifier, nonce}, "|")
	return s.jwt.CreateShortLivedToken(subject, stateTTL)
}

// parseStateJWT validates and decodes a social-login state JWT.
func (s *SocialAuthService) parseStateJWT(stateJWT string) (provider, redirectURI, pkceVerifier, nonce string, err error) {
	subject, err := s.jwt.ValidateShortLivedToken(stateJWT)
	if err != nil {
		return "", "", "", "", fmt.Errorf("invalid state: %w", err)
	}
	parts := strings.SplitN(subject, "|", 4)
	if len(parts) != 4 {
		return "", "", "", "", fmt.Errorf("malformed state")
	}
	return parts[0], parts[1], parts[2], parts[3], nil
}

// isAllowedRedirectURI reports whether uri is permitted as a post-auth redirect
// target. Guards against open-redirect token theft (Pitfall 4): a redirect is
// allowed only when it exactly matches, or is prefixed by, an allowlisted URI.
func (s *SocialAuthService) isAllowedRedirectURI(uri string) bool {
	if uri == "" {
		return false
	}
	for _, allowed := range s.redirectAllowlist {
		if allowed != "" && strings.HasPrefix(uri, allowed) {
			return true
		}
	}
	return false
}

// unusablePasswordHash returns a sentinel password hash that no password can
// match, disabling password login for social-only accounts.
func unusablePasswordHash() string {
	return "social:no-password"
}
