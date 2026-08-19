package identity

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aocybersystems/eden-platform-go/platform/auth/kmssigner"
	"github.com/golang-jwt/jwt/v5"
)

const (
	testKeyID  = "context-signing-key-1"
	testIssuer = "https://issuer.example/"
)

// ecdsaTestSigner is a kmssigner.Signer backed by an in-process P-256 key.
//
// It exists only so the tests have something to sign with, and it is
// deliberately not shipped: a signer that generates its own key is the kind of
// convenience that turns into a production code path. Minting through the
// signing seam is what keeps the private half somewhere this process cannot
// read it out of.
type ecdsaTestSigner struct {
	key *ecdsa.PrivateKey
	alg string
}

func (s *ecdsaTestSigner) Public() crypto.PublicKey { return &s.key.PublicKey }

// Sign returns an ASN.1-DER signature, which is the shape a real ES256 provider
// returns and the shape the seam converts to the JWS r||s form.
func (s *ecdsaTestSigner) Sign(r io.Reader, digest []byte, _ crypto.SignerOpts) ([]byte, error) {
	return ecdsa.SignASN1(r, s.key, digest)
}

func (s *ecdsaTestSigner) SigningAlgorithm() string { return s.alg }

func newES256Signer(t *testing.T) *ecdsaTestSigner {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey returned error: %v", err)
	}
	return &ecdsaTestSigner{key: key, alg: "ES256"}
}

// rsaTestSigner is the RS256 counterpart, so the tests can show the minter
// follows the signer's algorithm rather than hard-coding one of them.
type rsaTestSigner struct {
	key *rsa.PrivateKey
}

func (s *rsaTestSigner) Public() crypto.PublicKey { return &s.key.PublicKey }

func (s *rsaTestSigner) Sign(r io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error) {
	return rsa.SignPKCS1v15(r, s.key, opts.HashFunc(), digest)
}

func (s *rsaTestSigner) SigningAlgorithm() string { return "RS256" }

func newRS256Signer(t *testing.T) *rsaTestSigner {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey returned error: %v", err)
	}
	return &rsaTestSigner{key: key}
}

// newTestMinter builds a Minter over a fresh ES256 signer and returns both,
// since most assertions eventually need the public half to verify with.
func newTestMinter(t *testing.T, opts ...MinterOption) (*Minter, *ecdsaTestSigner) {
	t.Helper()

	signer := newES256Signer(t)
	minter, err := NewMinter(signer, testKeyID, testIssuer, opts...)
	if err != nil {
		t.Fatalf("NewMinter returned error: %v", err)
	}
	return minter, signer
}

// validInput is a complete set of caller-supplied authenticated subject data:
// the happy path, which individual tests then degrade one field at a time.
func validInput() MintInput {
	return MintInput{
		Subject:      "principal-0001",
		Tenant:       "acme",
		Entitlements: []string{"reports.read", "reports.write"},
		Assurance:    AAL2,
		TokenID:      "context-0001",
		TokenRef:     "7bcc86737e4b8043",
	}
}

// decodeSegment base64url-decodes one segment of a compact JWS and returns it
// as an untyped map.
//
// Assertions land here rather than on a struct round trip on purpose: decoding
// into Claims and reading Go fields would pass unchanged even if every JSON tag
// were renamed, and the wire format is the contract.
func decodeSegment(t *testing.T, token string, index int) map[string]any {
	t.Helper()

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d segments, want 3 (compact JWS)", len(parts))
	}

	raw, err := base64.RawURLEncoding.DecodeString(parts[index])
	if err != nil {
		t.Fatalf("base64 decode of segment %d returned error: %v", index, err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("json.Unmarshal(%s) returned error: %v", raw, err)
	}
	return decoded
}

func header(t *testing.T, token string) map[string]any {
	t.Helper()
	return decodeSegment(t, token, 0)
}

func payload(t *testing.T, token string) map[string]any {
	t.Helper()
	return decodeSegment(t, token, 1)
}

