package cms

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func testSecret() []byte { return []byte("test-preview-signing-secret-do-not-use-in-prod") }

func TestMintAndVerifyPreviewToken_RoundTrips(t *testing.T) {
	pageID := uuid.New()
	companyID := uuid.New()

	token, expiresAt, err := MintPreviewToken(testSecret(), pageID, companyID)
	if err != nil {
		t.Fatalf("MintPreviewToken: %v", err)
	}
	if token == "" {
		t.Fatal("token is empty")
	}
	wantExpiry := time.Now().UTC().Add(PreviewTokenTTL)
	if expiresAt.Sub(wantExpiry).Abs() > time.Second {
		t.Fatalf("expiresAt = %v, want ~%v", expiresAt, wantExpiry)
	}

	grant, err := VerifyPreviewToken(testSecret(), token)
	if err != nil {
		t.Fatalf("VerifyPreviewToken: %v", err)
	}
	if grant.PageID != pageID {
		t.Errorf("grant.PageID = %v, want %v", grant.PageID, pageID)
	}
	if grant.CompanyID != companyID {
		t.Errorf("grant.CompanyID = %v, want %v", grant.CompanyID, companyID)
	}
	// Token expiry is carried at Unix-seconds precision (see previewExpiresLen);
	// the decoded grant loses sub-second precision relative to the value
	// MintPreviewToken returned, so compare with a sub-second tolerance.
	if grant.ExpiresAt.Sub(expiresAt).Abs() >= time.Second {
		t.Errorf("grant.ExpiresAt = %v, want ~%v", grant.ExpiresAt, expiresAt)
	}
}

// TestMintPreviewToken_Unguessable_UsesRandomNonce proves the "unguessable
// (crypto/rand)" must-have concretely: minting twice for the IDENTICAL
// (secret, pageID, companyID) never produces the same token, because a
// fresh crypto/rand nonce enters the signed payload every call. A purely
// deterministic HMAC(secret, pageID||companyID||ttl) design (as in the
// donor sources) would fail this test.
func TestMintPreviewToken_Unguessable_UsesRandomNonce(t *testing.T) {
	pageID := uuid.New()
	companyID := uuid.New()
	secret := testSecret()

	tokenA, _, err := mintPreviewToken(secret, pageID, companyID, time.Now(), PreviewTokenTTL)
	if err != nil {
		t.Fatalf("mint A: %v", err)
	}
	tokenB, _, err := mintPreviewToken(secret, pageID, companyID, time.Now(), PreviewTokenTTL)
	if err != nil {
		t.Fatalf("mint B: %v", err)
	}
	if tokenA == tokenB {
		t.Fatal("two mints for the same page/tenant produced an identical token — nonce is not random")
	}

	// Both must independently verify to the same scope.
	grantA, err := VerifyPreviewToken(secret, tokenA)
	if err != nil {
		t.Fatalf("verify A: %v", err)
	}
	grantB, err := VerifyPreviewToken(secret, tokenB)
	if err != nil {
		t.Fatalf("verify B: %v", err)
	}
	if grantA.PageID != grantB.PageID || grantA.CompanyID != grantB.CompanyID {
		t.Fatal("independently-minted tokens for the same scope decoded to different scopes")
	}
}

func TestVerifyPreviewToken_ExpiredToken_Refused(t *testing.T) {
	pageID := uuid.New()
	companyID := uuid.New()
	secret := testSecret()

	// Mint a token that "expired" 1 hour ago (issued 2h ago with a 1h ttl).
	token, expiresAt, err := mintPreviewToken(secret, pageID, companyID, time.Now().Add(-2*time.Hour), time.Hour)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if !expiresAt.Before(time.Now()) {
		t.Fatalf("test setup bug: expiresAt %v is not in the past", expiresAt)
	}

	_, err = VerifyPreviewToken(secret, token)
	if !errors.Is(err, ErrPreviewTokenExpired) {
		t.Fatalf("err = %v, want ErrPreviewTokenExpired", err)
	}
}

func TestVerifyPreviewToken_NotYetExpired_Accepted(t *testing.T) {
	pageID := uuid.New()
	companyID := uuid.New()
	secret := testSecret()

	// Issued now, expires 1 second from now — still valid.
	token, _, err := mintPreviewToken(secret, pageID, companyID, time.Now(), time.Second)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if _, err := VerifyPreviewToken(secret, token); err != nil {
		t.Fatalf("VerifyPreviewToken: %v", err)
	}
}

