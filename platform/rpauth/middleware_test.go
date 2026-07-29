package rpauth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// testPrincipal stands in for a relying party's own user type — deliberately
// carrying fields AOID has never heard of, which is the reason the library
// does not define this type itself.
type testPrincipal struct {
	LocalID string
	IsAdmin bool
}

// quietLogger keeps expected WARN lines (outage, principal-load failure) out of
// the test output; the assertions are on behaviour, not on logging.
func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// introspectServer returns a handler serving a fixed introspection body, plus a
// counter of how many times it was actually called.
func introspectServer(t *testing.T, body string, status int) (*Introspector, *int) {
	t.Helper()
	calls := 0
	i := newTestIntrospector(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if status != 0 && status != http.StatusOK {
			w.WriteHeader(status)
		}
		_, _ = w.Write([]byte(body))
	}, nil, nil)
	return i, &calls
}

// AOID's real active-token response: `sub` is the global identities.id and
// `tnt` is the tenant SLUG. Nothing else is emitted.
const activeBody = `{"active":true,"sub":"identity-1","tnt":"saas",
	"client_id":"aodex","scope":"openid","token_type":"Bearer","exp":4102444800}`

func newAuth(t *testing.T, i *Introspector, load PrincipalLoader[testPrincipal], opts ...Option[testPrincipal]) *Authenticator[testPrincipal] {
	t.Helper()
	opts = append(opts, WithLogger[testPrincipal](quietLogger()))
	a, err := NewAuthenticator(i, load, opts...)
	if err != nil {
		t.Fatalf("NewAuthenticator: %v", err)
	}
	return a
}

// mirrorLoader keys the local principal on SUBJECT — the global identity id —
// which is the join key a relying party actually stores.
func mirrorLoader(_ context.Context, id *Identity) (testPrincipal, error) {
	return testPrincipal{LocalID: "local-" + id.Subject, IsAdmin: true}, nil
}

// serve runs a request through mw and returns the recorder plus whatever the
// terminal handler saw.
func serve(t *testing.T, mw func(http.Handler) http.Handler, r *http.Request, h http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	if h == nil {
		h = func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }
	}
	mw(h).ServeHTTP(rec, r)
	return rec
}

func bearerReq(token string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	return r
}

func TestRequire_AuthenticatedPassesPrincipal(t *testing.T) {
	i, _ := introspectServer(t, activeBody, http.StatusOK)
	a := newAuth(t, i, mirrorLoader)

	var got testPrincipal
	var gotIdentity *Identity
	rec := serve(t, a.Require, bearerReq("tok"), func(w http.ResponseWriter, r *http.Request) {
		got = MustPrincipal[testPrincipal](r.Context())
		gotIdentity, _ = IdentityFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if got.LocalID != "local-identity-1" || !got.IsAdmin {
		t.Errorf("principal not loaded from the app's own mirror: %+v", got)
	}
	// The Identity must be readable WITHOUT naming the principal type — audit
	// and tracing code depends on that.
	if gotIdentity == nil || gotIdentity.TenantSlug != "saas" {
		t.Errorf("identity not readable independently of the principal type: %+v", gotIdentity)
	}
}

func TestRequire_AnonymousIs401(t *testing.T) {
	i, calls := introspectServer(t, activeBody, http.StatusOK)
	a := newAuth(t, i, mirrorLoader)

	rec := serve(t, a.Require, bearerReq(""), nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
	if *calls != 0 {
		t.Errorf("a request with no token must not call the authority (got %d calls)", *calls)
	}
}

func TestRequire_InactiveIs401(t *testing.T) {
	i, _ := introspectServer(t, `{"active":false}`, http.StatusOK)
	a := newAuth(t, i, mirrorLoader)

	rec := serve(t, a.Require, bearerReq("revoked"), nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 for a revoked token, got %d", rec.Code)
	}
}

// THE LOAD-BEARING DISTINCTION. Once AOID is in the request path, an outage
// must not present as every user's credentials being rejected — that is
// indistinguishable from a credential-stuffing wave in logs and dashboards, and
// it bounces live sessions to a login screen that also cannot work.
func TestRequire_AuthorityOutageIs503Not401(t *testing.T) {
	i, _ := introspectServer(t, `upstream exploded`, http.StatusBadGateway)
	a := newAuth(t, i, mirrorLoader)

	rec := serve(t, a.Require, bearerReq("tok"), nil)
	if rec.Code == http.StatusUnauthorized {
		t.Fatal("an authority outage must NOT be reported as an authentication failure")
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", rec.Code)
	}
}

// A valid token whose local mirror row cannot be read is also 503: AOID
// explicitly said the credential was good, so telling the client to
// re-authenticate would be both wrong and useless.
func TestRequire_PrincipalLoadFailureIs503(t *testing.T) {
	i, _ := introspectServer(t, activeBody, http.StatusOK)
	a := newAuth(t, i, func(context.Context, *Identity) (testPrincipal, error) {
		return testPrincipal{}, errors.New("mirror row unreadable")
	})

	rec := serve(t, a.Require, bearerReq("tok"), nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 for a local failure on a valid token, got %d", rec.Code)
	}
}

func TestOptional_NeverBlocksAndRecordsOutcome(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		status int
		token  string
		want   Outcome
	}{
		{"anonymous", activeBody, http.StatusOK, "", OutcomeAnonymous},
		{"authenticated", activeBody, http.StatusOK, "tok", OutcomeAuthenticated},
		{"rejected", `{"active":false}`, http.StatusOK, "tok", OutcomeRejected},
		{"unavailable", `boom`, http.StatusBadGateway, "tok", OutcomeUnavailable},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			i, _ := introspectServer(t, tc.body, tc.status)
			a := newAuth(t, i, mirrorLoader)

			var got Outcome = -1
			rec := serve(t, a.Optional, bearerReq(tc.token), func(w http.ResponseWriter, r *http.Request) {
				res, ok := ResultFrom[testPrincipal](r.Context())
				if !ok {
					t.Fatal("Optional must always record a Result")
				}
				got = res.Outcome
				w.WriteHeader(http.StatusOK)
			})

			if rec.Code != http.StatusOK {
				t.Fatalf("Optional must never block: got %d", rec.Code)
			}
			if got != tc.want {
				t.Errorf("outcome = %v, want %v", got, tc.want)
			}
		})
	}
}

