package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/aocybersystems/eden-platform-go/platform/rbac"
	"github.com/google/uuid"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/crewjam/saml"
	"github.com/crewjam/saml/samlsp"
	"golang.org/x/oauth2"
)

// SSOService handles OIDC and SAML SSO flows.
type SSOService struct {
	store      AuthStore
	jwtManager *JWTManager
	baseURL    string

	// Cached providers
	oidcMu        sync.RWMutex
	oidcProviders map[string]*oidcCachedProvider

	// SAML SP certificate (auto-generated for dev)
	samlCert tls.Certificate
}

type oidcCachedProvider struct {
	provider *gooidc.Provider
	config   oauth2.Config
}

// NewSSOService creates a new SSO service.
func NewSSOService(store AuthStore, jwtManager *JWTManager, baseURL string) *SSOService {
	svc := &SSOService{
		store:         store,
		jwtManager:    jwtManager,
		baseURL:       strings.TrimRight(baseURL, "/"),
		oidcProviders: make(map[string]*oidcCachedProvider),
	}

	// Generate self-signed SAML SP certificate for development
	cert, err := generateSelfSignedCert()
	if err != nil {
		slog.Warn("failed to generate SAML SP certificate", "error", err)
	} else {
		svc.samlCert = cert
	}

	return svc
}

// ProviderPreset maps well-known provider names to their OIDC issuer URLs.
var ProviderPresets = map[string]struct {
	IssuerURL   string
	DisplayName string
}{
	"microsoft": {
		IssuerURL:   "https://login.microsoftonline.com/common/v2.0",
		DisplayName: "Microsoft",
	},
	"google": {
		IssuerURL:   "https://accounts.google.com",
		DisplayName: "Google",
	},
}

// InitiateOIDC starts an OIDC authorization code flow for the given company and provider.
// Provider can be "microsoft", "google", or "oidc" (custom).
// redirectURI is encoded into the state JWT so the callback knows where to send the user.
// Returns the authorization URL the client should redirect to, plus the state JWT.
func (s *SSOService) InitiateOIDC(ctx context.Context, companyID uuid.UUID, provider, redirectURI string) (authURL string, state string, err error) {
	if provider == "" {
		provider = "oidc"
	}

	ssoConfig, err := s.store.GetSSOConfig(ctx, companyID, provider)
	if err != nil {
		return "", "", fmt.Errorf("SSO provider %q not configured for this company", provider)
	}

	// Resolve issuer URL from preset if empty
	if ssoConfig.IssuerURL == "" {
		if preset, ok := ProviderPresets[provider]; ok {
			ssoConfig.IssuerURL = preset.IssuerURL
		} else {
			return "", "", fmt.Errorf("no issuer URL configured for provider %q", provider)
		}
	}

	cached, err := s.getOrCreateOIDCProvider(ctx, ssoConfig)
	if err != nil {
		return "", "", fmt.Errorf("create OIDC provider: %w", err)
	}

	// Generate a per-authorization PKCE verifier (RFC 7636, 32 octets). AOID is
	// OAuth 2.1 PKCE-mandatory, so the authorize URL MUST carry an S256
	// code_challenge; the verifier is threaded through the state JWT so it
	// survives the stateless redirect and can be replayed on Exchange. The
	// params are additive and transparent to IdPs (Google/Microsoft/customer
	// OIDC) that do not enforce PKCE.
	verifier := oauth2.GenerateVerifier()

	// Generate state as signed JWT (stateless, works across instances)
	state, err = s.createStateJWT(companyID, provider, redirectURI, verifier)
	if err != nil {
		return "", "", fmt.Errorf("create state: %w", err)
	}

	authURL = cached.config.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))
	return authURL, state, nil
}

// createStateJWT creates a short-lived signed JWT encoding the SSO flow context.
// The PKCE verifier is carried as a 4th "|"-delimited field so it survives the
// stateless redirect. Legacy states written before PKCE carried only the first
// three fields; parseStateJWT still accepts that shape (empty verifier).
func (s *SSOService) createStateJWT(companyID uuid.UUID, provider, redirectURI, verifier string) (string, error) {
	// Reuse JWTManager to sign. Encode context as subject with a 10-minute expiry.
	// Format: "companyID|provider|redirectURI|pkceVerifier"
	subject := companyID.String() + "|" + provider + "|" + redirectURI + "|" + verifier
	return s.jwtManager.CreateShortLivedToken(subject, 10*time.Minute)
}

