//go:build dev

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	connect "connectrpc.com/connect"
	platformv1connect "github.com/aocybersystems/eden-platform-go/gen/go/platform/v1/platformv1connect"
	"github.com/aocybersystems/eden-platform-go/platform/auth"
	"github.com/aocybersystems/eden-platform-go/platform/company"
	"github.com/aocybersystems/eden-platform-go/platform/config"
	"github.com/aocybersystems/eden-platform-go/platform/connectapi"
	"github.com/aocybersystems/eden-platform-go/platform/devstore"
	"github.com/aocybersystems/eden-platform-go/platform/rbac"
	"github.com/aocybersystems/eden-platform-go/platform/server"
)

// This file is TRD 04's headline deliverable and closes the one gap TRD 03
// left open (see 42-03-SUMMARY.md, "Closing TRD 02's gap"): TRD 03's
// live-server test proved the LOGIN PATH (auth.Service.Login -> real
// auth+RBAC interceptors -> real handler) works, but it never issued a
// request against the literal GET /dev/session HTTP route, because
// DevSession/startDevSession/registerDevSessionEndpoint live in
// devsession.go, outside TRD 03's file ownership. This file owns
// integration_test.go and has no such restriction, so setupIntegrationTestEnv
// below calls the REAL startDevSession entry point (the exact function
// main.go calls at boot) to mount /dev/session on a live httptest server,
// and TestIntegration_StartSeededSessionAuthenticatedOK_UnauthorizedDenied
// issues a real HTTP GET against it.
//
// GAP STATUS: CLOSED. See that test's doc comment for the full chain.

// integrationTestEnv wires the same real pieces main.go's runServer wires
// for the in-memory path — a real auth.Service backed by a real
// auth.JWTManager, a real RBAC enforcer, and the real auth + RBAC ConnectRPC
// interceptors around CompanyService — and additionally mounts the real
// GET /dev/session endpoint via startDevSession, the literal function
// main.go calls at boot. Nothing here is a hand-simulated stand-in for
// devsession.go's behavior.
type integrationTestEnv struct {
	ts            *httptest.Server
	companyClient platformv1connect.CompanyServiceClient
	authService   *auth.Service
	backend       *devstore.Backend
}

func setupIntegrationTestEnv(t *testing.T) *integrationTestEnv {
	t.Helper()

	backend := devstore.NewMemoryBackend()
	seedRBACData(backend)  // roles + permissions (main.go) — owner/admin/member/viewer
	seedDevTenant(backend) // the seeded owner user /dev/session hands out

	jwtManager, err := auth.NewJWTManager(auth.JWTConfig{
		Issuer:             "eden-platform-dev-integration-test",
		AccessTokenExpiry:  auth.DefaultJWTConfig().AccessTokenExpiry,
		RefreshTokenExpiry: auth.DefaultJWTConfig().RefreshTokenExpiry,
	})
	if err != nil {
		t.Fatalf("NewJWTManager() error = %v", err)
	}

	authService := auth.NewService(backend.AuthStore(), jwtManager, auth.NewPasswordHasher())
	companyService := company.NewService(backend.CompanyStore())
	enforcer := rbac.NewEnforcer(backend.RBACStore(), nil)

	authInterceptor := server.NewAuthInterceptor(jwtManager, server.DefaultPublicProcedures())
	rbacInterceptor := server.NewRBACInterceptor(enforcer, server.InterceptorConfig{
		PublicProcedures: server.DefaultPublicProcedures(),
		ProcedurePermissions: map[string]server.Permission{
			// Mirrors main.go's defaultProcedurePermissions exactly for the
			// one procedure this test exercises: UpdateCompany requires
			// settings:edit, which the seeded owner holds and a seeded
			// viewer does not.
			platformv1connect.CompanyServiceUpdateCompanyProcedure: {Feature: "settings", Action: "edit"},
		},
	})

	mux := http.NewServeMux()
	server.RegisterPlatformHandlers(
		mux,
		server.PlatformHandlers{
			Company: connectapi.NewCompanyHandler(companyService, nil),
		},
		connect.WithInterceptors(authInterceptor, rbacInterceptor),
	)

	// Mount the REAL /dev/session endpoint through the REAL entry point
	// main.go calls at boot. cfg deliberately uses loopback values so
	// guardAgainstProductionConfig (also exercised here, not bypassed)
	// allows startup to proceed.
	cfg := &config.PlatformConfig{
		DatabaseURL: "postgres://localhost:5432/eden_dev?sslmode=disable",
		ServerAddr:  DefaultDevServerAddr,
	}
	if err := startDevSession(cfg, authService, mux); err != nil {
		t.Fatalf("startDevSession() error = %v", err)
	}

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	return &integrationTestEnv{
		ts:            ts,
		companyClient: platformv1connect.NewCompanyServiceClient(ts.Client(), ts.URL),
		authService:   authService,
		backend:       backend,
	}
}

// fetchDevSession issues a real HTTP GET against the live server's literal
// /dev/session route and decodes the JSON body into a DevSession — the exact
// round trip a zero-interaction client performs.
func fetchDevSession(t *testing.T, ts *httptest.Server) *DevSession {
	t.Helper()

	resp, err := http.Get(ts.URL + DevSessionEndpoint)
	if err != nil {
		t.Fatalf("GET %s error = %v", DevSessionEndpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status = %d, want %d", DevSessionEndpoint, resp.StatusCode, http.StatusOK)
	}

	var session DevSession
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		t.Fatalf("decode GET %s response: %v", DevSessionEndpoint, err)
	}
	return &session
}

