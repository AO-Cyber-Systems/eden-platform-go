package identity

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aocybersystems/eden-platform-go/platform/auth/jwks"
	"github.com/aocybersystems/eden-platform-go/platform/kms"
)

// These tests are about what a consumer receives, not about what the handler
// meant to send.
//
// A key document is the one response in an authentication system that decides
// which key a signature is checked against, so the assertions below read the
// served bytes rather than the handler's fields: the content type a consumer
// sees, the identifier a consumer selects by, the bytes a consumer gets on a
// second request, and — the one that matters most — whether this package's own
// remote key source can resolve the signer's key from them.

const (
	// keySetKeyID names the key these tests publish. It is deliberately
	// unlike the minter's and the issuer's fixtures so that a change to
	// either suite cannot quietly retune this one.
	keySetKeyID = "published-signing-key-01"

	// keySetMediaType is the RFC 7517 media type for a key set document,
	// spelled out here rather than read off the package so that a change to
	// the constant has to be made twice, on purpose.
	keySetMediaType = "application/jwk-set+json"
)

// --- fixtures ----------------------------------------------------------------

// publishableSigner is a signing key surface with an identifier: the package's
// ES256 and RS256 test signers supply the crypto half, and this adds the kid a
// consumer selects the key by.
type publishableSigner struct {
	jwks.Signer

	keyID string
}

func (s publishableSigner) KeyID() string { return s.keyID }

// newPublishedES256 returns an ES256 signer published under keySetKeyID,
// alongside the underlying signer so a test can compare the resolved key
// against the public half that was actually generated.
func newPublishedES256(t *testing.T) (KeySetSigner, *ecdsaTestSigner) {
	t.Helper()

	signer := newES256Signer(t)
	return publishableSigner{Signer: signer, keyID: keySetKeyID}, signer
}

// newPublishedRS256 is the RS256 counterpart, so the tests show the handler
// follows the signer's algorithm rather than assuming one of them.
func newPublishedRS256(t *testing.T) (KeySetSigner, *rsaTestSigner) {
	t.Helper()

	signer := newRS256Signer(t)
	return publishableSigner{Signer: signer, keyID: keySetKeyID}, signer
}

// regeneratingSigner hands out a DIFFERENT public key every time it is asked
// for one, and counts the asking.
//
// It is the instrument for the render-once property. A handler that built the
// document per request would read this signer per request and serve different
// bytes each time; one that built it at construction reads it once and serves
// the same bytes forever. Nothing else distinguishes the two implementations
// from outside, because rendering a FIXED key twice produces identical bytes
// either way.
//
// It stands for a real thing rather than an invented one: a key surface whose
// answer can change between two reads is what a rotation, a failover or a
// partially degraded provider looks like from in here.
type regeneratingSigner struct {
	t *testing.T

	mu    sync.Mutex
	calls int
}

func (s *regeneratingSigner) Public() crypto.PublicKey {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.calls++
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		s.t.Fatalf("ecdsa.GenerateKey returned error: %v", err)
	}
	return &key.PublicKey
}

func (s *regeneratingSigner) SigningAlgorithm() string { return "ES256" }

func (s *regeneratingSigner) KeyID() string { return keySetKeyID }

func (s *regeneratingSigner) publicReads() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.calls
}

// mismatchedSigner declares one algorithm and holds a public key of another
// type, which is a key the publication surface refuses to render.
type mismatchedSigner struct {
	public crypto.PublicKey
	alg    string
	keyID  string
}

func (s mismatchedSigner) Public() crypto.PublicKey { return s.public }
func (s mismatchedSigner) SigningAlgorithm() string { return s.alg }
func (s mismatchedSigner) KeyID() string            { return s.keyID }

// Compile-time assertions. The second is the one that matters: the platform
// key surface is publishable as it stands, so an operator wiring an issuer and
// its key document together needs no adapter between them and cannot pair a
// document with a key surface other than the one that signs.
var (
	_ KeySetSigner = publishableSigner{}
	_ KeySetSigner = (kms.KMSSigner)(nil)
	_ KeySetSigner = (*regeneratingSigner)(nil)
	_ KeySetSigner = mismatchedSigner{}
	_ http.Handler = (*KeySetHandler)(nil)
)

// --- helpers -----------------------------------------------------------------

