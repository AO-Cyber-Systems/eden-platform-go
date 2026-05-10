package saml

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"
)

// SigningKey holds an RSA private key + matching X.509 certificate. It is
// the unit shared between SP and IdP code: the SP uses it to sign
// AuthnRequests (when configured), the IdP uses it to sign assertions and
// to publish the public key in metadata.
type SigningKey struct {
	PrivateKey  *rsa.PrivateKey
	Certificate *x509.Certificate
}

// CertificateBase64 returns the certificate's DER body base64-encoded
// (without PEM headers) suitable for embedding in SAML metadata X509Data
// elements.
func (k *SigningKey) CertificateBase64() string {
	if k == nil || k.Certificate == nil {
		return ""
	}
	return base64Encode(k.Certificate.Raw)
}

// CertificatePEM returns the certificate as a PEM block.
func (k *SigningKey) CertificatePEM() []byte {
	if k == nil || k.Certificate == nil {
		return nil
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: k.Certificate.Raw})
}

// GenerateSigningKey returns a new self-signed 2048-bit RSA key valid for
// the given duration. The certificate's CommonName is set to commonName.
// Use this for development; production deployments should provide their own
// key + certificate via LoadSigningKey.
func GenerateSigningKey(commonName string, validFor time.Duration) (*SigningKey, error) {
	if validFor <= 0 {
		validFor = 365 * 24 * time.Hour
	}
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("rsa generate: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("serial: %w", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(validFor),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		return nil, fmt.Errorf("create certificate: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("parse certificate: %w", err)
	}
	return &SigningKey{PrivateKey: priv, Certificate: cert}, nil
}

// LoadSigningKey constructs a SigningKey from PEM-encoded RSA private key
// and certificate bytes.
func LoadSigningKey(privPEM, certPEM []byte) (*SigningKey, error) {
	privBlock, _ := pem.Decode(privPEM)
	if privBlock == nil {
		return nil, fmt.Errorf("decode private PEM")
	}
	var priv *rsa.PrivateKey
	switch privBlock.Type {
	case "RSA PRIVATE KEY":
		k, err := x509.ParsePKCS1PrivateKey(privBlock.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse PKCS1 key: %w", err)
		}
		priv = k
	case "PRIVATE KEY":
		k, err := x509.ParsePKCS8PrivateKey(privBlock.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse PKCS8 key: %w", err)
		}
		rk, ok := k.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("PKCS8 key is not RSA")
		}
		priv = rk
	default:
		return nil, fmt.Errorf("unsupported private key type: %s", privBlock.Type)
	}
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		return nil, fmt.Errorf("decode certificate PEM")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse certificate: %w", err)
	}
	return &SigningKey{PrivateKey: priv, Certificate: cert}, nil
}

// base64Encode wraps encoding/base64.StdEncoding so callers don't need to
// import it separately. Returned in standard form (with padding); SAML
// metadata readers tolerate single-line and 64/76-char-wrapped variants.
func base64Encode(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return base64.StdEncoding.EncodeToString(b)
}
