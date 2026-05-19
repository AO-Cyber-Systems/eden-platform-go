package saml

import (
	_ "embed"
	"errors"
	"testing"
)

//go:embed testdata/xsw_wrapped_assertion.xml
var xswWrappedAssertion []byte

const cleanOktaResponse = `<?xml version="1.0" encoding="UTF-8"?>
<samlp:Response xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion" ID="_clean_response" Version="2.0" IssueInstant="2026-05-19T00:00:00Z" Destination="https://aoid.example.com/acs">
  <saml:Issuer>https://trusted.example.com/idp</saml:Issuer>
  <samlp:Status><samlp:StatusCode Value="urn:oasis:names:tc:SAML:2.0:status:Success"/></samlp:Status>
  <saml:Assertion ID="_legit_assert" Version="2.0" IssueInstant="2026-05-19T00:00:00Z">
    <saml:Issuer>https://trusted.example.com/idp</saml:Issuer>
    <saml:Subject>
      <saml:NameID Format="urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress">user@trusted.example</saml:NameID>
    </saml:Subject>
    <saml:AttributeStatement>
      <saml:Attribute Name="email"><saml:AttributeValue>user@trusted.example</saml:AttributeValue></saml:Attribute>
    </saml:AttributeStatement>
  </saml:Assertion>
</samlp:Response>`

func TestXSWGuard_CleanResponseAccepted(t *testing.T) {
	if err := XSWGuard([]byte(cleanOktaResponse)); err != nil {
		t.Fatalf("expected nil for clean response, got %v", err)
	}
}

func TestXSWGuard_MultipleAssertionsRejected(t *testing.T) {
	err := XSWGuard(xswWrappedAssertion)
	if err == nil {
		t.Fatal("expected error for response with two Assertion elements, got nil")
	}
	if !errors.Is(err, ErrMultipleAssertions) {
		t.Fatalf("expected ErrMultipleAssertions, got %v", err)
	}
}

// The mattermost validator catches XML round-trip mismatches the Go
// encoding/xml parser would silently accept (CVE-class). This crafted
// document has an attribute-name overflow that the validator detects.
const xmlRoundTripBait = "<?xml version=\"1.0\"?>\n" +
	"<root>\n" +
	"  <bait a:=\"1\" b=\"2\"/>\n" +
	"</root>"

func TestXSWGuard_RoundTripMismatchRejected(t *testing.T) {
	err := XSWGuard([]byte(xmlRoundTripBait))
	if err == nil {
		t.Fatal("expected error for round-trip-mismatch XML, got nil")
	}
	if !errors.Is(err, ErrXMLRoundTripMismatch) {
		t.Fatalf("expected ErrXMLRoundTripMismatch, got %v", err)
	}
}

func TestXSWGuard_NilInputRejected(t *testing.T) {
	err := XSWGuard(nil)
	if err == nil {
		t.Fatal("expected error for nil input, got nil")
	}
}

func TestXSWGuard_NotXMLRejected(t *testing.T) {
	err := XSWGuard([]byte("definitely not xml at all"))
	if err == nil {
		t.Fatal("expected error for non-XML input, got nil")
	}
}

func TestXSWGuard_ZeroAssertionsAccepted(t *testing.T) {
	// A response containing no Assertion (e.g. status-only failure) MUST
	// NOT trigger ErrMultipleAssertions — the guard's job is to reject
	// >1, not exactly-1. SAML status-only responses are legitimate.
	statusOnly := `<?xml version="1.0"?>
<samlp:Response xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol">
  <samlp:Status><samlp:StatusCode Value="urn:oasis:names:tc:SAML:2.0:status:Requester"/></samlp:Status>
</samlp:Response>`
	if err := XSWGuard([]byte(statusOnly)); err != nil {
		t.Fatalf("status-only response rejected: %v", err)
	}
}
