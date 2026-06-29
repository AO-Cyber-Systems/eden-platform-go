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
