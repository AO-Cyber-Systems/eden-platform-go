package idp

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/beevik/etree"
	dsig "github.com/russellhaering/goxmldsig"
)

// attribute is the internal representation of an AttributeStatement entry.
type attribute struct {
	Name   string
	Values []string
}

type buildAssertionInput struct {
	issuer               string
	audience             string
	recipient            string
	nameID               string
	nameIDFormat         string
	issueInstant         time.Time
	notBefore            time.Time
	notOnOrAfter         time.Time
	inResponseTo         string
	authnContextClassRef string
	attributes           []attribute
}

type buildResponseInput struct {
	issuer       string
	destination  string
	issueInstant time.Time
	inResponseTo string
	assertion    string // serialized assertion XML
}

// buildAssertion produces the assertion XML string. We hand-build the
// document via etree so signing can target the assertion's ID with the
// canonicalization SAML SPs expect.
func buildAssertion(in buildAssertionInput) string {
	doc := etree.NewDocument()
	assert := doc.CreateElement("saml:Assertion")
	assert.CreateAttr("xmlns:saml", "urn:oasis:names:tc:SAML:2.0:assertion")
	assert.CreateAttr("ID", "_"+randomID())
	assert.CreateAttr("Version", "2.0")
	assert.CreateAttr("IssueInstant", in.issueInstant.UTC().Format(time.RFC3339))

	issuer := assert.CreateElement("saml:Issuer")
	issuer.SetText(in.issuer)

	subject := assert.CreateElement("saml:Subject")
	nameID := subject.CreateElement("saml:NameID")
	nameID.CreateAttr("Format", in.nameIDFormat)
	nameID.SetText(in.nameID)
	confirm := subject.CreateElement("saml:SubjectConfirmation")
	confirm.CreateAttr("Method", "urn:oasis:names:tc:SAML:2.0:cm:bearer")
	confirmData := confirm.CreateElement("saml:SubjectConfirmationData")
	confirmData.CreateAttr("NotOnOrAfter", in.notOnOrAfter.UTC().Format(time.RFC3339))
	confirmData.CreateAttr("Recipient", in.recipient)
	if in.inResponseTo != "" {
		confirmData.CreateAttr("InResponseTo", in.inResponseTo)
	}

	conditions := assert.CreateElement("saml:Conditions")
	conditions.CreateAttr("NotBefore", in.notBefore.UTC().Format(time.RFC3339))
	conditions.CreateAttr("NotOnOrAfter", in.notOnOrAfter.UTC().Format(time.RFC3339))
	audRestriction := conditions.CreateElement("saml:AudienceRestriction")
	aud := audRestriction.CreateElement("saml:Audience")
	aud.SetText(in.audience)

	authnStmt := assert.CreateElement("saml:AuthnStatement")
	authnStmt.CreateAttr("AuthnInstant", in.issueInstant.UTC().Format(time.RFC3339))
	authnStmt.CreateAttr("SessionIndex", "_"+randomID())
	ctxEl := authnStmt.CreateElement("saml:AuthnContext")
	ctxRef := ctxEl.CreateElement("saml:AuthnContextClassRef")
	ctxRef.SetText(in.authnContextClassRef)

	if len(in.attributes) > 0 {
		stmt := assert.CreateElement("saml:AttributeStatement")
		for _, a := range in.attributes {
			el := stmt.CreateElement("saml:Attribute")
			el.CreateAttr("Name", a.Name)
			el.CreateAttr("NameFormat", "urn:oasis:names:tc:SAML:2.0:attrname-format:basic")
			for _, v := range a.Values {
				val := el.CreateElement("saml:AttributeValue")
				val.CreateAttr("xmlns:xsi", "http://www.w3.org/2001/XMLSchema-instance")
				val.CreateAttr("xmlns:xs", "http://www.w3.org/2001/XMLSchema")
				val.CreateAttr("xsi:type", "xs:string")
				val.SetText(v)
			}
		}
	}

	out, err := doc.WriteToString()
	if err != nil {
		// Document construction is local; failure indicates a bug. Return
		// an empty string so callers fail loud.
		return ""
	}
	return strings.TrimSpace(out)
}