// mustMint fails the test if minting fails, so that the assertion which follows
// is about the token rather than about the error.
func mustMint(t *testing.T, m *Minter, in MintInput) string {
	t.Helper()

	token, err := m.Mint(in)
	if err != nil {
		t.Fatalf("Mint returned error: %v", err)
	}
	if token == "" {
		t.Fatal("Mint returned an empty token and a nil error")
	}
	return token
}

func stringClaim(t *testing.T, claims map[string]any, key string) string {
	t.Helper()

	value, ok := claims[key]
	if !ok {
		t.Fatalf("claim %q is absent; present keys: %v", key, sortedKeys(claims))
	}
	s, ok := value.(string)
	if !ok {
		t.Fatalf("claim %q = %v (%T), want a string", key, value, value)
	}
	return s
}

func numberClaim(t *testing.T, claims map[string]any, key string) float64 {
	t.Helper()

	value, ok := claims[key]
	if !ok {
		t.Fatalf("claim %q is absent; present keys: %v", key, sortedKeys(claims))
	}
	n, ok := value.(float64)
	if !ok {
		t.Fatalf("claim %q = %v (%T), want a number", key, value, value)
	}
	return n
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// --- construction -----------------------------------------------------------

func TestNewMinterRejectsANilSigner(t *testing.T) {
	minter, err := NewMinter(nil, testKeyID, testIssuer)
	if !errors.Is(err, ErrNilSigner) {
		t.Errorf("NewMinter(nil signer) error = %v, want %v", err, ErrNilSigner)
	}
	if minter != nil {
		t.Error("NewMinter returned a non-nil Minter alongside an error")
	}
}

func TestNewMinterRejectsAnEmptyKeyID(t *testing.T) {
	// Without a kid a verifier cannot select the key to check the signature
	// against, so the context is unverifiable the moment the issuer holds more
	// than one key — which it does the first time a key is rotated.
	minter, err := NewMinter(newES256Signer(t), "", testIssuer)
	if !errors.Is(err, ErrMissingKeyID) {
		t.Errorf("NewMinter(empty kid) error = %v, want %v", err, ErrMissingKeyID)
	}
	if minter != nil {
		t.Error("NewMinter returned a non-nil Minter alongside an error")
	}
}

func TestNewMinterRejectsAnEmptyIssuer(t *testing.T) {
	// iss is what a verifier resolves a key source against; an empty issuer
	// matches no configured trust entry.
	minter, err := NewMinter(newES256Signer(t), testKeyID, "")
	if !errors.Is(err, ErrMissingIssuer) {
		t.Errorf("NewMinter(empty issuer) error = %v, want %v", err, ErrMissingIssuer)
	}
	if minter != nil {
		t.Error("NewMinter returned a non-nil Minter alongside an error")
	}
}

func TestNewMinterRejectionsAreDistinguishable(t *testing.T) {
	// Each misconfiguration is separately diagnosable: one opaque "bad config"
	// error makes an operator guess which of three fields is wrong. This is
	// deployment-time configuration rather than an authorization decision, so
	// unlike the verifier there is no oracle to protect here.
	distinct := []error{ErrNilSigner, ErrMissingKeyID, ErrMissingIssuer}
	for i, a := range distinct {
		for j, b := range distinct {
			if i == j {
				continue
			}
			if errors.Is(a, b) {
				t.Errorf("errors.Is(%v, %v) = true; construction errors must not alias", a, b)
			}
		}
	}
}

func TestNewMinterRejectsAnUnsupportedSigningAlgorithm(t *testing.T) {
	// The signing seam implements ES256 and RS256 only. Pairing this minter
	// with a signer for anything else is a wiring mistake, and it fails at
	// construction rather than at the first mint.
	for _, alg := range []string{"", "HS256", "none", "ES384", "PS256"} {
		t.Run(alg, func(t *testing.T) {
			signer := newES256Signer(t)
			signer.alg = alg

			minter, err := NewMinter(signer, testKeyID, testIssuer)
			if !errors.Is(err, ErrUnsupportedAlgorithm) {
				t.Errorf("NewMinter(alg %q) error = %v, want %v", alg, err, ErrUnsupportedAlgorithm)
			}
			if minter != nil {
				t.Error("NewMinter returned a non-nil Minter alongside an error")
			}
		})
	}
}

func TestNewMinterRejectsANonPositiveTTL(t *testing.T) {
	// A zero or negative lifetime mints a context that has already expired.
	for name, ttl := range map[string]time.Duration{
		"zero":     0,
		"negative": -time.Second,
	} {
		t.Run(name, func(t *testing.T) {
			minter, err := NewMinter(newES256Signer(t), testKeyID, testIssuer, WithTTL(ttl))
			if !errors.Is(err, ErrInvalidTTL) {
				t.Errorf("NewMinter(WithTTL(%v)) error = %v, want %v", ttl, err, ErrInvalidTTL)
			}
			if minter != nil {
				t.Error("NewMinter returned a non-nil Minter alongside an error")
			}
		})
	}
}

func TestNewMinterRejectsANilClock(t *testing.T) {
	minter, err := NewMinter(newES256Signer(t), testKeyID, testIssuer, WithClock(nil))
	if !errors.Is(err, ErrInvalidClock) {
		t.Errorf("NewMinter(WithClock(nil)) error = %v, want %v", err, ErrInvalidClock)
	}
	if minter != nil {
		t.Error("NewMinter returned a non-nil Minter alongside an error")
	}
}

func TestDefaultTTLIsFifteenMinutes(t *testing.T) {
	// A context is a short-lived assertion. The default is pinned because
	// lengthening it silently widens the replay window for every consumer.
	if got, want := DefaultTTL, 15*time.Minute; got != want {
		t.Errorf("DefaultTTL = %v, want %v", got, want)
	}
}

// --- headers ----------------------------------------------------------------

func TestMintStampsTheIdentityContextTokenType(t *testing.T) {
	// typ is defence in depth: it is what lets a verifier reject an access
	// token presented in the identity-context slot on its type rather than on
	// its signature, even when one key signs both.
	minter, _ := newTestMinter(t)
	token := mustMint(t, minter, validInput())

	if got := header(t, token)["typ"]; got != JWTType {
		t.Errorf("header typ = %v, want %q", got, JWTType)
	}
}

func TestMintStampsTheConfiguredKeyID(t *testing.T) {
	minter, _ := newTestMinter(t)
	token := mustMint(t, minter, validInput())

	if got := header(t, token)["kid"]; got != testKeyID {
		t.Errorf("header kid = %v, want %q", got, testKeyID)
	}
}

func TestMintHeaderAlgFollowsTheSigner(t *testing.T) {
	t.Run("ES256", func(t *testing.T) {
		minter, signer := newTestMinter(t)
		token := mustMint(t, minter, validInput())

		if got, want := header(t, token)["alg"], signer.SigningAlgorithm(); got != want {
			t.Errorf("header alg = %v, want %q", got, want)
		}
	})

	t.Run("RS256", func(t *testing.T) {
		signer := newRS256Signer(t)
		minter, err := NewMinter(signer, testKeyID, testIssuer)
		if err != nil {
			t.Fatalf("NewMinter returned error: %v", err)
		}
		token := mustMint(t, minter, validInput())

		if got, want := header(t, token)["alg"], signer.SigningAlgorithm(); got != want {
			t.Errorf("header alg = %v, want %q", got, want)
		}
		if got := header(t, token)["typ"]; got != JWTType {
			t.Errorf("header typ = %v, want %q", got, JWTType)
		}
	})
}

// --- claims -----------------------------------------------------------------

func TestMintStampsTheEmittedSchemaVersion(t *testing.T) {
	minter, _ := newTestMinter(t)
	token := mustMint(t, minter, validInput())

	if got, want := numberClaim(t, payload(t, token), "ctx_ver"), float64(Version); got != want {
		t.Errorf("ctx_ver = %v, want %v", got, want)
	}
}

func TestMintStampsTheConfiguredIssuer(t *testing.T) {
	minter, _ := newTestMinter(t)
	token := mustMint(t, minter, validInput())

	if got := stringClaim(t, payload(t, token), "iss"); got != testIssuer {
		t.Errorf("iss = %q, want %q", got, testIssuer)
	}
}

func TestMintCarriesTheCallerSuppliedSubjectData(t *testing.T) {
	minter, _ := newTestMinter(t)
	in := validInput()
	claims := payload(t, mustMint(t, minter, in))

	if got := stringClaim(t, claims, "sub"); got != in.Subject {
		t.Errorf("sub = %q, want %q", got, in.Subject)
	}
	if got := stringClaim(t, claims, "tnt"); got != in.Tenant {
		t.Errorf("tnt = %q, want %q", got, in.Tenant)
	}
	if got := stringClaim(t, claims, "aal"); got != in.Assurance {
		t.Errorf("aal = %q, want %q", got, in.Assurance)
	}
	if got := stringClaim(t, claims, "jti"); got != in.TokenID {
		t.Errorf("jti = %q, want %q", got, in.TokenID)
	}
	if got := stringClaim(t, claims, "tok_ref"); got != in.TokenRef {
		t.Errorf("tok_ref = %q, want %q", got, in.TokenRef)
	}

	raw, ok := claims["ent"].([]any)
	if !ok {
		t.Fatalf("ent = %v (%T), want an array", claims["ent"], claims["ent"])
	}
	got := make([]string, 0, len(raw))
	for _, v := range raw {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("ent element = %v (%T), want a string", v, v)
		}
		got = append(got, s)
	}
	if !slices.Equal(got, in.Entitlements) {
		t.Errorf("ent = %v, want %v", got, in.Entitlements)
	}
}

