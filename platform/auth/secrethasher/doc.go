// Package secrethasher provides FIPS-aware secret hashing for high-value
// secrets like API keys, service-account credentials, and short-lived
// session tokens.
//
// The encoded output is algorithm-tagged so a single column can hold rows
// minted under either algorithm:
//
//	$argon2id$v=19$m=47104,t=1,p=1$<base64Salt>$<base64Hash>     (non-FIPS, default)
//	$pbkdf2-sha256$i=600000$<base64Salt>$<base64Hash>            (FIPS mode)
//
// # Runtime selection
//
// Hash branches at call time on the platform/fipsmode runtime flag. Verify
// reads the algorithm marker from the encoded string and uses the matching
// code path regardless of current FIPS posture — this lets a deployment
// flip non-FIPS -> FIPS (or back) without invalidating already-stored
// hashes. The "permissive" Verify is the v1.0 default; a future
// VerifyStrict API (reserved via ErrAlgorithmMismatch) will reject a
// posture-mismatched hash for callers that need belt-and-suspenders.
//
// # Example (mint side, AOID API key issuance)
//
//	rawKey, err := apikey.Generate()
//	if err != nil { return err }
//	encoded, err := secrethasher.Hash(rawKey)
//	if err != nil { return err }
//	// Persist `encoded` to aoid.api_keys.key_hash.
//	// Return rawKey to caller exactly once (one-time view).
//
// # Example (validate side, AOID API-key-validation handler)
//
//	ok, err := secrethasher.Verify(submittedKey, row.KeyHash)
//	if err != nil {
//	    // Parser-level failure (ErrInvalidFormat / ErrUnknownAlgorithm).
//	    // Treat as a hard authentication failure but do NOT leak the
//	    // sentinel back to the client — it would distinguish "your
//	    // key is malformed" from "your key is wrong".
//	    return ErrInvalidKey
//	}
//	if !ok { return ErrInvalidKey }
//
// # Parameters
//
// Do NOT tune in-place. A parameter bump warrants a new TRD plus a
// hash-format-version constant for forward migration. Verify already
// reads parameters from the encoded string at call time, so older rows
// minted under earlier parameter sets continue to verify byte-for-byte.
//
//   - Argon2id: time=1, memory=47104 KiB, threads=1, keyLen=32, saltLen=16
//     (OWASP Password Storage Cheat Sheet, 2024 refresh)
//   - PBKDF2-SHA256: iter=600000, keyLen=32, saltLen=16
//     (NIST SP 800-132 + FIPS 140-3 IG D.B; OWASP 2024 floor)
//
// # FIPS posture
//
// Argon2id is NOT FIPS-validated. PBKDF2-SHA256 IS FIPS-approved when
// running on a FIPS-validated crypto module (BoringCrypto in eden FIPS
// builds; see platform/fipsmode). Hash switches automatically — consumers
// do not need to know the runtime FIPS posture to call Hash correctly.
//
// # Constant-time comparison
//
// Verify uses crypto/subtle.ConstantTimeCompare for the final byte
// comparison to prevent timing-side-channel leaks during verification.
// Callers MUST NOT replace this with bytes.Equal in downstream wrappers.
package secrethasher
