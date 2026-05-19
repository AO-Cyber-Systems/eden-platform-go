package saml

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/beevik/etree"
	"github.com/crewjam/saml/samlidp"
	dsig "github.com/russellhaering/goxmldsig"
)

// inMemoryStore satisfies samlidp.Store with an in-process map. The
// production wiring (TRD 06-09) uses a DB-backed store; this fake is only
// for unit tests so we can exercise the NewIDP wiring end-to-end.
type inMemoryStore struct {
	data map[string][]byte
}

func newInMemoryStore() *inMemoryStore { return &inMemoryStore{data: map[string][]byte{}} }

func (s *inMemoryStore) Get(key string, value interface{}) error {
	raw, ok := s.data[key]
	if !ok {
		return samlidp.ErrNotFound
	}
	// Use the same JSON shape samlidp uses internally.
	return jsonUnmarshal(raw, value)
}
func (s *inMemoryStore) Put(key string, value interface{}) error {
	raw, err := jsonMarshal(value)
	if err != nil {
		return err
	}
	s.data[key] = raw
	return nil
}
func (s *inMemoryStore) Delete(key string) error {
	delete(s.data, key)
	return nil
}
func (s *inMemoryStore) List(prefix string) ([]string, error) {
	out := []string{}
	for k := range s.data {
		if strings.HasPrefix(k, prefix) {
			out = append(out, strings.TrimPrefix(k, prefix))
		}
	}
	return out, nil
}

// Use encoding/json — but capture into a small helper so we don't pollute
// the package imports when this is the only consumer.
func jsonMarshal(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}
func jsonUnmarshal(b []byte, v interface{}) error {
	return json.Unmarshal(b, v)
}

func TestNewIDP_ConstructsServerWithKMSSigner(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	cert := newRSACert(t, priv)
	signer := &KMSSigner{
		Signer: &fakeRSAKMSBackend{priv: priv, alg: "RS256", keyID: "idp-key"},
		KeyID:  "idp-key",
		Cert:   cert,
	}

	idpURL := mustURL(t, "https://aoid.example.com/saml/idp")
	store := newInMemoryStore()

	server, err := NewIDP(IDPOptions{
		TenantSlug:  "acme",
		EntityID:    "https://aoid.example.com/saml/idp/acme",
		URL:         idpURL,
		MetadataURL: mustURL(t, "https://aoid.example.com/saml/idp/metadata"),
		SSOURL:      mustURL(t, "https://aoid.example.com/saml/idp/sso"),
		LogoutURL:   mustURL(t, "https://aoid.example.com/saml/idp/logout"),
		Cert:        cert,
		Signer:      signer,
		Store:       store,
	})
	if err != nil {
		t.Fatalf("NewIDP: %v", err)
	}
	if server == nil {
		t.Fatal("NewIDP returned nil server")
	}
	if server.IDP.Signer != signer {
		t.Fatal("server.IDP.Signer not patched to KMSSigner")
	}
	if server.IDP.Certificate != cert {
		t.Fatal("server.IDP.Certificate not set")
	}
	if server.IDP.SignatureMethod != dsig.RSASHA256SignatureMethod {
		t.Fatalf("server.IDP.SignatureMethod = %q, want RSASHA256", server.IDP.SignatureMethod)
	}
}

func TestNewIDP_MetadataServesSigningCert(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	cert := newRSACert(t, priv)
	signer := &KMSSigner{
		Signer: &fakeRSAKMSBackend{priv: priv, alg: "RS256", keyID: "idp-key"},
		KeyID:  "idp-key",
		Cert:   cert,
	}
	server, err := NewIDP(IDPOptions{
		TenantSlug:  "acme",
		EntityID:    "https://aoid.example.com/saml/idp/acme",
		URL:         mustURL(t, "https://aoid.example.com/saml/idp"),
		MetadataURL: mustURL(t, "https://aoid.example.com/saml/idp/metadata"),
		SSOURL:      mustURL(t, "https://aoid.example.com/saml/idp/sso"),
		LogoutURL:   mustURL(t, "https://aoid.example.com/saml/idp/logout"),
		Cert:        cert,
		Signer:      signer,
		Store:       newInMemoryStore(),
	})
	if err != nil {
		t.Fatalf("NewIDP: %v", err)
	}

	// Serve metadata and confirm the signing X509 element matches our cert.
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metadata", nil)
	server.IDP.ServeMetadata(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("metadata HTTP code: %d", rr.Code)
	}
	wantCertB64 := base64.StdEncoding.EncodeToString(cert.Raw)
	if !strings.Contains(rr.Body.String(), wantCertB64) {
		t.Fatalf("metadata does not contain our signing cert\n--- got body:\n%s", rr.Body.String())
	}
}

