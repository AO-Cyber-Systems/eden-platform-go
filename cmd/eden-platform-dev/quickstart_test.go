//go:build dev

package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
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

// quickstartTestEnv wires the SAME real pieces main.go's runServer wires —
// a real auth.Service backed by a real auth.JWTManager, a real RBAC
// enforcer, and the real auth + RBAC ConnectRPC interceptors — around
// CompanyService.GetCompany, the procedure QuickstartCurlProcedure names.
// ProcedurePermissions is deliberately left with no entry for GetCompany,
// matching main.go's defaultProcedurePermissions exactly: GetCompany is
// authenticated but carries no specific permission requirement, so the real
// RBAC interceptor's "no mapping => allow any authenticated caller" branch
// is what this test exercises — not a permission this test manufactures.
type quickstartTestEnv struct {
	ts            *httptest.Server
	companyClient platformv1connect.CompanyServiceClient
	authService   *auth.Service
}

func setupQuickstartTestEnv(t *testing.T) *quickstartTestEnv {
	t.Helper()

	backend := devstore.NewMemoryBackend()
	seedRBACData(backend) // roles + permissions (main.go) — owner/admin/member/viewer
	seedDevTenant(backend)

	jwtManager, err := auth.NewJWTManager(auth.JWTConfig{
		Issuer:             "eden-platform-dev-quickstart-test",
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
		PublicProcedures:     server.DefaultPublicProcedures(),
		ProcedurePermissions: map[string]server.Permission{},
	})

	mux := http.NewServeMux()
	server.RegisterPlatformHandlers(
		mux,
		server.PlatformHandlers{
			Company: connectapi.NewCompanyHandler(companyService, nil),
		},
		connect.WithInterceptors(authInterceptor, rbacInterceptor),
	)
	mux.Handle("/up", (&server.HealthChecker{}).Handler())

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	return &quickstartTestEnv{
		ts:            ts,
		companyClient: platformv1connect.NewCompanyServiceClient(ts.Client(), ts.URL),
		authService:   authService,
	}
}

