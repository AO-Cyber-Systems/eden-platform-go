package piv

import (
	"crypto/x509"
	"encoding/asn1"
	"errors"
	"fmt"
)

// Microsoft UPN extension OID per
// https://learn.microsoft.com/en-us/windows/win32/seccertenroll/about-other-names.
// SubjectAltName OID per RFC 5280 §4.2.1.6.
var (
	oidSubjectAltName = asn1.ObjectIdentifier{2, 5, 29, 17}
	oidMicrosoftUPN   = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 311, 20, 2, 3}
)

// ErrNoUPN indicates the certificate has no SubjectAltName entry of type
// OtherName with the Microsoft UPN OID.
var ErrNoUPN = errors.New("piv: no UPN extension in certificate")

// ExtractUPN walks the SubjectAltName extension looking for an OtherName
// entry tagged with the Microsoft UPN OID (1.3.6.1.4.1.311.20.2.3), and
// returns its UTF8String value.
//
// Returns ErrNoUPN when:
//   - cert is nil, or
//   - cert has no SubjectAltName extension, or
//   - SAN exists but contains no OtherName entries, or
//   - OtherName entries exist but none has the Microsoft UPN OID.
//
// Returns a wrapped parse error for malformed ASN.1. Individual
// unparseable OtherName entries are skipped (a later SAN may still
// match) — only failure to parse the outer SAN SEQUENCE OF GeneralName
// surfaces as an error.
func ExtractUPN(cert *x509.Certificate) (string, error) {
	if cert == nil {
		return "", ErrNoUPN
	}
	for _, ext := range cert.Extensions {
		if !ext.Id.Equal(oidSubjectAltName) {
			continue
		}
		var sans []asn1.RawValue
		if _, err := asn1.Unmarshal(ext.Value, &sans); err != nil {
			return "", fmt.Errorf("piv: parse SubjectAltName: %w", err)
		}
		for _, san := range sans {
			// GeneralName ::= CHOICE { otherName [0] OtherName, ... }
			// otherName uses an IMPLICIT context-specific [0] tag.
			if san.Class != asn1.ClassContextSpecific || san.Tag != 0 {
				continue
			}
			// OtherName ::= SEQUENCE {
			//   type-id OBJECT IDENTIFIER,
			//   value   [0] EXPLICIT ANY DEFINED BY type-id
			// }
			//
			// san.Bytes carries the OtherName SEQUENCE's content
			// octets (IMPLICIT tag stripped). Re-wrap with a
			// universal SEQUENCE header so encoding/asn1 can decode.
			wrapped := append([]byte{0x30}, encodeLength(len(san.Bytes))...)
			wrapped = append(wrapped, san.Bytes...)

			var otherName struct {
				OID     asn1.ObjectIdentifier
				Wrapper asn1.RawValue // EXPLICIT [0] wrapper retained verbatim
			}
			if _, err := asn1.Unmarshal(wrapped, &otherName); err != nil {
				// Skip unparseable OtherName; another SAN may still match.
				continue
			}
			if !otherName.OID.Equal(oidMicrosoftUPN) {
				continue
			}
			// Wrapper is `[0] EXPLICIT ANY DEFINED BY type-id` —
			// content octets carry the inner UTF8String TLV.
			if otherName.Wrapper.Class != asn1.ClassContextSpecific || otherName.Wrapper.Tag != 0 {
				return "", fmt.Errorf("piv: unexpected OtherName value wrapper class=%d tag=%d", otherName.Wrapper.Class, otherName.Wrapper.Tag)
			}
			var upn string
			if _, err := asn1.Unmarshal(otherName.Wrapper.Bytes, &upn); err != nil {
				return "", fmt.Errorf("piv: parse UPN value: %w", err)
			}
			return upn, nil
		}
	}
	return "", ErrNoUPN
}

// encodeLength returns the DER definite-length octets for a SEQUENCE
// of the given content length. We only need this for re-wrapping
// IMPLICIT [0] OtherName values; in practice OtherName payloads are
// always < 64 KiB so the long-form path covers the realistic envelope.
func encodeLength(n int) []byte {
	switch {
	case n < 0x80:
		return []byte{byte(n)}
	case n < 0x100:
		return []byte{0x81, byte(n)}
	case n < 0x10000:
		return []byte{0x82, byte(n >> 8), byte(n)}
	default:
		return []byte{0x83, byte(n >> 16), byte(n >> 8), byte(n)}
	}
}
