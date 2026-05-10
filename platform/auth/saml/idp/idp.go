// Package idp implements the platform's SAML 2.0 Identity Provider surface:
// metadata publishing, AuthnRequest acceptance, and signed assertion
// issuance. Built on top of crewjam/saml's IdP primitives, but with a
// platform-friendly Config that supports per-tenant configuration and key
// rotation.
//
// The package is the IdP counterpart to platform/auth/saml (SP). Together
// they fulfill objectives 22 and 23: SP for "AOC apps consume external
// IdPs" (e.g. Okta, Azure AD) and IdP for "AOC apps issue SAML assertions
// to downstream SPs". Use the IdP for B2B federation (AO ID acting as IdP
// for partner apps).
package idp

import (
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	platsaml "github.com/aocybersystems/eden-platform-go/platform/auth/saml"
	"github.com/crewjam/saml"
)

// Config configures a tenant-scoped IdP instance.
type Config struct {
	// EntityID is the IdP entity URL — typically the metadata URL.
	EntityID string

	// SSOURL is the public URL of the SSO endpoint that accepts AuthnRequests.
	SSOURL string

	// CurrentKey is the active signing key + certificate. Required.
	CurrentKey *platsaml.SigningKey

	// PreviousKey is the most recent retired signing key, if any. Published
	// in metadata so SPs that have not yet rotated their trust anchors can
	// still validate the previous IdP's signatures during the rotation
	// window. Optional.
	PreviousKey *platsaml.SigningKey

	// AssertionLifetime is the validity window of issued assertions
	// (NotBefore .. NotOnOrAfter). Defaults to 5 minutes.
	AssertionLifetime time.Duration

	// AllowedSPs maps SP entity ID → SP metadata. Used to validate
	// AuthnRequests' Issuer and to resolve recipient ACS URLs. The IdP
	// rejects requests from any SP not present in this map.
	AllowedSPs map[string]SPRegistration
}

// SPRegistration is a registered Service Provider known to this IdP.
type SPRegistration struct {
	// EntityID matches the AuthnRequest's Issuer.
	EntityID string

	// ACSURL is the AssertionConsumerService URL where this IdP posts
	// signed Response messages.
	ACSURL string

	// SigningCertificate is the SP's signing certificate (optional). When
	// present, the IdP validates the AuthnRequest's XML signature against
	// it; when nil, the request is accepted unsigned.
	SigningCertificate *x509.Certificate
}

// IdentityProvider is a small wrapper around crewjam/saml's IdP type that
// applies platform conventions and supplies metadata + assertion helpers.
type IdentityProvider struct {
	cfg Config
	sp  *saml.IdentityProvider
}

// New constructs an IdentityProvider from the supplied Config. Returns an
// error when CurrentKey is missing or AssertionLifetime is negative.
func New(cfg Config) (*IdentityProvider, error) {
	if cfg.CurrentKey == nil {
		return nil, fmt.Errorf("idp: CurrentKey is required")
	}
	if cfg.CurrentKey.Certificate == nil || cfg.CurrentKey.PrivateKey == nil {
		return nil, fmt.Errorf("idp: CurrentKey is incomplete")
	}
	if cfg.AssertionLifetime < 0 {
		return nil, fmt.Errorf("idp: AssertionLifetime must be non-negative")
	}
	if cfg.AssertionLifetime == 0 {
		cfg.AssertionLifetime = 5 * time.Minute
	}
	ssoURL, err := url.Parse(cfg.SSOURL)
	if err != nil {
		return nil, fmt.Errorf("idp: SSOURL invalid: %w", err)
	}
	metaURL, err := url.Parse(cfg.EntityID)
	if err != nil {
		return nil, fmt.Errorf("idp: EntityID invalid: %w", err)
	}
	idp := &saml.IdentityProvider{
		Key:         cfg.CurrentKey.PrivateKey,
		Certificate: cfg.CurrentKey.Certificate,
		Logger:      nil,
		MetadataURL: *metaURL,
		SSOURL:      *ssoURL,
		ServiceProviderProvider: &lookupProvider{cfg: cfg},
	}
	return &IdentityProvider{cfg: cfg, sp: idp}, nil
}

// Metadata returns the SAML EntityDescriptor XML for this IdP. The
// document publishes both CurrentKey and (if set) PreviousKey so SPs can
// pre-trust both halves of a rotation window.
func (i *IdentityProvider) Metadata() ([]byte, error) {
	keys := []KeyDescriptor{{Use: "signing", Cert: i.cfg.CurrentKey.CertificateBase64()}}
	if i.cfg.PreviousKey != nil && i.cfg.PreviousKey.Certificate != nil {
		keys = append(keys, KeyDescriptor{Use: "signing", Cert: i.cfg.PreviousKey.CertificateBase64()})
	}
	doc := EntityDescriptor{
		EntityID: i.cfg.EntityID,
		IDPSSODescriptor: IDPSSODescriptor{
			ProtocolSupportEnumeration: "urn:oasis:names:tc:SAML:2.0:protocol",
			KeyDescriptors:             keys,
			NameIDFormats: []string{
				"urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress",
				"urn:oasis:names:tc:SAML:1.1:nameid-format:unspecified",
			},
			SingleSignOnServices: []SingleSignOnService{
				{
					Binding:  "urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect",
					Location: i.cfg.SSOURL,
				},
				{
					Binding:  "urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST",
					Location: i.cfg.SSOURL,
				},
			},
		},
	}
	body, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal metadata: %w", err)
	}
	return append([]byte(xml.Header), body...), nil
}

