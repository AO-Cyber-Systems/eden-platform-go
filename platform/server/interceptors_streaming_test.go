package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	connect "connectrpc.com/connect"
	"github.com/aocybersystems/eden-platform-go/platform/auth"
	"github.com/aocybersystems/eden-platform-go/platform/rbac"
	"github.com/google/uuid"
)

// fakeStreamConn is a minimal connect.StreamingHandlerConn for interceptor tests.
type fakeStreamConn struct {
	procedure string
	header    http.Header
}

func newFakeStreamConn(procedure, authHeader string) *fakeStreamConn {
	h := http.Header{}
	if authHeader != "" {
		h.Set("Authorization", authHeader)
	}
	return &fakeStreamConn{procedure: procedure, header: h}
}

func (f *fakeStreamConn) Spec() connect.Spec {
	return connect.Spec{Procedure: f.procedure, StreamType: connect.StreamTypeServer}
}
func (f *fakeStreamConn) Peer() connect.Peer           { return connect.Peer{} }
func (f *fakeStreamConn) Receive(any) error            { return io.EOF }
func (f *fakeStreamConn) RequestHeader() http.Header   { return f.header }
func (f *fakeStreamConn) Send(any) error               { return nil }
func (f *fakeStreamConn) ResponseHeader() http.Header  { return http.Header{} }
func (f *fakeStreamConn) ResponseTrailer() http.Header { return http.Header{} }

func newTestJWTManager(t *testing.T) *auth.JWTManager {
	t.Helper()
	m, err := auth.NewJWTManager(auth.JWTConfig{
		Issuer:            "test",
		AccessTokenExpiry: time.Minute,
	})
	if err != nil {
		t.Fatalf("NewJWTManager: %v", err)
	}
	return m
}

// TestAuthInterceptor_Streaming_RejectsMissingOrInvalidBearer locks in the fix
// for the dormant streaming bypass: prior to this commit, NewAuthInterceptor
// returned connect.UnaryInterceptorFunc, whose WrapStreamingHandler is a
// no-op pass-through. Streaming RPCs reached the handler without auth.
func TestAuthInterceptor_Streaming_RejectsMissingOrInvalidBearer(t *testing.T) {
	mgr := newTestJWTManager(t)
	interceptor := NewAuthInterceptor(mgr, nil)

	cases := []struct {
		name, header string
	}{
		{"missing_header", ""},
		{"not_bearer_scheme", "Basic abc"},
		{"malformed_bearer", "Bearer garbage.not.a.token"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			invoked := false
			handler := connect.StreamingHandlerFunc(func(ctx context.Context, conn connect.StreamingHandlerConn) error {
				invoked = true
				return nil
			})
			wrapped := interceptor.WrapStreamingHandler(handler)

			conn := newFakeStreamConn("/Test/Stream", tc.header)
			err := wrapped(context.Background(), conn)

			var connErr *connect.Error
			if !errors.As(err, &connErr) {
				t.Fatalf("got err type %T (%v), want *connect.Error", err, err)
			}
			if connErr.Code() != connect.CodeUnauthenticated {
				t.Fatalf("got code %v, want CodeUnauthenticated", connErr.Code())
			}
			if invoked {
				t.Fatal("handler was invoked despite auth failure — security regression")
			}
		})
	}
}

// TestAuthInterceptor_Streaming_PopulatesClaimsViaCanonicalKey asserts that on
// a valid Bearer the streaming interceptor injects claims that are readable
// via BOTH platform/server.ClaimsFromContext (the local shim) AND
// platform/auth.ClaimsFromContext (the canonical helper). This is the
// streaming-side mirror of
// platform/auth/require_test.go:roundtrip_via_platform_server_re_export and
// guards the most likely regression — a future refactor that drifts the two
// context keys apart again.
func TestAuthInterceptor_Streaming_PopulatesClaimsViaCanonicalKey(t *testing.T) {
	mgr := newTestJWTManager(t)
	userID := uuid.NewString()
	companyID := uuid.NewString()
	token, err := mgr.CreateAccessToken(userID, companyID, "admin", 80, []string{companyID})
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}

	interceptor := NewAuthInterceptor(mgr, nil)

	var (
		serverShimClaims *auth.Claims
		canonicalClaims  *auth.Claims
	)
	handler := connect.StreamingHandlerFunc(func(ctx context.Context, _ connect.StreamingHandlerConn) error {
		serverShimClaims = ClaimsFromContext(ctx)
		canonicalClaims = auth.ClaimsFromContext(ctx)
		return nil
	})
	wrapped := interceptor.WrapStreamingHandler(handler)

	conn := newFakeStreamConn("/Test/Stream", "Bearer "+token)
	if err := wrapped(context.Background(), conn); err != nil {
		t.Fatalf("wrapped streaming handler: unexpected err %v", err)
	}

	if serverShimClaims == nil {
		t.Fatal("server.ClaimsFromContext returned nil — streaming did not populate claims")
	}
	if canonicalClaims == nil {
		t.Fatal("auth.ClaimsFromContext returned nil — streaming bypassed canonical key (bridge contract regression)")
	}
	if serverShimClaims.UserID != userID || canonicalClaims.UserID != userID {
		t.Fatalf("UserID mismatch: shim=%q canonical=%q want %q",
			serverShimClaims.UserID, canonicalClaims.UserID, userID)
	}
	if canonicalClaims.CompanyID != companyID {
		t.Fatalf("canonical CompanyID = %q, want %q", canonicalClaims.CompanyID, companyID)
	}
}

