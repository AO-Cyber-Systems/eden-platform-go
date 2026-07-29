package rpauth

// revalidate.go — the hook a LONG-LIVED CONNECTION uses to notice that the
// session behind it has been revoked.
//
// WHY THIS IS A SEPARATE THING AT ALL. An HTTP request re-authenticates from
// scratch every time, so the revocation lag for HTTP is just the cache TTL and
// nothing else has to be built. A WebSocket authenticates ONCE, at the upgrade,
// and then streams for hours. Without a periodic re-check, revoking a session in
// AOID would close every future request and none of the connections already
// open — the user is logged out everywhere except the one place still receiving
// their messages. That is the failure this file exists to prevent, and it is why
// "revocation actually works" cannot be verified on the HTTP path alone.
//
// The three decisions that carry the weight:
//
//   - THE INTERVAL IS NOT THE CALLER'S TO CHOOSE. DefaultMaxTTL is the
//     platform's revocation-lag budget. A hub that picked its own period would
//     widen that budget silently and nobody would find out until an incident.
//     So the period is DERIVED — RevalidationInterval() reads the same
//     Introspector the middleware uses — and RevalidateLoop never accepts one as
//     an argument. See the RevalidationInterval doc for the arithmetic.
//
//   - REVALIDATION BYPASSES THE CACHE, DELIBERATELY. See RevalidationInterval:
//     a re-check answered from cache proves only that the token was valid when
//     the entry was written, which is the exact question being asked. This is
//     the difference between a revocation guarantee that holds and one that only
//     reads as though it does.
//
//   - THERE ARE THREE ANSWERS, NOT TWO. rpauth already establishes that
//     ErrUnavailable is a 503 and not a 401: an AOID outage must never present
//     as a credential problem. On this path the stakes are higher than a status
//     code. If "cannot reach AOID" collapsed into "not valid", a thirty-second
//     AOID blip would force-close every live WebSocket on the platform at once,
//     and every one of those clients would reconnect into an authority that is
//     still down. A transient failure holds the connection open; only a
//     definitive negative closes it.

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"
)

// RevalidationStatus is the answer to "may this connection keep streaming?".
type RevalidationStatus int

const (
	// RevalidationIndeterminate — the authority could not be consulted, or could
	// be consulted but the relying party could not load its own principal. This
	// is NOT a negative answer and MUST NOT close the connection: nothing has
	// been learned about the credential. Log it, alarm on a sustained run of it,
	// and keep streaming.
	//
	// It is the ZERO VALUE on purpose. A RevalidationResult that was never
	// filled in — a struct built by mistake, a map lookup that missed — then
	// reads as "no answer", which is the only safe thing for it to mean. Making
	// Valid the zero value would fail open on a bug; making Revoked the zero
	// value would drop live connections on one.
	RevalidationIndeterminate RevalidationStatus = iota

	// RevalidationValid — the authority was reached and confirmed the token is
	// still active, and the principal reloaded. The connection continues.
	RevalidationValid

	// RevalidationRevoked — a DEFINITIVE negative: the authority answered
	// active=false, the token's own exp has passed, or the credential was
	// rejected outright (a refresh token presented as a bearer token). This is
	// the only status that closes the connection.
	RevalidationRevoked
)

// String renders a RevalidationStatus for logs and metrics.
func (s RevalidationStatus) String() string {
	switch s {
	case RevalidationValid:
		return "valid"
	case RevalidationRevoked:
		return "revoked"
	case RevalidationIndeterminate:
		return "indeterminate"
	default:
		return "unknown"
	}
}

// RevalidationResult is one answer from Revalidate.
//
// Identity and Principal are meaningful only when Status is RevalidationValid.
// Err carries the reason for a Revoked or an Indeterminate answer and is nil
// otherwise; it is for logs, not for control flow — branch on Status, or on
// ShouldDisconnect.
type RevalidationResult[P any] struct {
	Status    RevalidationStatus
	Identity  *Identity
	Principal P
	Err       error
	// CheckedAt is when the answer was produced, so a caller can age it out or
	// report how long a connection has been running on an indeterminate check.
	CheckedAt time.Time
}