// MetadataHandler returns an http.Handler that serves Metadata() at the
// IdP's metadata endpoint.
func (i *IdentityProvider) MetadataHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := i.Metadata()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/samlmetadata+xml")
		_, _ = w.Write(body)
	})
}

// AssertionInput captures everything the IdP needs to issue an assertion
// for a successfully-authenticated user.
type AssertionInput struct {
	// SPEntityID identifies the audience SP. Must be present in
	// Config.AllowedSPs.
	SPEntityID string

	// InResponseTo, if non-empty, mirrors the original AuthnRequest's ID
	// onto the SubjectConfirmationData and Response elements. SPs that
	// initiated SP-first SSO require this.
	InResponseTo string

	// NameID is the user's identifier (typically email).
	NameID string

	// NameIDFormat overrides the default emailAddress format.
	NameIDFormat string

	// Attributes is a map of attribute name → value(s) emitted in the
	// AttributeStatement.
	Attributes map[string][]string

	// AuthnContextClassRef defaults to PasswordProtectedTransport.
	AuthnContextClassRef string

	// IssueInstant overrides the assertion's IssueInstant. Zero value uses
	// time.Now().UTC().
	IssueInstant time.Time
}

// IssueAssertion produces a signed SAML <Response> XML document containing
// a single signed Assertion. It is the core IdP operation.
func (i *IdentityProvider) IssueAssertion(in AssertionInput) ([]byte, error) {
	sp, ok := i.cfg.AllowedSPs[in.SPEntityID]
	if !ok {
		return nil, fmt.Errorf("idp: SP %q not registered", in.SPEntityID)
	}
	if in.NameID == "" {
		return nil, fmt.Errorf("idp: NameID is required")
	}
	now := in.IssueInstant
	if now.IsZero() {
		now = time.Now().UTC()
	}
	notOnOrAfter := now.Add(i.cfg.AssertionLifetime)
	nameIDFormat := in.NameIDFormat
	if nameIDFormat == "" {
		nameIDFormat = "urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress"
	}
	authnContext := in.AuthnContextClassRef
	if authnContext == "" {
		authnContext = "urn:oasis:names:tc:SAML:2.0:ac:classes:PasswordProtectedTransport"
	}

	assertion := buildAssertion(buildAssertionInput{
		issuer:               i.cfg.EntityID,
		audience:             sp.EntityID,
		recipient:            sp.ACSURL,
		nameID:               in.NameID,
		nameIDFormat:         nameIDFormat,
		issueInstant:         now,
		notBefore:            now,
		notOnOrAfter:         notOnOrAfter,
		inResponseTo:         in.InResponseTo,
		authnContextClassRef: authnContext,
		attributes:           sortedAttributes(in.Attributes),
	})

	respXML := buildResponse(buildResponseInput{
		issuer:       i.cfg.EntityID,
		destination:  sp.ACSURL,
		issueInstant: now,
		inResponseTo: in.InResponseTo,
		assertion:    assertion,
	})

	signed, err := signResponse([]byte(respXML), i.cfg.CurrentKey.PrivateKey, i.cfg.CurrentKey.Certificate)
	if err != nil {
		return nil, fmt.Errorf("sign response: %w", err)
	}
	return signed, nil
}

// HasKey reports whether the certificate fingerprint is one of the IdP's
// trusted (current or previous) certificates. Used by SPs migrating onto
// the platform to verify they are pinning the right key.
func (i *IdentityProvider) HasKey(cert *x509.Certificate) bool {
	if cert == nil {
		return false
	}
	if i.cfg.CurrentKey != nil && i.cfg.CurrentKey.Certificate != nil &&
		certificatesEqual(cert, i.cfg.CurrentKey.Certificate) {
		return true
	}
	if i.cfg.PreviousKey != nil && i.cfg.PreviousKey.Certificate != nil &&
		certificatesEqual(cert, i.cfg.PreviousKey.Certificate) {
		return true
	}
	return false
}

// PublicKey returns the active signing public key for assertion validation
// helpers that prefer keys over certificates.
func (i *IdentityProvider) PublicKey() *rsa.PublicKey {
	return &i.cfg.CurrentKey.PrivateKey.PublicKey
}

