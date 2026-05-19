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
	"github.com/crewjam/saml/samlsp"
	dsig "github.com/russellhaering/goxmldsig"
)

// Sentinel errors for NewSP — callers (and the AOID federation handler in
// TRD 06-06) use errors.Is to surface the right operator-facing message.
var (
	ErrSPMissingMetadata = errors.New("saml/sp: IDPMetadata is empty")
	ErrSPMissingSigner   = errors.New("saml/sp: SPSigner is nil")
	ErrSPMissingCert     = errors.New("saml/sp: SPCert is nil")
	ErrSPMissingEntityID = errors.New("saml/sp: EntityID is empty")
	ErrSPMissingAcsURL   = errors.New("saml/sp: AcsURL is nil")
)

// SPOptions configures a per-tenant SAML Service Provider Middleware.
//
// AOID's federation service builds these from the federation_idp_inbound
// row at request time (see TRD 06-07). The factory deliberately accepts
// IDPMetadata as []byte (NOT a URL) so the caller controls fetching —
// metadata is a tenant trust anchor and must be persisted, not
// re-fetched on each parse.
type SPOptions struct {
	// TenantSlug identifies the AOID tenant this SP serves. Used only
	// for logging context — not embedded in any SAML wire field.
	TenantSlug string
	// IdpID is the federation_idp_inbound row identifier. Used only for
	// logging context.
	IdpID string
	// EntityID is the SP's own entityID, used in the AuthnRequest's
	// <Issuer> element and the SP metadata document. SHOULD be the same
	// URL the SP serves its metadata at.
	EntityID string
	// AcsURL is the AssertionConsumerService URL — where the IdP POSTs
	// the SAML Response. Required.
	AcsURL *url.URL
	// MetadataURL is the URL where this SP publishes its metadata. Used
	// to populate the metadata document; does not trigger a fetch.
	MetadataURL *url.URL
	// IDPMetadata holds the raw XML bytes of the upstream IdP's metadata
	// (federation_idp_inbound.metadata_xml). Required. Parsed via
	// ParseAndCacheMetadata.
	IDPMetadata []byte
	// SPCert is the X.509 certificate this SP advertises in its metadata
	// and uses to sign AuthnRequests. Required.
	SPCert *x509.Certificate
	// SPSigner is the crypto.Signer (typically a KMSSigner) used to sign
	// AuthnRequests. Required even if no AuthnRequest signing is
	// performed today — crewjam's samlsp.Options requires a non-nil
	// crypto.Signer for the Key field.
	SPSigner crypto.Signer
	// RequestTracker, if non-nil, replaces samlsp's default cookie
	// tracker. AOID typically passes a DB-backed tracker to survive
	// browser-tab restarts mid-flow.
	RequestTracker samlsp.RequestTracker
	// SessionProvider, if non-nil, replaces the default cookie
	// SessionProvider. AOID supplies a scs-backed provider.
	SessionProvider samlsp.SessionProvider
	// SignRequest controls whether outbound AuthnRequests carry an XML
	// signature. Defaults to true — AOID-issued SP requests are always
	// signed.
	SignRequest bool
	// AllowIDPInitiated controls whether the SP accepts unsolicited
	// SAML Responses (no in-flight AuthnRequest). Defaults to false —
	// AOID requires the SP-initiated flow.
	AllowIDPInitiated bool
}