// parseStateJWT decodes and validates the state JWT, returning companyID,
// provider, redirectURI, and the PKCE verifier.
//
// Back-compat: a legacy 3-field state (companyID|provider|redirectURI, written
// before PKCE) parses cleanly and yields an empty verifier, so an in-flight
// redirect issued just before deploy does not 500. Fewer than 3 fields is still
// malformed. The PKCE addition does not weaken state validation.
func (s *SSOService) parseStateJWT(stateJWT string) (companyID uuid.UUID, provider, redirectURI, verifier string, err error) {
	subject, err := s.jwtManager.ValidateShortLivedToken(stateJWT)
	if err != nil {
		return uuid.Nil, "", "", "", fmt.Errorf("invalid state: %w", err)
	}

	parts := strings.SplitN(subject, "|", 4)
	if len(parts) < 3 {
		return uuid.Nil, "", "", "", fmt.Errorf("malformed state")
	}

	companyID, err = uuid.Parse(parts[0])
	if err != nil {
		return uuid.Nil, "", "", "", fmt.Errorf("invalid company_id in state")
	}

	if len(parts) == 4 {
		verifier = parts[3]
	}

	return companyID, parts[1], parts[2], verifier, nil
}

// HandleOIDCCallbackWithState handles the OIDC redirect callback using the state JWT.
// Extracts companyID and provider from state, exchanges code, provisions user, stores OAuth tokens.
// Returns AuthResponse and the redirectURI from state (so caller knows where to send the user).
func (s *SSOService) HandleOIDCCallbackWithState(ctx context.Context, code, stateJWT string) (*AuthResponse, string, error) {
	companyID, provider, redirectURI, verifier, err := s.parseStateJWT(stateJWT)
	if err != nil {
		return nil, "", fmt.Errorf("invalid state: %w", err)
	}

	resp, err := s.HandleOIDCCallbackWithVerifier(ctx, code, provider, companyID, verifier)
	if err != nil {
		return nil, "", err
	}

	return resp, redirectURI, nil
}

// HandleOIDCCallback handles the OIDC redirect callback with authorization code.
// Performs JIT provisioning: creates user if not found, links to company.
// Stores the OAuth access/refresh tokens for future API use.
//
// This public signature is preserved for backward compatibility. It delegates
// to HandleOIDCCallbackWithVerifier with an empty PKCE verifier, which is
// byte-for-byte the pre-PKCE Exchange behavior (no code_verifier posted).
func (s *SSOService) HandleOIDCCallback(ctx context.Context, code, provider string, companyID uuid.UUID) (*AuthResponse, error) {
	return s.HandleOIDCCallbackWithVerifier(ctx, code, provider, companyID, "")
}

