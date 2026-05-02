package connectapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	connect "connectrpc.com/connect"
	platformv1 "github.com/aocybersystems/eden-platform-go/gen/go/platform/v1"
	platformv1connect "github.com/aocybersystems/eden-platform-go/gen/go/platform/v1/platformv1connect"
	"github.com/aocybersystems/eden-platform-go/platform/auth"
	"github.com/aocybersystems/eden-platform-go/platform/company"
	"github.com/aocybersystems/eden-platform-go/platform/devstore"
	"github.com/aocybersystems/eden-platform-go/platform/rbac"
	platformregistry "github.com/aocybersystems/eden-platform-go/platform/registry"
	"github.com/aocybersystems/eden-platform-go/platform/server"
)

type testEnv struct {
	ts             *httptest.Server
	authClient     platformv1connect.AuthServiceClient
	companyClient  platformv1connect.CompanyServiceClient
	registryClient platformv1connect.RegistryServiceClient
	rbacClient     platformv1connect.RBACServiceClient
	backend        *devstore.Backend
}

func setupTestEnv(t *testing.T) *testEnv {
	t.Helper()
	backend := devstore.NewMemoryBackend()
	authStore := backend.AuthStore()
	companyStore := backend.CompanyStore()
	rbacStore := backend.RBACStore()

	// Seed system RBAC roles
	backend.SeedRBACRole(rbac.Role{ID: rbac.OwnerRoleID, Name: "owner", Level: rbac.RoleLevelOwner, IsSystem: true})
	backend.SeedRBACRole(rbac.Role{ID: rbac.AdminRoleID, Name: "admin", Level: rbac.RoleLevelAdmin, IsSystem: true})
	backend.SeedRBACRole(rbac.Role{ID: rbac.MemberRoleID, Name: "member", Level: rbac.RoleLevelMember, IsSystem: true})
	backend.SeedRBACRole(rbac.Role{ID: rbac.ViewerRoleID, Name: "viewer", Level: rbac.RoleLevelViewer, IsSystem: true})

	reg := platformregistry.New()
	reg.Register(&platformregistry.ModuleRegistration{
		Name: "home",
		NavItems: []platformregistry.NavItem{
			{ID: "home", Label: "Home", Icon: "home", Path: "/home", Feature: "home", Priority: 0},
		},
		BadgeProvider: func(companyID, userID string) int { return 1 },
	})

	jwtManager, err := auth.NewJWTManager(auth.JWTConfig{
		Issuer:             "eden-platform-test",
		AccessTokenExpiry:  auth.DefaultJWTConfig().AccessTokenExpiry,
		RefreshTokenExpiry: auth.DefaultJWTConfig().RefreshTokenExpiry,
	})
	if err != nil {
		t.Fatalf("NewJWTManager() error = %v", err)
	}

	authSvc := auth.NewService(authStore, jwtManager, auth.NewPasswordHasher())
	companySvc := company.NewService(companyStore)
	enforcer := rbac.NewEnforcer(rbacStore, nil)
	resolver := rbac.NewHierarchyResolver(rbacStore)
	rbacSvc := rbac.NewService(rbacStore, enforcer, resolver)

	mux := http.NewServeMux()
	server.RegisterPlatformHandlers(
		mux,
		server.PlatformHandlers{
			Auth:     NewAuthHandler(authSvc, auth.NewSSOService(authStore, jwtManager, "http://localhost:0")),
			Company:  NewCompanyHandler(companySvc, companyStore),
			Registry: NewRegistryHandler(reg, companyStore),
			RBAC:     NewRBACHandler(rbacSvc, enforcer, resolver),
		},
		connect.WithInterceptors(server.NewAuthInterceptor(jwtManager, server.DefaultPublicProcedures())),
	)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	return &testEnv{
		ts:             ts,
		authClient:     platformv1connect.NewAuthServiceClient(ts.Client(), ts.URL),
		companyClient:  platformv1connect.NewCompanyServiceClient(ts.Client(), ts.URL),
		registryClient: platformv1connect.NewRegistryServiceClient(ts.Client(), ts.URL),
		rbacClient:     platformv1connect.NewRBACServiceClient(ts.Client(), ts.URL),
		backend:        backend,
	}
}

func signUp(t *testing.T, env *testEnv, email, password, displayName string) *platformv1.AuthData {
	t.Helper()
	resp, err := env.authClient.SignUp(
		t.Context(),
		connect.NewRequest(&platformv1.SignUpRequest{
			Email:       email,
			Password:    password,
			DisplayName: displayName,
		}),
	)
	if err != nil {
		t.Fatalf("SignUp(%s) error = %v", email, err)
	}
	return resp.Msg.GetAuth()
}

func authedRequest[T any](t *testing.T, token string, msg *T) *connect.Request[T] {
	t.Helper()
	req := connect.NewRequest(msg)
	req.Header().Set("Authorization", "Bearer "+token)
	return req
}