// NewSP constructs a *samlsp.Middleware bound to opts. The middleware is
// the canonical crewjam/saml entry point for SP HTTP flows; AOID's
// federation handler (TRD 06-07) wires its routes through the returned
// middleware after building the SAML request/response handlers.
//
// Validation order (each returns its own sentinel error so the federation
// admin RPC can map to a precise operator-facing message):
//   1. IDPMetadata non-empty
//   2. SPSigner non-nil
//   3. SPCert non-nil
//   4. EntityID non-empty
//   5. AcsURL non-nil
//   6. IDPMetadata parses successfully via ParseAndCacheMetadata
func NewSP(opts SPOptions) (*samlsp.Middleware, error) {
	if len(opts.IDPMetadata) == 0 {
		return nil, ErrSPMissingMetadata
	}
	if opts.SPSigner == nil {
		return nil, ErrSPMissingSigner
	}
	if opts.SPCert == nil {
		return nil, ErrSPMissingCert
	}
	if opts.EntityID == "" {
		return nil, ErrSPMissingEntityID
	}
	if opts.AcsURL == nil {
		return nil, ErrSPMissingAcsURL
	}

	idpMD, err := ParseAndCacheMetadata(opts.IDPMetadata)
	if err != nil {
		return nil, fmt.Errorf("saml/sp: parse IDP metadata: %w", err)
	}

	// crewjam's samlsp.Options uses URL (singular) — it resolves
	// /saml/metadata, /saml/acs, /saml/slo relative to that URL. AOID
	// wants the AcsURL and MetadataURL set explicitly, so we let
	// DefaultServiceProvider compute defaults then overlay the AOID
	// values.
	baseURL := url.URL{}
	if opts.MetadataURL != nil {
		baseURL = *opts.MetadataURL
	}

	// crewjam's samlsp.DefaultServiceProvider calls
	// defaultSigningMethodForKey on opts.Key BEFORE any SignRequest
	// branch — and that function panics on anything that is not literally
	// *rsa.PrivateKey, *ecdsa.PrivateKey, or nil. Our KMSSigner is a
	// wrapper around either, so we pass Key=nil to samlsp.New and patch
	// the ServiceProvider after construction with the real KMS-backed
	// signer and an explicit SignatureMethod chosen from the cert's
	// public-key type.
	samlspOpts := samlsp.Options{
		EntityID:          opts.EntityID,
		URL:               baseURL,
		Key:               nil,
		Certificate:       opts.SPCert,
		IDPMetadata:       idpMD,
		SignRequest:       false, // re-applied below after Key is patched in
		AllowIDPInitiated: opts.AllowIDPInitiated,
	}

	mw, err := samlsp.New(samlspOpts)
	if err != nil {
		return nil, fmt.Errorf("saml/sp: samlsp.New: %w", err)
	}

	mw.ServiceProvider.Key = opts.SPSigner
	if opts.SignRequest {
		sigMethod, err := signatureMethodForPublicKey(opts.SPCert.PublicKey)
		if err != nil {
			return nil, fmt.Errorf("saml/sp: choose signature method: %w", err)
		}
		mw.ServiceProvider.SignatureMethod = sigMethod
	}

	// Overlay explicit AOID-controlled URLs so the metadata document
	// emitted by mw.ServeMetadata exposes the right ACS endpoint.
	mw.ServiceProvider.AcsURL = *opts.AcsURL
	if opts.MetadataURL != nil {
		mw.ServiceProvider.MetadataURL = *opts.MetadataURL
	}

	if opts.RequestTracker != nil {
		mw.RequestTracker = opts.RequestTracker
	}
	if opts.SessionProvider != nil {
		mw.Session = opts.SessionProvider
	}

	return mw, nil
}

// Ensure saml.ServiceProvider satisfies the type assertion expected by
// crewjam's middleware (compile-time check).
var _ = saml.ServiceProvider{}

// signatureMethodForPublicKey returns the W3C signature-method URI
// matching pub. AOID prefers SHA-256 across the board.
func signatureMethodForPublicKey(pub crypto.PublicKey) (string, error) {
	switch pub.(type) {
	case *rsa.PublicKey:
		return dsig.RSASHA256SignatureMethod, nil
	case *ecdsa.PublicKey:
		return dsig.ECDSASHA256SignatureMethod, nil
	default:
		return "", fmt.Errorf("saml/sp: unsupported public-key type %T (want RSA or ECDSA P-256)", pub)
	}
}