func TestMintEmitsAnEmptyEntitlementArrayRatherThanNull(t *testing.T) {
	// "No entitlements" is a normal outcome and stays distinguishable on the
	// wire from "this issuer does not speak the claim". A consumer that mirrors
	// the schema by hand reads a JSON array; null makes it work harder for no
	// reason and invites a null-check that quietly becomes a permit.
	minter, _ := newTestMinter(t)

	for name, entitlements := range map[string][]string{
		"nil slice":   nil,
		"empty slice": {},
	} {
		t.Run(name, func(t *testing.T) {
			in := validInput()
			in.Entitlements = entitlements
			claims := payload(t, mustMint(t, minter, in))

			value, ok := claims["ent"]
			if !ok {
				t.Fatal("ent key is absent; it must be emitted even with no entitlements")
			}
			list, ok := value.([]any)
			if !ok {
				t.Fatalf("ent = %v (%T), want an empty array", value, value)
			}
			if len(list) != 0 {
				t.Errorf("ent = %v, want an empty array", list)
			}
		})
	}
}

func TestMintCarriesNoCorrelationHandlesWhenNoneAreSupplied(t *testing.T) {
	// Both correlation claims are optional input, but they leave the minter
	// differently, and the difference is inherited from the frozen wire format
	// rather than chosen here: jti is omitempty and disappears, tok_ref is not
	// and is always emitted. Whichever way a claim goes, the minter must not
	// invent a handle for a context that has none.
	minter, _ := newTestMinter(t)
	in := validInput()
	in.TokenID = ""
	in.TokenRef = ""

	claims := payload(t, mustMint(t, minter, in))

	if value, ok := claims["jti"]; ok {
		t.Errorf("jti = %v, want the key to be absent", value)
	}
	value, ok := claims["tok_ref"]
	if !ok {
		t.Fatal("tok_ref key is absent; the frozen wire format emits it unconditionally")
	}
	if value != "" {
		t.Errorf("tok_ref = %v, want an empty string; a context with no upstream credential has no handle", value)
	}
}

