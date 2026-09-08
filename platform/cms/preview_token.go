package cms

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// A preview token is a BEARER CREDENTIAL for unpublished content — see this
// TRD's <this_is_an_auth_surface>. It grants time-bounded read access to a
// single Page belonging to a single tenant (CompanyID) without requiring the
// viewer to authenticate as that tenant.
//
// # Design: self-contained signed token, no persistence
//
// The token is HMAC-SHA256-signed over (nonce, pageID, companyID,
// expiresAt) and verified by recomputing that signature — it requires NO
// server-side storage or lookup. This deliberately differs from the
// donor implementations (justinforme/smartWellness app/cms/preview_token.go),
// which sign the SAME shape of payload but derive it deterministically with
// no random component at all. Two properties this TRD requires led to the
// change:
//
//   - "unguessable (crypto/rand)" — MintPreviewToken mixes a crypto/rand
//     nonce into the signed payload, so the token value itself is produced
//     by, and depends on, a CSPRNG on every mint call (the donors' tokens
//     are a pure deterministic function of secret+payload; this file's
//     tokens are not, even for the same page/tenant/expiry).
//   - file ownership — this TRD does not own platform/cms/store.go /
//     store_pg.go (41-02, a parallel TRD) or migrations/platform (also
//     41-02). An opaque-random-token-plus-server-side-lookup design (the
//     more common "API key" shape) would need a persisted grants table this
//     TRD has no seam to add. A self-verifying signed token needs no store
//     at all, so it fits within this file's ownership cleanly. If a later
//     TRD wants revocation-before-expiry or a listing of live preview
//     links, that needs a Store this file does not define — flagged here so
//     it's a deliberate, visible gap rather than a silent one.
var (
	// ErrPreviewSecretEmpty is returned by Mint/Verify when the caller's
	// signing secret is empty. A caller MUST supply a real secret (e.g. from
	// an env-configured key) — this package has no config/env dependency of
	// its own and never invents or defaults one.
	ErrPreviewSecretEmpty = errors.New("cms: preview-token secret is empty")

	// ErrPreviewTokenMalformed indicates the token is structurally invalid
	// (bad encoding, wrong length). Surface as 401, not 400 — do not leak
	// parse-state details to a probing attacker.
	ErrPreviewTokenMalformed = errors.New("cms: preview token malformed")

	// ErrPreviewTokenSignature indicates HMAC verification failed: a
	// tampered payload, or a token signed with a different secret. Surface
	// as 401.
	ErrPreviewTokenSignature = errors.New("cms: preview token signature mismatch")

	// ErrPreviewTokenExpired indicates the token's embedded expiry has
	// passed. An expired token is refused even if its signature is valid.
	ErrPreviewTokenExpired = errors.New("cms: preview token expired")

	// ErrPreviewTokenScope indicates the token's signature and expiry are
	// valid, but it was not minted for the (pageID, companyID) the caller
	// asked Authorize to check — i.e. a token scoped to page A was
	// presented for page B, or a token scoped to one tenant was presented
	// for another tenant's page.
	ErrPreviewTokenScope = errors.New("cms: preview token not valid for this page or tenant")
)

// PreviewTokenTTL is the default lifetime of a minted preview token. Short
// by design: preview URLs leak (pasted into chats, shared Slack links) so
// the window an exposed link stays live is bounded.
const PreviewTokenTTL = 30 * time.Minute

const (
	previewNonceLen      = 16 // crypto/rand bytes mixed into the signed payload
	previewUUIDLen       = 16 // len(uuid.UUID)
	previewExpiresLen    = 8  // int64 unix seconds, big-endian
	previewPayloadLen    = previewNonceLen + previewUUIDLen + previewUUIDLen + previewExpiresLen
	previewSignatureLen  = sha256.Size // 32
	previewTokenRawBytes = previewPayloadLen + previewSignatureLen
)

// PreviewGrant is the decoded, signature-verified content of a preview
// token: which page and tenant it authorizes, and when that authorization
// expires. Returned by VerifyPreviewToken once signature and expiry both
// check out. Most callers want Authorize instead, which additionally
// enforces scope.
type PreviewGrant struct {
	PageID    uuid.UUID
	CompanyID uuid.UUID
	ExpiresAt time.Time
}

