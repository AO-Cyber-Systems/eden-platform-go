package entitlements

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
)

type contextKey string

const bootstrapContextKey contextKey = "eden.entitlements.bootstrap"

// RequireEntitlement returns middleware that checks whether the request's company
// is entitled to the given feature. Returns 403 if the feature is not allowed.
//
// companyIDFromCtx extracts the company ID from the request context. Each service
// provides its own implementation (e.g., from JWT claims or session data).
func RequireEntitlement(client *EntitlementClient, featureKey string, companyIDFromCtx func(context.Context) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			companyID := companyIDFromCtx(r.Context())
			if companyID == "" {
				writeErr(w, http.StatusUnauthorized, "missing company context")
				return
			}

			allowed, err := client.CanUseFeature(r.Context(), companyID, featureKey)
			if err != nil {
				slog.Warn("[Entitlements] check failed, denying by default",
					"feature", featureKey,
					"company_id", companyID,
					"error", err,
				)
				writeErr(w, http.StatusForbidden, "feature not available")
				return
			}

			if !allowed {
				writeErr(w, http.StatusForbidden, "feature '"+featureKey+"' is not included in your plan")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireFeatureFlag returns middleware that checks whether a feature flag is enabled
// for the request's company. Returns 403 if the flag is off.
func RequireFeatureFlag(client *FeatureFlagClient, flagKey string, companyIDFromCtx func(context.Context) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			companyID := companyIDFromCtx(r.Context())
			if companyID == "" {
				writeErr(w, http.StatusUnauthorized, "missing company context")
				return
			}

			enabled, err := client.IsEnabled(r.Context(), companyID, flagKey)
			if err != nil {
				slog.Warn("[FeatureFlags] check failed, denying by default",
					"flag", flagKey,
					"company_id", companyID,
					"error", err,
				)
				writeErr(w, http.StatusForbidden, "feature not available")
				return
			}

			if !enabled {
				writeErr(w, http.StatusForbidden, "feature '"+flagKey+"' is not enabled")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// InjectEntitlements middleware pre-fetches the bootstrap data and stores it
// in the request context. Downstream handlers can read it with EntitlementsFromContext
// without making additional HTTP calls.
func InjectEntitlements(client *EntitlementClient, companyIDFromCtx func(context.Context) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			companyID := companyIDFromCtx(r.Context())
			if companyID == "" {
				next.ServeHTTP(w, r)
				return
			}

			bootstrap, err := client.Bootstrap(r.Context(), companyID)
			if err != nil {
				slog.Warn("[Entitlements] bootstrap prefetch failed", "company_id", companyID, "error", err)
				next.ServeHTTP(w, r)
				return
			}

			ctx := context.WithValue(r.Context(), bootstrapContextKey, bootstrap)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireEntitlementByIdentity is the by-AOID-identity twin of RequireEntitlement.
// It gates a feature by the request's AOID subject instead of company_id and is
// FAIL-CLOSED: an empty subject is 401, and any error or disallowed feature is 403.
//
// subjectFromCtx / emailFromCtx extract the AOID subject and (optional) email from
// the request context. The email is forwarded to the biz backend so it can self-heal
// a missing subject->company link on the first call, exactly as CanUseFeatureByIdentity.
func RequireEntitlementByIdentity(client *EntitlementClient, featureKey string, subjectFromCtx, emailFromCtx func(context.Context) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			subject := subjectFromCtx(r.Context())
			if subject == "" {
				writeErr(w, http.StatusUnauthorized, "missing identity context")
				return
			}

			email := emailFromCtx(r.Context())
			allowed, err := client.CanUseFeatureByIdentity(r.Context(), subject, email, featureKey)
			if err != nil {
				slog.Warn("[Entitlements] identity check failed, denying by default",
					"feature", featureKey,
					"subject", subject,
					"error", err,
				)
				writeErr(w, http.StatusForbidden, "feature not available")
				return
			}

			if !allowed {
				writeErr(w, http.StatusForbidden, "feature '"+featureKey+"' is not included in your plan")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// InjectEntitlementsByIdentity is the by-AOID-identity twin of InjectEntitlements.
// It pre-fetches the subject's bootstrap and stores it in the request context under
// the SAME bootstrapContextKey, so downstream handlers read it with
// EntitlementsFromContext unchanged. It is FAIL-OPEN: an empty subject or a fetch
// error passes the request through without injecting.
func InjectEntitlementsByIdentity(client *EntitlementClient, subjectFromCtx, emailFromCtx func(context.Context) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			subject := subjectFromCtx(r.Context())
			if subject == "" {
				next.ServeHTTP(w, r)
				return
			}

			email := emailFromCtx(r.Context())
			bootstrap, err := client.BootstrapByIdentity(r.Context(), subject, email)
			if err != nil {
				slog.Warn("[Entitlements] bootstrap-by-identity prefetch failed", "subject", subject, "error", err)
				next.ServeHTTP(w, r)
				return
			}

			ctx := context.WithValue(r.Context(), bootstrapContextKey, bootstrap)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// EntitlementsFromContext retrieves the pre-fetched BootstrapResponse from the
// request context. Returns nil if InjectEntitlements middleware was not used or
// the prefetch failed.
func EntitlementsFromContext(ctx context.Context) *BootstrapResponse {
	v, _ := ctx.Value(bootstrapContextKey).(*BootstrapResponse)
	return v
}

func writeErr(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