func TestMintDoesNotAliasTheCallerEntitlementSlice(t *testing.T) {
	minter, _ := newTestMinter(t)
	in := validInput()
	in.Entitlements = []string{"reports.read"}

	token := mustMint(t, minter, in)
	in.Entitlements[0] = "reports.write"

	claims := payload(t, token)
	list, _ := claims["ent"].([]any)
	if len(list) != 1 || list[0] != "reports.read" {
		t.Errorf("ent = %v, want [reports.read]; a minted token must not observe a later caller mutation", list)
	}
}

// --- lifetime ---------------------------------------------------------------

func TestMintExpiryIsExactlyTheConfiguredTTL(t *testing.T) {
	for name, ttl := range map[string]time.Duration{
		"default":        DefaultTTL,
		"one hour":       time.Hour,
		"thirty seconds": 30 * time.Second,
	} {
		t.Run(name, func(t *testing.T) {
			var opts []MinterOption
			if ttl != DefaultTTL {
				opts = append(opts, WithTTL(ttl))
			}
			minter, _ := newTestMinter(t, opts...)
			claims := payload(t, mustMint(t, minter, validInput()))

			iat := numberClaim(t, claims, "iat")
			exp := numberClaim(t, claims, "exp")
			if got, want := exp-iat, ttl.Seconds(); got != want {
				t.Errorf("exp-iat = %v seconds, want %v", got, want)
			}
		})
	}
}

