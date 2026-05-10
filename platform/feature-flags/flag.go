// Package featureflags provides runtime feature gating for the Eden portfolio.
//
// This package is DISTINCT from billing entitlements. Use it for:
//
//   - Toggling code paths during a rollout ("new_billing_dashboard")
//   - Killing a misbehaving feature without a deploy
//   - Per-tenant or per-household variants
//   - Gradual percentage rollouts
//
// For "did this caller pay for tier X?" questions, see
// platform/entitlements (canonical billing-entitlement source) — and
// platform/billing-rail for the upstream payment events that feed it.
//
// Composition example:
//
//	if !flags.IsEnabled(ctx, "household_billing", featureflags.Eval{TenantID: t}) {
//	    return ErrFeatureDisabled
//	}
//	if !ent.HasEntitlement(ctx, customer, "household_billing") {
//	    return ErrUpgradeRequired
//	}
//	// proceed
package featureflags

// Eval is the per-call evaluation context. All fields are optional; a
// zero-value Eval is legal and matches only environment-agnostic flags.
type Eval struct {
	// SubjectID is the user / actor id (e.g. user UUID).
	SubjectID string
	// HouseholdID is the household scope (Eden Family).
	HouseholdID string
	// TenantID is the tenant / company id.
	TenantID string
	// Environment is the deployment environment ("dev","staging","prod").
	Environment string
}

// Flag is the canonical feature-flag record stored by a Source.
//
// A flag is either:
//   - Boolean — Variants is nil; Enabled determines the value
//   - Variant — Variants is non-nil; Default names the fallback variant
//
// Override and Rollout fields apply uniformly to both shapes.
type Flag struct {
	// Key is the flag's stable identifier (e.g. "household_billing").
	Key string

	// Enabled is the master switch. If false the flag is OFF for every caller
	// regardless of overrides or rollout.
	Enabled bool

	// Variants is the named-variant table. Nil means this is a boolean flag.
	// Values are arbitrary (string, struct, etc.); consumers know the shape.
	Variants map[string]any

	// Default is the variant name returned when Enabled and no override or
	// rollout matches. Empty for boolean flags.
	Default string

	// Rollout, if set, gates the flag by deterministic percentage of subjects.
	// Subjects with a hash bucket below Percentage receive the Default variant
	// (or true for boolean flags); others receive off / "".
	Rollout *Rollout

	// Overrides are matched in order; the first matching override wins.
	// More specific overrides should be listed first.
	Overrides []Override
}

// Rollout describes a deterministic percentage rollout. Same SubjectID + Salt
// always yields the same bucket, so a subject's experience is stable across
// process restarts and pod replicas.
type Rollout struct {
	// Percentage is in the range [0, 100]. 0 disables the rollout (flag is
	// off via rollout); 100 enables it for everyone (rollout always passes).
	Percentage int

	// Salt is mixed into the hash so two flags rolling out to "50%" don't
	// pick the same half of the user base. Use the flag key when in doubt.
	Salt string
}

// Override matches a Flag against a subset of an Eval and returns Value when
// every non-empty axis on the Override matches the corresponding axis on the
// Eval. Empty axes are wildcards.
type Override struct {
	// Axes — empty fields wildcard. All non-empty fields must match.
	TenantID    string
	HouseholdID string
	SubjectID   string
	Environment string

	// Value is what the override returns. For boolean flags, Value should
	// be a bool. For variant flags, Value should be a variant name (string).
	Value any
}

// matches reports whether o applies to the given Eval. An empty axis on the
// Override is a wildcard for that axis.
func (o Override) matches(e Eval) bool {
	if o.TenantID != "" && o.TenantID != e.TenantID {
		return false
	}
	if o.HouseholdID != "" && o.HouseholdID != e.HouseholdID {
		return false
	}
	if o.SubjectID != "" && o.SubjectID != e.SubjectID {
		return false
	}
	if o.Environment != "" && o.Environment != e.Environment {
		return false
	}
	return true
}

// specificity counts how many axes the override pins. Higher specificity
// wins when multiple overrides match the same Eval.
func (o Override) specificity() int {
	n := 0
	if o.SubjectID != "" {
		n += 8
	}
	if o.HouseholdID != "" {
		n += 4
	}
	if o.TenantID != "" {
		n += 2
	}
	if o.Environment != "" {
		n += 1
	}
	return n
}
