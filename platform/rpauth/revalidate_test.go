package rpauth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// revalidatingAuth builds an Authenticator whose introspection endpoint is
// driven by respond, so a test can flip the authority's answer mid-flight the
// way a revocation does.
func revalidatingAuth(
	t *testing.T,
	respond func(w http.ResponseWriter),
	cache *Cache[Identity],
	load PrincipalLoader[testPrincipal],
) (*Authenticator[testPrincipal], *int) {
	t.Helper()
	calls := 0
	i := newTestIntrospector(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		respond(w)
	}, cache, nil)
	if load == nil {
		load = mirrorLoader
	}
	return newAuth(t, i, load), &calls
}

// The three answers, and specifically that only ONE of them disconnects.
func TestRevalidate_ThreeOutcomes(t *testing.T) {
	tests := []struct {
		name           string
		body           string
		status         int
		load           PrincipalLoader[testPrincipal]
		want           RevalidationStatus
		wantDisconnect bool
	}{
		{
			name: "still valid",
			body: activeBody,
			want: RevalidationValid,
		},
		{
			name:           "revoked — the authority says active=false",
			body:           `{"active":false}`,
			want:           RevalidationRevoked,
			wantDisconnect: true,
		},
		{
			// A connection outlives the token it was opened with. Nothing else
			// re-checks exp on this path, so this package has to.
			name:           "expired — active, but exp is in the past",
			body:           `{"active":true,"sub":"identity-1","tnt":"saas","token_type":"Bearer","exp":946684800}`,
			want:           RevalidationRevoked,
			wantDisconnect: true,
		},
		{
			// THE LOAD-BEARING CASE. If this disconnected, a thirty-second AOID
			// blip would force-close every live WebSocket on the platform, and
			// every client would reconnect into an authority that is still down.
			name:   "issuer unavailable — must NOT disconnect",
			body:   `upstream exploded`,
			status: http.StatusBadGateway,
			want:   RevalidationIndeterminate,
		},
		{
			// AOID explicitly said the credential is good; the fault is local.
			// Same reasoning as OutcomePrincipalError being 503, not 401.
			name: "principal reload fails — must NOT disconnect",
			body: activeBody,
			load: func(context.Context, *Identity) (testPrincipal, error) {
				return testPrincipal{}, errors.New("mirror row unreadable")
			},
			want: RevalidationIndeterminate,
		},
		{
			// AOID's refresh-token introspection path returns an ACCOUNT id in
			// `sub` where the access-token path returns an IDENTITY id. Honouring
			// one here would keep a connection alive against a principal resolved
			// from the wrong entity.
			name:           "refresh token stays rejected on this path too",
			body:           `{"active":true,"sub":"account-1","tnt":"saas","token_type":"refresh_token","exp":4102444800}`,
			want:           RevalidationRevoked,
			wantDisconnect: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			status := tc.status
			a, _ := revalidatingAuth(t, func(w http.ResponseWriter) {
				if status != 0 && status != http.StatusOK {
					w.WriteHeader(status)
				}
				_, _ = w.Write([]byte(tc.body))
			}, nil, tc.load)

			res := a.Revalidate(context.Background(), "tok")

			if res.Status != tc.want {
				t.Fatalf("status = %v, want %v (err: %v)", res.Status, tc.want, res.Err)
			}
			if got := res.ShouldDisconnect(); got != tc.wantDisconnect {
				t.Errorf("ShouldDisconnect() = %v, want %v", got, tc.wantDisconnect)
			}
			if tc.want == RevalidationValid && res.Principal.LocalID != "local-identity-1" {
				t.Errorf("a valid revalidation must reload the principal, got %+v", res.Principal)
			}
			if tc.want != RevalidationValid && res.Err == nil {
				t.Error("a non-valid result must carry a reason for the logs")
			}
		})
	}
}

// The property the rest of the file exists to protect, asserted on its own: an
// authority outage is never a disconnect signal, however it presents.
func TestRevalidate_UnavailableNeverDisconnects(t *testing.T) {
	cases := map[string]func(w http.ResponseWriter){
		"5xx":          func(w http.ResponseWriter) { w.WriteHeader(http.StatusBadGateway) },
		"garbage body": func(w http.ResponseWriter) { _, _ = w.Write([]byte("<html>")) },
	}
	for name, respond := range cases {
		t.Run(name, func(t *testing.T) {
			a, _ := revalidatingAuth(t, respond, nil, nil)

			res := a.Revalidate(context.Background(), "tok")
			if res.ShouldDisconnect() {
				t.Fatal("an authority outage must never close a live connection")
			}
			if res.Status != RevalidationIndeterminate {
				t.Errorf("status = %v, want indeterminate", res.Status)
			}
			if !errors.Is(res.Err, ErrUnavailable) {
				t.Errorf("the outage must stay legible as ErrUnavailable, got %v", res.Err)
			}
		})
	}
}