func TestMintStampsIssuedAtFromTheClock(t *testing.T) {
	frozen := time.Date(2030, time.January, 2, 3, 4, 5, 0, time.UTC)
	minter, _ := newTestMinter(t, WithClock(func() time.Time { return frozen }))

	claims := payload(t, mustMint(t, minter, validInput()))
	if got, want := numberClaim(t, claims, "iat"), float64(frozen.Unix()); got != want {
		t.Errorf("iat = %v, want %v", got, want)
	}
	if got, want := numberClaim(t, claims, "exp"), float64(frozen.Add(DefaultTTL).Unix()); got != want {
		t.Errorf("exp = %v, want %v", got, want)
	}
}

func TestMintStampsIssuedAtFromTheWallClockByDefault(t *testing.T) {
	minter, _ := newTestMinter(t)

	before := time.Now().Add(-time.Second).Unix()
	claims := payload(t, mustMint(t, minter, validInput()))
	after := time.Now().Add(time.Second).Unix()

	iat := int64(numberClaim(t, claims, "iat"))
	if iat < before || iat > after {
		t.Errorf("iat = %d, want it within [%d, %d]", iat, before, after)
	}
}

// --- required caller input --------------------------------------------------

func TestMintRequiresAuthenticatedSubjectData(t *testing.T) {
	minter, _ := newTestMinter(t)

	tests := []struct {
		name    string
		degrade func(*MintInput)
		want    error
	}{
		{"no subject", func(in *MintInput) { in.Subject = "" }, ErrMissingSubject},
		{"no tenant", func(in *MintInput) { in.Tenant = "" }, ErrMissingTenant},
		{"no assurance", func(in *MintInput) { in.Assurance = "" }, ErrMissingAssurance},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := validInput()
			tt.degrade(&in)

			token, err := minter.Mint(in)
			if !errors.Is(err, tt.want) {
				t.Errorf("Mint error = %v, want %v", err, tt.want)
			}
			if token != "" {
				t.Errorf("Mint returned a token (%q) alongside an error", token)
			}
		})
	}
}

func TestMintRejectsTheZeroValueInput(t *testing.T) {
	// The zero value carries no authenticated subject data at all. There is no
	// path that turns it into a context.
	minter, _ := newTestMinter(t)

	token, err := minter.Mint(MintInput{})
	if err == nil {
		t.Fatal("Mint(MintInput{}) returned no error; a context with no subject data must never be minted")
	}
	if token != "" {
		t.Errorf("Mint returned a token (%q) alongside an error", token)
	}
}