// HandleOIDCCallbackWithVerifier is HandleOIDCCallback with an explicit PKCE
// verifier. When verifier is non-empty, the code exchange posts the matching
// code_verifier (required by AOID's OAuth 2.1 PKCE-mandatory /oauth/token).
// When verifier is empty, NO code_verifier is sent — keeping the exchange
// byte-for-byte identical to the pre-PKCE path so existing Google/Microsoft/
// customer OIDC SSO (and any in-flight legacy state) is unaffected.
func (s *SSOService) HandleOIDCCallbackWithVerifier(ctx context.Context, code, provider string, companyID uuid.UUID, verifier string) (*AuthResponse, error) {
	ssoConfig, err := s.store.GetSSOConfig(ctx, companyID, provider)
	if err != nil {
		return nil, fmt.Errorf("SSO provider %q not configured for this company", provider)
	}

	cached, err := s.getOrCreateOIDCProvider(ctx, ssoConfig)
	if err != nil {
		return nil, fmt.Errorf("create OIDC provider: %w", err)
	}

	// Exchange code for tokens. Only attach the PKCE verifier when present, so
	// the empty-verifier path is identical to the legacy/non-PKCE exchange.
	var exchangeOpts []oauth2.AuthCodeOption
	if verifier != "" {
		exchangeOpts = append(exchangeOpts, oauth2.VerifierOption(verifier))
	}
	oauth2Token, err := cached.config.Exchange(ctx, code, exchangeOpts...)
	if err != nil {
		return nil, fmt.Errorf("exchange authorization code: %w", err)
	}

	// Extract and verify ID token
	rawIDToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		return nil, fmt.Errorf("no id_token in token response")
	}

	// Named idTokenVerifier to avoid shadowing the PKCE code verifier parameter.
	idTokenVerifier := cached.provider.Verifier(&gooidc.Config{ClientID: ssoConfig.ClientID})
	idToken, err := idTokenVerifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("verify ID token: %w", err)
	}

	// Extract claims
	var claims struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("extract ID token claims: %w", err)
	}

	if claims.Email == "" {
		return nil, fmt.Errorf("email claim not found in ID token")
	}

	authResp, err := s.jitProvision(ctx, claims.Email, claims.Name, companyID)
	if err != nil {
		return nil, err
	}

	// Store the provider's OAuth tokens for future API use (email sync, calendar, etc.)
	if oauth2Token.AccessToken != "" {
		_ = s.store.UpsertOAuthCredential(ctx, OAuthCredential{
			CompanyID:    companyID,
			UserID:       authResp.User.ID,
			Provider:     provider,
			AccessToken:  oauth2Token.AccessToken,
			RefreshToken: oauth2Token.RefreshToken,
			TokenExpiry:  oauth2Token.Expiry,
			Scopes:       ssoConfig.ExtraScopes,
		})
	}

	return authResp, nil
}

// InitiateSAML starts a SAML authentication flow for the given company.
func (s *SSOService) InitiateSAML(ctx context.Context, companyID uuid.UUID) (string, error) {
	ssoConfig, err := s.store.GetSSOConfig(ctx, companyID, "saml")
	if err != nil {
		return "", fmt.Errorf("SAML not configured for this company")
	}

	sp, err := s.createSAMLSP(ssoConfig)
	if err != nil {
		return "", fmt.Errorf("create SAML SP: %w", err)
	}

	// Build AuthnRequest
	authnReq, err := sp.MakeAuthenticationRequest(sp.GetSSOBindingLocation(saml.HTTPRedirectBinding), saml.HTTPRedirectBinding, saml.HTTPPostBinding)
	if err != nil {
		return "", fmt.Errorf("create SAML authn request: %w", err)
	}

	redirectURL, err := authnReq.Redirect("", sp)
	if err != nil {
		return "", fmt.Errorf("build SAML redirect URL: %w", err)
	}

	return redirectURL.String(), nil
}

// HandleSAMLCallback processes a SAML assertion from an HTTP request.
func (s *SSOService) HandleSAMLCallback(r *http.Request, companyID uuid.UUID) (*AuthResponse, error) {
	ssoConfig, err := s.store.GetSSOConfig(r.Context(), companyID, "saml")
	if err != nil {
		return nil, fmt.Errorf("SAML not configured for this company")
	}

	sp, err := s.createSAMLSP(ssoConfig)
	if err != nil {
		return nil, fmt.Errorf("create SAML SP: %w", err)
	}

	assertion, err := sp.ParseResponse(r, []string{})
	if err != nil {
		return nil, fmt.Errorf("validate SAML assertion: %w", err)
	}

	// Extract attributes
	email := getAssertionAttribute(assertion, "email", "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress")
	name := getAssertionAttribute(assertion, "displayName", "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/name")

	if email == "" {
		return nil, fmt.Errorf("email attribute not found in SAML assertion")
	}

	return s.jitProvision(r.Context(), email, name, companyID)
}

