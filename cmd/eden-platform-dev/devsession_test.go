//go:build dev

package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	connect "connectrpc.com/connect"
	platformv1 "github.com/aocybersystems/eden-platform-go/gen/go/platform/v1"
	platformv1connect "github.com/aocybersystems/eden-platform-go/gen/go/platform/v1/platformv1connect"
	"github.com/aocybersystems/eden-platform-go/platform/auth"
	"github.com/aocybersystems/eden-platform-go/platform/company"
	"github.com/aocybersystems/eden-platform-go/platform/config"
	"github.com/aocybersystems/eden-platform-go/platform/connectapi"
	"github.com/aocybersystems/eden-platform-go/platform/devstore"
	"github.com/aocybersystems/eden-platform-go/platform/rbac"
	"github.com/aocybersystems/eden-platform-go/platform/server"
)

// devSessionTestEnv wires the SAME pieces main.go's runServer wires — a real
// auth.Service (backed by a real auth.JWTManager), a real RBAC enforcer, and
// both the real auth and RBAC ConnectRPC interceptors — around a single
// RBAC-protected procedure (CompanyService.UpdateCompany, gated on
// settings:edit, exactly as defaultProcedurePermissions in main.go maps it).
// This is what "authenticate, don't exempt" is tested against: nothing here
// is a stand-in for the production wiring.
type devSessionTestEnv struct {
	ts            *httptest.Server
	companyClient platformv1connect.CompanyServiceClient
	authService   *auth.Service
	backend       *devstore.Backend
}

func setupDevSessionTestEnv(t *testing.T) *devSessionTestEnv {
	t.Helper()

	backend := devstore.NewMemoryBackend()
	seedRBACData(backend)  // roles + permissions (main.go) — owner/admin/member/viewer
	seedDevTenant(backend) // the seeded owner user this TRD authenticates

	jwtManager, err := auth.NewJWTManager(auth.JWTConfig{
		Issuer:             "eden-platform-dev-test",
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

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	return &devSessionTestEnv{
		ts:            ts,
		companyClient: platformv1connect.NewCompanyServiceClient(ts.Client(), ts.URL),
		authService:   authService,
		backend:       backend,
	}
}

// seedUnderPrivilegedUser seeds a SECOND real user, distinct from the
// playground owner, bound to the viewer role (settings:view + projects:view
// only — no settings:edit, no settings:admin) in the SAME seeded company. It
// goes through the exact same CreateUser + real Argon2id hash +
// CreateCompanyMembership path devseed.go uses for the owner — the only
// difference is which role it is bound to.
func seedUnderPrivilegedUser(t *testing.T, backend *devstore.Backend, email, password string) {
	t.Helper()
	ctx := context.Background()
	hasher := auth.NewPasswordHasher()
	hash, err := hasher.Hash(password)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	user, err := backend.AuthStore().CreateUser(ctx, email, hash, "Under-Privileged Dev User")
	if err != nil {
		t.Fatalf("create under-privileged user: %v", err)
	}
	if err := backend.AuthStore().CreateCompanyMembership(ctx, DevTenantCompanyID, user.ID, rbac.ViewerRoleID); err != nil {
		t.Fatalf("create under-privileged membership: %v", err)
	}
}

func updateCompanyRequest() *platformv1.UpdateCompanyRequest {
	return &platformv1.UpdateCompanyRequest{
		Id:          DevTenantCompanyID.String(),
		Name:        DevTenantCompanyName,
		Slug:        DevTenantCompanySlug,
		CompanyType: string(company.CompanyTypeStandalone),
	}
}

// TestMintDevSession_AcceptedByRealAuthInterceptor proves the token
// mintDevSession hands back is not a fabricated/bypass token — the REAL
// server.NewAuthInterceptor, backed by the SAME auth.JWTManager, validates
// it and lets the call reach the handler, and the REAL RBAC interceptor
// allows it because the seeded owner genuinely holds settings:edit.
func TestMintDevSession_AcceptedByRealAuthInterceptor(t *testing.T) {
	env := setupDevSessionTestEnv(t)

	session, err := mintDevSession(context.Background(), env.authService)
	if err != nil {
		t.Fatalf("mintDevSession() error = %v", err)
	}
	if session.AccessToken == "" {
		t.Fatal("mintDevSession() returned an empty access token")
	}
	if session.CompanyID != DevTenantCompanyID.String() {
		t.Fatalf("session.CompanyID = %q, want %q", session.CompanyID, DevTenantCompanyID.String())
	}

	req := connect.NewRequest(updateCompanyRequest())
	req.Header().Set("Authorization", "Bearer "+session.AccessToken)

	resp, err := env.companyClient.UpdateCompany(context.Background(), req)
	if err != nil {
		t.Fatalf("UpdateCompany() with minted token error = %v, want success (real auth+RBAC interceptors should accept the owner)", err)
	}
	if resp.Msg.Company.Name != DevTenantCompanyName {
		t.Fatalf("Company.Name = %q, want %q", resp.Msg.Company.Name, DevTenantCompanyName)
	}
}

// TestUpdateCompany_NoTokenIsUnauthenticated proves the auth interceptor is
// live, not skipped — DefaultPublicProcedures does not include
// CompanyService.UpdateCompany, so a request with no Authorization header
// must be rejected before it ever reaches RBAC or the handler.
func TestUpdateCompany_NoTokenIsUnauthenticated(t *testing.T) {
	env := setupDevSessionTestEnv(t)

	req := connect.NewRequest(updateCompanyRequest())
	_, err := env.companyClient.UpdateCompany(context.Background(), req)
	if err == nil {
		t.Fatal("UpdateCompany() with no token succeeded, want CodeUnauthenticated")
	}
	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("UpdateCompany() error code = %v, want %v", connect.CodeOf(err), connect.CodeUnauthenticated)
	}
}

// TestUpdateCompany_UnderPrivilegedUserIsDenied is the deny test: it proves
// a real, validly-authenticated principal who genuinely lacks the required
// permission is REJECTED by the real RBAC interceptor. Without this test,
// "authorization is live" would be an unfalsifiable claim — every other
// test in this file only proves the owner (who holds every permission) is
// let through, which would pass whether RBAC enforcement is real or
// silently disabled. This test seeds a second user bound to the viewer role
// (settings:view only, no settings:edit) in the same company, logs them in
// through the exact same auth.Service.Login path, and asserts the call is
// rejected with CodePermissionDenied — not CodeOK, not a fabricated pass.
func TestUpdateCompany_UnderPrivilegedUserIsDenied(t *testing.T) {
	env := setupDevSessionTestEnv(t)

	const viewerEmail = "dev-viewer@eden.local"
	const viewerPassword = "devplaygroundviewer123!"
	seedUnderPrivilegedUser(t, env.backend, viewerEmail, viewerPassword)

	resp, err := env.authService.Login(context.Background(), viewerEmail, viewerPassword)
	if err != nil {
		t.Fatalf("log in under-privileged user: %v", err)
	}
	if resp.AccessToken == "" {
		t.Fatal("Login() for under-privileged user returned an empty access token")
	}

	req := connect.NewRequest(updateCompanyRequest())
	req.Header().Set("Authorization", "Bearer "+resp.AccessToken)

	_, err = env.companyClient.UpdateCompany(context.Background(), req)
	if err == nil {
		t.Fatal("UpdateCompany() by a viewer-role user succeeded, want CodePermissionDenied — authorization is not being enforced")
	}
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("UpdateCompany() by a viewer-role user error code = %v, want %v", connect.CodeOf(err), connect.CodePermissionDenied)
	}
}

