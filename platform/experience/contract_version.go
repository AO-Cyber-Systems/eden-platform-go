// contract_version.go -- TRD 140-12 (GREEN).
//
// THE CROSS-REPO VERSIONING POLICY (the in-process half; the wire half is the
// buf breaking-change CI gate in .github/workflows/experience-proto-breaking.yml).
//
// The experience.v1 contract fans out across FOUR repos:
//
//	eden-platform-go (proto, this repo)  --  the single source of truth
//	    -> eden-experience-api-dart       --  generated Dart consumer
//	        -> eden-platform-flutter      --  Flutter consumer
//	            -> eden-biz (replace-pin) --  product consumer
//	    -> aocore                         --  REST/ORG consumer
//
// The fan-out desynced once already (the 3 Flutter clients). M0 ships the
// bump/compat policy so the FROZEN contract stays frozen SAFELY:
//
//   - ContractVersion is the single source-of-truth version token. Every
//     fixture / spec / AppDefinition stamps THIS value; the drift guard
//     (contract_version_test.go) fails if a decoded spec disagrees.
//   - ClassifyChange compares two proto-message field descriptor sets and
//     classifies the delta as None / Minor (additive) / Breaking
//     (removal / retype / renumber). This is the SAME judgment buf breaking
//     makes on the wire -- modeled in-process so a Go caller (and a test) can
//     reason about a proposed change without shelling out to buf.
//   - BumpVersion applies the classification to a semver string: additive bumps
//     the MINOR; a BREAKING change is a BLOCKED bump (a frozen v1 contract never
//     silently majors -- the gate must reject it and force a deliberate v2).
//
// SEMVER BUMP POLICY (mirrored in docs/experience-contract-versioning.md):
//
//	additive (new field / message / rpc) -> MINOR bump          (compatible)
//	field removal / retype / renumber    -> BREAKING -> BLOCKED (forbidden on v1)
package experience

import (
	"fmt"
	"strconv"
	"strings"
)

// ContractVersion is the single source-of-truth experience.v1 contract version
// token. It is the value every ExperienceSpec.contract_version /
// AppDefinition.contract_version field carries, and the value the per-consumer
// compat tests (Go + Dart) assert a decoded spec agrees with. Bumping the WIRE
// contract (the proto shape) without acknowledging it here is exactly the
// desync M0 forbids -- the drift guard test couples the two.
//
// The token is the contract NAME ("experience.v1"), not a semver: v1 is frozen
// forever, and the orthogonal semver axes (spec_schema_version /
// surface_contract_version / min_binary_version) carry the fine-grained
// versioning. A genuine breaking change is a NEW contract (experience.v2), never
// a major bump of this token -- see BumpVersion's BREAKING handling.
const ContractVersion = "experience.v1"

// ChangeClass is the compatibility class of a proto-message delta, ordered by
// severity (None < Minor < Breaking).
type ChangeClass int

const (
	// ChangeNone means the two descriptor sets are identical -- no version bump.
	ChangeNone ChangeClass = iota
	// ChangeMinor means the new set is a strict ADDITIVE superset of the old
	// (every old field preserved by number+name+kind, plus new fields). This is
	// the only kind of change a frozen v1 contract permits -- a MINOR bump.
	ChangeMinor
	// ChangeBreaking means an old field was removed, retyped, or renumbered. On a
	// frozen v1 contract this is FORBIDDEN -- BumpVersion blocks it.
	ChangeBreaking
)

// String renders a ChangeClass for test output / logs.
func (c ChangeClass) String() string {
	switch c {
	case ChangeNone:
		return "None"
	case ChangeMinor:
		return "Minor"
	case ChangeBreaking:
		return "Breaking"
	default:
		return fmt.Sprintf("ChangeClass(%d)", int(c))
	}
}

// FieldDescriptor is a minimal, transport-neutral description of one proto field:
// its number, name, and wire kind (e.g. "string", "int32", "message:Foo"). It is
// the unit ClassifyChange compares. Modeling the proto delta over these three
// load-bearing attributes mirrors buf's FIELD breaking-change rules
// (NO_DELETE / SAME_TYPE / SAME_NUMBER) without coupling to buf internals.
type FieldDescriptor struct {
	Number int32
	Name   string
	Kind   string
}

