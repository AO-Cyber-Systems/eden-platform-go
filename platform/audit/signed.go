package audit

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/aocybersystems/eden-platform-go/platform/kms"
	"github.com/aocybersystems/eden-platform-go/platform/kms/signature"
)

// SignedEventStore is implemented by audit stores that natively persist a JWS
// alongside the Event (preferred path — the buffer keeps the signature in a
// dedicated column for chain-of-custody). PostgresBufferStore implements this.
//
// Stores that do NOT implement SignedEventStore fall back to plain AuditStore;
// SignedStore embeds the JWS into a JSON wrapper in the details field.
type SignedEventStore interface {
	CreateSignedAuditLog(ctx context.Context, e Event, jwsCompact string, signingError string) error
}

// VerifiedEvent is returned by VerifySignedEvent — the canonical payload
// parsed back into an Event plus the JWS metadata the verifier extracted.
type VerifiedEvent struct {
	Event    Event
	Issuer   string
	IssuedAt int64
	KID      string
	Alg      string
}

// ErrUnsupportedAlgorithm is returned when a signer reports an algorithm that
// SignedStore is not allowed to use. Only ES256 and RS256 are permitted —
// HMAC variants are explicitly rejected (TRD 09-02 §C.2).
var ErrUnsupportedAlgorithm = errors.New("audit: signer algorithm must be ES256 or RS256")

// ErrInvalidJWS is returned by VerifySignedEvent for malformed inputs.
var ErrInvalidJWS = errors.New("audit: invalid JWS Compact")

// ErrSignatureInvalid is returned by VerifySignedEvent when the signature
// fails to validate against the supplied public key.
var ErrSignatureInvalid = errors.New("audit: signature verification failed")

// SignedStore wraps an AuditStore and signs every event with the configured
// kms.KMSSigner before delegating. It is the entry point between the
// audit.Logger emission path and the durable buffer.
//
// Algorithm support: ES256 (preferred — FIPS BoringCrypto ECDSA P-256) or
// RS256 (RSA PKCS#1 v1.5 with SHA-256, the algorithm the platform kms package
// already exposes for the AWS/Azure/PKCS#11 RSA path).
//
// HMAC (HS256/HS384/HS512) is rejected at NewSignedStore time — symmetric
// signing breaks the non-repudiation guarantee (the AOAudit operator could
// forge events). PS256 is not supported by the underlying kms.KMSSigner
// implementations today; if a future provider exposes it, add it here.
type SignedStore struct {
	inner    AuditStore
	signer   kms.KMSSigner
	issuerID string
	clock    func() time.Time
}

// NewSignedStore constructs a SignedStore. issuerID is the JWS `iss` claim
// (e.g. "aoid"). Returns ErrUnsupportedAlgorithm if signer.SigningAlgorithm()
// is not in {ES256, RS256}.
func NewSignedStore(inner AuditStore, signer kms.KMSSigner, issuerID string) (*SignedStore, error) {
	if signer == nil {
		return nil, errors.New("audit: nil signer")
	}
	if inner == nil {
		return nil, errors.New("audit: nil inner store")
	}
	alg := signer.SigningAlgorithm()
	if alg != "ES256" && alg != "RS256" {
		return nil, fmt.Errorf("%w: got %q", ErrUnsupportedAlgorithm, alg)
	}
	if issuerID == "" {
		return nil, errors.New("audit: empty issuerID")
	}
	return &SignedStore{
		inner:    inner,
		signer:   signer,
		issuerID: issuerID,
		clock:    time.Now,
	}, nil
}

