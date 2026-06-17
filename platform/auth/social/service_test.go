package social

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aocybersystems/eden-platform-go/platform/auth"
	"github.com/google/uuid"
)

// --- Hand-built fakes (no generated test data) ---------------------------------

// fakeSocialStore implements auth.SocialStore in-memory.
type fakeSocialStore struct {
	byProviderSub map[string]auth.UserIdentity // key: provider|sub
	byEmail       map[string]auth.UserIdentity // key: email (only non-nil emails)
	upserts       int
}

func newFakeSocialStore() *fakeSocialStore {
	return &fakeSocialStore{
		byProviderSub: map[string]auth.UserIdentity{},
		byEmail:       map[string]auth.UserIdentity{},
	}
}

func psKey(provider, sub string) string { return provider + "|" + sub }

func (f *fakeSocialStore) UpsertUserIdentity(_ context.Context, identity auth.UserIdentity) (auth.UserIdentity, error) {
	f.upserts++
	if identity.ID == uuid.Nil {
		identity.ID = uuid.New()
	}
	now := time.Now()
	if identity.CreatedAt.IsZero() {
		identity.CreatedAt = now
	}
	identity.UpdatedAt = now
	f.byProviderSub[psKey(identity.Provider, identity.ProviderSub)] = identity
	if identity.Email != nil {
		f.byEmail[*identity.Email] = identity
	}
	return identity, nil
}

func (f *fakeSocialStore) GetUserIdentityByProviderSub(_ context.Context, provider, sub string) (auth.UserIdentity, error) {
	id, ok := f.byProviderSub[psKey(provider, sub)]
	if !ok {
		return auth.UserIdentity{}, fmt.Errorf("user identity not found")
	}
	return id, nil
}

func (f *fakeSocialStore) GetUserIdentityByEmail(_ context.Context, email string) (auth.UserIdentity, error) {
	id, ok := f.byEmail[email]
	if !ok {
		return auth.UserIdentity{}, fmt.Errorf("user identity not found")
	}
	return id, nil
}

func (f *fakeSocialStore) ListUserIdentitiesByUser(_ context.Context, userID uuid.UUID) ([]auth.UserIdentity, error) {
	var out []auth.UserIdentity
	for _, id := range f.byProviderSub {
		if id.UserID == userID {
			out = append(out, id)
		}
	}
	return out, nil
}

func (f *fakeSocialStore) DeleteUserIdentity(_ context.Context, id uuid.UUID) error {
	for k, v := range f.byProviderSub {
		if v.ID == id {
			delete(f.byProviderSub, k)
		}
	}
	return nil
}

// fakeAuthStore implements auth.AuthStore. Only GetUserByEmail and CreateUser
// carry behavior; the remaining methods are unused by Provision and return
// errors/zero values to satisfy the interface.
type fakeAuthStore struct {
	usersByEmail  map[string]auth.User
	usersByID     map[uuid.UUID]auth.User
	createdUsers  int
	refreshTokens []auth.RefreshTokenRecord
}

var _ auth.AuthStore = (*fakeAuthStore)(nil)

func newFakeAuthStore() *fakeAuthStore {
	return &fakeAuthStore{
		usersByEmail: map[string]auth.User{},
		usersByID:    map[uuid.UUID]auth.User{},
	}
}

func (f *fakeAuthStore) seedUser(email string) auth.User {
	u := auth.User{
		ID:          uuid.New(),
		Email:       strings.ToLower(strings.TrimSpace(email)),
		DisplayName: "Seeded",
		IsActive:    true,
		CreatedAt:   time.Now(),
	}
	f.usersByEmail[u.Email] = u
	f.usersByID[u.ID] = u
	return u
}

func (f *fakeAuthStore) GetUserByEmail(_ context.Context, email string) (auth.User, error) {
	u, ok := f.usersByEmail[strings.ToLower(strings.TrimSpace(email))]
	if !ok {
		return auth.User{}, fmt.Errorf("user not found")
	}
	return u, nil
}

func (f *fakeAuthStore) GetUserByID(_ context.Context, id uuid.UUID) (auth.User, error) {
	u, ok := f.usersByID[id]
	if !ok {
		return auth.User{}, fmt.Errorf("user not found")
	}
	return u, nil
}

func (f *fakeAuthStore) CreateUser(_ context.Context, email, passwordHash, displayName string) (auth.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if _, exists := f.usersByEmail[email]; exists {
		return auth.User{}, fmt.Errorf("duplicate key: email already exists")
	}
	f.createdUsers++
	u := auth.User{
		ID:           uuid.New(),
		Email:        email,
		PasswordHash: passwordHash,
		DisplayName:  displayName,
		IsActive:     true,
		CreatedAt:    time.Now(),
	}
	f.usersByEmail[email] = u
	f.usersByID[u.ID] = u
	return u, nil
}

