package identity

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Cache settings the remote tests share. The floor is deliberately shorter
// than the lifetime so the two refetch triggers can be exercised one at a
// time: advancing past the floor alone isolates the unknown-kid trigger, and
// advancing past the lifetime isolates the expiry trigger.
const (
	testCacheTTL     = 10 * time.Minute
	testRefetchFloor = 1 * time.Minute
)

// testClock is a controllable time source. TTL expiry is a behaviour worth
// pinning, and pinning it by sleeping would make the suite slow and flaky, so
// the tests move time themselves. It is mutex-guarded because the concurrency
// tests read it from many goroutines at once.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func newTestClock() *testClock {
	return &testClock{now: time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC)}
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// jwksServer is a stand-in issuer endpoint that counts how many times its key
// set was actually fetched.
//
// The counter is the point. Asserting that a lookup returned a key says
// nothing about whether the cache did its job; asserting how many upstream
// requests the lookup caused is what distinguishes a cache hit from a refetch,
// and a bounded refetch from a request amplifier.
type jwksServer struct {
	server  *httptest.Server
	fetches atomic.Int64

	mu     sync.Mutex
	status int
	body   []byte
}

// newJWKSServer starts a server on an OS-assigned port. The port is never
// chosen by the test: a fixed port collides with whatever else is listening
// and makes the suite fail for reasons that have nothing to do with the code.
func newJWKSServer(t *testing.T, body []byte) *jwksServer {
	t.Helper()
	s := &jwksServer{status: http.StatusOK, body: body}
	s.server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.server.Close)
	return s
}

func (s *jwksServer) serve(w http.ResponseWriter, _ *http.Request) {
	s.fetches.Add(1)
	s.mu.Lock()
	status, body := s.status, s.body
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/jwk-set+json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func (s *jwksServer) serveDocument(body []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status, s.body = http.StatusOK, body
}

func (s *jwksServer) serveStatus(status int, body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status, s.body = status, []byte(body)
}

func (s *jwksServer) fetchCount() int64 { return s.fetches.Load() }

func rawURL(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func jwksDocument(t *testing.T, entries ...map[string]any) []byte {
	t.Helper()
	if entries == nil {
		entries = []map[string]any{}
	}
	body, err := json.Marshal(map[string]any{"keys": entries})
	if err != nil {
		t.Fatalf("json.Marshal(key set) returned error: %v", err)
	}
	return body
}

// ecJWK encodes a P-256 key the way RFC 7518 section 6.2.1.2 asks for it, with
// each coordinate zero-left-padded to the full width of the curve.
func ecJWK(kid string, pub *ecdsa.PublicKey) map[string]any {
	return map[string]any{
		"kty": "EC",
		"crv": "P-256",
		"kid": kid,
		"use": "sig",
		"alg": "ES256",
		"x":   rawURL(pub.X.FillBytes(make([]byte, 32))),
		"y":   rawURL(pub.Y.FillBytes(make([]byte, 32))),
	}
}

// unpaddedECJWK encodes a P-256 key the way an issuer that reaches for
// big.Int.Bytes() encodes it, which drops leading zero bytes and so emits a
// coordinate narrower than the curve whenever the high byte happens to be
// zero. Both encodings describe the same point and both are found in the wild.
func unpaddedECJWK(kid string, pub *ecdsa.PublicKey) map[string]any {
	return map[string]any{
		"kty": "EC",
		"crv": "P-256",
		"kid": kid,
		"x":   rawURL(pub.X.Bytes()),
		"y":   rawURL(pub.Y.Bytes()),
	}
}

func rsaJWK(kid string, pub *rsa.PublicKey) map[string]any {
	return map[string]any{
		"kty": "RSA",
		"kid": kid,
		"use": "sig",
		"alg": "RS256",
		"n":   rawURL(pub.N.Bytes()),
		"e":   rawURL(big.NewInt(int64(pub.E)).Bytes()),
	}
}

func newECKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey returned error: %v", err)
	}
	return key
}