// CreateAuditLog implements AuditStore. Reconstructs an Event from the
// (companyID, actorID, action, ...) args, signs via JWS Compact, then
// delegates to the inner store.
//
// Dispatch:
//   - inner implements SignedEventStore → CreateSignedAuditLog (preferred).
//   - else → AuditStore.CreateAuditLog with JWS embedded in details JSON
//     wrapper `{"jws": "...", "payload": {...}}`.
//
// Failure mode: if signing returns an error (KMS transient failure, network
// glitch), the event STILL lands in inner with jwsCompact="" and a populated
// signingError. The Forwarder's re-signer pump retries later. Losing an
// unsigned event would violate AUD-07.
func (s *SignedStore) CreateAuditLog(
	ctx context.Context,
	companyID, actorID uuid.UUID,
	action, resource, resourceID, ipAddress string,
	details []byte,
) error {
	var detailsMap map[string]any
	if len(details) > 0 {
		_ = json.Unmarshal(details, &detailsMap)
	}
	if detailsMap == nil {
		detailsMap = map[string]any{}
	}
	// Ensure a stable jti — generate one if the caller didn't.
	jti, _ := detailsMap["jti"].(string)
	if jti == "" {
		jti = generateJTI()
		detailsMap["jti"] = jti
	}
	e := Event{
		CompanyID:  companyID.String(),
		ActorID:    actorID.String(),
		Action:     action,
		Resource:   resource,
		ResourceID: resourceID,
		IPAddress:  ipAddress,
		Details:    detailsMap,
	}

	jwsCompact, signErr := s.sign(e)
	signingErrStr := ""
	if signErr != nil {
		signingErrStr = signErr.Error()
		jwsCompact = ""
	}

	if ses, ok := s.inner.(SignedEventStore); ok {
		return ses.CreateSignedAuditLog(ctx, e, jwsCompact, signingErrStr)
	}

	// Fallback: embed in details JSON wrapper. Loses some structure but the
	// JWS itself is recoverable for offline verification.
	wrapped := map[string]any{
		"jws":     jwsCompact,
		"payload": detailsMap,
	}
	if signingErrStr != "" {
		wrapped["signing_error"] = signingErrStr
	}
	wrappedJSON, err := json.Marshal(wrapped)
	if err != nil {
		return fmt.Errorf("audit: marshal wrapped details: %w", err)
	}
	return s.inner.CreateAuditLog(ctx, companyID, actorID, action, resource, resourceID, ipAddress, wrappedJSON)
}

// sign produces a JWS Compact over MarshalCanonical(e, iss, iat). Header
// carries alg + kid + typ=JWT; signature is in the JWS-canonical raw form
// (r||s for ES256; PKCS1v15 bytes for RS256).
func (s *SignedStore) sign(e Event) (string, error) {
	now := s.clock().UTC()
	iat := now.Unix()
	payload, err := MarshalCanonical(e, s.issuerID, iat)
	if err != nil {
		return "", fmt.Errorf("audit: canonicalize: %w", err)
	}
	alg := s.signer.SigningAlgorithm()
	header := map[string]string{
		"alg": alg,
		"kid": s.signer.KeyID(),
		"typ": "JWT",
	}
	headerBytes, err := json.Marshal(header)
	if err != nil {
		return "", fmt.Errorf("audit: marshal header: %w", err)
	}
	headerB64 := base64.RawURLEncoding.EncodeToString(headerBytes)
	payloadB64 := base64.RawURLEncoding.EncodeToString(payload)
	signingInput := headerB64 + "." + payloadB64
	digest := sha256.Sum256([]byte(signingInput))

	sig, err := s.signer.Sign(rand.Reader, digest[:], crypto.SHA256)
	if err != nil {
		return "", fmt.Errorf("audit: signer.Sign: %w", err)
	}

	// ES256: the kms package returns ASN.1-DER (AWS KMS / PKCS#11 / Azure
	// Managed HSM all do). JWS requires raw r||s — convert via the shared
	// signature helper. RS256 PKCS1v15 already matches JWS shape; no
	// conversion needed.
	if alg == "ES256" {
		sig, err = signature.ECDSAJWSFromDER(sig, 32) // 32 bytes per component for P-256
		if err != nil {
			return "", fmt.Errorf("audit: ECDSA DER→raw: %w", err)
		}
	}

	sigB64 := base64.RawURLEncoding.EncodeToString(sig)
	return signingInput + "." + sigB64, nil
}

// SignForResign exposes the sign path to the forwarder's re-signer pump so
// it can re-sign rows that landed in the buffer with signing_error set
// (the original Log call hit a KMS transient failure).
func (s *SignedStore) SignForResign(e Event) (string, error) {
	return s.sign(e)
}

