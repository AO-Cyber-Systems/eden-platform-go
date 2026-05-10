package federation

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func newTenantConfig(t *testing.T) TenantIdPConfig {
	t.Helper()
	return TenantIdPConfig{
		TenantID:    uuid.New(),
		DisplayName: "Acme Corp",
		EntityID:    "https://id.aocyber.com/saml/idp/acme/metadata",
		SSOURL:      "https://id.aocyber.com/saml/idp/acme/sso",
		MetadataURL: "https://id.aocyber.com/saml/idp/acme/metadata",
		AllowedSPs: map[string]SPRegistration{
			"https://partner.example.com/saml/metadata": {
				EntityID: "https://partner.example.com/saml/metadata",
				ACSURL:   "https://partner.example.com/saml/acs",
				Label:    "Partner Portal",
			},
		},
		AttributeTemplate: map[string][]string{
			"email":      {"email"},
			"first_name": {"given_name"},
		},
		AssertionLifetime: 5 * time.Minute,
		IsActive:          true,
	}
}

func TestInMemoryRegistry_CRUD(t *testing.T) {
	r := NewInMemoryRegistry()
	ctx := context.Background()

	cfg := newTenantConfig(t)
	if err := r.Register(ctx, cfg); err != nil {
		t.Fatalf("Register: %v", err)
	}

	got, err := r.Get(ctx, cfg.TenantID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.DisplayName != "Acme Corp" {
		t.Errorf("DisplayName: got %q, want Acme Corp", got.DisplayName)
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Errorf("expected CreatedAt+UpdatedAt to be stamped")
	}

	// Update
	cfg.DisplayName = "Acme Corp (Production)"
	if err := r.Update(ctx, cfg); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, _ = r.Get(ctx, cfg.TenantID)
	if got.DisplayName != "Acme Corp (Production)" {
		t.Errorf("after update DisplayName: got %q", got.DisplayName)
	}
	if !got.UpdatedAt.After(got.CreatedAt) && !got.UpdatedAt.Equal(got.CreatedAt) {
		t.Errorf("UpdatedAt should be >= CreatedAt")
	}

	// List
	list, _ := r.List(ctx, true)
	if len(list) != 1 {
		t.Errorf("List(activeOnly=true): got %d, want 1", len(list))
	}

	// Delete
	if err := r.Delete(ctx, cfg.TenantID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := r.Get(ctx, cfg.TenantID); !errors.Is(err, ErrTenantNotFound) {
		t.Errorf("expected ErrTenantNotFound, got %v", err)
	}
}

func TestInMemoryRegistry_Validation(t *testing.T) {
	r := NewInMemoryRegistry()
	ctx := context.Background()

	cases := []struct {
		name string
		cfg  TenantIdPConfig
	}{
		{"missing tenant", TenantIdPConfig{EntityID: "x", SSOURL: "y"}},
		{"missing entity id", TenantIdPConfig{TenantID: uuid.New(), SSOURL: "y"}},
		{"missing sso url", TenantIdPConfig{TenantID: uuid.New(), EntityID: "x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := r.Register(ctx, tc.cfg)
			if !errors.Is(err, ErrInvalidConfig) {
				t.Errorf("expected ErrInvalidConfig, got %v", err)
			}
		})
	}
}

func TestInMemoryRegistry_Duplicate(t *testing.T) {
	r := NewInMemoryRegistry()
	ctx := context.Background()
	cfg := newTenantConfig(t)
	if err := r.Register(ctx, cfg); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	if err := r.Register(ctx, cfg); !errors.Is(err, ErrDuplicateTenant) {
		t.Errorf("expected ErrDuplicateTenant, got %v", err)
	}
}

func TestInMemoryRegistry_UpdateUnknown(t *testing.T) {
	r := NewInMemoryRegistry()
	ctx := context.Background()
	cfg := newTenantConfig(t)
	if err := r.Update(ctx, cfg); !errors.Is(err, ErrTenantNotFound) {
		t.Errorf("expected ErrTenantNotFound, got %v", err)
	}
}

func TestInMemoryRegistry_ListActiveOnly(t *testing.T) {
	r := NewInMemoryRegistry()
	ctx := context.Background()

	active := newTenantConfig(t)
	inactive := newTenantConfig(t)
	inactive.IsActive = false
	_ = r.Register(ctx, active)
	_ = r.Register(ctx, inactive)

	all, _ := r.List(ctx, false)
	if len(all) != 2 {
		t.Errorf("List(false): got %d, want 2", len(all))
	}
	activeOnly, _ := r.List(ctx, true)
	if len(activeOnly) != 1 {
		t.Errorf("List(true): got %d, want 1", len(activeOnly))
	}
	if activeOnly[0].TenantID != active.TenantID {
		t.Errorf("active-only filter picked wrong record")
	}
}

func TestInMemoryRegistry_Concurrent(t *testing.T) {
	r := NewInMemoryRegistry()
	ctx := context.Background()

	// Seed: 10 tenants.
	seeds := make([]uuid.UUID, 10)
	for i := range seeds {
		cfg := newTenantConfig(t)
		seeds[i] = cfg.TenantID
		if err := r.Register(ctx, cfg); err != nil {
			t.Fatalf("seed Register: %v", err)
		}
	}

	var wg sync.WaitGroup
	const goroutines = 10
	const opsPerGoroutine = 100
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				idx := (g + i) % len(seeds)
				_, _ = r.Get(ctx, seeds[idx])
				_, _ = r.List(ctx, true)
			}
		}(g)
	}
	wg.Wait()

	// Verify all seeds survive.
	list, _ := r.List(ctx, true)
	if len(list) != len(seeds) {
		t.Errorf("after concurrent run: %d configs, want %d", len(list), len(seeds))
	}
}

func TestInMemoryRegistry_DeleteIdempotent(t *testing.T) {
	r := NewInMemoryRegistry()
	ctx := context.Background()
	if err := r.Delete(ctx, uuid.New()); err != nil {
		t.Errorf("Delete of unknown tenant should be nil, got %v", err)
	}
}

func TestSPRegistration_Validate(t *testing.T) {
	cases := []struct {
		name string
		sp   SPRegistration
		ok   bool
	}{
		{"valid", SPRegistration{EntityID: "x", ACSURL: "y"}, true},
		{"missing entity", SPRegistration{ACSURL: "y"}, false},
		{"missing acs", SPRegistration{EntityID: "x"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.sp.Validate()
			if tc.ok && err != nil {
				t.Errorf("expected nil, got %v", err)
			}
			if !tc.ok && !errors.Is(err, ErrInvalidConfig) {
				t.Errorf("expected ErrInvalidConfig, got %v", err)
			}
		})
	}
}
