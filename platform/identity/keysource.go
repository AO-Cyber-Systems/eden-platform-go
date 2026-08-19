package identity

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// Cache defaults for RemoteJWKS.
const (
	// DefaultJWKSCacheTTL is how long a fetched key set is served before it is
	// fetched again.
	//
	// This is the bound on the rotation trap described on RemoteJWKS: it is
	// the longest an issuer that rotated a key WITHOUT changing its kid can
	// keep failing before this source heals itself. Lengthening it lengthens
	// that outage; shortening it costs an extra request per issuer per period.
	DefaultJWKSCacheTTL = 5 * time.Minute

	// DefaultJWKSMinRefetchInterval is the shortest gap allowed between two
	// upstream fetches.
	//
	// A kid arrives in the header of whatever token was presented, so it is
	// attacker-controlled. Without a floor, a stream of invented kids would be
	// a stream of cache misses, and every miss would become a request aimed at
	// the issuer with this process as the pump. The floor is what caps that at
	// one request per interval no matter how many distinct kids arrive.
	DefaultJWKSMinRefetchInterval = 30 * time.Second

	// defaultJWKSHTTPTimeout bounds a fetch made with the default client. A
	// fetch runs with the cache lock held, so an unbounded request against an
	// issuer that accepts the connection and then stops talking would wedge
	// every lookup in the process, not just its own.
	defaultJWKSHTTPTimeout = 10 * time.Second

	// maxJWKSBytes caps how much of a key set document is read. The body is
	// remote input and its length is not this process's to trust.
	maxJWKSBytes = 1 << 20

	// p256CoordinateBytes is the width of a P-256 coordinate.
	p256CoordinateBytes = 32

	// minRSAModulusBits is the smallest RSA modulus accepted from a key set. A
	// signature is only worth as much as the key behind it, and a modulus
	// below this is not one this package will verify a security decision with.
	minRSAModulusBits = 2048
)

// Misconfiguration reported by NewRemoteJWKS. As with the minter, these are
// distinct and separately matchable with errors.Is: they surface at process
// start, to an operator who has to know which setting is wrong.
var (
	// ErrMissingJWKSURL reports a key set source built without a URL.
	ErrMissingJWKSURL = errors.New("identity: key set url is empty")

	// ErrInvalidJWKSURL reports a URL that is not an absolute http or https
	// one. A relative or exotic URL cannot be fetched, and finding that out at
	// the first verification rather than at start-up means finding it out
	// during an incident.
	ErrInvalidJWKSURL = errors.New("identity: key set url must be an absolute http or https url")

	// ErrNilHTTPClient reports a nil client passed to WithJWKSHTTPClient.
	ErrNilHTTPClient = errors.New("identity: http client must not be nil")

	// ErrInvalidCacheTTL reports a non-positive cache lifetime, which would
	// re-fetch the key set on every single lookup.
	ErrInvalidCacheTTL = errors.New("identity: key set cache lifetime must be positive")

	// ErrInvalidRefetchInterval reports a refetch floor that is not positive,
	// or one longer than the cache lifetime. A floor longer than the lifetime
	// would throttle the scheduled refresh itself, which is the one fetch that
	// must not be delayed: it is what recovers from a key rotated under an
	// unchanged kid.
	ErrInvalidRefetchInterval = errors.New("identity: minimum refetch interval must be positive and no longer than the cache lifetime")
)

// Failures reported by a key lookup.
var (
	// ErrUnknownKeyID reports that no key in the source answers to this kid.
	// A verifier treats it as a refusal to verify, never as permission to try
	// another key.
	ErrUnknownKeyID = errors.New("identity: no verification key for key id")

	// ErrJWKSUnavailable reports that the issuer's key set could not be
	// fetched: a transport failure, a timeout, a cancelled context, or a
	// non-2xx response. The set already cached, if any, is left alone.
	ErrJWKSUnavailable = errors.New("identity: key set could not be fetched")

	// ErrJWKSMalformed reports that the issuer answered but the document
	// cannot be used: it is not JSON, not a key set, larger than this package
	// will read, or contains no key this package can verify with. The set
	// already cached, if any, is left alone.
	ErrJWKSMalformed = errors.New("identity: key set is not a usable jwks document")
)

