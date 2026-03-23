package auth_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/aocybersystems/eden-platform-go/platform/auth"
	"github.com/aocybersystems/eden-platform-go/platform/devstore"
)

func setupAuthService(t *testing.T) (*auth.Service, *devstore.Backend) {
	t.Helper()
	backend := devstore.NewMemoryBackend()
	jwtManager, err := auth.NewJWTManager(auth.JWTConfig{
		Issuer:             "test",
		AccessTokenExpiry:  time.Minute,
		RefreshTokenExpiry: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewJWTManager() error = %v", err)
	}
	return auth.NewService(backend.AuthStore(), jwtManager, auth.NewPasswordHasher()), backend
}

func TestService_SignUp_Success(t *testing.T) {
	svc, _ := setupAuthService(t)
	ctx := context.Background()

	resp, err := svc.SignUp(ctx, "test@example.com", "password123", "Test User")
	if err != nil {
		t.Fatalf("SignUp() error = %v", err)
	}

	if resp.AccessToken == "" {
		t.Errorf("SignUp() AccessToken is empty")
	}
	if resp.RefreshToken == "" {
		t.Errorf("SignUp() RefreshToken is empty")
	}
	if resp.User.Email != "test@example.com" {
		t.Errorf("SignUp() User.Email = %q, want %q", resp.User.Email, "test@example.com")
	}
	if resp.User.DisplayName != "Test User" {
		t.Errorf("SignUp() User.DisplayName = %q, want %q", resp.User.DisplayName, "Test User")
	}
	if !resp.User.IsActive {
		t.Errorf("SignUp() User.IsActive = false, want true")
	}
}

func TestService_SignUp_DuplicateEmail(t *testing.T) {
	svc, _ := setupAuthService(t)
	ctx := context.Background()

	_, err := svc.SignUp(ctx, "dupe@example.com", "password123", "User One")
	if err != nil {
		t.Fatalf("First SignUp() error = %v", err)
	}

	_, err = svc.SignUp(ctx, "dupe@example.com", "password456", "User Two")
	if err == nil {
		t.Fatalf("Second SignUp() expected error for duplicate email, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("Second SignUp() error = %q, want message containing 'already exists'", err.Error())
	}
}

func TestService_SignUp_InvalidEmail(t *testing.T) {
	svc, _ := setupAuthService(t)
	ctx := context.Background()

	tests := []struct {
		name  string
		email string
	}{
		{"empty email", ""},
		{"malformed email", "not-an-email"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.SignUp(ctx, tt.email, "password123", "Test User")
			if err == nil {
				t.Errorf("SignUp() with email %q expected error, got nil", tt.email)
			}
		})
	}
}

func TestService_SignUp_ShortPassword(t *testing.T) {
	svc, _ := setupAuthService(t)
	ctx := context.Background()

	_, err := svc.SignUp(ctx, "short@example.com", "short", "Test User")
	if err == nil {
		t.Fatalf("SignUp() with short password expected error, got nil")
	}
	if !strings.Contains(err.Error(), "at least 8 characters") {
		t.Errorf("SignUp() error = %q, want message about minimum characters", err.Error())
	}
}

func TestService_SignUp_EmptyDisplayName(t *testing.T) {
	svc, _ := setupAuthService(t)
	ctx := context.Background()

	_, err := svc.SignUp(ctx, "empty@example.com", "password123", "")
	if err == nil {
		t.Fatalf("SignUp() with empty display name expected error, got nil")
	}
	if !strings.Contains(err.Error(), "display name") {
		t.Errorf("SignUp() error = %q, want message about display name", err.Error())
	}
}

func TestService_Login_Success(t *testing.T) {
	svc, _ := setupAuthService(t)
	ctx := context.Background()

	_, err := svc.SignUp(ctx, "login@example.com", "password123", "Login User")
	if err != nil {
		t.Fatalf("SignUp() error = %v", err)
	}

	resp, err := svc.Login(ctx, "login@example.com", "password123")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	if resp.AccessToken == "" {
		t.Errorf("Login() AccessToken is empty")
	}
	if resp.RefreshToken == "" {
		t.Errorf("Login() RefreshToken is empty")
	}
	if resp.User.Email != "login@example.com" {
		t.Errorf("Login() User.Email = %q, want %q", resp.User.Email, "login@example.com")
	}
}

func TestService_Login_WrongPassword(t *testing.T) {
	svc, _ := setupAuthService(t)
	ctx := context.Background()

	_, err := svc.SignUp(ctx, "wrong@example.com", "password123", "Test User")
	if err != nil {
		t.Fatalf("SignUp() error = %v", err)
	}

	_, err = svc.Login(ctx, "wrong@example.com", "wrongpassword")
	if err == nil {
		t.Fatalf("Login() with wrong password expected error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid credentials") {
		t.Errorf("Login() error = %q, want 'invalid credentials'", err.Error())
	}
}

func TestService_Login_NonexistentUser(t *testing.T) {
	svc, _ := setupAuthService(t)
	ctx := context.Background()

	_, err := svc.Login(ctx, "nobody@example.com", "password123")
	if err == nil {
		t.Fatalf("Login() for nonexistent user expected error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid credentials") {
		t.Errorf("Login() error = %q, want 'invalid credentials'", err.Error())
	}
}

func TestService_RefreshToken_Success(t *testing.T) {
	svc, _ := setupAuthService(t)
	ctx := context.Background()

	signupResp, err := svc.SignUp(ctx, "refresh@example.com", "password123", "Refresh User")
	if err != nil {
		t.Fatalf("SignUp() error = %v", err)
	}

	resp, err := svc.RefreshToken(ctx, signupResp.RefreshToken)
	if err != nil {
		t.Fatalf("RefreshToken() error = %v", err)
	}

	if resp.AccessToken == "" {
		t.Errorf("RefreshToken() AccessToken is empty")
	}
	if resp.RefreshToken == "" {
		t.Errorf("RefreshToken() RefreshToken is empty")
	}
	if resp.RefreshToken == signupResp.RefreshToken {
		t.Errorf("RefreshToken() returned same refresh token (expected rotation)")
	}
}

func TestService_RefreshToken_RevokedToken(t *testing.T) {
	svc, _ := setupAuthService(t)
	ctx := context.Background()

	signupResp, err := svc.SignUp(ctx, "revoked@example.com", "password123", "Revoked User")
	if err != nil {
		t.Fatalf("SignUp() error = %v", err)
	}

	// First refresh succeeds and revokes the original token
	_, err = svc.RefreshToken(ctx, signupResp.RefreshToken)
	if err != nil {
		t.Fatalf("First RefreshToken() error = %v", err)
	}

	// Second refresh with original token should fail (already revoked)
	_, err = svc.RefreshToken(ctx, signupResp.RefreshToken)
	if err == nil {
		t.Fatalf("Second RefreshToken() with revoked token expected error, got nil")
	}
}

func TestService_RefreshToken_InvalidToken(t *testing.T) {
	svc, _ := setupAuthService(t)
	ctx := context.Background()

	_, err := svc.RefreshToken(ctx, "random-invalid-token")
	if err == nil {
		t.Fatalf("RefreshToken() with invalid token expected error, got nil")
	}
}

func TestService_Logout_RevokesToken(t *testing.T) {
	svc, _ := setupAuthService(t)
	ctx := context.Background()

	signupResp, err := svc.SignUp(ctx, "logout@example.com", "password123", "Logout User")
	if err != nil {
		t.Fatalf("SignUp() error = %v", err)
	}

	err = svc.Logout(ctx, signupResp.RefreshToken)
	if err != nil {
		t.Fatalf("Logout() error = %v", err)
	}

	// Refresh with logged-out token should fail
	_, err = svc.RefreshToken(ctx, signupResp.RefreshToken)
	if err == nil {
		t.Fatalf("RefreshToken() after logout expected error, got nil")
	}
}
