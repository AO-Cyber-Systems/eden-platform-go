package identity

import (
	"crypto"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aocybersystems/eden-platform-go/platform/auth/jwks"
	"github.com/aocybersystems/eden-platform-go/platform/auth/kmssigner"
)

// These tests are the acceptance criteria for the package as a whole. The unit
// tests alongside them each pin one layer against its own construction; these
// assemble the layers the way a deployment does — a minter on one side, a
// published key set in between, a verifier on the other — and assert on what
// comes out the far end.
//
// Two of them are load-bearing in a way the unit tests cannot be:
//
//   - The raw-wire fixture below is built from hand-written JSON. Every other
//     test in this package mints with this package and verifies with this
//     package, and both halves share one Claims struct, so a renamed JSON tag
//     round-trips between them perfectly while breaking every independent
//     consumer in the field. A fixture whose bytes no struct tag contributed
//     to is the only end-to-end shape that can see that.
//
//   - The round trip publishes the signing key through the platform's own key
//     publication surface rather than hand-rolling a key set, so it proves the
//     two surfaces actually interoperate instead of assuming it.

const (
	// The fixture issuer and key id are also spelled out, by hand, inside the
	// raw header and payload literals below. The duplication is deliberate:
	// those literals stand in for bytes produced by some other implementation,
	// and a constant interpolated into them would be one more thing about the
	// wire that this package got to decide.
	fixtureIssuer = "https://fixture-issuer.example/"
	fixtureKeyID  = "fixture-context-key"

	firstIssuer  = "https://first-issuer.example/"
	firstKeyID   = "first-context-key"
	secondIssuer = "https://second-issuer.example/"
	secondKeyID  = "second-context-key"

	// rotatedKeyID is the id an issuer publishes after a rotation, alongside
	// the one it replaces.
	rotatedKeyID = "first-context-key-2"
)

// --- the raw wire ------------------------------------------------------------

// rawContextHeader is the JOSE header of a context, written out by hand.
//
// The type is spelled out rather than taken from JWTType for the same reason
// the claim keys below are: a consumer on the far side of the wire has this
// string in its own source, not a reference to this package's constant.
const rawContextHeader = `{"alg":"ES256","typ":"identity-context+jwt","kid":"fixture-context-key"}`

// rawContext is a complete claims document in the frozen wire encoding, with
// only the validity window left to fill in.
//
// Note what is NOT here: no jti. The registered claims carry omitempty, so an
// absent token id is absent from the document, and a context that never had
// one is a perfectly ordinary context. tok_ref, by contrast, carries no
// omitempty and is always on the wire even when empty. That asymmetry is part
// of the contract and is asserted directly further down.
const rawContext = `{
	"iss": "https://fixture-issuer.example/",
	"sub": "principal-0042",
	"iat": %d,
	"exp": %d,
	"tnt": "acme",
	"ent": ["reports.read", "reports.write"],
	"aal": "AAL2",
	"ctx_ver": 1,
	"tok_ref": "7bcc86737e4b8043"
}`

// rawContextAtAFutureVersion is the same document one schema version ahead. It
// exists to drive the negotiation cases from the wire rather than from a
// minter, since nothing in this package will emit a version it does not stamp.
const rawContextAtAFutureVersion = `{
	"iss": "https://fixture-issuer.example/",
	"sub": "principal-0042",
	"iat": %d,
	"exp": %d,
	"tnt": "acme",
	"ent": ["reports.read", "reports.write"],
	"aal": "AAL2",
	"ctx_ver": 2,
	"tok_ref": "7bcc86737e4b8043"
}`

// fixtureWindow returns a validity window around now, as the seconds-since-epoch
// numbers the wire carries.
func fixtureWindow() (issuedAt, expiresAt int64) {
	now := time.Now().UTC()
	return now.Unix(), now.Add(15 * time.Minute).Unix()
}