// RegisterHTTPHandlers registers SSO callback HTTP handlers on the given mux.
func (s *SSOService) RegisterHTTPHandlers(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/oidc/callback", s.handleOIDCCallbackHTTP)
	mux.HandleFunc("POST /auth/saml/acs", s.handleSAMLACSHTTP)
	mux.HandleFunc("GET /auth/saml/metadata", s.handleSAMLMetadataHTTP)

	slog.Info("registered SSO HTTP endpoints",
		"oidc_callback", "/auth/oidc/callback",
		"saml_acs", "/auth/saml/acs",
		"saml_metadata", "/auth/saml/metadata",
	)
}

func (s *SSOService) handleOIDCCallbackHTTP(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	stateJWT := r.URL.Query().Get("state")

	if code == "" || stateJWT == "" {
		http.Error(w, "missing code or state parameter", http.StatusBadRequest)
		return
	}

	resp, redirectURI, err := s.HandleOIDCCallbackWithState(r.Context(), code, stateJWT)
	if err != nil {
		slog.Error("OIDC callback failed", "error", err)
		http.Error(w, "authentication failed", http.StatusInternalServerError)
		return
	}

	// Redirect based on the redirectURI encoded in the state JWT.
	if redirectURI != "" && redirectURI != "json" {
		// Append tokens as query params for desktop Flutter or portal redirect.
		sep := "?"
		if strings.Contains(redirectURI, "?") {
			sep = "&"
		}
		target := fmt.Sprintf("%s%saccess_token=%s&refresh_token=%s", redirectURI, sep, resp.AccessToken, resp.RefreshToken)
		http.Redirect(w, r, target, http.StatusFound)
		return
	}

	// Default: return JSON (for API clients or testing).
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"access_token":"%s","refresh_token":"%s"}`, resp.AccessToken, resp.RefreshToken)
}

func (s *SSOService) handleSAMLACSHTTP(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "failed to parse form", http.StatusBadRequest)
		return
	}

	companyIDStr := r.FormValue("RelayState") // We encode company_id in RelayState
	companyID, err := uuid.Parse(companyIDStr)
	if err != nil {
		http.Error(w, "invalid company_id in RelayState", http.StatusBadRequest)
		return
	}

	resp, err := s.HandleSAMLCallback(r, companyID)
	if err != nil {
		slog.Error("SAML ACS callback failed", "error", err)
		http.Error(w, "authentication failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"access_token":"%s","refresh_token":"%s"}`, resp.AccessToken, resp.RefreshToken)
}