// denyRBACStore yields a real Role but no permissions, so
// rbac.Enforcer.HasPermission returns (false, nil) for any check —
// exercising the RBAC denial path without an error code.
type denyRBACStore struct{}

func (denyRBACStore) GetRoleByID(_ context.Context, id uuid.UUID) (rbac.Role, error) {
	return rbac.Role{ID: id}, nil
}
func (denyRBACStore) ListRolesByCompany(context.Context, uuid.UUID) ([]rbac.Role, error) {
	return nil, nil
}
func (denyRBACStore) CreateRole(context.Context, uuid.UUID, string, string, rbac.RoleLevel) (rbac.Role, error) {
	return rbac.Role{}, errors.New("not implemented")
}
func (denyRBACStore) ListPermissionsByRole(context.Context, uuid.UUID) ([]rbac.Permission, error) {
	return nil, nil
}
func (denyRBACStore) ListAllPermissions(context.Context) ([]rbac.Permission, error) {
	return nil, nil
}
func (denyRBACStore) AddRolePermission(context.Context, uuid.UUID, uuid.UUID) error {
	return errors.New("not implemented")
}
func (denyRBACStore) GetUserRole(context.Context, uuid.UUID, uuid.UUID) (rbac.Role, error) {
	return rbac.Role{ID: uuid.New(), Level: 10, Name: "denied"}, nil
}
func (denyRBACStore) GetMembership(context.Context, uuid.UUID, uuid.UUID) (rbac.Membership, error) {
	return rbac.Membership{}, errors.New("no membership")
}
func (denyRBACStore) GetUserPermissions(context.Context, uuid.UUID, uuid.UUID) ([]rbac.Permission, error) {
	return nil, nil
}
func (denyRBACStore) AssignRoleToUser(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error {
	return errors.New("not implemented")
}
func (denyRBACStore) CreateMembership(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error {
	return errors.New("not implemented")
}
func (denyRBACStore) GetCompanyAncestors(context.Context, uuid.UUID) ([]rbac.CompanyAncestor, error) {
	return nil, nil
}
func (denyRBACStore) GetMembershipInCompany(context.Context, uuid.UUID, uuid.UUID) (*rbac.Membership, error) {
	return nil, errors.New("no membership")
}

// TestRBACInterceptor_Streaming_DeniesWithoutPermission asserts that a
// streaming procedure with a declared permission requirement is rejected with
// CodePermissionDenied when the caller's RBAC role grants no matching
// permissions, and that the underlying handler is never invoked.
func TestRBACInterceptor_Streaming_DeniesWithoutPermission(t *testing.T) {
	procedure := "/wizards.v1.WizardService/GenerateStep"
	cfg := InterceptorConfig{
		ProcedurePermissions: map[string]Permission{
			procedure: {Feature: "wizards", Action: "generate"},
		},
	}
	enforcer := rbac.NewEnforcer(denyRBACStore{}, nil)
	interceptor := NewRBACInterceptor(enforcer, cfg)

	invoked := false
	handler := connect.StreamingHandlerFunc(func(ctx context.Context, _ connect.StreamingHandlerConn) error {
		invoked = true
		return nil
	})
	wrapped := interceptor.WrapStreamingHandler(handler)

	claims := &auth.Claims{
		UserID:    uuid.NewString(),
		CompanyID: uuid.NewString(),
		Role:      "viewer",
		RoleLevel: 10,
	}
	ctx := WithClaims(context.Background(), claims)
	conn := newFakeStreamConn(procedure, "")

	err := wrapped(ctx, conn)
	var connErr *connect.Error
	if !errors.As(err, &connErr) {
		t.Fatalf("got err type %T (%v), want *connect.Error", err, err)
	}
	if connErr.Code() != connect.CodePermissionDenied {
		t.Fatalf("got code %v, want CodePermissionDenied", connErr.Code())
	}
	if invoked {
		t.Fatal("streaming handler was invoked despite RBAC deny — security regression")
	}
}
