package breach

import (
	"context"
	"errors"
)

// Screener checks whether a password appears in a known-compromised corpus.
//
// Implementations satisfy NIST SP 800-63B Rev 4 §5.1.1 ("Verifiers SHALL
// compare prospective secrets against a list of known compromised values").
// See package documentation in doc.go for the choosing-an-implementation
// matrix (HIBPScreener / LocalListScreener / DisabledScreener).
type Screener interface {
	// Check returns compromised=true if the password appears in the corpus.
	//
	// occurrences may be >0 when the source provides count information
	// (HIBPScreener returns the corpus count; LocalListScreener returns 1).
	//
	// err is non-nil only for screener-level failures (network, parse,
	// programming bug). Callers SHOULD fail-open on err — defense-in-depth
	// means hashing + length + MFA stand independently of breach screening
	// — unless deployment policy requires fail-closed.
	Check(ctx context.Context, password string) (compromised bool, occurrences int, err error)
}

// Sentinel errors. Consumers use errors.Is for stable identity.
var (
	// ErrScreenerUnavailable indicates a transient screener failure
	// (network down, rate-limited, malformed response, persistent 5xx).
	// Callers should fail-open per defense-in-depth: password hashing
	// (Argon2id / PBKDF2-SHA256 in FIPS mode), length floor, and MFA
	// remain in effect independent of breach screening.
	ErrScreenerUnavailable = errors.New("breach: screener unavailable")

	// ErrInvalidPrefix indicates a programming bug — the HIBP prefix
	// must be exactly 5 uppercase hex chars. SHA-1 produces 40-char
	// hex output by construction; this error fires only if the hash
	// chain is corrupted.
	ErrInvalidPrefix = errors.New("breach: invalid HIBP prefix")
)
