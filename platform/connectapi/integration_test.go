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
	platformregistry "github.com/aocybersystems/eden-platform-go/platform/registry"
	"github.com/aocybersystems/eden-platform-go/platform/server"
)

func TestGeneratedClientsWorkAgainstPlatformHandlers(t *testing.T) {
	backend := devstore.NewMemoryBackend()
	authStore := backend.AuthStore()
	companyStore := backend.CompanyStore()
	reg := platformregistry.New()
	reg.Register(&platformregistry.ModuleRegistration{
		Name: "home",
		NavItems: []platformregistry.NavItem{
			{ID: "home", Label: "Home", Icon: "home", Path: "/home", Feature: "home", Priority: 0},
		},
		BadgeProvider: func(companyID, userID string) int { return 1 },
	})

	jwtManager, err := auth.NewJWTManager(auth.JWTConfig{
		PrivateKeyPath:     "../../dev/jwt/jwt_es256_private.pem",
		PublicKeyPath:      "../../dev/jwt/jwt_es256_public.pem",
		Issuer:             "eden-platform-test",
		AccessTokenExpiry:  auth.DefaultJWTConfig().AccessTokenExpiry,
		RefreshTokenExpiry: auth.DefaultJWTConfig().RefreshTokenExpiry,
	})
	if err != nil {
		t.Fatalf("NewJWTManager() error = %v", err)
	}

	mux := http.NewServeMux()
	server.RegisterPlatformHandlers(
		mux,
		server.PlatformHandlers{
			Auth:     NewAuthHandler(auth.NewService(authStore, jwtManager, auth.NewPasswordHasher())),
			Company:  NewCompanyHandler(company.NewService(companyStore), companyStore),
			Registry: NewRegistryHandler(reg, companyStore),
		},
		connect.WithInterceptors(server.NewAuthInterceptor(jwtManager, server.DefaultPublicProcedures())),
	)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	authClient := platformv1connect.NewAuthServiceClient(ts.Client(), ts.URL)
	signUpResp, err := authClient.SignUp(
		t.Context(),
		connect.NewRequest(&platformv1.SignUpRequest{
			Email:       "dev@example.com",
			Password:    "password123",
			DisplayName: "Dev User",
		}),
	)
	if err != nil {
		t.Fatalf("SignUp() error = %v", err)
	}

	if signUpResp.Msg.GetAccessToken() == "" {
		t.Fatalf("expected access token in signup response")
	}

	companyClient := platformv1connect.NewCompanyServiceClient(ts.Client(), ts.URL)
	companyReq := connect.NewRequest(&platformv1.ListCompaniesRequest{})
	companyReq.Header().Set("Authorization", "Bearer "+signUpResp.Msg.GetAccessToken())
	companyResp, err := companyClient.ListCompanies(t.Context(), companyReq)
	if err != nil {
		t.Fatalf("ListCompanies() error = %v", err)
	}
	if len(companyResp.Msg.GetCompanies()) != 1 {
		t.Fatalf("expected one company, got %d", len(companyResp.Msg.GetCompanies()))
	}

	registryClient := platformv1connect.NewRegistryServiceClient(ts.Client(), ts.URL)
	navReq := connect.NewRequest(&platformv1.GetNavItemsRequest{
		CompanyId: companyResp.Msg.GetCompanies()[0].GetId(),
	})
	navReq.Header().Set("Authorization", "Bearer "+signUpResp.Msg.GetAccessToken())
	navResp, err := registryClient.GetNavItems(t.Context(), navReq)
	if err != nil {
		t.Fatalf("GetNavItems() error = %v", err)
	}
	if len(navResp.Msg.GetItems()) != 1 {
		t.Fatalf("expected one nav item, got %d", len(navResp.Msg.GetItems()))
	}
}
