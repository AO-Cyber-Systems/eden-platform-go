package rpauth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTestIntrospector(t *testing.T, h http.HandlerFunc, cache *Cache[Identity], now func() time.Time) *Introspector {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	i, err := NewIntrospector(Config{
		Endpoint: srv.URL,
		Client:   srv.Client(),
		Cache:    cache,
		NowFn:    now,
	})
	if err != nil {
		t.Fatalf("NewIntrospector: %v", err)
	}
	return i
}

func TestValidate_ActiveToken(t *testing.T) {
	var gotForm string
	i := newTestIntrospector(t, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotForm = r.Form.Get("token")
		w.Header().Set("Content-Type", "application/json")
		// AOID's REAL response shape (internal/oauth IntrospectResp): sub is the
		// global identities.id and `tnt` carries the tenant SLUG. The previous
		// fixture asserted account_id/tenant_id/email — fields AOID has never
		// sent — which is exactly how the mismatch survived review.
		_, _ = w.Write([]byte(`{"active":true,"sub":"identity-1","tnt":"saas",
			"client_id":"aodex","scope":"openid profile","token_type":"Bearer","exp":4102444800}`))
	}, nil, nil)

	id, err := i.Validate(context.Background(), "raw-token")
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if gotForm != "raw-token" {
		t.Errorf("token not sent in form: %q", gotForm)
	}
	if id.Subject != "identity-1" {
		t.Errorf("sub must map to Subject (the global identity id): %+v", id)
	}
	if id.TenantSlug != "saas" {
		t.Errorf("`tnt` carries the tenant SLUG and must map to TenantSlug: %+v", id)
	}
	if len(id.Scopes) != 2 || id.Scopes[0] != "openid" {
		t.Errorf("scopes not split: %v", id.Scopes)
	}
}

// RFC 7662: an inactive token is HTTP 200 with active=false, NOT an error status.
// Treating it as a transport failure would fail open on a revoked token.
func TestValidate_InactiveIsRejection(t *testing.T) {
	i := newTestIntrospector(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"active":false}`))
	}, nil, nil)

	_, err := i.Validate(context.Background(), "revoked")
	if !errors.Is(err, ErrInactive) {
		t.Fatalf("want ErrInactive, got %v", err)
	}
}

// An outage must be distinguishable from a rejection, or an AOID blip reads as
// a spike in credential attacks.
func TestValidate_OutageIsNotRejection(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"5xx":          func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) },
		"garbage body": func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("<html>")) },
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			i := newTestIntrospector(t, h, nil, nil)
			_, err := i.Validate(context.Background(), "tok")
			if !errors.Is(err, ErrUnavailable) {
				t.Fatalf("want ErrUnavailable, got %v", err)
			}
			if errors.Is(err, ErrInactive) {
				t.Error("an outage must not be reported as a rejection")
			}
		})
	}
}

func TestValidate_EmptyTokenShortCircuits(t *testing.T) {
	called := false
	i := newTestIntrospector(t, func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{"active":true}`))
	}, nil, nil)

	if _, err := i.Validate(context.Background(), "   "); !errors.Is(err, ErrInactive) {
		t.Fatalf("want ErrInactive, got %v", err)
	}
	if called {
		t.Error("an empty token must not reach the authority")
	}
}

func TestValidate_CachesPositiveResult(t *testing.T) {
	calls := 0
	now := time.Now()
	cache := NewCache[Identity](16, func() time.Time { return now })

	i := newTestIntrospector(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"active":true,"sub":"acct-1"}`))
	}, cache, func() time.Time { return now })

	for n := 0; n < 3; n++ {
		if _, err := i.Validate(context.Background(), "tok"); err != nil {
			t.Fatalf("call %d: %v", n, err)
		}
	}
	if calls != 1 {
		t.Errorf("expected 1 authority call, got %d", calls)
	}
}

// A rejection must never be cached: a transient bad state would otherwise pin a
// valid session into denial for the whole TTL.
func TestValidate_DoesNotCacheRejections(t *testing.T) {
	calls := 0
	cache := NewCache[Identity](16, nil)
	i := newTestIntrospector(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"active":false}`))
	}, cache, nil)

	for n := 0; n < 3; n++ {
		_, _ = i.Validate(context.Background(), "tok")
	}
	if calls != 3 {
		t.Errorf("rejections must not be cached; got %d calls, want 3", calls)
	}
}

