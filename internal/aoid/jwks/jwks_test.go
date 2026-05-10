package jwks

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/aocybersystems/eden-platform-go/platform/auth"
	"github.com/cloudflare/circl/sign/mldsa/mldsa65"
)

func writeSeed(t *testing.T, dir, name string) string {
	t.Helper()
	seed, err := auth.GenerateKeySeed()
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, seed[:], 0600); err != nil {
		t.Fatalf("write seed: %v", err)
	}
	return p
}

func newJWTManagerWithKeys(t *testing.T, kids ...string) *auth.JWTManager {
	t.Helper()
	dir := t.TempDir()
	paths := make(map[string]string, len(kids))
	for _, kid := range kids {
		paths[kid] = writeSeed(t, dir, kid+".seed")
	}
	cfg := auth.DefaultJWTConfig()
	cfg.KeySeedPaths = paths
	cfg.ActiveKID = kids[0]
	jm, err := auth.NewJWTManager(cfg)
	if err != nil {
		t.Fatalf("NewJWTManager: %v", err)
	}
	return jm
}

func TestBuild_TwoKeys(t *testing.T) {
	jm := newJWTManagerWithKeys(t, "a", "b")
	set := Build(jm)

	if len(set.Keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(set.Keys))
	}
	// Active kid first.
	if set.Keys[0].Kid != "a" {
		t.Errorf("first kid = %q want a (active)", set.Keys[0].Kid)
	}

	seen := map[string]bool{}
	for _, k := range set.Keys {
		seen[k.Kid] = true
		if k.Kty != KtyAKP {
			t.Errorf("kty = %q want %q", k.Kty, KtyAKP)
		}
		if k.Alg != AlgMLDSA65 {
			t.Errorf("alg = %q want %q", k.Alg, AlgMLDSA65)
		}
		if k.Use != "sig" {
			t.Errorf("use = %q want sig", k.Use)
		}
		raw, err := base64.RawURLEncoding.DecodeString(k.Pub)
		if err != nil {
			t.Errorf("pub not base64url: %v", err)
		}
		if len(raw) != mldsa65.PublicKeySize {
			t.Errorf("pub length = %d want %d", len(raw), mldsa65.PublicKeySize)
		}
	}
	if !seen["a"] || !seen["b"] {
		t.Errorf("missing kids: %v", seen)
	}
}

func TestHandler_HeadersAndBody(t *testing.T) {
	jm := newJWTManagerWithKeys(t, "primary")
	r := httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil)
	w := httptest.NewRecorder()
	Handler(jm).ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if got := w.Header().Get("Content-Type"); got != "application/jwk-set+json" {
		t.Errorf("Content-Type = %q", got)
	}
	if cc := w.Header().Get("Cache-Control"); cc == "" {
		t.Errorf("Cache-Control missing")
	}

	var set Set
	if err := json.Unmarshal(w.Body.Bytes(), &set); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(set.Keys) != 1 {
		t.Fatalf("len(keys) = %d", len(set.Keys))
	}
	if set.Keys[0].Kid != "primary" {
		t.Errorf("kid = %q", set.Keys[0].Kid)
	}
}

func TestEmptyHandler(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil)
	w := httptest.NewRecorder()
	EmptyHandler().ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var set Set
	if err := json.Unmarshal(w.Body.Bytes(), &set); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(set.Keys) != 0 {
		t.Errorf("expected empty key set, got %d keys", len(set.Keys))
	}
}

func TestBuild_NilManager(t *testing.T) {
	set := Build(nil)
	if len(set.Keys) != 0 {
		t.Errorf("expected empty for nil manager, got %d", len(set.Keys))
	}
}
