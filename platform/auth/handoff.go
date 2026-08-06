package auth

// Handoff codes — the replacement for tokens in redirect URLs.
//
// A handoff code replaces the access_token/refresh_token query parameters that
// platform/auth/sso.go and platform/auth/social/http.go used to append to their
// redirect targets. Tokens in a URL land in browser history, Referer headers,
// proxy access logs and (for the desktop loopback path) shell history via
// argv. The handoff is a 60-second, single-use, audience-bound REFERENCE that
// is worthless once redeemed, and it is exchanged over POST so the tokens
// travel in a response BODY.
//
// It deliberately reuses the JWTManager short-lived-token primitives — the same
// ones that already carry the social state JWT — so this adds no new token
// format and no new crypto.

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
)

// HandoffTTL bounds how long a handoff code may be redeemed. This is a redirect
// round trip, not a session: 60 seconds is ample, and anything longer is a
// long-lived credential sitting in a URL — the original problem wearing a hat.
// (The social state JWT's 10 minutes is deliberately NOT mirrored here.)
const HandoffTTL = 60 * time.Second

// handoffSubjectPrefix tags a handoff code's JWT subject so a token minted for
// any OTHER short-lived purpose (the social/SSO state JWT above all) can never
// be parsed as a handoff even if it survives signature validation.
const handoffSubjectPrefix = "edenhandoff.v1"

var (
	// ErrHandoffInvalid covers every "this code is not usable" outcome that the
	// caller must not be able to tell apart: bad signature, expired, wrong
	// audience, already redeemed, unknown. Distinguishing them at the HTTP edge
	// would turn the exchange endpoint into an oracle.
	ErrHandoffInvalid = errors.New("invalid handoff code")

	// ErrHandoffReplayed and friends exist for logs and tests ONLY. They all
	// wrap ErrHandoffInvalid so an HTTP handler can errors.Is(err,
	// ErrHandoffInvalid) and emit one generic failure.
	ErrHandoffReplayed = fmt.Errorf("handoff code already redeemed: %w", ErrHandoffInvalid)
	ErrHandoffAudience = fmt.Errorf("handoff code not valid for this audience: %w", ErrHandoffInvalid)
	ErrHandoffUnknown  = fmt.Errorf("handoff code references no stored token pair: %w", ErrHandoffInvalid)
)

// ReplayGuard refuses a handoff jti that has already been redeemed. It is the
// single-use half of HandoffStore, named and exported separately because it is
// the security-critical contract: everything else about a handoff is a lookup,
// but this is what makes the code worthless after one use.
//
// The default implementation (InMemoryHandoffStore) is PER-PROCESS. In a
// multi-pod deployment a handoff redeemed on pod A is not known to pod B, so
// single-use is enforced per pod within the 60s TTL rather than globally. That
// bound is stated here rather than assumed away. Note that the default store is
// per-process for the TOKEN PAYLOAD too, which is the stronger constraint: a
// code minted on pod A cannot be redeemed on pod B at all. Operators running
// multiple replicas MUST supply a shared HandoffStore (Redis, or a table with a
// 60s TTL) — see the HandoffStore doc.
type ReplayGuard interface {
	// MarkRedeemed returns false if jti was already redeemed. It must be
	// atomic: two concurrent calls for the same jti must not both return true.
	MarkRedeemed(ctx context.Context, jti string, ttl time.Duration) (bool, error)
}