func TestInvalidate_ForcesRevalidation(t *testing.T) {
	calls := 0
	cache := NewCache[Identity](16, nil)
	i := newTestIntrospector(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"active":true,"sub":"acct-1"}`))
	}, cache, nil)

	_, _ = i.Validate(context.Background(), "tok")
	i.Invalidate("tok")
	_, _ = i.Validate(context.Background(), "tok")

	if calls != 2 {
		t.Errorf("expected revalidation after Invalidate, got %d calls", calls)
	}
}

// The cache must never outlive the token, and must honour a shorter max-age.
func TestComputeTTL_TakesTheSmallest(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	i := &Introspector{maxTTL: 60 * time.Second, nowFn: func() time.Time { return now }}

	tests := []struct {
		name         string
		cacheControl string
		exp          int64
		want         time.Duration
	}{
		{"no hints uses maxTTL", "", 0, 60 * time.Second},
		{"shorter max-age wins", "max-age=10", 0, 10 * time.Second},
		{"longer max-age is capped", "max-age=600", 0, 60 * time.Second},
		{"token exp wins when sooner", "", now.Unix() + 5, 5 * time.Second},
		{"already expired yields zero", "", now.Unix() - 1, 0},
		{"malformed max-age ignored", "max-age=abc", 0, 60 * time.Second},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := i.computeTTL(tc.cacheControl, tc.exp); got != tc.want {
				t.Errorf("computeTTL = %v, want %v", got, tc.want)
			}
		})
	}
}

// The cache key must be a hash — a raw bearer token sitting in a map is exactly
// what leaks in a heap dump.
func TestCache_KeysAreHashed(t *testing.T) {
	secret := "super-secret-token"
	if got := hashKey(secret); strings.Contains(got, secret) {
		t.Error("cache key contains the raw secret")
	}
	if len(hashKey(secret)) != 64 {
		t.Errorf("expected a hex sha256 key, got %d chars", len(hashKey(secret)))
	}
}

func TestCache_ExpiryAndEviction(t *testing.T) {
	now := time.Unix(1_000, 0)
	c := NewCache[Identity](2, func() time.Time { return now })

	c.Put("a", Identity{Subject: "a"}, 10*time.Second)
	if _, ok := c.Get("a"); !ok {
		t.Fatal("expected a hit")
	}

	now = now.Add(11 * time.Second)
	if _, ok := c.Get("a"); ok {
		t.Error("expected expiry")
	}

	now = time.Unix(2_000, 0)
	c.Put("x", Identity{Subject: "x"}, time.Minute)
	c.Put("y", Identity{Subject: "y"}, time.Minute)
	c.Put("z", Identity{Subject: "z"}, time.Minute) // evicts LRU ("x")
	if _, ok := c.Get("x"); ok {
		t.Error("expected the least-recently-used entry to be evicted")
	}
	if _, ok := c.Get("z"); !ok {
		t.Error("expected the newest entry to be present")
	}
}

func TestCache_NonPositiveTTLIsNotCached(t *testing.T) {
	c := NewCache[Identity](4, nil)
	c.Put("a", Identity{Subject: "a"}, 0)
	c.Put("b", Identity{Subject: "b"}, -time.Second)
	if c.Len() != 0 {
		t.Errorf("a non-positive TTL must not cache; len=%d", c.Len())
	}
}

func TestNewIntrospector_RequiresConfig(t *testing.T) {
	if _, err := NewIntrospector(Config{Client: http.DefaultClient}); err == nil {
		t.Error("expected an error for a missing Endpoint")
	}
	if _, err := NewIntrospector(Config{Endpoint: "https://x/introspect"}); err == nil {
		t.Error("expected an error for a missing Client")
	}
}

// TestValidate_RejectsRefreshToken guards the hazard that AOID's two
// introspection paths disagree about what `sub` means: on the access-token path
// it is aoid.identities.id, on the refresh-token path it is aoid.accounts.id.
// Accepting a refresh token would hand back a valid-LOOKING Identity whose
// Subject is an account id, and the relying party would join its mirror row on
// the wrong entity — silently, for only those requests.
func TestValidate_RejectsRefreshToken(t *testing.T) {
	i := newTestIntrospector(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"active":true,"sub":"account-1","tnt":"t-1",
			"token_type":"refresh_token","exp":4102444800}`))
	}, nil, nil)

	_, err := i.Validate(context.Background(), "a-refresh-token")
	if !errors.Is(err, ErrInactive) {
		t.Fatalf("a refresh token must be rejected as an invalid bearer credential, got %v", err)
	}
	if !strings.Contains(err.Error(), "refresh token") {
		t.Errorf("the reason must stay legible in logs, got %q", err.Error())
	}
}
