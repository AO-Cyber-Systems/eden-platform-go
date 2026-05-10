package federation

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// InMemoryRegistry is the reference Registry implementation. Safe for
// concurrent use. Always construct via NewInMemoryRegistry — the zero
// value's mutex is OK but the map will be nil.
type InMemoryRegistry struct {
	mu      sync.RWMutex
	configs map[uuid.UUID]*TenantIdPConfig
}

// NewInMemoryRegistry returns an empty in-memory Registry.
func NewInMemoryRegistry() *InMemoryRegistry {
	return &InMemoryRegistry{configs: make(map[uuid.UUID]*TenantIdPConfig)}
}

// Register inserts a new TenantIdPConfig. Returns ErrDuplicateTenant if
// the tenant ID is already present, ErrInvalidConfig if the supplied
// config fails Validate().
func (r *InMemoryRegistry) Register(_ context.Context, cfg TenantIdPConfig) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.configs[cfg.TenantID]; exists {
		return ErrDuplicateTenant
	}
	now := time.Now().UTC()
	if cfg.CreatedAt.IsZero() {
		cfg.CreatedAt = now
	}
	cfg.UpdatedAt = now
	if cfg.AllowedSPs == nil {
		cfg.AllowedSPs = make(map[string]SPRegistration)
	}
	if cfg.AttributeTemplate == nil {
		cfg.AttributeTemplate = make(map[string][]string)
	}
	r.configs[cfg.TenantID] = copyConfig(cfg)
	return nil
}

// Get returns the config for the given tenant. ErrTenantNotFound when
// the tenant is unknown.
func (r *InMemoryRegistry) Get(_ context.Context, tenantID uuid.UUID) (TenantIdPConfig, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cfg, ok := r.configs[tenantID]
	if !ok {
		return TenantIdPConfig{}, ErrTenantNotFound
	}
	return *copyConfig(*cfg), nil
}

// Update replaces an existing config in place. ErrTenantNotFound when
// the tenant doesn't exist; ErrInvalidConfig on validation failure.
// CreatedAt is preserved from the original record.
func (r *InMemoryRegistry) Update(_ context.Context, cfg TenantIdPConfig) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, ok := r.configs[cfg.TenantID]
	if !ok {
		return ErrTenantNotFound
	}
	cfg.CreatedAt = existing.CreatedAt
	cfg.UpdatedAt = time.Now().UTC()
	if cfg.AllowedSPs == nil {
		cfg.AllowedSPs = make(map[string]SPRegistration)
	}
	if cfg.AttributeTemplate == nil {
		cfg.AttributeTemplate = make(map[string][]string)
	}
	r.configs[cfg.TenantID] = copyConfig(cfg)
	return nil
}

// Delete removes a config. Returns nil if the tenant didn't exist
// (idempotent — matches platform/auth.AuthStore semantics).
func (r *InMemoryRegistry) Delete(_ context.Context, tenantID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.configs, tenantID)
	return nil
}

// List returns configs in stable order by TenantID. When activeOnly is
// true, configs with IsActive=false are excluded.
func (r *InMemoryRegistry) List(_ context.Context, activeOnly bool) ([]TenantIdPConfig, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]TenantIdPConfig, 0, len(r.configs))
	for _, cfg := range r.configs {
		if activeOnly && !cfg.IsActive {
			continue
		}
		out = append(out, *copyConfig(*cfg))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].TenantID.String() < out[j].TenantID.String()
	})
	return out, nil
}

// copyConfig returns a deep-ish copy: the top-level struct + the two
// maps. Map values (SPRegistration, []string) are themselves copied
// because the caller might mutate slices.
func copyConfig(cfg TenantIdPConfig) *TenantIdPConfig {
	cp := cfg
	if cfg.AllowedSPs != nil {
		cp.AllowedSPs = make(map[string]SPRegistration, len(cfg.AllowedSPs))
		for k, v := range cfg.AllowedSPs {
			cp.AllowedSPs[k] = v
		}
	}
	if cfg.AttributeTemplate != nil {
		cp.AttributeTemplate = make(map[string][]string, len(cfg.AttributeTemplate))
		for k, v := range cfg.AttributeTemplate {
			cloned := make([]string, len(v))
			copy(cloned, v)
			cp.AttributeTemplate[k] = cloned
		}
	}
	return &cp
}
