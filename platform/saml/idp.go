package saml

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/x509"
	"errors"
	"fmt"
	"net/url"

	"github.com/crewjam/saml"
	"github.com/crewjam/saml/logger"
	"github.com/crewjam/saml/samlidp"
	dsig "github.com/russellhaering/goxmldsig"

	forkdsig "github.com/aocybersystems/eden-platform-go/internal/forked/goxmldsig"
)

// Sentinel errors for NewIDP — AOID's federation admin RPC maps each to a
// distinct operator-facing message.
var (
	ErrIDPMissingCert     = errors.New("saml/idp: Cert is nil")
	ErrIDPMissingSigner   = errors.New("saml/idp: Signer is nil")
	ErrIDPMissingStore    = errors.New("saml/idp: Store is nil")
	ErrIDPMissingURL      = errors.New("saml/idp: URL is nil")
	ErrIDPMissingEntityID = errors.New("saml/idp: EntityID is empty")
)

// IDPOptions configures a per-tenant SAML IdP server.
//
// AOID's outbound SAML IdP (TRD 06-09) builds these from
// federation_idp_outbound rows, one per tenant + downstream-SP combination.
// The factory wraps crewjam's *samlidp.Server, patching the underlying
// saml.IdentityProvider to use AOID-supplied SessionProvider /
// ServiceProviderProvider / AssertionMaker hooks (samlidp's defaults read
// from Store; AOID delegates to its DB-backed federation service).
type IDPOptions struct {
	// TenantSlug — log context only, never on the wire.
	TenantSlug string
	// EntityID is the IdP's entityID, advertised in metadata.
	EntityID string
	// URL is the base URL for the IdP HTTP routes (/metadata, /sso,
	// /login). Required.
	URL *url.URL
	// MetadataURL overrides the metadata location embedded in the
	// generated EntityDescriptor. If nil, samlidp's default
	// (URL + /metadata) is used.
	MetadataURL *url.URL
	// SSOURL overrides the SSO endpoint location embedded in metadata.
	// If nil, samlidp's default (URL + /sso) is used.
	SSOURL *url.URL
	// LogoutURL is the SLO endpoint to advertise. Optional.
	LogoutURL *url.URL
	// Cert is the X.509 certificate this IdP advertises in metadata and
	// uses to sign assertions. Required.
	Cert *x509.Certificate
	// Intermediates are intermediate certificates appended after Cert
	// in the KeyInfo block. Optional.
	Intermediates []*x509.Certificate
	// Signer signs SAML assertions. MUST be a crypto.Signer; typically a
	// KMSSigner. Required.
	Signer crypto.Signer
	// Store is the samlidp persistence layer. AOID passes a DB-backed
	// store in production; tests use an in-memory map. Required.
	Store samlidp.Store
	// ServiceProviderProvider, if non-nil, replaces samlidp's default
	// Store-backed lookup. AOID supplies a DB-backed provider keyed by
	// the inbound SP's entityID.
	ServiceProviderProvider saml.ServiceProviderProvider
	// SessionProvider, if non-nil, replaces samlidp's default
	// Store-backed session lookup. AOID supplies its scs-backed
	// SessionProvider.
	SessionProvider saml.SessionProvider
	// AssertionMaker, if non-nil, replaces saml.DefaultAssertionMaker.
	// AOID supplies a maker that pulls attributes from the federation
	// service.
	AssertionMaker saml.AssertionMaker
	// Logger for the underlying samlidp server. If nil, samlidp's
	// DefaultLogger is used.
	Logger logger.Interface
}