// PrincipalFrom must report false for every non-authenticated outcome, so a
// handler cannot accidentally act on a zero principal.
func TestPrincipalFrom_FalseUnlessAuthenticated(t *testing.T) {
	i, _ := introspectServer(t, `{"active":false}`, http.StatusOK)
	a := newAuth(t, i, mirrorLoader)

	serve(t, a.Optional, bearerReq("revoked"), func(w http.ResponseWriter, r *http.Request) {
		if _, ok := PrincipalFrom[testPrincipal](r.Context()); ok {
			t.Error("a rejected token must not yield a principal")
		}
		w.WriteHeader(http.StatusOK)
	})
}

// Browsers send OPTIONS without credentials. A 401 here would also lack CORS
// headers, blocking the real request — this guard is why.
func TestPreflightPassesThrough(t *testing.T) {
	i, calls := introspectServer(t, activeBody, http.StatusOK)
	a := newAuth(t, i, mirrorLoader)

	r := httptest.NewRequest(http.MethodOptions, "/x", nil)
	rec := serve(t, a.Require, r, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("CORS preflight must pass through Require, got %d", rec.Code)
	}
	if *calls != 0 {
		t.Errorf("preflight must not be introspected (got %d calls)", *calls)
	}
}

// A service with its own credential scheme must be able to keep AOID out of it.
// Without Excluding, every one of that service's API keys would cost a round
// trip to be told "inactive", masking a working credential scheme underneath.
func TestExcluding_DeclinesForeignCredentials(t *testing.T) {
	i, calls := introspectServer(t, activeBody, http.StatusOK)
	a := newAuth(t, i, mirrorLoader,
		WithExtractor[testPrincipal](Excluding(BearerToken, func(tok string) bool {
			return strings.HasPrefix(tok, "aodex_")
		})),
	)

	rec := serve(t, a.Optional, bearerReq("aodex_livekey123"), func(w http.ResponseWriter, r *http.Request) {
		res, _ := ResultFrom[testPrincipal](r.Context())
		if res.Outcome != OutcomeAnonymous {
			t.Errorf("a declined token must read as anonymous, not rejected: %v", res.Outcome)
		}
		w.WriteHeader(http.StatusOK)
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if *calls != 0 {
		t.Errorf("an excluded token must never be introspected (got %d calls)", *calls)
	}
}

// A browser cannot set headers on a WebSocket handshake, so header-only
// extraction would make /cable unauthenticatable.
func TestFirstToken_FallsBackToCookie(t *testing.T) {
	i, _ := introspectServer(t, activeBody, http.StatusOK)
	a := newAuth(t, i, mirrorLoader,
		WithExtractor[testPrincipal](FirstToken(BearerToken, CookieToken("aoid_session"))),
	)

	r := httptest.NewRequest(http.MethodGet, "/cable", nil) // no Authorization header
	r.AddCookie(&http.Cookie{Name: "aoid_session", Value: "tok"})

	rec := serve(t, a.Require, r, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("cookie-carried token must authenticate a WebSocket upgrade, got %d", rec.Code)
	}
}

// The composition AODex needs in Phase 3c: rpauth.Optional → the service's own
// API-key middleware → RequireAuthenticated. The service's scheme can authorise
// a request rpauth declined, and when NOTHING authenticates, the recorded
// rpauth outcome still decides 401 vs 503.
func TestRequireAuthenticated_ComposesWithAForeignScheme(t *testing.T) {
	i, _ := introspectServer(t, activeBody, http.StatusOK)
	a := newAuth(t, i, mirrorLoader,
		WithExtractor[testPrincipal](Excluding(BearerToken, func(tok string) bool {
			return strings.HasPrefix(tok, "aodex_")
		})),
	)

	// The service's own scheme: accepts its prefixed keys.
	ownScheme := func(r *http.Request) bool {
		return strings.HasPrefix(r.Header.Get("Authorization"), "Bearer aodex_")
	}
	chain := func(next http.Handler) http.Handler {
		return a.Optional(RequireAuthenticated[testPrincipal](ownScheme, nil)(next))
	}

	t.Run("foreign_scheme_authenticates", func(t *testing.T) {
		rec := serve(t, chain, bearerReq("aodex_livekey123"), nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("the service's own credential must still work: got %d", rec.Code)
		}
	})

	t.Run("nothing_authenticates_is_401", func(t *testing.T) {
		rec := serve(t, chain, bearerReq(""), nil)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("want 401, got %d", rec.Code)
		}
	})
}

// The reason RequireAuthenticated reads the recorded outcome rather than just
// "is there a principal": an AOID outage must not be reported as a credential
// problem merely because a later link in the chain also declined.
func TestRequireAuthenticated_OutagePropagatesAs503(t *testing.T) {
	i, _ := introspectServer(t, `boom`, http.StatusBadGateway)
	a := newAuth(t, i, mirrorLoader)

	chain := func(next http.Handler) http.Handler {
		return a.Optional(RequireAuthenticated[testPrincipal](
			func(*http.Request) bool { return false }, nil)(next))
	}

	rec := serve(t, chain, bearerReq("tok"), nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("an outage must survive the chain as 503, got %d", rec.Code)
	}
}

// Two services (or one service with two principal types) must not read each
// other's principal out of the same context.
func TestPrincipalKeysAreTypeScoped(t *testing.T) {
	type otherPrincipal struct{ Name string }

	i, _ := introspectServer(t, activeBody, http.StatusOK)
	a := newAuth(t, i, mirrorLoader)

	serve(t, a.Require, bearerReq("tok"), func(w http.ResponseWriter, r *http.Request) {
		if _, ok := PrincipalFrom[testPrincipal](r.Context()); !ok {
			t.Error("own principal must be readable")
		}
		if _, ok := PrincipalFrom[otherPrincipal](r.Context()); ok {
			t.Error("a different principal type must NOT resolve from the same context")
		}
		w.WriteHeader(http.StatusOK)
	})
}

// Adopting this package must not change a service's error wire format, and the
// body must stay coarse — the status says what to do, and nothing more granular
// is owed to an unauthenticated caller.
func TestDefaultErrorWriter_Shape(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   string
	}{
		{http.StatusUnauthorized, "Unauthorized"},
		{http.StatusServiceUnavailable, "Authentication temporarily unavailable"},
	} {
		rec := httptest.NewRecorder()
		DefaultErrorWriter(rec, httptest.NewRequest(http.MethodGet, "/", nil), tc.status, OutcomeRejected)

		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("content-type = %q", ct)
		}
		var body map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("body is not JSON: %v", err)
		}
		if body["error"] != tc.want {
			t.Errorf("error = %q, want %q", body["error"], tc.want)
		}
	}
}

func TestNewAuthenticator_RequiresDependencies(t *testing.T) {
	i, _ := introspectServer(t, activeBody, http.StatusOK)

	if _, err := NewAuthenticator[testPrincipal](nil, mirrorLoader); err == nil {
		t.Error("a nil Introspector must be rejected at construction")
	}
	if _, err := NewAuthenticator[testPrincipal](i, nil); err == nil {
		t.Error("a nil PrincipalLoader must be rejected at construction")
	}
}
