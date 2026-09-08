package telephony

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// fakeStore is a minimal ConfigStore for resolver tests.
type fakeStore struct {
	configs    map[uuid.UUID]Config
	getCalls   int
	getErr     error
	notFoundIs []uuid.UUID // companies that should return ErrTenantNotConfigured
}

func newFakeStore() *fakeStore { return &fakeStore{configs: map[uuid.UUID]Config{}} }

func (f *fakeStore) Get(_ context.Context, companyID uuid.UUID) (Config, error) {
	f.getCalls++
	if f.getErr != nil {
		return Config{}, f.getErr
	}
	for _, id := range f.notFoundIs {
		if id == companyID {
			return Config{}, ErrTenantNotConfigured
		}
	}
	c, ok := f.configs[companyID]
	if !ok {
		return Config{}, ErrTenantNotConfigured
	}
	return c, nil
}

func (f *fakeStore) LookupBySendingNumber(_ context.Context, _ ProviderType, _ string) (Config, error) {
	return Config{}, ErrTenantNotConfigured
}
func (f *fakeStore) Upsert(_ context.Context, _ Config) error        { return nil }
func (f *fakeStore) Deactivate(_ context.Context, _ uuid.UUID) error { return nil }
func (f *fakeStore) List(_ context.Context) ([]Config, error)        { return nil, nil }

// fakeEnt is a minimal EntitlementChecker for tier-gate tests.
type fakeEnt struct{ allowed bool }

func (f fakeEnt) Allowed(_ context.Context, _ uuid.UUID, _ string) (bool, string, error) {
	return f.allowed, "local", nil
}

// fakePlatformStore is a minimal PlatformDefaultStore for resolver tests. When
// has is true it returns cfg as the platform default; otherwise it returns
// ErrTenantNotConfigured (no platform default configured).
type fakePlatformStore struct {
	cfg   Config
	has   bool
	calls int
}

func (f *fakePlatformStore) TwilioPlatformDefault(_ context.Context) (Config, error) {
	f.calls++
	if !f.has {
		return Config{}, ErrTenantNotConfigured
	}
	return f.cfg, nil
}

// Test: a company row present resolves the company provider WITHOUT
// consulting the platform-default store (precedence tier 1).
func TestResolver_CompanyRowBeatsPlatformDefault(t *testing.T) {
	store := newFakeStore()
	reg := NewRegistry()
	reg.Register(ProviderTwilio, func(c Config) (Provider, error) {
		return stubProvider{pType: c.Provider}, nil
	})
	platform := &fakePlatformStore{has: true, cfg: Config{Provider: ProviderTwilio, AccountSID: "PLAT", AuthToken: "p", SendingNumber: "+1"}}
	r := NewResolver(store, reg, stubProvider{pType: "fallback"}, nil)
	r.WithPlatformDefaults(platform)

	company := uuid.New()
	store.configs[company] = Config{CompanyID: company, Provider: ProviderTwilio, AccountSID: "COMP", AuthToken: "c", SendingNumber: "+2"}

	p, err := r.For(context.Background(), company)
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	if p.Type() != ProviderTwilio {
		t.Errorf("type = %s; want twilio (company row)", p.Type())
	}
	if platform.calls != 0 {
		t.Errorf("platform store consulted %d times; want 0 when a company row exists", platform.calls)
	}
}

// Test: no company row but a platform default present resolves the platform
// provider (precedence tier 2).
func TestResolver_PlatformDefaultWhenNoCompanyRow(t *testing.T) {
	store := newFakeStore()
	reg := NewRegistry()
	reg.Register(ProviderTwilio, func(c Config) (Provider, error) {
		return stubProvider{pType: ProviderType("platform-" + string(c.Provider))}, nil
	})
	platform := &fakePlatformStore{has: true, cfg: Config{Provider: ProviderTwilio, AccountSID: "PLAT", AuthToken: "p", SendingNumber: "+1"}}
	r := NewResolver(store, reg, stubProvider{pType: "fallback"}, nil)
	r.WithPlatformDefaults(platform)

	company := uuid.New()
	store.notFoundIs = []uuid.UUID{company}

	p, err := r.For(context.Background(), company)
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	if p.Type() != ProviderType("platform-twilio") {
		t.Errorf("type = %s; want platform-twilio", p.Type())
	}
	if platform.calls != 1 {
		t.Errorf("platform store consulted %d times; want 1", platform.calls)
	}
}