// newKeySetServer publishes signer on a test server and returns its URL. The
// port is whatever the operating system assigns; no test here binds a fixed
// one.
func newKeySetServer(t *testing.T, signer KeySetSigner, opts ...KeySetOption) string {
	t.Helper()

	handler, err := NewKeySetHandler(signer, opts...)
	if err != nil {
		t.Fatalf("NewKeySetHandler returned error: %v", err)
	}

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server.URL
}

// fetchKeySet issues one request and returns the response and its body, with
// the body already read and closed.
func fetchKeySet(t *testing.T, method, url string) (*http.Response, []byte) {
	t.Helper()

	req, err := http.NewRequestWithContext(context.Background(), method, url, nil)
	if err != nil {
		t.Fatalf("http.NewRequestWithContext returned error: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s returned error: %v", method, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the response body returned error: %v", err)
	}
	return resp, body
}

// publicKeyEqualer is the Equal method every standard library public key
// carries, used so a resolved key can be compared against the generated one
// without reaching into either type's fields.
type publicKeyEqualer interface {
	Equal(crypto.PublicKey) bool
}

// --- the loop ----------------------------------------------------------------

// TestKeySetHandlerServesADocumentThisPackageResolvesAKeyFrom is the test this
// file exists for.
//
// "We publish a valid key set" and "we can read a published key set" are two
// separate assumptions, and each can hold while the pair fails: a document
// valid by inspection is not necessarily one this package's fetcher accepts,
// and a fetcher that passes against a hand-written fixture is not necessarily
// one that accepts what this handler emits. Feeding the handler's own output
// back through the remote key source collapses both into a single demonstrated
// fact, and it does it over a real socket, so the media type, the status and
// the encoding are all on the path.
func TestKeySetHandlerServesADocumentThisPackageResolvesAKeyFrom(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		build func(*testing.T) (KeySetSigner, crypto.PublicKey)
	}{
		{
			name: "ES256",
			build: func(t *testing.T) (KeySetSigner, crypto.PublicKey) {
				published, signer := newPublishedES256(t)
				return published, signer.Public()
			},
		},
		{
			name: "RS256",
			build: func(t *testing.T) (KeySetSigner, crypto.PublicKey) {
				published, signer := newPublishedRS256(t)
				return published, signer.Public()
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			signer, want := tc.build(t)
			url := newKeySetServer(t, signer)

			source, err := NewRemoteJWKS(url)
			if err != nil {
				t.Fatalf("NewRemoteJWKS returned error: %v", err)
			}

			// The kid asked for is the signer's own, which is the kid a
			// minted context carries in its header. Anything else would be
			// this test agreeing with itself about a name.
			got, err := source.KeyForKID(context.Background(), signer.KeyID())
			if err != nil {
				t.Fatalf("KeyForKID(%q) returned error: %v", signer.KeyID(), err)
			}

			equaler, ok := got.(publicKeyEqualer)
			if !ok {
				t.Fatalf("resolved key of type %T has no Equal method", got)
			}
			if !equaler.Equal(want) {
				t.Fatal("the resolved key is not the signer's public key")
			}
		})
	}
}

