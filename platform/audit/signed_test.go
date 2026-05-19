package audit

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// --------------------------------------------------------------------------
// fakeECDSASigner is a kms.KMSSigner-compatible test signer backed by a
// freshly-generated P-256 key. SigningAlgorithm returns ES256; Sign returns
// ASN.1-DER (the same shape AWS KMS / PKCS#11 / Azure Managed HSM produce).
// --------------------------------------------------------------------------

type fakeECDSASigner struct {
	priv  *ecdsa.PrivateKey
	keyID string
	alg   string
	fail  error
}

func newFakeECDSASigner(t *testing.T) *fakeECDSASigner {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	return &fakeECDSASigner{priv: priv, keyID: "test-key-es256", alg: "ES256"}
}

func (s *fakeECDSASigner) Public() crypto.PublicKey { return &s.priv.PublicKey }
func (s *fakeECDSASigner) Sign(rnd io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error) {
	if s.fail != nil {
		return nil, s.fail
	}
	r, sigS, err := ecdsa.Sign(rnd, s.priv, digest)
	if err != nil {
		return nil, err
	}
	return asn1.Marshal(struct{ R, S *big.Int }{r, sigS})
}
func (s *fakeECDSASigner) KeyID() string                         { return s.keyID }
func (s *fakeECDSASigner) SigningAlgorithm() string              { return s.alg }
func (s *fakeECDSASigner) HealthCheck(ctx context.Context) error { return nil }

// fakeRSASigner — RS256 (PKCS1v15) variant matching the platform's RSA path.
type fakeRSASigner struct {
	priv  *rsa.PrivateKey
	keyID string
	alg   string
}

func newFakeRSASigner(t *testing.T) *fakeRSASigner {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return &fakeRSASigner{priv: priv, keyID: "test-key-rs256", alg: "RS256"}
}

func (s *fakeRSASigner) Public() crypto.PublicKey { return &s.priv.PublicKey }
func (s *fakeRSASigner) Sign(rnd io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error) {
	return rsa.SignPKCS1v15(rnd, s.priv, crypto.SHA256, digest)
}
func (s *fakeRSASigner) KeyID() string                         { return s.keyID }
func (s *fakeRSASigner) SigningAlgorithm() string              { return s.alg }
func (s *fakeRSASigner) HealthCheck(ctx context.Context) error { return nil }

// fakeUnsupportedSigner advertises HMAC256 — SignedStore must reject.
type fakeUnsupportedSigner struct{ fakeECDSASigner }

func (s *fakeUnsupportedSigner) SigningAlgorithm() string { return "HS256" }

// --------------------------------------------------------------------------
// recordingStore implements AuditStore (legacy fallback path).
// --------------------------------------------------------------------------

type recordingStore struct {
	mu    sync.Mutex
	calls []recordingCall
}

type recordingCall struct {
	companyID  uuid.UUID
	actorID    uuid.UUID
	action     string
	resource   string
	resourceID string
	ipAddress  string
	details    []byte
}

func (r *recordingStore) CreateAuditLog(_ context.Context, companyID, actorID uuid.UUID, action, resource, resourceID, ipAddress string, details []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, recordingCall{companyID, actorID, action, resource, resourceID, ipAddress, append([]byte(nil), details...)})
	return nil
}

func (r *recordingStore) last() recordingCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls[len(r.calls)-1]
}

// recordingSignedStore implements BOTH AuditStore + SignedEventStore.
type recordingSignedStore struct {
	recordingStore
	signedCalls []signedCall
}

type signedCall struct {
	event        Event
	jwsCompact   string
	signingError string
}

func (r *recordingSignedStore) CreateSignedAuditLog(_ context.Context, e Event, jwsCompact, signingError string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.signedCalls = append(r.signedCalls, signedCall{event: e, jwsCompact: jwsCompact, signingError: signingError})
	return nil
}

// --------------------------------------------------------------------------
// Tests
// --------------------------------------------------------------------------

func TestSignedStore_HappyPath_ES256(t *testing.T) {
	signer := newFakeECDSASigner(t)
	store := &recordingStore{}
	ss, err := NewSignedStore(store, signer, "aoid")
	require.NoError(t, err)

	companyID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	actorID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	require.NoError(t, ss.CreateAuditLog(context.Background(), companyID, actorID, "auth.user.login", "session", "abc", "10.0.0.1", []byte(`{"k":"v"}`)))

	require.Len(t, store.calls, 1)
	call := store.last()
	// Fallback path embedded jws into a wrapper {jws, payload}.
	var wrapped map[string]any
	require.NoError(t, json.Unmarshal(call.details, &wrapped))
	jwsCompact, _ := wrapped["jws"].(string)
	require.NotEmpty(t, jwsCompact)

	// Parse JWS Compact and verify with ecdsa.Verify against signer's public key.
	verifyES256(t, jwsCompact, &signer.priv.PublicKey, "test-key-es256")
}

