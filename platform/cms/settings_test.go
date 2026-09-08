package cms

import (
	"context"
	"encoding/json"
	"reflect"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// TestDefaultSettings_TenantWithNoRow proves the required contract
// verbatim: a tenant that has never saved a settings row receives
// documented defaults from the store, not an error.
func TestDefaultSettings_TenantWithNoRow(t *testing.T) {
	store := NewMemorySettingsStore()
	companyID := uuid.New()

	got, err := store.GetSettings(context.Background(), companyID)
	if err != nil {
		t.Fatalf("GetSettings on a tenant with no row returned an error: %v", err)
	}

	want := DefaultSettings(companyID)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetSettings(unconfigured tenant) = %#v, want DefaultSettings(companyID) = %#v", got, want)
	}

	// The documented shape of the defaults, checked field by field so a
	// future change to DefaultSettings' contents is caught even if it
	// still happens to reflect.DeepEqual its own definition.
	if got.CompanyID != companyID {
		t.Errorf("CompanyID = %v, want %v", got.CompanyID, companyID)
	}
	if got.SiteName != "" || got.SiteDescription != "" || got.Tagline != "" {
		t.Errorf("expected empty identity strings by default, got %#v", got)
	}
	if got.Nav != nil {
		t.Errorf("Nav = %#v, want nil for a tenant with no navigation configured", got.Nav)
	}
	if got.Brand.Colors == nil || got.Brand.TypeRoles == nil || got.Brand.Spacing == nil {
		t.Errorf("Brand token maps must be non-nil empty maps, got %#v", got.Brand)
	}
	if len(got.Brand.Colors) != 0 || len(got.Brand.TypeRoles) != 0 || len(got.Brand.Spacing) != 0 {
		t.Errorf("Brand token maps must be empty by default, got %#v", got.Brand)
	}
}

// TestMemorySettingsStore_RoundTrip proves Settings round-trips through
// the store unchanged: what UpsertSettings stores is exactly what a
// subsequent GetSettings returns, across every field including nested
// navigation and brand-token maps.
func TestMemorySettingsStore_RoundTrip(t *testing.T) {
	store := NewMemorySettingsStore()
	companyID := uuid.New()
	parentID := uuid.New()
	childID := uuid.New()

	in := Settings{
		CompanyID:       companyID,
		SiteName:        "Acme Corp",
		SiteDescription: "Widgets for the modern age",
		ContactEmail:    "contact@acme.example",
		ContactPhone:    "+1-555-0100",
		Tagline:         "Widgets, delivered",
		FooterText:      "(c) Acme Corp",
		OGImageURL:      "https://cdn.acme.example/og.png",
		Social: SocialLinks{
			Twitter:   "https://twitter.com/acme",
			Facebook:  "https://facebook.com/acme",
			Instagram: "https://instagram.com/acme",
		},
		Nav: []NavItem{
			{ID: parentID, Label: "Products", URL: "/products", Position: 0, IsPublished: true},
			{ID: childID, ParentID: &parentID, Label: "Pricing", URL: "/products/pricing", Position: 0, IsPublished: true},
		},
		Brand: BrandTokens{
			Colors:    map[string]string{"primary": "#112233", "accent": "#AABBCC"},
			TypeRoles: map[string]string{"heading": "sans-serif-bold", "body": "sans-serif-regular"},
			Spacing:   map[string]string{"section": "64px", "gutter": "16px"},
			LogoURL:   "https://cdn.acme.example/logo.svg",
		},
	}

	stored, err := store.UpsertSettings(context.Background(), in)
	if err != nil {
		t.Fatalf("UpsertSettings: %v", err)
	}
	if stored.UpdatedAt.IsZero() {
		t.Error("UpsertSettings did not set UpdatedAt")
	}

	got, err := store.GetSettings(context.Background(), companyID)
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if !reflect.DeepEqual(got, stored) {
		t.Errorf("GetSettings after UpsertSettings = %#v, want %#v", got, stored)
	}

	// Field-level spot checks on the parts a renderer would actually walk,
	// proving a caller needs nothing beyond the exported Settings shape.
	if got.SiteName != "Acme Corp" {
		t.Errorf("SiteName = %q, want %q", got.SiteName, "Acme Corp")
	}
	if len(got.Nav) != 2 || got.Nav[1].ParentID == nil || *got.Nav[1].ParentID != parentID {
		t.Errorf("Nav hierarchy did not survive the round trip: %#v", got.Nav)
	}
	if got.Brand.Colors["primary"] != "#112233" {
		t.Errorf("Brand.Colors[primary] = %q, want %q", got.Brand.Colors["primary"], "#112233")
	}
}

// TestMemorySettingsStore_UpsertRequiresCompanyID proves an ownerless
// settings row is rejected rather than silently stored under a Nil key.
func TestMemorySettingsStore_UpsertRequiresCompanyID(t *testing.T) {
	store := NewMemorySettingsStore()
	_, err := store.UpsertSettings(context.Background(), Settings{SiteName: "no tenant"})
	if err == nil {
		t.Fatal("UpsertSettings with a zero CompanyID: expected an error, got nil")
	}
	if err != ErrSettingsCompanyRequired {
		t.Errorf("err = %v, want ErrSettingsCompanyRequired", err)
	}
}