func (s *SSOService) handleSAMLMetadataHTTP(w http.ResponseWriter, r *http.Request) {
	// Return basic SP metadata XML
	spURL := s.baseURL
	metadata := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<md:EntityDescriptor xmlns:md="urn:oasis:names:tc:SAML:2.0:metadata"
    entityID="%s/auth/saml/metadata">
  <md:SPSSODescriptor AuthnRequestsSigned="true"
      protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">
    <md:AssertionConsumerService
        Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST"
        Location="%s/auth/saml/acs"
        index="0" isDefault="true"/>
  </md:SPSSODescriptor>
</md:EntityDescriptor>`, spURL, spURL)

	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, metadata)
}

// jitProvision performs Just-In-Time user provisioning for SSO.
// If a user with the email exists, link them to the company. Otherwise, create a new user.
func (s *SSOService) jitProvision(ctx context.Context, email, name string, companyID uuid.UUID) (*AuthResponse, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if name == "" {
		name = strings.Split(email, "@")[0]
	}

	// Try to find existing user
	user, err := s.store.GetUserByEmail(ctx, email)
	if err != nil {
		// User not found -- create new user (no password for SSO users)
		user, err = s.store.CreateUser(ctx, email, "", name)
		if err != nil {
			return nil, fmt.Errorf("create SSO user: %w", err)
		}
	}

	// Ensure company membership exists (try create, ignore duplicate)
	_ = s.store.CreateCompanyMembership(ctx, companyID, user.ID, rbac.MemberRoleID)

	// Get role for token
	role, err := s.store.GetUserRole(ctx, companyID, user.ID)
	roleName := "member"
	roleLevel := 40
	if err == nil {
		roleName = role.Name
		roleLevel = role.RoleLevel
	}

	companyIDStr := companyID.String()
	accessToken, err := s.jwtManager.CreateAccessToken(user.ID.String(), companyIDStr, roleName, roleLevel, []string{companyIDStr})
	if err != nil {
		return nil, fmt.Errorf("create access token: %w", err)
	}

	refreshToken, err := s.jwtManager.CreateRefreshToken(user.ID.String())
	if err != nil {
		return nil, fmt.Errorf("create refresh token: %w", err)
	}

	tokenHash := HashToken(refreshToken)
	if err := s.store.CreateRefreshToken(ctx, user.ID, tokenHash, time.Now().Add(s.jwtManager.config.RefreshTokenExpiry)); err != nil {
		return nil, fmt.Errorf("store refresh token: %w", err)
	}

	return &AuthResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		User:         user,
	}, nil
}

func (s *SSOService) getOrCreateOIDCProvider(ctx context.Context, cfg SSOConfig) (*oidcCachedProvider, error) {
	key := cfg.CompanyID.String() + ":" + cfg.Provider

	s.oidcMu.RLock()
	if cached, ok := s.oidcProviders[key]; ok {
		s.oidcMu.RUnlock()
		return cached, nil
	}
	s.oidcMu.RUnlock()

	s.oidcMu.Lock()
	defer s.oidcMu.Unlock()

	// Double-check
	if cached, ok := s.oidcProviders[key]; ok {
		return cached, nil
	}

	// Resolve issuer URL from preset if needed
	issuerURL := cfg.IssuerURL
	if issuerURL == "" {
		if preset, ok := ProviderPresets[cfg.Provider]; ok {
			issuerURL = preset.IssuerURL
		}
	}

	provider, err := gooidc.NewProvider(ctx, issuerURL)
	if err != nil {
		return nil, fmt.Errorf("discover OIDC provider at %s: %w", issuerURL, err)
	}

	// Base scopes + any extra scopes configured
	scopes := []string{gooidc.ScopeOpenID, "profile", "email"}
	scopes = append(scopes, cfg.ExtraScopes...)

	oauthConfig := oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  s.baseURL + "/auth/oidc/callback",
		Scopes:       scopes,
	}

	cached := &oidcCachedProvider{provider: provider, config: oauthConfig}
	s.oidcProviders[key] = cached
	return cached, nil
}

func (s *SSOService) createSAMLSP(cfg SSOConfig) (*saml.ServiceProvider, error) {
	if cfg.MetadataURL == "" {
		return nil, fmt.Errorf("SAML metadata URL not configured")
	}

	metadataURL, err := url.Parse(cfg.MetadataURL)
	if err != nil {
		return nil, fmt.Errorf("parse metadata URL: %w", err)
	}

	rootURL, err := url.Parse(s.baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse base URL: %w", err)
	}

	idpMetadata, err := samlsp.FetchMetadata(context.Background(), http.DefaultClient, *metadataURL)
	if err != nil {
		return nil, fmt.Errorf("fetch IdP metadata: %w", err)
	}

	sp := saml.ServiceProvider{
		Key:         s.samlCert.PrivateKey.(*rsa.PrivateKey),
		Certificate: s.samlCert.Leaf,
		MetadataURL: *rootURL,
		AcsURL:      *rootURL,
		IDPMetadata: idpMetadata,
	}
	sp.AcsURL.Path = "/auth/saml/acs"
	sp.MetadataURL.Path = "/auth/saml/metadata"

	return &sp, nil
}

func getAssertionAttribute(assertion *saml.Assertion, names ...string) string {
	if assertion == nil {
		return ""
	}
	for _, stmt := range assertion.AttributeStatements {
		for _, attr := range stmt.Attributes {
			for _, name := range names {
				if attr.Name == name || attr.FriendlyName == name {
					if len(attr.Values) > 0 {
						return attr.Values[0].Value
					}
				}
			}
		}
	}
	return ""
}

func generateSelfSignedCert() (tls.Certificate, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generate RSA key: %w", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "Eden Platform SAML SP",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("create certificate: %w", err)
	}

	leaf, err := x509.ParseCertificate(certDER)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("parse certificate: %w", err)
	}

	return tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  key,
		Leaf:        leaf,
	}, nil
}
