package federation

import (
	"context"
	"errors"
	"testing"

	"github.com/aocybersystems/eden-platform-go/platform/auth"
	"github.com/aocybersystems/eden-platform-go/platform/devstore"
)

func newProvisionSvc(t *testing.T) *auth.Service {
	t.Helper()
	backend := devstore.NewMemoryBackend()
	jwtCfg := auth.DefaultJWTConfig()
	jwt, err := auth.NewJWTManager(jwtCfg)
	if err != nil {
		t.Fatalf("NewJWTManager: %v", err)
	}
	return auth.NewService(backend.AuthStore(), jwt, auth.NewPasswordHasher())
}

func TestProvision_NewUser(t *testing.T) {
	svc := newProvisionSvc(t)
	user, isNew, err := Provision(context.Background(), svc, "u@acme.com", "U User", JITPolicy{Enabled: true})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if !isNew {
		t.Errorf("expected isNew=true")
	}
	if user.Email != "u@acme.com" {
		t.Errorf("Email: %q", user.Email)
	}
	if user.DisplayName != "U User" {
		t.Errorf("DisplayName: %q", user.DisplayName)
	}
	if !contains(user.PasswordHash, "fed:") {
		t.Errorf("PasswordHash should be unusable (got %q)", user.PasswordHash)
	}
}

func TestProvision_ExistingUser(t *testing.T) {
	svc := newProvisionSvc(t)
	_, _ = svc.CreateUser(context.Background(), "u@acme.com", "$2a$10$dummy", "Original")
	user, isNew, err := Provision(context.Background(), svc, "u@acme.com", "Original", JITPolicy{Enabled: false})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if isNew {
		t.Errorf("expected isNew=false")
	}
	if user.Email != "u@acme.com" {
		t.Errorf("Email: %q", user.Email)
	}
}

func TestProvision_JITDisabled(t *testing.T) {
	svc := newProvisionSvc(t)
	_, _, err := Provision(context.Background(), svc, "unknown@acme.com", "", JITPolicy{Enabled: false})
	if !errors.Is(err, ErrFederationUserNotFound) {
		t.Errorf("expected ErrFederationUserNotFound, got %v", err)
	}
}

func TestProvision_DomainRejected(t *testing.T) {
	svc := newProvisionSvc(t)
	policy := JITPolicy{Enabled: true, AllowedDomains: []string{"acme.com"}}
	_, _, err := Provision(context.Background(), svc, "user@evil.com", "", policy)
	if !errors.Is(err, ErrJITDomainNotAllowed) {
		t.Errorf("expected ErrJITDomainNotAllowed, got %v", err)
	}
}

func TestProvision_DefaultDisplayName(t *testing.T) {
	svc := newProvisionSvc(t)
	user, _, err := Provision(context.Background(), svc, "u@acme.com", "", JITPolicy{Enabled: true})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if user.DisplayName != "u" {
		t.Errorf("DisplayName default: got %q, want %q", user.DisplayName, "u")
	}
}

func TestDomainAllowed(t *testing.T) {
	cases := []struct {
		domains []string
		email   string
		ok      bool
	}{
		{nil, "a@b.com", true},
		{[]string{}, "a@b.com", true},
		{[]string{"acme.com"}, "u@acme.com", true},
		{[]string{"ACME.com"}, "u@acme.com", true},
		{[]string{"acme.com"}, "u@evil.com", false},
		{[]string{"acme.com"}, "no-at-sign", false},
		{[]string{"acme.com"}, "u@", false},
	}
	for _, tc := range cases {
		if got := domainAllowed(JITPolicy{AllowedDomains: tc.domains}, tc.email); got != tc.ok {
			t.Errorf("domainAllowed(%v, %q) = %v, want %v", tc.domains, tc.email, got, tc.ok)
		}
	}
}
