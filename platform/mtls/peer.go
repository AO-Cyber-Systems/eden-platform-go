package mtls

import (
	"crypto/tls"
	"errors"
)

// ErrNoVerifiedChain is returned by the peer-extraction helpers when the
// ConnectionState has no verified chain to inspect — usually because the
// server was misconfigured with a ClientAuth weaker than
// RequireAndVerifyClientCert.
var ErrNoVerifiedChain = errors.New("mtls: no verified chain in connection state")

// ErrNoSPIFFEID is returned by ExtractPeerSPIFFEID when the verified leaf
// cert has no URI SAN with scheme "spiffe".
var ErrNoSPIFFEID = errors.New("mtls: no SPIFFE URI SAN in leaf cert")

// ExtractPeerSPIFFEID returns the SPIFFE URI from the first verified leaf
// cert's URI SANs. Forward-compatible with the Obj 5 workload-identity work
// where AOID issues SPIFFE-compatible SVID certs.
//
// Returns ErrNoVerifiedChain if state is nil or contains no verified chain;
// returns ErrNoSPIFFEID if the leaf cert exists but has no spiffe:// URI SAN.
func ExtractPeerSPIFFEID(state *tls.ConnectionState) (string, error) {
	if state == nil || len(state.VerifiedChains) == 0 || len(state.VerifiedChains[0]) == 0 {
		return "", ErrNoVerifiedChain
	}
	leaf := state.VerifiedChains[0][0]
	for _, u := range leaf.URIs {
		if u != nil && u.Scheme == "spiffe" {
			return u.String(), nil
		}
	}
	return "", ErrNoSPIFFEID
}

// ExtractPeerCommonName returns the leaf cert's Subject CN.
// Returns ErrNoVerifiedChain when the chain is empty.
func ExtractPeerCommonName(state *tls.ConnectionState) (string, error) {
	if state == nil || len(state.VerifiedChains) == 0 || len(state.VerifiedChains[0]) == 0 {
		return "", ErrNoVerifiedChain
	}
	return state.VerifiedChains[0][0].Subject.CommonName, nil
}