// newShortCoordinateECKey returns a P-256 key at least one of whose
// coordinates is narrower than 32 bytes once its leading zero byte is dropped.
// Roughly one key in 128 qualifies, so the search is short; it is a search
// rather than a fixture because a hand-written point is not obviously on the
// curve and would prove less.
func newShortCoordinateECKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	const attempts = 20000
	for range attempts {
		key := newECKey(t)
		if len(key.PublicKey.X.Bytes()) < 32 || len(key.PublicKey.Y.Bytes()) < 32 {
			return key
		}
	}
	t.Fatalf("no P-256 key with a short coordinate in %d attempts", attempts)
	return nil
}

func newRSAKey(t *testing.T, bits int) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		t.Fatalf("rsa.GenerateKey(%d) returned error: %v", bits, err)
	}
	return key
}

func newRemote(t *testing.T, s *jwksServer, clock *testClock, opts ...RemoteJWKSOption) *RemoteJWKS {
	t.Helper()
	base := []RemoteJWKSOption{
		WithJWKSHTTPClient(s.server.Client()),
		WithJWKSClock(clock.Now),
		WithJWKSCacheTTL(testCacheTTL),
		WithJWKSMinRefetchInterval(testRefetchFloor),
	}
	r, err := NewRemoteJWKS(s.server.URL, append(base, opts...)...)
	if err != nil {
		t.Fatalf("NewRemoteJWKS returned error: %v", err)
	}
	return r
}

func mustKey(t *testing.T, src KeySource, kid string) crypto.PublicKey {
	t.Helper()
	key, err := src.KeyForKID(context.Background(), kid)
	if err != nil {
		t.Fatalf("KeyForKID(%q) returned error: %v", kid, err)
	}
	if key == nil {
		t.Fatalf("KeyForKID(%q) returned a nil key and a nil error", kid)
	}
	return key
}

func wantUnknownKID(t *testing.T, src KeySource, kid string) {
	t.Helper()
	key, err := src.KeyForKID(context.Background(), kid)
	if !errors.Is(err, ErrUnknownKeyID) {
		t.Fatalf("KeyForKID(%q) error = %v, want ErrUnknownKeyID", kid, err)
	}
	if key != nil {
		t.Fatalf("KeyForKID(%q) returned key %v alongside an error, want nil", kid, key)
	}
}

func assertFetches(t *testing.T, s *jwksServer, want int64, when string) {
	t.Helper()
	if got := s.fetchCount(); got != want {
		t.Fatalf("upstream fetches %s = %d, want %d", when, got, want)
	}
}

// assertVerifies proves the resolved key is the published key rather than
// merely a well-formed one, by checking it against a signature only the
// matching private half could have produced. A coordinate rebuilt with the
// wrong width would still be a usable ecdsa.PublicKey; it just would not
// verify anything.
func assertVerifies(t *testing.T, priv *ecdsa.PrivateKey, resolved crypto.PublicKey) {
	t.Helper()
	pub, ok := resolved.(*ecdsa.PublicKey)
	if !ok {
		t.Fatalf("resolved key is %T, want *ecdsa.PublicKey", resolved)
	}
	digest := sha256.Sum256([]byte("identity context signing input"))
	sig, err := ecdsa.SignASN1(rand.Reader, priv, digest[:])
	if err != nil {
		t.Fatalf("ecdsa.SignASN1 returned error: %v", err)
	}
	if !ecdsa.VerifyASN1(pub, digest[:], sig) {
		t.Fatal("the resolved key does not verify a signature made by the published key")
	}
}

