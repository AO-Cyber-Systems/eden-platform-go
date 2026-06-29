// content_hash.go -- TRD 140-08, must-have #4 (the IRREVERSIBLE cache-key shape).
//
// ContentHash computes the rollback id + cache key of a RESOLVED spec. The
// resolution tuple {role, entitlements, form_factor, tenant, org} is IN THE
// PREIMAGE -- this is the load-bearing, frozen-forever decision: a device caches
// a resolved spec keyed by this hash, so two users whose tuples differ (different
// role / entitlements / form-factor / tenant / org) MUST get different hashes, or
// one user would render another's cache entry. The reverse must also hold:
// identical inputs MUST yield an identical deterministic hash across processes.
//
// Encoding reuses the biz website/build_artifact.go precedent: canonical bytes ->
// SHA-256 -> 64-char lowercase hex (SHA256Hex). The canonical bytes here are the
// DETERMINISTICALLY-marshaled resolved spec ++ a canonical tuple encoding. proto
// marshaling is NOT guaranteed stable across versions for arbitrary messages, so
// we use proto.MarshalOptions{Deterministic:true} (stable map ordering) and frame
// each segment with a length prefix so spec-bytes and tuple-bytes can never blur
// into one another (a length-framed concatenation is collision-resistant at the
// segment boundary).
package experience

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"sort"

	"github.com/aocybersystems/eden-platform-go/platform/experience/fixtures"
	experiencev1 "github.com/aocybersystems/eden-platform-go/gen/go/experience/v1"
	"google.golang.org/protobuf/proto"
)

// ResolverVersionTag is the resolver build identity stamped into every
// ResolutionContext and folded into ResolverConfig defaults. Exposed so callers
// and tests can reference the canonical value.
const ResolverVersionTag = "experience-resolver/140-08"

// SHA256Hex returns the SHA-256 hex (64-char lowercase) of b. Mirrors the biz
// website/build_artifact.go precedent so the platform shares one canonical
// content-hash encoding.
func SHA256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// ContentHash hashes (resolved spec bytes ++ resolution tuple bytes) -> SHA256Hex.
//
// The tuple is part of the preimage by construction: tuplePreimage encodes the
// FULL cache-key shape. Any tuple-axis change flips the preimage and thus the
// hash; identical inputs reproduce the same hash deterministically.
//
// The spec's own content_hash field is ZEROED before hashing so the hash is a
// fixed point (stamping the hash back onto the spec does not change what the hash
// would recompute to).
func ContentHash(resolved *experiencev1.ExperienceSpec, tuple fixtures.ResolutionTuple) string {
	specBytes := canonicalSpecBytes(resolved)
	tupleBytes := tuplePreimage(tuple)

	// Length-framed concatenation so the spec/tuple boundary is unambiguous --
	// no concatenation collision can move bytes across the segment edge.
	preimage := make([]byte, 0, len(specBytes)+len(tupleBytes)+16)
	preimage = appendFramed(preimage, specBytes)
	preimage = appendFramed(preimage, tupleBytes)

	return SHA256Hex(preimage)
}

// canonicalSpecBytes deterministically marshals the resolved spec with its
// content_hash field zeroed (so the hash is a fixed point). Deterministic:true
// stabilizes map field ordering (surface_offline, flag_overrides, custom_fields).
func canonicalSpecBytes(resolved *experiencev1.ExperienceSpec) []byte {
	clone := proto.Clone(resolved).(*experiencev1.ExperienceSpec)
	clone.ContentHash = ""
	b, err := proto.MarshalOptions{Deterministic: true}.Marshal(clone)
	if err != nil {
		// A resolved spec built from valid proto messages cannot fail to marshal;
		// fall back to an empty segment rather than panic so a hash is always
		// produced (the framing still distinguishes it from a populated spec).
		return nil
	}
	return b
}

// tuplePreimage canonically encodes the resolution tuple {role, entitlements,
// form_factor, tenant, org} PLUS the granted-surface set. Each axis is
// length-framed and the granted set is sorted so the encoding is order-stable.
// This is THE proof object: distinct tuples -> distinct preimages -> distinct
// hashes.
func tuplePreimage(t fixtures.ResolutionTuple) []byte {
	out := make([]byte, 0, 128)
	out = appendFramed(out, []byte("role="+t.Role))
	out = appendFramed(out, []byte("entitlements="+t.Entitlement))
	out = appendFramed(out, []byte("form_factor="+t.FormFactor))
	out = appendFramed(out, []byte("tenant="+t.TenantID))
	out = appendFramed(out, []byte("org="+t.OrgID))

	granted := make([]string, 0, len(t.Granted))
	for k := range t.Granted {
		granted = append(granted, k)
	}
	sort.Strings(granted)
	for _, g := range granted {
		out = appendFramed(out, []byte("granted="+g))
	}
	return out
}

// appendFramed appends a 4-byte big-endian length prefix followed by seg, so the
// concatenation of segments is unambiguous (no segment's bytes can spill into the
// next under any input).
func appendFramed(dst, seg []byte) []byte {
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(seg)))
	dst = append(dst, hdr[:]...)
	dst = append(dst, seg...)
	return dst
}