// A cancelled context (the connection closing mid-check) is an indeterminate
// answer, not a revocation.
func TestRevalidate_CancelledContextIsIndeterminate(t *testing.T) {
	a, _ := revalidatingAuth(t, func(w http.ResponseWriter) {
		_, _ = w.Write([]byte(activeBody))
	}, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	res := a.Revalidate(ctx, "tok")
	if res.ShouldDisconnect() {
		t.Fatal("a cancelled check must not read as a revocation")
	}
}

// THE GUARANTEE-OR-NOT TEST. A revalidation that can be answered from the cache
// proves only that the token was valid when the entry was written — which is
// precisely the question being asked. Without the bypass the revocation window
// is nominal.
func TestRevalidate_BypassesTheCache(t *testing.T) {
	revoked := false
	cache := NewCache[Identity](16, nil)
	a, calls := revalidatingAuth(t, func(w http.ResponseWriter) {
		if revoked {
			_, _ = w.Write([]byte(`{"active":false}`))
			return
		}
		_, _ = w.Write([]byte(activeBody))
	}, cache, nil)

	// Warm the cache exactly as an ordinary HTTP request would.
	if _, err := a.introspector.Validate(context.Background(), "tok"); err != nil {
		t.Fatalf("priming call: %v", err)
	}
	if *calls != 1 {
		t.Fatalf("priming should cost one call, got %d", *calls)
	}

	// The session is revoked in AOID. The cache entry is still warm and still
	// says "valid" — that is the whole hazard.
	revoked = true

	res := a.Revalidate(context.Background(), "tok")
	if *calls != 2 {
		t.Errorf("revalidation must reach the authority even on a cache hit; calls=%d", *calls)
	}
	if !res.ShouldDisconnect() {
		t.Fatalf("a revoked token must disconnect, got %v", res.Status)
	}
}

// A revocation noticed by a connection must also end that token's ordinary HTTP
// requests, rather than leaving them to ride out the remaining cache TTL.
func TestRevalidate_RevocationDropsTheCachedEntry(t *testing.T) {
	revoked := false
	cache := NewCache[Identity](16, nil)
	a, _ := revalidatingAuth(t, func(w http.ResponseWriter) {
		if revoked {
			_, _ = w.Write([]byte(`{"active":false}`))
			return
		}
		_, _ = w.Write([]byte(activeBody))
	}, cache, nil)

	if _, err := a.introspector.Validate(context.Background(), "tok"); err != nil {
		t.Fatalf("priming call: %v", err)
	}
	if _, ok := cache.Get("tok"); !ok {
		t.Fatal("expected the priming call to populate the cache")
	}

	revoked = true
	if res := a.Revalidate(context.Background(), "tok"); !res.ShouldDisconnect() {
		t.Fatalf("expected a revocation, got %v", res.Status)
	}

	if _, ok := cache.Get("tok"); ok {
		t.Error("a revocation seen by revalidation must drop the cached positive")
	}
	// And the HTTP path must now see it too, not in 60s.
	if _, err := a.introspector.Validate(context.Background(), "tok"); !errors.Is(err, ErrInactive) {
		t.Errorf("the HTTP path must see the revocation immediately, got %v", err)
	}
}

// A successful revalidation still refreshes the cache: a fresh positive answer
// is a fresh positive answer, and no cache hit can be older than MaxTTL
// regardless of which path wrote it.
func TestRevalidate_RefreshesTheCacheOnSuccess(t *testing.T) {
	cache := NewCache[Identity](16, nil)
	a, calls := revalidatingAuth(t, func(w http.ResponseWriter) {
		_, _ = w.Write([]byte(activeBody))
	}, cache, nil)

	if res := a.Revalidate(context.Background(), "tok"); res.Status != RevalidationValid {
		t.Fatalf("want valid, got %v", res.Status)
	}
	if _, ok := cache.Get("tok"); !ok {
		t.Fatal("a fresh positive from revalidation should populate the cache")
	}
	if _, err := a.introspector.Validate(context.Background(), "tok"); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if *calls != 1 {
		t.Errorf("the refreshed entry should serve the HTTP path; calls=%d, want 1", *calls)
	}
}

// The interval is DERIVED from the cache TTL, never chosen. A caller that picked
// its own would widen the platform's revocation budget silently.
func TestRevalidationInterval_DerivesFromTheTTLBudget(t *testing.T) {
	t.Run("defaults to the platform budget", func(t *testing.T) {
		a, _ := revalidatingAuth(t, func(w http.ResponseWriter) {
			_, _ = w.Write([]byte(activeBody))
		}, nil, nil)
		if got := a.RevalidationInterval(); got != DefaultMaxTTL {
			t.Errorf("interval = %v, want DefaultMaxTTL (%v)", got, DefaultMaxTTL)
		}
	})

	t.Run("tracks a configured MaxTTL", func(t *testing.T) {
		i, err := NewIntrospector(Config{
			Endpoint: "https://auth.example/introspect",
			Client:   http.DefaultClient,
			MaxTTL:   15 * time.Second,
		})
		if err != nil {
			t.Fatalf("NewIntrospector: %v", err)
		}
		a := newAuth(t, i, mirrorLoader)
		if got := a.introspector.MaxTTL(); got != 15*time.Second {
			t.Fatalf("MaxTTL = %v, want 15s", got)
		}
		if got := a.RevalidationInterval(); got != 15*time.Second {
			t.Errorf("interval = %v, want it to track MaxTTL (15s)", got)
		}
	})
}

// The loop takes a fresh, uncached baseline immediately: the upgrade that
// opened the connection was authenticated through the cache and may already be
// up to MaxTTL stale, so starting the first sleep from it would put the first
// honest check at 2×MaxTTL.
func TestRevalidateLoop_BaselineChecksImmediately(t *testing.T) {
	a, calls := revalidatingAuth(t, func(w http.ResponseWriter) {
		_, _ = w.Write([]byte(activeBody))
	}, NewCache[Identity](16, nil), nil)

	// A real interval: nothing but the baseline may fire during this test.
	if a.RevalidationInterval() != DefaultMaxTTL {
		t.Fatalf("precondition: interval = %v", a.RevalidationInterval())
	}

	got := make(chan RevalidationResult[testPrincipal], 1)
	a.RevalidateLoop(context.Background(), "tok", func(res RevalidationResult[testPrincipal]) bool {
		got <- res
		return false // stop after the baseline
	})

	select {
	case res := <-got:
		if res.Status != RevalidationValid {
			t.Errorf("baseline status = %v", res.Status)
		}
	default:
		t.Fatal("the loop must check once immediately, not after a full interval")
	}
	if *calls != 1 {
		t.Errorf("baseline must reach the authority exactly once; calls=%d", *calls)
	}
}

func TestRevalidateLoop_RepeatsOnTheInterval(t *testing.T) {
	a, _ := revalidatingAuth(t, func(w http.ResponseWriter) {
		_, _ = w.Write([]byte(activeBody))
	}, nil, nil)
	a.delayFn = func(time.Duration) time.Duration { return time.Millisecond }

	var seen int
	a.RevalidateLoop(context.Background(), "tok", func(res RevalidationResult[testPrincipal]) bool {
		if res.Status != RevalidationValid {
			t.Errorf("check %d: status = %v", seen, res.Status)
		}
		seen++
		return seen < 3
	})

	if seen != 3 {
		t.Errorf("loop delivered %d results, want 3", seen)
	}
}

// A revoked token does not come back, so the loop stops on its own rather than
// polling the authority forever about a session that has ended — even if the
// callback says to continue.
func TestRevalidateLoop_StopsOnRevocation(t *testing.T) {
	a, calls := revalidatingAuth(t, func(w http.ResponseWriter) {
		_, _ = w.Write([]byte(`{"active":false}`))
	}, nil, nil)
	a.delayFn = func(time.Duration) time.Duration { return time.Millisecond }

	var seen int
	a.RevalidateLoop(context.Background(), "tok", func(res RevalidationResult[testPrincipal]) bool {
		seen++
		if !res.ShouldDisconnect() {
			t.Errorf("expected a disconnect signal, got %v", res.Status)
		}
		return true // "keep going" — the loop must stop anyway
	})

	if seen != 1 || *calls != 1 {
		t.Errorf("loop must stop after a revocation; results=%d calls=%d", seen, *calls)
	}
}

// The mirror image: an outage must NOT stop the loop, or a blip would silently
// leave every surviving connection unrevalidated for the rest of its life.
func TestRevalidateLoop_KeepsGoingThroughAnOutage(t *testing.T) {
	var down atomic.Bool
	down.Store(true)
	a, _ := revalidatingAuth(t, func(w http.ResponseWriter) {
		if down.Load() {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(activeBody))
	}, nil, nil)
	a.delayFn = func(time.Duration) time.Duration { return time.Millisecond }

	var statuses []RevalidationStatus
	a.RevalidateLoop(context.Background(), "tok", func(res RevalidationResult[testPrincipal]) bool {
		statuses = append(statuses, res.Status)
		if len(statuses) == 2 {
			down.Store(false) // AOID comes back
		}
		return len(statuses) < 3
	})

	if len(statuses) != 3 {
		t.Fatalf("the loop must survive an outage, got %d checks: %v", len(statuses), statuses)
	}
	if statuses[0] != RevalidationIndeterminate || statuses[2] != RevalidationValid {
		t.Errorf("expected indeterminate → recovery, got %v", statuses)
	}
}

// The caller owns the lifecycle: cancelling the connection's context ends the
// loop. Nothing to Stop, nothing to leak.
func TestRevalidateLoop_StopsOnContextCancel(t *testing.T) {
	a, _ := revalidatingAuth(t, func(w http.ResponseWriter) {
		_, _ = w.Write([]byte(activeBody))
	}, nil, nil)
	a.delayFn = func(time.Duration) time.Duration { return time.Millisecond }

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.RevalidateLoop(ctx, "tok", func(RevalidationResult[testPrincipal]) bool { return true })
	}()

	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("cancelling the connection's context must end the loop")
	}
}

