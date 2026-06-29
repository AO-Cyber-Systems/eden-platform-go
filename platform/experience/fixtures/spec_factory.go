// Package fixtures provides hand-built factory builders for the
// experience.v1 contract messages, plus a cassette scaffold for any
// external API the contract references.
//
// Playbook habit #4: fixture generators, not LLM-generated test data. This
// factory is the SHARED SUBSTRATE every downstream isolation / coherence /
// version test draws from — including the one-call WrongTenant variant that
// every cross-tenant test uses to construct a divergent-scope request.
//
// SCOPE DISCIPLINE (Wave 1): only the ExperienceSpec ENVELOPE fields exist in
// the generated contract today (TRD 140-01). Keep these builders MINIMAL —
// each downstream TRD's Task 1 GROWS the factory per-message-group as those
// messages land (FeatureSurface, ToolDefinition, etc.). Do NOT speculatively
// add options for fields that do not yet exist on the generated types.
package fixtures

import (
	experiencev1 "github.com/aocybersystems/eden-platform-go/gen/go/experience/v1"
	"google.golang.org/protobuf/proto"
)

// Default scope + version values for a freshly built spec. Kept as named
// constants so downstream tests can reference the baseline without
// re-deriving it, and so WrongTenant has a guaranteed-different alternate
// scope to diverge into.
const (
	DefaultTenantID = "tenant-fixture-0001"
	DefaultOrgID    = "org-fixture-0001"

	// Alternate scope used by WrongTenant. MUST differ from the defaults so a
	// baseline built with NewSpec() (which uses the defaults) still diverges.
	WrongTenantID = "tenant-fixture-9999"
	WrongOrgID    = "org-fixture-9999"

	defaultSpecSchemaVersion      = "1.0.0"
	defaultSurfaceContractVersion = "1.0.0"
	defaultContractVersion        = "experience.v1"
	defaultMinBinaryVersion       = "1.0.0"
	defaultContentHash            = "sha256:fixture-default"
)

// SpecOpt mutates an ExperienceSpec in place. Compose them in NewSpec.
type SpecOpt func(*experiencev1.ExperienceSpec)

// --- 140-03 version-negotiation factory options ---------------------------
//
// These GROW the factory for the SurfaceRegistryManifest + UnknownSurfacePolicy
// + version-negotiation surface this TRD adds. Per the scope-discipline note
// above, they reference only fields that the generated contract carries once
// 140-03's proto lands (UnknownSurfacePolicy enum, referenced_surface_ids,
// unknown_surface_policy on ExperienceSpec).

// WithSurfaceContractVersion overrides the surface_contract_version axis.
func WithSurfaceContractVersion(v string) SpecOpt {
	return func(s *experiencev1.ExperienceSpec) { s.SurfaceContractVersion = v }
}

// WithMinBinaryVersion overrides the min_binary_version floor the spec demands.
func WithMinBinaryVersion(v string) SpecOpt {
	return func(s *experiencev1.ExperienceSpec) { s.MinBinaryVersion = v }
}

// WithReferencedSurfaces sets the surface ids the spec references — these are
// negotiated against the binary's compiled-in SurfaceRegistryManifest.
func WithReferencedSurfaces(ids ...string) SpecOpt {
	return func(s *experiencev1.ExperienceSpec) { s.ReferencedSurfaceIds = ids }
}

// WithUnknownSurfacePolicy sets the policy a binary applies when the spec
// references a surface it does not know about.
func WithUnknownSurfacePolicy(p experiencev1.UnknownSurfacePolicy) SpecOpt {
	return func(s *experiencev1.ExperienceSpec) { s.UnknownSurfacePolicy = p }
}

// NewManifest builds a SurfaceRegistryManifest a binary compiles in: the
// contract version it speaks + the surface ids it knows how to render.
func NewManifest(contractVersion string, knownSurfaceIDs ...string) *experiencev1.SurfaceRegistryManifest {
	return &experiencev1.SurfaceRegistryManifest{
		ContractVersion: contractVersion,
		KnownSurfaceIds: knownSurfaceIDs,
	}
}

