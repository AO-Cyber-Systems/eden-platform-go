package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/mail"
	"strings"
	"time"

	"github.com/aocybersystems/eden-platform-go/platform/rbac"
	"github.com/google/uuid"
)

// AuthResponse contains the tokens and user info returned after successful authentication.
type AuthResponse struct {
	AccessToken  string
	RefreshToken string
	User         User
}

// Service contains the business logic for authentication operations.
type Service struct {
	store          TxAuthStore
	jwtManager     *JWTManager
	passwordHasher *PasswordHasher
	b2cMode        bool
}

// NewService creates a new auth Service.
// When b2cMode is true, signup creates a personal workspace instead of a named company.
func NewService(store TxAuthStore, jwtManager *JWTManager, passwordHasher *PasswordHasher, b2cMode ...bool) *Service {
	mode := false
	if len(b2cMode) > 0 {
		mode = b2cMode[0]
	}
	return &Service{
		store:          store,
		jwtManager:     jwtManager,
		passwordHasher: passwordHasher,
		b2cMode:        mode,
	}
}

// SignUp creates a new user account, a default company, and assigns the user as owner.
func (s *Service) SignUp(ctx context.Context, email, password, displayName string) (*AuthResponse, error) {
	if err := validateEmail(email); err != nil {
		return nil, fmt.Errorf("invalid email: %w", err)
	}
	if len(password) < 8 {
		return nil, fmt.Errorf("password must be at least 8 characters")
	}
	if strings.TrimSpace(displayName) == "" {
		return nil, fmt.Errorf("display name is required")
	}

	hashedPassword, err := s.passwordHasher.Hash(password)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	tx, err := s.store.BeginTx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	email = strings.ToLower(strings.TrimSpace(email))
	user, err := tx.CreateUser(ctx, email, hashedPassword, strings.TrimSpace(displayName))
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint") {
			return nil, fmt.Errorf("an account with this email already exists")
		}
		return nil, fmt.Errorf("create user: %w", err)
	}

	var companyName, companySlug, companyType string
	if s.b2cMode {
		companyName = strings.TrimSpace(displayName)
		companySlug = fmt.Sprintf("personal-%s", user.ID.String()[:8])
		companyType = "personal"
	} else {
		companyName = fmt.Sprintf("%s's Company", strings.TrimSpace(displayName))
		companySlug = generateSlug(displayName)
		companyType = "standalone"
	}
	companyID, err := tx.CreateCompany(ctx, companyName, companySlug, companyType)
	if err != nil {
		return nil, fmt.Errorf("create company: %w", err)
	}

	if err := tx.CreateCompanyMembership(ctx, companyID, user.ID, rbac.OwnerRoleID); err != nil {
		return nil, fmt.Errorf("create company membership: %w", err)
	}

	companyIDStr := companyID.String()
	accessToken, err := s.jwtManager.CreateAccessToken(user.ID.String(), companyIDStr, "owner", 90, []string{companyIDStr})
	if err != nil {
		return nil, fmt.Errorf("create access token: %w", err)
	}

	refreshToken, err := s.jwtManager.CreateRefreshToken(user.ID.String())
	if err != nil {
		return nil, fmt.Errorf("create refresh token: %w", err)
	}

	tokenHash := HashToken(refreshToken)
	if err := tx.CreateRefreshToken(ctx, user.ID, tokenHash, time.Now().Add(s.jwtManager.config.RefreshTokenExpiry)); err != nil {
		return nil, fmt.Errorf("store refresh token: %w", err)
	}

	_ = tx.CreateAuditLog(ctx, companyID, user.ID, "user.signup", "user", user.ID.String(), "", []byte("{}"))

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return &AuthResponse{AccessToken: accessToken, RefreshToken: refreshToken, User: user}, nil
}

// Login authenticates a user with email and password.
func (s *Service) Login(ctx context.Context, email, password string) (*AuthResponse, error) {
	genericErr := fmt.Errorf("invalid credentials")

	user, err := s.store.GetUserByEmail(ctx, strings.ToLower(strings.TrimSpace(email)))
	if err != nil {
		slog.Debug("login: user not found", "email", email)
		return nil, genericErr
	}
	if !user.IsActive {
		return nil, genericErr
	}

	match, err := s.passwordHasher.Verify(password, user.PasswordHash)
	if err != nil || !match {
		return nil, genericErr
	}

	membership, err := s.store.GetCompanyMembershipByUser(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("get company membership: %w", err)
	}

	role, err := s.store.GetRoleByID(ctx, membership.RoleID)
	if err != nil {
		return nil, fmt.Errorf("get role: %w", err)
	}

	companyIDStr := membership.CompanyID.String()
	accessToken, err := s.jwtManager.CreateAccessToken(user.ID.String(), companyIDStr, role.Name, role.RoleLevel, []string{companyIDStr})
	if err != nil {
		return nil, fmt.Errorf("create access token: %w", err)
	}

	refreshToken, err := s.jwtManager.CreateRefreshToken(user.ID.String())
	if err != nil {
		return nil, fmt.Errorf("create refresh token: %w", err)
	}

	tokenHash := HashToken(refreshToken)
	if err := s.store.CreateRefreshToken(ctx, user.ID, tokenHash, time.Now().Add(s.jwtManager.config.RefreshTokenExpiry)); err != nil {
		return nil, fmt.Errorf("store refresh token: %w", err)
	}

	_ = s.store.CreateAuditLog(ctx, membership.CompanyID, user.ID, "user.login", "user", user.ID.String(), "", []byte("{}"))

	return &AuthResponse{AccessToken: accessToken, RefreshToken: refreshToken, User: user}, nil
}

