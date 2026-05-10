package federation

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"sync"
	"time"

	platsaml "github.com/aocybersystems/eden-platform-go/platform/auth/saml"
	"github.com/aocybersystems/eden-platform-go/platform/auth/saml/idp"
	"github.com/google/uuid"
)

// IdPManager constructs and caches per-tenant SAML IdP instances. The
// Phase A flow:
//
//  1. Boot calls NewIdPManager(registry, resolver).
//  2. HTTP handlers call mgr.Metadata(tenantID) / mgr.IssueAssertion(...).
//  3. The first call for a tenant builds an *idp.IdentityProvider from
//     the registry config + the resolved signing key; subsequent calls
//     reuse the cached instance until the cache is invalidated (e.g.
//     after Registry.Update writes a new revision).
//
// Cache invalidation: callers MUST invoke Invalidate(tenantID) after
// any registry write, otherwise the cached IdP keeps serving the old
// AllowedSPs map. The HTTP admin handlers in TRD 31-05 do this.
type IdPManager struct {
	reg      Registry
	resolver KeyResolver

	mu    sync.RWMutex
	cache map[uuid.UUID]*idp.IdentityProvider
}

// NewIdPManager returns a new manager wired to the given registry and
// key resolver. Returns an error when either is nil.
func NewIdPManager(reg Registry, resolver KeyResolver) (*IdPManager, error) {
	if reg == nil {
		return nil, fmt.Errorf("federation: NewIdPManager: registry is required")
	}
	if resolver == nil {
		return nil, fmt.Errorf("federation: NewIdPManager: resolver is required")
	}
	return &IdPManager{
		reg:      reg,
		resolver: resolver,
		cache:    make(map[uuid.UUID]*idp.IdentityProvider),
	}, nil
}

// Get returns the tenant's IdentityProvider, constructing it on the
// first call. Returns ErrTenantNotFound / ErrTenantInactive as
// appropriate.
func (m *IdPManager) Get(ctx context.Context, tenantID uuid.UUID) (*idp.IdentityProvider, error) {
	m.mu.RLock()
	if cached, ok := m.cache[tenantID]; ok {
		m.mu.RUnlock()
		return cached, nil
	}
	m.mu.RUnlock()

	cfg, err := m.reg.Get(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if !cfg.IsActive {
		return nil, ErrTenantInactive
	}

	current, previous, err := m.resolver.Resolve(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("federation: resolve key: %w", err)
	}

	idpCfg := idp.Config{
		EntityID:          cfg.EntityID,
		SSOURL:            cfg.SSOURL,
		CurrentKey:        current,
		PreviousKey:       previous,
		AssertionLifetime: cfg.AssertionLifetime,
		AllowedSPs:        convertAllowedSPs(cfg.AllowedSPs),
	}
	provider, err := idp.New(idpCfg)
	if err != nil {
		return nil, fmt.Errorf("federation: construct IdP: %w", err)
	}

	m.mu.Lock()
	// Double-check after lock upgrade.
	if cached, ok := m.cache[tenantID]; ok {
		m.mu.Unlock()
		return cached, nil
	}
	m.cache[tenantID] = provider
	m.mu.Unlock()
	return provider, nil
}

// Metadata returns the tenant's SAML metadata XML.
func (m *IdPManager) Metadata(ctx context.Context, tenantID uuid.UUID) ([]byte, error) {
	provider, err := m.Get(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return provider.Metadata()
}

// IssueAssertion delegates to the tenant's IdP. Callers populate the
// AssertionInput from the AO ID user record + the federation request's
// `InResponseTo` value.
func (m *IdPManager) IssueAssertion(ctx context.Context, tenantID uuid.UUID, in idp.AssertionInput) ([]byte, error) {
	provider, err := m.Get(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return provider.IssueAssertion(in)
}

// AcceptAuthnRequest decodes a base64 SAMLRequest and looks up the
// referenced SP in the tenant's AllowedSPs.
func (m *IdPManager) AcceptAuthnRequest(ctx context.Context, tenantID uuid.UUID, samlRequest string) (idp.SPRegistration, string, error) {
	provider, err := m.Get(ctx, tenantID)
	if err != nil {
		return idp.SPRegistration{}, "", err
	}
	return provider.AcceptAuthnRequest(samlRequest)
}

// Invalidate drops the cached IdP for a tenant; the next Get rebuilds
// it from the registry. Idempotent for unknown tenants.
func (m *IdPManager) Invalidate(tenantID uuid.UUID) {
	m.mu.Lock()
	delete(m.cache, tenantID)
	m.mu.Unlock()
}

// InvalidateAll drops every cached IdP. Useful after bulk-rotating the
// shared signing key.
func (m *IdPManager) InvalidateAll() {
	m.mu.Lock()
	m.cache = make(map[uuid.UUID]*idp.IdentityProvider)
	m.mu.Unlock()
}

// AttributeTemplate is exposed for the HTTP layer that builds
// AssertionInput from the AO ID user record + the tenant's template.
func (m *IdPManager) AttributeTemplate(ctx context.Context, tenantID uuid.UUID) (map[string][]string, error) {
	cfg, err := m.reg.Get(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]string, len(cfg.AttributeTemplate))
	for k, v := range cfg.AttributeTemplate {
		cloned := make([]string, len(v))
		copy(cloned, v)
		out[k] = cloned
	}
	return out, nil
}

// AssertionLifetime returns the tenant's configured assertion lifetime
// (or the default 5 minutes when unset).
func (m *IdPManager) AssertionLifetime(ctx context.Context, tenantID uuid.UUID) (time.Duration, error) {
	cfg, err := m.reg.Get(ctx, tenantID)
	if err != nil {
		return 0, err
	}
	if cfg.AssertionLifetime <= 0 {
		return 5 * time.Minute, nil
	}
	return cfg.AssertionLifetime, nil
}

// convertAllowedSPs adapts the federation SP registration shape to the
// underlying idp package's shape. The SigningCertificatePEM is parsed
// here (errors silently demote to "no signature verification"
// matching idp.SPRegistration zero-value behavior).
func convertAllowedSPs(in map[string]SPRegistration) map[string]idp.SPRegistration {
	out := make(map[string]idp.SPRegistration, len(in))
	for k, v := range in {
		entry := idp.SPRegistration{
			EntityID: v.EntityID,
			ACSURL:   v.ACSURL,
		}
		if v.SigningCertificatePEM != "" {
			if cert := parseCertPEM(v.SigningCertificatePEM); cert != nil {
				entry.SigningCertificate = cert
			}
		}
		out[k] = entry
	}
	return out
}

// parseCertPEM returns the parsed certificate or nil on any error.
// Matches Obj 23's tolerant behavior for SP signature verification.
func parseCertPEM(pemStr string) *x509.Certificate {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil
	}
	return cert
}

// MustGenerateSharedKey returns a SharedKeyResolver backed by a freshly
// generated 1-year cert with the supplied common name. Suitable for
// boot from in-memory composition; production callers should plumb a
// loaded key in via LoadSigningKey.
func MustGenerateSharedKey(commonName string) (*SharedKeyResolver, error) {
	key, err := platsaml.GenerateSigningKey(commonName, 365*24*time.Hour)
	if err != nil {
		return nil, fmt.Errorf("federation: generate shared key: %w", err)
	}
	return &SharedKeyResolver{Current: key}, nil
}
