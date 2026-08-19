package identity

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/aocybersystems/eden-platform-go/platform/auth/jwks"
)

// Published key set defaults and constants.
const (
	// KeySetContentType is the RFC 7517 media type for a JWK Set document.
	//
	// A consumer that selects a parser by content type is entitled to be told
	// this is a key set rather than some other JSON, and the specific type is
	// what lets a proxy or a gateway recognise the document without reading
	// it.
	KeySetContentType = "application/jwk-set+json"

	// DefaultKeySetMaxAge is how long a consumer is invited to cache the
	// published document.
	//
	// It mirrors the default lifetime the remote key source in this package
	// applies to a set it has fetched, so the two halves of the loop agree on
	// how stale a key set is allowed to get. It is also the number a rotation
	// has to be planned around: a key withdrawn from this document is still
	// being verified against for up to this long afterwards, so a retiring key
	// has to stay published at least this far past the last context it signed.
	DefaultKeySetMaxAge = DefaultJWKSCacheTTL

	// keySetAllowedMethods is the Allow header sent with a refusal. A bare 405
	// tells a consumer nothing about what to do instead, and RFC 9110 requires
	// the header on this status.
	keySetAllowedMethods = http.MethodGet + ", " + http.MethodHead
)

// Misconfiguration reported by NewKeySetHandler. Each is separately matchable
// with errors.Is because each surfaces at process start, to an operator who
// has to know which of several settings is wrong.
var (
	// ErrNilKeySetSigner reports a key set built without a signer. There is
	// nothing to publish, and an endpoint answering with an empty set would
	// look to a consumer exactly like an issuer that had withdrawn every key
	// it had.
	ErrNilKeySetSigner = errors.New("identity: key set has no signer")

	// ErrMissingKeySetKeyID reports a signer whose KeyID is empty. A verifier
	// selects a key by the kid the context carries, so an unnamed key is one
	// no verifier can ever reach for.
	ErrMissingKeySetKeyID = errors.New("identity: key set signer has no key id")

	// ErrUnpublishableKey reports a signing key the publication surface will
	// not render — an algorithm it does not emit, or a public key whose type
	// contradicts the declared algorithm. It wraps the underlying cause, which
	// is what names the actual problem.
	ErrUnpublishableKey = errors.New("identity: signing key cannot be published as a jwk")

	// ErrInvalidKeySetMaxAge reports a cache lifetime that cannot be expressed
	// as a positive whole number of seconds. RFC 9111 states max-age in
	// seconds, so anything under one second reaches consumers as max-age=0 —
	// an instruction to refetch on every single request, which is the failure
	// the header exists to prevent.
	ErrInvalidKeySetMaxAge = errors.New("identity: key set cache lifetime must be at least one second")
)

// KeySetSigner is what publishing a key set needs from a signing key surface:
// the public half, the algorithm it signs with, and the identifier a consumer
// selects it by.
//
// It is deliberately narrower than the platform key surface — no signing, no
// health check — because this endpoint neither signs nor needs permission to.
// kms.KMSSigner satisfies it as it stands, so an issuer and its published
// document are wired from the same value with no adapter between them, which
// is what stops a deployment from publishing one key while signing with
// another.
type KeySetSigner interface {
	jwks.Signer

	// KeyID returns the stable identifier the key is published under. It is
	// the same value a minted context carries in its kid header.
	KeyID() string
}

// KeySetOption adjusts a KeySetHandler at construction. Options are applied
// before validation, so an option carrying an unusable value is reported by
// NewKeySetHandler rather than discovered by a consumer.
type KeySetOption func(*KeySetHandler)

// WithKeySetMaxAge sets how long a consumer is invited to cache the document,
// overriding DefaultKeySetMaxAge. It must be at least one second.
//
// The value is a rotation decision, not a performance one: see
// DefaultKeySetMaxAge for what it obliges a retiring key to do.
func WithKeySetMaxAge(maxAge time.Duration) KeySetOption {
	return func(h *KeySetHandler) { h.maxAge = maxAge }
}