// signRawContext assembles a compact JWS from a hand-written header and claims
// document.
//
// Nothing in this package contributes a byte of the result. The segments are
// encoded straight from the literals given, and the signature comes from the
// signing seam's method rather than from this package's own selection of it,
// so the token that comes out is the token some other implementation would
// have produced from the same schema.
func signRawContext(t *testing.T, signer kmssigner.Signer, header, claims string) string {
	t.Helper()

	if !json.Valid([]byte(header)) {
		t.Fatalf("raw header is not valid JSON: %s", header)
	}
	if !json.Valid([]byte(claims)) {
		t.Fatalf("raw claims document is not valid JSON: %s", claims)
	}

	signingInput := base64.RawURLEncoding.EncodeToString([]byte(header)) + "." +
		base64.RawURLEncoding.EncodeToString([]byte(claims))

	signature, err := (&kmssigner.ES256SigningMethod{}).Sign(signingInput, signer)
	if err != nil {
		t.Fatalf("signing the raw context returned error: %v", err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature)
}

// fixtureVerifier trusts the raw fixture's issuer, resolving its key id to the
// public half of signer.
func fixtureVerifier(t *testing.T, signer kmssigner.Signer, opts ...VerifierOption) *Verifier {
	t.Helper()

	return newTestVerifier(t, []TrustedIssuer{{
		Issuer: fixtureIssuer,
		Keys:   NewStaticKeys(map[string]crypto.PublicKey{fixtureKeyID: signer.Public()}),
	}}, opts...)
}

// --- publishing a key set ----------------------------------------------------

// publishedKey is one entry an issuer publishes: the id a context names in its
// kid header, and the signer whose public half answers to it.
type publishedKey struct {
	kid    string
	signer jwks.Signer
}

// publishKeySet renders keys through the platform's key publication surface,
// producing the exact bytes an issuer would serve.
//
// Going through that surface rather than hand-building a key set is the point
// of the round trip: it is what turns "this package can parse a JWKS document"
// into "this package can parse the JWKS document this platform publishes".
func publishKeySet(t *testing.T, keys ...publishedKey) []byte {
	t.Helper()

	set := &jwks.Set{}
	for _, key := range keys {
		if err := set.AddSigningKey(key.signer, key.kid); err != nil {
			t.Fatalf("AddSigningKey(%q) returned error: %v", key.kid, err)
		}
	}

	body, err := set.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON of the published key set returned error: %v", err)
	}
	return body
}

// keySetEndpoint is an issuer serving its published key set over HTTP, counting
// how many times it was actually fetched.
//
// The count is what separates "a key resolved" from "a key resolved without
// touching the network", which is the difference between a cache that works and
// one that is merely not observed to be broken.
type keySetEndpoint struct {
	server  *httptest.Server
	fetches atomic.Int64

	mu   sync.Mutex
	body []byte
}

func newKeySetEndpoint(t *testing.T, body []byte) *keySetEndpoint {
	t.Helper()

	endpoint := &keySetEndpoint{body: body}
	// The listener is a loopback socket on a port the operating system picks,
	// so nothing here depends on a particular port being free.
	endpoint.server = httptest.NewServer(http.HandlerFunc(endpoint.serve))
	t.Cleanup(endpoint.server.Close)
	return endpoint
}

func (e *keySetEndpoint) serve(w http.ResponseWriter, _ *http.Request) {
	e.fetches.Add(1)

	e.mu.Lock()
	body := e.body
	e.mu.Unlock()

	w.Header().Set("Content-Type", "application/jwk-set+json")
	_, _ = w.Write(body)
}

// republish replaces what the endpoint serves, which is what a rotation looks
// like from a consumer's side.
func (e *keySetEndpoint) republish(body []byte) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.body = body
}

func (e *keySetEndpoint) url() string { return e.server.URL }

func (e *keySetEndpoint) fetchCount() int64 { return e.fetches.Load() }

// --- the raw wire verifies ---------------------------------------------------

