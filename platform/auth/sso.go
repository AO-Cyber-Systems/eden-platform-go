package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
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

// InitiateOIDC starts an OIDC authorization code flow for the given company.
// Returns the authorization URL the client should redirect to, plus a state parameter.
func (s *SSOService) InitiateOIDC(ctx context.Context, companyID uuid.UUID) (authURL string, state string, err error) {
	ssoConfig, err := s.store.GetSSOConfig(ctx, companyID, "oidc")
	if err != nil {
		return "", "", fmt.Errorf("OIDC not configured for this company")
	}

	cached, err := s.getOrCreateOIDCProvider(ctx, ssoConfig)
	if err != nil {
		return "", "", fmt.Errorf("create OIDC provider: %w", err)
	}

	// Generate state parameter
	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		return "", "", fmt.Errorf("generate state: %w", err)
	}
	state = base64.URLEncoding.EncodeToString(stateBytes)

	authURL = cached.config.AuthCodeURL(state)
	return authURL, state, nil
}

// HandleOIDCCallback handles the OIDC redirect callback with authorization code.
// Performs JIT provisioning: creates user if not found, links to company.
func (s *SSOService) HandleOIDCCallback(ctx context.Context, code, state string, companyID uuid.UUID) (*AuthResponse, error) {
	ssoConfig, err := s.store.GetSSOConfig(ctx, companyID, "oidc")
	if err != nil {
		return nil, fmt.Errorf("OIDC not configured for this company")
	}

	cached, err := s.getOrCreateOIDCProvider(ctx, ssoConfig)
	if err != nil {
		return nil, fmt.Errorf("create OIDC provider: %w", err)
	}

	// Exchange code for tokens
	oauth2Token, err := cached.config.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("exchange authorization code: %w", err)
	}

	// Extract and verify ID token
	rawIDToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		return nil, fmt.Errorf("no id_token in token response")
	}

	verifier := cached.provider.Verifier(&gooidc.Config{ClientID: ssoConfig.ClientID})
	idToken, err := verifier.Verify(ctx, rawIDToken)
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

	return s.jitProvision(ctx, claims.Email, claims.Name, companyID)
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
	state := r.URL.Query().Get("state")
	companyIDStr := r.URL.Query().Get("company_id")

	if code == "" || state == "" {
		http.Error(w, "missing code or state parameter", http.StatusBadRequest)
		return
	}
	if companyIDStr == "" {
		http.Error(w, "missing company_id parameter", http.StatusBadRequest)
		return
	}

	companyID, err := uuid.Parse(companyIDStr)
	if err != nil {
		http.Error(w, "invalid company_id", http.StatusBadRequest)
		return
	}

	resp, err := s.HandleOIDCCallback(r.Context(), code, state, companyID)
	if err != nil {
		slog.Error("OIDC callback failed", "error", err)
		http.Error(w, "authentication failed", http.StatusInternalServerError)
		return
	}

	// In production, redirect to the Flutter app with tokens.
	// For now, return JSON.
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
	key := cfg.CompanyID.String()

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

	provider, err := gooidc.NewProvider(ctx, cfg.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("discover OIDC provider at %s: %w", cfg.IssuerURL, err)
	}

	oauthConfig := oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  s.baseURL + "/auth/oidc/callback?company_id=" + key,
		Scopes:       []string{gooidc.ScopeOpenID, "profile", "email"},
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