// TestIntegration_StartSeededSessionAuthenticatedOK_UnauthorizedDenied is
// this TRD's required must-have: start -> seeded session -> authenticated
// call succeeds -> unauthorized call is DENIED, all against one live,
// in-process server.
//
// It also closes TRD 03's flagged gap: the "seeded session" step below is a
// real net/http GET against the literal /dev/session route (not a call to
// mintDevSession as a bare Go function), served by the real
// registerDevSessionEndpoint handler mounted by the real startDevSession —
// the identical call chain main.go's boot sequence runs. The response is
// decoded into DevSession and its access_token is then used exactly as a
// zero-interaction client would use it.
//
//  1. start: setupIntegrationTestEnv stands up a live httptest.Server with
//     the real auth+RBAC interceptors and the real /dev/session route.
//  2. seeded session: fetchDevSession GETs /dev/session and decodes a
//     usable DevSession (non-empty token, correct seeded identity).
//  3. authenticated call succeeds: the owner's /dev/session token is
//     accepted by the real interceptors; UpdateCompany succeeds.
//  4. unauthorized call is DENIED: a second, genuinely under-privileged but
//     validly authenticated user (seeded viewer role, logged in through the
//     real auth.Service.Login path) is rejected by the real RBAC
//     interceptor with CodePermissionDenied — not silently allowed through.
func TestIntegration_StartSeededSessionAuthenticatedOK_UnauthorizedDenied(t *testing.T) {
	env := setupIntegrationTestEnv(t)

	// --- seeded session: real GET /dev/session against the live server ---
	session := fetchDevSession(t, env.ts)
	if session.AccessToken == "" {
		t.Fatal("GET /dev/session returned an empty access_token")
	}
	if session.Email != DevTenantEmail {
		t.Fatalf("GET /dev/session email = %q, want %q", session.Email, DevTenantEmail)
	}
	if session.UserID == "" {
		t.Fatal("GET /dev/session returned an empty user_id")
	}
	if session.CompanyID != DevTenantCompanyID.String() {
		t.Fatalf("GET /dev/session company_id = %q, want %q", session.CompanyID, DevTenantCompanyID.String())
	}

	// --- authenticated call succeeds: the /dev/session token is genuine ---
	okReq := connect.NewRequest(updateCompanyRequest())
	okReq.Header().Set("Authorization", "Bearer "+session.AccessToken)

	okResp, err := env.companyClient.UpdateCompany(context.Background(), okReq)
	if err != nil {
		t.Fatalf("UpdateCompany() with /dev/session token error = %v, want success (real auth+RBAC interceptors should accept the seeded owner)", err)
	}
	if okResp.Msg.Company.Name != DevTenantCompanyName {
		t.Fatalf("Company.Name = %q, want %q", okResp.Msg.Company.Name, DevTenantCompanyName)
	}

	// --- unauthorized call is DENIED: real RBAC rejects a real, but under-
	// privileged, principal ---
	const viewerEmail = "dev-integration-viewer@eden.local"
	const viewerPassword = "devintegrationviewer123!"
	seedUnderPrivilegedUser(t, env.backend, viewerEmail, viewerPassword)

	viewerLogin, err := env.authService.Login(context.Background(), viewerEmail, viewerPassword)
	if err != nil {
		t.Fatalf("log in under-privileged user: %v", err)
	}
	if viewerLogin.AccessToken == "" {
		t.Fatal("Login() for under-privileged user returned an empty access token")
	}

	deniedReq := connect.NewRequest(updateCompanyRequest())
	deniedReq.Header().Set("Authorization", "Bearer "+viewerLogin.AccessToken)

	_, err = env.companyClient.UpdateCompany(context.Background(), deniedReq)
	if err == nil {
		t.Fatal("UpdateCompany() by an under-privileged, but authenticated, user succeeded, want CodePermissionDenied — authorization is not being enforced")
	}
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("UpdateCompany() by an under-privileged user error code = %v, want %v", connect.CodeOf(err), connect.CodePermissionDenied)
	}
}

// TestIntegration_DevSessionEndpoint_StableAcrossRepeatedRequests proves
// GET /dev/session serves the SAME pre-minted session on every hit within
// one running process, rather than minting a fresh user/token per request —
// registerDevSessionEndpoint closes over a single session captured at boot
// (see devsession.go), and this is the behavior a client relying on a
// stable identity across multiple requests depends on.
func TestIntegration_DevSessionEndpoint_StableAcrossRepeatedRequests(t *testing.T) {
	env := setupIntegrationTestEnv(t)

	first := fetchDevSession(t, env.ts)
	second := fetchDevSession(t, env.ts)

	if first.AccessToken != second.AccessToken {
		t.Fatalf("GET /dev/session returned different access tokens across repeated requests: %q vs %q", first.AccessToken, second.AccessToken)
	}
	if first.UserID != second.UserID {
		t.Fatalf("GET /dev/session returned different user ids across repeated requests: %q vs %q", first.UserID, second.UserID)
	}
}