// --- 140-04 ServiceTransportBinding + AOID-scope factory options ----------
//
// These GROW the factory for the ServiceTransportBinding message group (proto
// field 50 on ExperienceSpec, un-reserved from the 50-59 binding range). A
// binding is transport- AND scope-agnostic: the SAME spec can carry a
// CONNECT/COMPANY binding (eden-biz) AND a REST_OPENAPI/ORG binding (aocore).
//
// AOID NOTE: scope_authority is the proto-level mapping of ONE AOID identity to
// each backend's tenant scope (COMPANY=biz, ORG=aocore). The projection itself
// is runtime (binding.ResolveScope), not a proto field -- there is NO
// device-side multi-credential store.

// AllOperations is the full read+write operation set (get/list/create/update/
// delete) a binding can declare -- proves writes are first-class, not read-only.
func AllOperations() []experiencev1.Operation {
	return []experiencev1.Operation{
		experiencev1.Operation_OPERATION_GET,
		experiencev1.Operation_OPERATION_LIST,
		experiencev1.Operation_OPERATION_CREATE,
		experiencev1.Operation_OPERATION_UPDATE,
		experiencev1.Operation_OPERATION_DELETE,
	}
}

// NewBinding builds a ServiceTransportBinding with the given transport + scope +
// operations. service_package/service_name/repo_interface_id are derived from
// the entity so a fixture is self-consistent without per-call boilerplate.
func NewBinding(
	entity string,
	transport experiencev1.TransportKind,
	scope experiencev1.ScopeAuthority,
	pagination experiencev1.PaginationKind,
	operations ...experiencev1.Operation,
) *experiencev1.ServiceTransportBinding {
	return &experiencev1.ServiceTransportBinding{
		Entity:          entity,
		ServicePackage:  "experience.v1." + entity,
		ServiceName:     entity + "Service",
		Operations:      operations,
		TransportKind:   transport,
		ScopeAuthority:  scope,
		Pagination:      pagination,
		RepoInterfaceId: entity + "Repository",
	}
}

// WithBinding appends a ServiceTransportBinding to the spec. Compose it twice to
// build the two-transport proof (a CONNECT/COMPANY and a REST_OPENAPI/ORG
// binding on ONE spec).
func WithBinding(b *experiencev1.ServiceTransportBinding) SpecOpt {
	return func(s *experiencev1.ExperienceSpec) {
		s.Bindings = append(s.Bindings, b)
	}
}

// --- 140-05 NavGraph + DeepLinkSpec factory options -----------------------
//
// These GROW the factory for the NavGraph message group (proto field 20 on
// ExperienceSpec, un-reserved from the 20-29 nav range) and DeepLinkSpec (proto
// field 10 on AppDefinition, un-reserved from the 10-19 nav range).
//
// NavGraph is a GRAPH, not a flat surface list: a landing surface + typed
// NavSlots (surface + placement + order) + typed NavEdges that carry
// param_bindings (the customer->invoice->payment SELECTION-passing proof). The
// <=5 PRIMARY rule is a VALIDATOR concern (ValidateNavGraph), NOT a data
// constraint -- the factory must be able to express >5 PRIMARY slots so the
// validator has something to reject.

// NewNavSlot builds a single NavSlot: a surface placed at a placement + order.
func NewNavSlot(surfaceID string, placement experiencev1.Placement, order int32) *experiencev1.NavSlot {
	return &experiencev1.NavSlot{
		SurfaceId: surfaceID,
		Placement: placement,
		Order:     order,
	}
}

// NewNavEdge builds a typed inter-surface edge. param_bindings carries the
// selection passed across the hop (e.g. {"customerId": "$selection.id"} from a
// customer surface into an invoice surface) -- this is the flow-passing proof
// that the graph composes flows, not a launcher.
func NewNavEdge(from, to string, paramBindings map[string]string, trigger string) *experiencev1.NavEdge {
	return &experiencev1.NavEdge{
		FromSurfaceId: from,
		ToSurfaceId:   to,
		ParamBindings: paramBindings,
		Trigger:       trigger,
	}
}

