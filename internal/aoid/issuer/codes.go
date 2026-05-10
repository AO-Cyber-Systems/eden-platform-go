package issuer

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"time"
)

// AuthCode is a single-use authorization code issued by /oauth2/authorize
// and consumed by /oauth2/token. The code itself is an opaque random
// 256-bit token; everything else here is the binding context for PKCE
// + redirect + nonce verification at exchange time.
type AuthCode struct {
	Code                string
	ClientID            string
	UserID              string
	Scope               []string
	RedirectURI         string
	CodeChallenge       string
	CodeChallengeMethod string
	Nonce               string
	ExpiresAt           time.Time
	CreatedAt           time.Time
}

// CodeStore is the persistence interface for auth codes. The in-memory
// implementation is fine for Phase A; production volume + cross-instance
// fanout pushes us to a Redis or pgstore-backed store later.
type CodeStore interface {
	Save(ctx context.Context, code AuthCode) error
	Consume(ctx context.Context, codeStr string) (*AuthCode, error)
}

// Errors returned by CodeStore.
var (
	ErrCodeNotFound  = errors.New("code not found")
	ErrCodeExpired   = errors.New("code expired")
	ErrCodeConsumed  = errors.New("code already consumed")
)

// MemoryCodeStore is a thread-safe in-memory CodeStore. A background
// pruner runs every PruneInterval to drop expired entries; tests can
// disable this by passing 0.
type MemoryCodeStore struct {
	mu      sync.Mutex
	codes   map[string]*AuthCode
	stop    chan struct{}
	stopped chan struct{}
}

// PruneInterval is the cadence at which expired codes are reaped.
const PruneInterval = 60 * time.Second

// NewMemoryCodeStore constructs a MemoryCodeStore and starts the
// pruner. Call Stop to release the goroutine when the issuer shuts
// down.
func NewMemoryCodeStore() *MemoryCodeStore {
	s := &MemoryCodeStore{
		codes:   make(map[string]*AuthCode),
		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
	go s.prune()
	return s
}

// Save inserts a new code. The Code field must be non-empty.
func (s *MemoryCodeStore) Save(_ context.Context, c AuthCode) error {
	if c.Code == "" {
		return fmt.Errorf("codes: empty code string")
	}
	if c.ExpiresAt.IsZero() {
		return fmt.Errorf("codes: ExpiresAt unset")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.codes[c.Code]; exists {
		return fmt.Errorf("codes: duplicate code")
	}
	cp := c
	cp.Scope = append([]string(nil), c.Scope...)
	s.codes[c.Code] = &cp
	return nil
}

// Consume atomically removes-and-returns a code. Returns ErrCodeNotFound
// if the code doesn't exist (which covers both "never issued" and
// "already consumed"); ErrCodeExpired if found but past ExpiresAt.
//
// Single-use semantics: a successful Consume removes the entry, so
// reuse attempts surface as ErrCodeNotFound.
func (s *MemoryCodeStore) Consume(_ context.Context, codeStr string) (*AuthCode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.codes[codeStr]
	if !ok {
		return nil, ErrCodeNotFound
	}
	delete(s.codes, codeStr)
	if time.Now().After(c.ExpiresAt) {
		return nil, ErrCodeExpired
	}
	cp := *c
	cp.Scope = append([]string(nil), c.Scope...)
	return &cp, nil
}

// Stop halts the prune goroutine. Safe to call multiple times.
func (s *MemoryCodeStore) Stop() {
	select {
	case <-s.stop:
		// already stopped
	default:
		close(s.stop)
		<-s.stopped
	}
}

func (s *MemoryCodeStore) prune() {
	defer close(s.stopped)
	t := time.NewTicker(PruneInterval)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			return
		case now := <-t.C:
			s.mu.Lock()
			for k, c := range s.codes {
				if now.After(c.ExpiresAt) {
					delete(s.codes, k)
				}
			}
			s.mu.Unlock()
		}
	}
}

// generateCode returns a base64-url-encoded 32-byte random string —
// 256 bits of entropy, plenty for an opaque single-use code.
func generateCode() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
