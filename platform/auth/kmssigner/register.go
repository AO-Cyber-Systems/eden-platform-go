package kmssigner

import (
	"sync"

	"github.com/golang-jwt/jwt/v5"
)

// registerOnce gates RegisterAll so that repeated invocations are no-ops.
//
// Rationale: jwt.RegisterSigningMethod mutates a process-global map. Without
// the gate, multiple RegisterAll calls would each install a fresh
// SigningMethod factory; functionally a no-op (the factory returns the same
// kind of value) but wasteful and a footgun if a caller ever conditions on
// the registry-mutation order.
var registerOnce sync.Once

// RegisterAll registers ES256SigningMethod and RS256SigningMethod with the
// global golang-jwt/v5 registry. It is idempotent — calling more than once is
// safe and the second-and-subsequent calls are no-ops (sync.Once gate).
//
// IMPORTANT: RegisterAll OVERRIDES the default golang-jwt ES256/RS256 methods,
// which expect *ecdsa.PrivateKey / *rsa.PrivateKey in process memory. After
// RegisterAll, every jwt.NewWithClaims(jwt.GetSigningMethod("ES256"), ...) in
// this process expects a kmssigner.Signer-shaped key, NOT a raw
// *ecdsa.PrivateKey.
//
// Tests that need the in-memory golang-jwt default must NOT call RegisterAll.
// There is no jwt.UnregisterSigningMethod — once registered, the override
// sticks for the lifetime of the process.
//
// Usage:
//
//	// process boot (e.g. main.go after KMS handle is open)
//	kmssigner.RegisterAll()
//
//	// later, anywhere:
//	method := jwt.GetSigningMethod("ES256") // returns *ES256SigningMethod
//	token := jwt.NewWithClaims(method, claims)
//	signed, err := token.SignedString(kmsSigner) // kmsSigner implements Signer.
func RegisterAll() {
	registerOnce.Do(func() {
		jwt.RegisterSigningMethod("ES256", func() jwt.SigningMethod { return &ES256SigningMethod{} })
		jwt.RegisterSigningMethod("RS256", func() jwt.SigningMethod { return &RS256SigningMethod{} })
	})
}