// RefreshToken validates, revokes old, and issues new token pair.
func (s *Service) RefreshToken(ctx context.Context, refreshTokenStr string) (*AuthResponse, error) {
	_, err := s.jwtManager.ValidateRefreshToken(refreshTokenStr)
	if err != nil {
		return nil, fmt.Errorf("invalid refresh token")
	}

	tokenHash := HashToken(refreshTokenStr)
	storedToken, err := s.store.GetRefreshToken(ctx, tokenHash)
	if err != nil {
		return nil, fmt.Errorf("refresh token not found or expired")
	}

	if err := s.store.RevokeRefreshToken(ctx, tokenHash); err != nil {
		return nil, fmt.Errorf("revoke old refresh token: %w", err)
	}

	user, err := s.store.GetUserByID(ctx, storedToken.UserID)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}

	membership, err := s.store.GetCompanyMembershipByUser(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("get company membership: %w", err)
	}

	role, err := s.store.GetRoleByID(ctx, membership.RoleID)
	if err != nil {
		return nil, fmt.Errorf("get role: %w", err)
	}

	companyIDStr := membership.CompanyID.String()
	newAccessToken, err := s.jwtManager.CreateAccessToken(user.ID.String(), companyIDStr, role.Name, role.RoleLevel, []string{companyIDStr})
	if err != nil {
		return nil, fmt.Errorf("create access token: %w", err)
	}

	newRefreshToken, err := s.jwtManager.CreateRefreshToken(user.ID.String())
	if err != nil {
		return nil, fmt.Errorf("create refresh token: %w", err)
	}

	newTokenHash := HashToken(newRefreshToken)
	if err := s.store.CreateRefreshToken(ctx, user.ID, newTokenHash, time.Now().Add(s.jwtManager.config.RefreshTokenExpiry)); err != nil {
		return nil, fmt.Errorf("store new refresh token: %w", err)
	}

	return &AuthResponse{AccessToken: newAccessToken, RefreshToken: newRefreshToken, User: user}, nil
}

// UpdateProfile updates the user's display name and avatar URL.
func (s *Service) UpdateProfile(ctx context.Context, userID uuid.UUID, displayName, avatarURL string) (User, error) {
	if strings.TrimSpace(displayName) == "" {
		return User{}, fmt.Errorf("display name is required")
	}
	return s.store.UpdateUser(ctx, userID, strings.TrimSpace(displayName), strings.TrimSpace(avatarURL))
}

// Logout revokes the provided refresh token.
func (s *Service) Logout(ctx context.Context, refreshTokenStr string) error {
	tokenHash := HashToken(refreshTokenStr)
	return s.store.RevokeRefreshToken(ctx, tokenHash)
}

// RememberRefreshToken records a freshly-minted refresh token in the
// auth store so it can be rotated later via RefreshToken.
//
// Used by external issuers (e.g. AO ID) that mint a refresh token via
// JWTManager.CreateRefreshToken outside the SignUp/Login pipeline and
// still need rotation to work. The platform's own SignUp + Login paths
// embed this call inside their transactions.
func (s *Service) RememberRefreshToken(ctx context.Context, userID uuid.UUID, refreshTokenStr string, expiresAt time.Time) error {
	tokenHash := HashToken(refreshTokenStr)
	return s.store.CreateRefreshToken(ctx, userID, tokenHash, expiresAt)
}

// GetUserByID resolves a user by ID. The auth.Service does not own a
// public user lookup; this thin pass-through exposes the underlying
// store for issuer code that needs claim values without re-importing
// the store interface.
func (s *Service) GetUserByID(ctx context.Context, id uuid.UUID) (User, error) {
	return s.store.GetUserByID(ctx, id)
}

// GetUserByEmail resolves a user by email. Same shape as
// GetUserByID — exposed so federation flows (aoid Bridge) can JIT-
// provision without re-importing the store interface.
func (s *Service) GetUserByEmail(ctx context.Context, email string) (User, error) {
	return s.store.GetUserByEmail(ctx, strings.ToLower(strings.TrimSpace(email)))
}

// CreateUser inserts a new user. Federation callers supply an unusable
// password hash (e.g. "fed:<random>") to disable password login for
// SSO-only accounts. Companies and memberships are NOT created here —
// that remains the caller's responsibility.
func (s *Service) CreateUser(ctx context.Context, email, passwordHash, displayName string) (User, error) {
	return s.store.CreateUser(ctx, strings.ToLower(strings.TrimSpace(email)), passwordHash, strings.TrimSpace(displayName))
}

// HashToken computes SHA-256 hash of a token for database storage.
func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func validateEmail(email string) error {
	_, err := mail.ParseAddress(email)
	return err
}

func generateSlug(name string) string {
	slug := strings.ToLower(strings.TrimSpace(name))
	slug = strings.ReplaceAll(slug, " ", "-")
	var result strings.Builder
	for _, c := range slug {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			result.WriteRune(c)
		}
	}
	s := result.String()
	if s == "" {
		s = "default-company"
	}
	return s + "-" + time.Now().Format("150405")
}