// buildResponse wraps the assertion in a Response envelope.
func buildResponse(in buildResponseInput) string {
	doc := etree.NewDocument()
	resp := doc.CreateElement("samlp:Response")
	resp.CreateAttr("xmlns:samlp", "urn:oasis:names:tc:SAML:2.0:protocol")
	resp.CreateAttr("xmlns:saml", "urn:oasis:names:tc:SAML:2.0:assertion")
	resp.CreateAttr("ID", "_"+randomID())
	resp.CreateAttr("Version", "2.0")
	resp.CreateAttr("IssueInstant", in.issueInstant.UTC().Format(time.RFC3339))
	resp.CreateAttr("Destination", in.destination)
	if in.inResponseTo != "" {
		resp.CreateAttr("InResponseTo", in.inResponseTo)
	}
	issuer := resp.CreateElement("saml:Issuer")
	issuer.SetText(in.issuer)
	status := resp.CreateElement("samlp:Status")
	statusCode := status.CreateElement("samlp:StatusCode")
	statusCode.CreateAttr("Value", "urn:oasis:names:tc:SAML:2.0:status:Success")

	// Attach the assertion XML as a sibling of <Status>.
	assertionDoc := etree.NewDocument()
	if err := assertionDoc.ReadFromString(in.assertion); err == nil {
		if root := assertionDoc.Root(); root != nil {
			resp.AddChild(root.Copy())
		}
	}

	out, _ := doc.WriteToString()
	return strings.TrimSpace(out)
}

// signResponse uses goxmldsig to enveloped-sign the Response document with
// the IdP's RSA-SHA256 key. Returns the signed XML bytes.
func signResponse(responseXML []byte, key *rsa.PrivateKey, cert *x509.Certificate) ([]byte, error) {
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(responseXML); err != nil {
		return nil, fmt.Errorf("read response xml: %w", err)
	}
	root := doc.Root()
	if root == nil {
		return nil, fmt.Errorf("response xml has no root")
	}

	keyStore := &fixedKeyStore{key: key, cert: cert}
	signCtx := dsig.NewDefaultSigningContext(keyStore)
	signCtx.Canonicalizer = dsig.MakeC14N10ExclusiveCanonicalizerWithPrefixList("")
	if err := signCtx.SetSignatureMethod(dsig.RSASHA256SignatureMethod); err != nil {
		return nil, fmt.Errorf("set signature method: %w", err)
	}

	signed, err := signCtx.SignEnveloped(root)
	if err != nil {
		return nil, fmt.Errorf("sign enveloped: %w", err)
	}
	signedDoc := etree.NewDocument()
	signedDoc.SetRoot(signed)

	// Pretty-print for readability; SPs canonicalize before verifying so
	// whitespace doesn't matter.
	out, err := signedDoc.WriteToString()
	if err != nil {
		return nil, fmt.Errorf("write signed xml: %w", err)
	}
	return []byte(out), nil
}

// fixedKeyStore implements goxmldsig.X509KeyStore using a pre-loaded RSA
// key and X509 certificate.
type fixedKeyStore struct {
	key  *rsa.PrivateKey
	cert *x509.Certificate
}

func (s *fixedKeyStore) GetKeyPair() (*rsa.PrivateKey, []byte, error) {
	return s.key, s.cert.Raw, nil
}

// decodeSAMLParam decodes a base64-encoded SAMLRequest or SAMLResponse
// query parameter or form value. Both standard and URL-safe base64 are
// accepted (some IdPs/SPs use one or the other).
func decodeSAMLParam(s string) ([]byte, error) {
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	if b, err := base64.RawStdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	if b, err := base64.URLEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	if b, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return nil, fmt.Errorf("idp: SAMLRequest is not valid base64")
}

func randomID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Should not happen with crypto/rand; if it does, fall back to a
		// time-based fallback so the IdP keeps producing unique-enough IDs.
		now := time.Now().UnixNano()
		for i := range b {
			b[i] = byte(now >> (i * 4))
		}
	}
	const hex = "0123456789abcdef"
	var out [32]byte
	for i, by := range b {
		out[i*2] = hex[by>>4]
		out[i*2+1] = hex[by&0x0f]
	}
	return string(out[:])
}