// TestNewIDP_SignAssertionRoundTrip exercises the actual sign path:
// build a SigningContext via the same forked goxmldsig wrapper the real
// IDP uses, sign an Assertion element, and validate with the cert.
func TestNewIDP_SignAssertionRoundTrip(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	cert := newRSACert(t, priv)
	signer := &KMSSigner{
		Signer: &fakeRSAKMSBackend{priv: priv, alg: "RS256", keyID: "idp-key"},
		KeyID:  "idp-key",
		Cert:   cert,
	}
	_, err = NewIDP(IDPOptions{
		TenantSlug:  "acme",
		EntityID:    "https://aoid.example.com/saml/idp/acme",
		URL:         mustURL(t, "https://aoid.example.com/saml/idp"),
		MetadataURL: mustURL(t, "https://aoid.example.com/saml/idp/metadata"),
		SSOURL:      mustURL(t, "https://aoid.example.com/saml/idp/sso"),
		LogoutURL:   mustURL(t, "https://aoid.example.com/saml/idp/logout"),
		Cert:        cert,
		Signer:      signer,
		Store:       newInMemoryStore(),
	})
	if err != nil {
		t.Fatalf("NewIDP: %v", err)
	}

	// Build a SigningContext via the same code path AOID's IDP uses.
	ctx, err := NewIDPSigningContext(signer, cert)
	if err != nil {
		t.Fatalf("NewIDPSigningContext: %v", err)
	}

	doc := etree.NewDocument()
	assertion := doc.CreateElement("Assertion")
	assertion.CreateAttr("ID", "id-1")
	assertion.SetText("hello")

	signed, err := ctx.SignEnveloped(assertion)
	if err != nil {
		t.Fatalf("SignEnveloped: %v", err)
	}

	signedDoc := etree.NewDocument()
	signedDoc.SetRoot(signed)
	bs, err := signedDoc.WriteToBytes()
	if err != nil {
		t.Fatalf("WriteToBytes: %v", err)
	}
	freshDoc := etree.NewDocument()
	if err := freshDoc.ReadFromBytes(bs); err != nil {
		t.Fatalf("ReadFromBytes: %v", err)
	}
	store := &dsig.MemoryX509CertificateStore{Roots: []*x509.Certificate{cert}}
	if _, err := dsig.NewDefaultValidationContext(store).Validate(freshDoc.Root()); err != nil {
		t.Fatalf("Validate signed assertion: %v", err)
	}
}

func TestNewIDP_MissingFields(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	cert := newRSACert(t, priv)
	signer := &KMSSigner{Signer: &fakeRSAKMSBackend{priv: priv, alg: "RS256"}, Cert: cert}

	base := IDPOptions{
		EntityID:    "x",
		URL:         mustURL(t, "https://x/idp"),
		MetadataURL: mustURL(t, "https://x/idp/metadata"),
		SSOURL:      mustURL(t, "https://x/idp/sso"),
		LogoutURL:   mustURL(t, "https://x/idp/logout"),
		Cert:        cert,
		Signer:      signer,
		Store:       newInMemoryStore(),
	}

	t.Run("nil cert", func(t *testing.T) {
		o := base
		o.Cert = nil
		if _, err := NewIDP(o); !errors.Is(err, ErrIDPMissingCert) {
			t.Fatalf("want ErrIDPMissingCert, got %v", err)
		}
	})
	t.Run("nil signer", func(t *testing.T) {
		o := base
		o.Signer = nil
		if _, err := NewIDP(o); !errors.Is(err, ErrIDPMissingSigner) {
			t.Fatalf("want ErrIDPMissingSigner, got %v", err)
		}
	})
	t.Run("nil store", func(t *testing.T) {
		o := base
		o.Store = nil
		if _, err := NewIDP(o); !errors.Is(err, ErrIDPMissingStore) {
			t.Fatalf("want ErrIDPMissingStore, got %v", err)
		}
	})
	t.Run("empty entityID", func(t *testing.T) {
		o := base
		o.EntityID = ""
		if _, err := NewIDP(o); err == nil {
			t.Fatal("want non-nil error for empty EntityID")
		}
	})
}

// Sanity check we can resolve the URL package fields we set.
func TestNewIDP_PreservesURLs(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	cert := newRSACert(t, priv)
	signer := &KMSSigner{
		Signer: &fakeRSAKMSBackend{priv: priv, alg: "RS256", keyID: "idp-key"},
		KeyID:  "idp-key",
		Cert:   cert,
	}
	server, err := NewIDP(IDPOptions{
		EntityID:    "acme",
		URL:         mustURL(t, "https://aoid.example.com/saml/idp"),
		MetadataURL: mustURL(t, "https://aoid.example.com/saml/idp/metadata"),
		SSOURL:      mustURL(t, "https://aoid.example.com/saml/idp/sso"),
		LogoutURL:   mustURL(t, "https://aoid.example.com/saml/idp/logout"),
		Cert:        cert,
		Signer:      signer,
		Store:       newInMemoryStore(),
	})
	if err != nil {
		t.Fatalf("NewIDP: %v", err)
	}
	if got, want := server.IDP.MetadataURL.Path, "/saml/idp/metadata"; !strings.HasSuffix(got, want) {
		t.Fatalf("MetadataURL path: got %q want suffix %q", got, want)
	}
	if got, want := server.IDP.SSOURL.Path, "/saml/idp/sso"; !strings.HasSuffix(got, want) {
		t.Fatalf("SSOURL path: got %q want suffix %q", got, want)
	}
}

func init() {
	// guard against accidentally importing a wrong url.URL helper.
	_ = (*url.URL)(nil)
}