// ShouldDisconnect reports whether the caller must terminate the connection.
//
// It is true for RevalidationRevoked and NOTHING ELSE. This is the only
// predicate this type offers, and the omission is the point: an `Ok()` or
// `IsValid()` would invite `if !res.Ok() { close() }`, which closes every live
// connection on the platform the moment AOID hiccups. There is no negation of
// "valid" that is safe to disconnect on, so the type does not provide one.
func (r RevalidationResult[P]) ShouldDisconnect() bool {
	return r.Status == RevalidationRevoked
}

// RevalidationInterval is how often a long-lived connection must call
// Revalidate. DERIVE THE PERIOD FROM THIS; do not hard-code one.
//
// It equals the Introspector's MaxTTL (DefaultMaxTTL, 60s, unless configured),
// so the platform has ONE revocation-lag number rather than one per transport.
//
// THE ARITHMETIC, because it is the whole point of the objective's revocation
// gate:
//
//	worst-case lag (HTTP)      = cache TTL                 = MaxTTL
//	worst-case lag (WebSocket) = interval + introspect RTT = MaxTTL + RTT
//
// The WebSocket line only holds because of two things this package does and a
// naive implementation would not:
//
//  1. REVALIDATION BYPASSES THE CACHE (ValidateUncached). If it read through the
//     cache, a check at interval == TTL could be answered by an entry written
//     just before the revocation, and the real lag would be TTL + interval —
//     120s with the defaults, twice the documented budget, and worse: the check
//     would sometimes contact the authority and sometimes not, so the guarantee
//     would hold intermittently and pass a casual test. Choosing an interval
//     shorter than the TTL "so it always misses" does not fix this either; it
//     only makes the coincidence less frequent.
//
//  2. THE LOOP TAKES A FRESH BASELINE AT START. The connection's UPGRADE was
//     authenticated through the ordinary middleware, so that answer may itself
//     already be up to MaxTTL stale. Starting the first sleep from a stale
//     answer would put the first honest check at up to 2×MaxTTL after the last
//     contact with AOID. RevalidateLoop therefore revalidates once,
//     uncached, immediately on entry, and only then starts the interval —
//     one extra introspection per connection ESTABLISHMENT, which for a
//     transport whose whole premise is that connections are long-lived is
//     nothing.
//
// Between ticks the delay is jittered into [interval/2, interval) so ten
// thousand connections do not all introspect on the same second after a mass
// reconnect. Jitter can only SHORTEN a gap, never lengthen it, so the bound
// above is unaffected.
func (a *Authenticator[P]) RevalidationInterval() time.Duration {
	return a.introspector.MaxTTL()
}

