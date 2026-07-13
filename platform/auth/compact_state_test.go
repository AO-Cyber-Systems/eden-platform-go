package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestCompactShortLivedToken_RoundTripAndSize locks in the fix for the AO ID
// "No pending login request" dead-end: the SSO state must carry a COMPACT HS256
// signature, not the ~3.3KB ML-DSA-65 one, so it survives being embedded in an
// upstream continuation cookie (browser ~4KB per-cookie limit).
func TestCompactShortLivedToken_RoundTripAndSize(t *testing.T) {
	m, err := NewJWTManager(JWTConfig{}) // auto-generates an ML-DSA-65 key (dev)
	if err != nil {
		t.Fatalf("NewJWTManager: %v", err)
	}

	const subject = "00000000-0000-0000-0000-0000000000ac|oidc|https://biz.aocyber.ai/auth/complete|D3klJ87fOoEIrWiJD5IEukNdAPv7beVJt84jnWA3vqQ"

	compact, err := m.CreateCompactShortLivedToken(subject, 10*time.Minute)
	if err != nil {
		t.Fatalf("CreateCompactShortLivedToken: %v", err)
	}
	mldsa, err := m.CreateShortLivedToken(subject, 10*time.Minute)
	if err != nil {
		t.Fatalf("CreateShortLivedToken: %v", err)
	}

	// Round-trip.
	got, err := m.ValidateCompactShortLivedToken(compact)
	if err != nil {
		t.Fatalf("ValidateCompactShortLivedToken: %v", err)
	}
	if got != subject {
		t.Fatalf("subject mismatch: got %q want %q", got, subject)
	}

	// Size: the compact token MUST be well under the 4KB cookie budget, and the
	// old ML-DSA one MUST be over it (the bug we're fixing).
	if len(compact) > 1024 {
		t.Errorf("compact state too large: %d bytes (want < 1024)", len(compact))
	}
	if len(mldsa) < 4096 {
		t.Errorf("expected ML-DSA state > 4096 bytes (the bug); got %d — test may be stale", len(mldsa))
	}
	t.Logf("compact=%d bytes  mldsa=%d bytes", len(compact), len(mldsa))

	// Alg-confusion defence: each validator rejects the other's alg.
	if _, err := m.ValidateShortLivedToken(compact); err == nil {
		t.Error("ML-DSA validator accepted an HS256 token (alg confusion)")
	}
	if _, err := m.ValidateCompactShortLivedToken(mldsa); err == nil {
		t.Error("compact validator accepted an ML-DSA token (alg confusion)")
	}
}

// TestParseStateJWT_CompactAndFallback verifies createStateJWT now emits a
// compact state AND that parseStateJWT still accepts a legacy ML-DSA state
// (rolling-deploy back-compat).
func TestParseStateJWT_CompactAndFallback(t *testing.T) {
	m, err := NewJWTManager(JWTConfig{})
	if err != nil {
		t.Fatalf("NewJWTManager: %v", err)
	}
	s := &SSOService{jwtManager: m}

	cid := uuid.MustParse("00000000-0000-0000-0000-0000000000ac")
	const redirect = "https://biz.aocyber.ai/auth/complete"
	const verifier = "D3klJ87fOoEIrWiJD5IEukNdAPv7beVJt84jnWA3vqQ"

	// New path: createStateJWT is compact and parses.
	state, err := s.createStateJWT(cid, "oidc", redirect, verifier)
	if err != nil {
		t.Fatalf("createStateJWT: %v", err)
	}
	if len(state) > 1024 {
		t.Errorf("state not compact: %d bytes", len(state))
	}
	gotCID, prov, rURI, ver, err := s.parseStateJWT(state)
	if err != nil {
		t.Fatalf("parseStateJWT(compact): %v", err)
	}
	if gotCID != cid || prov != "oidc" || rURI != redirect || ver != verifier {
		t.Fatalf("round-trip mismatch: %v %q %q %q", gotCID, prov, rURI, ver)
	}

	// Back-compat: a legacy ML-DSA state still parses via the fallback.
	legacy, err := m.CreateShortLivedToken(strings.Join([]string{cid.String(), "oidc", redirect, verifier}, "|"), 10*time.Minute)
	if err != nil {
		t.Fatalf("legacy CreateShortLivedToken: %v", err)
	}
	if _, _, _, _, err := s.parseStateJWT(legacy); err != nil {
		t.Fatalf("parseStateJWT(legacy ML-DSA) should fall back and succeed: %v", err)
	}
}