func TestGeneratedClientsWorkAgainstPlatformHandlers(t *testing.T) {
	env := setupTestEnv(t)

	signUpResp := signUp(t, env, "dev@example.com", "password123", "Dev User")

	if signUpResp.GetAccessToken() == "" {
		t.Fatalf("expected access token in signup response")
	}

	companyResp, err := env.companyClient.ListCompanies(
		t.Context(),
		authedRequest(t, signUpResp.GetAccessToken(), &platformv1.ListCompaniesRequest{}),
	)
	if err != nil {
		t.Fatalf("ListCompanies() error = %v", err)
	}
	if len(companyResp.Msg.GetCompanies()) != 1 {
		t.Fatalf("expected one company, got %d", len(companyResp.Msg.GetCompanies()))
	}

	navResp, err := env.registryClient.GetNavItems(
		t.Context(),
		authedRequest(t, signUpResp.GetAccessToken(), &platformv1.GetNavItemsRequest{
			CompanyId: companyResp.Msg.GetCompanies()[0].GetId(),
		}),
	)
	if err != nil {
		t.Fatalf("GetNavItems() error = %v", err)
	}
	if len(navResp.Msg.GetItems()) != 1 {
		t.Fatalf("expected one nav item, got %d", len(navResp.Msg.GetItems()))
	}
}

func TestAuthLifecycle(t *testing.T) {
	env := setupTestEnv(t)

	t.Run("SignUp", func(t *testing.T) {
		resp := signUp(t, env, "lifecycle@example.com", "password123", "Lifecycle User")
		if resp.GetAccessToken() == "" {
			t.Errorf("SignUp: AccessToken is empty")
		}
		if resp.GetRefreshToken() == "" {
			t.Errorf("SignUp: RefreshToken is empty")
		}
	})

	t.Run("Login", func(t *testing.T) {
		resp, err := env.authClient.Login(t.Context(), connect.NewRequest(&platformv1.LoginRequest{
			Email:    "lifecycle@example.com",
			Password: "password123",
		}))
		if err != nil {
			t.Fatalf("Login() error = %v", err)
		}
		if resp.Msg.GetAuth().GetAccessToken() == "" {
			t.Errorf("Login: AccessToken is empty")
		}

		t.Run("RefreshToken", func(t *testing.T) {
			refreshResp, err := env.authClient.RefreshToken(t.Context(), connect.NewRequest(&platformv1.RefreshTokenRequest{
				RefreshToken: resp.Msg.GetAuth().GetRefreshToken(),
			}))
			if err != nil {
				t.Fatalf("RefreshToken() error = %v", err)
			}
			if refreshResp.Msg.GetAuth().GetAccessToken() == "" {
				t.Errorf("RefreshToken: AccessToken is empty")
			}
			if refreshResp.Msg.GetAuth().GetRefreshToken() == resp.Msg.GetAuth().GetRefreshToken() {
				t.Errorf("RefreshToken: tokens should rotate")
			}

			t.Run("Logout", func(t *testing.T) {
				_, err := env.authClient.Logout(t.Context(), connect.NewRequest(&platformv1.LogoutRequest{
					RefreshToken: refreshResp.Msg.GetAuth().GetRefreshToken(),
				}))
				if err != nil {
					t.Fatalf("Logout() error = %v", err)
				}

				// Refreshing again should fail
				_, err = env.authClient.RefreshToken(t.Context(), connect.NewRequest(&platformv1.RefreshTokenRequest{
					RefreshToken: refreshResp.Msg.GetAuth().GetRefreshToken(),
				}))
				if err == nil {
					t.Errorf("RefreshToken after logout should fail")
				}
			})
		})
	})

	t.Run("LoginWrongPassword", func(t *testing.T) {
		_, err := env.authClient.Login(t.Context(), connect.NewRequest(&platformv1.LoginRequest{
			Email:    "lifecycle@example.com",
			Password: "wrongpassword",
		}))
		if err == nil {
			t.Errorf("Login with wrong password should fail")
		}
	})
}

func TestCompanyOperations(t *testing.T) {
	env := setupTestEnv(t)
	resp := signUp(t, env, "company@example.com", "password123", "Company User")
	token := resp.GetAccessToken()

	t.Run("ListCompanies", func(t *testing.T) {
		companyResp, err := env.companyClient.ListCompanies(
			t.Context(),
			authedRequest(t, token, &platformv1.ListCompaniesRequest{}),
		)
		if err != nil {
			t.Fatalf("ListCompanies() error = %v", err)
		}
		if len(companyResp.Msg.GetCompanies()) != 1 {
			t.Fatalf("expected 1 company, got %d", len(companyResp.Msg.GetCompanies()))
		}

		companyID := companyResp.Msg.GetCompanies()[0].GetId()

		t.Run("GetCompany", func(t *testing.T) {
			getResp, err := env.companyClient.GetCompany(
				t.Context(),
				authedRequest(t, token, &platformv1.GetCompanyRequest{Id: companyID}),
			)
			if err != nil {
				t.Fatalf("GetCompany() error = %v", err)
			}
			if getResp.Msg.GetCompany().GetId() != companyID {
				t.Errorf("GetCompany ID = %q, want %q", getResp.Msg.GetCompany().GetId(), companyID)
			}
		})

		t.Run("UpdateCompany", func(t *testing.T) {
			updateResp, err := env.companyClient.UpdateCompany(
				t.Context(),
				authedRequest(t, token, &platformv1.UpdateCompanyRequest{
					Id:   companyID,
					Name: "Updated Company",
					Slug: "updated-company",
				}),
			)
			if err != nil {
				t.Fatalf("UpdateCompany() error = %v", err)
			}
			if updateResp.Msg.GetCompany().GetName() != "Updated Company" {
				t.Errorf("UpdateCompany Name = %q, want %q", updateResp.Msg.GetCompany().GetName(), "Updated Company")
			}
		})
	})
}