// KeySource resolves the kid from a context's header to the public key that
// context should be verified against.
//
// It is the seam a verifier trusts an issuer through, and it is an interface
// rather than a fixed fetcher for two reasons: a consumer that already runs a
// key set cache can plug it in instead of operating a second one, and keeping
// the shape abstract lets this package stay on the standard library.
//
// An implementation must be safe for concurrent use: one source is shared
// across every request path in a process.
//
// KeyForKID returns an error for a kid it cannot resolve. It never falls back
// to "the only key" or "the first key" — a verifier that accepts a key the
// token did not name is a verifier an attacker gets to choose the key for.
type KeySource interface {
	KeyForKID(ctx context.Context, kid string) (crypto.PublicKey, error)
}

var (
	_ KeySource = (*StaticKeys)(nil)
	_ KeySource = (*RemoteJWKS)(nil)
)

// StaticKeys is a KeySource over a fixed, in-memory set of public keys.
//
// It serves an in-process issuer, a pinned key set an operator configured by
// hand, and tests. Nothing about it expires or refreshes: the set it was built
// with is the set it answers from for the life of the process, so rotation
// means building a new one.
//
// It is immutable after construction and safe for concurrent use.
type StaticKeys struct {
	keys map[string]crypto.PublicKey
}

// NewStaticKeys returns a KeySource over a copy of keys.
//
// The copy is the point: a map the caller can still write to after
// construction is a trust set some unrelated code path can widen at run time,
// and a key set that can grow without anyone deciding to grow it is not a
// trust decision any more.
//
// Entries with an empty kid or a nil key are dropped. A nil key would
// otherwise resolve successfully and hand a verifier nothing to verify with.
// A nil or empty map is allowed and yields a source that resolves nothing.
func NewStaticKeys(keys map[string]crypto.PublicKey) *StaticKeys {
	copied := make(map[string]crypto.PublicKey, len(keys))
	for kid, key := range keys {
		if kid == "" || key == nil {
			continue
		}
		copied[kid] = key
	}
	return &StaticKeys{keys: copied}
}

// KeyForKID returns the key filed under kid, or ErrUnknownKeyID.
//
// The context is unused: the answer is already in memory. It is in the
// signature because callers hold one and the seam has remote implementations
// that need it.
func (s *StaticKeys) KeyForKID(_ context.Context, kid string) (crypto.PublicKey, error) {
	if kid == "" {
		return nil, fmt.Errorf("%w: token carries no key id", ErrUnknownKeyID)
	}
	key, ok := s.keys[kid]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownKeyID, kid)
	}
	return key, nil
}

// RemoteJWKSOption adjusts a RemoteJWKS at construction. Options are applied
// before validation, so an option carrying an unusable value is reported by
// NewRemoteJWKS rather than at the first lookup.
type RemoteJWKSOption func(*RemoteJWKS)

// WithJWKSHTTPClient replaces the client used to fetch the key set. It must
// not be nil.
//
// Supply one to reuse a connection pool, to add transport instrumentation, or
// to pin the issuer's certificate. Whatever is supplied should carry a
// timeout: a fetch runs with the cache lock held.
func WithJWKSHTTPClient(client *http.Client) RemoteJWKSOption {
	return func(r *RemoteJWKS) { r.client = client }
}

// WithJWKSCacheTTL sets how long a fetched key set is served before it is
// fetched again, overriding DefaultJWKSCacheTTL. It must be positive and no
// shorter than the refetch floor.
func WithJWKSCacheTTL(ttl time.Duration) RemoteJWKSOption {
	return func(r *RemoteJWKS) { r.ttl = ttl }
}

// WithJWKSMinRefetchInterval sets the shortest gap allowed between two
// upstream fetches, overriding DefaultJWKSMinRefetchInterval. It must be
// positive and no longer than the cache lifetime.
//
// Raising it hardens the issuer against a flood of invented kids and slows
// recovery from a rotation that introduced a new one, by the same amount.
func WithJWKSMinRefetchInterval(interval time.Duration) RemoteJWKSOption {
	return func(r *RemoteJWKS) { r.minRefetch = interval }
}

// WithJWKSClock replaces the time source the cache ages against. It must not
// be nil.
//
// This is a seam for tests and for callers that already hold a controlled time
// source, in the same shape the minter uses. It cannot be used to resolve a
// key that is not in the issuer's published set; all it moves is when the
// cached set is considered stale.
func WithJWKSClock(now func() time.Time) RemoteJWKSOption {
	return func(r *RemoteJWKS) { r.now = now }
}