// AllowedSPs lists registered SP entity IDs in stable order — useful for
// admin UI and tests.
func (i *IdentityProvider) AllowedSPs() []string {
	out := make([]string, 0, len(i.cfg.AllowedSPs))
	for k := range i.cfg.AllowedSPs {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// crewjamSP returns the SP registration as crewjam/saml expects it. This is
// a deliberately small bridge — the rest of the IdP runs on platform code
// so we can swap crewjam out without breaking the public API.
func (l *lookupProvider) GetServiceProvider(_ *http.Request, entityID string) (*saml.EntityDescriptor, error) {
	sp, ok := l.cfg.AllowedSPs[entityID]
	if !ok {
		return nil, fmt.Errorf("SP %q not registered", entityID)
	}
	descriptor := &saml.EntityDescriptor{
		EntityID: sp.EntityID,
		SPSSODescriptors: []saml.SPSSODescriptor{
			{
				SSODescriptor: saml.SSODescriptor{
					RoleDescriptor: saml.RoleDescriptor{
						ProtocolSupportEnumeration: "urn:oasis:names:tc:SAML:2.0:protocol",
					},
				},
				AssertionConsumerServices: []saml.IndexedEndpoint{
					{
						Binding:  "urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST",
						Location: sp.ACSURL,
						Index:    1,
					},
				},
			},
		},
	}
	if sp.SigningCertificate != nil {
		spKey := &platsaml.SigningKey{Certificate: sp.SigningCertificate}
		descriptor.SPSSODescriptors[0].KeyDescriptors = []saml.KeyDescriptor{
			{
				Use: "signing",
				KeyInfo: saml.KeyInfo{
					X509Data: saml.X509Data{
						X509Certificates: []saml.X509Certificate{
							{Data: spKey.CertificateBase64()},
						},
					},
				},
			},
		}
	}
	return descriptor, nil
}

type lookupProvider struct{ cfg Config }

// CrewjamIDP exposes the underlying crewjam/saml IdP for callers wanting
// the full crewjam middleware stack (e.g. AcceptedRequestHandler). New
// integrations should prefer the platform-shaped helpers above.
func (i *IdentityProvider) CrewjamIDP() *saml.IdentityProvider {
	return i.sp
}

// certificatesEqual compares two certificates by raw DER bytes.
func certificatesEqual(a, b *x509.Certificate) bool {
	if a == nil || b == nil {
		return false
	}
	if len(a.Raw) != len(b.Raw) {
		return false
	}
	for i := range a.Raw {
		if a.Raw[i] != b.Raw[i] {
			return false
		}
	}
	return true
}

// sortedAttributes turns an unordered map into a stable slice for
// deterministic XML output (essential for canonicalization and signing).
func sortedAttributes(m map[string][]string) []attribute {
	out := make([]attribute, 0, len(m))
	for k, vs := range m {
		out = append(out, attribute{Name: k, Values: append([]string(nil), vs...)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// AcceptAuthnRequest decodes a base64-encoded AuthnRequest and returns the
// SP entity it references plus the request ID. It validates that the
// referenced SP is in AllowedSPs but does NOT validate XML signatures —
// that remains the caller's responsibility (use crewjam/saml's parsing for
// fully-signed requests). For unsigned-request flows the signature check
// is skipped.
func (i *IdentityProvider) AcceptAuthnRequest(samlRequest string) (sp SPRegistration, requestID string, err error) {
	if strings.TrimSpace(samlRequest) == "" {
		return SPRegistration{}, "", fmt.Errorf("idp: empty SAMLRequest")
	}
	xmlBytes, err := decodeSAMLParam(samlRequest)
	if err != nil {
		return SPRegistration{}, "", err
	}
	var req incomingAuthnRequest
	if err := xml.Unmarshal(xmlBytes, &req); err != nil {
		return SPRegistration{}, "", fmt.Errorf("idp: parse AuthnRequest: %w", err)
	}
	if req.Issuer.Value == "" {
		return SPRegistration{}, "", fmt.Errorf("idp: AuthnRequest missing Issuer")
	}
	registered, ok := i.cfg.AllowedSPs[req.Issuer.Value]
	if !ok {
		return SPRegistration{}, "", fmt.Errorf("idp: SP %q not registered", req.Issuer.Value)
	}
	return registered, req.ID, nil
}

// AcceptedSPSet is exported for tests + admin tooling.
func (i *IdentityProvider) AcceptedSPSet() map[string]SPRegistration {
	cp := make(map[string]SPRegistration, len(i.cfg.AllowedSPs))
	for k, v := range i.cfg.AllowedSPs {
		cp[k] = v
	}
	return cp
}

type incomingAuthnRequest struct {
	XMLName xml.Name `xml:"AuthnRequest"`
	ID      string   `xml:"ID,attr"`
	Issuer  struct {
		Value string `xml:",chardata"`
	} `xml:"Issuer"`
}

// TLSCertificate returns the signing certificate as a tls.Certificate for
// TLS-mTLS scenarios where the SAML signing key doubles as a transport
// credential. Most deployments will not need this.
func (i *IdentityProvider) TLSCertificate() *tls.Certificate {
	if i.cfg.CurrentKey == nil || i.cfg.CurrentKey.Certificate == nil {
		return nil
	}
	return &tls.Certificate{
		Certificate: [][]byte{i.cfg.CurrentKey.Certificate.Raw},
		PrivateKey:  i.cfg.CurrentKey.PrivateKey,
		Leaf:        i.cfg.CurrentKey.Certificate,
	}
}
