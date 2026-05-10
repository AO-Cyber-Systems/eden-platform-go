package federation

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// SPRegistry persists TenantExternalIdP records. The Phase A in-memory
// implementation is the reference; pgstore-backed implementations
// satisfy the same interface in a follow-on.
type SPRegistry interface {
	Register(ctx context.Context, cfg TenantExternalIdP) error
	Get(ctx context.Context, id uuid.UUID) (TenantExternalIdP, error)
	Update(ctx context.Context, cfg TenantExternalIdP) error
	Delete(ctx context.Context, id uuid.UUID) error
	ListByTenant(ctx context.Context, tenantID uuid.UUID, activeOnly bool) ([]TenantExternalIdP, error)
}

// InMemorySPRegistry is the reference SPRegistry implementation. Safe
// for concurrent use. Always construct via NewInMemorySPRegistry.
type InMemorySPRegistry struct {
	mu      sync.RWMutex
	configs map[uuid.UUID]*TenantExternalIdP
}

// NewInMemorySPRegistry returns an empty in-memory SPRegistry.
func NewInMemorySPRegistry() *InMemorySPRegistry {
	return &InMemorySPRegistry{configs: make(map[uuid.UUID]*TenantExternalIdP)}
}

// Register inserts a new TenantExternalIdP. Returns
// ErrDuplicateExternalIdP if the ID is already present;
// ErrInvalidConfig on validation failure.
func (r *InMemorySPRegistry) Register(_ context.Context, cfg TenantExternalIdP) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.configs[cfg.ID]; exists {
		return ErrDuplicateExternalIdP
	}
	now := time.Now().UTC()
	if cfg.CreatedAt.IsZero() {
		cfg.CreatedAt = now
	}
	cfg.UpdatedAt = now
	if cfg.AttributeMapping == nil {
		cfg.AttributeMapping = make(map[string]string)
	}
	r.configs[cfg.ID] = copyExternalIdP(cfg)
	return nil
}

// Get returns the entry by its ID. ErrExternalIdPNotFound when unknown.
func (r *InMemorySPRegistry) Get(_ context.Context, id uuid.UUID) (TenantExternalIdP, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cfg, ok := r.configs[id]
	if !ok {
		return TenantExternalIdP{}, ErrExternalIdPNotFound
	}
	return *copyExternalIdP(*cfg), nil
}

// Update replaces an existing entry. ErrExternalIdPNotFound when
// unknown; ErrInvalidConfig on validation failure.
func (r *InMemorySPRegistry) Update(_ context.Context, cfg TenantExternalIdP) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, ok := r.configs[cfg.ID]
	if !ok {
		return ErrExternalIdPNotFound
	}
	cfg.CreatedAt = existing.CreatedAt
	cfg.UpdatedAt = time.Now().UTC()
	if cfg.AttributeMapping == nil {
		cfg.AttributeMapping = make(map[string]string)
	}
	r.configs[cfg.ID] = copyExternalIdP(cfg)
	return nil
}

// Delete removes the entry. Returns nil for unknown IDs (idempotent).
func (r *InMemorySPRegistry) Delete(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.configs, id)
	return nil
}

// ListByTenant returns entries filtered by tenantID + activeOnly. The
// result is stable-sorted by ID.
func (r *InMemorySPRegistry) ListByTenant(_ context.Context, tenantID uuid.UUID, activeOnly bool) ([]TenantExternalIdP, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]TenantExternalIdP, 0)
	for _, cfg := range r.configs {
		if cfg.TenantID != tenantID {
			continue
		}
		if activeOnly && !cfg.IsActive {
			continue
		}
		out = append(out, *copyExternalIdP(*cfg))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID.String() < out[j].ID.String()
	})
	return out, nil
}

func copyExternalIdP(cfg TenantExternalIdP) *TenantExternalIdP {
	cp := cfg
	if cfg.AttributeMapping != nil {
		cp.AttributeMapping = make(map[string]string, len(cfg.AttributeMapping))
		for k, v := range cfg.AttributeMapping {
			cp.AttributeMapping[k] = v
		}
	}
	if cfg.JITPolicy.AllowedDomains != nil {
		cp.JITPolicy.AllowedDomains = make([]string, len(cfg.JITPolicy.AllowedDomains))
		copy(cp.JITPolicy.AllowedDomains, cfg.JITPolicy.AllowedDomains)
	}
	return &cp
}