// MintPreviewToken creates a bearer preview token scoped to (pageID,
// companyID), valid for PreviewTokenTTL from now.
//
// secret is caller-supplied (typically a per-deployment, env-configured
// key) so this package carries no config/env dependency of its own. It
// must be non-empty — an empty secret would make every token forgeable by
// anyone, defeating the entire mechanism.
func MintPreviewToken(secret []byte, pageID, companyID uuid.UUID) (token string, expiresAt time.Time, err error) {
	return mintPreviewToken(secret, pageID, companyID, time.Now().UTC(), PreviewTokenTTL)
}

// mintPreviewToken is the testable core of MintPreviewToken: issuedAt and
// ttl are explicit so tests can mint an already-expired token deterministically
// (e.g. issuedAt in the past, or a negative ttl) without sleeping.
func mintPreviewToken(secret []byte, pageID, companyID uuid.UUID, issuedAt time.Time, ttl time.Duration) (string, time.Time, error) {
	if len(secret) == 0 {
		return "", time.Time{}, ErrPreviewSecretEmpty
	}
	if pageID == uuid.Nil || companyID == uuid.Nil {
		return "", time.Time{}, errors.New("cms: preview token requires non-zero page and company IDs")
	}

	var nonce [previewNonceLen]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", time.Time{}, fmt.Errorf("cms: preview token nonce: %w", err)
	}

	expiresAt := issuedAt.UTC().Add(ttl)

	payload := make([]byte, 0, previewPayloadLen)
	payload = append(payload, nonce[:]...)
	payload = append(payload, pageID[:]...)
	payload = append(payload, companyID[:]...)
	var expBuf [previewExpiresLen]byte
	binary.BigEndian.PutUint64(expBuf[:], uint64(expiresAt.Unix()))
	payload = append(payload, expBuf[:]...)

	sig := signPreviewPayload(secret, payload)

	raw := append(payload, sig...)
	return base64.RawURLEncoding.EncodeToString(raw), expiresAt, nil
}

func signPreviewPayload(secret, payload []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	mac.Write(payload)
	return mac.Sum(nil)
}

// VerifyPreviewToken parses, signature-verifies, and expiry-checks token,
// returning the PreviewGrant it authorizes. It does NOT check scope against
// a caller's expected page/tenant — use Authorize for that (the property
// this TRD requires: a token scoped to page A must not grant access to
// page B or to another tenant's page).
//
// Signature comparison is constant-time (crypto/subtle.ConstantTimeCompare)
// so a bearer credential's validity can never be inferred from response
// timing — see this TRD's <constraints>.
func VerifyPreviewToken(secret []byte, token string) (PreviewGrant, error) {
	if len(secret) == 0 {
		return PreviewGrant{}, ErrPreviewSecretEmpty
	}

	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return PreviewGrant{}, ErrPreviewTokenMalformed
	}
	if len(raw) != previewTokenRawBytes {
		return PreviewGrant{}, ErrPreviewTokenMalformed
	}

	payload := raw[:previewPayloadLen]
	sig := raw[previewPayloadLen:]

	expectedSig := signPreviewPayload(secret, payload)
	if subtle.ConstantTimeCompare(expectedSig, sig) != 1 {
		return PreviewGrant{}, ErrPreviewTokenSignature
	}

	pageID, err := uuid.FromBytes(payload[previewNonceLen : previewNonceLen+previewUUIDLen])
	if err != nil {
		return PreviewGrant{}, ErrPreviewTokenMalformed
	}
	companyID, err := uuid.FromBytes(payload[previewNonceLen+previewUUIDLen : previewNonceLen+2*previewUUIDLen])
	if err != nil {
		return PreviewGrant{}, ErrPreviewTokenMalformed
	}
	expiresUnix := int64(binary.BigEndian.Uint64(payload[previewNonceLen+2*previewUUIDLen:]))
	expiresAt := time.Unix(expiresUnix, 0).UTC()

	if !time.Now().UTC().Before(expiresAt) {
		return PreviewGrant{}, ErrPreviewTokenExpired
	}

	return PreviewGrant{PageID: pageID, CompanyID: companyID, ExpiresAt: expiresAt}, nil
}

// Authorize verifies token (signature + expiry, via VerifyPreviewToken) and
// additionally enforces that it was minted for exactly pageID and
// companyID. An otherwise-valid, unexpired token scoped to a DIFFERENT page
// or a different tenant is refused with ErrPreviewTokenScope.
func Authorize(secret []byte, token string, pageID, companyID uuid.UUID) (PreviewGrant, error) {
	grant, err := VerifyPreviewToken(secret, token)
	if err != nil {
		return PreviewGrant{}, err
	}
	if grant.PageID != pageID || grant.CompanyID != companyID {
		return PreviewGrant{}, ErrPreviewTokenScope
	}
	return grant, nil
}