// TestGuardAgainstProductionConfig proves the dev binary refuses to start
// against config that looks production-shaped: a non-loopback DATABASE_URL
// host, or a SERVER_ADDR that binds beyond loopback (including the common
// bare ":PORT" wildcard-bind idiom).
func TestGuardAgainstProductionConfig(t *testing.T) {
	cases := []struct {
		name    string
		cfg     *config.PlatformConfig
		wantErr bool
	}{
		{
			name:    "shared config defaults are loopback-safe",
			cfg:     &config.PlatformConfig{DatabaseURL: "postgres://localhost:5432/eden_dev?sslmode=disable", ServerAddr: "127.0.0.1:8091"},
			wantErr: false,
		},
		{
			name:    "explicit loopback IP is safe",
			cfg:     &config.PlatformConfig{DatabaseURL: "postgres://127.0.0.1:5432/eden_dev", ServerAddr: "localhost:8091"},
			wantErr: false,
		},
		{
			name:    "real database host is refused",
			cfg:     &config.PlatformConfig{DatabaseURL: "postgres://prod-db.aocyber.internal:5432/eden", ServerAddr: "127.0.0.1:8091"},
			wantErr: true,
		},
		{
			name:    "wildcard bind address is refused",
			cfg:     &config.PlatformConfig{DatabaseURL: "postgres://localhost:5432/eden_dev", ServerAddr: ":8091"},
			wantErr: true,
		},
		{
			name:    "public bind address is refused",
			cfg:     &config.PlatformConfig{DatabaseURL: "postgres://localhost:5432/eden_dev", ServerAddr: "0.0.0.0:8091"},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := guardAgainstProductionConfig(tc.cfg)
			if tc.wantErr && err == nil {
				t.Fatal("guardAgainstProductionConfig() = nil, want error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("guardAgainstProductionConfig() = %v, want nil", err)
			}
		})
	}
}
