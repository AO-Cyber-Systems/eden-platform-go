package pgstore

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/aocybersystems/eden-platform-go/internal/db"
	"github.com/aocybersystems/eden-platform-go/platform/auth"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

var _ auth.TxAuthStore = (*AuthStore)(nil)

// AuthStore implements auth.TxAuthStore backed by PostgreSQL via pgx and sqlc.
type AuthStore struct {
	pool *pgxpool.Pool // nil when wrapping a transaction
	dbtx db.DBTX
	tx   pgx.Tx // non-nil only for transactional instances
}

// NewAuthStore creates a new PostgreSQL-backed auth store.
func NewAuthStore(pool *pgxpool.Pool) *AuthStore {
	return &AuthStore{pool: pool, dbtx: pool}
}

func (s *AuthStore) queries() *db.Queries {
	return db.New(s.dbtx)
}

// -- User operations --

func (s *AuthStore) GetUserByEmail(ctx context.Context, email string) (auth.User, error) {
	row, err := s.queries().GetUserByEmail(ctx, email)
	if err != nil {
		if err == pgx.ErrNoRows {
			return auth.User{}, fmt.Errorf("user not found")
		}
		return auth.User{}, fmt.Errorf("get user by email: %w", err)
	}
	return dbUserToAuth(row), nil
}

func (s *AuthStore) GetUserByID(ctx context.Context, id uuid.UUID) (auth.User, error) {
	row, err := s.queries().GetUserByID(ctx, id)
	if err != nil {
		if err == pgx.ErrNoRows {
			return auth.User{}, fmt.Errorf("user not found")
		}
		return auth.User{}, fmt.Errorf("get user by id: %w", err)
	}
	return dbUserToAuth(row), nil
}

func (s *AuthStore) CreateUser(ctx context.Context, email, passwordHash, displayName string) (auth.User, error) {
	row, err := s.queries().CreateUser(ctx, db.CreateUserParams{
		Email:        email,
		PasswordHash: passwordHash,
		DisplayName:  displayName,
	})
	if err != nil {
		return auth.User{}, fmt.Errorf("create user: %w", err)
	}
	return dbUserToAuth(row), nil
}

// -- Company operations --

func (s *AuthStore) CreateCompany(ctx context.Context, name, slug string) (uuid.UUID, error) {
	id := uuid.New()
	row, err := s.queries().CreateCompany(ctx, db.CreateCompanyParams{
		ID:          id,
		Name:        name,
		Slug:        slug,
		CompanyType: "standalone",
		Settings:    json.RawMessage(`{"enabled_features":[]}`),
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("create company: %w", err)
	}
	return row.ID, nil
}

func (s *AuthStore) CreateCompanyMembership(ctx context.Context, companyID, userID, roleID uuid.UUID) error {
	return s.queries().CreateMembership(ctx, db.CreateMembershipParams{
		CompanyID: companyID,
		UserID:    userID,
		RoleID:    roleID,
	})
}

// -- Membership & role operations --

func (s *AuthStore) GetCompanyMembershipByUser(ctx context.Context, userID uuid.UUID) (auth.Membership, error) {
	// Get first company membership for this user
	companyIDs, err := s.queries().ListUserCompanyIDs(ctx, userID)
	if err != nil {
		return auth.Membership{}, fmt.Errorf("list user companies: %w", err)
	}
	if len(companyIDs) == 0 {
		return auth.Membership{}, fmt.Errorf("membership not found")
	}

	// Get membership details for first company
	row, err := s.queries().GetMembership(ctx, db.GetMembershipParams{
		CompanyID: companyIDs[0],
		UserID:    userID,
	})
	if err != nil {
		if err == pgx.ErrNoRows {
			return auth.Membership{}, fmt.Errorf("membership not found")
		}
		return auth.Membership{}, fmt.Errorf("get membership: %w", err)
	}
	return auth.Membership{
		CompanyID: row.CompanyID,
		UserID:    row.UserID,
		RoleID:    row.RoleID,
		RoleName:  row.RoleName,
	}, nil
}

func (s *AuthStore) GetRoleByID(ctx context.Context, roleID uuid.UUID) (auth.Role, error) {
	row, err := s.queries().GetRoleByID(ctx, roleID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return auth.Role{}, fmt.Errorf("role not found")
		}
		return auth.Role{}, fmt.Errorf("get role: %w", err)
	}
	return auth.Role{
		ID:          row.ID,
		Name:        row.Name,
		Description: row.Description,
		RoleLevel:   int(row.Level),
	}, nil
}

