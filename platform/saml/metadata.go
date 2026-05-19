package saml

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"

	"github.com/crewjam/saml"
	"github.com/crewjam/saml/samlsp"
)

// ErrParseMetadata wraps any error returned by samlsp.ParseMetadata so
// callers can distinguish parse failures from cache miss / refresh
// failures.
var ErrParseMetadata = errors.New("saml/metadata: ParseMetadata failed")

// metadataCacheKey is the SHA-256 of the raw metadata XML bytes. Using the
// hash (not the descriptor itself) lets callers feed semantically identical
// metadata from different sources (DB blob vs. file vs. HTTP body cache)
// and still hit the cache.
type metadataCacheKey [sha256.Size]byte

var (
	metadataCacheMu sync.Mutex
	metadataCache   = map[metadataCacheKey]*saml.EntityDescriptor{}
)

// ParseAndCacheMetadata parses rawXML using crewjam's samlsp.ParseMetadata
// and caches the resulting *saml.EntityDescriptor keyed by the SHA-256 of
// the input bytes. Subsequent calls with the same rawXML return the cached
// pointer (so callers may compare with == to detect a cache hit; this is
// also useful for short-circuiting downstream validators that hash on the
// descriptor pointer).
//
// IMPORTANT: This function does NOT fetch metadata over the network. The
// deprecated IDPMetadataURL surface on samlsp.Options is intentionally not
// exposed; callers are responsible for fetching metadata (typically from a
// DB row or a local file) and passing the bytes. This is a load-bearing
// design choice — federation metadata is a tenant trust anchor and must be
// stored, not retrieved on every parse.
func ParseAndCacheMetadata(rawXML []byte) (*saml.EntityDescriptor, error) {
	key := metadataCacheKey(sha256.Sum256(rawXML))

	metadataCacheMu.Lock()
	if cached, ok := metadataCache[key]; ok {
		metadataCacheMu.Unlock()
		return cached, nil
	}
	metadataCacheMu.Unlock()

	ed, err := samlsp.ParseMetadata(rawXML)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrParseMetadata, err)
	}

	metadataCacheMu.Lock()
	metadataCache[key] = ed
	metadataCacheMu.Unlock()
	return ed, nil
}

// RefreshMetadata evicts any cached descriptor for rawXML (keyed by
// sha256(rawXML)), re-parses, and re-caches. Used when an upstream IdP
// rotates a signing cert and the underlying bytes change — operators call
// Refresh with the new bytes; subsequent ParseAndCacheMetadata with those
// new bytes will hit a fresh parse.
//
// Returns the same error semantics as ParseAndCacheMetadata.
func RefreshMetadata(rawXML []byte) error {
	key := metadataCacheKey(sha256.Sum256(rawXML))

	metadataCacheMu.Lock()
	delete(metadataCache, key)
	metadataCacheMu.Unlock()

	ed, err := samlsp.ParseMetadata(rawXML)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrParseMetadata, err)
	}

	metadataCacheMu.Lock()
	metadataCache[key] = ed
	metadataCacheMu.Unlock()
	return nil
}

// ResetMetadataCache empties the cache. Exported for tests; not intended
// for production use.
func ResetMetadataCache() {
	metadataCacheMu.Lock()
	metadataCache = map[metadataCacheKey]*saml.EntityDescriptor{}
	metadataCacheMu.Unlock()
}
