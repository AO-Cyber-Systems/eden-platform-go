package pki

import (
	"context"
	"crypto"
	"crypto/x509"
	"encoding/asn1"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ocsp"
)

// fakeOCSPStore is an in-process implementation of OCSPStore backed by a map.
// Hand-built, no LLM data — keys are serial.String() so big.Int equality works
// across copies.
type fakeOCSPStore struct {
	mu      sync.Mutex
	revoked map[string]*RevokedCert
}

func newFakeOCSPStore() *fakeOCSPStore {
	return &fakeOCSPStore{revoked: make(map[string]*RevokedCert)}
}

func (s *fakeOCSPStore) Set(r RevokedCert) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r2 := r
	s.revoked[r.SerialNumber.String()] = &r2
}

func (s *fakeOCSPStore) GetRevocation(_ context.Context, serial *big.Int) (*RevokedCert, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v, ok := s.revoked[serial.String()]; ok {
		return v, nil
	}
	return nil, nil
}

// buildRequest builds an OCSP request DER for (serial). issuer is the CA cert
// the OCSP request is *about*. If nonce is non-nil, an OCSP nonce extension is
// added.
func buildRequest(t *testing.T, issuer *x509.Certificate, serial *big.Int, nonce []byte) []byte {
	t.Helper()
	// We need a parsed leaf certificate to use ocsp.CreateRequest; the test
	// signs a throw-away leaf to anchor the serial number.
	leafKey := newP256Signer(t)
	template := &x509.Certificate{
		SerialNumber: serial,
		NotBefore:    time.Now().Add(-1 * time.Minute),
		NotAfter:     time.Now().Add(1 * time.Hour),
	}
	der, err := x509.CreateCertificate(nil, template, issuer, &leafKey.PublicKey, leafKey)
	require.NoError(t, err)
	leaf, err := x509.ParseCertificate(der)
	require.NoError(t, err)

	var opts *ocsp.RequestOptions
	if nonce != nil {
		opts = &ocsp.RequestOptions{Hash: crypto.SHA1}
	}
	reqDER, err := ocsp.CreateRequest(leaf, issuer, opts)
	require.NoError(t, err)

	if nonce != nil {
		// ocsp.CreateRequest does NOT include a nonce extension. We rebuild
		// the request manually to add one. Round-trip through stdlib via
		// asn1 marshal/unmarshal would be heavy; instead use a literal
		// pre-built nonce-bearing request by injecting the extension via
		// the asn1 layer.
		//
		// Strategy: marshal an OCTET STRING containing the nonce as the
		// extension value, append to the parsed request's RequestExtensions,
		// then return a NEW request DER. We do this by parsing reqDER,
		// modifying the OCSP request structure, and re-encoding.
		reqDER = injectNonce(t, reqDER, nonce)
	}
	return reqDER
}

// injectNonce parses an OCSP request DER and re-encodes it with a nonce
// extension. Hand-rolled to avoid LLM-generated wire bytes.
//
// OCSP request ASN.1 (RFC 6960 §4.1.1):
//
//	OCSPRequest ::= SEQUENCE {
//	    tbsRequest               TBSRequest,
//	    optionalSignature   [0]  EXPLICIT Signature OPTIONAL }
//	TBSRequest ::= SEQUENCE {
//	    version             [0]  EXPLICIT Version DEFAULT v1,
//	    requestorName       [1]  EXPLICIT GeneralName OPTIONAL,
//	    requestList              SEQUENCE OF Request,
//	    requestExtensions   [2]  EXPLICIT Extensions OPTIONAL }
func injectNonce(t *testing.T, reqDER []byte, nonce []byte) []byte {
	t.Helper()
	// Use the asn1 package: unmarshal into a raw structure, append the
	// extension, re-marshal.
	type request struct {
		TBSRequest tbsRequest
		// Signature is optional and unused in our tests.
	}

	var r request
	rest, err := asn1.Unmarshal(reqDER, &r)
	require.NoError(t, err)
	require.Empty(t, rest)

	nonceValue, err := asn1.Marshal(nonce)
	require.NoError(t, err)
	r.TBSRequest.RequestExtensions = append(r.TBSRequest.RequestExtensions, pkixExtension{
		Id:    asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 48, 1, 2},
		Value: nonceValue,
	})

	out, err := asn1.Marshal(r)
	require.NoError(t, err)
	return out
}

// Minimal ASN.1 types to round-trip an OCSP request with extensions. We use
// asn1.RawValue for fields we don't care about so the marshaling is
// transparent.
type tbsRequest struct {
	Version           int                  `asn1:"optional,explicit,tag:0,default:0"`
	RequestorName     asn1.RawValue        `asn1:"optional,explicit,tag:1"`
	RequestList       []asn1.RawValue      `asn1:"sequence"`
	RequestExtensions []pkixExtension      `asn1:"optional,explicit,tag:2"`
}

type pkixExtension struct {
	Id    asn1.ObjectIdentifier
	Value []byte // OCTET STRING wrapping the nonce
}

