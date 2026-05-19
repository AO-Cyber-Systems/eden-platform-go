package risk

import (
	"context"
	"net"
	"time"

	"github.com/google/uuid"
)

// Request is the input to Evaluator.Eval. Populated by the caller (typically
// AOID's Obj 3 login service) with what's known about the auth attempt.
//
// All fields are optional. Signals must be nil-safe on every field —
// missing data MUST NOT produce a false positive.
type Request struct {
	// TenantID identifies the tenant policy + history scope.
	TenantID uuid.UUID

	// AccountID is the candidate account. May be the zero UUID early in the
	// flow (e.g., username not yet resolved); historical signals MUST handle
	// this case as "no history" rather than panicking.
	AccountID uuid.UUID

	// SourceIP is the dotted-quad / hex IPv6 string parsed from the request
	// (typically populated by httputil.EventFromHTTP). Signals MUST nil-check
	// net.ParseIP before use.
	SourceIP string

	// UserAgent is the raw HTTP User-Agent header. May be empty or
	// malformed. UA-based signals MUST tolerate both.
	UserAgent string

	// AcceptLanguage is the raw HTTP Accept-Language header, used for
	// device-fingerprint composition. May be empty.
	AcceptLanguage string

	// AttemptedAt is the moment the auth attempt began (set by the caller).
	// Used for velocity calculations against historical geos.
	AttemptedAt time.Time

	// PolicyContext is a snapshot of tenant policy relevant to risk
	// evaluation. Used by signals like MFABypassAttempted. Keys MUST be
	// documented in the README's signal table. Nil-safe.
	PolicyContext map[string]any

	// HistoricalLookups is the AOID-side query interface. Eden defines the
	// contract; AOID's TRD 09-05 implements it against aoid.auth_attempts
	// and aoid.account_known_devices. May be nil — historical signals MUST
	// no-op when nil.
	HistoricalLookups HistoricalLookups

	// GeoIP is the IP-to-geo lookup. May be a NoOpGeoIP when the MaxMind DB
	// is missing. Signals MUST tolerate Lookup returning (nil, nil).
	GeoIP GeoIPLookup
}

// HistoricalLookups is the contract for AOID-side historical queries.
// Implemented by AOID's risk service (TRD 09-05).
//
// Implementations MUST be safe for concurrent use.
type HistoricalLookups interface {
	// RecentAttempts returns the account's auth attempts within window,
	// most-recent-first. Empty slice if none.
	RecentAttempts(ctx context.Context, accountID uuid.UUID, window time.Duration) ([]Attempt, error)

	// KnownDeviceFingerprints returns the set of device-fingerprint hashes
	// the account has previously authenticated with successfully, observed
	// inside window. Empty slice if none.
	KnownDeviceFingerprints(ctx context.Context, accountID uuid.UUID, window time.Duration) ([]string, error)

	// LastLoginGeo returns the geo recorded for the account's most recent
	// successful login. Returns (nil, nil) if the account has no prior
	// successful login.
	LastLoginGeo(ctx context.Context, accountID uuid.UUID) (*GeoLocation, error)

	// IsAnonymizerIP reports whether ip is on the operator-supplied
	// anonymizer / Tor exit-node list. Implementations refresh the list out
	// of band; this method is hot-path and must be cheap.
	IsAnonymizerIP(ctx context.Context, ip net.IP) (bool, error)
}

// Attempt is a historical auth attempt record summary.
type Attempt struct {
	// Outcome is one of: "success", "failure", "blocked".
	Outcome     string
	AttemptedAt time.Time
	SourceIP    string
	CountryCode string
	UserAgent   string
}

// GeoLocation describes a geographic point + the moment it was observed.
//
// The At field is populated by callers (e.g., HistoricalLookups.LastLoginGeo
// sets it to the attempted_at of the source row; MaxMindGeoIP.Lookup sets it
// to time.Now() to denote "this lookup time").
type GeoLocation struct {
	CountryCode string
	City        string
	Lat         float64
	Lng         float64
	AccuracyKM  int32
	At          time.Time
}

// Signal is the pluggable contract that risk-scoring extensions implement.
//
// Implementations MUST be:
//   - Safe for concurrent Evaluate calls (no mutable state, or guarded).
//   - Nil-safe on every field of Request (missing data = no trigger).
//   - Side-effect free (no audit emission, no DB writes). Audit emission is
//     the caller's responsibility.
//   - Sub-millisecond on the hot path (avoid network I/O; HistoricalLookups
//   - GeoIP are the only acceptable I/O calls and both are in-process).
type Signal interface {
	// Name is the stable signal identifier emitted in audit records and
	// metrics. Use snake_case (e.g., "geo_velocity_anomaly").
	Name() string

	// Evaluate decides whether the signal triggers for the given request.
	// On trigger, returns (true, weight, details). On no trigger, returns
	// (false, 0, nil). details is carried verbatim into Triggered.Details
	// and surfaced to the audit pipeline for operator forensics.
	Evaluate(ctx context.Context, req Request) (triggered bool, weight int32, details map[string]any)
}

// Result is the Evaluator's output.
type Result struct {
	// Score is the post-clip cumulative weight in [0, clipAt]. Default
	// clipAt is 100.
	Score int32

	// Triggered is the in-order list of signals that fired. Empty when no
	// signal triggered.
	Triggered []Triggered
}

// Triggered is one signal that fired.
type Triggered struct {
	Signal  string
	Weight  int32
	Details map[string]any
}

// Evaluator orchestrates Signal evaluations + clipping.
//
// An Evaluator is constructed once per tenant (or once per shared signal
// set) and may be safely called concurrently from any number of goroutines.
type Evaluator struct {
	signals []Signal
	clipAt  int32
}

// Option configures the Evaluator.
type Option func(*Evaluator)

// WithClip sets the maximum score returned by Eval. The default is 100.
// Pass 0 to allow unclipped (additive) scores — typically a debugging aid.
func WithClip(max int32) Option {
	return func(e *Evaluator) { e.clipAt = max }
}

// NewEvaluator constructs an Evaluator from an in-order signal list.
//
// Signals are evaluated in the order provided. Order matters only for the
// Triggered slice (audit trail); the final Score is order-independent.
func NewEvaluator(signals []Signal, opts ...Option) *Evaluator {
	e := &Evaluator{signals: signals, clipAt: 100}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Signals returns the signal list this Evaluator was constructed with.
// Returned slice MUST be treated as read-only.
func (e *Evaluator) Signals() []Signal { return e.signals }

// ClipAt returns the configured clip threshold.
func (e *Evaluator) ClipAt() int32 { return e.clipAt }

// Eval runs every Signal in order and returns the cumulative Result.
//
// Safe for concurrent use as long as Signal implementations are too (the
// baseline set in signals.go IS safe).
func (e *Evaluator) Eval(ctx context.Context, req Request) Result {
	out := Result{Score: 0, Triggered: make([]Triggered, 0, 4)}
	for _, s := range e.signals {
		triggered, weight, details := s.Evaluate(ctx, req)
		if !triggered {
			continue
		}
		out.Score += weight
		out.Triggered = append(out.Triggered, Triggered{
			Signal:  s.Name(),
			Weight:  weight,
			Details: details,
		})
	}
	if e.clipAt > 0 && out.Score > e.clipAt {
		out.Score = e.clipAt
	}
	if out.Score < 0 {
		out.Score = 0
	}
	return out
}
