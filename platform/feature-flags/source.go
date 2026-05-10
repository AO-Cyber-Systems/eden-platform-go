package featureflags

import (
	"context"
	"os"
	"strings"
	"sync"
)

// Source is the pluggable backing store the Client reads flags from. Lookups
// must be cheap; the Client treats a Source as the read-mostly hot path.
//
// Implementations:
//
//   - MemorySource — in-process map; ideal for tests and small static configs.
//   - EnvSource — reads FEATURE_FLAGS_<KEY> env vars (boolean flags only).
//   - Custom — wrap a remote service (Eden-Biz, Unleash, ConfigCat) by
//     implementing Lookup. Cache aggressively in your wrapper.
//
// Lookup contract:
//   - (Flag, true, nil) — flag found and returned
//   - (Flag{}, false, nil) — flag not found (caller defaults to OFF)
//   - (Flag{}, false, err) — lookup failed (caller defaults to OFF)
type Source interface {
	Lookup(ctx context.Context, key string) (Flag, bool, error)
}

// MemorySource is a thread-safe in-memory Source. Useful for tests, embedded
// defaults, and cases where flags are loaded once at boot.
type MemorySource struct {
	mu    sync.RWMutex
	flags map[string]Flag
}

// NewMemorySource returns an empty MemorySource. Add flags with Set.
func NewMemorySource() *MemorySource {
	return &MemorySource{flags: make(map[string]Flag)}
}

// NewMemorySourceWithFlags returns a MemorySource pre-populated with the given
// flags, keyed by Flag.Key.
func NewMemorySourceWithFlags(flags ...Flag) *MemorySource {
	s := NewMemorySource()
	for _, f := range flags {
		s.Set(f)
	}
	return s
}

// Set inserts or replaces a flag.
func (s *MemorySource) Set(f Flag) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flags[f.Key] = f
}

// Delete removes a flag. No error if the key isn't present.
func (s *MemorySource) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.flags, key)
}

// Lookup implements Source.
func (s *MemorySource) Lookup(_ context.Context, key string) (Flag, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	f, ok := s.flags[key]
	return f, ok, nil
}

// EnvSource reads boolean feature flags from environment variables. The env
// var name is "FEATURE_FLAGS_" + uppercased key, with non-alphanumeric
// characters replaced by '_'.
//
// Examples:
//
//	"household_billing" → FEATURE_FLAGS_HOUSEHOLD_BILLING
//	"new-dashboard"     → FEATURE_FLAGS_NEW_DASHBOARD
//
// Recognized truthy values: "1","true","TRUE","yes","on" (case-insensitive).
// Anything else returns Enabled=false. Variant flags are NOT supported via
// this source — use MemorySource or a custom Source for those.
type EnvSource struct {
	// Prefix is the env-var prefix. Defaults to "FEATURE_FLAGS_" if empty.
	Prefix string

	// Lookup is the function used to read env vars. Defaults to os.LookupEnv.
	// Override in tests to avoid touching the real environment.
	LookupFn func(string) (string, bool)
}

// NewEnvSource returns a default EnvSource (FEATURE_FLAGS_ prefix, os.LookupEnv).
func NewEnvSource() *EnvSource {
	return &EnvSource{}
}

func (s *EnvSource) prefix() string {
	if s.Prefix != "" {
		return s.Prefix
	}
	return "FEATURE_FLAGS_"
}

func (s *EnvSource) lookup(name string) (string, bool) {
	if s.LookupFn != nil {
		return s.LookupFn(name)
	}
	return os.LookupEnv(name)
}

// Lookup implements Source.
func (s *EnvSource) Lookup(_ context.Context, key string) (Flag, bool, error) {
	envKey := s.prefix() + envify(key)
	v, ok := s.lookup(envKey)
	if !ok {
		return Flag{}, false, nil
	}
	return Flag{
		Key:     key,
		Enabled: parseBool(v),
	}, true, nil
}

func envify(key string) string {
	var b strings.Builder
	b.Grow(len(key))
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - 'a' + 'A')
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

func parseBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