// TestBuildQuickstartCurl_Format proves BuildQuickstartCurl composes its
// arguments (base URL, procedure path, token, company id) into the curl
// invocation rather than hand-typing any of them.
func TestBuildQuickstartCurl_Format(t *testing.T) {
	got := BuildQuickstartCurl("http://127.0.0.1:8091", "tok123", "20000000-0000-0000-0000-000000000001")

	for _, want := range []string{
		"curl",
		"-X POST",
		"http://127.0.0.1:8091" + QuickstartCurlProcedure,
		`-H "Authorization: Bearer tok123"`,
		`"id":"20000000-0000-0000-0000-000000000001"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("BuildQuickstartCurl() = %q, missing %q", got, want)
		}
	}
}

// TestBuildQuickstartBanner_NamesDevOnlyInMemoryDataLoss proves the banner
// states, in plain language, that the playground is dev-only, in-memory,
// and that all data is lost on exit — the must-have truth this TRD requires
// verbatim.
func TestBuildQuickstartBanner_NamesDevOnlyInMemoryDataLoss(t *testing.T) {
	banner := BuildQuickstartBanner("http://127.0.0.1:8091", "dev@eden.local", "user-1", "company-1", "tok123")
	lower := strings.ToLower(banner)

	for _, want := range []string{"dev-only", "in-memory", "lost"} {
		if !strings.Contains(lower, want) {
			t.Fatalf("BuildQuickstartBanner() does not mention %q:\n%s", want, banner)
		}
	}
	if !strings.Contains(lower, "exit") {
		t.Fatalf("BuildQuickstartBanner() does not mention data loss on exit:\n%s", banner)
	}
	if !strings.Contains(lower, "local dev credential") {
		t.Fatalf("BuildQuickstartBanner() does not warn the token is a local dev credential:\n%s", banner)
	}
}

// TestBuildQuickstartBanner_ContainsCurl proves the banner embeds the exact
// same string BuildQuickstartCurl returns for the identical inputs — the
// banner cannot drift from the curl builder because it is not a second,
// independently hand-typed copy.
func TestBuildQuickstartBanner_ContainsCurl(t *testing.T) {
	baseURL, token, companyID := "http://127.0.0.1:8091", "tok123", "company-1"
	banner := BuildQuickstartBanner(baseURL, "dev@eden.local", "user-1", companyID, token)
	wantCurl := BuildQuickstartCurl(baseURL, token, companyID)

	if !strings.Contains(banner, wantCurl) {
		t.Fatalf("BuildQuickstartBanner() does not embed BuildQuickstartCurl()'s output.\nbanner:\n%s\nwant curl:\n%s", banner, wantCurl)
	}
}

// TestPrintQuickstart_WritesBannerForSeededUser exercises the actual
// production call path — printQuickstart logging in the seeded dev tenant
// user through the real auth.Service — and captures stdout to prove the
// printed banner names the real seeded identity, not placeholder values.
func TestPrintQuickstart_WritesBannerForSeededUser(t *testing.T) {
	backend := devstore.NewMemoryBackend()
	seedRBACData(backend)
	seedDevTenant(backend)

	jwtManager, err := auth.NewJWTManager(auth.JWTConfig{
		Issuer:             "eden-platform-dev-quickstart-print-test",
		AccessTokenExpiry:  auth.DefaultJWTConfig().AccessTokenExpiry,
		RefreshTokenExpiry: auth.DefaultJWTConfig().RefreshTokenExpiry,
	})
	if err != nil {
		t.Fatalf("NewJWTManager() error = %v", err)
	}
	authService := auth.NewService(backend.AuthStore(), jwtManager, auth.NewPasswordHasher())
	cfg := &config.PlatformConfig{ServerAddr: "127.0.0.1:8091"}

	stdout := captureStdout(t, func() {
		if err := printQuickstart(cfg, authService); err != nil {
			t.Fatalf("printQuickstart() error = %v", err)
		}
	})

	for _, want := range []string{DevTenantEmail, DevTenantCompanyID.String(), "http://127.0.0.1:8091", QuickstartCurlProcedure} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("printQuickstart() stdout missing %q:\n%s", want, stdout)
		}
	}
}

// captureStdout redirects os.Stdout for the duration of fn and returns
// everything written to it.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	original := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = original }()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("read captured stdout: %v", err)
	}
	return buf.String()
}

// TestQuickstartCurl_ExecutedAgainstRealServer_Returns2xx is the test this
// TRD requires: it starts a real server (a real auth.Service, a real
// auth.JWTManager, the real auth + RBAC ConnectRPC interceptors), mints a
// real token through auth.Service.Login, builds the curl EXACTLY the way
// the startup banner does (BuildQuickstartCurl — the same function, the
// same inputs), then hands that literal string to a real `curl` subprocess
// and asserts the response is 2xx. This is what closes TRD 02's honest gap:
// the /dev/session token and the printed curl are proven to work against a
// live, in-process server — not merely unit-tested for shape.
func TestQuickstartCurl_ExecutedAgainstRealServer_Returns2xx(t *testing.T) {
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl not found on PATH — cannot execute the printed quickstart curl")
	}

	env := setupQuickstartTestEnv(t)

	resp, err := env.authService.Login(context.Background(), DevTenantEmail, DevTenantPassword)
	if err != nil {
		t.Fatalf("log in seeded dev tenant user: %v", err)
	}
	if resp.AccessToken == "" {
		t.Fatal("Login() returned an empty access token")
	}

	curlCmd := BuildQuickstartCurl(env.ts.URL, resp.AccessToken, DevTenantCompanyID.String())

	// Run the printed curl verbatim, with a status-capturing suffix appended
	// purely so this test can observe the HTTP status code — the request
	// itself (method, URL, headers, body) is byte-for-byte what curlCmd
	// says, i.e. byte-for-byte what a developer would paste from the
	// startup banner.
	fullCmd := curlCmd + ` -w '\n%{http_code}'`
	out, err := exec.Command("sh", "-c", fullCmd).CombinedOutput()
	if err != nil {
		t.Fatalf("executing printed curl failed: %v\noutput: %s\ncurl: %s", err, out, curlCmd)
	}

	outStr := string(out)
	idx := strings.LastIndex(outStr, "\n")
	if idx < 0 {
		t.Fatalf("unexpected curl output (missing status line): %q\ncurl: %s", outStr, curlCmd)
	}
	body := outStr[:idx]
	status := strings.TrimSpace(outStr[idx+1:])

	if !strings.HasPrefix(status, "2") {
		t.Fatalf("printed curl returned HTTP status %q, want 2xx\ncurl: %s\nbody: %s", status, curlCmd, body)
	}
	if !strings.Contains(body, DevTenantCompanyName) {
		t.Fatalf("printed curl body does not contain seeded company name %q: %s", DevTenantCompanyName, body)
	}
}

// TestQuickstartCurl_InvalidTokenIsRejected is the negative control for the
// test above: it proves a curl built the same way but with a garbage token
// is REJECTED (not silently accepted), so the 2xx result above is proof the
// real auth interceptor validated a genuine token — not an artifact of
// auth being disabled in this test environment.
func TestQuickstartCurl_InvalidTokenIsRejected(t *testing.T) {
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl not found on PATH — cannot execute the printed quickstart curl")
	}

	env := setupQuickstartTestEnv(t)

	curlCmd := BuildQuickstartCurl(env.ts.URL, "not-a-real-token", DevTenantCompanyID.String())
	fullCmd := curlCmd + ` -w '\n%{http_code}'`
	out, err := exec.Command("sh", "-c", fullCmd).CombinedOutput()
	if err != nil {
		t.Fatalf("executing curl with invalid token failed: %v\noutput: %s", err, out)
	}

	outStr := string(out)
	idx := strings.LastIndex(outStr, "\n")
	if idx < 0 {
		t.Fatalf("unexpected curl output (missing status line): %q", outStr)
	}
	status := strings.TrimSpace(outStr[idx+1:])
	if strings.HasPrefix(status, "2") {
		t.Fatalf("curl with an invalid token returned status %q, want a non-2xx rejection", status)
	}
}