// NewIDP constructs a *samlidp.Server with KMS-backed signing. The
// returned server has:
//
//   - server.IDP.Signer set to opts.Signer (crypto.Signer; a KMSSigner)
//   - server.IDP.Certificate set to opts.Cert
//   - server.IDP.SignatureMethod set to RSASHA256 or ECDSASHA256 based on
//     opts.Cert's public-key type
//   - server.IDP.MetadataURL / SSOURL / LogoutURL overlaid if non-nil
//   - server.IDP.ServiceProviderProvider / SessionProvider /
//     AssertionMaker overlaid if non-nil
//
// crewjam's samlidp.New is called with Key=nil (the deprecated
// *rsa.PrivateKey path) — Signer is the load-bearing field.
func NewIDP(opts IDPOptions) (*samlidp.Server, error) {
	if opts.Cert == nil {
		return nil, ErrIDPMissingCert
	}
	if opts.Signer == nil {
		return nil, ErrIDPMissingSigner
	}
	if opts.Store == nil {
		return nil, ErrIDPMissingStore
	}
	if opts.URL == nil {
		return nil, ErrIDPMissingURL
	}
	if opts.EntityID == "" {
		return nil, ErrIDPMissingEntityID
	}

	server, err := samlidp.New(samlidp.Options{
		URL:         *opts.URL,
		Key:         nil, // deprecated *rsa.PrivateKey path; we use Signer
		Signer:      opts.Signer,
		Logger:      opts.Logger,
		Certificate: opts.Cert,
		Store:       opts.Store,
	})
	if err != nil {
		return nil, fmt.Errorf("saml/idp: samlidp.New: %w", err)
	}

	// Patch URLs to AOID-controlled values (samlidp.New computes them
	// from opts.URL with hard-coded path suffixes).
	if opts.MetadataURL != nil {
		server.IDP.MetadataURL = *opts.MetadataURL
	}
	if opts.SSOURL != nil {
		server.IDP.SSOURL = *opts.SSOURL
	}
	if opts.LogoutURL != nil {
		server.IDP.LogoutURL = *opts.LogoutURL
	}
	if len(opts.Intermediates) > 0 {
		server.IDP.Intermediates = opts.Intermediates
	}

	// Choose SignatureMethod from the cert's public-key type so
	// crewjam's signingContext() doesn't default to SHA-1.
	sigMethod, err := signatureMethodForIDPCert(opts.Cert)
	if err != nil {
		return nil, err
	}
	server.IDP.SignatureMethod = sigMethod

	// Overlay the AOID-supplied SP / session / assertion hooks
	// (samlidp's defaults read from Store; AOID's flow uses its own DB).
	if opts.ServiceProviderProvider != nil {
		server.IDP.ServiceProviderProvider = opts.ServiceProviderProvider
	}
	if opts.SessionProvider != nil {
		server.IDP.SessionProvider = opts.SessionProvider
	}
	if opts.AssertionMaker != nil {
		server.IDP.AssertionMaker = opts.AssertionMaker
	}

	return server, nil
}

// NewIDPSigningContext returns the same *dsig.SigningContext that crewjam's
// internal IdpAuthnRequest.signingContext() would produce for an IDP
// configured with signer + cert. AOID's outbound SAML assertion-maker
// (TRD 06-09) uses this directly when it has already built the assertion
// XML and wants to sign without going through the full HTTP flow.
//
// The context is bound to the SHA-256 algorithm matching cert's public-key
// type (RSA-SHA256 for *rsa.PublicKey, ECDSA-SHA256 for *ecdsa.PublicKey).
func NewIDPSigningContext(signer crypto.Signer, cert *x509.Certificate) (*dsig.SigningContext, error) {
	if signer == nil {
		return nil, ErrIDPMissingSigner
	}
	if cert == nil {
		return nil, ErrIDPMissingCert
	}
	ctx, err := forkdsig.NewSigningContextSigner(signer, [][]byte{cert.Raw})
	if err != nil {
		return nil, fmt.Errorf("saml/idp: NewSigningContextSigner: %w", err)
	}
	sigMethod, err := signatureMethodForIDPCert(cert)
	if err != nil {
		return nil, err
	}
	if err := ctx.SetSignatureMethod(sigMethod); err != nil {
		return nil, fmt.Errorf("saml/idp: SetSignatureMethod: %w", err)
	}
	return ctx, nil
}

// signatureMethodForIDPCert returns the W3C signature-method URI matching
// cert's public-key type. ES256 / RS256 only — AOID does not permit SHA-1
// or RSA-with-SHA-384/512 on the SAML wire (the SAML-specific signature
// methods exist but interop is poor outside SHA-256).
func signatureMethodForIDPCert(cert *x509.Certificate) (string, error) {
	switch cert.PublicKey.(type) {
	case *rsa.PublicKey:
		return dsig.RSASHA256SignatureMethod, nil
	case *ecdsa.PublicKey:
		return dsig.ECDSASHA256SignatureMethod, nil
	default:
		return "", fmt.Errorf("saml/idp: unsupported public-key type %T (want RSA or ECDSA P-256)", cert.PublicKey)
	}
}
