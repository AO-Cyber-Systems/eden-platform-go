package featureflags

import (
	"context"
	"errors"
	"sort"
)

// Client evaluates flags against a Source. Construct one per process and
// share — Client is safe for concurrent use as long as the underlying Source
// is.
type Client struct {
	src    Source
	logger ErrorLogger
}

// ErrorLogger is the optional sink for non-fatal evaluation errors (Source
// failures). The Client never returns these from IsEnabled / Variant; they
// are surfaced here for observability.
type ErrorLogger func(key string, err error)

// New returns a Client backed by src. The Source must not be nil.
func New(src Source) *Client {
	if src == nil {
		panic("featureflags: nil Source")
	}
	return &Client{src: src}
}

// WithErrorLogger attaches an error sink for source failures and returns the
// receiver for chaining. nil clears the sink.
func (c *Client) WithErrorLogger(l ErrorLogger) *Client {
	c.logger = l
	return c
}

// IsEnabled returns whether the boolean flag named key is on for the given
// Eval. On any source error or unknown flag, the result is false (closed).
//
// For variant flags, IsEnabled returns true iff the resolved variant is the
// flag's Default and the Default is non-empty — i.e. "the flag fired with
// its expected value". Use Variant for direct variant inspection.
func (c *Client) IsEnabled(ctx context.Context, key string, e Eval) bool {
	flag, ok, err := c.lookup(ctx, key)
	if err != nil || !ok {
		return false
	}
	return c.evaluateBool(flag, e)
}

// Variant resolves a variant flag to its (name, value) pair. Returns
// (name, value, true) when the flag is on and a variant resolves.
// Returns ("", nil, false) when the flag is off, missing, or has no variants.
func (c *Client) Variant(ctx context.Context, key string, e Eval) (string, any, bool) {
	flag, ok, err := c.lookup(ctx, key)
	if err != nil || !ok {
		return "", nil, false
	}
	if !flag.Enabled {
		return "", nil, false
	}
	if flag.Variants == nil {
		return "", nil, false
	}
	name, ok := c.evaluateVariant(flag, e)
	if !ok {
		return "", nil, false
	}
	v, exists := flag.Variants[name]
	if !exists {
		return "", nil, false
	}
	return name, v, true
}

func (c *Client) lookup(ctx context.Context, key string) (Flag, bool, error) {
	flag, ok, err := c.src.Lookup(ctx, key)
	if err != nil {
		if c.logger != nil {
			c.logger(key, err)
		}
		return Flag{}, false, err
	}
	return flag, ok, nil
}

// evaluateBool resolves a boolean flag against e. The resolution order is:
//  1. Master switch off → false
//  2. Best-matching override → coerced to bool (true if Value is bool true,
//     a string equal to flag.Default, or — for boolean flags w/o variants —
//     truthy)
//  3. Rollout (if set) → true when subject is in the rollout, else false
//  4. Variant flags fall back to (Default == Default) → true
//  5. Plain boolean flag with no rollout/overrides → true
func (c *Client) evaluateBool(flag Flag, e Eval) bool {
	if !flag.Enabled {
		return false
	}
	if v, ok := bestOverride(flag, e); ok {
		return overrideToBool(flag, v)
	}
	if flag.Rollout != nil {
		return inRollout(e.SubjectID, flag.Rollout.Salt, flag.Rollout.Percentage)
	}
	// Variant flag with default → "fired with default" is true.
	if flag.Variants != nil {
		return flag.Default != ""
	}
	return true
}

// evaluateVariant resolves a variant flag's resulting variant name. Returns
// ("", false) when the flag fails to resolve.
func (c *Client) evaluateVariant(flag Flag, e Eval) (string, bool) {
	if v, ok := bestOverride(flag, e); ok {
		if name, ok := v.(string); ok {
			if _, exists := flag.Variants[name]; exists {
				return name, true
			}
		}
		// Override was non-string for a variant flag; fall through to default.
	}
	if flag.Rollout != nil {
		if !inRollout(e.SubjectID, flag.Rollout.Salt, flag.Rollout.Percentage) {
			return "", false
		}
	}
	if flag.Default == "" {
		return "", false
	}
	return flag.Default, true
}

// bestOverride returns the most-specific matching override's Value.
func bestOverride(flag Flag, e Eval) (any, bool) {
	type match struct {
		spec int
		idx  int
		val  any
	}
	var matches []match
	for i, o := range flag.Overrides {
		if o.matches(e) {
			matches = append(matches, match{spec: o.specificity(), idx: i, val: o.Value})
		}
	}
	if len(matches) == 0 {
		return nil, false
	}
	// Sort by specificity desc, then by index asc (stable tie-break by
	// declaration order — earlier declared wins on a tie).
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].spec != matches[j].spec {
			return matches[i].spec > matches[j].spec
		}
		return matches[i].idx < matches[j].idx
	})
	return matches[0].val, true
}

// overrideToBool maps an override Value into a bool for boolean evaluation.
// For boolean flags (no variants), the override Value should be a bool;
// non-bool values fall back to false.
// For variant flags, an override Value of flag.Default returns true; any
// other variant returns false.
func overrideToBool(flag Flag, v any) bool {
	if flag.Variants == nil {
		if b, ok := v.(bool); ok {
			return b
		}
		return false
	}
	if name, ok := v.(string); ok {
		return name == flag.Default && name != ""
	}
	return false
}

// ErrSourceUnavailable is returned by a Source when it cannot read flags
// (e.g. remote service down). The Client treats this as "flag off" and
// records via the ErrorLogger if attached.
var ErrSourceUnavailable = errors.New("featureflags: source unavailable")
