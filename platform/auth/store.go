package auth

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// User represents a platform user. Consuming apps may extend this.
type User struct {
	ID           uuid.UUID
	Email        string
	PasswordHash string
	DisplayName  string
	IsActive     bool
	CreatedAt    time.Time
}

// Membership represents a user's membership in a company.
type Membership struct {
	CompanyID uuid.UUID
	UserID    uuid.UUID
	RoleID    uuid.UUID
	RoleName  string
}

// Role represents a role in the RBAC system.
type Role struct {
	ID          uuid.UUID
	Name        string
	Description string
	RoleLevel   int // 20=viewer, 40=member, 60=manager, 80=admin, 90=owner, 100=super_admin
}

// SSOConfig holds SSO provider configuration.
type SSOConfig struct {
	CompanyID    uuid.UUID
	Provider     string // "oidc", "saml", "microsoft", "google"
	IssuerURL    string
	ClientID     string
	ClientSecret string
	MetadataURL  string
	DisplayName  string
	ExtraScopes  []string
	EnforceSSO   bool
	IsActive     bool
}

// OAuthCredential stores a provider's access/refresh tokens for API use.
type OAuthCredential struct {
	CompanyID    uuid.UUID
	UserID       uuid.UUID
	Provider     string
	AccessToken  string
	RefreshToken string
	TokenExpiry  time.Time
	Scopes       []string
}

// RefreshTokenRecord represents a stored refresh token.
type RefreshTokenRecord struct {
	UserID    uuid.UUID
	TokenHash string
	ExpiresAt time.Time
}

// AuthStore defines the database operations needed by the auth package.
// Consuming apps implement this with their sqlc-generated code.
type AuthStore interface {
	// User operations
	GetUserByEmail(ctx context.Context, email string) (User, error)
	GetUserByID(ctx context.Context, id uuid.UUID) (User, error)
	CreateUser(ctx context.Context, email, passwordHash, displayName string) (User, error)

	// Company operations (replaces eden-circle's Org)
	CreateCompany(ctx context.Context, name, slug string) (uuid.UUID, error)
	CreateCompanyMembership(ctx context.Context, companyID, userID, roleID uuid.UUID) error

	// Membership & role operations
	GetCompanyMembershipByUser(ctx context.Context, userID uuid.UUID) (Membership, error)
	GetRoleByID(ctx context.Context, roleID uuid.UUID) (Role, error)
	GetUserRole(ctx context.Context, companyID, userID uuid.UUID) (Role, error)

	// Refresh token operations
	CreateRefreshToken(ctx context.Context, userID uuid.UUID, tokenHash string, expiresAt time.Time) error
	GetRefreshToken(ctx context.Context, tokenHash string) (RefreshTokenRecord, error)
	RevokeRefreshToken(ctx context.Context, tokenHash string) error

	// SSO operations
	GetSSOConfig(ctx context.Context, companyID uuid.UUID, provider string) (SSOConfig, error)
	ListSSOConfigs(ctx context.Context, companyID uuid.UUID) ([]SSOConfig, error)
	UpsertSSOConfig(ctx context.Context, cfg SSOConfig) error
	DeleteSSOConfig(ctx context.Context, companyID uuid.UUID, provider string) error
	HasEnforcedSSO(ctx context.Context, companyID uuid.UUID) (bool, error)

	// OAuth credential operations (provider access/refresh tokens for API use)
	UpsertOAuthCredential(ctx context.Context, cred OAuthCredential) error
	GetOAuthCredential(ctx context.Context, userID uuid.UUID, provider string) (OAuthCredential, error)

	// Audit (optional -- noop if nil)
	CreateAuditLog(ctx context.Context, companyID, actorID uuid.UUID, action, resource, resourceID, ipAddress string, details []byte) error
}

// TxAuthStore extends AuthStore with transaction support.
type TxAuthStore interface {
	AuthStore
	BeginTx(ctx context.Context) (TxAuthStore, error)
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}