// WithNavGraph sets the spec's NavGraph: a landing surface + its slots. Compose
// with WithNavEdge to add typed edges after the slots are placed.
func WithNavGraph(landingSurfaceID string, slots ...*experiencev1.NavSlot) SpecOpt {
	return func(s *experiencev1.ExperienceSpec) {
		if s.NavGraph == nil {
			s.NavGraph = &experiencev1.NavGraph{}
		}
		s.NavGraph.LandingSurfaceId = landingSurfaceID
		s.NavGraph.Slots = append(s.NavGraph.Slots, slots...)
	}
}

// WithNavEdge appends a typed edge to the spec's NavGraph. Compose it after
// WithNavGraph so the graph exists; calling it alone lazily creates the graph
// (so an edge-only test can still build a graph with no landing/slots, e.g. to
// prove an edge whose `to` is not in any slot fails coherence).
func WithNavEdge(edge *experiencev1.NavEdge) SpecOpt {
	return func(s *experiencev1.ExperienceSpec) {
		if s.NavGraph == nil {
			s.NavGraph = &experiencev1.NavGraph{}
		}
		s.NavGraph.Edges = append(s.NavGraph.Edges, edge)
	}
}

// NewDeepLink builds a DeepLinkSpec: the url scheme + route templates a store
// binary commits to (these can't change post-submit).
func NewDeepLink(urlScheme string, routeTemplates ...string) *experiencev1.DeepLinkSpec {
	return &experiencev1.DeepLinkSpec{
		UrlScheme:      urlScheme,
		RouteTemplates: routeTemplates,
	}
}

// --- 140-06 ThemeSpec + TermSet + LocaleSpec + per-surface OfflineSpec ----
//
// These GROW the factory for must-haves #5 + #6: the presentation/runtime-policy
// message group. ThemeSpec/TermSet/LocaleSpec land on ExperienceSpec fields
// 30/31/32 (un-reserved from the 30-39 presentation range); the per-surface
// OfflineSpec map lands on field 40 (un-reserved from the 40-49 offline range),
// keyed by surface_id so offline policy is PER-SURFACE, never one global bool.
//
// LocaleSpec.timezone is the net-new load-bearing field (absent everywhere
// today, required for scheduling/field surfaces) -- modeled as an IANA tz string.
// TermSet is PRESENTATION-only (Job->Visit relabeling); it MUST NOT be load-
// bearing for any logic -- the offering keys off surface/entity ids, never the
// displayed term.

// DefaultLocale / DefaultCurrency / DefaultTimezone are the sane LocaleSpec
// defaults a freshly built locale carries. Currency is NOT hardcoded to USD at
// the contract level -- it is a LocaleSpec field; these are merely the fixture
// baseline so a locale-less spec still has a coherent default to diverge from.
const (
	DefaultLocale   = "en-US"
	DefaultCurrency = "USD"
	DefaultTimezone = "America/New_York"
)

// WithTheme sets the spec's ThemeSpec: a brand preset + logo ref + color
// override map + density token. color_overrides is a map so a builder can
// override an arbitrary set of brand tokens without a fixed schema.
func WithTheme(brandPreset, logoRef, density string, colorOverrides map[string]string) SpecOpt {
	return func(s *experiencev1.ExperienceSpec) {
		s.Theme = &experiencev1.ThemeSpec{
			BrandPreset:    brandPreset,
			LogoRef:        logoRef,
			ColorOverrides: colorOverrides,
			Density:        density,
		}
	}
}

// WithTermSet sets the spec's TermSet: a presentation-only term-override map
// (e.g. {"job": "visit"}). PRESENTATION-only -- never load-bearing for logic.
func WithTermSet(overrides map[string]string) SpecOpt {
	return func(s *experiencev1.ExperienceSpec) {
		s.Terms = &experiencev1.TermSet{Overrides: overrides}
	}
}