func TestMintDoesNotDefaultAssuranceUpward(t *testing.T) {
	// The headline honesty property. An absent assurance level is an error and
	// never a silently chosen one: the claim lands in a downstream audit record
	// where an inflated value is indistinguishable from a real one.
	minter, _ := newTestMinter(t)
	in := validInput()
	in.Assurance = ""

	token, err := minter.Mint(in)
	if !errors.Is(err, ErrMissingAssurance) {
		t.Fatalf("Mint(no assurance) error = %v, want %v", err, ErrMissingAssurance)
	}
	if token != "" {
		t.Fatalf("Mint minted a context with no supplied assurance level: %q", token)
	}
}

func TestMintReportsEachAssuranceLevelVerbatim(t *testing.T) {
	// Single-factor input produces a single-factor claim. Rounding AAL1 up to
	// AAL2 would be indistinguishable, downstream, from a real second factor.
	minter, _ := newTestMinter(t)

	for _, level := range []string{AAL1, AAL2, AAL3} {
		t.Run(level, func(t *testing.T) {
			in := validInput()
			in.Assurance = level

			if got := stringClaim(t, payload(t, mustMint(t, minter, in)), "aal"); got != level {
				t.Errorf("aal = %q, want %q", got, level)
			}
		})
	}
}

func TestMintRejectsAnUnrecognisedAssuranceLevel(t *testing.T) {
	// The ladder is fixed. A free-text level cannot be compared against a
	// policy threshold, so a consumer either drops it or guesses; neither is
	// acceptable for a value an audit record is built from.
	for _, level := range []string{"aal2", "AAL4", "high", "2"} {
		t.Run(level, func(t *testing.T) {
			minter, _ := newTestMinter(t)
			in := validInput()
			in.Assurance = level

			token, err := minter.Mint(in)
			if !errors.Is(err, ErrInvalidAssurance) {
				t.Errorf("Mint(assurance %q) error = %v, want %v", level, err, ErrInvalidAssurance)
			}
			if token != "" {
				t.Errorf("Mint returned a token (%q) alongside an error", token)
			}
		})
	}
}

func TestMintRejectsATokenRefThatIsNotADerivedHandle(t *testing.T) {
	// tok_ref must be the output of TokRef, never the upstream credential
	// itself. A raw credential stamped here would be signed into a token that
	// crosses service boundaries and is logged by every hop along the way.
	minter, _ := newTestMinter(t)

	for name, ref := range map[string]string{
		"an upstream credential": "eyJhbGciOiJFUzI1NiJ9.e30.c2lnbmF0dXJl",
		"too short":              "7bcc8673",
		"too long":               "7bcc86737e4b80431",
		"uppercase hex":          "7BCC86737E4B8043",
		"not hex":                "zzcc86737e4b8043",
	} {
		t.Run(name, func(t *testing.T) {
			in := validInput()
			in.TokenRef = ref

			token, err := minter.Mint(in)
			if !errors.Is(err, ErrMalformedTokenRef) {
				t.Errorf("Mint(tok_ref %q) error = %v, want %v", ref, err, ErrMalformedTokenRef)
			}
			if token != "" {
				t.Errorf("Mint returned a token (%q) alongside an error", token)
			}
		})
	}
}

func TestMintAcceptsATokenRefDerivedFromTheUpstreamCredential(t *testing.T) {
	minter, _ := newTestMinter(t)
	in := validInput()
	in.TokenRef = TokRef("an-upstream-credential")

	if got := stringClaim(t, payload(t, mustMint(t, minter, in)), "tok_ref"); got != in.TokenRef {
		t.Errorf("tok_ref = %q, want %q", got, in.TokenRef)
	}
}

// --- the minted token is usable ---------------------------------------------

