package entitlements

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// testToken is the literal service token used across the by-identity contract
// tests. It is a fixture value, never a real secret.
const testToken = "svc-token"

// -----------------------------------------------------------------------------
// Hand-built fixtures (no LLM-generated JSON blobs).
// -----------------------------------------------------------------------------

// newBootstrapResponse builds a BootstrapResponse with the given entitlement
// entries plus a minimal Subscription/Plan. This is the fixture generator that
// every by-identity test uses instead of literal JSON.
func newBootstrapResponse(entries ...EntitlementEntry) *BootstrapResponse {
	return &BootstrapResponse{
		Subscription: &Subscription{
			ID:     "sub_test",
			PlanID: "plan_test",
			Status: "active",
		},
		Plan: &Plan{
			ID:       "plan_test",
			Name:     "Test Plan",
			Interval: "month",
			Amount:   0,
			Currency: "usd",
		},
		Entitlements: entries,
	}
}

// entitled builds a single boolean EntitlementEntry.
func entitled(featureKey string, allowed bool) EntitlementEntry {
	return EntitlementEntry{
		FeatureKey:  featureKey,
		FeatureType: "boolean",
		Allowed:     allowed,
	}
}

// -----------------------------------------------------------------------------
// Asserting httptest harness.
// -----------------------------------------------------------------------------

// requestExpectations describes what the asserting server should require of each
// inbound request and what it should respond with.
type requestExpectations struct {
	token        string             // expected bearer token
	subject      string             // if non-empty, assert aoid_subject query == subject
	emailPresent bool               // whether X-AOID-Email must be present
	email        string             // expected X-AOID-Email value when present
	status       int                // response status; 0 => 200
	response     *BootstrapResponse // body to encode when status < 300
	// responseFn overrides response and is computed per-request (used for
	// subject-isolation, where each subject must get a distinct body).
	responseFn func(r *http.Request) *BootstrapResponse
}

