package entitlements

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// -----------------------------------------------------------------------------
// Middleware harness: context extractors + spy handler.
//
// Reuses the in-package fixtures from client_by_identity_test.go
// (newBootstrapResponse, entitled, newAssertingServer, newTestClient, testToken,
// requestExpectations) — declared once there, referenced here.
// -----------------------------------------------------------------------------

// staticSubject returns an extractor that always yields s (use "" for the
// empty-subject cases).
func staticSubject(s string) func(context.Context) string {
	return func(context.Context) string { return s }
}

// staticEmail returns an extractor that always yields e.
func staticEmail(e string) func(context.Context) string {
	return func(context.Context) string { return e }
}

// spyHandler is the downstream handler. It records whether it was invoked and
// captures whatever EntitlementsFromContext sees (for Inject assertions).
type spyHandler struct {
	called    bool
	bootstrap *BootstrapResponse
}

func (h *spyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.called = true
	h.bootstrap = EntitlementsFromContext(r.Context())
	w.WriteHeader(http.StatusOK)
}

func newRequestRecorder() (*http.Request, *httptest.ResponseRecorder) {
	return httptest.NewRequest(http.MethodGet, "/", nil), httptest.NewRecorder()
}

// -----------------------------------------------------------------------------
// C1-03: InjectEntitlementsByIdentity — FAIL-OPEN.
// -----------------------------------------------------------------------------