func assertDoesNotVerify(t *testing.T, priv *ecdsa.PrivateKey, resolved crypto.PublicKey) {
	t.Helper()
	pub, ok := resolved.(*ecdsa.PublicKey)
	if !ok {
		t.Fatalf("resolved key is %T, want *ecdsa.PublicKey", resolved)
	}
	digest := sha256.Sum256([]byte("identity context signing input"))
	sig, err := ecdsa.SignASN1(rand.Reader, priv, digest[:])
	if err != nil {
		t.Fatalf("ecdsa.SignASN1 returned error: %v", err)
	}
	if ecdsa.VerifyASN1(pub, digest[:], sig) {
		t.Fatal("the resolved key verifies a signature from the superseded key")
	}
}

func TestStaticKeysAndRemoteJWKSAreKeySources(t *testing.T) {
	// The verifier depends on the seam, not on either implementation. If this
	// stops compiling, a consumer can no longer swap one for the other.
	var _ KeySource = (*StaticKeys)(nil)
	var _ KeySource = (*RemoteJWKS)(nil)
}

func TestStaticKeySourceResolvesAConfiguredKID(t *testing.T) {
	key := newECKey(t)
	src := NewStaticKeys(map[string]crypto.PublicKey{"in-process": &key.PublicKey})

	assertVerifies(t, key, mustKey(t, src, "in-process"))
}

func TestStaticKeySourceRejectsAnAbsentKID(t *testing.T) {
	key := newECKey(t)
	src := NewStaticKeys(map[string]crypto.PublicKey{"in-process": &key.PublicKey})

	wantUnknownKID(t, src, "some-other-key")
}

func TestStaticKeySourceRejectsAnEmptyKID(t *testing.T) {
	// A token with no kid header must not resolve to a key by accident, even
	// if something managed to file one under the empty string.
	src := NewStaticKeys(map[string]crypto.PublicKey{"": nil})

	wantUnknownKID(t, src, "")
}

func TestStaticKeySourceRejectsEveryKIDWhenBuiltFromNothing(t *testing.T) {
	for name, src := range map[string]*StaticKeys{
		"nil map":   NewStaticKeys(nil),
		"empty map": NewStaticKeys(map[string]crypto.PublicKey{}),
	} {
		t.Run(name, func(t *testing.T) {
			wantUnknownKID(t, src, "anything")
		})
	}
}

func TestStaticKeySourceDoesNotAliasTheCallerMap(t *testing.T) {
	// A trust set the caller can still edit after construction is a trust set
	// an unrelated code path can widen at run time.
	key := newECKey(t)
	supplied := map[string]crypto.PublicKey{"in-process": &key.PublicKey}
	src := NewStaticKeys(supplied)

	intruder := newECKey(t)
	supplied["smuggled"] = &intruder.PublicKey
	delete(supplied, "in-process")

	wantUnknownKID(t, src, "smuggled")
	assertVerifies(t, key, mustKey(t, src, "in-process"))
}

func TestStaticKeySourceIsSafeForConcurrentUse(t *testing.T) {
	key := newECKey(t)
	src := NewStaticKeys(map[string]crypto.PublicKey{"in-process": &key.PublicKey})

	var wg sync.WaitGroup
	for i := range 32 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for range 25 {
				if i%2 == 0 {
					if _, err := src.KeyForKID(context.Background(), "in-process"); err != nil {
						t.Errorf("KeyForKID returned error: %v", err)
						return
					}
					continue
				}
				if _, err := src.KeyForKID(context.Background(), "absent"); !errors.Is(err, ErrUnknownKeyID) {
					t.Errorf("KeyForKID error = %v, want ErrUnknownKeyID", err)
					return
				}
			}
		}(i)
	}
	wg.Wait()
}

