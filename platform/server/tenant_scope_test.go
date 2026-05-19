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

// --------------------------------------------------------------------------
// Dedicated-mode tests (TRD 10-04 — AOID physical-isolation tier).
//
// Dedicated mode is configured via the WithDedicatedTenant functional
// option. In this mode, EVERY request — including super_admin — must
// target the dedicated tenant or have no target (chassis-wide endpoint).
// Cross-tenant requests are CodePermissionDenied with an error message
// containing the substring "dedicated-mode" for operator log-grep.
// --------------------------------------------------------------------------

// runDedicatedInterceptor is the dedicated-mode equivalent of runInterceptor:
// it constructs the interceptor with WithDedicatedTenant(dedicated) and runs
// a single round-trip, reporting whether next() was invoked.
func runDedicatedInterceptor(
	t *testing.T,
	ctx context.Context,
	extractor server.TenantExtractor,
	dedicated uuid.UUID,
) (called bool, err error) {
	t.Helper()
	icpt := server.NewTenantScopeInterceptor(extractor, server.WithDedicatedTenant(dedicated))
	handler := icpt.WrapUnary(func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		called = true
		return nil, nil
	})
	_, err = handler(ctx, fakeAnyRequest{})
	return called, err
}

// SuperAdmin requesting a tenant other than the dedicated one MUST be
// denied — dedicated mode tightens the super_admin bypass.
func TestTenantScope_DedicatedMode_SuperAdmin_CrossTenantDenied(t *testing.T) {
	dedicated := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	other := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	ctx := ctxWithIdentity(adminauth.AdminIdentity{
		SubjectCN: "aoid-superadmin", IsSuperAdmin: true,
	})
	called, err := runDedicatedInterceptor(t, ctx,
		staticExtractor(other, nil), dedicated)
	require.False(t, called)
	var ce *connect.Error
	require.True(t, errors.As(err, &ce))
	require.Equal(t, connect.CodePermissionDenied, ce.Code())
	require.True(t, strings.Contains(err.Error(), "dedicated-mode"),
		"error message must contain 'dedicated-mode' for operator log-grep; got %q", err.Error())
}

// SuperAdmin targeting the dedicated tenant is permitted in dedicated mode.
func TestTenantScope_DedicatedMode_SuperAdmin_DedicatedTenantAllowed(t *testing.T) {
	dedicated := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	ctx := ctxWithIdentity(adminauth.AdminIdentity{
		SubjectCN: "aoid-superadmin", IsSuperAdmin: true,
	})
	called, err := runDedicatedInterceptor(t, ctx,
		staticExtractor(dedicated, nil), dedicated)
	require.NoError(t, err)
	require.True(t, called)
}

// Chassis-wide endpoints (target tenant == uuid.Nil) MUST pass in dedicated
// mode — the route/handler is responsible for tenant-resolution on chassis
// endpoints like /.well-known/jwks.json. The interceptor is purely a
// cross-tenant guard, not a chassis-route guard.
func TestTenantScope_DedicatedMode_NoTargetTenant_AllowedForChassis(t *testing.T) {
	dedicated := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	ctx := ctxWithIdentity(adminauth.AdminIdentity{
		SubjectCN: "aoid-superadmin", IsSuperAdmin: true,
	})
	called, err := runDedicatedInterceptor(t, ctx,
		staticExtractor(uuid.Nil, nil), dedicated)
	require.NoError(t, err)
	require.True(t, called)
}

// Tenant admin (non-super_admin) targeting the dedicated tenant is allowed
// — same behavior as non-dedicated mode for an admin operating in their
// own tenant.
func TestTenantScope_DedicatedMode_TenantAdmin_DedicatedTenantAllowed(t *testing.T) {
	dedicated := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	ctx := ctxWithIdentity(adminauth.AdminIdentity{
		SubjectCN: "aoid-admin-acme", TenantID: dedicated, Roles: []string{"tenant_admin"},
	})
	called, err := runDedicatedInterceptor(t, ctx,
		staticExtractor(dedicated, nil), dedicated)
	require.NoError(t, err)
	require.True(t, called)
}

// Tenant admin targeting a foreign tenant is denied — same as above, the
// dedicated-mode predicate fires before the per-admin tenant check.
func TestTenantScope_DedicatedMode_TenantAdmin_ForeignTenantDenied(t *testing.T) {
	dedicated := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	other := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	ctx := ctxWithIdentity(adminauth.AdminIdentity{
		SubjectCN: "aoid-admin-acme", TenantID: other, Roles: []string{"tenant_admin"},
	})
	called, err := runDedicatedInterceptor(t, ctx,
		staticExtractor(other, nil), dedicated)
	require.False(t, called)
	var ce *connect.Error
	require.True(t, errors.As(err, &ce))
	require.Equal(t, connect.CodePermissionDenied, ce.Code())
	require.True(t, strings.Contains(err.Error(), "dedicated-mode"),
		"error message must contain 'dedicated-mode'; got %q", err.Error())
}

// Backward-compatibility: passing zero opts keeps existing behavior — a
// super_admin can hit any tenant (no dedicated-mode predicate).
func TestTenantScope_NoOpts_BackwardCompatible(t *testing.T) {
	other := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	ctx := ctxWithIdentity(adminauth.AdminIdentity{
		SubjectCN: "aoid-superadmin", IsSuperAdmin: true,
	})
	// No WithDedicatedTenant — super_admin should pass to any target.
	icpt := server.NewTenantScopeInterceptor(staticExtractor(other, nil))
	var called bool
	handler := icpt.WrapUnary(func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		called = true
		return nil, nil
	})
	_, err := handler(ctx, fakeAnyRequest{})
	require.NoError(t, err)
	require.True(t, called)
}
