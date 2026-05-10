package federation

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func newExternalIdPConfig(t *testing.T, provider string, tenantID uuid.UUID) TenantExternalIdP {
	t.Helper()
	cfg := TenantExternalIdP{
		ID:          uuid.New(),
		TenantID:    tenantID,
		Provider:    provider,
		DisplayName: "Acme " + provider,
		EntityID:    "https://idp.acme.com",
		MetadataURL: "https://idp.acme.com/metadata",
		ClientID:    "acme-client",
		ClientSecret: "shh",
		AttributeMapping: map[string]string{
			"email": "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress",
		},
		JITPolicy: JITPolicy{
			Enabled:        true,
			DefaultRole:    "member",
			AllowedDomains: []string{"acme.com"},
		},
		IsActive: true,
	}
	return cfg
}

func TestInMemorySPRegistry_CRUD(t *testing.T) {
	r := NewInMemorySPRegistry()
	ctx := context.Background()
	tenantID := uuid.New()

	cfg := newExternalIdPConfig(t, ProviderSAML, tenantID)
	if err := r.Register(ctx, cfg); err != nil {
		t.Fatalf("Register: %v", err)
	}

	got, err := r.Get(ctx, cfg.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.DisplayName != cfg.DisplayName {
		t.Errorf("DisplayName mismatch")
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Errorf("CreatedAt/UpdatedAt not stamped")
	}

	// Update
	cfg.DisplayName = "Acme Corp Okta (Prod)"
	if err := r.Update(ctx, cfg); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, _ = r.Get(ctx, cfg.ID)
	if got.DisplayName != "Acme Corp Okta (Prod)" {
		t.Errorf("update did not persist new DisplayName")
	}

	// List
	list, _ := r.ListByTenant(ctx, tenantID, true)
	if len(list) != 1 {
		t.Errorf("ListByTenant: got %d, want 1", len(list))
	}

	// Delete
	if err := r.Delete(ctx, cfg.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := r.Get(ctx, cfg.ID); !errors.Is(err, ErrExternalIdPNotFound) {
		t.Errorf("expected ErrExternalIdPNotFound, got %v", err)
	}
}

func TestInMemorySPRegistry_ListFiltering(t *testing.T) {
	r := NewInMemorySPRegistry()
	ctx := context.Background()
	tenantA := uuid.New()
	tenantB := uuid.New()

	cfgA := newExternalIdPConfig(t, ProviderSAML, tenantA)
	cfgB := newExternalIdPConfig(t, ProviderOIDC, tenantB)
	cfgInactive := newExternalIdPConfig(t, ProviderSAML, tenantA)
	cfgInactive.IsActive = false
	_ = r.Register(ctx, cfgA)
	_ = r.Register(ctx, cfgB)
	_ = r.Register(ctx, cfgInactive)

	listA, _ := r.ListByTenant(ctx, tenantA, false)
	if len(listA) != 2 {
		t.Errorf("tenantA all: got %d, want 2", len(listA))
	}
	listAActive, _ := r.ListByTenant(ctx, tenantA, true)
	if len(listAActive) != 1 {
		t.Errorf("tenantA active: got %d, want 1", len(listAActive))
	}
	listB, _ := r.ListByTenant(ctx, tenantB, true)
	if len(listB) != 1 {
		t.Errorf("tenantB: got %d, want 1", len(listB))
	}
}

func TestInMemorySPRegistry_Validation(t *testing.T) {
	r := NewInMemorySPRegistry()
	ctx := context.Background()
	tenantID := uuid.New()

	cases := []struct {
		name string
		cfg  TenantExternalIdP
	}{
		{"missing id", TenantExternalIdP{TenantID: tenantID, Provider: ProviderSAML, EntityID: "x", MetadataURL: "y"}},
		{"missing tenant", TenantExternalIdP{ID: uuid.New(), Provider: ProviderSAML, EntityID: "x", MetadataURL: "y"}},
		{"unknown provider", TenantExternalIdP{ID: uuid.New(), TenantID: tenantID, Provider: "foo", EntityID: "x"}},
		{"saml missing metadata", TenantExternalIdP{ID: uuid.New(), TenantID: tenantID, Provider: ProviderSAML, EntityID: "x"}},
		{"oidc missing client", TenantExternalIdP{ID: uuid.New(), TenantID: tenantID, Provider: ProviderOIDC, EntityID: "x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := r.Register(ctx, tc.cfg); !errors.Is(err, ErrInvalidConfig) {
				t.Errorf("expected ErrInvalidConfig, got %v", err)
			}
		})
	}
}

func TestInMemorySPRegistry_DuplicateID(t *testing.T) {
	r := NewInMemorySPRegistry()
	ctx := context.Background()
	cfg := newExternalIdPConfig(t, ProviderSAML, uuid.New())
	if err := r.Register(ctx, cfg); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := r.Register(ctx, cfg); !errors.Is(err, ErrDuplicateExternalIdP) {
		t.Errorf("expected ErrDuplicateExternalIdP, got %v", err)
	}
}

func TestInMemorySPRegistry_UpdateUnknown(t *testing.T) {
	r := NewInMemorySPRegistry()
	ctx := context.Background()
	cfg := newExternalIdPConfig(t, ProviderSAML, uuid.New())
	if err := r.Update(ctx, cfg); !errors.Is(err, ErrExternalIdPNotFound) {
		t.Errorf("expected ErrExternalIdPNotFound, got %v", err)
	}
}