// Test: no company row AND no platform default falls through to the
// caller-supplied fallback UNCHANGED (precedence tier 3 — live tenant
// unbroken). Extends TestResolver_FallbackOnMissingTenantRow with a platform
// store wired but empty.
func TestResolver_FallbackWhenNoPlatformDefault(t *testing.T) {
	store := newFakeStore()
	reg := NewRegistry()
	platform := &fakePlatformStore{has: false} // configured but returns ErrTenantNotConfigured
	r := NewResolver(store, reg, stubProvider{pType: "fallback"}, nil)
	r.WithPlatformDefaults(platform)

	company := uuid.New()
	store.notFoundIs = []uuid.UUID{company}

	p, err := r.For(context.Background(), company)
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	if p.Type() != ProviderType("fallback") {
		t.Errorf("type = %s; want fallback (final tier)", p.Type())
	}
	if platform.calls != 1 {
		t.Errorf("platform store consulted %d times; want 1 (then fallback)", platform.calls)
	}
}

// Test: a nil platformStore behaves EXACTLY as today — fallback on a missing
// company row (current behavior / dev preserved).
func TestResolver_NilPlatformStorePreservesBehavior(t *testing.T) {
	store := newFakeStore()
	reg := NewRegistry()
	r := NewResolver(store, reg, stubProvider{pType: "fallback"}, nil)
	// No WithPlatformDefaults call => platformStore stays nil.

	company := uuid.New()
	store.notFoundIs = []uuid.UUID{company}

	p, err := r.For(context.Background(), company)
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	if p.Type() != ProviderType("fallback") {
		t.Errorf("type = %s; want fallback with nil platform store", p.Type())
	}
}

// Test: InvalidateAll clears EVERY cached entry — a previously cached company
// re-resolves on the next For (platform-default writes flush all).
func TestResolver_InvalidateAllClearsCache(t *testing.T) {
	store := newFakeStore()
	reg := NewRegistry()
	reg.Register(ProviderTwilio, func(c Config) (Provider, error) {
		return stubProvider{pType: c.Provider}, nil
	})
	r := NewResolver(store, reg, nil, nil)

	a := uuid.New()
	b := uuid.New()
	store.configs[a] = Config{CompanyID: a, Provider: ProviderTwilio, AccountSID: "A", AuthToken: "t", SendingNumber: "+1"}
	store.configs[b] = Config{CompanyID: b, Provider: ProviderTwilio, AccountSID: "B", AuthToken: "t", SendingNumber: "+2"}

	// Prime both into the cache.
	if _, err := r.For(context.Background(), a); err != nil {
		t.Fatalf("For a: %v", err)
	}
	if _, err := r.For(context.Background(), b); err != nil {
		t.Fatalf("For b: %v", err)
	}
	if store.getCalls != 2 {
		t.Fatalf("getCalls = %d after priming; want 2", store.getCalls)
	}

	r.InvalidateAll()

	// Both must re-resolve (cache flushed).
	if _, err := r.For(context.Background(), a); err != nil {
		t.Fatalf("For a (post-flush): %v", err)
	}
	if _, err := r.For(context.Background(), b); err != nil {
		t.Fatalf("For b (post-flush): %v", err)
	}
	if store.getCalls != 4 {
		t.Errorf("getCalls = %d after InvalidateAll; want 4 (both re-resolved)", store.getCalls)
	}
}

func TestResolver_FallbackOnMissingTenantRow(t *testing.T) {
	store := newFakeStore()
	reg := NewRegistry()
	fallback := stubProvider{pType: "fallback"}
	r := NewResolver(store, reg, fallback, nil)

	company := uuid.New()
	store.notFoundIs = []uuid.UUID{company}
	p, err := r.For(context.Background(), company)
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	if p.Type() != "fallback" {
		t.Errorf("got %s, want fallback", p.Type())
	}
}

func TestResolver_NoFallbackReturnsNoop(t *testing.T) {
	store := newFakeStore()
	reg := NewRegistry()
	r := NewResolver(store, reg, nil, nil)

	company := uuid.New()
	store.notFoundIs = []uuid.UUID{company}
	p, err := r.For(context.Background(), company)
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	if p.Type() != ProviderType("noop") {
		t.Errorf("got %s, want noop", p.Type())
	}
}

func TestResolver_RegistryBuildsForConfiguredTenant(t *testing.T) {
	store := newFakeStore()
	reg := NewRegistry()
	reg.Register(ProviderTwilio, func(c Config) (Provider, error) {
		return stubProvider{pType: c.Provider}, nil
	})

	r := NewResolver(store, reg, nil, nil)

	company := uuid.New()
	store.configs[company] = Config{
		CompanyID: company, Provider: ProviderTwilio,
		AccountSID: "AC", AuthToken: "tok", SendingNumber: "+1",
	}

	p, err := r.For(context.Background(), company)
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	if p.Type() != ProviderTwilio {
		t.Errorf("type = %s; want twilio", p.Type())
	}
}