func (s *AuthStore) GetUserRole(ctx context.Context, companyID, userID uuid.UUID) (auth.Role, error) {
	row, err := s.queries().GetUserRole(ctx, db.GetUserRoleParams{
		CompanyID: companyID,
		UserID:    userID,
	})
	if err != nil {
		if err == pgx.ErrNoRows {
			return auth.Role{}, fmt.Errorf("role not found")
		}
		return auth.Role{}, fmt.Errorf("get user role: %w", err)
	}
	return auth.Role{
		ID:          row.ID,
		Name:        row.Name,
		Description: row.Description,
		RoleLevel:   int(row.Level),
	}, nil
}

// -- Refresh token operations --

func (s *AuthStore) CreateRefreshToken(ctx context.Context, userID uuid.UUID, tokenHash string, expiresAt time.Time) error {
	return s.queries().CreateRefreshToken(ctx, db.CreateRefreshTokenParams{
		UserID:    userID,
		TokenHash: tokenHash,
		ExpiresAt: expiresAt,
	})
}

func (s *AuthStore) GetRefreshToken(ctx context.Context, tokenHash string) (auth.RefreshTokenRecord, error) {
	row, err := s.queries().GetRefreshToken(ctx, tokenHash)
	if err != nil {
		if err == pgx.ErrNoRows {
			return auth.RefreshTokenRecord{}, fmt.Errorf("refresh token not found")
		}
		return auth.RefreshTokenRecord{}, fmt.Errorf("get refresh token: %w", err)
	}
	return auth.RefreshTokenRecord{
		UserID:    row.UserID,
		TokenHash: row.TokenHash,
		ExpiresAt: row.ExpiresAt,
	}, nil
}

func (s *AuthStore) RevokeRefreshToken(ctx context.Context, tokenHash string) error {
	return s.queries().RevokeRefreshToken(ctx, tokenHash)
}

// -- SSO operations --

func (s *AuthStore) GetSSOConfig(ctx context.Context, companyID uuid.UUID, provider string) (auth.SSOConfig, error) {
	row, err := s.queries().GetSSOConfig(ctx, db.GetSSOConfigParams{
		CompanyID: companyID,
		Provider:  provider,
	})
	if err != nil {
		if err == pgx.ErrNoRows {
			return auth.SSOConfig{}, fmt.Errorf("sso config not found")
		}
		return auth.SSOConfig{}, fmt.Errorf("get sso config: %w", err)
	}
	return auth.SSOConfig{
		CompanyID:    row.CompanyID,
		Provider:     row.Provider,
		IssuerURL:    row.IssuerUrl,
		ClientID:     row.ClientID,
		ClientSecret: row.ClientSecret,
		MetadataURL:  row.MetadataUrl,
	}, nil
}

// -- Audit --

func (s *AuthStore) CreateAuditLog(ctx context.Context, companyID, actorID uuid.UUID, action, resource, resourceID, ipAddress string, details []byte) error {
	return s.queries().CreateAuditLog(ctx, db.CreateAuditLogParams{
		CompanyID:  companyID,
		ActorID:    actorID,
		Action:     action,
		Resource:   resource,
		ResourceID: resourceID,
		Details:    json.RawMessage(details),
		IpAddress:  ipAddress,
	})
}

// -- Transaction support --

func (s *AuthStore) BeginTx(ctx context.Context) (auth.TxAuthStore, error) {
	if s.pool == nil {
		return nil, fmt.Errorf("cannot begin transaction: no pool available")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	return &AuthStore{pool: nil, dbtx: tx, tx: tx}, nil
}

func (s *AuthStore) Commit(ctx context.Context) error {
	if s.tx == nil {
		return fmt.Errorf("not in a transaction")
	}
	return s.tx.Commit(ctx)
}

func (s *AuthStore) Rollback(ctx context.Context) error {
	if s.tx == nil {
		return nil
	}
	return s.tx.Rollback(ctx)
}

// -- Type conversion helpers --

func dbUserToAuth(u db.User) auth.User {
	return auth.User{
		ID:           u.ID,
		Email:        u.Email,
		PasswordHash: u.PasswordHash,
		DisplayName:  u.DisplayName,
		IsActive:     u.IsActive,
		CreatedAt:    u.CreatedAt,
	}
}

// pgtypeUUID converts a pgtype.UUID to *uuid.UUID.
func pgtypeUUID(u pgtype.UUID) *uuid.UUID {
	if !u.Valid {
		return nil
	}
	id := uuid.UUID(u.Bytes)
	return &id
}

// uuidToPgtype converts a *uuid.UUID to pgtype.UUID.
func uuidToPgtype(u *uuid.UUID) pgtype.UUID {
	if u == nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: *u, Valid: true}
}
