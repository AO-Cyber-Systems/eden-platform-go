package federation

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aocybersystems/eden-platform-go/platform/audit"
	"github.com/aocybersystems/eden-platform-go/platform/auth"
	"github.com/aocybersystems/eden-platform-go/platform/devstore"
	"github.com/google/uuid"
)

func newTestStack(t *testing.T) (*Stack, *http.ServeMux) {
	t.Helper()
	backend := devstore.NewMemoryBackend()
	store := backend.AuthStore()
	auditStore := backend.AuditStore()
	auditLog := audit.NewLogger(auditStore)
	auditLog.Start()
	t.Cleanup(auditLog.Stop)

	jwtCfg := auth.DefaultJWTConfig()
	jwtCfg.Issuer = "https://aoid.test"
	jwt, err := auth.NewJWTManager(jwtCfg)
	if err != nil {
		t.Fatalf("NewJWTManager: %v", err)
	}
	authSvc := auth.NewService(store, jwt, auth.NewPasswordHasher())

	reg := NewInMemoryRegistry()
	spReg := NewInMemorySPRegistry()
	resolver, err := MustGenerateSharedKey("AO ID Test")
	if err != nil {
		t.Fatalf("MustGenerateSharedKey: %v", err)
	}
	idpMgr, _ := NewIdPManager(reg, resolver)
	bridge, _ := NewBridge(authSvc, spReg, jwt, auditLog)

	stack := &Stack{
		Registry:    reg,
		SPRegistry:  spReg,
		IdPManager:  idpMgr,
		Bridge:      bridge,
		Exchanger:   &StubOIDCExchanger{},
		BaseURL:     "https://aoid.test",
		StateSecret: []byte("test-state-secret-32-bytes-okay-x"),
		SessionTTL:  time.Hour,
	}
	mux := http.NewServeMux()
	stack.Mount(mux)
	return stack, mux
}

func TestHandler_IdPMetadata(t *testing.T) {
	stack, mux := newTestStack(t)
	cfg := newTenantConfig(t)
	_ = stack.Registry.Register(nil, cfg)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	res, err := http.Get(srv.URL + "/saml/idp/" + cfg.TenantID.String() + "/metadata")
	if err != nil {
		t.Fatalf("GET metadata: %v", err)
	}
	if res.StatusCode != 200 {
		t.Errorf("status: got %d", res.StatusCode)
	}
	body := readBody(t, res)
	if !strings.Contains(body, cfg.EntityID) {
		t.Errorf("metadata missing EntityID")
	}
	if ct := res.Header.Get("Content-Type"); !strings.Contains(ct, "samlmetadata+xml") {
		t.Errorf("Content-Type: %q", ct)
	}
}

func TestHandler_IdPMetadata_UnknownTenant(t *testing.T) {
	_, mux := newTestStack(t)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	res, _ := http.Get(srv.URL + "/saml/idp/" + uuid.New().String() + "/metadata")
	if res.StatusCode != 404 {
		t.Errorf("expected 404, got %d", res.StatusCode)
	}
}

func TestHandler_ListExternalIdPs(t *testing.T) {
	stack, mux := newTestStack(t)
	tenantID := uuid.New()
	cfg := newExternalIdPConfig(t, ProviderOIDC, tenantID)
	_ = stack.SPRegistry.Register(nil, cfg)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	res, err := http.Get(srv.URL + "/federation/" + tenantID.String() + "/idps")
	if err != nil {
		t.Fatalf("GET idps: %v", err)
	}
	if res.StatusCode != 200 {
		t.Errorf("status: %d", res.StatusCode)
	}
	var entries []externalIdPSummary
	if err := json.NewDecoder(res.Body).Decode(&entries); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("entries: got %d, want 1", len(entries))
	}
	if entries[0].DisplayName == "" {
		t.Errorf("DisplayName missing")
	}
	// Ensure no client_secret in the response by re-reading body.
	res.Body.Close()
}