// RemoteJWKS is a KeySource backed by an issuer's published JWKS endpoint. It
// fetches the RFC 7517 document over HTTP, caches the keys it can use, and is
// safe for concurrent use.
//
// # Both refetch triggers
//
// The cache refetches on two independent triggers, and it needs both.
//
// The first is an unknown kid. Rotation normally introduces a new kid, the
// lookup misses, and the miss is what prompts a fetch. That alone looks like
// enough, and it is not.
//
// The second is expiry. An issuer may publish a FIXED kid and regenerate the
// key behind it — a restart holding an ephemeral key does exactly this. The
// kid never changes, so the cache never misses, so a source that only
// refetched on a miss would go on verifying against a public key whose private
// half no longer exists. Every context would fail, indefinitely, with nothing
// in the request path able to notice or recover. Expiry is the only thing that
// heals that, which is why the cache ages out even when every kid it holds is
// still being asked for.
//
// # The floor
//
// The first trigger is reachable by anyone who can present a token, because
// the kid is just a header field. Left ungoverned it turns this process into a
// request amplifier pointed at the issuer: one invented kid, one upstream
// request. So no two fetches happen closer together than the refetch floor.
// Inside that window an unknown kid is answered from the cached set — which
// means ErrUnknownKeyID — rather than by reaching for the network.
//
// The floor covers every fetch, not only the ones a miss prompted, so a
// failing issuer is retried at the floor rather than on every request. It is
// required to be no longer than the cache lifetime, so it can never delay the
// scheduled refresh that recovers from a rotation under an unchanged kid.
//
// # Degradation
//
// A fetch that fails leaves the cached set exactly as it was: unreachable is
// not the same as untrusted, and discarding good keys because the issuer had a
// bad minute would turn a blip into an outage. While the floor holds off the
// retry, a still-cached key keeps resolving.
type RemoteJWKS struct {
	url        string
	client     *http.Client
	ttl        time.Duration
	minRefetch time.Duration
	now        func() time.Time

	// mu guards the cache below and is held across the fetch itself. Holding
	// it that long is deliberate: it collapses a burst of concurrent misses
	// into a single upstream request, so a cold cache under load does not
	// become a thundering herd at the issuer.
	mu          sync.Mutex
	keys        map[string]crypto.PublicKey
	fetchedAt   time.Time
	lastAttempt time.Time
}

// NewRemoteJWKS returns a KeySource that fetches its keys from jwksURL.
//
// Nothing is fetched here. The first fetch happens on the first lookup, so a
// process starts even if the issuer is briefly unreachable.
//
// Prefer an https URL. The response decides which key a context is verified
// against, so anyone able to rewrite a plaintext response is able to choose
// that key; http is permitted only because an issuer reachable over a trusted
// internal path is a real deployment, and it is the operator who knows whether
// that is what this is.
func NewRemoteJWKS(jwksURL string, opts ...RemoteJWKSOption) (*RemoteJWKS, error) {
	r := &RemoteJWKS{
		url:        jwksURL,
		client:     &http.Client{Timeout: defaultJWKSHTTPTimeout},
		ttl:        DefaultJWKSCacheTTL,
		minRefetch: DefaultJWKSMinRefetchInterval,
		now:        time.Now,
	}
	for _, opt := range opts {
		opt(r)
	}

	if r.url == "" {
		return nil, ErrMissingJWKSURL
	}
	parsed, err := url.Parse(r.url)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidJWKSURL, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("%w: got scheme %q", ErrInvalidJWKSURL, parsed.Scheme)
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("%w: no host", ErrInvalidJWKSURL)
	}
	if r.client == nil {
		return nil, ErrNilHTTPClient
	}
	if r.ttl <= 0 {
		return nil, fmt.Errorf("%w: got %v", ErrInvalidCacheTTL, r.ttl)
	}
	if r.minRefetch <= 0 || r.minRefetch > r.ttl {
		return nil, fmt.Errorf("%w: got %v with a cache lifetime of %v", ErrInvalidRefetchInterval, r.minRefetch, r.ttl)
	}
	if r.now == nil {
		return nil, ErrInvalidClock
	}

	return r, nil
}

