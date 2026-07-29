package rpauth_test

// example_aodex_test.go — the Phase 3c adoption, written out against AODex's
// ACTUAL current middleware so the seam is demonstrated rather than asserted.
//
// This compiles. That is the point: the objective's gate for the shared library
// is "a second consumer compiling against it, not an assertion that it is
// reusable", and this is the first consumer's real shape — CurrentUser copied
// field-for-field from aodex-go/internal/middleware/auth.go, including the
// fields AOID has never heard of, and the API-key path that has to keep working
// through the cutover.
//
// It also doubles as the adoption recipe: what AODex's RequireAuth becomes.

import (
	"context"
	"crypto/tls"
	"net/http"
	"strings"
	"time"

	"github.com/aocybersystems/eden-platform-go/platform/rpauth"
)

// CurrentUser is AODex's existing principal, verbatim. Of its eight fields
// exactly two — Email, and the AOID subject — come from AOID. The local row id,
// display name, admin flag, must-change-password flag and API-key scopes are
// application data. That ratio is why rpauth does not define this type.
type CurrentUser struct {
	ID                 string
	Email              string
	Name               string
	IsAdmin            bool
	APIKeyID           *string
	APIKeyScopes       []string
	PasswordMustChange bool
	AOIDSubject        string
}

// userStore stands in for AODex's users repository.
// userStore is AODex's users repository, keyed on the AOID SUBJECT — the global
// identities.id, stored locally as users.aoid_subject. Keying on the account
// would be the wrong axis: an account is per-tenant, so someone in two
// workspaces has two accounts but one identity, while AODex keeps one row per
// person.
type userStore interface {
	ByAOIDSubject(ctx context.Context, subject string) (CurrentUser, error)
}

// ExampleAuthenticator shows the whole Phase 3c wiring: introspection over
// mTLS, the mirror-row loader, and the middleware chain that keeps AODex's own
// API keys working while AOID owns sessions.
func ExampleAuthenticator() {
	var (
		users     userStore
		clientTLS *tls.Certificate // AOEdge already does this; AODex copies the pattern
	)
	_ = clientTLS

	// 1. Introspection client. AOID is mTLS-only, so this carries a client cert.
	introspector, err := rpauth.NewIntrospector(rpauth.Config{
		Endpoint: "https://auth.aocyber.ai/oauth/introspect",
		Client: &http.Client{
			Timeout:   5 * time.Second,
			Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13}},
		},
		Cache: rpauth.NewCache[rpauth.Identity](10_000, nil),
		// MaxTTL left at DefaultMaxTTL. THIS IS THE REVOCATION LAG BUDGET:
		// revoking in AOID ends an AODex session within this window, and the
		// /cable hub's revalidation interval must be chosen against the same
		// number.
	})
	if err != nil {
		panic(err)
	}

	// 2. The mapping. AODex keeps a local users row keyed by the AOID account
	//    id — forced by the data model, since conversations, tiers, credits and
	//    quotas all FK to it — so the loader is a mirror-row read, keyed by the
	//    AOID subject that signup stored on the row.
	loadUser := func(ctx context.Context, id *rpauth.Identity) (*CurrentUser, error) {
		u, err := users.ByAOIDSubject(ctx, id.Subject)
		if err != nil {
			return nil, err
		}
		u.AOIDSubject = id.Subject
		// Email comes from the mirror row, NOT the token. Deriving it from the
		// authority would have meant adding a lookup to AOID's hottest endpoint
		// for a value AODex already holds.
		return &u, nil
	}

	// 3. AODex's own API keys arrive as `Authorization: Bearer aodex_...` and
	//    must never be introspected — AOID cannot know them, so every one would
	//    cost a round trip to be told "inactive".
	extractor := rpauth.Excluding(
		rpauth.FirstToken(
			rpauth.BearerToken,
			// /cable is a browser WebSocket upgrade, which cannot carry an
			// Authorization header.
			rpauth.CookieToken("aoid_session"),
		),
		func(tok string) bool { return strings.HasPrefix(tok, "aodex_") },
	)

	auth, err := rpauth.NewAuthenticator(introspector, loadUser,
		rpauth.WithExtractor[*CurrentUser](extractor),
	)
	if err != nil {
		panic(err)
	}

	// 4a. Routes that only accept an AOID session: RequireAuth's replacement.
	_ = auth.Require

	// 4b. Routes that must also accept an AODex API key. rpauth annotates but
	//     never blocks; the service's own scheme runs next; the closing gate
	//     enforces. Because the gate reads the recorded rpauth Outcome, an AOID
	//     outage still surfaces as 503 rather than 401 even though the API-key
	//     middleware also declined.
	apiKeyAuth := func(next http.Handler) http.Handler { return next } // AODex's existing middleware
	hasAPIKeyPrincipal := func(r *http.Request) bool {
		u, ok := rpauth.PrincipalFrom[*CurrentUser](r.Context())
		return ok && u.APIKeyID != nil
	}

	_ = func(next http.Handler) http.Handler {
		return auth.Optional(
			apiKeyAuth(
				rpauth.RequireAuthenticated[*CurrentUser](hasAPIKeyPrincipal, nil)(next),
			),
		)
	}

	// 5. Handlers read the principal exactly as MustGetCurrentUser does today.
	_ = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := rpauth.MustPrincipal[*CurrentUser](r.Context())
		_ = user.ID
	})

	// Output:
}

// cableConn stands in for one connection in AODex's /cable hub.
type cableConn struct {
	token string       // the AOID token the upgrade authenticated with
	user  *CurrentUser // refreshed on every successful revalidation
}

func (c *cableConn) close(string) {}

// ExampleAuthenticator_Revalidate is the /cable half of the adoption: the
// piece without which revoking a session in AOID would close every future HTTP
// request and none of the WebSockets already streaming.
//
// It has no "Output:" comment on purpose — the wiring above is nil, so this
// example is COMPILED but not run. Compiling is the gate that matters: it is
// what proves the API is usable from the shape /cable actually has.
func ExampleAuthenticator_Revalidate() {
	var (
		auth *rpauth.Authenticator[*CurrentUser] // built exactly as above
		conn *cableConn
		ctx  context.Context // scoped to the connection, cancelled on close
	)

	// The interval is NOT chosen here. It comes from the same Introspector the
	// HTTP middleware uses, so /cable cannot widen the platform's revocation
	// budget by picking its own number. With the default 60s cache TTL the
	// worst-case lag for a live socket is 60s + one introspection round trip:
	// the loop bypasses the cache and takes a fresh baseline on entry, so the
	// TTL does not stack on top of the interval.
	_ = auth.RevalidationInterval()

	// The loop blocks, so the hub decides where it runs and cancels it with the
	// connection. Nothing to Stop, nothing to leak.
	go auth.RevalidateLoop(ctx, conn.token, func(res rpauth.RevalidationResult[*CurrentUser]) bool {
		// ShouldDisconnect is TRUE ONLY for a definitive negative. An AOID
		// outage lands here as RevalidationIndeterminate, and the socket stays
		// up — bouncing every connection on the platform because the authority
		// blipped would be a self-inflicted outage, and every client would
		// reconnect into an authority that is still down.
		if res.ShouldDisconnect() {
			conn.close("session revoked")
			return false
		}
		if res.Status == rpauth.RevalidationValid {
			// The principal is reloaded on every check, so a tier change or a
			// demotion takes effect without a reconnect.
			conn.user = res.Principal
		}
		return true
	})
}
