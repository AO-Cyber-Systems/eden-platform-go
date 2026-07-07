// navgraph.go -- TRD 140-05, must-have #2.
//
// NavGraph (proto) is the typed navigation GRAPH: a landing surface + placed
// NavSlots + typed NavEdges carrying param_bindings (the customer->invoice->
// payment selection-passing). This file is its COHERENCE logic -- the
// machine-checked nav rules the gap review demanded ("machine-checked in the M0
// validator + composition tests", not a ProblemsPanel of two ad-hoc checks).
//
// ValidateNavGraph returns a slice of typed CoherenceError (never panics, never
// a bare error string). The 140-09 M0 validator composes these same checks.
//
// NON-LEAKING SCOPE GUARANTEE: a graph node referencing a surface the requesting
// scope is NOT entitled to collapses to the SAME CoherenceSurfaceNotEntitled
// code whether the surface exists for ANOTHER tenant or does not exist at all.
// Entitlement is decided ONLY against the caller-supplied entitled set (the
// requesting scope's granted surfaces) -- this denies a cross-tenant existence
// oracle, mirroring binding.ResolveScope's single ErrScopeDenied sentinel.
package experience

import (
	"fmt"

	experiencev1 "github.com/aocybersystems/eden-platform-go/gen/go/experience/v1"
)

// maxPrimarySlots is the coherence ceiling on PRIMARY-placement slots -- the
// primary nav rail/tab bar cannot hold more than this. Machine-checked here so
// the builder (and the 140-09 validator) reject an over-stuffed primary nav.
const maxPrimarySlots = 5

// CoherenceCode is the typed kind of a nav-graph coherence violation. Stable
// codes let callers (tests, the builder ProblemsPanel, the M0 validator) branch
// without string-matching.
type CoherenceCode string

const (
	// CoherenceTooManyPrimary -- more than maxPrimarySlots PRIMARY slots.
	CoherenceTooManyPrimary CoherenceCode = "nav.too_many_primary"
	// CoherenceLandingMissing -- landing_surface_id is not any slot's surface.
	CoherenceLandingMissing CoherenceCode = "nav.landing_missing"
	// CoherenceEdgeEndpointMissing -- an edge from/to surface is not a slot.
	CoherenceEdgeEndpointMissing CoherenceCode = "nav.edge_endpoint_missing"
	// CoherenceSurfaceNotEntitled -- a graph surface is not in the requesting
	// scope's entitled set. SAME code whether it exists for another tenant or
	// not at all (no existence oracle).
	CoherenceSurfaceNotEntitled CoherenceCode = "nav.surface_not_entitled"
)

// CoherenceError is one machine-checked nav-graph violation. SurfaceID is the
// offending surface when the rule is surface-scoped ("" for graph-level rules
// like too-many-primary).
type CoherenceError struct {
	Code      CoherenceCode
	SurfaceID string
	Message   string
}

func (e CoherenceError) Error() string {
	if e.SurfaceID != "" {
		return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.SurfaceID)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// ValidateNavGraph machine-checks a NavGraph's coherence against the requesting
// scope's entitled surface set. entitled is the set of surface ids the caller's
// scope is GRANTED -- a graph node outside it is CoherenceSurfaceNotEntitled,
// identical whether it belongs to another tenant or does not exist (no oracle).
//
// Rules checked (all, accumulated -- not short-circuited, so the builder shows
// every problem at once):
//  1. <=5 PRIMARY slots.
//  2. landing_surface_id is one of the slots' surfaces.
//  3. every edge endpoint (from + to) is one of the slots' surfaces.
//  4. every slot surface is in the entitled set (scope non-leak).
//
// A nil graph is vacuously coherent (no nav declared yet).
func ValidateNavGraph(g *experiencev1.NavGraph, entitled map[string]struct{}) []CoherenceError {
	if g == nil {
		return nil
	}

	var errs []CoherenceError

	// Index the slots' surfaces for membership checks.
	slotSurfaces := make(map[string]struct{}, len(g.GetSlots()))
	primaryCount := 0
	for _, sl := range g.GetSlots() {
		slotSurfaces[sl.GetSurfaceId()] = struct{}{}
		if sl.GetPlacement() == experiencev1.Placement_PLACEMENT_PRIMARY {
			primaryCount++
		}
	}

	// (1) <=5 PRIMARY slots.
	if primaryCount > maxPrimarySlots {
		errs = append(errs, CoherenceError{
			Code:    CoherenceTooManyPrimary,
			Message: fmt.Sprintf("primary nav has %d slots; max is %d", primaryCount, maxPrimarySlots),
		})
	}

	// (2) landing must be a placed surface.
	if landing := g.GetLandingSurfaceId(); landing != "" {
		if _, ok := slotSurfaces[landing]; !ok {
			errs = append(errs, CoherenceError{
				Code:      CoherenceLandingMissing,
				SurfaceID: landing,
				Message:   "landing_surface_id is not a placed slot",
			})
		}
	}

	// (3) every edge endpoint must be a placed surface.
	for _, e := range g.GetEdges() {
		for _, endpoint := range []string{e.GetFromSurfaceId(), e.GetToSurfaceId()} {
			if endpoint == "" {
				continue
			}
			if _, ok := slotSurfaces[endpoint]; !ok {
				errs = append(errs, CoherenceError{
					Code:      CoherenceEdgeEndpointMissing,
					SurfaceID: endpoint,
					Message:   "edge endpoint is not a placed slot",
				})
			}
		}
	}

	// (4) scope non-leak: every placed surface must be entitled to the
	// requesting scope. Not-entitled is the SAME outcome whether the surface
	// belongs to another tenant or does not exist -- no existence oracle.
	for _, sl := range g.GetSlots() {
		if _, ok := entitled[sl.GetSurfaceId()]; !ok {
			errs = append(errs, CoherenceError{
				Code:      CoherenceSurfaceNotEntitled,
				SurfaceID: sl.GetSurfaceId(),
				Message:   "surface is not entitled to the requesting scope",
			})
		}
	}

	return errs
}
