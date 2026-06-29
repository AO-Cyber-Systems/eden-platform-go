// presentation.go -- TRD 140-06, must-haves #5 + #6.
//
// The 140-06 contract adds the TYPED presentation + runtime-offline-policy
// surface to ExperienceSpec:
//   - ThemeSpec  (brand_preset / logo_ref / color_overrides / density)
//   - TermSet    (presentation-only term overrides, e.g. Job->Visit)
//   - LocaleSpec (locale / currency / TIMEZONE -- timezone is the net-new
//                 load-bearing field, required for scheduling/field surfaces)
//   - surface_offline: map<surface_id, OfflineSpec> -- offline is PER-SURFACE
//     and STRUCTURED (policy/cache_ttl/conflict_policy/grace), never a bare bool.
//
// This file is the COHERENCE logic for that surface -- the machine-checked rules
// the M0 validator (140-09) composes. It reuses the SAME typed CoherenceError /
// CoherenceCode vocabulary as navgraph.go so callers branch on stable codes,
// never string-match. ValidatePresentation never panics and accumulates every
// violation (so the builder ProblemsPanel shows them all at once).
//
// Two rules are checked:
//
//  1. CoherenceMissingTimezone -- a surface that (a) declares an offline policy
//     that serves data offline (READ_CACHE or READ_WRITE_QUEUE) AND (b) is a
//     scheduling surface needs LocaleSpec.timezone. tz is load-bearing: an
//     offline scheduling surface cannot resolve local appointment times without
//     it. The set of scheduling surface ids is supplied by the caller
//     (SchedulingSurfaces) -- the contract does not hardcode which surfaces
//     schedule.
//
//  2. CoherenceIncoherentOffline -- an OfflineSpec whose policy is NONE (or
//     UNSPECIFIED, which fails safe to NONE) but which still carries a
//     non-UNSPECIFIED conflict_policy. A conflict strategy is meaningless when
//     nothing is queued offline -- this is an incoherent (warn-level) config.
package experience

import (
	"fmt"

	experiencev1 "github.com/aocybersystems/eden-platform-go/gen/go/experience/v1"
)

const (
	// CoherenceMissingTimezone -- an offline scheduling surface has no
	// LocaleSpec.timezone. tz is load-bearing for resolving local appointment
	// times; an offline scheduling surface without it is incoherent.
	CoherenceMissingTimezone CoherenceCode = "presentation.missing_timezone"
	// CoherenceIncoherentOffline -- an OfflineSpec carries a conflict_policy
	// while its policy is NONE/UNSPECIFIED (nothing is queued offline, so a
	// conflict strategy is meaningless). Warn-level.
	CoherenceIncoherentOffline CoherenceCode = "presentation.incoherent_offline"
)

// SchedulingSurfaces builds the set of surface ids the caller designates as
// scheduling surfaces (the surfaces for which LocaleSpec.timezone is
// load-bearing). The contract does NOT hardcode which surfaces schedule -- the
// resolving context supplies them, mirroring how ValidateNavGraph takes the
// entitled set as a caller-supplied argument.
func SchedulingSurfaces(ids ...string) map[string]struct{} {
	m := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		m[id] = struct{}{}
	}
	return m
}

// servesDataOffline reports whether a policy keeps data available offline
// (READ_CACHE or READ_WRITE_QUEUE). NONE / UNSPECIFIED do not.
func servesDataOffline(p experiencev1.OfflinePolicy) bool {
	switch p {
	case experiencev1.OfflinePolicy_OFFLINE_POLICY_READ_CACHE,
		experiencev1.OfflinePolicy_OFFLINE_POLICY_READ_WRITE_QUEUE:
		return true
	default:
		return false
	}
}

// offlineIsActive reports whether a policy actually puts the surface offline at
// all (anything other than NONE / UNSPECIFIED-fails-safe-to-NONE).
func offlineIsActive(p experiencev1.OfflinePolicy) bool {
	switch p {
	case experiencev1.OfflinePolicy_OFFLINE_POLICY_UNSPECIFIED,
		experiencev1.OfflinePolicy_OFFLINE_POLICY_NONE:
		return false
	default:
		return true
	}
}

// ValidatePresentation machine-checks the presentation + offline coherence of a
// resolved ExperienceSpec. schedulingSurfaces is the caller-supplied set of
// surface ids for which LocaleSpec.timezone is load-bearing.
//
// Rules (all accumulated, not short-circuited):
//  1. an offline-serving (READ_CACHE/READ_WRITE_QUEUE) surface that is a
//     scheduling surface requires a non-empty LocaleSpec.timezone.
//  2. an OfflineSpec with a non-UNSPECIFIED conflict_policy while its policy is
//     NONE/UNSPECIFIED is incoherent (warn).
//
// A nil spec is vacuously coherent. The surface_offline map is iterated in no
// guaranteed order; every offending surface yields its own CoherenceError.
func ValidatePresentation(spec *experiencev1.ExperienceSpec, schedulingSurfaces map[string]struct{}) []CoherenceError {
	if spec == nil {
		return nil
	}

	var errs []CoherenceError

	timezone := spec.GetLocale().GetTimezone()

	for surfaceID, off := range spec.GetSurfaceOffline() {
		if off == nil {
			continue
		}
		policy := off.GetPolicy()

		// (1) offline scheduling surface needs a timezone.
		if servesDataOffline(policy) {
			if _, isScheduling := schedulingSurfaces[surfaceID]; isScheduling && timezone == "" {
				errs = append(errs, CoherenceError{
					Code:      CoherenceMissingTimezone,
					SurfaceID: surfaceID,
					Message:   "offline scheduling surface requires LocaleSpec.timezone (load-bearing for local appointment times)",
				})
			}
		}

		// (2) conflict_policy set while offline is inactive (NONE/UNSPECIFIED).
		if !offlineIsActive(policy) &&
			off.GetConflictPolicy() != experiencev1.ConflictPolicy_CONFLICT_POLICY_UNSPECIFIED {
			errs = append(errs, CoherenceError{
				Code:      CoherenceIncoherentOffline,
				SurfaceID: surfaceID,
				Message: fmt.Sprintf(
					"conflict_policy %s set while offline policy is %s (nothing is queued offline)",
					off.GetConflictPolicy(), policy,
				),
			})
		}
	}

	return errs
}
