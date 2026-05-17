// Package mtls is the canonical mTLS server config builder for Eden services.
//
// BuildServerTLSConfig returns a *tls.Config that REQUIRES every client to
// present a certificate verifying against the configured trust pool. There is
// NO laxer mode — by construction this primitive does not allow
// VerifyClientCertIfGiven, RequestClientCert, or any other ClientAuth value
// that silently weakens the boundary. The minimum TLS version is 1.3; callers
// cannot select 1.2 or below. Cipher selection is left to crypto/tls (which
// chooses FIPS-approved suites automatically under GODEBUG=fips140=on).
//
// Two server-cert modes are supported:
//
//   - File mode (Obj 1 default): set Config.ServerCertFile + ServerKeyFile.
//   - KMS mode (Obj 5+ upgrade): set Config.KMSSigner + ServerCertChain.
//
// Exactly one of the two modes must be set; configuring both is a
// constructor error.
//
// Peer cert inspection:
//
//   - ExtractPeerSPIFFEID  — returns the first spiffe:// URI SAN on the
//     verified leaf cert. Forward-compatible with the Obj 5 SPIFFE
//     workload-identity work.
//   - ExtractPeerCommonName — returns the leaf cert's Subject CN.
//
// Deferred to later objectives:
//
//   - OCSP stapling: Obj 5 (internal CA exists then).
//   - Trust-pool hot-reload (tls.Config.GetConfigForClient): Obj 11
//     (operational posture). For Obj 1 the trust pool is captured at
//     server boot — reloading requires restart. Document this in the
//     consumer's runbook.
package mtls