func (f *fakeAuthStore) CreateRefreshToken(_ context.Context, userID uuid.UUID, tokenHash string, expiresAt time.Time) error {
	f.refreshTokens = append(f.refreshTokens, auth.RefreshTokenRecord{UserID: userID, TokenHash: tokenHash, ExpiresAt: expiresAt})
	return nil
}

// --- Unused AuthStore methods (interface satisfaction only) --------------------

func (f *fakeAuthStore) UpdateUser(context.Context, uuid.UUID, string, string) (auth.User, error) {
	return auth.User{}, fmt.Errorf("not implemented")
}
func (f *fakeAuthStore) CreateCompany(context.Context, string, string, string) (uuid.UUID, error) {
	return uuid.Nil, fmt.Errorf("not implemented")
}
func (f *fakeAuthStore) CreateCompanyMembership(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error {
	return fmt.Errorf("not implemented")
}
func (f *fakeAuthStore) GetCompanyMembershipByUser(context.Context, uuid.UUID) (auth.Membership, error) {
	return auth.Membership{}, fmt.Errorf("not implemented")
}
func (f *fakeAuthStore) GetRoleByID(context.Context, uuid.UUID) (auth.Role, error) {
	return auth.Role{}, fmt.Errorf("not implemented")
}
func (f *fakeAuthStore) GetUserRole(context.Context, uuid.UUID, uuid.UUID) (auth.Role, error) {
	return auth.Role{}, fmt.Errorf("not implemented")
}
func (f *fakeAuthStore) GetRefreshToken(context.Context, string) (auth.RefreshTokenRecord, error) {
	return auth.RefreshTokenRecord{}, fmt.Errorf("not implemented")
}
func (f *fakeAuthStore) RevokeRefreshToken(context.Context, string) error {
	return fmt.Errorf("not implemented")
}
func (f *fakeAuthStore) GetSSOConfig(context.Context, uuid.UUID, string) (auth.SSOConfig, error) {
	return auth.SSOConfig{}, fmt.Errorf("not implemented")
}
func (f *fakeAuthStore) ListSSOConfigs(context.Context, uuid.UUID) ([]auth.SSOConfig, error) {
	return nil, fmt.Errorf("not implemented")
}
func (f *fakeAuthStore) UpsertSSOConfig(context.Context, auth.SSOConfig) error {
	return fmt.Errorf("not implemented")
}
func (f *fakeAuthStore) DeleteSSOConfig(context.Context, uuid.UUID, string) error {
	return fmt.Errorf("not implemented")
}
func (f *fakeAuthStore) HasEnforcedSSO(context.Context, uuid.UUID) (bool, error) {
	return false, fmt.Errorf("not implemented")
}
func (f *fakeAuthStore) UpsertOAuthCredential(context.Context, auth.OAuthCredential) error {
	return fmt.Errorf("not implemented")
}
func (f *fakeAuthStore) GetOAuthCredential(context.Context, uuid.UUID, string) (auth.OAuthCredential, error) {
	return auth.OAuthCredential{}, fmt.Errorf("not implemented")
}
func (f *fakeAuthStore) CreateAuditLog(context.Context, uuid.UUID, uuid.UUID, string, string, string, string, []byte) error {
	return nil
}

// --- Test harness --------------------------------------------------------------

func newTestJWT(t *testing.T) *auth.JWTManager {
	t.Helper()
	jwt, err := auth.NewJWTManager(auth.JWTConfig{
		Issuer:             "test",
		AccessTokenExpiry:  time.Minute,
		RefreshTokenExpiry: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewJWTManager: %v", err)
	}
	return jwt
}

func newTestService(t *testing.T) (*SocialAuthService, *fakeSocialStore, *fakeAuthStore) {
	t.Helper()
	social := newFakeSocialStore()
	users := newFakeAuthStore()
	svc := NewSocialAuthService(social, users, newTestJWT(t), "https://example.test",
		[]string{"com.justindonnaruma.app://auth/social/callback"})
	return svc, social, users
}

// assertTokensIssued verifies a success path minted a non-empty access token
// and recorded exactly one refresh-token hash for the resolved user.
func assertTokensIssued(t *testing.T, resp *auth.AuthResponse, users *fakeAuthStore) {
	t.Helper()
	if resp == nil {
		t.Fatal("nil AuthResponse")
	}
	if resp.AccessToken == "" {
		t.Error("expected non-empty access token")
	}
	if resp.RefreshToken == "" {
		t.Error("expected non-empty refresh token")
	}
	if len(users.refreshTokens) != 1 {
		t.Fatalf("expected exactly 1 refresh-token hash recorded, got %d", len(users.refreshTokens))
	}
	if users.refreshTokens[0].UserID != resp.User.ID {
		t.Errorf("refresh token recorded for wrong user: got %s want %s", users.refreshTokens[0].UserID, resp.User.ID)
	}
	if users.refreshTokens[0].TokenHash != auth.HashToken(resp.RefreshToken) {
		t.Error("recorded refresh-token hash does not match HashToken(refreshToken)")
	}
}

// Case 1: repeat sign-in — provider_sub already known → existing user, no CreateUser.
func TestProvision_RepeatSignIn_ReturnsExistingUser(t *testing.T) {
	svc, social, users := newTestService(t)
	ctx := context.Background()

	existing := users.seedUser("repeat@example.com")
	email := "repeat@example.com"
	if _, err := social.UpsertUserIdentity(ctx, auth.UserIdentity{
		UserID: existing.ID, Provider: "google", ProviderSub: "google-sub-1",
		Email: &email, IsVerified: true,
	}); err != nil {
		t.Fatalf("seed identity: %v", err)
	}
	social.upserts = 0 // reset to observe only Provision's behavior

	resp, err := svc.Provision(ctx, Identity{
		Provider: "google", ProviderSub: "google-sub-1", Email: "repeat@example.com", EmailVerified: true,
	})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if resp.User.ID != existing.ID {
		t.Errorf("expected existing user %s, got %s", existing.ID, resp.User.ID)
	}
	if users.createdUsers != 0 {
		t.Errorf("expected no user creation on repeat sign-in, got %d", users.createdUsers)
	}
	if social.upserts != 0 {
		t.Errorf("expected no identity upsert on fast path, got %d", social.upserts)
	}
	assertTokensIssued(t, resp, users)
}

// Case 2: new identity, verified email, matching existing user → link, no new user.
func TestProvision_VerifiedEmail_LinksToExistingUser(t *testing.T) {
	svc, social, users := newTestService(t)
	ctx := context.Background()

	existing := users.seedUser("link@example.com")

	resp, err := svc.Provision(ctx, Identity{
		Provider: "google", ProviderSub: "google-sub-2", Email: "link@example.com", EmailVerified: true,
	})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if resp.User.ID != existing.ID {
		t.Errorf("expected link to existing user %s, got %s", existing.ID, resp.User.ID)
	}
	if users.createdUsers != 0 {
		t.Errorf("expected NO new user (link path), got %d created", users.createdUsers)
	}
	linked, err := social.GetUserIdentityByProviderSub(ctx, "google", "google-sub-2")
	if err != nil {
		t.Fatalf("expected identity row inserted: %v", err)
	}
	if linked.UserID != existing.ID {
		t.Errorf("identity linked to wrong user: got %s want %s", linked.UserID, existing.ID)
	}
	assertTokensIssued(t, resp, users)
}

// Case 3: new identity, verified email, no existing user → create user + identity.
func TestProvision_VerifiedEmail_NoUser_CreatesUserAndIdentity(t *testing.T) {
	svc, social, users := newTestService(t)
	ctx := context.Background()

	resp, err := svc.Provision(ctx, Identity{
		Provider: "google", ProviderSub: "google-sub-3", Email: "fresh@example.com",
		EmailVerified: true, DisplayName: "Fresh User",
	})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if users.createdUsers != 1 {
		t.Errorf("expected exactly 1 user created, got %d", users.createdUsers)
	}
	if resp.User.Email != "fresh@example.com" {
		t.Errorf("created user email = %q, want fresh@example.com", resp.User.Email)
	}
	identity, err := social.GetUserIdentityByProviderSub(ctx, "google", "google-sub-3")
	if err != nil {
		t.Fatalf("expected identity inserted: %v", err)
	}
	if identity.UserID != resp.User.ID {
		t.Errorf("identity user mismatch: got %s want %s", identity.UserID, resp.User.ID)
	}
	if identity.Email == nil || *identity.Email != "fresh@example.com" {
		t.Errorf("identity email = %v, want fresh@example.com", identity.Email)
	}
	assertTokensIssued(t, resp, users)
}

// Case 4: new identity, UNVERIFIED email, matching existing user → do NOT link;
// create a NEW user (account-takeover prevention).
func TestProvision_UnverifiedEmail_DoesNotLink_CreatesNewUser(t *testing.T) {
	svc, social, users := newTestService(t)
	ctx := context.Background()

	victim := users.seedUser("victim@example.com")

	resp, err := svc.Provision(ctx, Identity{
		Provider: "facebook", ProviderSub: "fb-sub-4", Email: "victim@example.com", EmailVerified: false,
	})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if resp.User.ID == victim.ID {
		t.Fatal("SECURITY: unverified email must NOT link to existing account")
	}
	if users.createdUsers != 1 {
		t.Errorf("expected a new user created (no link), got %d", users.createdUsers)
	}
	if strings.HasSuffix(resp.User.Email, "@social.placeholder") == false && resp.User.Email == victim.Email {
		t.Error("new user must not reuse victim's email identity")
	}
	identity, err := social.GetUserIdentityByProviderSub(ctx, "facebook", "fb-sub-4")
	if err != nil {
		t.Fatalf("expected identity inserted: %v", err)
	}
	if identity.UserID == victim.ID {
		t.Error("SECURITY: identity must not be linked to victim user")
	}
	assertTokensIssued(t, resp, users)
}

// Case 5: empty email (X case) → create user with placeholder email,
// user_identities.email NULL.
func TestProvision_EmailLess_CreatesPlaceholderUser_NullIdentityEmail(t *testing.T) {
	svc, social, users := newTestService(t)
	ctx := context.Background()

	resp, err := svc.Provision(ctx, Identity{
		Provider: "x", ProviderSub: "x-sub-5", Email: "", EmailVerified: false, DisplayName: "X User",
	})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if users.createdUsers != 1 {
		t.Errorf("expected 1 user created for email-less identity, got %d", users.createdUsers)
	}
	if !strings.HasSuffix(resp.User.Email, "@social.placeholder") {
		t.Errorf("expected placeholder email, got %q", resp.User.Email)
	}
	if !strings.Contains(resp.User.Email, "noemail+x-") {
		t.Errorf("placeholder must encode provider, got %q", resp.User.Email)
	}
	identity, err := social.GetUserIdentityByProviderSub(ctx, "x", "x-sub-5")
	if err != nil {
		t.Fatalf("expected identity inserted: %v", err)
	}
	if identity.Email != nil {
		t.Errorf("email-less identity must have NULL email, got %v", *identity.Email)
	}
	assertTokensIssued(t, resp, users)

	// Determinism: a second Provision for the same identity must not collide on
	// the users.email UNIQUE constraint (placeholder is stable per identity).
	resp2, err := svc.Provision(ctx, Identity{
		Provider: "x", ProviderSub: "x-sub-5", Email: "", EmailVerified: false,
	})
	if err != nil {
		t.Fatalf("second Provision (idempotent placeholder): %v", err)
	}
	if resp2.User.ID != resp.User.ID {
		t.Errorf("repeat email-less sign-in should return same user, got %s vs %s", resp2.User.ID, resp.User.ID)
	}
}

// Case 6 is asserted by assertTokensIssued across every success path above, but
// also verified independently here for the create path.
func TestProvision_EverySuccess_IssuesTokenAndRecordsRefreshHash(t *testing.T) {
	svc, _, users := newTestService(t)
	ctx := context.Background()

	resp, err := svc.Provision(ctx, Identity{
		Provider: "google", ProviderSub: "google-sub-6", Email: "tok@example.com", EmailVerified: true,
	})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	assertTokensIssued(t, resp, users)
}

// --- State JWT + redirect allowlist tests --------------------------------------

func TestStateJWT_RoundTrip_NoCompanyID(t *testing.T) {
	svc, _, _ := newTestService(t)

	state, err := svc.createStateJWT("x", "com.justindonnaruma.app://auth/social/callback", "pkce-verifier-abc", "nonce-xyz")
	if err != nil {
		t.Fatalf("createStateJWT: %v", err)
	}
	provider, redirect, pkce, nonce, err := svc.parseStateJWT(state)
	if err != nil {
		t.Fatalf("parseStateJWT: %v", err)
	}
	if provider != "x" {
		t.Errorf("provider = %q, want x", provider)
	}
	if redirect != "com.justindonnaruma.app://auth/social/callback" {
		t.Errorf("redirect = %q", redirect)
	}
	if pkce != "pkce-verifier-abc" {
		t.Errorf("pkce = %q", pkce)
	}
	if nonce != "nonce-xyz" {
		t.Errorf("nonce = %q", nonce)
	}
}

func TestIsAllowedRedirectURI(t *testing.T) {
	svc, _, _ := newTestService(t)

	if !svc.isAllowedRedirectURI("com.justindonnaruma.app://auth/social/callback") {
		t.Error("allowlisted redirect URI should be accepted")
	}
	if svc.isAllowedRedirectURI("https://evil.example.com/steal") {
		t.Error("non-allowlisted redirect URI must be rejected")
	}
	if svc.isAllowedRedirectURI("") {
		t.Error("empty redirect URI must be rejected")
	}
}