// Success: extractor yields subject → client fetches → downstream reads the
// injected bootstrap via EntitlementsFromContext; X-AOID-Email carried to biz.
func TestInjectEntitlementsByIdentity_SuccessInjectsBootstrap(t *testing.T) {
	srv, hits := newAssertingServer(t, requestExpectations{
		token:        testToken,
		subject:      "aoid-inject-1",
		emailPresent: true,
		email:        "buyer@example.com",
		response:     newBootstrapResponse(entitled("feat.reports", true)),
	})
	c := newTestClient(t, srv)

	spy := &spyHandler{}
	h := InjectEntitlementsByIdentity(c, staticSubject("aoid-inject-1"), staticEmail("buyer@example.com"))(spy)

	req, rec := newRequestRecorder()
	h.ServeHTTP(rec, req)

	if !spy.called {
		t.Fatal("downstream handler was not called")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if spy.bootstrap == nil {
		t.Fatal("EntitlementsFromContext = nil, want injected bootstrap")
	}
	if len(spy.bootstrap.Entitlements) != 1 || spy.bootstrap.Entitlements[0].FeatureKey != "feat.reports" {
		t.Fatalf("injected entitlements = %+v, want one feat.reports entry", spy.bootstrap.Entitlements)
	}
	if got := atomic.LoadInt32(hits); got != 1 {
		t.Fatalf("server hits = %d, want 1", got)
	}
}

// Empty subject: extractor yields "" → pass-through with no client call; context
// bootstrap nil; response 200.
func TestInjectEntitlementsByIdentity_EmptySubjectPassesThrough(t *testing.T) {
	srv, hits := newAssertingServer(t, requestExpectations{
		token:    testToken,
		response: newBootstrapResponse(),
	})
	c := newTestClient(t, srv)

	spy := &spyHandler{}
	h := InjectEntitlementsByIdentity(c, staticSubject(""), staticEmail(""))(spy)

	req, rec := newRequestRecorder()
	h.ServeHTTP(rec, req)

	if !spy.called {
		t.Fatal("downstream handler was not called (fail-open pass-through)")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if spy.bootstrap != nil {
		t.Fatalf("EntitlementsFromContext = %+v, want nil (no inject on empty subject)", spy.bootstrap)
	}
	if got := atomic.LoadInt32(hits); got != 0 {
		t.Fatalf("server hits = %d, want 0 (no client call on empty subject)", got)
	}
}

// Server 500: BootstrapByIdentity errors → fail-open warn+pass; context nil; 200.
func TestInjectEntitlementsByIdentity_ServerErrorFailsOpen(t *testing.T) {
	srv, hits := newAssertingServer(t, requestExpectations{
		token:   testToken,
		subject: "aoid-500",
		status:  http.StatusInternalServerError,
	})
	c := newTestClient(t, srv)

	spy := &spyHandler{}
	h := InjectEntitlementsByIdentity(c, staticSubject("aoid-500"), staticEmail(""))(spy)

	req, rec := newRequestRecorder()
	h.ServeHTTP(rec, req)

	if !spy.called {
		t.Fatal("downstream handler was not called (fail-open on server error)")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (fail-open)", rec.Code)
	}
	if spy.bootstrap != nil {
		t.Fatalf("EntitlementsFromContext = %+v, want nil (prefetch failed)", spy.bootstrap)
	}
	if got := atomic.LoadInt32(hits); got != 1 {
		t.Fatalf("server hits = %d, want 1", got)
	}
}

// -----------------------------------------------------------------------------
// C1-04: RequireEntitlementByIdentity — FAIL-CLOSED.
// -----------------------------------------------------------------------------

// Allowed: subject present + feature Allowed=true → next runs, 200.
func TestRequireEntitlementByIdentity_AllowedRunsNext(t *testing.T) {
	srv, _ := newAssertingServer(t, requestExpectations{
		token:    testToken,
		subject:  "aoid-req-allow",
		response: newBootstrapResponse(entitled("feat.pro", true)),
	})
	c := newTestClient(t, srv)

	spy := &spyHandler{}
	h := RequireEntitlementByIdentity(c, "feat.pro", staticSubject("aoid-req-allow"), staticEmail(""))(spy)

	req, rec := newRequestRecorder()
	h.ServeHTTP(rec, req)

	if !spy.called {
		t.Fatal("downstream handler was not called for allowed feature")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

// Not allowed: feature Allowed=false → 403, next NOT called.
func TestRequireEntitlementByIdentity_NotAllowedForbidden(t *testing.T) {
	srv, _ := newAssertingServer(t, requestExpectations{
		token:    testToken,
		subject:  "aoid-req-deny",
		response: newBootstrapResponse(entitled("feat.pro", false)),
	})
	c := newTestClient(t, srv)

	spy := &spyHandler{}
	h := RequireEntitlementByIdentity(c, "feat.pro", staticSubject("aoid-req-deny"), staticEmail(""))(spy)

	req, rec := newRequestRecorder()
	h.ServeHTTP(rec, req)

	if spy.called {
		t.Fatal("downstream handler was called; want blocked (403)")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

// Empty subject: extractor yields "" → 401 before any client call, next NOT called.
func TestRequireEntitlementByIdentity_EmptySubjectUnauthorized(t *testing.T) {
	srv, hits := newAssertingServer(t, requestExpectations{
		token:    testToken,
		response: newBootstrapResponse(entitled("feat.pro", true)),
	})
	c := newTestClient(t, srv)

	spy := &spyHandler{}
	h := RequireEntitlementByIdentity(c, "feat.pro", staticSubject(""), staticEmail(""))(spy)

	req, rec := newRequestRecorder()
	h.ServeHTTP(rec, req)

	if spy.called {
		t.Fatal("downstream handler was called; want 401 blocked")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if got := atomic.LoadInt32(hits); got != 0 {
		t.Fatalf("server hits = %d, want 0 (no client call on empty subject)", got)
	}
}

// Server error (500): CanUseFeatureByIdentity errors → 403 (deny-by-default), next NOT called.
func TestRequireEntitlementByIdentity_ServerErrorForbidden(t *testing.T) {
	srv, _ := newAssertingServer(t, requestExpectations{
		token:   testToken,
		subject: "aoid-req-500",
		status:  http.StatusInternalServerError,
	})
	c := newTestClient(t, srv)

	spy := &spyHandler{}
	h := RequireEntitlementByIdentity(c, "feat.pro", staticSubject("aoid-req-500"), staticEmail(""))(spy)

	req, rec := newRequestRecorder()
	h.ServeHTTP(rec, req)

	if spy.called {
		t.Fatal("downstream handler was called; want 403 blocked on server error")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (fail-closed)", rec.Code)
	}
}