func TestMintedTokenVerifiesAndValidates(t *testing.T) {
	// End to end within this package: the signature checks out against the
	// signer's public half, the typed claims decode, and the result passes the
	// semantic validation a consumer applies.
	minter, signer := newTestMinter(t)
	in := validInput()
	token := mustMint(t, minter, in)

	var claims Claims
	parsed, err := jwt.ParseWithClaims(token, &claims, func(tok *jwt.Token) (any, error) {
		if got := tok.Header["kid"]; got != testKeyID {
			t.Errorf("header kid = %v, want %q", got, testKeyID)
		}
		return signer.Public(), nil
	}, jwt.WithValidMethods([]string{signer.SigningAlgorithm()}))
	if err != nil {
		t.Fatalf("jwt.ParseWithClaims returned error: %v", err)
	}
	if !parsed.Valid {
		t.Fatal("parsed token is not valid")
	}

	if err := claims.Validate(DefaultVersionSet()); err != nil {
		t.Errorf("Claims.Validate(DefaultVersionSet()) returned error: %v", err)
	}
	if got, want := claims.Issuer, testIssuer; got != want {
		t.Errorf("Issuer = %q, want %q", got, want)
	}
	if got, want := claims.Subject, in.Subject; got != want {
		t.Errorf("Subject = %q, want %q", got, want)
	}
	if got, want := claims.Tenant, in.Tenant; got != want {
		t.Errorf("Tenant = %q, want %q", got, want)
	}
	if got, want := claims.AAL, in.Assurance; got != want {
		t.Errorf("AAL = %q, want %q", got, want)
	}
	if got, want := claims.CtxVer, Version; got != want {
		t.Errorf("CtxVer = %d, want %d", got, want)
	}
	if !slices.Equal(claims.Entitlements, in.Entitlements) {
		t.Errorf("Entitlements = %v, want %v", claims.Entitlements, in.Entitlements)
	}
}

func TestMintedTokenIsRejectedByAnUnrelatedKey(t *testing.T) {
	// Guards against the signature being decorative: a context minted by one
	// signer must not verify against a key the issuer never held.
	minter, _ := newTestMinter(t)
	token := mustMint(t, minter, validInput())
	other := newES256Signer(t)

	_, err := jwt.ParseWithClaims(token, &Claims{}, func(*jwt.Token) (any, error) {
		return other.Public(), nil
	}, jwt.WithValidMethods([]string{"ES256"}))
	if !errors.Is(err, jwt.ErrTokenSignatureInvalid) {
		t.Errorf("parse with an unrelated key error = %v, want %v", err, jwt.ErrTokenSignatureInvalid)
	}
}

func TestMintIsSafeForConcurrentUse(t *testing.T) {
	// One Minter is process-wide infrastructure that every request path shares.
	minter, _ := newTestMinter(t)

	const goroutines = 16
	var wg sync.WaitGroup
	errs := make([]error, goroutines)
	tokens := make([]string, goroutines)

	wg.Add(goroutines)
	for i := range goroutines {
		go func() {
			defer wg.Done()
			tokens[i], errs[i] = minter.Mint(validInput())
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: Mint returned error: %v", i, err)
			continue
		}
		if got := header(t, tokens[i])["typ"]; got != JWTType {
			t.Errorf("goroutine %d: header typ = %v, want %q", i, got, JWTType)
		}
	}
}

// --- no unauthenticated mint path -------------------------------------------

func TestPackageHasNoBuildTaggedFiles(t *testing.T) {
	// A mint path that fabricates a subject must not be reachable behind a
	// build tag. Build-constrained files are invisible to the default test
	// run, so the ordinary suite cannot observe such a path even in principle.
	// This check is what notices the file appearing at all.
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("os.ReadDir(.) returned error: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		source, err := os.ReadFile(entry.Name())
		if err != nil {
			t.Fatalf("os.ReadFile(%s) returned error: %v", entry.Name(), err)
		}
		// A constraint is only meaningful above the package clause.
		for _, line := range strings.Split(string(source), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "package ") {
				break
			}
			if strings.HasPrefix(line, "//go:build") || strings.HasPrefix(line, "// +build") {
				t.Errorf("%s carries the build constraint %q; this package must not gain build-tagged files", entry.Name(), line)
			}
		}
	}
}

// The test signers must satisfy the signing seam the minter is built around.
var (
	_ kmssigner.Signer = (*ecdsaTestSigner)(nil)
	_ kmssigner.Signer = (*rsaTestSigner)(nil)
)