// ClassifyChange compares an OLD field-descriptor set to a NEW one and returns
// the compatibility class. The rules mirror the buf breaking gate:
//
//   - a field present in OLD but absent in NEW (by number) -> Breaking (removal).
//   - a field present in BOTH by number whose name or kind changed -> Breaking
//     (retype / rename-at-number).
//   - a field whose NAME exists in OLD but moved to a different NUMBER in NEW
//     -> Breaking (renumber) -- caught because the old number goes missing.
//   - only-additive (new numbers, every old field byte-identical) -> Minor.
//   - identical sets -> None.
//
// Order-independent: both sets are indexed by field number before comparison.
func ClassifyChange(oldFields, newFields []FieldDescriptor) ChangeClass {
	oldByNum := indexByNumber(oldFields)
	newByNum := indexByNumber(newFields)

	breaking := false
	for num, of := range oldByNum {
		nf, ok := newByNum[num]
		if !ok {
			// The old field's number is gone -> removal or renumber. Both break.
			breaking = true
			continue
		}
		if nf.Name != of.Name || nf.Kind != of.Kind {
			// Same number, different name/kind -> retype / repurpose. Breaking.
			breaking = true
		}
	}
	if breaking {
		return ChangeBreaking
	}

	// No breakage. If NEW added any number the OLD set lacked, it is additive.
	for num := range newByNum {
		if _, ok := oldByNum[num]; !ok {
			return ChangeMinor
		}
	}
	return ChangeNone
}

// BumpVersion applies a ChangeClass to a dotted semver string per the frozen-v1
// bump policy:
//
//	ChangeNone     -> version unchanged.
//	ChangeMinor    -> minor incremented, patch reset to 0 (additive release).
//	ChangeBreaking -> ERROR. A frozen v1 contract never silently majors; a
//	                  genuine breaking change must become a NEW contract
//	                  (experience.v2) by deliberate human decision, never an
//	                  automatic bump. The buf gate hard-fails the same case.
//
// version must be a dotted MAJOR.MINOR.PATCH (missing trailing components
// default to 0). A malformed version returns an error rather than guessing.
func BumpVersion(version string, class ChangeClass) (string, error) {
	switch class {
	case ChangeBreaking:
		return "", fmt.Errorf(
			"contract: a BREAKING change to the frozen experience.v1 contract is forbidden; "+
				"it must become a new contract (experience.v2) by deliberate decision, not a bump of %q",
			version,
		)
	case ChangeNone:
		return version, nil
	case ChangeMinor:
		major, minor, _, err := parseSemver(version)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%d.%d.0", major, minor+1), nil
	default:
		return "", fmt.Errorf("contract: unknown change class %v", class)
	}
}

// indexByNumber maps a field set by field number. A duplicate number (malformed
// input) keeps the last occurrence -- ClassifyChange's comparison is still
// conservative (any name/kind mismatch flags breaking).
func indexByNumber(fields []FieldDescriptor) map[int32]FieldDescriptor {
	m := make(map[int32]FieldDescriptor, len(fields))
	for _, f := range fields {
		m[f.Number] = f
	}
	return m
}

// parseSemver splits a dotted MAJOR.MINOR.PATCH. Missing trailing components
// default to 0 ("1.2" == "1.2.0"). Non-numeric components return an error.
func parseSemver(v string) (major, minor, patch int, err error) {
	parts := strings.Split(strings.TrimSpace(v), ".")
	get := func(i int) (int, error) {
		if i >= len(parts) {
			return 0, nil
		}
		return strconv.Atoi(strings.TrimSpace(parts[i]))
	}
	if major, err = get(0); err != nil {
		return 0, 0, 0, fmt.Errorf("contract: malformed semver %q: %w", v, err)
	}
	if minor, err = get(1); err != nil {
		return 0, 0, 0, fmt.Errorf("contract: malformed semver %q: %w", v, err)
	}
	if patch, err = get(2); err != nil {
		return 0, 0, 0, fmt.Errorf("contract: malformed semver %q: %w", v, err)
	}
	return major, minor, patch, nil
}