// Jitter spreads connections across the window; it may only SHORTEN a gap, so
// the worst-case revocation bound stays at exactly one interval.
func TestNextDelay_JitterNeverExceedsTheInterval(t *testing.T) {
	a, _ := revalidatingAuth(t, func(w http.ResponseWriter) {
		_, _ = w.Write([]byte(activeBody))
	}, nil, nil)

	const interval = 60 * time.Second
	var sawBelowFull bool
	for n := 0; n < 200; n++ {
		d := a.nextDelay(interval)
		if d >= interval {
			t.Fatalf("delay %v must be < the interval %v, or the budget is wrong", d, interval)
		}
		if d < interval/2 {
			t.Fatalf("delay %v is shorter than half the interval — wasteful, not unsafe, but unintended", d)
		}
		if d < interval-time.Second {
			sawBelowFull = true
		}
	}
	if !sawBelowFull {
		t.Error("expected the delay to actually vary")
	}
}

func TestRevalidationStatus_String(t *testing.T) {
	// The zero value must read as "indeterminate": a result that was never
	// filled in has learned nothing, and must not disconnect anything.
	var zero RevalidationResult[testPrincipal]
	if zero.Status != RevalidationIndeterminate {
		t.Error("the zero RevalidationStatus must be indeterminate")
	}
	if zero.ShouldDisconnect() {
		t.Error("a zero-value result must never signal a disconnect")
	}

	for status, want := range map[RevalidationStatus]string{
		RevalidationValid:         "valid",
		RevalidationRevoked:       "revoked",
		RevalidationIndeterminate: "indeterminate",
		RevalidationStatus(99):    "unknown",
	} {
		if got := status.String(); got != want {
			t.Errorf("String() = %q, want %q", got, want)
		}
	}
}

func TestRevalidate_EmptyTokenIsRevoked(t *testing.T) {
	a, calls := revalidatingAuth(t, func(w http.ResponseWriter) {
		_, _ = w.Write([]byte(activeBody))
	}, nil, nil)

	res := a.Revalidate(context.Background(), "  ")
	if !res.ShouldDisconnect() {
		t.Errorf("a connection holding no token must disconnect, got %v", res.Status)
	}
	if *calls != 0 {
		t.Errorf("an empty token must not reach the authority (got %d calls)", *calls)
	}
}

// The refresh-token rejection must stay legible in logs on this path too.
func TestRevalidate_RefreshTokenReasonStaysLegible(t *testing.T) {
	a, _ := revalidatingAuth(t, func(w http.ResponseWriter) {
		_, _ = w.Write([]byte(`{"active":true,"sub":"account-1","token_type":"refresh_token","exp":4102444800}`))
	}, nil, nil)

	res := a.Revalidate(context.Background(), "a-refresh-token")
	if !strings.Contains(res.Err.Error(), "refresh token") {
		t.Errorf("the reason must stay legible, got %q", res.Err)
	}
}