func TestSignedStore_HappyPath_RS256(t *testing.T) {
	signer := newFakeRSASigner(t)
	store := &recordingStore{}
	ss, err := NewSignedStore(store, signer, "aoid")
	require.NoError(t, err)

	companyID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	actorID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	require.NoError(t, ss.CreateAuditLog(context.Background(), companyID, actorID, "auth.user.login", "session", "xyz", "10.0.0.2", []byte(`{}`)))

	require.Len(t, store.calls, 1)
	var wrapped map[string]any
	require.NoError(t, json.Unmarshal(store.last().details, &wrapped))
	jwsCompact, _ := wrapped["jws"].(string)
	require.NotEmpty(t, jwsCompact)

	verifyRS256(t, jwsCompact, &signer.priv.PublicKey, "test-key-rs256")
}

func TestSignedStore_RejectsUnsupportedAlg(t *testing.T) {
	signer := &fakeUnsupportedSigner{fakeECDSASigner: *newFakeECDSASigner(t)}
	_, err := NewSignedStore(&recordingStore{}, signer, "aoid")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrUnsupportedAlgorithm)
}

func TestSignedStore_NilSigner(t *testing.T) {
	_, err := NewSignedStore(&recordingStore{}, nil, "aoid")
	require.Error(t, err)
	require.Contains(t, err.Error(), "nil signer")
}

func TestSignedStore_EmptyIssuer(t *testing.T) {
	_, err := NewSignedStore(&recordingStore{}, newFakeECDSASigner(t), "")
	require.Error(t, err)
}

func TestSignedStore_SignerFailure_StillPersists(t *testing.T) {
	signer := newFakeECDSASigner(t)
	signer.fail = errors.New("KMS temporarily unavailable")
	store := &recordingSignedStore{}
	ss, err := NewSignedStore(store, signer, "aoid")
	require.NoError(t, err)

	companyID := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	actorID := uuid.MustParse("66666666-6666-6666-6666-666666666666")
	require.NoError(t, ss.CreateAuditLog(context.Background(), companyID, actorID, "auth.user.login", "session", "abc", "10.0.0.1", []byte(`{}`)))

	require.Len(t, store.signedCalls, 1)
	require.Empty(t, store.signedCalls[0].jwsCompact, "jwsCompact should be empty on signer failure")
	require.Contains(t, store.signedCalls[0].signingError, "KMS temporarily unavailable")
}

func TestSignedStore_SignedEventStorePreferred(t *testing.T) {
	signer := newFakeECDSASigner(t)
	store := &recordingSignedStore{}
	ss, err := NewSignedStore(store, signer, "aoid")
	require.NoError(t, err)

	companyID := uuid.MustParse("77777777-7777-7777-7777-777777777777")
	actorID := uuid.MustParse("88888888-8888-8888-8888-888888888888")
	require.NoError(t, ss.CreateAuditLog(context.Background(), companyID, actorID, "auth.user.login", "session", "id1", "10.0.0.1", []byte(`{"a":1}`)))

	// Plain CreateAuditLog must NOT have been called when SignedEventStore is implemented.
	require.Empty(t, store.calls, "fallback path called when SignedEventStore was available")
	require.Len(t, store.signedCalls, 1)
	require.NotEmpty(t, store.signedCalls[0].jwsCompact)
	require.Empty(t, store.signedCalls[0].signingError)
	require.Equal(t, "auth.user.login", store.signedCalls[0].event.Action)
}

func TestSignedStore_DeterministicSignaturePayload_ClockFixed(t *testing.T) {
	signer := newFakeECDSASigner(t)
	store := &recordingSignedStore{}
	ss, err := NewSignedStore(store, signer, "aoid")
	require.NoError(t, err)
	// Pin the clock so iat is identical across two signs.
	fixed := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	ss.clock = func() time.Time { return fixed }

	companyID := uuid.MustParse("99999999-9999-9999-9999-999999999999")
	actorID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	// Use SAME jti so canonical payload is identical.
	d := []byte(`{"jti":"FIXED-JTI-FOR-TEST"}`)
	require.NoError(t, ss.CreateAuditLog(context.Background(), companyID, actorID, "test.det", "r", "rid", "10.0.0.1", d))
	require.NoError(t, ss.CreateAuditLog(context.Background(), companyID, actorID, "test.det", "r", "rid", "10.0.0.1", d))

	require.Len(t, store.signedCalls, 2)
	// Payload (canonical JSON) inside each JWS must be byte-identical.
	a := middlePart(t, store.signedCalls[0].jwsCompact)
	b := middlePart(t, store.signedCalls[1].jwsCompact)
	require.Equal(t, a, b, "canonical payload differs across signs with fixed clock+input")
}

