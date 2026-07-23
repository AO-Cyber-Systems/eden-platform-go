package saml_test

import (
	"encoding/base64"
	"testing"
	"time"

	platsaml "github.com/aocybersystems/eden-platform-go/platform/auth/saml"
	"github.com/aocybersystems/eden-platform-go/platform/auth/saml/idp"
	"github.com/beevik/etree"
)

// Unit tests for platsaml.VerifyResponse — the signature-VERIFYING SAML
// primitive that replaces the unverified ParseResponse (now
// ParseResponseUnverified) on authentication paths. VerifyResponse must reject
// any response whose signature does not validate against the configured IdP
// certificate.
//
// Every fixture is minted programmatically from a real signing IdP via this
// package's own saml/idp toolkit (GenerateSigningKey -> idp.New ->
// IssueAssertion). Attack variants are deterministic etree transforms over
// genuinely-signed documents — there is NO hand-authored / string-literal SAML
// XML here (the fixture-generator discipline).
//
// This lives in the external test package (saml_test) rather than the white-box
// package (saml) because it imports saml/idp, which imports the parent saml
// package — a white-box test would create an import cycle.

const (
	vIDPEntityID = "https://test-idp.platform.local/metadata"
	vIDPSSOURL   = "https://test-idp.platform.local/sso"
	vSPEntityID  = "https://app.platform.local/auth/saml/verify-cfg/metadata"
	vACSURL      = "https://app.platform.local/auth/saml/verify-cfg/callback"
)

// newSigningKey mints a fresh self-signed RSA signing key (RSA-2048).
func newSigningKey(t *testing.T, cn string) *platsaml.SigningKey {
	t.Helper()
	k, err := platsaml.GenerateSigningKey(cn, time.Hour)
	if err != nil {
		t.Fatalf("GenerateSigningKey(%q): %v", cn, err)
	}
	return k
}

// newIDP builds an IdP backed by key with exactly the SP under test registered,
// so the minted assertion's audience + recipient satisfy crewjam validation.
func newIDP(t *testing.T, key *platsaml.SigningKey) *idp.IdentityProvider {
	t.Helper()
	p, err := idp.New(idp.Config{
		EntityID:          vIDPEntityID,
		SSOURL:            vIDPSSOURL,
		CurrentKey:        key,
		AssertionLifetime: 5 * time.Minute,
		AllowedSPs: map[string]idp.SPRegistration{
			vSPEntityID: {EntityID: vSPEntityID, ACSURL: vACSURL},
		},
	})
	if err != nil {
		t.Fatalf("idp.New: %v", err)
	}
	return p
}

// mintSigned returns a base64 SAMLResponse: a real, IdP-signed <Response>
// carrying a single signed assertion for email. The valid-signed variant.
func mintSigned(t *testing.T, p *idp.IdentityProvider, email string) string {
	t.Helper()
	signed, err := p.IssueAssertion(idp.AssertionInput{
		SPEntityID: vSPEntityID,
		NameID:     email,
		Attributes: map[string][]string{
			"email":      {email},
			"first_name": {"Test"},
			"last_name":  {"User"},
		},
	})
	if err != nil {
		t.Fatalf("IssueAssertion: %v", err)
	}
	return base64.StdEncoding.EncodeToString(signed)
}

// verifyCfg builds a Config that pins cert as the trusted IdP signing cert and
// matches the fixture IdP's entity ID / SP entity ID / ACS URL.
func verifyCfg(cert string) platsaml.Config {
	return platsaml.Config{
		IDPCert:     cert,
		IDPEntityID: vIDPEntityID,
		SPEntityID:  vSPEntityID,
		ACSURL:      vACSURL,
	}
}

func certPEM(t *testing.T, key *platsaml.SigningKey) string {
	t.Helper()
	return string(key.CertificatePEM())
}

func decodeDoc(t *testing.T, b64 string) *etree.Document {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(raw); err != nil {
		t.Fatalf("parse xml: %v", err)
	}
	return doc
}

func encodeDoc(t *testing.T, doc *etree.Document) string {
	t.Helper()
	out, err := doc.WriteToBytes()
	if err != nil {
		t.Fatalf("write xml: %v", err)
	}
	return base64.StdEncoding.EncodeToString(out)
}

// stripSignature deletes every <ds:Signature> element from a signed response,
// yielding a well-formed but UNSIGNED response — the primary case to reject. Node
// deletion is a deterministic etree transform, not hand-authored XML.
func stripSignature(t *testing.T, b64 string) string {
	t.Helper()
	doc := decodeDoc(t, b64)
	for _, sig := range doc.FindElements("//Signature") {
		if p := sig.Parent(); p != nil {
			p.RemoveChild(sig)
		}
	}
	return encodeDoc(t, doc)
}