func TestOCSP_GoodForUnknownSerial(t *testing.T) {
	_, ca, _ := newTestCA(t)
	store := newFakeOCSPStore()
	resp := NewOCSPResponder(ca.Intermediate(), ca.Signer(), store, 10*time.Minute)

	reqDER := buildRequest(t, ca.Intermediate(), big.NewInt(999), nil)
	respDER, ct, err := resp.RespondToRequest(context.Background(), reqDER)
	require.NoError(t, err)
	require.Equal(t, "application/ocsp-response", ct)

	parsed, err := ocsp.ParseResponse(respDER, ca.Intermediate())
	require.NoError(t, err)
	require.Equal(t, ocsp.Good, parsed.Status)
}

func TestOCSP_RevokedSerial(t *testing.T) {
	_, ca, _ := newTestCA(t)
	store := newFakeOCSPStore()
	revokedAt := time.Now().UTC().Truncate(time.Second).Add(-1 * time.Hour)
	store.Set(RevokedCert{
		SerialNumber: big.NewInt(42),
		RevokedAt:    revokedAt,
		Reason:       1, // ocsp.KeyCompromise
	})

	resp := NewOCSPResponder(ca.Intermediate(), ca.Signer(), store, 10*time.Minute)
	reqDER := buildRequest(t, ca.Intermediate(), big.NewInt(42), nil)
	respDER, _, err := resp.RespondToRequest(context.Background(), reqDER)
	require.NoError(t, err)
	parsed, err := ocsp.ParseResponse(respDER, ca.Intermediate())
	require.NoError(t, err)
	require.Equal(t, ocsp.Revoked, parsed.Status)
	require.True(t, parsed.RevokedAt.Equal(revokedAt), "RevokedAt mismatch: got %v want %v", parsed.RevokedAt, revokedAt)
	require.Equal(t, 1, parsed.RevocationReason)
}

func TestOCSP_NonceEchoedInResponse(t *testing.T) {
	_, ca, _ := newTestCA(t)
	resp := NewOCSPResponder(ca.Intermediate(), ca.Signer(), newFakeOCSPStore(), 10*time.Minute)

	nonce := []byte{0xDE, 0xAD, 0xBE, 0xEF, 0xCA, 0xFE, 0xBA, 0xBE}
	reqDER := buildRequest(t, ca.Intermediate(), big.NewInt(7), nonce)
	respDER, _, err := resp.RespondToRequest(context.Background(), reqDER)
	require.NoError(t, err)
	parsed, err := ocsp.ParseResponse(respDER, ca.Intermediate())
	require.NoError(t, err)

	// Locate nonce extension in parsed response.
	nonceOIDLocal := asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 48, 1, 2}
	found := false
	for _, ext := range parsed.Extensions {
		if ext.Id.Equal(nonceOIDLocal) {
			found = true
			// Unwrap the OCTET STRING.
			var got []byte
			_, err := asn1.Unmarshal(ext.Value, &got)
			require.NoError(t, err)
			require.Equal(t, nonce, got, "echoed nonce must match request")
		}
	}
	require.True(t, found, "response must carry nonce extension")
}

func TestOCSP_NoNonceInRequest(t *testing.T) {
	_, ca, _ := newTestCA(t)
	resp := NewOCSPResponder(ca.Intermediate(), ca.Signer(), newFakeOCSPStore(), 10*time.Minute)

	reqDER := buildRequest(t, ca.Intermediate(), big.NewInt(7), nil)
	respDER, _, err := resp.RespondToRequest(context.Background(), reqDER)
	require.NoError(t, err)
	parsed, err := ocsp.ParseResponse(respDER, ca.Intermediate())
	require.NoError(t, err)

	nonceOIDLocal := asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 48, 1, 2}
	for _, ext := range parsed.Extensions {
		require.False(t, ext.Id.Equal(nonceOIDLocal), "response must NOT carry nonce when request had none")
	}
}

func TestOCSP_MalformedRequest(t *testing.T) {
	_, ca, _ := newTestCA(t)
	resp := NewOCSPResponder(ca.Intermediate(), ca.Signer(), newFakeOCSPStore(), 10*time.Minute)
	_, _, err := resp.RespondToRequest(context.Background(), []byte{0x00, 0x01, 0x02})
	require.Error(t, err)
}

func TestOCSP_SignatureValidates(t *testing.T) {
	_, ca, _ := newTestCA(t)
	resp := NewOCSPResponder(ca.Intermediate(), ca.Signer(), newFakeOCSPStore(), 10*time.Minute)
	reqDER := buildRequest(t, ca.Intermediate(), big.NewInt(7), nil)
	respDER, _, err := resp.RespondToRequest(context.Background(), reqDER)
	require.NoError(t, err)

	// ocsp.ParseResponse with a non-nil issuer calls CheckSignatureFrom
	// internally.
	parsed, err := ocsp.ParseResponse(respDER, ca.Intermediate())
	require.NoError(t, err)
	require.NotNil(t, parsed)
	require.NoError(t, parsed.CheckSignatureFrom(ca.Intermediate()))
}
