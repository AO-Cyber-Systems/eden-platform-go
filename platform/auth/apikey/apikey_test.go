package apikey

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// memStore is a tiny in-memory Store for tests.
type memStore struct {
	candidates map[string][]Candidate
	used       map[string]time.Time
	lookupErr  error
}

func newMem() *memStore {
	return &memStore{
		candidates: make(map[string][]Candidate),
		used:       make(map[string]time.Time),
	}
}

func (s *memStore) LookupByPrefix(_ context.Context, prefix string) ([]Candidate, error) {
	if s.lookupErr != nil {
		return nil, s.lookupErr
	}
	return s.candidates[prefix], nil
}

func (s *memStore) MarkUsed(_ context.Context, keyID string, at time.Time) error {
	s.used[keyID] = at
	return nil
}

func mustHash(t *testing.T, raw string) string {
	t.Helper()
	h, err := HashKey(raw)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	return h
}

func TestAuthenticate_Success(t *testing.T) {
	rawKey := "aoc_abcdef1234567890"
	store := newMem()
	store.candidates["aoc_abcd"] = []Candidate{{
		ID:        "k-1",
		KeyDigest: mustHash(t, rawKey),
		UserID:    "u-1",
		Scopes:    []string{"read", "write"},
	}}
	a := New(store)

	got, err := a.Authenticate(context.Background(), rawKey)
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	if got.UserID != "u-1" || got.KeyID != "k-1" {
		t.Errorf("identity mismatch: %+v", got)
	}
	if len(got.Scopes) != 2 || got.Scopes[0] != "read" {
		t.Errorf("scopes: %v", got.Scopes)
	}
	if _, ok := store.used["k-1"]; !ok {
		t.Error("expected MarkUsed to be called")
	}
}

func TestAuthenticate_PrefixMismatch(t *testing.T) {
	a := New(newMem())
	if _, err := a.Authenticate(context.Background(), "wrong_prefix_xxx"); !errors.Is(err, ErrInvalidKeyFormat) {
		t.Errorf("expected ErrInvalidKeyFormat, got %v", err)
	}
}

func TestAuthenticate_TooShort(t *testing.T) {
	a := New(newMem())
	if _, err := a.Authenticate(context.Background(), "aoc_"); !errors.Is(err, ErrInvalidKeyFormat) {
		t.Errorf("expected ErrInvalidKeyFormat, got %v", err)
	}
}

func TestAuthenticate_NoCandidates(t *testing.T) {
	a := New(newMem())
	if _, err := a.Authenticate(context.Background(), "aoc_unknown1234"); !errors.Is(err, ErrKeyNotFound) {
		t.Errorf("expected ErrKeyNotFound, got %v", err)
	}
}

func TestAuthenticate_BadDigest(t *testing.T) {
	store := newMem()
	store.candidates["aoc_abcd"] = []Candidate{{
		ID:        "k-1",
		KeyDigest: mustHash(t, "different-key"),
		UserID:    "u-1",
	}}
	a := New(store)
	if _, err := a.Authenticate(context.Background(), "aoc_abcdef1234567890"); !errors.Is(err, ErrKeyNotFound) {
		t.Errorf("expected ErrKeyNotFound, got %v", err)
	}
}

func TestAuthenticate_StoreError(t *testing.T) {
	store := newMem()
	store.lookupErr = errors.New("db down")
	a := New(store)
	if _, err := a.Authenticate(context.Background(), "aoc_abcdef1234567890"); err == nil {
		t.Error("expected error for store failure")
	}
}

func TestAuthenticate_CustomPrefix(t *testing.T) {
	rawKey := "myapp_xxxxxxxxxxxxxxx"
	store := newMem()
	store.candidates["myapp_xx"] = []Candidate{{
		ID:        "k-1",
		KeyDigest: mustHash(t, rawKey),
		UserID:    "u-1",
	}}
	a := &Authenticator{Store: store, Prefix: "myapp_"}

	if _, err := a.Authenticate(context.Background(), rawKey); err != nil {
		t.Fatalf("auth: %v", err)
	}
}

func TestExtractFromHeaders(t *testing.T) {
	cases := []struct {
		name     string
		x        string
		auth     string
		query    string
		want     string
	}{
		{name: "x-api-key wins", x: "aoc_x", auth: "Bearer aoc_y", want: "aoc_x"},
		{name: "bearer fallback", auth: "Bearer aoc_y", want: "aoc_y"},
		{name: "bearer wrong prefix ignored", auth: "Bearer wrong_y", query: "q", want: "q"},
		{name: "query fallback", query: "q", want: "q"},
		{name: "none", want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractFromHeaders(DefaultPrefix, tc.x, tc.auth, tc.query)
			if got != tc.want {
				t.Errorf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestHashKeyAndCompare(t *testing.T) {
	hash, err := HashKey("aoc_secret")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte("aoc_secret")); err != nil {
		t.Errorf("compare matching: %v", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte("not_it")); err == nil {
		t.Error("expected compare to fail for wrong key")
	}
}

func TestGenerateKey(t *testing.T) {
	bytes := make([]byte, 16)
	_, _ = rand.Read(bytes)
	key := GenerateKey("aoc_", bytes)
	if !strings.HasPrefix(key, "aoc_") {
		t.Errorf("missing prefix: %s", key)
	}
	// 16 random bytes encoded as hex = 32 chars
	if len(key) != len("aoc_")+32 {
		t.Errorf("unexpected length: %d (%s)", len(key), key)
	}
}

func TestGenerateKey_DefaultPrefix(t *testing.T) {
	key := GenerateKey("", []byte{0x01, 0x02})
	if !strings.HasPrefix(key, DefaultPrefix) {
		t.Errorf("default prefix not applied: %s", key)
	}
}