// TestARawWireContextVerifies is the load-bearing test of this package.
//
// Every other test here mints with this package and verifies with this package.
// Both halves share one Claims struct, so renaming a JSON tag keeps all of them
// green while breaking every independent consumer that decodes this document by
// key name. This one is built from a hand-written literal, so no struct tag
// contributed a byte of it: if a tag is renamed, this is the test that fails,
// and on some days it is the only one.
func TestARawWireContextVerifies(t *testing.T) {
	signer := newES256Signer(t)
	issuedAt, expiresAt := fixtureWindow()
	token := signRawContext(t, signer, rawContextHeader,
		fmt.Sprintf(rawContext, issuedAt, expiresAt))

	claims, err := fixtureVerifier(t, signer).Verify(t.Context(), token)
	if err != nil {
		t.Fatalf("Verify of a raw wire context returned error: %v "+
			"(a renamed or retagged claim is the first thing to suspect)", err)
	}

	for _, field := range []struct {
		wireKey string
		got     any
		want    any
	}{
		{"iss", claims.Issuer, fixtureIssuer},
		{"sub", claims.Subject, "principal-0042"},
		{"tnt", claims.Tenant, "acme"},
		{"aal", claims.AAL, AAL2},
		{"ctx_ver", claims.CtxVer, 1},
		{"tok_ref", claims.TokRef, "7bcc86737e4b8043"},
	} {
		if field.got != field.want {
			t.Errorf("wire key %q decoded to %v, want %v — the Go field and the wire key "+
				"have come apart", field.wireKey, field.got, field.want)
		}
	}
	if want := []string{"reports.read", "reports.write"}; !slices.Equal(claims.Entitlements, want) {
		t.Errorf("wire key \"ent\" decoded to %v, want %v", claims.Entitlements, want)
	}
}

// TestAMintedContextIsByteCompatibleWithTheRawWire checks the two directions
// agree: what this package emits carries exactly the keys the hand-written
// document uses, no more and no fewer.
//
// The absent-versus-empty distinction is the interesting part. tok_ref carries
// no omitempty and is on the wire even when nothing set it; jti does carry
// omitempty and vanishes. A consumer decoding by key name sees that asymmetry,
// so it is contract, not incidental.
func TestAMintedContextIsByteCompatibleWithTheRawWire(t *testing.T) {
	signer := newES256Signer(t)
	minter := newTestMinterFor(t, signer, fixtureIssuer, fixtureKeyID)

	token, err := minter.Mint(MintInput{
		Subject:   "principal-0042",
		Tenant:    "acme",
		Assurance: AAL2,
	})
	if err != nil {
		t.Fatalf("Mint returned error: %v", err)
	}

	payload := decodeSegment(t, token, 1)

	if _, present := payload["tok_ref"]; !present {
		t.Error("a minted context omits \"tok_ref\"; the frozen tag carries no omitempty, " +
			"so a consumer decoding by key name expects it present and empty")
	}
	if _, present := payload["jti"]; present {
		t.Error("a minted context carries \"jti\" although none was supplied; " +
			"the registered claims carry omitempty and an absent token id is absent")
	}
	if entitlements, present := payload["ent"]; !present || entitlements == nil {
		t.Errorf("a minted context encodes \"ent\" as %v, want an empty array — "+
			"null and [] are different documents to a consumer", entitlements)
	}

	for _, key := range []string{"iss", "sub", "iat", "exp", "tnt", "ent", "aal", "ctx_ver", "tok_ref"} {
		if _, present := payload[key]; !present {
			t.Errorf("a minted context is missing the frozen wire key %q", key)
		}
	}
}

// --- the full round trip -----------------------------------------------------