func TestRemoteKeySourceRejectsUnusableConfiguration(t *testing.T) {
	for name, tc := range map[string]struct {
		url  string
		opts []RemoteJWKSOption
		want error
	}{
		"empty url": {
			url:  "",
			want: ErrMissingJWKSURL,
		},
		"relative url": {
			url:  "/.well-known/jwks.json",
			want: ErrInvalidJWKSURL,
		},
		"non http scheme": {
			url:  "ftp://issuer.example/jwks.json",
			want: ErrInvalidJWKSURL,
		},
		"nil http client": {
			url:  "https://issuer.example/jwks.json",
			opts: []RemoteJWKSOption{WithJWKSHTTPClient(nil)},
			want: ErrNilHTTPClient,
		},
		"zero cache lifetime": {
			url:  "https://issuer.example/jwks.json",
			opts: []RemoteJWKSOption{WithJWKSCacheTTL(0)},
			want: ErrInvalidCacheTTL,
		},
		"negative cache lifetime": {
			url:  "https://issuer.example/jwks.json",
			opts: []RemoteJWKSOption{WithJWKSCacheTTL(-time.Second)},
			want: ErrInvalidCacheTTL,
		},
		"zero refetch floor": {
			url:  "https://issuer.example/jwks.json",
			opts: []RemoteJWKSOption{WithJWKSMinRefetchInterval(0)},
			want: ErrInvalidRefetchInterval,
		},
		"refetch floor longer than the cache lifetime": {
			url: "https://issuer.example/jwks.json",
			opts: []RemoteJWKSOption{
				WithJWKSCacheTTL(time.Minute),
				WithJWKSMinRefetchInterval(time.Hour),
			},
			want: ErrInvalidRefetchInterval,
		},
		"nil clock": {
			url:  "https://issuer.example/jwks.json",
			opts: []RemoteJWKSOption{WithJWKSClock(nil)},
			want: ErrInvalidClock,
		},
	} {
		t.Run(name, func(t *testing.T) {
			r, err := NewRemoteJWKS(tc.url, tc.opts...)
			if !errors.Is(err, tc.want) {
				t.Fatalf("NewRemoteJWKS error = %v, want %v", err, tc.want)
			}
			if r != nil {
				t.Fatal("NewRemoteJWKS returned a source alongside an error, want nil")
			}
		})
	}
}

func TestRemoteKeySourceDefaultsAreUsable(t *testing.T) {
	r, err := NewRemoteJWKS("https://issuer.example/.well-known/jwks.json")
	if err != nil {
		t.Fatalf("NewRemoteJWKS returned error: %v", err)
	}
	if r.ttl != DefaultJWKSCacheTTL {
		t.Errorf("cache lifetime = %v, want %v", r.ttl, DefaultJWKSCacheTTL)
	}
	if r.minRefetch != DefaultJWKSMinRefetchInterval {
		t.Errorf("refetch floor = %v, want %v", r.minRefetch, DefaultJWKSMinRefetchInterval)
	}
	if r.minRefetch > r.ttl {
		t.Errorf("refetch floor %v exceeds cache lifetime %v, which would throttle scheduled refresh", r.minRefetch, r.ttl)
	}
	if r.now == nil {
		t.Error("clock is nil")
	}
	// A fetch runs while the cache lock is held, so a client that can hang
	// forever is a client that can wedge every lookup in the process.
	if r.client == nil {
		t.Fatal("http client is nil")
	}
	if r.client.Timeout <= 0 {
		t.Errorf("default http client timeout = %v, want a positive bound", r.client.Timeout)
	}
	if r.client == http.DefaultClient {
		t.Error("default http client is http.DefaultClient, which has no timeout and is shared process-wide")
	}
}

func TestRemoteKeySourceFetchesAndParsesAnECKeySet(t *testing.T) {
	key := newECKey(t)
	server := newJWKSServer(t, jwksDocument(t, ecJWK("ec-1", &key.PublicKey)))
	r := newRemote(t, server, newTestClock())

	assertVerifies(t, key, mustKey(t, r, "ec-1"))
	assertFetches(t, server, 1, "after one lookup")
}

func TestRemoteKeySourceFetchesAndParsesAnRSAKeySet(t *testing.T) {
	key := newRSAKey(t, 2048)
	server := newJWKSServer(t, jwksDocument(t, rsaJWK("rsa-1", &key.PublicKey)))
	r := newRemote(t, server, newTestClock())

	resolved, ok := mustKey(t, r, "rsa-1").(*rsa.PublicKey)
	if !ok {
		t.Fatal("resolved key is not *rsa.PublicKey")
	}
	if !resolved.Equal(&key.PublicKey) {
		t.Fatal("resolved RSA key does not equal the published key")
	}
}