func TestSignedStore_VerifyAgainstSignerPublicKey_ES256(t *testing.T) {
	signer := newFakeECDSASigner(t)
	store := &recordingSignedStore{}
	ss, err := NewSignedStore(store, signer, "aoid")
	require.NoError(t, err)
	companyID := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	actorID := uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")
	require.NoError(t, ss.CreateAuditLog(context.Background(), companyID, actorID, "auth.user.login", "session", "id1", "10.0.0.1", []byte(`{}`)))

	jws := store.signedCalls[0].jwsCompact
	require.NotEmpty(t, jws)
	// VerifySignedEvent exposed for downstream verifiers.
	pub := &signer.priv.PublicKey
	got, err := VerifySignedEvent(jws, pub)
	require.NoError(t, err)
	require.Equal(t, "auth.user.login", got.Event.Action)
	require.Equal(t, "aoid", got.Issuer)
	require.NotZero(t, got.IssuedAt)
	require.Equal(t, "test-key-es256", got.KID)
}

func TestSignedStore_JWSHeaderContainsKidAlgTyp(t *testing.T) {
	signer := newFakeECDSASigner(t)
	store := &recordingSignedStore{}
	ss, err := NewSignedStore(store, signer, "aoid")
	require.NoError(t, err)
	companyID := uuid.MustParse("dddddddd-dddd-dddd-dddd-dddddddddddd")
	actorID := uuid.MustParse("eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee")
	require.NoError(t, ss.CreateAuditLog(context.Background(), companyID, actorID, "auth.user.login", "session", "id", "10.0.0.1", []byte(`{}`)))

	jws := store.signedCalls[0].jwsCompact
	parts := strings.Split(jws, ".")
	require.Len(t, parts, 3)
	hdrBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	require.NoError(t, err)
	var hdr map[string]string
	require.NoError(t, json.Unmarshal(hdrBytes, &hdr))
	require.Equal(t, "ES256", hdr["alg"])
	require.Equal(t, "test-key-es256", hdr["kid"])
	require.Equal(t, "JWT", hdr["typ"])
}

// --------------------------------------------------------------------------
// Helpers
// --------------------------------------------------------------------------

func middlePart(t *testing.T, jws string) string {
	t.Helper()
	parts := strings.Split(jws, ".")
	require.Len(t, parts, 3)
	return parts[1]
}

// verifyES256 parses a JWS Compact and verifies ES256 against pub.
func verifyES256(t *testing.T, jws string, pub *ecdsa.PublicKey, wantKID string) {
	t.Helper()
	parts := strings.Split(jws, ".")
	require.Len(t, parts, 3, "jws must have 3 parts")
	hdrBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	require.NoError(t, err)
	var hdr map[string]string
	require.NoError(t, json.Unmarshal(hdrBytes, &hdr))
	require.Equal(t, "ES256", hdr["alg"])
	require.Equal(t, wantKID, hdr["kid"])
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	require.NoError(t, err)
	require.Len(t, sig, 64, "ES256 raw r||s length must be 64")
	signingInput := parts[0] + "." + parts[1]
	digest := sha256.Sum256([]byte(signingInput))
	r := new(big.Int).SetBytes(sig[:32])
	s := new(big.Int).SetBytes(sig[32:])
	require.True(t, ecdsa.Verify(pub, digest[:], r, s), "ecdsa.Verify failed")
}

// verifyRS256 parses and verifies an RS256 JWS Compact.
func verifyRS256(t *testing.T, jws string, pub *rsa.PublicKey, wantKID string) {
	t.Helper()
	parts := strings.Split(jws, ".")
	require.Len(t, parts, 3)
	hdrBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	require.NoError(t, err)
	var hdr map[string]string
	require.NoError(t, json.Unmarshal(hdrBytes, &hdr))
	require.Equal(t, "RS256", hdr["alg"])
	require.Equal(t, wantKID, hdr["kid"])
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	require.NoError(t, err)
	signingInput := parts[0] + "." + parts[1]
	digest := sha256.Sum256([]byte(signingInput))
	require.NoError(t, rsa.VerifyPKCS1v15(pub, crypto.SHA256, digest[:], sig))
}