// KeySetHandler serves an issuer's public signing key as an RFC 7517 key set,
// so a verifier elsewhere can fetch the key a context was signed with.
//
// # It renders no key material of its own
//
// The bytes come from the platform's key set type, which is the one place that
// decides what a published key looks like. A second renderer here would be a
// second definition of this process's key, and the two would drift in silence:
// a correction to the coordinate padding or to the algorithm field made in one
// would leave the other publishing subtly wrong keys with nothing failing.
//
// # The document is rendered once
//
// A key set changes only when keys rotate, which is not per request, so it is
// built at construction and the same immutable bytes are served from then on.
//
// The saved work is the lesser reason. Rendering per request would mean reading
// the key surface per request — a call to a hosted key service on the request
// path — and it would make the response able to differ between two requests
// under load or partial failure. A key document that can differ between two
// requests is one where a consumer's answer to "which key signed this" depends
// on which response it happened to get, and no amount of correct signing
// survives that.
//
// Rotation is therefore a new handler, not a mutated one: build one over the
// new signer and swap it in. Nothing here mutates after construction, so a
// KeySetHandler is safe for concurrent use by every request path in a process.
//
// # Only the public half is served
//
// The publication surface renders public halves only. The tests assert it
// anyway, on the served bytes, because the cost of being wrong once is a forged
// identity for every principal the issuer serves.
type KeySetHandler struct {
	// document is the rendered key set, written to every response and never
	// modified after construction.
	document []byte

	// cacheControl is the rendered header, kept alongside the document so
	// neither is recomputed per request.
	cacheControl string

	maxAge time.Duration
}

// Compile-time assertion: this is an ordinary handler and mounts wherever a
// deployment publishes its keys, commonly a well-known path.
var _ http.Handler = (*KeySetHandler)(nil)

// NewKeySetHandler returns a handler that publishes signer's public key under
// signer.KeyID().
//
// # The key id is not a parameter
//
// It is read from the signer for the same reason the issuer reads it there,
// and the reason is the whole point of this endpoint: the document has to name
// the key by the identifier that appears in the kid header of the contexts that
// key signs. A caller allowed to pass a different one here could publish a
// perfectly valid key set that no context ever resolves against, and the
// mismatch would surface as every verification failing with a correct-looking
// key on both sides. There is nowhere to pass one.
//
// # Failures happen here, not per request
//
// A key the publication surface will not render, an empty identifier, or a
// cache lifetime that cannot be stated in seconds are all refused at
// construction. Each of them, deferred, becomes an endpoint that serves
// something useless to every consumer for the life of the process — and a key
// endpoint that answers wrongly is worse than one that is not there, because a
// consumer takes an answer as the truth about which keys the issuer has.
func NewKeySetHandler(signer KeySetSigner, opts ...KeySetOption) (*KeySetHandler, error) {
	h := &KeySetHandler{maxAge: DefaultKeySetMaxAge}
	for _, opt := range opts {
		opt(h)
	}

	if signer == nil {
		return nil, ErrNilKeySetSigner
	}
	if h.maxAge < time.Second {
		return nil, fmt.Errorf("%w: got %v", ErrInvalidKeySetMaxAge, h.maxAge)
	}

	// Read the identifier once and publish under that same value, so there is
	// no window in which the key could be added under one id and described by
	// another.
	keyID := signer.KeyID()
	if keyID == "" {
		return nil, ErrMissingKeySetKeyID
	}

	// The platform key set type is the renderer. This is the only place the
	// signing key is read.
	var set jwks.Set
	if err := set.AddSigningKey(signer, keyID); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnpublishableKey, err)
	}
	document, err := set.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnpublishableKey, err)
	}

	h.document = document
	h.cacheControl = "public, max-age=" + strconv.FormatInt(int64(h.maxAge/time.Second), 10)
	return h, nil
}

// ServeHTTP writes the published key set.
//
// The endpoint is read-only. GET returns the document; HEAD returns the same
// headers with no body, which is what lets a consumer ask whether the document
// is worth fetching without paying for it. Every other method is refused with
// 405 and an Allow header.
//
// The response is public and cacheable by design: it contains no secret, and
// the whole point of the lifetime it carries is to keep a consumer from
// fetching it on every verification. Without that, one request to a service
// that verifies a context becomes two.
func (h *KeySetHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", keySetAllowedMethods)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	header := w.Header()
	header.Set("Content-Type", KeySetContentType)
	header.Set("Content-Length", strconv.Itoa(len(h.document)))
	header.Set("Cache-Control", h.cacheControl)
	w.WriteHeader(http.StatusOK)

	// A HEAD response carries the headers that describe the document and none
	// of the document.
	if r.Method == http.MethodHead {
		return
	}

	// The write is best effort: a consumer that hangs up mid-response is an
	// ordinary event, the status is already committed, and there is nothing
	// left to tell anyone.
	_, _ = w.Write(h.document)
}
