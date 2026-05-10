// Package hashutil exposes plain SHA-256 helpers used for non-secret digesting
// (audit-log hash chains, content-addressed identifiers, deterministic IDs).
//
// For password hashing or KDF use cases, do NOT use these helpers — choose a
// dedicated KDF (bcrypt, argon2, scrypt) instead. SHA-256 is intentionally fast
// and unsalted here, which is the wrong shape for a credential.
//
// Promoted from aosentry/pkg/crypto (the audit-chain helper there is the
// canonical implementation across the portfolio per the standardization plan
// §3 Hidden Gems).
package hashutil

import (
	"crypto/sha256"
	"encoding/hex"
)

// SHA256Hash returns the lowercase hex SHA-256 digest of s.
func SHA256Hash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// SHA256HashBytes returns the lowercase hex SHA-256 digest of data.
func SHA256HashBytes(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// ChainHash computes a deterministic hash for an audit-log entry by chaining
// the previous entry's hash with the current event fields. The pipe delimiter
// preserves field-order semantics so two events with swapped fields produce
// distinct chain values.
//
// The output is lowercase hex SHA-256 (64 chars).
func ChainHash(previousHash, action, tableName, objectID, changedBy string) string {
	payload := previousHash + "|" + action + "|" + tableName + "|" + objectID + "|" + changedBy
	return SHA256Hash(payload)
}