func TestRBACViaHandler(t *testing.T) {
	env := setupTestEnv(t)
	resp := signUp(t, env, "rbac@example.com", "password123", "RBAC User")
	token := resp.GetAccessToken()

	// Get company ID
	companyResp, err := env.companyClient.ListCompanies(
		t.Context(),
		authedRequest(t, token, &platformv1.ListCompaniesRequest{}),
	)
	if err != nil {
		t.Fatalf("ListCompanies() error = %v", err)
	}
	companyID := companyResp.Msg.GetCompanies()[0].GetId()

	t.Run("ListRoles", func(t *testing.T) {
		rolesResp, err := env.rbacClient.ListRoles(
			t.Context(),
			authedRequest(t, token, &platformv1.ListRolesRequest{CompanyId: companyID}),
		)
		if err != nil {
			t.Fatalf("ListRoles() error = %v", err)
		}
		if len(rolesResp.Msg.GetRoles()) < 4 {
			t.Errorf("ListRoles() = %d roles, want at least 4 system roles", len(rolesResp.Msg.GetRoles()))
		}
	})
}

func TestUnauthenticatedAccess(t *testing.T) {
	env := setupTestEnv(t)

	t.Run("ListCompaniesNoToken", func(t *testing.T) {
		_, err := env.companyClient.ListCompanies(
			t.Context(),
			connect.NewRequest(&platformv1.ListCompaniesRequest{}),
		)
		if err == nil {
			t.Errorf("ListCompanies without token should fail")
		}
		if connect.CodeOf(err) != connect.CodeUnauthenticated {
			t.Errorf("Expected CodeUnauthenticated, got %v", connect.CodeOf(err))
		}
	})

	t.Run("GetNavItemsNoToken", func(t *testing.T) {
		_, err := env.registryClient.GetNavItems(
			t.Context(),
			connect.NewRequest(&platformv1.GetNavItemsRequest{CompanyId: "some-id"}),
		)
		if err == nil {
			t.Errorf("GetNavItems without token should fail")
		}
		if connect.CodeOf(err) != connect.CodeUnauthenticated {
			t.Errorf("Expected CodeUnauthenticated, got %v", connect.CodeOf(err))
		}
	})

	t.Run("SignUpPublic", func(t *testing.T) {
		resp, err := env.authClient.SignUp(t.Context(), connect.NewRequest(&platformv1.SignUpRequest{
			Email:       "public@example.com",
			Password:    "password123",
			DisplayName: "Public User",
		}))
		if err != nil {
			t.Fatalf("SignUp (public) should succeed without token, got error = %v", err)
		}
		if resp.Msg.GetAuth().GetAccessToken() == "" {
			t.Errorf("SignUp (public) AccessToken is empty")
		}
	})
}

func TestSignupToCompanyToNavItems(t *testing.T) {
	env := setupTestEnv(t)

	// Full end-to-end flow
	signUpResp := signUp(t, env, "e2e@example.com", "password123", "E2E User")
	token := signUpResp.GetAccessToken()

	// List companies
	companyResp, err := env.companyClient.ListCompanies(
		t.Context(),
		authedRequest(t, token, &platformv1.ListCompaniesRequest{}),
	)
	if err != nil {
		t.Fatalf("ListCompanies() error = %v", err)
	}
	if len(companyResp.Msg.GetCompanies()) == 0 {
		t.Fatalf("Expected at least 1 company")
	}
	companyID := companyResp.Msg.GetCompanies()[0].GetId()

	// Get nav items
	navResp, err := env.registryClient.GetNavItems(
		t.Context(),
		authedRequest(t, token, &platformv1.GetNavItemsRequest{CompanyId: companyID}),
	)
	if err != nil {
		t.Fatalf("GetNavItems() error = %v", err)
	}
	if len(navResp.Msg.GetItems()) == 0 {
		t.Errorf("Expected at least 1 nav item")
	}

	// Get badges
	badgeResp, err := env.registryClient.GetBadgeCounts(
		t.Context(),
		authedRequest(t, token, &platformv1.GetBadgeCountsRequest{CompanyId: companyID}),
	)
	if err != nil {
		t.Fatalf("GetBadgeCounts() error = %v", err)
	}
	if len(badgeResp.Msg.GetCounts()) == 0 {
		t.Errorf("Expected at least 1 badge count")
	}
}
