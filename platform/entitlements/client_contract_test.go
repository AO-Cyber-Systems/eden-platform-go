package entitlements_test

// client_contract_test.go — locks the CALLER half of the Eden Biz entitlements
// wire contract.
//
// The eden-biz server side is proven by biz-go integration tests (bootstrap
// target-from-request, metering caller-scoping, the service-token allowlist).
// But the prod incident that motivated #393/#396 lived at the CLIENT↔server
// boundary — a request whose path/header/query didn't line up with what the
// server allowlists and resolves. A server test alone can't catch client drift.
//
// This test stands up an httptest server that ASSERTS the exact request the
// EntitlementClient emits (method, path, query params, Bearer header, JSON body)
// and returns biz-shaped responses, proving the client both SENDS the contract
// the biz allowlist expects and PARSES the biz response shape. If someone renames
// a path (e.g. drops the /api/v1 prefix on bootstrap, or adds it to check/usage),
// or changes the auth header, or the query key, this test fails.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aocybersystems/eden-platform-go/platform/entitlements"
)

const testToken = "svc-tok-contract"

func TestClientContract_BootstrapRequestAndParse(t *testing.T) {
	var gotMethod, gotPath, gotQuery, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotQuery, gotAuth = r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(entitlements.BootstrapResponse{
			Entitlements: []entitlements.EntitlementEntry{
				{FeatureKey: "crm", FeatureType: "boolean", Allowed: true},
			},
		})
	}))
	defer srv.Close()

	c := entitlements.NewClient(srv.URL, entitlements.WithServiceToken(testToken))
	resp, err := c.Bootstrap(context.Background(), "company-123")
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	// Request contract — the exact shape the biz allowlist + handler expect.
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if gotPath != "/api/v1/entitlements/bootstrap" {
		t.Errorf("path = %q, want /api/v1/entitlements/bootstrap", gotPath)
	}
	if gotQuery != "company_id=company-123" {
		t.Errorf("query = %q, want company_id=company-123", gotQuery)
	}
	if gotAuth != "Bearer "+testToken {
		t.Errorf("auth = %q, want Bearer %s", gotAuth, testToken)
	}

	// Response parse.
	if len(resp.Entitlements) != 1 || resp.Entitlements[0].FeatureKey != "crm" || !resp.Entitlements[0].Allowed {
		t.Fatalf("parsed entitlements = %+v, want [{crm ... allowed}]", resp.Entitlements)
	}
}

func TestClientContract_CheckRequestAndParse(t *testing.T) {
	var gotPath, gotQuery, gotAuth string
	remaining := int64(42)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery, gotAuth = r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(entitlements.EntitlementResult{Allowed: true, Remaining: &remaining})
	}))
	defer srv.Close()

	c := entitlements.NewClient(srv.URL, entitlements.WithServiceToken(testToken))
	res, err := c.CheckEntitlement(context.Background(), "sub-9", "seats")
	if err != nil {
		t.Fatalf("CheckEntitlement: %v", err)
	}

	// check uses the BARE path (no /api/v1) — must match the biz allowlist key
	// "GET /entitlements/check".
	if gotPath != "/entitlements/check" {
		t.Errorf("path = %q, want /entitlements/check (no /api/v1 prefix)", gotPath)
	}
	if gotQuery != "subscription_id=sub-9&feature=seats" {
		t.Errorf("query = %q, want subscription_id=sub-9&feature=seats", gotQuery)
	}
	if gotAuth != "Bearer "+testToken {
		t.Errorf("auth = %q, want Bearer token", gotAuth)
	}
	if !res.Allowed || res.Remaining == nil || *res.Remaining != 42 {
		t.Fatalf("parsed result = %+v, want allowed + remaining 42", res)
	}
}

func TestClientContract_RecordUsageRequestAndErrors(t *testing.T) {
	var gotMethod, gotPath, gotAuth, gotCT string
	var gotBody map[string]any
	status := http.StatusCreated
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotAuth, gotCT = r.Method, r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Content-Type")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.WriteHeader(status)
	}))
	defer srv.Close()

	c := entitlements.NewClient(srv.URL, entitlements.WithServiceToken(testToken))

	// 201 → no error, with the exact request contract.
	if err := c.RecordUsage(context.Background(), "sub-9", "seats", 3); err != nil {
		t.Fatalf("RecordUsage(201): %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/entitlements/usage" {
		t.Errorf("path = %q, want /entitlements/usage (no /api/v1 prefix)", gotPath)
	}
	if gotAuth != "Bearer "+testToken {
		t.Errorf("auth = %q, want Bearer token", gotAuth)
	}
	if !strings.HasPrefix(gotCT, "application/json") {
		t.Errorf("content-type = %q, want application/json", gotCT)
	}
	if gotBody["subscription_id"] != "sub-9" || gotBody["feature_key"] != "seats" || gotBody["quantity"] != float64(3) {
		t.Fatalf("body = %+v, want {subscription_id:sub-9, feature_key:seats, quantity:3}", gotBody)
	}

	// A non-2xx must surface as an error (so callers don't silently under-meter).
	status = http.StatusForbidden
	if err := c.RecordUsage(context.Background(), "sub-9", "seats", 3); err == nil {
		t.Fatal("RecordUsage(403) returned nil error, want error")
	}
}

func TestClientContract_BootstrapErrorAndCache(t *testing.T) {
	// (a) non-2xx bootstrap → error (the prod 401-per-company symptom must not be
	// swallowed).
	errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer errSrv.Close()
	if _, err := entitlements.NewClient(errSrv.URL, entitlements.WithServiceToken(testToken)).
		Bootstrap(context.Background(), "c1"); err == nil {
		t.Fatal("Bootstrap against 401 returned nil error, want error")
	}

	// (b) success is cached — a second Bootstrap for the same company does not hit
	// the server again (this is why AODex can gate per-request cheaply).
	var hits int
	okSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_ = json.NewEncoder(w).Encode(entitlements.BootstrapResponse{
			Entitlements: []entitlements.EntitlementEntry{{FeatureKey: "crm", Allowed: true}},
		})
	}))
	defer okSrv.Close()
	c := entitlements.NewClient(okSrv.URL, entitlements.WithServiceToken(testToken))
	for i := 0; i < 3; i++ {
		if _, err := c.Bootstrap(context.Background(), "c1"); err != nil {
			t.Fatalf("Bootstrap #%d: %v", i, err)
		}
	}
	if hits != 1 {
		t.Fatalf("server hits = %d, want 1 (bootstrap cache not applied)", hits)
	}

	// CanUseFeature reads the cached bootstrap: known feature allowed, unknown denied.
	if ok, err := c.CanUseFeature(context.Background(), "c1", "crm"); err != nil || !ok {
		t.Fatalf("CanUseFeature(crm) = %v,%v; want true,nil", ok, err)
	}
	if ok, _ := c.CanUseFeature(context.Background(), "c1", "nonexistent"); ok {
		t.Fatal("CanUseFeature(nonexistent) = true, want false (deny-by-default)")
	}
}