func TestRemoteKeySourceAcceptsACoordinateNarrowerThanTheCurve(t *testing.T) {
	// RFC 7518 says a coordinate SHOULD be zero-left-padded to the full width
	// of the curve, and plenty of issuers do. Plenty of others encode with
	// big.Int.Bytes(), which drops the leading zero byte about one time in
	// 128. Rejecting the short form fails on a valid key set, intermittently,
	// and for reasons that look like nothing at all from the outside.
	key := newShortCoordinateECKey(t)
	if len(key.PublicKey.X.Bytes()) == 32 && len(key.PublicKey.Y.Bytes()) == 32 {
		t.Fatal("test key has full-width coordinates, so this would not exercise the short encoding")
	}

	server := newJWKSServer(t, jwksDocument(t, unpaddedECJWK("short", &key.PublicKey)))
	r := newRemote(t, server, newTestClock())

	assertVerifies(t, key, mustKey(t, r, "short"))
}

func TestRemoteKeySourceCachesAKnownKIDAcrossLookups(t *testing.T) {
	key := newECKey(t)
	server := newJWKSServer(t, jwksDocument(t, ecJWK("ec-1", &key.PublicKey)))
	r := newRemote(t, server, newTestClock())

	mustKey(t, r, "ec-1")
	mustKey(t, r, "ec-1")
	mustKey(t, r, "ec-1")

	assertFetches(t, server, 1, "after three lookups of a known kid")
}

func TestRemoteKeySourceRefetchesForAnUnknownKID(t *testing.T) {
	// Rotation that introduces a new kid is the ordinary case: the cache
	// misses, and a miss has to be able to reach the issuer or the new key is
	// never learned.
	first := newECKey(t)
	server := newJWKSServer(t, jwksDocument(t, ecJWK("ec-1", &first.PublicKey)))
	clock := newTestClock()
	r := newRemote(t, server, clock)

	mustKey(t, r, "ec-1")
	assertFetches(t, server, 1, "after the priming lookup")

	second := newECKey(t)
	server.serveDocument(jwksDocument(t,
		ecJWK("ec-1", &first.PublicKey),
		ecJWK("ec-2", &second.PublicKey),
	))

	clock.Advance(testRefetchFloor)
	assertVerifies(t, second, mustKey(t, r, "ec-2"))
	assertFetches(t, server, 2, "after a lookup of a newly published kid")
}

func TestRemoteKeySourceDoesNotRefetchForAnUnknownKIDInsideTheFloor(t *testing.T) {
	key := newECKey(t)
	server := newJWKSServer(t, jwksDocument(t, ecJWK("ec-1", &key.PublicKey)))
	clock := newTestClock()
	r := newRemote(t, server, clock)

	mustKey(t, r, "ec-1")
	assertFetches(t, server, 1, "after the priming lookup")

	clock.Advance(testRefetchFloor)
	wantUnknownKID(t, r, "absent")
	assertFetches(t, server, 2, "after the first unknown kid")

	clock.Advance(testRefetchFloor - time.Second)
	wantUnknownKID(t, r, "absent")
	wantUnknownKID(t, r, "absent-too")
	assertFetches(t, server, 2, "after further unknown kids inside the floor")

	clock.Advance(time.Second)
	wantUnknownKID(t, r, "absent")
	assertFetches(t, server, 3, "after the floor elapsed again")
}

