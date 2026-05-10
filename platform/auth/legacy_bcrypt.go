package auth

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// LegacyBcryptCost matches Devise's default cost (12). Use this when
// generating new hashes that must remain compatible with Rails apps still
// reading the same column (notably AODex's transition window).
const LegacyBcryptCost = 12

// HashLegacyPassword generates a bcrypt hash with cost 12 (Devise default).
//
// New code should prefer PasswordHasher (Argon2id). This helper exists to
// keep bcrypt-stored passwords usable while AODex finishes its migration to
// platform/auth and to interoperate with any third-party Devise-compatible
// stores. Bcrypt natively handles $2a$, $2b$, and $2y$ prefixes.
func HashLegacyPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), LegacyBcryptCost)
	if err != nil {
		return "", fmt.Errorf("hashing password: %w", err)
	}
	return string(hash), nil
}

// VerifyLegacyPassword compares a plain-text password against a bcrypt hash.
// Returns nil on match, an error otherwise. Compatible with Devise-generated
// $2a$ hashes.
func VerifyLegacyPassword(hashedPassword, plainPassword string) error {
	if err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(plainPassword)); err != nil {
		return fmt.Errorf("password verification failed: %w", err)
	}
	return nil
}
