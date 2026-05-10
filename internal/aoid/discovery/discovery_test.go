package discovery

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aocybersystems/eden-platform-go/internal/aoid/config"
)

func TestBuildDoc_RequiredFields(t *testing.T) {
	cfg := &config.Config{Issuer: "https://id.example.com/"}
	doc := BuildDoc(cfg)

	if doc.Issuer != "https://id.example.com" {
		t.Errorf("Issuer = %q want trailing slash trimmed", doc.Issuer)
	}
	if doc.JWKSURI != "https://id.example.com/.well-known/jwks.json" {
		t.Errorf("JWKSURI = %q", doc.JWKSURI)
	}
	if doc.TokenEndpoint != "https://id.example.com/oauth2/token" {
		t.Errorf("TokenEndpoint = %q", doc.TokenEndpoint)
	}
	if doc.AuthorizationEndpoint != "https://id.example.com/oauth2/authorize" {
		t.Errorf("AuthorizationEndpoint = %q", doc.AuthorizationEndpoint)
	}
	if doc.UserinfoEndpoint != "https://id.example.com/oauth2/userinfo" {
		t.Errorf("UserinfoEndpoint = %q", doc.UserinfoEndpoint)
	}
	if doc.ServiceStatus != ServiceStatusScaffold {
		t.Errorf("ServiceStatus = %q want %q", doc.ServiceStatus, ServiceStatusScaffold)
	}

	// Spec-required fields
	if len(doc.ResponseTypesSupported) == 0 {
		t.Error("ResponseTypesSupported empty")
	}
	if len(doc.SubjectTypesSupported) == 0 {
		t.Error("SubjectTypesSupported empty")
	}
	if len(doc.IDTokenSigningAlgValuesSupported) == 0 {
		t.Error("IDTokenSigningAlgValuesSupported empty")
	}
	wantAlg := false
	for _, a := range doc.IDTokenSigningAlgValuesSupported {
		if a == "ML-DSA-65" {
			wantAlg = true
		}
	}
	if !wantAlg {
		t.Errorf("ML-DSA-65 not advertised in id_token_signing_alg_values_supported: %v", doc.IDTokenSigningAlgValuesSupported)
	}
}

func TestHandler_ServesDoc(t *testing.T) {
	cfg := &config.Config{Issuer: "http://localhost:8090"}
	r := httptest.NewRequest(http.MethodGet, "/.well-known/openid-configuration", nil)
	w := httptest.NewRecorder()
	Handler(cfg).ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d want 200", w.Code)
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}

	var doc Doc
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if doc.Issuer != "http://localhost:8090" {
		t.Errorf("Issuer = %q", doc.Issuer)
	}
	if doc.ServiceStatus != ServiceStatusScaffold {
		t.Errorf("ServiceStatus = %q", doc.ServiceStatus)
	}
}

func TestIssuerNotActive_Returns503(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/oauth2/token", nil)
	w := httptest.NewRecorder()
	IssuerNotActive(w, r)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d want 503", w.Code)
	}

	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body["error"] != "issuer_not_active" {
		t.Errorf("error = %q", body["error"])
	}
	if body["error_description"] == "" {
		t.Error("error_description missing")
	}
}

func TestHandlerActive_StampsActive(t *testing.T) {
	cfg := &config.Config{Issuer: "https://id.example.com"}
	rr := httptest.NewRecorder()
	HandlerActive(cfg)(rr, httptest.NewRequest(http.MethodGet, "/.well-known/openid-configuration", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d", rr.Code)
	}
	var got Doc
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ServiceStatus != ServiceStatusActive {
		t.Errorf("service_status=%q want %q", got.ServiceStatus, ServiceStatusActive)
	}
	// All standard fields still present.
	if got.AuthorizationEndpoint == "" || got.TokenEndpoint == "" || got.UserinfoEndpoint == "" {
		t.Error("HandlerActive lost required endpoints")
	}
}
