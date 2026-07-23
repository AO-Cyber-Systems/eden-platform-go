package saml

import (
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
)

func TestBuildAuthnRequestRedirectURL(t *testing.T) {
	cfg := Config{
		IDPSSOUrl:   "https://idp.example.com/sso",
		IDPEntityID: "https://idp.example.com",
		SPEntityID:  "https://sp.example.com/metadata",
		ACSURL:      "https://sp.example.com/acs",
	}
	redirect, err := BuildAuthnRequestRedirectURL(cfg)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.HasPrefix(redirect, cfg.IDPSSOUrl+"?") {
		t.Errorf("redirect %q should start with IdP SSO URL", redirect)
	}
	parsed, err := url.Parse(redirect)
	if err != nil {
		t.Fatalf("parse redirect: %v", err)
	}
	samlReq := parsed.Query().Get("SAMLRequest")
	if samlReq == "" {
		t.Fatal("missing SAMLRequest")
	}
	xmlBytes, err := base64.StdEncoding.DecodeString(samlReq)
	if err != nil {
		t.Fatalf("decode SAMLRequest: %v", err)
	}
	xmlStr := string(xmlBytes)
	for _, want := range []string{
		"urn:oasis:names:tc:SAML:2.0:protocol",
		`Version="2.0"`,
		cfg.ACSURL,
		cfg.IDPSSOUrl,
		cfg.SPEntityID,
		`ID="_`,
	} {
		if !strings.Contains(xmlStr, want) {
			t.Errorf("xml missing %q", want)
		}
	}
}

func TestParseResponseUnverified(t *testing.T) {
	responseXML := `<samlp:Response xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion">
	<saml:Assertion>
		<saml:Subject><saml:NameID>alice@acme.com</saml:NameID></saml:Subject>
		<saml:AttributeStatement>
			<saml:Attribute Name="email"><saml:AttributeValue>alice@acme.com</saml:AttributeValue></saml:Attribute>
			<saml:Attribute Name="first_name"><saml:AttributeValue>Alice</saml:AttributeValue></saml:Attribute>
			<saml:Attribute Name="last_name"><saml:AttributeValue>Smith</saml:AttributeValue></saml:Attribute>
		</saml:AttributeStatement>
	</saml:Assertion>
</samlp:Response>`
	encoded := base64.StdEncoding.EncodeToString([]byte(responseXML))
	cfg := Config{}
	attrs, err := ParseResponseUnverified(encoded, cfg)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if attrs["email"] != "alice@acme.com" {
		t.Errorf("email: %s", attrs["email"])
	}
	if attrs["first_name"] != "Alice" {
		t.Errorf("first_name: %s", attrs["first_name"])
	}
	if attrs["last_name"] != "Smith" {
		t.Errorf("last_name: %s", attrs["last_name"])
	}
}

func TestParseResponseUnverified_NameIDFallback(t *testing.T) {
	responseXML := `<samlp:Response xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion">
	<saml:Assertion>
		<saml:Subject><saml:NameID>bob@acme.com</saml:NameID></saml:Subject>
		<saml:AttributeStatement>
			<saml:Attribute Name="name"><saml:AttributeValue>Bob</saml:AttributeValue></saml:Attribute>
		</saml:AttributeStatement>
	</saml:Assertion>
</samlp:Response>`
	encoded := base64.StdEncoding.EncodeToString([]byte(responseXML))
	attrs, err := ParseResponseUnverified(encoded, Config{})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if attrs["email"] != "bob@acme.com" {
		t.Errorf("expected NameID fallback for email, got %q", attrs["email"])
	}
}

func TestParseResponseUnverified_InvalidBase64(t *testing.T) {
	_, err := ParseResponseUnverified("not-valid!!!", Config{})
	if err == nil {
		t.Error("expected error for invalid base64")
	}
}

func TestParseResponseUnverified_NonSAMLXML(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("<not>saml</not>"))
	attrs, err := ParseResponseUnverified(encoded, Config{})
	if err != nil {
		t.Errorf("expected no error for non-SAML XML, got %v", err)
	}
	if len(attrs) != 0 {
		t.Errorf("expected empty attrs for non-SAML XML, got %v", attrs)
	}
}

func TestParseResponseUnverified_CustomMapping(t *testing.T) {
	responseXML := `<samlp:Response xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion">
	<saml:Assertion>
		<saml:Subject><saml:NameID>carol@acme.com</saml:NameID></saml:Subject>
		<saml:AttributeStatement>
			<saml:Attribute Name="http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress"><saml:AttributeValue>carol@acme.com</saml:AttributeValue></saml:Attribute>
			<saml:Attribute Name="http://schemas.xmlsoap.org/ws/2005/05/identity/claims/givenname"><saml:AttributeValue>Carol</saml:AttributeValue></saml:Attribute>
		</saml:AttributeStatement>
	</saml:Assertion>
</samlp:Response>`
	encoded := base64.StdEncoding.EncodeToString([]byte(responseXML))
	cfg := Config{
		AttributeMapping: map[string]string{
			"email":      "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress",
			"first_name": "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/givenname",
		},
	}
	attrs, err := ParseResponseUnverified(encoded, cfg)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if attrs["email"] != "carol@acme.com" {
		t.Errorf("email: %s", attrs["email"])
	}
	if attrs["first_name"] != "Carol" {
		t.Errorf("first_name: %s", attrs["first_name"])
	}
}

func TestBuildSPMetadata(t *testing.T) {
	cfg := Config{
		SPEntityID: "https://sp.example.com/metadata",
		ACSURL:     "https://sp.example.com/acs",
	}
	metadata, err := BuildSPMetadata(cfg)
	if err != nil {
		t.Fatalf("metadata: %v", err)
	}
	xmlStr := string(metadata)
	for _, want := range []string{
		`entityID="https://sp.example.com/metadata"`,
		`Location="https://sp.example.com/acs"`,
		"urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress",
		"urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST",
		"<?xml",
	} {
		if !strings.Contains(xmlStr, want) {
			t.Errorf("metadata missing %q", want)
		}
	}
}

func TestParseIDPCertificate(t *testing.T) {
	if _, err := ParseIDPCertificate(""); err == nil {
		t.Error("expected error for empty cert")
	}
	if _, err := ParseIDPCertificate("not a cert"); err == nil {
		t.Error("expected error for non-cert")
	}
}

func TestDefaultAttributeMapping(t *testing.T) {
	m := defaultAttributeMapping()
	want := map[string]string{
		"email":      "email",
		"name":       "name",
		"first_name": "first_name",
		"last_name":  "last_name",
	}
	for k, v := range want {
		if m[k] != v {
			t.Errorf("mapping[%s]: want %s got %s", k, v, m[k])
		}
	}
}