// newAssertingServer returns an httptest.Server whose handler asserts the
// entitlements-bootstrap wire contract and increments an atomic hit counter.
// The returned *int32 lets tests assert exact server-hit counts (cache proofs).
func newAssertingServer(t *testing.T, want requestExpectations) (*httptest.Server, *int32) {
	t.Helper()
	var hits int32
	status := want.status
	if status == 0 {
		status = http.StatusOK
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)

		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want GET", r.Method)
		}
		if r.URL.Path != "/api/v1/entitlements/bootstrap" {
			t.Errorf("path = %q, want /api/v1/entitlements/bootstrap", r.URL.Path)
		}
		if want.subject != "" {
			if got := r.URL.Query().Get("aoid_subject"); got != want.subject {
				t.Errorf("aoid_subject = %q, want %q", got, want.subject)
			}
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+want.token {
			t.Errorf("Authorization = %q, want %q", got, "Bearer "+want.token)
		}

		// Canonical MIME key for X-AOID-Email is "X-Aoid-Email".
		_, hasEmail := r.Header["X-Aoid-Email"]
		if hasEmail != want.emailPresent {
			t.Errorf("X-AOID-Email present = %v, want %v", hasEmail, want.emailPresent)
		}
		if want.emailPresent {
			if got := r.Header.Get("X-AOID-Email"); got != want.email {
				t.Errorf("X-AOID-Email = %q, want %q", got, want.email)
			}
		}

		if status >= 300 {
			w.WriteHeader(status)
			return
		}

		resp := want.response
		if want.responseFn != nil {
			resp = want.responseFn(r)
		}
		if resp == nil {
			resp = newBootstrapResponse()
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// newTestClient builds an EntitlementClient pointed at the test server, using
// the fixture service token and the server's own HTTP client.
func newTestClient(t *testing.T, srv *httptest.Server) *EntitlementClient {
	t.Helper()
	return NewClient(srv.URL, WithServiceToken(testToken), WithHTTPClient(srv.Client()))
}

// -----------------------------------------------------------------------------
// C1-01: BootstrapByIdentity wire contract + parse + cache.
// -----------------------------------------------------------------------------

func TestBootstrapByIdentity_WireContractWithEmail(t *testing.T) {
	srv, hits := newAssertingServer(t, requestExpectations{
		token:        testToken,
		subject:      "aoid-sub-123",
		emailPresent: true,
		email:        "buyer@example.com",
		response:     newBootstrapResponse(entitled("feat.reports", true)),
	})
	c := newTestClient(t, srv)

	resp, err := c.BootstrapByIdentity(context.Background(), "aoid-sub-123", "buyer@example.com")
	if err != nil {
		t.Fatalf("BootstrapByIdentity: %v", err)
	}
	if len(resp.Entitlements) != 1 || resp.Entitlements[0].FeatureKey != "feat.reports" {
		t.Fatalf("parsed entitlements = %+v, want one feat.reports entry", resp.Entitlements)
	}
	if resp.Subscription == nil || resp.Subscription.ID != "sub_test" {
		t.Fatalf("subscription = %+v, want parsed sub_test", resp.Subscription)
	}
	if got := atomic.LoadInt32(hits); got != 1 {
		t.Fatalf("server hits = %d, want 1", got)
	}
}

func TestBootstrapByIdentity_EmailHeaderAbsentWhenEmpty(t *testing.T) {
	srv, _ := newAssertingServer(t, requestExpectations{
		token:        testToken,
		subject:      "aoid-sub-noemail",
		emailPresent: false, // handler asserts header entirely absent
		response:     newBootstrapResponse(),
	})
	c := newTestClient(t, srv)

	if _, err := c.BootstrapByIdentity(context.Background(), "aoid-sub-noemail", ""); err != nil {
		t.Fatalf("BootstrapByIdentity: %v", err)
	}
}

func TestBootstrapByIdentity_SubjectCacheSingleHit(t *testing.T) {
	srv, hits := newAssertingServer(t, requestExpectations{
		token:    testToken,
		subject:  "cached-sub",
		response: newBootstrapResponse(entitled("feat.x", true)),
	})
	c := newTestClient(t, srv)

	for i := 0; i < 3; i++ {
		if _, err := c.BootstrapByIdentity(context.Background(), "cached-sub", ""); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if got := atomic.LoadInt32(hits); got != 1 {
		t.Fatalf("server hits = %d, want 1 (subject cache)", got)
	}
}

func TestBootstrapByIdentity_SubjectIsolation(t *testing.T) {
	srv, hits := newAssertingServer(t, requestExpectations{
		token: testToken,
		// subject unset: two subjects flow through the same handler.
		responseFn: func(r *http.Request) *BootstrapResponse {
			s := r.URL.Query().Get("aoid_subject")
			return newBootstrapResponse(entitled("feat."+s, true))
		},
	})
	c := newTestClient(t, srv)

	a, err := c.BootstrapByIdentity(context.Background(), "sub-a", "")
	if err != nil {
		t.Fatalf("sub-a: %v", err)
	}
	b, err := c.BootstrapByIdentity(context.Background(), "sub-b", "")
	if err != nil {
		t.Fatalf("sub-b: %v", err)
	}
	if a.Entitlements[0].FeatureKey != "feat.sub-a" {
		t.Fatalf("sub-a entitlement = %q, want feat.sub-a (cross-bleed)", a.Entitlements[0].FeatureKey)
	}
	if b.Entitlements[0].FeatureKey != "feat.sub-b" {
		t.Fatalf("sub-b entitlement = %q, want feat.sub-b (cross-bleed)", b.Entitlements[0].FeatureKey)
	}
	if got := atomic.LoadInt32(hits); got != 2 {
		t.Fatalf("server hits = %d, want 2 (distinct subjects)", got)
	}
}

// TestBootstrapByIdentity_DistinctFromCompanyCache proves the by-identity cache
// does NOT share entries with the companyID bootstrapCache: a Bootstrap call and
// a BootstrapByIdentity call using the SAME string must both reach the server.
func TestBootstrapByIdentity_DistinctFromCompanyCache(t *testing.T) {
	srv, hits := newAssertingServer(t, requestExpectations{
		token: testToken,
		// subject unset: company_id and aoid_subject requests share the path.
		response: newBootstrapResponse(entitled("feat.x", true)),
	})
	c := newTestClient(t, srv)

	const collide = "collide-key"
	if _, err := c.Bootstrap(context.Background(), collide); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if _, err := c.BootstrapByIdentity(context.Background(), collide, ""); err != nil {
		t.Fatalf("BootstrapByIdentity: %v", err)
	}
	if got := atomic.LoadInt32(hits); got != 2 {
		t.Fatalf("server hits = %d, want 2 (caches must be distinct)", got)
	}
}

func TestBootstrapByIdentity_Non2xxReturnsErrorNothingCached(t *testing.T) {
	srv, hits := newAssertingServer(t, requestExpectations{
		token:   testToken,
		subject: "unauthorized-sub",
		status:  http.StatusUnauthorized,
	})
	c := newTestClient(t, srv)

	if _, err := c.BootstrapByIdentity(context.Background(), "unauthorized-sub", ""); err == nil {
		t.Fatal("BootstrapByIdentity: want error on 401, got nil")
	}
	// Nothing cached on error -> a second call re-hits the server.
	if _, err := c.BootstrapByIdentity(context.Background(), "unauthorized-sub", ""); err == nil {
		t.Fatal("BootstrapByIdentity (2nd): want error on 401, got nil")
	}
	if got := atomic.LoadInt32(hits); got != 2 {
		t.Fatalf("server hits = %d, want 2 (errors must not cache)", got)
	}
}

func TestInvalidateIdentity_ForcesRefetch(t *testing.T) {
	srv, hits := newAssertingServer(t, requestExpectations{
		token:    testToken,
		subject:  "evict-sub",
		response: newBootstrapResponse(entitled("feat.x", true)),
	})
	c := newTestClient(t, srv)

	if _, err := c.BootstrapByIdentity(context.Background(), "evict-sub", ""); err != nil {
		t.Fatalf("first call: %v", err)
	}
	c.InvalidateIdentity("evict-sub")
	if _, err := c.BootstrapByIdentity(context.Background(), "evict-sub", ""); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if got := atomic.LoadInt32(hits); got != 2 {
		t.Fatalf("server hits = %d, want 2 (InvalidateIdentity must force re-fetch)", got)
	}
}

// -----------------------------------------------------------------------------
// C1-02: CanUseFeatureByIdentity deny-by-default.
// -----------------------------------------------------------------------------

func TestCanUseFeatureByIdentity_AllowedTrue(t *testing.T) {
	srv, _ := newAssertingServer(t, requestExpectations{
		token:    testToken,
		subject:  "sub-allow",
		response: newBootstrapResponse(entitled("feat.pro", true)),
	})
	c := newTestClient(t, srv)

	ok, err := c.CanUseFeatureByIdentity(context.Background(), "sub-allow", "", "feat.pro")
	if err != nil {
		t.Fatalf("CanUseFeatureByIdentity: %v", err)
	}
	if !ok {
		t.Fatal("CanUseFeatureByIdentity = false, want true for allowed feature")
	}
}

func TestCanUseFeatureByIdentity_AllowedFalse(t *testing.T) {
	srv, _ := newAssertingServer(t, requestExpectations{
		token:    testToken,
		subject:  "sub-deny",
		response: newBootstrapResponse(entitled("feat.pro", false)),
	})
	c := newTestClient(t, srv)

	ok, err := c.CanUseFeatureByIdentity(context.Background(), "sub-deny", "", "feat.pro")
	if err != nil {
		t.Fatalf("CanUseFeatureByIdentity: %v", err)
	}
	if ok {
		t.Fatal("CanUseFeatureByIdentity = true, want false for disallowed feature")
	}
}

func TestCanUseFeatureByIdentity_UnknownFeatureDenyByDefault(t *testing.T) {
	srv, _ := newAssertingServer(t, requestExpectations{
		token:    testToken,
		subject:  "sub-unknown",
		response: newBootstrapResponse(entitled("feat.other", true)),
	})
	c := newTestClient(t, srv)

	ok, err := c.CanUseFeatureByIdentity(context.Background(), "sub-unknown", "", "feat.missing")
	if err != nil {
		t.Fatalf("CanUseFeatureByIdentity: %v", err)
	}
	if ok {
		t.Fatal("CanUseFeatureByIdentity = true, want false (deny-by-default) for unknown feature")
	}
}

func TestCanUseFeatureByIdentity_BootstrapErrorReturnsError(t *testing.T) {
	srv, _ := newAssertingServer(t, requestExpectations{
		token:   testToken,
		subject: "sub-500",
		status:  http.StatusInternalServerError,
	})
	c := newTestClient(t, srv)

	ok, err := c.CanUseFeatureByIdentity(context.Background(), "sub-500", "", "feat.x")
	if err == nil {
		t.Fatal("CanUseFeatureByIdentity: want error on server 500, got nil")
	}
	if ok {
		t.Fatal("CanUseFeatureByIdentity = true on error, want false")
	}
}

// -----------------------------------------------------------------------------
// Regression guard: the companyID Bootstrap path is unchanged.
// -----------------------------------------------------------------------------

func TestBootstrap_CompanyIDPathUnchanged(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		if r.URL.Path != "/api/v1/entitlements/bootstrap" {
			t.Errorf("path = %q, want /api/v1/entitlements/bootstrap", r.URL.Path)
		}
		if got := r.URL.Query().Get("company_id"); got != "comp-1" {
			t.Errorf("company_id = %q, want comp-1", got)
		}
		// companyID path must NOT emit the by-identity email header.
		if _, ok := r.Header["X-Aoid-Email"]; ok {
			t.Errorf("companyID path emitted X-AOID-Email; want absent")
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+testToken {
			t.Errorf("Authorization = %q, want Bearer %s", got, testToken)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(newBootstrapResponse(entitled("feat.x", true)))
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv)

	// Two calls for the same companyID -> cached by companyID -> exactly one hit.
	for i := 0; i < 2; i++ {
		if _, err := c.Bootstrap(context.Background(), "comp-1"); err != nil {
			t.Fatalf("Bootstrap call %d: %v", i, err)
		}
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("server hits = %d, want 1 (companyID cache unchanged)", got)
	}
}
