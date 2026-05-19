package saml

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"

	xmlrtv "github.com/mattermost/xml-roundtrip-validator"
)

// Sentinel errors for XSWGuard — AOID's inbound SAML handlers use
// errors.Is to distinguish the two attack classes when emitting audit
// events (auth.federation.saml.xsw_rejected with reason=multi-assertion
// vs reason=roundtrip-mismatch).
var (
	// ErrXMLRoundTripMismatch is returned when the input XML does not
	// survive a parse-and-re-emit round trip through encoding/xml.
	// This is the CVE-class that the mattermost team disclosed in
	// 2020 — multiple Go SAML libraries shipped patched-then-re-broken
	// validators, so we rely on the canonical
	// mattermost/xml-roundtrip-validator implementation rather than
	// rolling our own.
	ErrXMLRoundTripMismatch = errors.New("saml/xsw: XML failed round-trip validation")

	// ErrMultipleAssertions is returned when the input contains more
	// than one <saml:Assertion> element. SAML 2.0 §2.3.3 permits zero
	// or one assertions in a Response (subject confirmation requires
	// exactly one); multiple-assertion responses are an XSW-attack
	// hallmark.
	ErrMultipleAssertions = errors.New("saml/xsw: response contains more than one Assertion element")

	// ErrInvalidXML is returned for input that fails initial parse —
	// distinct from a round-trip mismatch (which is structurally
	// valid XML that re-emits differently).
	ErrInvalidXML = errors.New("saml/xsw: input is not valid XML")
)

// XSWGuard defends against XML Signature Wrapping (XSW) attacks by
// running two complementary checks before AOID's SAML handlers attempt
// signature validation:
//
//  1. mattermost/xml-roundtrip-validator — ensures the bytes survive a
//     parse-and-re-emit cycle without mutation. CVE-class attacks that
//     exploit Go's encoding/xml tolerance for whitespace, attribute
//     ordering, and namespace handling are caught here.
//
//  2. Single-Assertion check — counts SAML Assertion elements in the
//     protocol's assertion namespace
//     (urn:oasis:names:tc:SAML:2.0:assertion). Greater than one signals
//     a wrapping attack: the attacker embeds an extra Assertion that
//     the signature verifier ignores but the consuming application
//     reads.
//
// XSWGuard is a PURE function — no audit emission, no logging. AOID's
// federation handler is responsible for audit-event emission on
// rejection.
//
// MUST be called BEFORE any signature validation. Calling order is
// load-bearing: a wrapped-assertion attack can construct a signature
// that validates against ONE Assertion while the application reads a
// DIFFERENT Assertion. This guard short-circuits that ambiguity.
func XSWGuard(rawXML []byte) error {
	if len(rawXML) == 0 {
		return fmt.Errorf("%w: empty input", ErrInvalidXML)
	}

	// Step 1: round-trip validation.
	if err := xmlrtv.Validate(bytes.NewReader(rawXML)); err != nil {
		return fmt.Errorf("%w: %v", ErrXMLRoundTripMismatch, err)
	}

	// Step 2: count Assertion elements in the SAML assertion namespace.
	count, err := countAssertions(rawXML)
	if err != nil {
		return err
	}
	if count > 1 {
		return ErrMultipleAssertions
	}

	return nil
}

const samlAssertionNamespace = "urn:oasis:names:tc:SAML:2.0:assertion"

// countAssertions walks rawXML and counts <Assertion> elements in the
// SAML assertion namespace. Elements outside that namespace (e.g. a
// random <Assertion> in a custom protocol namespace) are not counted.
// We use encoding/xml's Tokenizer rather than a DOM library because the
// round-trip validator has already proven encoding/xml's view matches
// the bytes — so this count is authoritative.
//
// In addition to counting, this function asserts that the input is
// well-formed XML (contains at least one StartElement). Plain-text
// input that parses cleanly into a single text token is rejected as
// ErrInvalidXML — the XSW guard's mandate is to inspect SAML
// Responses, which by definition contain at least one element.
func countAssertions(rawXML []byte) (int, error) {
	dec := xml.NewDecoder(bytes.NewReader(rawXML))
	// Permit non-UTF-8 declarations; SAML responses can declare
	// encoding="UTF-8" but we already have bytes so this is a no-op
	// in practice. Without the charset reader, the decoder rejects
	// any non-UTF-8 declaration up-front.
	dec.CharsetReader = func(charset string, input io.Reader) (io.Reader, error) {
		return input, nil
	}

	count := 0
	sawElement := false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, fmt.Errorf("%w: %v", ErrInvalidXML, err)
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		sawElement = true
		if start.Name.Local == "Assertion" && start.Name.Space == samlAssertionNamespace {
			count++
		}
	}
	if !sawElement {
		return 0, fmt.Errorf("%w: no XML elements found", ErrInvalidXML)
	}
	return count, nil
}