// Revalidate re-checks a token that an already-established connection is
// holding, and reports whether that connection may continue.
//
// IT RETURNS NO ERROR, and that is deliberate. The idiomatic (P, error) shape
// would put an outage and a revocation in the same `err != nil` branch, and the
// natural handling of that branch — close the connection — is correct for one
// and catastrophic for the other. Everything the caller needs is in the result;
// the decision is res.ShouldDisconnect(), and there is no shape of `if err !=
// nil` that accidentally looks right.
//
// The token is introspected against AOID with the cache bypassed; see
// RevalidationInterval for why that is load-bearing rather than cautious.
//
// Refresh tokens are rejected here exactly as they are everywhere else in this
// package — AOID's refresh-token introspection path returns an ACCOUNT id in
// `sub` where the access-token path returns an IDENTITY id, so honouring one
// would keep a connection alive against a principal resolved from the wrong
// entity. A refresh token therefore revalidates as Revoked.
//
// Pass the same token the connection authenticated with. A connection
// authenticated by the relying party's OWN credential scheme (AODex's API keys,
// say) is not this package's to revalidate — those tokens are excluded from
// introspection at the extractor and would revalidate as Revoked here.
func (a *Authenticator[P]) Revalidate(ctx context.Context, rawToken string) RevalidationResult[P] {
	res := RevalidationResult[P]{CheckedAt: a.introspector.nowFn()}

	identity, err := a.introspector.ValidateUncached(ctx, rawToken)
	switch {
	case errors.Is(err, ErrInactive):
		// The authority gave a definitive negative: revoked, expired, never
		// issued, or the wrong kind of credential. This is the disconnect.
		res.Status = RevalidationRevoked
		res.Err = err
		return res

	case err != nil:
		// ErrUnavailable AND anything unclassified. Both are "we could not
		// find out", never "the credential is bad" — an unrecognised error is
		// not evidence of revocation, and manufacturing one from it would make
		// every new failure mode a platform-wide disconnect.
		a.logger.Warn("[rpauth] revalidation could not reach a verdict — holding the connection open",
			"error", err)
		res.Status = RevalidationIndeterminate
		res.Err = err
		return res
	}

	// The authority said active, but a token can outlive its own exp between
	// checks and a connection can outlive the token. Enforce exp locally rather
	// than assuming the authority always does: a connection is the one place
	// where nothing else will catch it.
	if !identity.ExpiresAt.IsZero() && !res.CheckedAt.Before(identity.ExpiresAt) {
		a.introspector.Invalidate(rawToken)
		res.Status = RevalidationRevoked
		res.Identity = identity
		res.Err = fmt.Errorf("%w: token expired at %s", ErrInactive,
			identity.ExpiresAt.UTC().Format(time.RFC3339))
		return res
	}

	// Reload the principal so the connection sees the same view of the user a
	// fresh HTTP request would — a tier change or a demotion should not need a
	// reconnect to take effect.
	principal, err := a.load(ctx, identity)
	if err != nil {
		// A LOCAL fault on a credential the authority just confirmed. Same
		// reasoning as OutcomePrincipalError mapping to 503 rather than 401 in
		// the middleware: AOID said yes, so this is not a revocation and must
		// not close the connection.
		a.logger.Warn("[rpauth] revalidation could not reload the principal — holding the connection open",
			"subject", identity.Subject,
			"tenant_slug", identity.TenantSlug,
			"error", err)
		res.Status = RevalidationIndeterminate
		res.Identity = identity
		res.Err = err
		return res
	}

	res.Status = RevalidationValid
	res.Identity = identity
	res.Principal = principal
	return res
}

// RevalidateLoop revalidates rawToken on the authenticator's own interval and
// hands every answer to onResult.
//
// IT BLOCKS IN THE CALLER'S GOROUTINE and starts none of its own, so the
// connection owns the lifecycle: run it as `go func() { auth.RevalidateLoop(...)
// }()` from wherever the connection lives, and cancel ctx when the connection
// closes. There is nothing to Stop, nothing to leak, and no interval to get
// wrong — RevalidationInterval supplies it.
//
// It takes a fresh, uncached baseline check IMMEDIATELY on entry (see
// RevalidationInterval, point 2) and then re-checks on the jittered interval.
//
// onResult returns whether to keep looping. The loop also stops on its own once
// it has delivered a RevalidationRevoked result, whatever onResult returned: a
// revoked token does not come back, so continuing would poll the authority
// forever about a session that has ended.
func (a *Authenticator[P]) RevalidateLoop(ctx context.Context, rawToken string, onResult func(RevalidationResult[P]) bool) {
	if onResult == nil {
		return
	}
	interval := a.RevalidationInterval()
	if interval <= 0 {
		interval = DefaultMaxTTL
	}

	timer := time.NewTimer(0) // the baseline check, immediately
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}

		res := a.Revalidate(ctx, rawToken)
		keepGoing := onResult(res)
		if res.Status == RevalidationRevoked || !keepGoing {
			return
		}
		timer.Reset(a.nextDelay(interval))
	}
}

// nextDelay spreads connections across the window instead of bunching them onto
// the same second. It can only shorten a gap, so the revocation bound holds.
func (a *Authenticator[P]) nextDelay(interval time.Duration) time.Duration {
	if a.delayFn != nil {
		return a.delayFn(interval)
	}
	half := interval / 2
	if half <= 0 {
		return interval
	}
	return half + rand.N(half)
}
