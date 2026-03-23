package auth

import (
	"testing"
	"time"
)

func TestJWTManagerLoadsStableKeysAndValidatesClaims(t *testing.T) {
	manager, err := NewJWTManager(JWTConfig{
		PrivateKeyPath:     "../../dev/jwt/jwt_es256_private.pem",
		PublicKeyPath:      "../../dev/jwt/jwt_es256_public.pem",
		Issuer:             "eden-platform-test",
		AccessTokenExpiry:  time.Minute,
		RefreshTokenExpiry: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewJWTManager() error = %v", err)
	}

	token, err := manager.CreateAccessToken("user-1", "company-1", "owner", 90, []string{"company-1"})
	if err != nil {
		t.Fatalf("CreateAccessToken() error = %v", err)
	}

	claims, err := manager.ValidateAccessToken(token)
	if err != nil {
		t.Fatalf("ValidateAccessToken() error = %v", err)
	}

	if claims.UserID != "user-1" {
		t.Fatalf("expected user ID user-1, got %s", claims.UserID)
	}
	if claims.CompanyID != "company-1" {
		t.Fatalf("expected company ID company-1, got %s", claims.CompanyID)
	}
	if claims.Role != "owner" {
		t.Fatalf("expected role owner, got %s", claims.Role)
	}
}