func TestRemoteKeySourceDoesNotAmplifyUnknownKIDsIntoUpstreamRequests(t *testing.T) {
	// A kid is attacker-supplied: it arrives in the header of whatever token
	// was presented. Without a floor, a stream of made-up kids becomes a
	// stream of requests aimed at the issuer, with this process as the pump.
	key := newECKey(t)
	server := newJWKSServer(t, jwksDocument(t, ecJWK("ec-1", &key.PublicKey)))
	clock := newTestClock()
	r := newRemote(t, server, clock)

	mustKey(t, r, "ec-1")
	clock.Advance(testRefetchFloor)

	for i := range 500 {
		wantUnknownKID(t, r, "forged-"+string(rune('a'+i%26))+string(rune('a'+i/26%26)))
	}

	assertFetches(t, server, 2, "after 500 distinct unknown kids")

	// The floor must not have cost the process its working key.
	assertVerifies(t, key, mustKey(t, r, "ec-1"))
}

func TestRemoteKeySourceRefetchesAfterTheCacheTTLExpires(t *testing.T) {
	// The trap. An issuer that publishes a fixed kid and regenerates the key
	// behind it -- a restart with an ephemeral key does exactly this -- never
	// produces a cache miss, because the kid never changes. A cache that only
	// refetches on an unknown kid therefore keeps verifying against a public
	// key whose private half is gone, and every request fails from then on
	// with nothing to trigger recovery. Expiry is what heals it.
	superseded := newECKey(t)
	server := newJWKSServer(t, jwksDocument(t, ecJWK("fixed", &superseded.PublicKey)))
	clock := newTestClock()
	r := newRemote(t, server, clock)

	assertVerifies(t, superseded, mustKey(t, r, "fixed"))
	assertFetches(t, server, 1, "after the priming lookup")

	current := newECKey(t)
	server.serveDocument(jwksDocument(t, ecJWK("fixed", &current.PublicKey)))

	// Still inside the lifetime: the kid is known, so nothing prompts a fetch
	// and the superseded key is still what resolves.
	clock.Advance(testCacheTTL - time.Second)
	assertVerifies(t, superseded, mustKey(t, r, "fixed"))
	assertFetches(t, server, 1, "while the cached set is still fresh")

	clock.Advance(time.Second)
	resolved := mustKey(t, r, "fixed")
	assertFetches(t, server, 2, "once the cached set expired")
	assertVerifies(t, current, resolved)
	assertDoesNotVerify(t, superseded, resolved)
}

func TestRemoteKeySourceKeepsTheCachedSetWhenTheIssuerRespondsNon2xx(t *testing.T) {
	key := newECKey(t)
	server := newJWKSServer(t, jwksDocument(t, ecJWK("ec-1", &key.PublicKey)))
	clock := newTestClock()
	r := newRemote(t, server, clock)

	assertVerifies(t, key, mustKey(t, r, "ec-1"))

	server.serveStatus(http.StatusInternalServerError, "upstream is unwell")
	clock.Advance(testCacheTTL)

	if _, err := r.KeyForKID(context.Background(), "ec-1"); !errors.Is(err, ErrJWKSUnavailable) {
		t.Fatalf("KeyForKID error = %v, want ErrJWKSUnavailable", err)
	}
	assertFetches(t, server, 2, "after the failed refresh")

	// A failed refresh must not have emptied the cache. Inside the floor no
	// further request is made, so what comes back can only be the cached key.
	assertVerifies(t, key, mustKey(t, r, "ec-1"))
	assertFetches(t, server, 2, "after a lookup served from the surviving cache")

	// And the source recovers on its own once the issuer does.
	server.serveDocument(jwksDocument(t, ecJWK("ec-1", &key.PublicKey)))
	clock.Advance(testRefetchFloor)
	assertVerifies(t, key, mustKey(t, r, "ec-1"))
	assertFetches(t, server, 3, "after the issuer recovered")
}

