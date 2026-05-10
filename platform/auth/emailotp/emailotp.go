// Package emailotp provides cryptographically secure 6-digit one-time
// passwords for email-based second-factor and verification flows.
//
// Promoted from aodex-go/internal/auth/email_otp.go.
package emailotp

import (
	"crypto/rand"
	"crypto/subtle"
	"fmt"
	"math/big"
	"time"
)

const (
	// CodeLength is the number of digits in an email OTP code.
	CodeLength = 6

	// DefaultExpiry is the duration after which an email OTP expires when
	// no explicit value is supplied to Verify.
	DefaultExpiry = 10 * time.Minute
)

// Generate creates a cryptographically secure 6-digit zero-padded OTP.
// Uses crypto/rand (not math/rand) for secure generation.
func Generate() (string, error) {
	max := big.NewInt(1000000) // 10^6
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", fmt.Errorf("generating email OTP: %w", err)
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// Verify validates an email OTP using constant-time comparison and the
// supplied expiry window. Returns true only if the code matches AND has
// not expired. Pass DefaultExpiry for the standard 10-minute window.
func Verify(storedCode, providedCode string, sentAt time.Time, expiry time.Duration) bool {
	if storedCode == "" || providedCode == "" {
		return false
	}
	if expiry == 0 {
		expiry = DefaultExpiry
	}
	if time.Since(sentAt) > expiry {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(storedCode), []byte(providedCode)) == 1
}