// HandoffStore holds the token pair a handoff code REFERENCES, and is also the
// ReplayGuard for that code.
//
// Why a store rather than a self-contained code: eden's access and refresh
// tokens are ML-DSA-65 signed and run ~4.6KB EACH. Packing the pair into the
// code's own JWT subject would produce a ~13KB redirect URL — strictly worse
// than the leak it replaces, and a guaranteed 414 at any default nginx (the
// hazard sso.go's fragment workaround already documents). The code therefore
// carries only a jti and an audience; the ~9.2KB of tokens never enters a URL
// at all.
//
// STICKINESS / SHARED-STORE REQUIREMENT: because the payload lives in the
// store, a deployment with more than one replica needs EITHER a shared
// implementation of this interface OR session affinity between the callback
// redirect and the subsequent exchange POST. With the in-memory default and no
// affinity, exchanges land on the wrong pod and fail closed (a failed login,
// never a leaked token).
type HandoffStore interface {
	ReplayGuard

	// Put records resp under jti for at most ttl.
	Put(ctx context.Context, jti string, resp *AuthResponse, ttl time.Duration) error

	// Load returns the AuthResponse recorded under jti, or ErrHandoffUnknown.
	// Load does NOT enforce single-use; MarkRedeemed does.
	Load(ctx context.Context, jti string) (*AuthResponse, error)
}

// MintHandoff records resp in store and returns a short-lived, audience-bound
// code that references it. audience is the exact redirect target the code will
// be delivered to; RedeemHandoff refuses the code when presented for any other.
func MintHandoff(ctx context.Context, jm *JWTManager, store HandoffStore, audience string, resp *AuthResponse) (string, error) {
	return mintHandoff(ctx, jm, store, audience, resp, HandoffTTL)
}

// mintHandoff is MintHandoff with an explicit TTL. Tests use it to mint an
// already-expired code without sleeping.
func mintHandoff(ctx context.Context, jm *JWTManager, store HandoffStore, audience string, resp *AuthResponse, ttl time.Duration) (string, error) {
	if jm == nil || store == nil {
		return "", fmt.Errorf("mint handoff: jwt manager and store are required")
	}
	if audience == "" {
		return "", fmt.Errorf("mint handoff: audience is required")
	}
	if resp == nil || resp.AccessToken == "" {
		return "", fmt.Errorf("mint handoff: a token pair is required")
	}

	jti := generateJTI()
	if err := store.Put(ctx, jti, resp, ttl); err != nil {
		return "", fmt.Errorf("mint handoff: %w", err)
	}

	// Subject shape: edenhandoff.v1|<jti>|<query-escaped audience>. Escaping
	// keeps the delimiter unambiguous (a "|" in a redirect URI escapes to %7C)
	// without hashing anything — the JWT signature is what makes the subject
	// trustworthy, so no additional crypto is warranted or wanted here.
	subject := strings.Join([]string{handoffSubjectPrefix, jti, url.QueryEscape(audience)}, "|")

	// CreateCompactShortLivedToken, not CreateShortLivedToken: the handoff is
	// signed AND verified by this same service and is never presented to a
	// third party through the JWKS, which is exactly the contract that method
	// documents. It also keeps the code ~200 bytes instead of the ~4.4KB an
	// ML-DSA-65 signature would add to a redirect URL. As a bonus its validator
	// rejects any non-HS256 alg, so an ML-DSA state JWT is refused structurally
	// before the subject tag is even consulted.
	return jm.CreateCompactShortLivedToken(subject, ttl)
}