// TestKeySetHandlerServesTheSignersOwnKeyID pins the identifier directly, so a
// failure says which half of the loop broke.
func TestKeySetHandlerServesTheSignersOwnKeyID(t *testing.T) {
	t.Parallel()

	signer, _ := newPublishedES256(t)
	_, body := fetchKeySet(t, http.MethodGet, newKeySetServer(t, signer))

	var document struct {
		Keys []struct {
			Kid string `json:"kid"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(body, &document); err != nil {
		t.Fatalf("the served document is not JSON: %v", err)
	}
	if len(document.Keys) != 1 {
		t.Fatalf("served %d keys, want exactly 1", len(document.Keys))
	}
	if document.Keys[0].Kid != signer.KeyID() {
		t.Fatalf("served kid %q, want the signer's %q", document.Keys[0].Kid, signer.KeyID())
	}
}

// --- what renders the document -----------------------------------------------

// TestKeySetHandlerServesWhatThePublicationSurfaceRenders shows the bytes come
// from the platform's key set type rather than from JSON assembled here.
//
// It matters because a second renderer is a second definition of what this
// process's key looks like, and the two would drift silently: a fix to the
// coordinate padding or to the algorithm field made in one would leave the
// other publishing subtly wrong keys with nothing failing.
func TestKeySetHandlerServesWhatThePublicationSurfaceRenders(t *testing.T) {
	t.Parallel()

	signer, _ := newPublishedES256(t)
	_, body := fetchKeySet(t, http.MethodGet, newKeySetServer(t, signer))

	var set jwks.Set
	if err := set.AddSigningKey(signer, signer.KeyID()); err != nil {
		t.Fatalf("AddSigningKey returned error: %v", err)
	}
	want, err := set.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON returned error: %v", err)
	}

	if !bytes.Equal(body, want) {
		t.Fatalf("served document\n\t%s\nis not what the key set type renders\n\t%s", body, want)
	}
}

// --- no private half ---------------------------------------------------------

// TestKeySetHandlerServesNoPrivateKeyMaterial decodes the served body and
// fails on any private component.
//
// The publication surface renders public halves only, so this should never
// fail. It is written anyway because it is the one assertion whose cost if it
// is ever wrong is unbounded — a published private key is a forged identity
// for every principal the issuer serves — and because a future change to what
// gets published would otherwise be silent. The fields are checked by name on
// the decoded document rather than by searching the text, so a component named
// anywhere in a key is caught rather than a substring that happens to appear.
func TestKeySetHandlerServesNoPrivateKeyMaterial(t *testing.T) {
	t.Parallel()

	// d is the EC private scalar and the RSA private exponent; the rest are
	// the RSA primes and the precomputed CRT values.
	private := []string{"d", "p", "q", "dp", "dq", "qi"}

	cases := []struct {
		name  string
		build func(*testing.T) KeySetSigner
	}{
		{
			name: "ES256",
			build: func(t *testing.T) KeySetSigner {
				published, _ := newPublishedES256(t)
				return published
			},
		},
		{
			name: "RS256",
			build: func(t *testing.T) KeySetSigner {
				published, _ := newPublishedRS256(t)
				return published
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, body := fetchKeySet(t, http.MethodGet, newKeySetServer(t, tc.build(t)))

			var document struct {
				Keys []map[string]json.RawMessage `json:"keys"`
			}
			if err := json.Unmarshal(body, &document); err != nil {
				t.Fatalf("the served document is not JSON: %v", err)
			}
			if len(document.Keys) == 0 {
				t.Fatal("the served document carries no keys")
			}

			for i, key := range document.Keys {
				for _, field := range private {
					if _, found := key[field]; found {
						t.Fatalf("key %d carries private component %q", i, field)
					}
				}
			}
		})
	}
}

// --- rendered once -----------------------------------------------------------

// TestKeySetHandlerServesByteIdenticalBodies shows the document is rendered
// once and then served unchanged.
//
// The signer behind it answers with a different key every time it is read, so
// two identical bodies can only mean the handler read it once. The property is
// not an optimisation: a key document that can differ between two requests is
// one where a consumer's answer to "which key signed this" depends on which
// response it happened to get, and no amount of correct signing survives that.
func TestKeySetHandlerServesByteIdenticalBodies(t *testing.T) {
	t.Parallel()

	url := newKeySetServer(t, &regeneratingSigner{t: t})

	_, first := fetchKeySet(t, http.MethodGet, url)
	_, second := fetchKeySet(t, http.MethodGet, url)

	if !bytes.Equal(first, second) {
		t.Fatalf("two requests returned different documents:\n\t%s\n\t%s", first, second)
	}
}

// TestKeySetHandlerReadsTheSigningKeyOnlyAtConstruction is the same property
// stated from the other side: the key surface is not touched while serving.
//
// Reading a key surface per request is a call to a hosted key service on the
// request path, which is a rate limit and an outage this response has no
// reason to be exposed to.
func TestKeySetHandlerReadsTheSigningKeyOnlyAtConstruction(t *testing.T) {
	t.Parallel()

	signer := &regeneratingSigner{t: t}
	url := newKeySetServer(t, signer)

	atConstruction := signer.publicReads()
	if atConstruction == 0 {
		t.Fatal("the signing key was never read, so nothing was published")
	}

	for range 3 {
		fetchKeySet(t, http.MethodGet, url)
	}

	if after := signer.publicReads(); after != atConstruction {
		t.Fatalf("the signing key was read %d more times while serving, want 0", after-atConstruction)
	}
}

// TestKeySetHandlerServesOneDocumentUnderConcurrentRequests runs the same
// assertion with the requests overlapping, which is where a handler that
// rebuilt shared state per request is caught by the race detector.
func TestKeySetHandlerServesOneDocumentUnderConcurrentRequests(t *testing.T) {
	t.Parallel()

	url := newKeySetServer(t, &regeneratingSigner{t: t})
	_, want := fetchKeySet(t, http.MethodGet, url)

	const requests = 16

	var wg sync.WaitGroup
	bodies := make([][]byte, requests)
	failures := make([]error, requests)
	for i := range requests {
		wg.Add(1)
		go func() {
			defer wg.Done()

			req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
			if err != nil {
				failures[i] = err
				return
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				failures[i] = err
				return
			}
			defer func() { _ = resp.Body.Close() }()

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				failures[i] = err
				return
			}
			bodies[i] = body
		}()
	}
	wg.Wait()

	for i, err := range failures {
		if err != nil {
			t.Fatalf("request %d returned error: %v", i, err)
		}
	}
	for i, body := range bodies {
		if !bytes.Equal(body, want) {
			t.Fatalf("request %d returned a different document:\n\t%s\n\t%s", i, body, want)
		}
	}
}

// --- http behaviour ----------------------------------------------------------

// TestKeySetHandlerServesTheKeySetMediaType checks the content type. A
// consumer choosing a parser by it is entitled to be told this is a key set
// and not some other JSON.
func TestKeySetHandlerServesTheKeySetMediaType(t *testing.T) {
	t.Parallel()

	signer, _ := newPublishedES256(t)
	resp, _ := fetchKeySet(t, http.MethodGet, newKeySetServer(t, signer))

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET returned %s, want 200", resp.Status)
	}
	if got := resp.Header.Get("Content-Type"); got != keySetMediaType {
		t.Fatalf("served content type %q, want %q", got, keySetMediaType)
	}
}

// TestKeySetHandlerAnswersHEADWithoutABody checks the method a consumer uses
// to ask whether the document is worth fetching without paying for it.
func TestKeySetHandlerAnswersHEADWithoutABody(t *testing.T) {
	t.Parallel()

	signer, _ := newPublishedES256(t)
	url := newKeySetServer(t, signer)

	_, want := fetchKeySet(t, http.MethodGet, url)
	resp, body := fetchKeySet(t, http.MethodHead, url)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("HEAD returned %s, want 200", resp.Status)
	}
	if len(body) != 0 {
		t.Fatalf("HEAD returned %d body bytes, want none", len(body))
	}
	if got := resp.Header.Get("Content-Type"); got != keySetMediaType {
		t.Fatalf("HEAD served content type %q, want %q", got, keySetMediaType)
	}
	// The headers have to describe the document a GET would return, or the
	// answer is useless for deciding whether to fetch it.
	if got := resp.Header.Get("Content-Length"); got != strconv.Itoa(len(want)) {
		t.Fatalf("HEAD declared content length %q, want %d", got, len(want))
	}
	if resp.Header.Get("Cache-Control") == "" {
		t.Fatal("HEAD served no Cache-Control")
	}
}

// TestKeySetHandlerRejectsOtherMethods checks that the endpoint is read-only.
//
// The refusal carries an Allow header because a bare 405 tells a consumer
// nothing about what to do instead, and because RFC 9110 requires it.
func TestKeySetHandlerRejectsOtherMethods(t *testing.T) {
	t.Parallel()

	signer, _ := newPublishedES256(t)
	url := newKeySetServer(t, signer)

	for _, method := range []string{
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
		http.MethodOptions,
	} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()

			resp, _ := fetchKeySet(t, method, url)

			if resp.StatusCode != http.StatusMethodNotAllowed {
				t.Fatalf("%s returned %s, want 405", method, resp.Status)
			}
			allow := resp.Header.Get("Allow")
			for _, want := range []string{http.MethodGet, http.MethodHead} {
				if !strings.Contains(allow, want) {
					t.Fatalf("%s returned Allow %q, want it to name %s", method, allow, want)
				}
			}
		})
	}
}

// TestKeySetHandlerSetsACacheLifetime checks the header that stops a consumer
// fetching this document on every verification.
//
// Without it, one request to a service that verifies a context becomes two:
// one to the service and one to this endpoint. The lifetime is what makes a
// published key a cached fact rather than a dependency in the request path.
func TestKeySetHandlerSetsACacheLifetime(t *testing.T) {
	t.Parallel()

	signer, _ := newPublishedES256(t)
	resp, _ := fetchKeySet(t, http.MethodGet, newKeySetServer(t, signer))

	got := resp.Header.Get("Cache-Control")
	want := "max-age=" + strconv.Itoa(int(DefaultKeySetMaxAge.Seconds()))
	if !strings.Contains(got, want) {
		t.Fatalf("served Cache-Control %q, want it to carry %q", got, want)
	}
}

// TestKeySetHandlerHonoursAConfiguredCacheLifetime shows the lifetime is an
// operator's decision. It is the same number as the rotation overlap: a key
// withdrawn sooner than this is a key still being verified against after it
// has gone.
func TestKeySetHandlerHonoursAConfiguredCacheLifetime(t *testing.T) {
	t.Parallel()

	signer, _ := newPublishedES256(t)
	url := newKeySetServer(t, signer, WithKeySetMaxAge(90*time.Second))

	resp, _ := fetchKeySet(t, http.MethodGet, url)

	if got, want := resp.Header.Get("Cache-Control"), "max-age=90"; !strings.Contains(got, want) {
		t.Fatalf("served Cache-Control %q, want it to carry %q", got, want)
	}
}

// --- construction ------------------------------------------------------------

// TestNewKeySetHandlerRejectsAMissingSigner: there is nothing to publish, and
// an endpoint answering with an empty set would look to a consumer exactly
// like an issuer that had withdrawn every key it had.
func TestNewKeySetHandlerRejectsAMissingSigner(t *testing.T) {
	t.Parallel()

	handler, err := NewKeySetHandler(nil)
	if !errors.Is(err, ErrNilKeySetSigner) {
		t.Fatalf("NewKeySetHandler(nil) returned error %v, want ErrNilKeySetSigner", err)
	}
	if handler != nil {
		t.Fatal("NewKeySetHandler returned a handler alongside an error")
	}
}

// TestNewKeySetHandlerRejectsAnEmptyKeyID: an unnamed key cannot be selected
// by the kid a context carries, so publishing one would be publishing a key no
// verifier can ever reach for.
func TestNewKeySetHandlerRejectsAnEmptyKeyID(t *testing.T) {
	t.Parallel()

	handler, err := NewKeySetHandler(publishableSigner{Signer: newES256Signer(t), keyID: ""})
	if !errors.Is(err, ErrMissingKeySetKeyID) {
		t.Fatalf("NewKeySetHandler returned error %v, want ErrMissingKeySetKeyID", err)
	}
	if handler != nil {
		t.Fatal("NewKeySetHandler returned a handler alongside an error")
	}
}

// TestNewKeySetHandlerRejectsAnUnrenderableKey: a key the publication surface
// will not render is a start-up failure, not an endpoint that serves an empty
// document to everyone who asks for the rest of the process's life.
func TestNewKeySetHandlerRejectsAnUnrenderableKey(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		build func(*testing.T) KeySetSigner
	}{
		{
			name: "unsupported algorithm",
			build: func(t *testing.T) KeySetSigner {
				signer := newES256Signer(t)
				signer.alg = "ES512"
				return publishableSigner{Signer: signer, keyID: keySetKeyID}
			},
		},
		{
			name: "algorithm does not match the key",
			build: func(t *testing.T) KeySetSigner {
				return mismatchedSigner{
					public: newRS256Signer(t).Public(),
					alg:    "ES256",
					keyID:  keySetKeyID,
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			handler, err := NewKeySetHandler(tc.build(t))
			if !errors.Is(err, ErrUnpublishableKey) {
				t.Fatalf("NewKeySetHandler returned error %v, want ErrUnpublishableKey", err)
			}
			if handler != nil {
				t.Fatal("NewKeySetHandler returned a handler alongside an error")
			}
		})
	}
}

// TestNewKeySetHandlerRejectsAnUnexpressibleCacheLifetime: a lifetime that
// cannot be stated in whole seconds is one that reaches a consumer as
// max-age=0, and asking every consumer to refetch on every request is the
// failure this header exists to prevent.
func TestNewKeySetHandlerRejectsAnUnexpressibleCacheLifetime(t *testing.T) {
	t.Parallel()

	signer, _ := newPublishedES256(t)

	for _, maxAge := range []time.Duration{0, -time.Second, 500 * time.Millisecond} {
		t.Run(maxAge.String(), func(t *testing.T) {
			t.Parallel()

			handler, err := NewKeySetHandler(signer, WithKeySetMaxAge(maxAge))
			if !errors.Is(err, ErrInvalidKeySetMaxAge) {
				t.Fatalf("NewKeySetHandler returned error %v, want ErrInvalidKeySetMaxAge", err)
			}
			if handler != nil {
				t.Fatal("NewKeySetHandler returned a handler alongside an error")
			}
		})
	}
}
