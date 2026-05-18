package server_test

// Test list (outside-in: assert observable Connect-RPC behavior):
//
// - TestNewTenantScopeInterceptor_NilExtractorPanics
// - TestTenantScopeInterceptor_MissingAdminIdentityReturnsUnauthenticated
// - TestTenantScopeInterceptor_SuperAdminPassesForAnyTenant
// - TestTenantScopeInterceptor_SuperAdminPassesForUUIDNilTarget
// - TestTenantScopeInterceptor_TenantAdminPassesForOwnTenant
// - TestTenantScopeInterceptor_TenantAdminRejectsCrossTenantAccess  <-- wrong-tenant assertion
// - TestTenantScopeInterceptor_ErrorMessageForCrossTenantContainsKeyword
// - TestTenantScopeInterceptor_ExtractorReturnsUUIDNilReturnsInvalidArgument
// - TestTenantScopeInterceptor_ExtractorReturnsErrorReturnsInvalidArgument
//
// All fixtures are inline struct literals or closure factories — no fixture
// builders, no LLM-generated test data, no property-based libs (per resolver
// constraints `fixture_strategy=inline` and `no_llm_test_data=true`).

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/aocybersystems/eden-platform-go/platform/adminauth"
	"github.com/aocybersystems/eden-platform-go/platform/server"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// fakeAnyRequest is a minimal connect.AnyRequest used in unit tests.
// The extractor never inspects it — we only need a non-nil value to thread
// through the interceptor's WrapUnary signature.
type fakeAnyRequest struct{ connect.AnyRequest }

// staticExtractor returns the configured uuid + err for every call.
func staticExtractor(id uuid.UUID, err error) server.TenantExtractor {
	return func(req connect.AnyRequest) (uuid.UUID, error) {
		return id, err
	}
}

// runInterceptor invokes the interceptor end-to-end and reports any error.
// On success, the recorded boolean reports whether next() was actually called.
func runInterceptor(
	t *testing.T,
	ctx context.Context,
	extractor server.TenantExtractor,
) (called bool, err error) {
	t.Helper()
	icpt := server.NewTenantScopeInterceptor(extractor)
	handler := icpt.WrapUnary(func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		called = true
		return nil, nil
	})
	_, err = handler(ctx, fakeAnyRequest{})
	return called, err
}

func ctxWithIdentity(a adminauth.AdminIdentity) context.Context {
	return adminauth.WithAdminIdentity(context.Background(), a)
}

func TestNewTenantScopeInterceptor_NilExtractorPanics(t *testing.T) {
	require.Panics(t, func() { server.NewTenantScopeInterceptor(nil) })
}

func TestTenantScopeInterceptor_MissingAdminIdentityReturnsUnauthenticated(t *testing.T) {
	called, err := runInterceptor(t, context.Background(),
		staticExtractor(uuid.New(), nil))
	require.False(t, called)
	var ce *connect.Error
	require.True(t, errors.As(err, &ce))
	require.Equal(t, connect.CodeUnauthenticated, ce.Code())
}

func TestTenantScopeInterceptor_SuperAdminPassesForAnyTenant(t *testing.T) {
	ctx := ctxWithIdentity(adminauth.AdminIdentity{
		SubjectCN: "aoid-superadmin", IsSuperAdmin: true,
	})
	called, err := runInterceptor(t, ctx,
		staticExtractor(uuid.New(), nil))
	require.NoError(t, err)
	require.True(t, called)
}

func TestTenantScopeInterceptor_SuperAdminPassesForUUIDNilTarget(t *testing.T) {
	ctx := ctxWithIdentity(adminauth.AdminIdentity{
		SubjectCN: "aoid-superadmin", IsSuperAdmin: true,
	})
	called, err := runInterceptor(t, ctx,
		staticExtractor(uuid.Nil, nil))
	require.NoError(t, err)
	require.True(t, called)
}

func TestTenantScopeInterceptor_TenantAdminPassesForOwnTenant(t *testing.T) {
	tenant := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	ctx := ctxWithIdentity(adminauth.AdminIdentity{
		SubjectCN: "aoid-admin-acme", TenantID: tenant, Roles: []string{"tenant_admin"},
	})
	called, err := runInterceptor(t, ctx,
		staticExtractor(tenant, nil))
	require.NoError(t, err)
	require.True(t, called)
}

// Wrong-tenant assertion (resolver `security_isolation=multi_tenant_required`).
// This test name + the cross-tenant error message together satisfy the
// security-isolation contract for Objective 2.
func TestTenantScopeInterceptor_TenantAdminRejectsCrossTenantAccess(t *testing.T) {
	own := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	other := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	ctx := ctxWithIdentity(adminauth.AdminIdentity{
		SubjectCN: "aoid-admin-acme", TenantID: own, Roles: []string{"tenant_admin"},
	})
	called, err := runInterceptor(t, ctx,
		staticExtractor(other, nil))
	require.False(t, called)
	var ce *connect.Error
	require.True(t, errors.As(err, &ce))
	require.Equal(t, connect.CodePermissionDenied, ce.Code())
	require.True(t, errors.Is(err, server.ErrCrossTenantAccess))
}

func TestTenantScopeInterceptor_ErrorMessageForCrossTenantContainsKeyword(t *testing.T) {
	own := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	other := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	ctx := ctxWithIdentity(adminauth.AdminIdentity{
		SubjectCN: "aoid-admin-acme", TenantID: own, Roles: []string{"tenant_admin"},
	})
	_, err := runInterceptor(t, ctx,
		staticExtractor(other, nil))
	require.True(t, strings.Contains(err.Error(), "cross-tenant"),
		"wrong-tenant assertion: error message must contain 'cross-tenant' for log-grep workflows; got %q", err.Error())
}

func TestTenantScopeInterceptor_ExtractorReturnsUUIDNilReturnsInvalidArgument(t *testing.T) {
	own := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	ctx := ctxWithIdentity(adminauth.AdminIdentity{
		SubjectCN: "aoid-admin-acme", TenantID: own, Roles: []string{"tenant_admin"},
	})
	called, err := runInterceptor(t, ctx,
		staticExtractor(uuid.Nil, nil))
	require.False(t, called)
	var ce *connect.Error
	require.True(t, errors.As(err, &ce))
	require.Equal(t, connect.CodeInvalidArgument, ce.Code())
	require.True(t, errors.Is(err, server.ErrNoTargetTenant))
}

func TestTenantScopeInterceptor_ExtractorReturnsErrorReturnsInvalidArgument(t *testing.T) {
	own := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	ctx := ctxWithIdentity(adminauth.AdminIdentity{
		SubjectCN: "aoid-admin-acme", TenantID: own, Roles: []string{"tenant_admin"},
	})
	parseErr := fmt.Errorf("uuid: incorrect UUID length")
	called, err := runInterceptor(t, ctx,
		staticExtractor(uuid.Nil, parseErr))
	require.False(t, called)
	var ce *connect.Error
	require.True(t, errors.As(err, &ce))
	require.Equal(t, connect.CodeInvalidArgument, ce.Code())
	require.True(t, strings.Contains(err.Error(), "tenant_id"),
		"error message must contain 'tenant_id' for operator log-grep workflows; got %q", err.Error())
}