// KeyForKID returns the published key filed under kid, fetching the issuer's
// key set if the cached one cannot answer or has expired.
//
// See the type documentation for when a fetch happens and when the refetch
// floor suppresses one.
func (r *RemoteJWKS) KeyForKID(ctx context.Context, kid string) (crypto.PublicKey, error) {
	if kid == "" {
		// An empty kid can never match an entry, so fetching on one would be
		// an amplification path open to any token with no kid header at all.
		return nil, fmt.Errorf("%w: token carries no key id", ErrUnknownKeyID)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now()
	key, cached := r.keys[kid]

	// The common path: a known kid in a set that has not aged out, answered
	// without touching the network.
	if cached && !r.expiredLocked(now) {
		return key, nil
	}

	// Past here the cache either does not hold this kid or has aged out, and
	// both want a fetch. The floor decides whether one may happen yet.
	if r.throttledLocked(now) {
		if cached {
			return key, nil
		}
		return nil, fmt.Errorf("%w: %q", ErrUnknownKeyID, kid)
	}

	if err := r.refreshLocked(ctx, now); err != nil {
		return nil, err
	}

	key, cached = r.keys[kid]
	if !cached {
		return nil, fmt.Errorf("%w: %q", ErrUnknownKeyID, kid)
	}
	return key, nil
}

// expiredLocked reports whether the cached set has aged out, treating a set
// that was never fetched as expired.
func (r *RemoteJWKS) expiredLocked(now time.Time) bool {
	return r.fetchedAt.IsZero() || !now.Before(r.fetchedAt.Add(r.ttl))
}

// throttledLocked reports whether the floor forbids a fetch right now. It
// anchors on the last attempt rather than the last success, so a failing
// issuer is retried at the floor instead of on every request.
func (r *RemoteJWKS) throttledLocked(now time.Time) bool {
	return !r.lastAttempt.IsZero() && now.Before(r.lastAttempt.Add(r.minRefetch))
}

// refreshLocked fetches the key set and installs it.
//
// The attempt is recorded before the fetch, so the floor applies whether or
// not it succeeds. The cached set is replaced only once a usable document has
// been parsed in full: a failed refresh must never leave the source with fewer
// keys than it started with.
func (r *RemoteJWKS) refreshLocked(ctx context.Context, now time.Time) error {
	r.lastAttempt = now

	set, err := r.fetch(ctx)
	if err != nil {
		return err
	}

	r.keys = set
	r.fetchedAt = now
	return nil
}

func (r *RemoteJWKS) fetch(ctx context.Context) (map[string]crypto.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.url, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrJWKSUnavailable, err)
	}
	req.Header.Set("Accept", "application/jwk-set+json, application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrJWKSUnavailable, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxJWKSBytes))
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("%w: %s responded %s", ErrJWKSUnavailable, r.url, resp.Status)
	}

	// One byte past the cap, so an oversized document is detected rather than
	// silently truncated into something that merely looks malformed.
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxJWKSBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrJWKSUnavailable, err)
	}
	if len(body) > maxJWKSBytes {
		return nil, fmt.Errorf("%w: document exceeds %d bytes", ErrJWKSMalformed, maxJWKSBytes)
	}

	return parseJWKS(body)
}