// TestMemorySettingsStore_TenantIsolation proves one tenant's Upsert never
// becomes visible to a different tenant's Get -- consistent with every
// other tenant-keyed accessor in this package.
func TestMemorySettingsStore_TenantIsolation(t *testing.T) {
	store := NewMemorySettingsStore()
	tenantA := uuid.New()
	tenantB := uuid.New()

	_, err := store.UpsertSettings(context.Background(), Settings{CompanyID: tenantA, SiteName: "Tenant A"})
	if err != nil {
		t.Fatalf("UpsertSettings(tenantA): %v", err)
	}

	gotB, err := store.GetSettings(context.Background(), tenantB)
	if err != nil {
		t.Fatalf("GetSettings(tenantB): %v", err)
	}
	if gotB.SiteName != "" {
		t.Errorf("tenantB.SiteName = %q, want empty -- tenantA's row leaked across tenants", gotB.SiteName)
	}
	if !reflect.DeepEqual(gotB, DefaultSettings(tenantB)) {
		t.Errorf("tenantB settings = %#v, want DefaultSettings(tenantB)", gotB)
	}
}

// TestMemorySettingsStore_ConcurrentAccess exercises the store's mutex
// under -race: concurrent Upserts for distinct tenants, and concurrent
// Gets, must never race or corrupt state.
func TestMemorySettingsStore_ConcurrentAccess(t *testing.T) {
	store := NewMemorySettingsStore()
	const n = 20
	ids := make([]uuid.UUID, n)
	for i := range ids {
		ids[i] = uuid.New()
	}

	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(id uuid.UUID) {
			defer wg.Done()
			_, err := store.UpsertSettings(context.Background(), Settings{CompanyID: id, SiteName: id.String()})
			if err != nil {
				t.Errorf("UpsertSettings(%s): %v", id, err)
			}
		}(id)
	}
	wg.Wait()

	for _, id := range ids {
		wg.Add(1)
		go func(id uuid.UUID) {
			defer wg.Done()
			got, err := store.GetSettings(context.Background(), id)
			if err != nil {
				t.Errorf("GetSettings(%s): %v", id, err)
				return
			}
			if got.SiteName != id.String() {
				t.Errorf("GetSettings(%s).SiteName = %q, want %q", id, got.SiteName, id.String())
			}
		}(id)
	}
	wg.Wait()
}

// TestSettings_JSONRoundTrip proves a renderer can consume Settings
// through its exported JSON shape alone -- marshal then unmarshal
// reproduces the original value with no cms-internal knowledge required.
func TestSettings_JSONRoundTrip(t *testing.T) {
	parentID := uuid.New()
	in := Settings{
		CompanyID: uuid.New(),
		SiteName:  "Acme Corp",
		Tagline:   "Widgets, delivered",
		Social:    SocialLinks{Twitter: "https://twitter.com/acme"},
		Nav: []NavItem{
			{ID: uuid.New(), ParentID: &parentID, Label: "Pricing", URL: "/pricing", Position: 1},
		},
		Brand: BrandTokens{
			Colors: map[string]string{"primary": "#112233"},
		},
	}

	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var out Settings
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Errorf("JSON round trip changed the value:\nin  = %#v\nout = %#v", in, out)
	}
}

// TestValidateHexColor covers the accepted #rrggbb shape and common
// malformed inputs (short form, missing '#', trailing junk, CSS-style
// function syntax) that must all be rejected.
func TestValidateHexColor(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"valid lowercase", "#112233", false},
		{"valid uppercase", "#AABBCC", false},
		{"valid mixed case", "#aAbBcC", false},
		{"missing hash", "112233", true},
		{"short form", "#123", true},
		{"trailing junk", "#112233;color:red", true},
		{"empty", "", true},
		{"named color", "red", true},
		{"rgb function form", "rgb(1,2,3)", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateHexColor(c.value)
			if c.wantErr && err == nil {
				t.Errorf("ValidateHexColor(%q): expected an error, got nil", c.value)
			}
			if !c.wantErr && err != nil {
				t.Errorf("ValidateHexColor(%q): unexpected error: %v", c.value, err)
			}
		})
	}
}

// TestValidateLogoURL covers the accepted shapes (absolute http(s), rooted
// relative path, empty-to-clear) and rejected shapes (scheme-relative,
// non-http(s) scheme, embedded whitespace/angle-brackets).
func TestValidateLogoURL(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"empty clears the logo", "", false},
		{"absolute https", "https://cdn.acme.example/logo.svg", false},
		{"absolute http", "http://cdn.acme.example/logo.svg", false},
		{"rooted relative path", "/media/logo.svg", false},
		{"scheme-relative rejected", "//cdn.acme.example/logo.svg", true},
		{"javascript scheme rejected", "javascript:alert(1)", true},
		{"unrooted relative rejected", "media/logo.svg", true},
		{"embedded angle bracket rejected", "https://cdn.acme.example/<script>", true},
		{"embedded whitespace rejected", "https://cdn.acme.example/ logo.svg", true},
		{"host-less absolute rejected", "https:///logo.svg", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateLogoURL(c.value)
			if c.wantErr && err == nil {
				t.Errorf("ValidateLogoURL(%q): expected an error, got nil", c.value)
			}
			if !c.wantErr && err != nil {
				t.Errorf("ValidateLogoURL(%q): unexpected error: %v", c.value, err)
			}
		})
	}
}