func TestRemoteKeySourceKeepsTheCachedSetWhenTheBodyIsUnusable(t *testing.T) {
	key := newECKey(t)
	good := jwksDocument(t, ecJWK("ec-1", &key.PublicKey))

	for name, body := range map[string]string{
		"not json":            "this is not a key set",
		"truncated json":      `{"keys":[`,
		"keys is not a list":  `{"keys":"nope"}`,
		"no keys member":      `{}`,
		"empty key list":      `{"keys":[]}`,
		"null key list":       `{"keys":null}`,
		"no usable key types": `{"keys":[{"kty":"OKP","crv":"Ed25519","kid":"x","x":"aaaa"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			server := newJWKSServer(t, good)
			clock := newTestClock()
			r := newRemote(t, server, clock)

			assertVerifies(t, key, mustKey(t, r, "ec-1"))

			server.serveStatus(http.StatusOK, body)
			clock.Advance(testCacheTTL)

			if _, err := r.KeyForKID(context.Background(), "ec-1"); !errors.Is(err, ErrJWKSMalformed) {
				t.Fatalf("KeyForKID error = %v, want ErrJWKSMalformed", err)
			}

			assertVerifies(t, key, mustKey(t, r, "ec-1"))
			assertFetches(t, server, 2, "after an unusable body")
		})
	}
}

func TestRemoteKeySourceSurfacesATransportError(t *testing.T) {
	key := newECKey(t)
	server := newJWKSServer(t, jwksDocument(t, ecJWK("ec-1", &key.PublicKey)))
	clock := newTestClock()
	r := newRemote(t, server, clock)

	assertVerifies(t, key, mustKey(t, r, "ec-1"))

	server.server.Close()
	clock.Advance(testCacheTTL)

	if _, err := r.KeyForKID(context.Background(), "ec-1"); !errors.Is(err, ErrJWKSUnavailable) {
		t.Fatalf("KeyForKID error = %v, want ErrJWKSUnavailable", err)
	}

	// Unreachable is not the same as untrusted: the set already fetched is
	// still the issuer's set, and dropping it would turn a blip into an outage.
	assertVerifies(t, key, mustKey(t, r, "ec-1"))
}

func TestRemoteKeySourceHonoursACancelledContext(t *testing.T) {
	key := newECKey(t)
	server := newJWKSServer(t, jwksDocument(t, ecJWK("ec-1", &key.PublicKey)))
	r := newRemote(t, server, newTestClock())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := r.KeyForKID(ctx, "ec-1")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("KeyForKID error = %v, want it to wrap context.Canceled", err)
	}
	if !errors.Is(err, ErrJWKSUnavailable) {
		t.Fatalf("KeyForKID error = %v, want it to wrap ErrJWKSUnavailable", err)
	}
}

func TestRemoteKeySourceRejectsAnEmptyKIDWithoutFetching(t *testing.T) {
	// An empty kid can never match an entry, so reaching for the network on
	// one is a free amplification path for a token with no kid header at all.
	key := newECKey(t)
	server := newJWKSServer(t, jwksDocument(t, ecJWK("ec-1", &key.PublicKey)))
	r := newRemote(t, server, newTestClock())

	wantUnknownKID(t, r, "")
	assertFetches(t, server, 0, "after a lookup of the empty kid")
}

func TestRemoteKeySourceSkipsEntriesItCannotUse(t *testing.T) {
	// An issuer publishes one key set for every consumer it has. A curve or a
	// key type this package does not verify with is not a broken document, and
	// discarding the whole set over one such entry would take down every
	// consumer of the keys that are usable.
	ec := newECKey(t)
	rsaKey := newRSAKey(t, 2048)

	p384, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey(P-384) returned error: %v", err)
	}
	weakRSA := newRSAKey(t, 1024)

	encryptionOnly := ecJWK("encryption-only", &ec.PublicKey)
	encryptionOnly["use"] = "enc"

	entries := []map[string]any{
		{"kty": "OKP", "crv": "Ed25519", "kid": "okp", "x": rawURL([]byte("not a key this package verifies with"))},
		{"kty": "EC", "crv": "P-384", "kid": "p384",
			"x": rawURL(p384.PublicKey.X.FillBytes(make([]byte, 48))),
			"y": rawURL(p384.PublicKey.Y.FillBytes(make([]byte, 48)))},
		{"kty": "EC", "crv": "P-256", "kid": "no-coordinates"},
		{"kty": "EC", "crv": "P-256", "kid": "bad-base64", "x": "!!!not base64!!!", "y": "!!!"},
		{"kty": "EC", "crv": "P-256", "kid": "off-curve",
			"x": rawURL(make([]byte, 32)), "y": rawURL(make([]byte, 32))},
		encryptionOnly,
		rsaJWK("weak-rsa", &weakRSA.PublicKey),
		{"kty": "EC", "crv": "P-256",
			"x": rawURL(ec.PublicKey.X.FillBytes(make([]byte, 32))),
			"y": rawURL(ec.PublicKey.Y.FillBytes(make([]byte, 32)))},
		ecJWK("ec-1", &ec.PublicKey),
		rsaJWK("rsa-1", &rsaKey.PublicKey),
	}

	server := newJWKSServer(t, jwksDocument(t, entries...))
	r := newRemote(t, server, newTestClock())

	assertVerifies(t, ec, mustKey(t, r, "ec-1"))
	mustKey(t, r, "rsa-1")
	assertFetches(t, server, 1, "after resolving the usable entries")

	for _, kid := range []string{"okp", "p384", "no-coordinates", "bad-base64", "off-curve", "encryption-only", "weak-rsa", ""} {
		wantUnknownKID(t, r, kid)
	}
}

func TestRemoteKeySourceKeepsTheFirstEntryForARepeatedKID(t *testing.T) {
	// A duplicate kid is how an appended entry would try to displace a real
	// key. Whatever the document says first is what stands.
	published := newECKey(t)
	shadow := newECKey(t)
	server := newJWKSServer(t, jwksDocument(t,
		ecJWK("ec-1", &published.PublicKey),
		ecJWK("ec-1", &shadow.PublicKey),
	))
	r := newRemote(t, server, newTestClock())

	resolved := mustKey(t, r, "ec-1")
	assertVerifies(t, published, resolved)
	assertDoesNotVerify(t, shadow, resolved)
}

func TestRemoteKeySourceIsSafeForConcurrentUse(t *testing.T) {
	key := newECKey(t)
	server := newJWKSServer(t, jwksDocument(t, ecJWK("ec-1", &key.PublicKey)))
	clock := newTestClock()
	r := newRemote(t, server, clock)

	var wg sync.WaitGroup
	for i := range 64 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := range 25 {
				if i%2 == 0 {
					if _, err := r.KeyForKID(context.Background(), "ec-1"); err != nil {
						t.Errorf("KeyForKID(ec-1) returned error: %v", err)
						return
					}
					continue
				}
				kid := "absent-" + string(rune('a'+(i+j)%26))
				if _, err := r.KeyForKID(context.Background(), kid); !errors.Is(err, ErrUnknownKeyID) {
					t.Errorf("KeyForKID(%q) error = %v, want ErrUnknownKeyID", kid, err)
					return
				}
			}
		}(i)
	}
	wg.Wait()

	// The clock never moved, so exactly one fetch may happen however the
	// goroutines interleaved: whoever reached the issuer first also armed the
	// floor for everyone behind them.
	assertFetches(t, server, 1, "after 1600 concurrent lookups")
}

func TestRemoteKeySourceDoesNotReadAnUnboundedBody(t *testing.T) {
	// The body is remote input, so its length is not this process's to trust.
	server := newJWKSServer(t, nil)
	server.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		server.fetches.Add(1)
		w.Header().Set("Content-Type", "application/jwk-set+json")
		_, _ = w.Write([]byte(`{"keys":[`))
		chunk := make([]byte, 64*1024)
		for i := range chunk {
			chunk[i] = ' '
		}
		for range 64 {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	})
	r := newRemote(t, server, newTestClock())

	if _, err := r.KeyForKID(context.Background(), "ec-1"); err == nil {
		t.Fatal("KeyForKID returned no error for an oversized key set document")
	}
}
