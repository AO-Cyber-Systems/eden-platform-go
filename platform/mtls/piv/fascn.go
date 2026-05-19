package piv

import (
	"crypto/x509"
	"encoding/asn1"
	"encoding/hex"
	"errors"
	"fmt"
)

// ErrNoFASCN indicates that no FASC-N OtherName SAN was found in the
// certificate. Malformed FASC-N entries collapse to the same sentinel:
// the caller treats absence and malformation identically because both
// preclude using FASC-N as a federation identifier.
var ErrNoFASCN = errors.New("piv: no FASC-N OtherName in cert")

// oidFASCN identifies the FASC-N OtherName SAN per FIPS PUB 201-2 and
// NIST SP 800-73-4 §3.2. The OtherName value is an OCTET STRING carrying
// the BCD-encoded FASC-N — typically the 16-byte "16-of-36" form used in
// PIV-Auth and CAC-PKI-Auth credentials.
var oidFASCN = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 6, 6}

// ExtractFASC_N walks the SubjectAltName extension looking for OtherName
// entries with OID 2.16.840.1.101.3.6.6 (FASC-N). Returns the raw bytes
// of the inner OCTET STRING — typically 16 bytes for the 16-of-36
// FASC-N digits placed into PIV-Auth and CAC-PKI-Auth certs.
//
// Returns ErrNoFASCN if:
//   - cert is nil
//   - cert has no SubjectAltName extension
//   - SAN exists but contains no FASC-N OtherName
//   - a FASC-N OtherName exists but its OCTET STRING fails to parse
//
// Unlike ExtractUPN, individual parse failures do NOT surface as wrapped
// errors. The caller cannot meaningfully distinguish "absent" from
// "malformed" — both preclude using FASC-N as a federation identifier —
// so we collapse them to a single sentinel.
func ExtractFASC_N(cert *x509.Certificate) ([]byte, error) {
	if cert == nil {
		return nil, ErrNoFASCN
	}
	for _, ext := range cert.Extensions {
		if !ext.Id.Equal(oidSubjectAltName) {
			continue
		}
		var sans []asn1.RawValue
		if _, err := asn1.Unmarshal(ext.Value, &sans); err != nil {
			// Outer SAN parse failure — no SAN entries to walk. Match
			// ExtractUPN's behaviour and surface the error wrapped so
			// callers can branch on it if needed.
			return nil, fmt.Errorf("piv: parse SubjectAltName: %w", err)
		}
		for _, san := range sans {
			// GeneralName ::= CHOICE { otherName [0] OtherName, ... }
			// otherName uses an IMPLICIT context-specific [0] tag.
			if san.Class != asn1.ClassContextSpecific || san.Tag != 0 {
				continue
			}
			// Mirror the wrap-in-universal-SEQUENCE trick from
			// ExtractUPN so encoding/asn1 can decode the OtherName.
			wrapped := append([]byte{0x30}, encodeLength(len(san.Bytes))...)
			wrapped = append(wrapped, san.Bytes...)

			var otherName struct {
				OID     asn1.ObjectIdentifier
				Wrapper asn1.RawValue // EXPLICIT [0] wrapper retained verbatim
			}
			if _, err := asn1.Unmarshal(wrapped, &otherName); err != nil {
				// Skip unparseable OtherName; later entries may still
				// match.
				continue
			}
			if !otherName.OID.Equal(oidFASCN) {
				continue
			}
			if otherName.Wrapper.Class != asn1.ClassContextSpecific || otherName.Wrapper.Tag != 0 {
				continue
			}
			// Wrapper.Bytes carries the inner ANY DEFINED BY OID. For
			// FASC-N, that ANY is an OCTET STRING. Unmarshal it to
			// strip the OCTET STRING TLV. If unmarshal fails the entry
			// is malformed — fall through to ErrNoFASCN per the
			// "absent or malformed" contract.
			var raw []byte
			if _, err := asn1.Unmarshal(otherName.Wrapper.Bytes, &raw); err != nil {
				continue
			}
			return raw, nil
		}
	}
	return nil, ErrNoFASCN
}

// FormatFASC_NHex returns the lowercase hex encoding of the raw FASC-N
// bytes (no separator), suitable for storage in an opaque identifier
// field. AOID Obj 7 uses this for the federation_subject column in the
// accounts table — callers add the "fascn:" prefix at write time so the
// raw helper can be reused for log lines, audit events, and so on.
func FormatFASC_NHex(raw []byte) string {
	return hex.EncodeToString(raw)
}
