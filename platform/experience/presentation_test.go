// presentation_test.go -- TRD 140-06, must-haves #5 + #6.
//
// ThemeSpec (preset/logo/colors/density), TermSet (presentation-only term
// overrides, e.g. Job->Visit), and LocaleSpec{locale, currency, timezone} are
// the typed presentation surface. timezone is the net-new LOAD-BEARING field
// (absent everywhere today, required for scheduling/field) -- these tests assert
// it is a real round-tripping field, not silently dropped.
//
// OfflineSpec is PER-SURFACE and STRUCTURED: {policy, cache_ttl, conflict_policy,
// read_only_grace} -- NOT a bare offlineCapable bool. The surface_offline map
// keys an OfflineSpec by surface_id, so two surfaces can carry DIFFERENT offline
// policies on ONE spec.
//
// ValidatePresentation machine-checks two coherence rules (typed, accumulated,
// never-panics CoherenceError -- mirroring navgraph.ValidateNavGraph):
//   - a surface declaring offline scheduling needs LocaleSpec.timezone (the tz
//     is load-bearing for scheduling; an offline scheduling surface without a tz
//     cannot resolve local appointment times).
//   - an OfflineSpec with a conflict_policy set but policy=NONE is incoherent
//     (you can't have a conflict strategy when nothing is queued offline).
//
// Wrong-tenant: a spec carrying theme/terms/locale/offline, run through
// fixtures.WrongTenant, keeps tenant_id/org_id intact (diverged) while every
// presentation value survives untouched -- no brand/term bleed across tenants.
//
// Fixtures only: NewSpec + WithTheme/WithTermSet/WithLocale/WithSurfaceOffline +
// NewOfflineSpec + WrongTenant. No hand-built literals.
package experience_test

import (
	"testing"

	experiencev1 "github.com/aocybersystems/eden-platform-go/gen/go/experience/v1"
	"github.com/aocybersystems/eden-platform-go/platform/experience"
	"github.com/aocybersystems/eden-platform-go/platform/experience/fixtures"
	"google.golang.org/protobuf/proto"
)