func TestResolver_CachesWithinTTL(t *testing.T) {
	store := newFakeStore()
	reg := NewRegistry()
	reg.Register(ProviderTwilio, func(c Config) (Provider, error) {
		return stubProvider{pType: c.Provider}, nil
	})
	r := NewResolver(store, reg, nil, nil)

	company := uuid.New()
	store.configs[company] = Config{
		CompanyID: company, Provider: ProviderTwilio,
		AccountSID: "AC", AuthToken: "t", SendingNumber: "+1",
	}

	for i := 0; i < 5; i++ {
		if _, err := r.For(context.Background(), company); err != nil {
			t.Fatalf("For: %v", err)
		}
	}
	if store.getCalls != 1 {
		t.Errorf("Get called %d times; want 1 due to caching", store.getCalls)
	}
}

func TestResolver_ExpiresAfterTTL(t *testing.T) {
	store := newFakeStore()
	reg := NewRegistry()
	reg.Register(ProviderTwilio, func(c Config) (Provider, error) {
		return stubProvider{pType: c.Provider}, nil
	})
	r := NewResolver(store, reg, nil, nil)

	// Override clock + TTL.
	now := time.Now()
	r.SetTTL(10 * time.Second)
	r.now = func() time.Time { return now }

	company := uuid.New()
	store.configs[company] = Config{
		CompanyID: company, Provider: ProviderTwilio,
		AccountSID: "AC", AuthToken: "t", SendingNumber: "+1",
	}
	if _, err := r.For(context.Background(), company); err != nil {
		t.Fatalf("first For: %v", err)
	}
	// Advance past TTL.
	now = now.Add(20 * time.Second)
	if _, err := r.For(context.Background(), company); err != nil {
		t.Fatalf("second For: %v", err)
	}
	if store.getCalls != 2 {
		t.Errorf("Get called %d times; want 2 across TTL boundary", store.getCalls)
	}
}

func TestResolver_InvalidateBustsCache(t *testing.T) {
	store := newFakeStore()
	reg := NewRegistry()
	reg.Register(ProviderTwilio, func(c Config) (Provider, error) {
		return stubProvider{pType: c.Provider}, nil
	})
	r := NewResolver(store, reg, nil, nil)
	company := uuid.New()
	store.configs[company] = Config{CompanyID: company, Provider: ProviderTwilio, AccountSID: "AC", AuthToken: "t", SendingNumber: "+1"}

	if _, err := r.For(context.Background(), company); err != nil {
		t.Fatalf("For: %v", err)
	}
	r.Invalidate(company)
	if _, err := r.For(context.Background(), company); err != nil {
		t.Fatalf("For: %v", err)
	}
	if store.getCalls != 2 {
		t.Errorf("Get called %d times after invalidate; want 2", store.getCalls)
	}
}

func TestResolver_TierGate_SignalWireBlocked(t *testing.T) {
	store := newFakeStore()
	reg := NewRegistry()
	reg.Register(ProviderSignalWire, func(c Config) (Provider, error) {
		return stubProvider{pType: c.Provider}, nil
	})
	r := NewResolver(store, reg, nil, fakeEnt{allowed: false})

	company := uuid.New()
	store.configs[company] = Config{
		CompanyID: company, Provider: ProviderSignalWire,
		AccountSID: "PRJ", AuthToken: "t", SpaceURL: "https://x.signalwire.com", SendingNumber: "+1",
	}
	_, err := r.For(context.Background(), company)
	if !errors.Is(err, ErrTierNotAllowed) {
		t.Errorf("err = %v; want ErrTierNotAllowed", err)
	}
}

func TestResolver_TierGate_TwilioNotChecked(t *testing.T) {
	// Twilio shouldn't trigger the tier gate even if the checker disallows.
	store := newFakeStore()
	reg := NewRegistry()
	reg.Register(ProviderTwilio, func(c Config) (Provider, error) {
		return stubProvider{pType: c.Provider}, nil
	})
	r := NewResolver(store, reg, nil, fakeEnt{allowed: false})

	company := uuid.New()
	store.configs[company] = Config{
		CompanyID: company, Provider: ProviderTwilio,
		AccountSID: "AC", AuthToken: "t", SendingNumber: "+1",
	}
	if _, err := r.For(context.Background(), company); err != nil {
		t.Errorf("For: %v", err)
	}
}

func TestResolver_TierGate_SignalWireAllowed(t *testing.T) {
	store := newFakeStore()
	reg := NewRegistry()
	reg.Register(ProviderSignalWire, func(c Config) (Provider, error) {
		return stubProvider{pType: c.Provider}, nil
	})
	r := NewResolver(store, reg, nil, fakeEnt{allowed: true})

	company := uuid.New()
	store.configs[company] = Config{
		CompanyID: company, Provider: ProviderSignalWire,
		AccountSID: "PRJ", AuthToken: "t", SpaceURL: "https://x.signalwire.com", SendingNumber: "+1",
	}
	if _, err := r.For(context.Background(), company); err != nil {
		t.Errorf("For: %v", err)
	}
}