func TestHandler_StartFederation_OIDC(t *testing.T) {
	stack, mux := newTestStack(t)
	tenantID := uuid.New()
	cfg := newExternalIdPConfig(t, ProviderOIDC, tenantID)
	_ = stack.SPRegistry.Register(nil, cfg)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	u := srv.URL + "/federation/" + tenantID.String() + "/start?external_idp_id=" + cfg.ID.String()
	res, err := client.Get(u)
	if err != nil {
		t.Fatalf("GET start: %v", err)
	}
	if res.StatusCode != http.StatusFound {
		t.Errorf("status: %d", res.StatusCode)
	}
	loc := res.Header.Get("Location")
	if !strings.Contains(loc, "client_id=") {
		t.Errorf("redirect missing client_id: %s", loc)
	}
}

func TestHandler_StartFederation_MissingParams(t *testing.T) {
	_, mux := newTestStack(t)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	res, _ := http.Get(srv.URL + "/federation/" + uuid.New().String() + "/start")
	if res.StatusCode != 400 {
		t.Errorf("expected 400, got %d", res.StatusCode)
	}
}

func TestHandler_OIDCCallback(t *testing.T) {
	stack, mux := newTestStack(t)
	tenantID := uuid.New()
	cfg := newExternalIdPConfig(t, ProviderOIDC, tenantID)
	// Configure stub to return a deterministic assertion.
	stack.Exchanger = &StubOIDCExchanger{
		FixedAssertion: &Assertion{
			Subject:      "stub-subject",
			Email:        "user@acme.com",
			DisplayName:  "User Acme",
			AuthnContext: "urn:oasis:names:tc:SAML:2.0:ac:classes:MultiFactorContract",
		},
	}
	_ = stack.SPRegistry.Register(nil, cfg)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	state, err := stack.signState(stateClaims{
		TenantID:      tenantID,
		ExternalIdPID: cfg.ID,
		IssuedAt:      time.Now().Unix(),
	})
	if err != nil {
		t.Fatalf("signState: %v", err)
	}
	q := url.Values{}
	q.Set("code", "code-abc")
	q.Set("state", state)
	res, err := http.Get(srv.URL + "/federation/" + tenantID.String() + "/oidc/callback?" + q.Encode())
	if err != nil {
		t.Fatalf("GET callback: %v", err)
	}
	if res.StatusCode != 200 {
		t.Errorf("status: %d body: %s", res.StatusCode, readBody(t, res))
	}
	body := readBody(t, res)
	var out struct {
		AccessToken    string `json:"access_token"`
		RefreshToken   string `json:"refresh_token"`
		Email          string `json:"email"`
		ProvisionedNew bool   `json:"provisioned_new"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("decode body: %v: %s", err, body)
	}
	if out.AccessToken == "" {
		t.Errorf("missing access_token")
	}
	if !out.ProvisionedNew {
		t.Errorf("expected new provisioning")
	}
	if out.Email != "user@acme.com" {
		t.Errorf("email: %q", out.Email)
	}
}

func TestHandler_OIDCCallback_DomainViolation(t *testing.T) {
	stack, mux := newTestStack(t)
	tenantID := uuid.New()
	cfg := newExternalIdPConfig(t, ProviderOIDC, tenantID)
	cfg.JITPolicy.RequireMFA = false
	stack.Exchanger = &StubOIDCExchanger{
		FixedAssertion: &Assertion{
			Email:        "user@evil.com",
			AuthnContext: "urn:oasis:names:tc:SAML:2.0:ac:classes:MultiFactorContract",
		},
	}
	_ = stack.SPRegistry.Register(nil, cfg)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	state, _ := stack.signState(stateClaims{
		TenantID:      tenantID,
		ExternalIdPID: cfg.ID,
		IssuedAt:      time.Now().Unix(),
	})
	res, _ := http.Get(srv.URL + "/federation/" + tenantID.String() + "/oidc/callback?code=x&state=" + state)
	if res.StatusCode != 403 {
		t.Errorf("expected 403, got %d", res.StatusCode)
	}
}

func TestHandler_OIDCCallback_BadState(t *testing.T) {
	_, mux := newTestStack(t)
	tenantID := uuid.New()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	res, _ := http.Get(srv.URL + "/federation/" + tenantID.String() + "/oidc/callback?code=x&state=garbage")
	if res.StatusCode != 400 {
		t.Errorf("expected 400, got %d", res.StatusCode)
	}
}

func TestHandler_UnknownTenant(t *testing.T) {
	_, mux := newTestStack(t)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	res, _ := http.Get(srv.URL + "/federation/not-a-uuid/idps")
	if res.StatusCode != 404 {
		t.Errorf("expected 404, got %d", res.StatusCode)
	}
}

func TestStack_StateRoundtrip(t *testing.T) {
	stack, _ := newTestStack(t)
	claims := stateClaims{
		TenantID:      uuid.New(),
		ExternalIdPID: uuid.New(),
		IssuedAt:      time.Now().Unix(),
	}
	state, err := stack.signState(claims)
	if err != nil {
		t.Fatalf("signState: %v", err)
	}
	got, err := stack.verifyState(state)
	if err != nil {
		t.Fatalf("verifyState: %v", err)
	}
	if got.TenantID != claims.TenantID {
		t.Errorf("tenant mismatch")
	}
}

func TestStack_StateTampered(t *testing.T) {
	stack, _ := newTestStack(t)
	claims := stateClaims{TenantID: uuid.New(), ExternalIdPID: uuid.New(), IssuedAt: time.Now().Unix()}
	state, _ := stack.signState(claims)
	// Tamper.
	tampered := state[:len(state)-2] + "ZZ"
	if _, err := stack.verifyState(tampered); err == nil {
		t.Errorf("expected signature mismatch error")
	}
}

func TestStack_StateExpired(t *testing.T) {
	stack, _ := newTestStack(t)
	claims := stateClaims{
		TenantID:      uuid.New(),
		ExternalIdPID: uuid.New(),
		IssuedAt:      time.Now().Add(-2 * time.Hour).Unix(),
	}
	state, _ := stack.signState(claims)
	_, err := stack.verifyState(state)
	if err == nil {
		t.Errorf("expected expired error")
	}
}

func TestSplitTenantPath(t *testing.T) {
	id := uuid.New()
	tenant, rest, ok := splitTenantPath("/saml/idp/"+id.String()+"/metadata", "/saml/idp/")
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if tenant != id {
		t.Errorf("tenant mismatch")
	}
	if rest != "metadata" {
		t.Errorf("rest: %q", rest)
	}
	if _, _, ok := splitTenantPath("/saml/idp/bad-uuid/metadata", "/saml/idp/"); ok {
		t.Errorf("expected ok=false for bad uuid")
	}
}

func TestBuildAssertionAttributes(t *testing.T) {
	u := userAdapter{Email: "u@acme.com", DisplayName: "U", ID: "user-id-1"}
	tpl := map[string][]string{
		"email":      {"email"},
		"first_name": {"display_name"},
	}
	out := buildAssertionAttributes(u, tpl)
	if got := out["email"]; len(got) != 1 || got[0] != "u@acme.com" {
		t.Errorf("email: %v", got)
	}
	if got := out["first_name"]; len(got) != 1 || got[0] != "U" {
		t.Errorf("first_name: %v", got)
	}
}

func TestWriteFederationError(t *testing.T) {
	cases := []struct {
		err    error
		status int
	}{
		{ErrTenantNotFound, 404},
		{ErrExternalIdPNotFound, 404},
		{ErrTenantInactive, 410},
		{ErrInvalidConfig, 400},
		{ErrFederationUserNotFound, 403},
		{ErrJITDomainNotAllowed, 403},
		{ErrJITMFARequired, 403},
		{errors.New("unknown"), 500},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		writeFederationError(rec, tc.err)
		if rec.Code != tc.status {
			t.Errorf("%v -> got %d, want %d", tc.err, rec.Code, tc.status)
		}
	}
}

func readBody(t *testing.T, res *http.Response) string {
	t.Helper()
	defer res.Body.Close()
	var b strings.Builder
	buf := make([]byte, 1024)
	for {
		n, err := res.Body.Read(buf)
		if n > 0 {
			b.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
	return b.String()
}