// roundTrip marshals then unmarshals a spec, failing the test on any wire error.
func roundTrip(t *testing.T, spec *experiencev1.ExperienceSpec) *experiencev1.ExperienceSpec {
	t.Helper()
	wire, err := proto.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got experiencev1.ExperienceSpec
	if err := proto.Unmarshal(wire, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return &got
}

// --- Happy: ThemeSpec ------------------------------------------------------

// ThemeSpec round-trips brand preset + logo ref + color-override map + density.
func TestThemeSpec_RoundTrips(t *testing.T) {
	spec := fixtures.NewSpec(
		fixtures.WithTheme(
			"eden-light",
			"asset://logo/acme.png",
			"comfortable",
			map[string]string{"primary": "#0A84FF", "surface": "#FFFFFF"},
		),
	)

	got := roundTrip(t, spec)
	th := got.GetTheme()
	if th == nil {
		t.Fatal("theme dropped on round-trip")
	}
	if th.GetBrandPreset() != "eden-light" {
		t.Errorf("brand_preset = %q, want eden-light", th.GetBrandPreset())
	}
	if th.GetLogoRef() != "asset://logo/acme.png" {
		t.Errorf("logo_ref = %q", th.GetLogoRef())
	}
	if th.GetDensity() != "comfortable" {
		t.Errorf("density = %q, want comfortable", th.GetDensity())
	}
	if th.GetColorOverrides()["primary"] != "#0A84FF" {
		t.Errorf("color_overrides[primary] = %q", th.GetColorOverrides()["primary"])
	}
	if th.GetColorOverrides()["surface"] != "#FFFFFF" {
		t.Errorf("color_overrides[surface] = %q", th.GetColorOverrides()["surface"])
	}
}

// --- Happy: TermSet --------------------------------------------------------

// TermSet round-trips a term-override map (the Job->Visit relabel). This is
// PRESENTATION-only: the key "job" is the logical entity, the value "visit" is
// the displayed label -- logic keys off "job", never "visit".
func TestTermSet_RoundTrips(t *testing.T) {
	spec := fixtures.NewSpec(
		fixtures.WithTermSet(map[string]string{"job": "visit", "customer": "client"}),
	)

	got := roundTrip(t, spec)
	ts := got.GetTerms()
	if ts == nil {
		t.Fatal("terms dropped on round-trip")
	}
	if ts.GetOverrides()["job"] != "visit" {
		t.Errorf("terms[job] = %q, want visit", ts.GetOverrides()["job"])
	}
	if ts.GetOverrides()["customer"] != "client" {
		t.Errorf("terms[customer] = %q, want client", ts.GetOverrides()["customer"])
	}
}

// --- Happy: LocaleSpec (timezone is the load-bearing net-new field) --------

// LocaleSpec round-trips locale + currency + IANA timezone. The timezone
// assertion is the POINT of this TRD: tz is absent everywhere today and MUST be
// a real, non-dropped field here (load-bearing for scheduling/field).
func TestLocaleSpec_RoundTrips_TimezoneIsRealField(t *testing.T) {
	spec := fixtures.NewSpec(
		fixtures.WithLocale("en-GB", "GBP", "Europe/London"),
	)

	got := roundTrip(t, spec)
	loc := got.GetLocale()
	if loc == nil {
		t.Fatal("locale dropped on round-trip")
	}
	if loc.GetLocale() != "en-GB" {
		t.Errorf("locale = %q, want en-GB", loc.GetLocale())
	}
	// Currency is NOT hardcoded to USD -- it round-trips the supplied value.
	if loc.GetCurrency() != "GBP" {
		t.Errorf("currency = %q, want GBP (must not be hardcoded USD)", loc.GetCurrency())
	}
	// THE load-bearing assertion: timezone is a real field, survives the wire.
	if loc.GetTimezone() != "Europe/London" {
		t.Fatalf("timezone = %q, want Europe/London -- timezone MUST round-trip (load-bearing, not dropped)", loc.GetTimezone())
	}
}

// The fixture LocaleSpec default carries a sane non-empty IANA timezone -- a
// locale built from defaults is never tz-less.
func TestLocaleSpec_DefaultTimezoneIsSane(t *testing.T) {
	spec := fixtures.NewSpec(
		fixtures.WithLocale(fixtures.DefaultLocale, fixtures.DefaultCurrency, fixtures.DefaultTimezone),
	)
	if got := spec.GetLocale().GetTimezone(); got == "" {
		t.Fatal("default timezone is empty; must default to a real IANA tz")
	}
	if fixtures.DefaultTimezone == "" {
		t.Fatal("fixtures.DefaultTimezone must be a non-empty IANA tz")
	}
}

// --- Happy: per-surface OfflineSpec ----------------------------------------

// A per-surface OfflineSpec round-trips all four typed fields: policy +
// cache_ttl + conflict_policy + read_only_grace. NOT a bare bool.
func TestOfflineSpec_RoundTrips_AllFourFields(t *testing.T) {
	spec := fixtures.NewSpec(
		fixtures.WithSurfaceOffline("scheduling", fixtures.NewOfflineSpec(
			experiencev1.OfflinePolicy_OFFLINE_POLICY_READ_WRITE_QUEUE,
			3600,
			experiencev1.ConflictPolicy_CONFLICT_POLICY_LAST_WRITE_WINS,
			900,
		)),
	)

	got := roundTrip(t, spec)
	off := got.GetSurfaceOffline()["scheduling"]
	if off == nil {
		t.Fatal("surface_offline[scheduling] dropped on round-trip")
	}
	if off.GetPolicy() != experiencev1.OfflinePolicy_OFFLINE_POLICY_READ_WRITE_QUEUE {
		t.Errorf("policy = %v", off.GetPolicy())
	}
	if off.GetCacheTtlSeconds() != 3600 {
		t.Errorf("cache_ttl_seconds = %d, want 3600", off.GetCacheTtlSeconds())
	}
	if off.GetConflictPolicy() != experiencev1.ConflictPolicy_CONFLICT_POLICY_LAST_WRITE_WINS {
		t.Errorf("conflict_policy = %v", off.GetConflictPolicy())
	}
	if off.GetReadOnlyGraceSeconds() != 900 {
		t.Errorf("read_only_grace_seconds = %d, want 900", off.GetReadOnlyGraceSeconds())
	}
}

// --- Edge: two surfaces, DIFFERENT offline policies (per-surface, not global) -

// Two surfaces with DIFFERENT OfflineSpecs both round-trip independently. This
// is the per-surface proof: offline is keyed by surface_id, not one global bool.
func TestOfflineSpec_PerSurface_DistinctPolicies(t *testing.T) {
	spec := fixtures.NewSpec(
		fixtures.WithSurfaceOffline("scheduling", fixtures.NewOfflineSpec(
			experiencev1.OfflinePolicy_OFFLINE_POLICY_READ_WRITE_QUEUE,
			3600,
			experiencev1.ConflictPolicy_CONFLICT_POLICY_MANUAL_RECONCILE,
			900,
		)),
		fixtures.WithSurfaceOffline("reports", fixtures.NewOfflineSpec(
			experiencev1.OfflinePolicy_OFFLINE_POLICY_READ_CACHE,
			120,
			experiencev1.ConflictPolicy_CONFLICT_POLICY_SERVER_WINS,
			0,
		)),
	)

	got := roundTrip(t, spec)
	m := got.GetSurfaceOffline()
	if len(m) != 2 {
		t.Fatalf("surface_offline has %d entries, want 2", len(m))
	}
	if m["scheduling"].GetPolicy() != experiencev1.OfflinePolicy_OFFLINE_POLICY_READ_WRITE_QUEUE {
		t.Errorf("scheduling policy = %v, want READ_WRITE_QUEUE", m["scheduling"].GetPolicy())
	}
	if m["reports"].GetPolicy() != experiencev1.OfflinePolicy_OFFLINE_POLICY_READ_CACHE {
		t.Errorf("reports policy = %v, want READ_CACHE", m["reports"].GetPolicy())
	}
	// Distinct policies prove the map is genuinely per-surface, not collapsed.
	if m["scheduling"].GetPolicy() == m["reports"].GetPolicy() {
		t.Fatal("two surfaces collapsed to the same policy; offline is not per-surface")
	}
}

// --- Edge: validation -- timezone required for offline scheduling surface ----

// A surface declaring offline scheduling (READ_WRITE_QUEUE) with NO LocaleSpec
// timezone is an incoherent presentation: ValidatePresentation must flag a
// CoherenceMissingTimezone. tz is load-bearing -- an offline scheduling surface
// can't resolve local appointment times without it.
func TestValidatePresentation_OfflineSchedulingRequiresTimezone(t *testing.T) {
	spec := fixtures.NewSpec(
		// locale present but timezone empty -> the offending state.
		fixtures.WithLocale("en-US", "USD", ""),
		fixtures.WithSurfaceOffline("scheduling", fixtures.NewOfflineSpec(
			experiencev1.OfflinePolicy_OFFLINE_POLICY_READ_WRITE_QUEUE,
			3600,
			experiencev1.ConflictPolicy_CONFLICT_POLICY_LAST_WRITE_WINS,
			900,
		)),
	)

	errs := experience.ValidatePresentation(spec, experience.SchedulingSurfaces("scheduling"))
	if !hasCode(errs, experience.CoherenceMissingTimezone) {
		t.Fatalf("offline scheduling surface without timezone must flag %s; got %v", experience.CoherenceMissingTimezone, errs)
	}
}

// With a valid timezone present, the same offline scheduling surface validates
// clean (no missing-timezone error).
func TestValidatePresentation_TimezonePresent_NoTimezoneError(t *testing.T) {
	spec := fixtures.NewSpec(
		fixtures.WithLocale("en-US", "USD", "America/Chicago"),
		fixtures.WithSurfaceOffline("scheduling", fixtures.NewOfflineSpec(
			experiencev1.OfflinePolicy_OFFLINE_POLICY_READ_WRITE_QUEUE,
			3600,
			experiencev1.ConflictPolicy_CONFLICT_POLICY_LAST_WRITE_WINS,
			900,
		)),
	)

	errs := experience.ValidatePresentation(spec, experience.SchedulingSurfaces("scheduling"))
	if hasCode(errs, experience.CoherenceMissingTimezone) {
		t.Fatalf("timezone is present; must not flag missing-timezone; got %v", errs)
	}
}

// --- Edge: validation -- conflict_policy set but policy=NONE is incoherent ----

// An OfflineSpec whose policy is NONE but still carries a non-UNSPECIFIED
// conflict_policy is incoherent (no offline writes are queued, so a conflict
// strategy is meaningless). ValidatePresentation warns via CoherenceIncoherentOffline.
func TestValidatePresentation_ConflictWithPolicyNone_Warns(t *testing.T) {
	spec := fixtures.NewSpec(
		fixtures.WithLocale("en-US", "USD", "America/New_York"),
		fixtures.WithSurfaceOffline("reports", fixtures.NewOfflineSpec(
			experiencev1.OfflinePolicy_OFFLINE_POLICY_NONE,
			0,
			experiencev1.ConflictPolicy_CONFLICT_POLICY_LAST_WRITE_WINS, // incoherent with NONE
			0,
		)),
	)

	errs := experience.ValidatePresentation(spec, experience.SchedulingSurfaces())
	if !hasCode(errs, experience.CoherenceIncoherentOffline) {
		t.Fatalf("conflict_policy set with policy=NONE must warn %s; got %v", experience.CoherenceIncoherentOffline, errs)
	}
}

// A coherent OfflineSpec (READ_WRITE_QUEUE + a conflict policy) does NOT warn.
func TestValidatePresentation_CoherentOffline_NoWarn(t *testing.T) {
	spec := fixtures.NewSpec(
		fixtures.WithLocale("en-US", "USD", "America/New_York"),
		fixtures.WithSurfaceOffline("reports", fixtures.NewOfflineSpec(
			experiencev1.OfflinePolicy_OFFLINE_POLICY_READ_WRITE_QUEUE,
			120,
			experiencev1.ConflictPolicy_CONFLICT_POLICY_LAST_WRITE_WINS,
			0,
		)),
	)

	errs := experience.ValidatePresentation(spec, experience.SchedulingSurfaces())
	if hasCode(errs, experience.CoherenceIncoherentOffline) {
		t.Fatalf("coherent offline config must not warn; got %v", errs)
	}
}

// --- Failure / wrong-tenant ------------------------------------------------

// A spec carrying theme/terms/locale/offline, run through WrongTenant, diverges
// tenant_id/org_id but keeps EVERY presentation value intact -- no brand/term
// bleed. The wrong-tenant copy carries ONLY its own (diverged) scope alongside
// the unchanged presentation, proving resolution never mixes tenant B's brand
// into tenant A's spec.
func TestPresentation_WrongTenant_NoBrandOrTermBleed(t *testing.T) {
	baseline := fixtures.NewSpec(
		fixtures.WithTheme("acme-dark", "asset://logo/acme.png", "compact",
			map[string]string{"primary": "#111111"}),
		fixtures.WithTermSet(map[string]string{"job": "visit"}),
		fixtures.WithLocale("en-US", "USD", "America/New_York"),
		fixtures.WithSurfaceOffline("scheduling", fixtures.NewOfflineSpec(
			experiencev1.OfflinePolicy_OFFLINE_POLICY_READ_WRITE_QUEUE,
			3600,
			experiencev1.ConflictPolicy_CONFLICT_POLICY_LAST_WRITE_WINS,
			900,
		)),
	)

	wrong := fixtures.WrongTenant(baseline)

	// Scope diverged.
	if wrong.GetTenantId() == baseline.GetTenantId() {
		t.Fatal("WrongTenant did not diverge tenant_id")
	}
	if wrong.GetOrgId() == baseline.GetOrgId() {
		t.Fatal("WrongTenant did not diverge org_id")
	}

	// Presentation values survived intact on the diverged copy (no bleed/loss).
	if wrong.GetTheme().GetBrandPreset() != "acme-dark" {
		t.Errorf("theme brand bled/lost on wrong-tenant copy: %q", wrong.GetTheme().GetBrandPreset())
	}
	if wrong.GetTerms().GetOverrides()["job"] != "visit" {
		t.Errorf("term override bled/lost on wrong-tenant copy")
	}
	if wrong.GetLocale().GetTimezone() != "America/New_York" {
		t.Errorf("locale timezone bled/lost on wrong-tenant copy: %q", wrong.GetLocale().GetTimezone())
	}
	if wrong.GetSurfaceOffline()["scheduling"].GetPolicy() != experiencev1.OfflinePolicy_OFFLINE_POLICY_READ_WRITE_QUEUE {
		t.Errorf("offline policy bled/lost on wrong-tenant copy")
	}

	// Mutating the baseline after the copy must not bleed into the wrong copy.
	baseline.GetTheme().BrandPreset = "MUTATED"
	if wrong.GetTheme().GetBrandPreset() == "MUTATED" {
		t.Fatal("WrongTenant aliased the theme; baseline mutation bled into the copy")
	}
}

// hasCode reports whether any CoherenceError carries the given code.
func hasCode(errs []experience.CoherenceError, code experience.CoherenceCode) bool {
	for _, e := range errs {
		if e.Code == code {
			return true
		}
	}
	return false
}