// VerifySignedEvent parses a JWS Compact, validates the signature against
// pub, and returns the decoded VerifiedEvent. Used by downstream consumers
// (AOAudit) to verify the chain-of-custody.
//
// pub must be:
//   - *ecdsa.PublicKey for ES256
//   - *rsa.PublicKey for RS256
//
// Returns ErrInvalidJWS for malformed input, ErrSignatureInvalid for failed
// verification, and ErrUnsupportedAlgorithm for headers advertising an
// algorithm SignedStore does not produce.
func VerifySignedEvent(jwsCompact string, pub crypto.PublicKey) (VerifiedEvent, error) {
	parts := strings.Split(jwsCompact, ".")
	if len(parts) != 3 {
		return VerifiedEvent{}, fmt.Errorf("%w: want 3 parts, got %d", ErrInvalidJWS, len(parts))
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return VerifiedEvent{}, fmt.Errorf("%w: decode header: %v", ErrInvalidJWS, err)
	}
	var header map[string]string
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return VerifiedEvent{}, fmt.Errorf("%w: parse header: %v", ErrInvalidJWS, err)
	}
	alg := header["alg"]
	if alg != "ES256" && alg != "RS256" {
		return VerifiedEvent{}, fmt.Errorf("%w: header alg=%q", ErrUnsupportedAlgorithm, alg)
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return VerifiedEvent{}, fmt.Errorf("%w: decode payload: %v", ErrInvalidJWS, err)
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return VerifiedEvent{}, fmt.Errorf("%w: decode signature: %v", ErrInvalidJWS, err)
	}
	signingInput := parts[0] + "." + parts[1]
	digest := sha256.Sum256([]byte(signingInput))

	switch alg {
	case "ES256":
		ecPub, ok := pub.(*ecdsa.PublicKey)
		if !ok {
			return VerifiedEvent{}, fmt.Errorf("%w: ES256 requires *ecdsa.PublicKey, got %T", ErrInvalidJWS, pub)
		}
		if len(sig) != 64 {
			return VerifiedEvent{}, fmt.Errorf("%w: ES256 raw signature length %d, want 64", ErrInvalidJWS, len(sig))
		}
		r := new(big.Int).SetBytes(sig[:32])
		sBig := new(big.Int).SetBytes(sig[32:])
		if !ecdsa.Verify(ecPub, digest[:], r, sBig) {
			return VerifiedEvent{}, ErrSignatureInvalid
		}
	case "RS256":
		rsaPub, ok := pub.(*rsa.PublicKey)
		if !ok {
			return VerifiedEvent{}, fmt.Errorf("%w: RS256 requires *rsa.PublicKey, got %T", ErrInvalidJWS, pub)
		}
		if err := rsa.VerifyPKCS1v15(rsaPub, crypto.SHA256, digest[:], sig); err != nil {
			return VerifiedEvent{}, fmt.Errorf("%w: %v", ErrSignatureInvalid, err)
		}
	}

	event, iss, iat, err := UnmarshalCanonical(payloadBytes)
	if err != nil {
		return VerifiedEvent{}, fmt.Errorf("%w: unmarshal payload: %v", ErrInvalidJWS, err)
	}
	return VerifiedEvent{
		Event:    event,
		Issuer:   iss,
		IssuedAt: iat,
		KID:      header["kid"],
		Alg:      alg,
	}, nil
}

// generateJTI returns a fresh ULID-like identifier. We avoid taking a hard
// dependency on github.com/oklog/ulid here by composing a 26-char crockford-
// base32 string from the current time + random tail. The output is sortable
// (timestamp-prefixed) and unique under realistic load.
//
// Format: 10 chars timestamp (ms since epoch, crockford base32) + 16 chars
// random crockford base32. Total: 26 chars, matching ULID width.
func generateJTI() string {
	now := time.Now().UTC()
	ms := uint64(now.UnixMilli())
	var tsBuf [10]byte
	for i := 9; i >= 0; i-- {
		tsBuf[i] = crockfordAlphabet[ms&0x1f]
		ms >>= 5
	}
	var randBuf [16]byte
	for i := range randBuf {
		var b [1]byte
		_, _ = rand.Read(b[:])
		randBuf[i] = crockfordAlphabet[b[0]&0x1f]
	}
	return string(tsBuf[:]) + string(randBuf[:])
}

// crockfordAlphabet is the 32-char alphabet used by ULID (Crockford base32 —
// no I/L/O/U for visual ambiguity). Public ordering matches the ULID spec so
// resulting strings sort correctly by time.
const crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// Compile-time interface assertion.
var _ AuditStore = (*SignedStore)(nil)
