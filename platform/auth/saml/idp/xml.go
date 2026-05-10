package idp

import (
	"encoding/xml"
)

// XML element types for IdP metadata. Defined here so tests can verify the
// emitted document matches what crewjam/saml (and Okta, SimpleSAMLphp,
// etc.) expect to parse.

// EntityDescriptor is the root metadata element.
type EntityDescriptor struct {
	XMLName          xml.Name         `xml:"urn:oasis:names:tc:SAML:2.0:metadata EntityDescriptor"`
	EntityID         string           `xml:"entityID,attr"`
	IDPSSODescriptor IDPSSODescriptor `xml:"urn:oasis:names:tc:SAML:2.0:metadata IDPSSODescriptor"`
}

// IDPSSODescriptor describes an IdP's capabilities.
type IDPSSODescriptor struct {
	ProtocolSupportEnumeration string                `xml:"protocolSupportEnumeration,attr"`
	KeyDescriptors             []KeyDescriptor       `xml:"urn:oasis:names:tc:SAML:2.0:metadata KeyDescriptor"`
	NameIDFormats              []string              `xml:"urn:oasis:names:tc:SAML:2.0:metadata NameIDFormat"`
	SingleSignOnServices       []SingleSignOnService `xml:"urn:oasis:names:tc:SAML:2.0:metadata SingleSignOnService"`
}

// KeyDescriptor publishes a public key used by the IdP.
type KeyDescriptor struct {
	Use     string `xml:"use,attr"`
	KeyInfo struct {
		XMLName  xml.Name `xml:"http://www.w3.org/2000/09/xmldsig# KeyInfo"`
		X509Data struct {
			XMLName         xml.Name `xml:"http://www.w3.org/2000/09/xmldsig# X509Data"`
			X509Certificate string   `xml:"http://www.w3.org/2000/09/xmldsig# X509Certificate"`
		} `xml:"http://www.w3.org/2000/09/xmldsig# X509Data"`
	} `xml:"-"`
	Cert string `xml:"-"` // populated via MarshalXML
}

// MarshalXML emits the KeyDescriptor with a properly-namespaced KeyInfo.
func (k KeyDescriptor) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	start.Name = xml.Name{Local: "md:KeyDescriptor"}
	start.Attr = []xml.Attr{{Name: xml.Name{Local: "use"}, Value: k.Use}}
	if err := e.EncodeToken(start); err != nil {
		return err
	}
	keyInfoStart := xml.StartElement{
		Name: xml.Name{Local: "ds:KeyInfo"},
		Attr: []xml.Attr{{
			Name:  xml.Name{Local: "xmlns:ds"},
			Value: "http://www.w3.org/2000/09/xmldsig#",
		}},
	}
	if err := e.EncodeToken(keyInfoStart); err != nil {
		return err
	}
	x509DataStart := xml.StartElement{Name: xml.Name{Local: "ds:X509Data"}}
	if err := e.EncodeToken(x509DataStart); err != nil {
		return err
	}
	certStart := xml.StartElement{Name: xml.Name{Local: "ds:X509Certificate"}}
	if err := e.EncodeToken(certStart); err != nil {
		return err
	}
	if err := e.EncodeToken(xml.CharData(k.Cert)); err != nil {
		return err
	}
	if err := e.EncodeToken(xml.EndElement{Name: certStart.Name}); err != nil {
		return err
	}
	if err := e.EncodeToken(xml.EndElement{Name: x509DataStart.Name}); err != nil {
		return err
	}
	if err := e.EncodeToken(xml.EndElement{Name: keyInfoStart.Name}); err != nil {
		return err
	}
	return e.EncodeToken(xml.EndElement{Name: start.Name})
}

// SingleSignOnService publishes an SSO endpoint binding+location.
type SingleSignOnService struct {
	Binding  string `xml:"Binding,attr"`
	Location string `xml:"Location,attr"`
}

// MarshalXML emits md:* namespace tags so external parsers (Okta, etc.)
// recognize the document. The encoding/xml namespace handling otherwise
// emits unprefixed names which some strict parsers reject.
func (e EntityDescriptor) MarshalXML(enc *xml.Encoder, start xml.StartElement) error {
	start.Name = xml.Name{Local: "md:EntityDescriptor"}
	start.Attr = []xml.Attr{
		{Name: xml.Name{Local: "xmlns:md"}, Value: "urn:oasis:names:tc:SAML:2.0:metadata"},
		{Name: xml.Name{Local: "entityID"}, Value: e.EntityID},
	}
	if err := enc.EncodeToken(start); err != nil {
		return err
	}
	idpStart := xml.StartElement{
		Name: xml.Name{Local: "md:IDPSSODescriptor"},
		Attr: []xml.Attr{{Name: xml.Name{Local: "protocolSupportEnumeration"}, Value: e.IDPSSODescriptor.ProtocolSupportEnumeration}},
	}
	if err := enc.EncodeToken(idpStart); err != nil {
		return err
	}
	for _, k := range e.IDPSSODescriptor.KeyDescriptors {
		if err := enc.EncodeElement(k, xml.StartElement{Name: xml.Name{Local: "md:KeyDescriptor"}}); err != nil {
			return err
		}
	}
	for _, f := range e.IDPSSODescriptor.NameIDFormats {
		if err := enc.EncodeElement(f, xml.StartElement{Name: xml.Name{Local: "md:NameIDFormat"}}); err != nil {
			return err
		}
	}
	for _, s := range e.IDPSSODescriptor.SingleSignOnServices {
		ssoStart := xml.StartElement{
			Name: xml.Name{Local: "md:SingleSignOnService"},
			Attr: []xml.Attr{
				{Name: xml.Name{Local: "Binding"}, Value: s.Binding},
				{Name: xml.Name{Local: "Location"}, Value: s.Location},
			},
		}
		if err := enc.EncodeToken(ssoStart); err != nil {
			return err
		}
		if err := enc.EncodeToken(xml.EndElement{Name: ssoStart.Name}); err != nil {
			return err
		}
	}
	if err := enc.EncodeToken(xml.EndElement{Name: idpStart.Name}); err != nil {
		return err
	}
	return enc.EncodeToken(xml.EndElement{Name: start.Name})
}