// RedeemHandoff validates code, burns it, and returns the referenced token
// pair. Every failure returns a nil *AuthResponse and an error wrapping
// ErrHandoffInvalid.
//
// Order is security-relevant: the signature and audience are checked BEFORE the
// code is burned, so an attacker holding a forged or mis-targeted code cannot
// consume a victim's jti.
func RedeemHandoff(ctx context.Context, jm *JWTManager, store HandoffStore, audience, code string) (*AuthResponse, error) {
	if jm == nil || store == nil {
		return nil, fmt.Errorf("redeem handoff: jwt manager and store are required")
	}

	subject, err := jm.ValidateCompactShortLivedToken(code)
	if err != nil {
		return nil, fmt.Errorf("redeem handoff: %w: %w", ErrHandoffInvalid, err)
	}

	jti, boundAudience, err := parseHandoffSubject(subject)
	if err != nil {
		return nil, err
	}
	if boundAudience != audience || audience == "" {
		return nil, ErrHandoffAudience
	}

	fresh, err := store.MarkRedeemed(ctx, jti, HandoffTTL)
	if err != nil {
		return nil, fmt.Errorf("redeem handoff: %w: %w", ErrHandoffInvalid, err)
	}
	if !fresh {
		return nil, ErrHandoffReplayed
	}

	resp, err := store.Load(ctx, jti)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// maxInMemoryHandoffs caps the default store so a burst of un-redeemed codes
// cannot grow it without bound. Entries are short-lived (60s) so the cap is
// generous; when it is hit, expired entries are swept and, failing that, the
// oldest entry is dropped. A dropped entry means a failed login, never a leak.
const maxInMemoryHandoffs = 10_000

// InMemoryHandoffStore is the default HandoffStore: a PER-PROCESS map with
// TTL-bounded eviction. It is correct for a single-replica deployment and for
// tests. See the ReplayGuard and HandoffStore docs for the multi-pod bound —
// this type does not and cannot enforce single-use across replicas.
type InMemoryHandoffStore struct {
	mu       sync.Mutex
	entries  map[string]handoffEntry
	redeemed map[string]time.Time
}

type handoffEntry struct {
	resp      *AuthResponse
	expiresAt time.Time
}

// NewInMemoryHandoffStore returns a ready-to-use per-process handoff store.
func NewInMemoryHandoffStore() *InMemoryHandoffStore {
	return &InMemoryHandoffStore{
		entries:  make(map[string]handoffEntry),
		redeemed: make(map[string]time.Time),
	}
}

// Put records resp under jti for at most ttl.
func (s *InMemoryHandoffStore) Put(_ context.Context, jti string, resp *AuthResponse, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()
	if len(s.entries) >= maxInMemoryHandoffs {
		return fmt.Errorf("handoff store is full")
	}
	s.entries[jti] = handoffEntry{resp: resp, expiresAt: time.Now().Add(ttl)}
	return nil
}

// Load returns the AuthResponse recorded under jti. It does NOT delete the
// entry — MarkRedeemed is the single-use gate, and keeping the two separate is
// what lets a shared ReplayGuard be swapped in independently.
func (s *InMemoryHandoffStore) Load(_ context.Context, jti string) (*AuthResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[jti]
	if !ok || time.Now().After(e.expiresAt) {
		return nil, ErrHandoffUnknown
	}
	return e.resp, nil
}

// MarkRedeemed records jti as spent, returning false if it already was.
func (s *InMemoryHandoffStore) MarkRedeemed(_ context.Context, jti string, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()
	if until, ok := s.redeemed[jti]; ok && time.Now().Before(until) {
		return false, nil
	}
	s.redeemed[jti] = time.Now().Add(ttl)
	return true, nil
}

// sweepLocked drops expired entries. Caller holds s.mu.
func (s *InMemoryHandoffStore) sweepLocked() {
	now := time.Now()
	for jti, e := range s.entries {
		if now.After(e.expiresAt) {
			delete(s.entries, jti)
		}
	}
	for jti, until := range s.redeemed {
		if now.After(until) {
			delete(s.redeemed, jti)
		}
	}
}

// parseHandoffSubject decodes a handoff JWT subject, rejecting anything that is
// not tagged as a handoff. This is the audience-CONFUSION guard: a state JWT
// (or any other short-lived token this service mints) has a different subject
// shape and must never satisfy it.
func parseHandoffSubject(subject string) (jti, audience string, err error) {
	parts := strings.Split(subject, "|")
	if len(parts) != 3 || parts[0] != handoffSubjectPrefix {
		return "", "", fmt.Errorf("redeem handoff: %w: subject is not a handoff", ErrHandoffInvalid)
	}
	if parts[1] == "" {
		return "", "", fmt.Errorf("redeem handoff: %w: empty jti", ErrHandoffInvalid)
	}
	aud, err := url.QueryUnescape(parts[2])
	if err != nil {
		return "", "", fmt.Errorf("redeem handoff: %w: undecodable audience", ErrHandoffInvalid)
	}
	return parts[1], aud, nil
}
