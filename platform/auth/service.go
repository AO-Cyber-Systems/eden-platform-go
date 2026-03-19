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
}

// NewService creates a new auth Service.
func NewService(store TxAuthStore, jwtManager *JWTManager, passwordHasher *PasswordHasher) *Service {
	return &Service{
		store:          store,
		jwtManager:     jwtManager,
		passwordHasher: passwordHasher,
	}
}

// Well-known role IDs
var (
	OwnerRoleID  = uuid.MustParse("10000000-0000-0000-0000-000000000001")
	AdminRoleID  = uuid.MustParse("10000000-0000-0000-0000-000000000002")
	MemberRoleID = uuid.MustParse("10000000-0000-0000-0000-000000000003")
	ViewerRoleID = uuid.MustParse("10000000-0000-0000-0000-000000000004")
)

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

	companySlug := generateSlug(displayName)
	companyName := fmt.Sprintf("%s's Company", strings.TrimSpace(displayName))
	companyID, err := tx.CreateCompany(ctx, companyName, companySlug)
	if err != nil {
		return nil, fmt.Errorf("create company: %w", err)
	}

	if err := tx.CreateCompanyMembership(ctx, companyID, user.ID, OwnerRoleID); err != nil {
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

// Logout revokes the provided refresh token.
func (s *Service) Logout(ctx context.Context, refreshTokenStr string) error {
	tokenHash := HashToken(refreshTokenStr)
	return s.store.RevokeRefreshToken(ctx, tokenHash)
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