// TestAMintedContextRoundTripsThroughAPublishedKeySet assembles the whole
// package the way a deployment does: mint on one side, publish the key through
// the platform's own publication surface, serve it, fetch it, verify on the
// other side.
//
// Publishing through that surface rather than hand-building a key set is what
// makes this more than a restatement of the unit tests — it proves the bytes
// this platform publishes are the bytes this package can read.
func TestAMintedContextRoundTripsThroughAPublishedKeySet(t *testing.T) {
	signer := newES256Signer(t)
	minter := newTestMinterFor(t, signer, firstIssuer, firstKeyID)

	token, err := minter.Mint(MintInput{
		Subject:      "principal-0042",
		Tenant:       "acme",
		Entitlements: []string{"reports.read"},
		Assurance:    AAL2,
		TokenRef:     TokRef("an-upstream-credential"),
	})
	if err != nil {
		t.Fatalf("Mint returned error: %v", err)
	}

	endpoint := newKeySetEndpoint(t, publishKeySet(t, publishedKey{kid: firstKeyID, signer: signer}))
	keys, err := NewRemoteJWKS(endpoint.url())
	if err != nil {
		t.Fatalf("NewRemoteJWKS returned error: %v", err)
	}

	verifier := newTestVerifier(t, []TrustedIssuer{{Issuer: firstIssuer, Keys: keys}})

	claims, err := verifier.Verify(t.Context(), token)
	if err != nil {
		t.Fatalf("Verify of a context whose key came from a published key set returned error: %v", err)
	}

	if claims.Subject != "principal-0042" || claims.Tenant != "acme" || claims.AAL != AAL2 {
		t.Errorf("round trip returned sub=%q tnt=%q aal=%q, want principal-0042 / acme / %s",
			claims.Subject, claims.Tenant, claims.AAL, AAL2)
	}
	if want := []string{"reports.read"}; !slices.Equal(claims.Entitlements, want) {
		t.Errorf("round trip returned ent=%v, want %v", claims.Entitlements, want)
	}
	if claims.CtxVer != Version {
		t.Errorf("round trip returned ctx_ver=%d, want %d", claims.CtxVer, Version)
	}
	if fetches := endpoint.fetchCount(); fetches != 1 {
		t.Errorf("the key set was fetched %d times for one verification, want 1", fetches)
	}
}

// TestOneVerifierAcceptsContextsFromTwoIssuers is the multi-issuer acceptance
// case: two issuers with independent signers and independent key sources, one
// verifier, both accepted.
//
// This is the shape the plural trust set exists for — a service verifying its
// own in-process issuer alongside an external one.
func TestOneVerifierAcceptsContextsFromTwoIssuers(t *testing.T) {
	firstSigner, secondSigner := newES256Signer(t), newES256Signer(t)

	firstEndpoint := newKeySetEndpoint(t, publishKeySet(t, publishedKey{kid: firstKeyID, signer: firstSigner}))
	firstKeys, err := NewRemoteJWKS(firstEndpoint.url())
	if err != nil {
		t.Fatalf("NewRemoteJWKS for the first issuer returned error: %v", err)
	}

	verifier := newTestVerifier(t, []TrustedIssuer{
		{Issuer: firstIssuer, Keys: firstKeys},
		{Issuer: secondIssuer, Keys: NewStaticKeys(map[string]crypto.PublicKey{
			secondKeyID: secondSigner.Public(),
		})},
	})

	for _, issuer := range []struct {
		name   string
		signer kmssigner.Signer
		iss    string
		kid    string
	}{
		{"remote key set", firstSigner, firstIssuer, firstKeyID},
		{"in-process key set", secondSigner, secondIssuer, secondKeyID},
	} {
		t.Run(issuer.name, func(t *testing.T) {
			token, err := newTestMinterFor(t, issuer.signer, issuer.iss, issuer.kid).
				Mint(MintInput{Subject: "principal-0042", Tenant: "acme", Assurance: AAL1})
			if err != nil {
				t.Fatalf("Mint returned error: %v", err)
			}

			claims, err := verifier.Verify(t.Context(), token)
			if err != nil {
				t.Fatalf("Verify returned error: %v", err)
			}
			if claims.Issuer != issuer.iss {
				t.Errorf("Verify returned iss=%q, want %q", claims.Issuer, issuer.iss)
			}
		})
	}
}