// jwk is the subset of an RFC 7517 key this package reads.
type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Crv string `json:"crv"`
	Use string `json:"use"`
	X   string `json:"x"`
	Y   string `json:"y"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// parseJWKS reads an RFC 7517 {"keys":[...]} document into a kid-indexed set.
//
// An entry this package cannot use is skipped rather than failing the whole
// document. An issuer publishes one key set for every consumer it has, so a
// curve or key type this package does not verify with is an ordinary thing to
// find there, and discarding the entire set over one such entry would take
// down verification for the keys that are perfectly usable.
//
// A document that yields no usable key at all IS an error. Installing an empty
// set would quietly replace a working cache with one that resolves nothing.
func parseJWKS(body []byte) (map[string]crypto.PublicKey, error) {
	var document struct {
		Keys []jwk `json:"keys"`
	}
	if err := json.Unmarshal(body, &document); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrJWKSMalformed, err)
	}

	set := make(map[string]crypto.PublicKey, len(document.Keys))
	for _, entry := range document.Keys {
		// An unnamed key cannot be selected by a kid header, and a key marked
		// for encryption is not one to verify a signature with.
		if entry.Kid == "" || (entry.Use != "" && entry.Use != "sig") {
			continue
		}
		// First entry wins. A repeated kid is how an appended entry would try
		// to displace a real key.
		if _, taken := set[entry.Kid]; taken {
			continue
		}
		key, err := entry.publicKey()
		if err != nil {
			continue
		}
		set[entry.Kid] = key
	}

	if len(set) == 0 {
		return nil, fmt.Errorf("%w: no usable verification key", ErrJWKSMalformed)
	}
	return set, nil
}

func (k jwk) publicKey() (crypto.PublicKey, error) {
	switch k.Kty {
	case "EC":
		return k.ecPublicKey()
	case "RSA":
		return k.rsaPublicKey()
	default:
		return nil, fmt.Errorf("unsupported key type %q", k.Kty)
	}
}

func (k jwk) ecPublicKey() (*ecdsa.PublicKey, error) {
	if k.Crv != "P-256" {
		return nil, fmt.Errorf("unsupported curve %q", k.Crv)
	}

	x, err := coordinate(k.X, p256CoordinateBytes)
	if err != nil {
		return nil, fmt.Errorf("x: %w", err)
	}
	y, err := coordinate(k.Y, p256CoordinateBytes)
	if err != nil {
		return nil, fmt.Errorf("y: %w", err)
	}

	// Re-encode as a SEC 1 uncompressed point, padding each coordinate back to
	// the full width of the curve. Parsing it is what checks the point is
	// actually on P-256 and is not the point at infinity; a pair of integers
	// assembled into an ecdsa.PublicKey by hand gets neither check.
	point := make([]byte, 1+2*p256CoordinateBytes)
	point[0] = 4
	x.FillBytes(point[1 : 1+p256CoordinateBytes])
	y.FillBytes(point[1+p256CoordinateBytes:])

	return ecdsa.ParseUncompressedPublicKey(elliptic.P256(), point)
}

// coordinate decodes one base64url EC coordinate into an integer.
//
// The width is an upper bound, never an equality check. RFC 7518 section
// 6.2.1.2 says a coordinate SHOULD be zero-left-padded to the full width of
// the curve, and many issuers do exactly that. Many others encode with
// big.Int.Bytes(), which drops leading zero bytes — so about one P-256 key in
// 128 arrives 31 bytes wide, and shorter still, more rarely. Both encodings
// describe the same integer. Requiring the padded form would reject a
// perfectly valid key set at a rate low enough to look like random breakage
// and high enough to happen; SetBytes reads either one correctly.
//
// Anything WIDER than the curve is rejected, because it cannot be a coordinate
// on it.
func coordinate(encoded string, width int) (*big.Int, error) {
	if encoded == "" {
		return nil, errors.New("coordinate is absent")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("coordinate is not unpadded base64url: %w", err)
	}
	if len(decoded) > width {
		return nil, fmt.Errorf("coordinate is %d bytes, wider than the %d-byte curve", len(decoded), width)
	}
	return new(big.Int).SetBytes(decoded), nil
}

func (k jwk) rsaPublicKey() (*rsa.PublicKey, error) {
	if k.N == "" || k.E == "" {
		return nil, errors.New("modulus or exponent is absent")
	}

	modulusBytes, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, fmt.Errorf("modulus is not unpadded base64url: %w", err)
	}
	exponentBytes, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, fmt.Errorf("exponent is not unpadded base64url: %w", err)
	}

	// Same reasoning as the EC coordinates: an issuer may or may not have
	// padded these, and SetBytes reads either form.
	modulus := new(big.Int).SetBytes(modulusBytes)
	if bits := modulus.BitLen(); bits < minRSAModulusBits {
		return nil, fmt.Errorf("modulus is %d bits, below the %d-bit floor", bits, minRSAModulusBits)
	}

	exponent := new(big.Int).SetBytes(exponentBytes)
	if exponent.BitLen() > 31 {
		return nil, errors.New("public exponent does not fit in an int")
	}
	e := int(exponent.Int64())
	if e < 3 || e%2 == 0 {
		return nil, fmt.Errorf("public exponent %d is not usable", e)
	}

	return &rsa.PublicKey{N: modulus, E: e}, nil
}
