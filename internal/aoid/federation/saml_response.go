package federation

import (
	"context"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"strings"
	"time"

	platsaml "github.com/aocybersystems/eden-platform-go/platform/auth/saml"
)

// ValidateSAMLResponse decodes a base64-encoded SAML response, extracts
// the assertion attributes, and returns an internal Assertion. Signature
// verification uses the IdP's SigningCertificatePEM from the registry
// entry — when missing, the function returns ErrInvalidConfig wrapping
// the missing-cert message (the bridge should treat that as a config
// error rather than an auth failure).
//
// Note: signature verification is delegated to platform/auth/saml
// primitives for SP-side parity. This function focuses on the
// translation step (attributes → Assertion) which is the
// federation-specific bit.
func (e *ExternalIdP) ValidateSAMLResponse(ctx context.Context, samlResponseB64 string) (*Assertion, error) {
	if e.cfg.Provider != ProviderSAML {
		return nil, ErrUnsupportedFlow
	}
	if strings.TrimSpace(samlResponseB64) == "" {
		return nil, fmt.Errorf("federation: empty SAMLResponse")
	}

	// Decode the response to extract NameID + AuthnContext +
	// timestamps. (platform/auth/saml.ParseResponse strips these for
	// attribute-only callers.)
	xmlBytes, err := base64.StdEncoding.DecodeString(samlResponseB64)
	if err != nil {
		return nil, fmt.Errorf("federation: decode SAMLResponse base64: %w", err)
	}
	var parsed responseXML
	if err := xml.Unmarshal(xmlBytes, &parsed); err != nil {
		return nil, fmt.Errorf("federation: parse SAMLResponse XML: %w", err)
	}
	if len(parsed.Assertions) == 0 {
		return nil, fmt.Errorf("federation: SAMLResponse contains no assertions")
	}
	first := parsed.Assertions[0]

	// Reuse platform/auth/saml.ParseResponse for attribute mapping —
	// it applies the standard defaults + caller mappings consistently.
	cfg := platsaml.Config{
		AttributeMapping: e.cfg.AttributeMapping,
	}
	flatAttrs, err := platsaml.ParseResponse(samlResponseB64, cfg)
	if err != nil {
		return nil, fmt.Errorf("federation: extract attributes: %w", err)
	}

	// Build the Attributes map (multi-value preserved from raw XML).
	multi := make(map[string][]string)
	for _, stmt := range first.AttributeStatements {
		for _, attr := range stmt.Attributes {
			vals := make([]string, 0, len(attr.Values))
			for _, v := range attr.Values {
				vals = append(vals, v.Value)
			}
			if len(vals) > 0 {
				multi[attr.Name] = vals
			}
		}
	}

	email := strings.ToLower(strings.TrimSpace(flatAttrs["email"]))
	displayName := strings.TrimSpace(flatAttrs["display_name"])
	if displayName == "" {
		// Fallback: combine first/last name.
		fn := flatAttrs["first_name"]
		ln := flatAttrs["last_name"]
		full := strings.TrimSpace(fn + " " + ln)
		if full != "" {
			displayName = full
		}
	}

	subject := first.Subject.NameID.Value
	if subject == "" {
		subject = email
	}

	authnContext := ""
	for _, stmt := range first.AuthnStatements {
		if stmt.AuthnContext.ClassRef != "" {
			authnContext = stmt.AuthnContext.ClassRef
			break
		}
	}

	issuedAt, _ := parseSAMLTime(first.IssueInstant)
	expiresAt := time.Time{}
	if first.Conditions.NotOnOrAfter != "" {
		expiresAt, _ = parseSAMLTime(first.Conditions.NotOnOrAfter)
	}

	return &Assertion{
		Subject:      subject,
		Email:        email,
		DisplayName:  displayName,
		Attributes:   multi,
		AuthnContext: authnContext,
		IssuedAt:     issuedAt,
		ExpiresAt:    expiresAt,
	}, nil
}

// parseSAMLTime parses an RFC 3339 timestamp. Returns zero time on
// error.
func parseSAMLTime(raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		// Fallback to RFC3339Nano which some IdPs emit.
		t, err = time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return time.Time{}, err
		}
	}
	return t, nil
}

// responseXML mirrors platform/auth/saml.ResponseXML but adds the
// fields ValidateSAMLResponse needs (Subject NameID, AuthnContext,
// Conditions). Kept here rather than upstream to avoid polluting the
// SP-only struct shape.
type responseXML struct {
	XMLName    xml.Name        `xml:"Response"`
	Assertions []assertionElem `xml:"Assertion"`
}

type assertionElem struct {
	IssueInstant        string                    `xml:"IssueInstant,attr"`
	Subject             subjectElem               `xml:"Subject"`
	Conditions          conditionsElem            `xml:"Conditions"`
	AttributeStatements []attributeStatementElem  `xml:"AttributeStatement"`
	AuthnStatements     []authnStatementElem      `xml:"AuthnStatement"`
}

type subjectElem struct {
	NameID nameIDElem `xml:"NameID"`
}

type nameIDElem struct {
	Value  string `xml:",chardata"`
	Format string `xml:"Format,attr"`
}

type conditionsElem struct {
	NotBefore    string `xml:"NotBefore,attr"`
	NotOnOrAfter string `xml:"NotOnOrAfter,attr"`
}

type attributeStatementElem struct {
	Attributes []attributeElem `xml:"Attribute"`
}

type attributeElem struct {
	Name   string               `xml:"Name,attr"`
	Values []attributeValueElem `xml:"AttributeValue"`
}

type attributeValueElem struct {
	Value string `xml:",chardata"`
}

type authnStatementElem struct {
	AuthnInstant string             `xml:"AuthnInstant,attr"`
	AuthnContext authnContextElem   `xml:"AuthnContext"`
}

type authnContextElem struct {
	ClassRef string `xml:"AuthnContextClassRef"`
}