func TestVerifyPreviewToken_WrongSecret_Refused(t *testing.T) {
	pageID := uuid.New()
	companyID := uuid.New()

	token, _, err := MintPreviewToken(testSecret(), pageID, companyID)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}

	_, err = VerifyPreviewToken([]byte("a-completely-different-secret"), token)
	if !errors.Is(err, ErrPreviewTokenSignature) {
		t.Fatalf("err = %v, want ErrPreviewTokenSignature", err)
	}
}

func TestVerifyPreviewToken_TamperedPayload_Refused(t *testing.T) {
	pageID := uuid.New()
	companyID := uuid.New()
	secret := testSecret()

	token, _, err := MintPreviewToken(secret, pageID, companyID)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}

	// Flip a character in the middle of the token (well inside the
	// base64-encoded payload, not the final symbol) to simulate tampering
	// while keeping it decodable. The FINAL base64 symbol here encodes only
	// 2 real bits (88 raw bytes leaves a 1-byte remainder), and Go's
	// non-strict decoder ignores that symbol's unused low bits — flipping
	// only the last character can decode to the exact same bytes. A middle
	// character always flips real payload/signature bits.
	tampered := []byte(token)
	mid := len(tampered) / 2
	if tampered[mid] == 'A' {
		tampered[mid] = 'B'
	} else {
		tampered[mid] = 'A'
	}

	_, err = VerifyPreviewToken(secret, string(tampered))
	if err == nil {
		t.Fatal("tampered token verified successfully")
	}
	if !errors.Is(err, ErrPreviewTokenSignature) && !errors.Is(err, ErrPreviewTokenMalformed) {
		t.Fatalf("err = %v, want ErrPreviewTokenSignature or ErrPreviewTokenMalformed", err)
	}
}

func TestVerifyPreviewToken_MalformedToken_Refused(t *testing.T) {
	secret := testSecret()
	cases := []string{"", "not-base64url!!!", "dGVzdA", "AAAA"}
	for _, c := range cases {
		if _, err := VerifyPreviewToken(secret, c); err == nil {
			t.Errorf("token %q verified successfully, want an error", c)
		}
	}
}

func TestVerifyPreviewToken_EmptySecret_Refused(t *testing.T) {
	_, err := VerifyPreviewToken(nil, "anything")
	if !errors.Is(err, ErrPreviewSecretEmpty) {
		t.Fatalf("err = %v, want ErrPreviewSecretEmpty", err)
	}
}

func TestMintPreviewToken_EmptySecret_Refused(t *testing.T) {
	_, _, err := MintPreviewToken(nil, uuid.New(), uuid.New())
	if !errors.Is(err, ErrPreviewSecretEmpty) {
		t.Fatalf("err = %v, want ErrPreviewSecretEmpty", err)
	}
}

// TestAuthorize_ScopedToWrongPage_Refused proves the must_have: a token
// scoped to page A does not grant access to page B.
func TestAuthorize_ScopedToWrongPage_Refused(t *testing.T) {
	pageA := uuid.New()
	pageB := uuid.New()
	companyID := uuid.New()
	secret := testSecret()

	token, _, err := MintPreviewToken(secret, pageA, companyID)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}

	if _, err := Authorize(secret, token, pageA, companyID); err != nil {
		t.Fatalf("Authorize for the correct page failed: %v", err)
	}

	_, err = Authorize(secret, token, pageB, companyID)
	if !errors.Is(err, ErrPreviewTokenScope) {
		t.Fatalf("err = %v, want ErrPreviewTokenScope (page A token used for page B)", err)
	}
}

// TestAuthorize_ScopedToWrongTenant_Refused proves the must_have: a token
// scoped to one tenant does not grant access to another tenant's page,
// even for the SAME page ID (defensive: page IDs are not guaranteed
// globally unique across a multi-tenant caller's mental model).
func TestAuthorize_ScopedToWrongTenant_Refused(t *testing.T) {
	pageID := uuid.New()
	companyA := uuid.New()
	companyB := uuid.New()
	secret := testSecret()

	token, _, err := MintPreviewToken(secret, pageID, companyA)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}

	_, err = Authorize(secret, token, pageID, companyB)
	if !errors.Is(err, ErrPreviewTokenScope) {
		t.Fatalf("err = %v, want ErrPreviewTokenScope (tenant A token used for tenant B)", err)
	}
}

func TestMintPreviewToken_NilPageOrCompanyID_Rejected(t *testing.T) {
	secret := testSecret()
	if _, _, err := MintPreviewToken(secret, uuid.Nil, uuid.New()); err == nil {
		t.Error("nil pageID accepted")
	}
	if _, _, err := MintPreviewToken(secret, uuid.New(), uuid.Nil); err == nil {
		t.Error("nil companyID accepted")
	}
}