// tamperEmail mutates the NameID + email AttributeValue AFTER signing. The
// signature no longer matches the content, so any verifying SP must reject it.
func tamperEmail(t *testing.T, b64, tamperedEmail string) string {
	t.Helper()
	doc := decodeDoc(t, b64)
	root := doc.Root()
	for _, el := range root.FindElements(".//Assertion//NameID") {
		el.SetText(tamperedEmail)
	}
	for _, el := range root.FindElements(".//Assertion//AttributeStatement//Attribute") {
		if el.SelectAttrValue("Name", "") == "email" {
			if v := el.FindElement("./AttributeValue"); v != nil {
				v.SetText(tamperedEmail)
			}
		}
	}
	return encodeDoc(t, doc)
}

// T1 — validly-signed assertion from the trusted cert → returns mapped attrs.
func TestVerifyResponse_ValidSigned_ReturnsAttributes(t *testing.T) {
	key := newSigningKey(t, "tenant-idp")
	p := newIDP(t, key)
	email := "alice@tenant.example.com"
	resp := mintSigned(t, p, email)

	attrs, err := platsaml.VerifyResponse(resp, verifyCfg(certPEM(t, key)))
	if err != nil {
		t.Fatalf("a genuinely-signed response from the trusted cert must verify: %v", err)
	}
	if attrs["email"] != email {
		t.Errorf("email: want %q got %q", email, attrs["email"])
	}
	if attrs["first_name"] != "Test" {
		t.Errorf("first_name: want %q got %q", "Test", attrs["first_name"])
	}
	if attrs["last_name"] != "User" {
		t.Errorf("last_name: want %q got %q", "User", attrs["last_name"])
	}
}

// T2 — unsigned response → error (the primary case to reject).
func TestVerifyResponse_Unsigned_Errors(t *testing.T) {
	key := newSigningKey(t, "tenant-idp")
	p := newIDP(t, key)
	resp := mintSigned(t, p, "bob@tenant.example.com")
	unsigned := stripSignature(t, resp)

	if _, err := platsaml.VerifyResponse(unsigned, verifyCfg(certPEM(t, key))); err == nil {
		t.Fatal("an unsigned response must be rejected")
	}
}

// T3 — validly-signed by an UNTRUSTED signer (wrong/attacker cert) → error.
func TestVerifyResponse_WrongCert_Errors(t *testing.T) {
	trusted := newSigningKey(t, "tenant-idp")
	attacker := newSigningKey(t, "attacker-idp")
	p := newIDP(t, attacker) // signed by attacker, but cfg pins trusted cert
	resp := mintSigned(t, p, "carol@tenant.example.com")

	if _, err := platsaml.VerifyResponse(resp, verifyCfg(certPEM(t, trusted))); err == nil {
		t.Fatal("a response signed by a cert the config does not trust must be rejected")
	}
}

// T4 — tampered-after-signing (email mutated post-sign) → error.
func TestVerifyResponse_TamperedAfterSigning_Errors(t *testing.T) {
	key := newSigningKey(t, "tenant-idp")
	p := newIDP(t, key)
	resp := mintSigned(t, p, "real@tenant.example.com")
	tampered := tamperEmail(t, resp, "attacker@evil.example.com")

	if _, err := platsaml.VerifyResponse(tampered, verifyCfg(certPEM(t, key))); err == nil {
		t.Fatal("mutating a signed assertion must invalidate the signature")
	}
}

// T5a — empty IdP cert → REJECT, never verify-skip.
func TestVerifyResponse_EmptyCert_Rejects(t *testing.T) {
	key := newSigningKey(t, "tenant-idp")
	p := newIDP(t, key)
	resp := mintSigned(t, p, "dave@tenant.example.com")

	if _, err := platsaml.VerifyResponse(resp, verifyCfg("")); err == nil {
		t.Fatal("a config with no IdP cert must reject, not verify-skip")
	}
}

// T5b — garbage (unparseable) IdP cert → REJECT.
func TestVerifyResponse_GarbageCert_Rejects(t *testing.T) {
	key := newSigningKey(t, "tenant-idp")
	p := newIDP(t, key)
	resp := mintSigned(t, p, "erin@tenant.example.com")

	if _, err := platsaml.VerifyResponse(resp, verifyCfg("not-a-certificate")); err == nil {
		t.Fatal("a config with an unparseable IdP cert must reject, not verify-skip")
	}
}
