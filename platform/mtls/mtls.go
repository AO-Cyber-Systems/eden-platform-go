package mtls

import (
	"crypto"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
)

// Config holds the inputs to BuildServerTLSConfig.
//
// Exactly one of the two server-cert sources MUST be set:
//
//   - File mode: ServerCertFile + ServerKeyFile
//   - KMS  mode: KMSSigner + ServerCertChain
//
// TrustAnchorsFile is always required.
type Config struct {
	// ServerCertFile + ServerKeyFile drive tls.LoadX509KeyPair.
	ServerCertFile string
	ServerKeyFile  string

	// KMSSigner carries the server cert's private key when sourced from a
	// KMS. Typed as crypto.Signer (stdlib) so this package has no import on
	// platform/kms — callers pass their kms.KMSSigner directly, since that
	// interface embeds crypto.Signer.
	KMSSigner crypto.Signer

	// ServerCertChain is the PEM-encoded leaf + intermediate cert chain
	// for the KMS-backed mode.
	ServerCertChain []byte

	// TrustAnchorsFile points to a PEM file containing all CA certs whose
	// signature on a presented client cert authorizes admission. Required.
	TrustAnchorsFile string

	// MinTLSVersion is the floor for negotiated TLS. Defaults to
	// tls.VersionTLS13 if unset. Callers attempting to set a lower floor
	// are silently raised to TLS 1.3 — this primitive does not negotiate
	// down.
	MinTLSVersion uint16
}

// BuildServerTLSConfig validates cfg and returns a *tls.Config enforcing the
// Eden mTLS boundary. The returned config is suitable for an http.Server's
// TLSConfig field or for httptest.NewUnstartedServer in tests.
func BuildServerTLSConfig(cfg Config) (*tls.Config, error) {
	fileMode := cfg.ServerCertFile != "" && cfg.ServerKeyFile != ""
	kmsMode := cfg.KMSSigner != nil && len(cfg.ServerCertChain) > 0
	if fileMode == kmsMode {
		return nil, errors.New("mtls: exactly one of (ServerCertFile+ServerKeyFile) OR (KMSSigner+ServerCertChain) is required")
	}
	if cfg.TrustAnchorsFile == "" {
		return nil, errors.New("mtls: TrustAnchorsFile required")
	}

	var serverCert tls.Certificate
	switch {
	case fileMode:
		c, err := tls.LoadX509KeyPair(cfg.ServerCertFile, cfg.ServerKeyFile)
		if err != nil {
			return nil, fmt.Errorf("mtls: load server keypair: %w", err)
		}
		serverCert = c
	default:
		// KMS mode: parse chain, attach KMSSigner as PrivateKey.
		var derChain [][]byte
		var leaf *x509.Certificate
		rest := cfg.ServerCertChain
		for {
			block, next := pem.Decode(rest)
			if block == nil {
				break
			}
			if block.Type == "CERTIFICATE" {
				cert, err := x509.ParseCertificate(block.Bytes)
				if err != nil {
					return nil, fmt.Errorf("mtls: parse cert in chain: %w", err)
				}
				derChain = append(derChain, block.Bytes)
				if leaf == nil {
					leaf = cert
				}
			}
			rest = next
		}
		if len(derChain) == 0 {
			return nil, errors.New("mtls: ServerCertChain has no CERTIFICATE PEM blocks")
		}
		serverCert = tls.Certificate{
			Certificate: derChain,
			PrivateKey:  cfg.KMSSigner,
			Leaf:        leaf,
		}
	}

	trustPEM, err := os.ReadFile(cfg.TrustAnchorsFile)
	if err != nil {
		return nil, fmt.Errorf("mtls: read trust pool %q: %w", cfg.TrustAnchorsFile, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(trustPEM) {
		return nil, fmt.Errorf("mtls: no certs parsed from %q (malformed PEM?)", cfg.TrustAnchorsFile)
	}

	minVer := cfg.MinTLSVersion
	if minVer < tls.VersionTLS13 {
		minVer = tls.VersionTLS13
	}

	return &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientCAs:    pool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   minVer,
	}, nil
}
