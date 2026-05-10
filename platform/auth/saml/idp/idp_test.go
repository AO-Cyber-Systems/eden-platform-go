package idp

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/beevik/etree"
	dsig "github.com/russellhaering/goxmldsig"

	platsaml "github.com/aocybersystems/eden-platform-go/platform/auth/saml"
)

func mustKey(t *testing.T) *platsaml.SigningKey {
	t.Helper()
	k, err := platsaml.GenerateSigningKey("test-idp", time.Hour)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return k
}

func TestNew_RequiresCurrentKey(t *testing.T) {
	if _, err := New(Config{EntityID: "https://idp/meta", SSOURL: "https://idp/sso"}); err == nil {
		t.Fatal("expected error when CurrentKey is nil")
	}
}

func TestNew_DefaultAssertionLifetime(t *testing.T) {
	idp, err := New(Config{
		EntityID:   "https://idp/meta",
		SSOURL:     "https://idp/sso",
		CurrentKey: mustKey(t),
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if idp.cfg.AssertionLifetime != 5*time.Minute {
		t.Errorf("expected default 5 min, got %v", idp.cfg.AssertionLifetime)
	}
}

func TestNew_RejectsNegativeLifetime(t *testing.T) {
	if _, err := New(Config{
		EntityID:          "https://idp/meta",
		SSOURL:            "https://idp/sso",
		CurrentKey:        mustKey(t),
		AssertionLifetime: -time.Second,
	}); err == nil {
		t.Fatal("expected error for negative lifetime")
	}
}

func TestMetadata_PublishesCurrentAndPreviousKeys(t *testing.T) {
	current := mustKey(t)
	previous := mustKey(t)
	idp, err := New(Config{
		EntityID:    "https://idp.example.com/metadata",
		SSOURL:      "https://idp.example.com/sso",
		CurrentKey:  current,
		PreviousKey: previous,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	body, err := idp.Metadata()
	if err != nil {
		t.Fatalf("metadata: %v", err)
	}
	xmlStr := string(body)

	for _, want := range []string{
		`xmlns:md="urn:oasis:names:tc:SAML:2.0:metadata"`,
		`entityID="https://idp.example.com/metadata"`,
		`<md:IDPSSODescriptor`,
		`urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress`,
		`urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect`,
		`urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST`,
		`Location="https://idp.example.com/sso"`,
		`<ds:KeyInfo`,
		`<ds:X509Data>`,
		current.CertificateBase64(),
		previous.CertificateBase64(),
	} {
		if !strings.Contains(xmlStr, want) {
			t.Errorf("metadata missing %q.\nfull document:\n%s", want, xmlStr)
		}
	}
}

func TestMetadataHandler_ServesXML(t *testing.T) {
	idp, _ := New(Config{
		EntityID:   "https://idp/meta",
		SSOURL:     "https://idp/sso",
		CurrentKey: mustKey(t),
	})
	rr := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/metadata", nil)
	idp.MetadataHandler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/samlmetadata+xml" {
		t.Errorf("content type: %s", ct)
	}
	if !strings.Contains(rr.Body.String(), "EntityDescriptor") {
		t.Error("missing EntityDescriptor")
	}
}

func TestIssueAssertion_RejectsUnregisteredSP(t *testing.T) {
	idp, _ := New(Config{
		EntityID:   "https://idp/meta",
		SSOURL:     "https://idp/sso",
		CurrentKey: mustKey(t),
	})
	if _, err := idp.IssueAssertion(AssertionInput{SPEntityID: "unknown", NameID: "u@example.com"}); err == nil {
		t.Fatal("expected error for unknown SP")
	}
}

func TestIssueAssertion_RequiresNameID(t *testing.T) {
	idp, _ := New(Config{
		EntityID:   "https://idp/meta",
		SSOURL:     "https://idp/sso",
		CurrentKey: mustKey(t),
		AllowedSPs: map[string]SPRegistration{
			"sp-1": {EntityID: "sp-1", ACSURL: "https://sp/acs"},
		},
	})
	if _, err := idp.IssueAssertion(AssertionInput{SPEntityID: "sp-1"}); err == nil {
		t.Fatal("expected error for empty NameID")
	}
}

func TestIssueAssertion_ProducesSignedResponse(t *testing.T) {
	current := mustKey(t)
	idp, err := New(Config{
		EntityID:          "https://idp.example.com/metadata",
		SSOURL:            "https://idp.example.com/sso",
		CurrentKey:        current,
		AssertionLifetime: 10 * time.Minute,
		AllowedSPs: map[string]SPRegistration{
			"https://sp.example.com/metadata": {
				EntityID: "https://sp.example.com/metadata",
				ACSURL:   "https://sp.example.com/acs",
			},
		},
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	signed, err := idp.IssueAssertion(AssertionInput{
		SPEntityID:   "https://sp.example.com/metadata",
		InResponseTo: "_req-123",
		NameID:       "alice@example.com",
		Attributes: map[string][]string{
			"email": {"alice@example.com"},
			"name":  {"Alice"},
		},
	})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(signed); err != nil {
		t.Fatalf("parse signed response: %v", err)
	}
	root := doc.Root()
	if root == nil {
		t.Fatal("nil root")
	}
	if root.Tag != "Response" {
		t.Fatalf("unexpected root tag=%q space=%q", root.Tag, root.Space)
	}
	dest := root.SelectAttrValue("Destination", "")
	if dest != "https://sp.example.com/acs" {
		t.Errorf("destination: %s", dest)
	}
	if root.SelectAttrValue("InResponseTo", "") != "_req-123" {
		t.Errorf("InResponseTo: %s", root.SelectAttrValue("InResponseTo", ""))
	}

	// Verify the signature using goxmldsig + the IdP's certificate. This
	// is the round-trip that proves the assertion would be accepted by an
	// SP that trusts the IdP's published metadata.
	store := &fixedCertStore{certs: []*x509.Certificate{current.Certificate}}
	ctx := dsig.NewDefaultValidationContext(store)
	ctx.IdAttribute = "ID"
	if _, err := ctx.Validate(root); err != nil {
		t.Errorf("signature validation failed: %v", err)
	}

	// Inspect the assertion structure.
	assertion := root.FindElement("./Assertion")
	if assertion == nil {
		t.Fatal("missing assertion")
	}
	subject := assertion.FindElement("./Subject/NameID")
	if subject == nil || subject.Text() != "alice@example.com" {
		t.Errorf("nameid: %v", subject)
	}
	conditions := assertion.FindElement("./Conditions")
	if conditions == nil {
		t.Fatal("missing Conditions")
	}
	if conditions.SelectAttrValue("NotBefore", "") == "" {
		t.Error("NotBefore missing")
	}
	if conditions.SelectAttrValue("NotOnOrAfter", "") == "" {
		t.Error("NotOnOrAfter missing")
	}
	aud := assertion.FindElement("./Conditions/AudienceRestriction/Audience")
	if aud == nil || aud.Text() != "https://sp.example.com/metadata" {
		t.Errorf("audience: %v", aud)
	}
}

func TestAcceptAuthnRequest_ValidatesIssuer(t *testing.T) {
	idp, _ := New(Config{
		EntityID:   "https://idp/meta",
		SSOURL:     "https://idp/sso",
		CurrentKey: mustKey(t),
		AllowedSPs: map[string]SPRegistration{
			"sp-1": {EntityID: "sp-1", ACSURL: "https://sp/acs"},
		},
	})
	rawXML := `<samlp:AuthnRequest xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion" ID="_req-1" Version="2.0" IssueInstant="2026-01-01T00:00:00Z" Destination="https://idp/sso"><saml:Issuer>sp-1</saml:Issuer></samlp:AuthnRequest>`
	encoded := base64.StdEncoding.EncodeToString([]byte(rawXML))

	sp, reqID, err := idp.AcceptAuthnRequest(encoded)
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	if sp.EntityID != "sp-1" {
		t.Errorf("sp: %s", sp.EntityID)
	}
	if reqID != "_req-1" {
		t.Errorf("reqID: %s", reqID)
	}
}

func TestAcceptAuthnRequest_RejectsUnknownIssuer(t *testing.T) {
	idp, _ := New(Config{
		EntityID:   "https://idp/meta",
		SSOURL:     "https://idp/sso",
		CurrentKey: mustKey(t),
		AllowedSPs: map[string]SPRegistration{
			"known": {EntityID: "known", ACSURL: "https://sp/acs"},
		},
	})
	rawXML := `<samlp:AuthnRequest xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion" ID="_req-1" Version="2.0"><saml:Issuer>not-known</saml:Issuer></samlp:AuthnRequest>`
	encoded := base64.StdEncoding.EncodeToString([]byte(rawXML))
	if _, _, err := idp.AcceptAuthnRequest(encoded); err == nil {
		t.Fatal("expected error for unknown SP issuer")
	}
}

func TestAcceptAuthnRequest_RejectsBadInput(t *testing.T) {
	idp, _ := New(Config{
		EntityID:   "https://idp/meta",
		SSOURL:     "https://idp/sso",
		CurrentKey: mustKey(t),
	})
	if _, _, err := idp.AcceptAuthnRequest(""); err == nil {
		t.Fatal("expected error for empty SAMLRequest")
	}
	if _, _, err := idp.AcceptAuthnRequest("!!!not-base64"); err == nil {
		t.Fatal("expected error for bad base64")
	}
	if _, _, err := idp.AcceptAuthnRequest(base64.StdEncoding.EncodeToString([]byte("<not>saml</not>"))); err == nil {
		t.Fatal("expected error for non-AuthnRequest XML")
	}
}

func TestHasKey_CurrentAndPrevious(t *testing.T) {
	current := mustKey(t)
	previous := mustKey(t)
	other := mustKey(t)
	idp, _ := New(Config{
		EntityID:    "https://idp/meta",
		SSOURL:      "https://idp/sso",
		CurrentKey:  current,
		PreviousKey: previous,
	})
	if !idp.HasKey(current.Certificate) {
		t.Error("HasKey(current) should be true")
	}
	if !idp.HasKey(previous.Certificate) {
		t.Error("HasKey(previous) should be true")
	}
	if idp.HasKey(other.Certificate) {
		t.Error("HasKey(unrelated) should be false")
	}
	if idp.HasKey(nil) {
		t.Error("HasKey(nil) should be false")
	}
}

func TestPublicKey(t *testing.T) {
	current := mustKey(t)
	idp, _ := New(Config{
		EntityID:   "https://idp/meta",
		SSOURL:     "https://idp/sso",
		CurrentKey: current,
	})
	got := idp.PublicKey()
	want := &current.PrivateKey.PublicKey
	if got.N.Cmp(want.N) != 0 || got.E != want.E {
		t.Error("public key mismatch")
	}
}

func TestAllowedSPs_StableOrder(t *testing.T) {
	idp, _ := New(Config{
		EntityID:   "https://idp/meta",
		SSOURL:     "https://idp/sso",
		CurrentKey: mustKey(t),
		AllowedSPs: map[string]SPRegistration{
			"zeta": {EntityID: "zeta", ACSURL: "https://z"},
			"alpha": {EntityID: "alpha", ACSURL: "https://a"},
			"mu":   {EntityID: "mu", ACSURL: "https://m"},
		},
	})
	got := idp.AllowedSPs()
	want := []string{"alpha", "mu", "zeta"}
	if len(got) != len(want) {
		t.Fatalf("len: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("AllowedSPs[%d]: got %s want %s", i, got[i], want[i])
		}
	}
}

func TestTLSCertificate(t *testing.T) {
	idp, _ := New(Config{
		EntityID:   "https://idp/meta",
		SSOURL:     "https://idp/sso",
		CurrentKey: mustKey(t),
	})
	cert := idp.TLSCertificate()
	if cert == nil {
		t.Fatal("nil cert")
	}
	if cert.Leaf == nil {
		t.Error("missing leaf")
	}
}

// fixedCertStore exposes a fixed list of certificates as a goxmldsig
// CertificateStore. Used to validate IdP-signed responses in tests.
type fixedCertStore struct {
	certs []*x509.Certificate
}

func (s *fixedCertStore) Certificates() ([]*x509.Certificate, error) { return s.certs, nil }

// Ensure xml import remains useful (used by external test paths in CI).
var _ = xml.Marshal

// Ensure rsa public key field is referenced so build picks up the import.
var _ rsa.PublicKey
