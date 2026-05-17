// Package signature contains DER↔JWS-raw ECDSA signature conversion helpers
// shared across kms providers. AWS KMS returns DER, Azure Managed HSM returns
// DER for some endpoints and raw for others, and PKCS#11 (Thales/crypto11)
// returns DER. golang-jwt/v5 ES256/ES384/ES512 signing methods expect raw r||s.
// These helpers do the lossless conversion in both directions.
package signature

import (
	"encoding/asn1"
	"errors"
	"fmt"
	"math/big"
)

// ecdsaSig is the ASN.1-DER shape of an ECDSA signature: SEQUENCE { R, S }.
type ecdsaSig struct {
	R, S *big.Int
}

// ECDSAJWSFromDER converts an ASN.1-DER ECDSA signature into the JWS raw r||s
// form. curveBytes is the per-component byte width (32 for P-256, 48 for
// P-384, 66 for P-521). Each component is left-padded with zeros so the
// returned slice is exactly 2*curveBytes wide — the shape golang-jwt ES*
// signing methods expect.
func ECDSAJWSFromDER(der []byte, curveBytes int) ([]byte, error) {
	if len(der) == 0 {
		return nil, errors.New("signature: empty DER input")
	}
	if curveBytes <= 0 {
		return nil, fmt.Errorf("signature: invalid curveBytes %d", curveBytes)
	}
	var sig ecdsaSig
	rest, err := asn1.Unmarshal(der, &sig)
	if err != nil {
		return nil, fmt.Errorf("signature: asn1 unmarshal: %w", err)
	}
	if len(rest) != 0 {
		return nil, errors.New("signature: trailing bytes after DER signature")
	}
	if sig.R == nil || sig.S == nil {
		return nil, errors.New("signature: DER signature missing R or S")
	}
	if sig.R.Sign() < 0 || sig.S.Sign() < 0 {
		return nil, errors.New("signature: negative R or S in DER signature")
	}
	rBytes := sig.R.Bytes()
	sBytes := sig.S.Bytes()
	if len(rBytes) > curveBytes || len(sBytes) > curveBytes {
		return nil, fmt.Errorf("signature: R or S exceeds curve width %d", curveBytes)
	}
	out := make([]byte, 2*curveBytes)
	copy(out[curveBytes-len(rBytes):curveBytes], rBytes)
	copy(out[2*curveBytes-len(sBytes):], sBytes)
	return out, nil
}

// ECDSADERFromJWS is the inverse of ECDSAJWSFromDER. raw must be exactly
// 2*curveBytes wide where curveBytes is inferred from len(raw)/2 (must be
// even). Returns an ASN.1-DER ECDSA signature.
func ECDSADERFromJWS(raw []byte) ([]byte, error) {
	if len(raw) == 0 {
		return nil, errors.New("signature: empty raw input")
	}
	if len(raw)%2 != 0 {
		return nil, fmt.Errorf("signature: raw length %d is odd", len(raw))
	}
	half := len(raw) / 2
	r := new(big.Int).SetBytes(raw[:half])
	s := new(big.Int).SetBytes(raw[half:])
	der, err := asn1.Marshal(ecdsaSig{R: r, S: s})
	if err != nil {
		return nil, fmt.Errorf("signature: asn1 marshal: %w", err)
	}
	return der, nil
}