// WithLocale sets the spec's LocaleSpec: locale + currency + IANA timezone.
// timezone is load-bearing for scheduling/field surfaces and is a first-class
// typed field here (it is absent everywhere else today).
func WithLocale(locale, currency, timezone string) SpecOpt {
	return func(s *experiencev1.ExperienceSpec) {
		s.Locale = &experiencev1.LocaleSpec{
			Locale:   locale,
			Currency: currency,
			Timezone: timezone,
		}
	}
}

// NewOfflineSpec builds a single per-surface OfflineSpec: the offline policy +
// cache TTL + conflict policy + read-only grace window. NOT a bare bool -- the
// structure is what lets gates G1/G5 reason about conflict + grace later.
func NewOfflineSpec(
	policy experiencev1.OfflinePolicy,
	cacheTTLSeconds uint32,
	conflict experiencev1.ConflictPolicy,
	readOnlyGraceSeconds uint32,
) *experiencev1.OfflineSpec {
	return &experiencev1.OfflineSpec{
		Policy:               policy,
		CacheTtlSeconds:      cacheTTLSeconds,
		ConflictPolicy:       conflict,
		ReadOnlyGraceSeconds: readOnlyGraceSeconds,
	}
}

// WithSurfaceOffline attaches an OfflineSpec to ONE surface in the spec's
// surface_offline map (keyed by surface_id). Compose it twice with DIFFERENT
// surface ids + policies to prove offline is per-surface, not one global bool.
func WithSurfaceOffline(surfaceID string, offline *experiencev1.OfflineSpec) SpecOpt {
	return func(s *experiencev1.ExperienceSpec) {
		if s.SurfaceOffline == nil {
			s.SurfaceOffline = make(map[string]*experiencev1.OfflineSpec)
		}
		s.SurfaceOffline[surfaceID] = offline
	}
}

// NewSpec returns a valid, marshalable ExperienceSpec with sane defaults,
// then applies the supplied options in order. Each call returns a fresh,
// independent (non-aliased) struct.
func NewSpec(opts ...SpecOpt) *experiencev1.ExperienceSpec {
	spec := &experiencev1.ExperienceSpec{
		SpecSchemaVersion:      defaultSpecSchemaVersion,
		SurfaceContractVersion: defaultSurfaceContractVersion,
		ContentHash:            defaultContentHash,
		ContractVersion:        defaultContractVersion,
		MinBinaryVersion:       defaultMinBinaryVersion,
		TenantId:               DefaultTenantID,
		OrgId:                  DefaultOrgID,
	}
	for _, opt := range opts {
		opt(spec)
	}
	return spec
}

// WithTenant overrides only the tenant_id scope field.
func WithTenant(id string) SpecOpt {
	return func(s *experiencev1.ExperienceSpec) { s.TenantId = id }
}

// WithOrg overrides only the org_id scope field.
func WithOrg(id string) SpecOpt {
	return func(s *experiencev1.ExperienceSpec) { s.OrgId = id }
}

// WrongTenant returns a divergent-scope COPY of baseline whose tenant_id AND
// org_id differ from the baseline's, leaving every non-scope field intact.
//
// This is THE load-bearing helper: every downstream isolation test calls it
// once to construct a cross-tenant request from a known-good baseline. It
// deep-copies (proto.Clone) so mutating the baseline afterward never bleeds
// into the wrong-tenant copy, and it guarantees divergence even when baseline
// already happens to use the wrong-scope constants (it flips to a second
// alternate in that case rather than silently echoing).
func WrongTenant(baseline *experiencev1.ExperienceSpec) *experiencev1.ExperienceSpec {
	clone := proto.Clone(baseline).(*experiencev1.ExperienceSpec)

	clone.TenantId = WrongTenantID
	clone.OrgId = WrongOrgID

	// Guarantee strict divergence even if the baseline itself used the
	// wrong-scope constants — fall back to a distinct alternate.
	if clone.TenantId == baseline.GetTenantId() {
		clone.TenantId = WrongTenantID + "-alt"
	}
	if clone.OrgId == baseline.GetOrgId() {
		clone.OrgId = WrongOrgID + "-alt"
	}
	return clone
}