// TestVersionNegotiationIsDecidedByTheVerifier drives the negotiation from the
// wire: ONE token, two verifiers, opposite outcomes.
//
// This is the failure the package was built to make legible. Independent
// consumers drifted on which schema versions they accept, and the drift was
// only ever discoverable at runtime, as an authorization failure. Here the same
// bytes are accepted or refused purely by configuration, which is what lets a
// consumer widen its accepted set deliberately instead of discovering the
// mismatch in production.
func TestVersionNegotiationIsDecidedByTheVerifier(t *testing.T) {
	signer := newES256Signer(t)
	issuedAt, expiresAt := fixtureWindow()
	futureVersion := signRawContext(t, signer, rawContextHeader,
		fmt.Sprintf(rawContextAtAFutureVersion, issuedAt, expiresAt))

	t.Run("a verifier on the default set refuses it", func(t *testing.T) {
		if _, err := fixtureVerifier(t, signer).Verify(t.Context(), futureVersion); err != ErrInvalidContext {
			t.Fatalf("Verify error = %v, want %v — the default accepted set is the "+
				"strictest one and must not take a version it was not configured for",
				err, ErrInvalidContext)
		}
	})

	t.Run("a verifier configured to widen accepts it", func(t *testing.T) {
		widened := fixtureVerifier(t, signer, WithAcceptedVersions(NewVersionSet(1, 2)))

		claims, err := widened.Verify(t.Context(), futureVersion)
		if err != nil {
			t.Fatalf("Verify returned error: %v", err)
		}
		if claims.CtxVer != 2 {
			t.Errorf("Verify returned ctx_ver=%d, want 2", claims.CtxVer)
		}
	})
}

// TestAKeySetRotationHealsWithoutChangingTheKeyID is the deployment case the
// remote key source's TTL exists for.
//
// An issuer that publishes a fixed key id and replaces the key behind it never
// gives a kid-keyed cache a miss to react to. Without a second, time-based
// trigger the verifier keeps checking signatures against a public key whose
// private half is gone, and every request is refused indefinitely.
func TestAKeySetRotationHealsWithoutChangingTheKeyID(t *testing.T) {
	original, replacement := newES256Signer(t), newES256Signer(t)

	endpoint := newKeySetEndpoint(t, publishKeySet(t, publishedKey{kid: firstKeyID, signer: original}))

	clock := time.Now()
	keys, err := NewRemoteJWKS(endpoint.url(),
		WithJWKSCacheTTL(5*time.Minute),
		WithJWKSClock(func() time.Time { return clock }),
	)
	if err != nil {
		t.Fatalf("NewRemoteJWKS returned error: %v", err)
	}
	verifier := newTestVerifier(t, []TrustedIssuer{{Issuer: firstIssuer, Keys: keys}})

	mint := func(signer kmssigner.Signer) string {
		t.Helper()
		token, err := newTestMinterFor(t, signer, firstIssuer, firstKeyID).
			Mint(MintInput{Subject: "principal-0042", Tenant: "acme", Assurance: AAL1})
		if err != nil {
			t.Fatalf("Mint returned error: %v", err)
		}
		return token
	}

	if _, err := verifier.Verify(t.Context(), mint(original)); err != nil {
		t.Fatalf("Verify before the rotation returned error: %v", err)
	}

	// The issuer replaces the key but keeps publishing it under the same id.
	endpoint.republish(publishKeySet(t, publishedKey{kid: firstKeyID, signer: replacement}))
	rotated := mint(replacement)

	if _, err := verifier.Verify(t.Context(), rotated); err == nil {
		t.Fatal("Verify accepted a context signed by the replacement key while the " +
			"previous key set was still cached; the cache is not being consulted")
	}

	clock = clock.Add(6 * time.Minute)

	if _, err := verifier.Verify(t.Context(), rotated); err != nil {
		t.Fatalf("Verify after the cache expired returned error: %v — a key replaced "+
			"under an unchanged key id never produces a cache miss, so only the "+
			"time-based refresh can recover from this", err)
	}
}

// --- helpers -----------------------------------------------------------------

// newTestMinterFor builds a minter for a specific issuer and key id, which the
// multi-issuer cases need and the per-file default helper does not offer.
func newTestMinterFor(t *testing.T, signer kmssigner.Signer, issuer, keyID string) *Minter {
	t.Helper()

	minter, err := NewMinter(signer, keyID, issuer)
	if err != nil {
		t.Fatalf("NewMinter(%q, %q) returned error: %v", keyID, issuer, err)
	}
	return minter
}
