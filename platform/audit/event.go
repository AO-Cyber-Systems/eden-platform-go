package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

// Action is a typed string for canonical audit action names. Naming convention:
// "domain.entity.verb" (e.g. "auth.user.login"). Consumers can use the typed
// constants below or pass any string that follows the convention.
type Action string

// String returns the underlying action name.
func (a Action) String() string { return string(a) }

// Canonical actions used across the portfolio. Add new constants here when a
// new domain joins; consumers should prefer constants over magic strings.
const (
	// Auth domain.
	ActionUserLogin    Action = "auth.user.login"
	ActionUserLogout   Action = "auth.user.logout"
	ActionUserSignup   Action = "auth.user.signup"
	ActionUserPwReset  Action = "auth.user.password_reset"
	ActionTokenRefresh Action = "auth.token.refresh"
	ActionAPIKeyCreate Action = "auth.apikey.create"
	ActionAPIKeyRevoke Action = "auth.apikey.revoke"
	ActionAPIKeyRotate Action = "auth.apikey.rotate"

	// Generic CRUD.
	ActionCreate Action = "generic.create"
	ActionUpdate Action = "generic.update"
	ActionDelete Action = "generic.delete"

	// Permissions / RBAC.
	ActionRoleGrant  Action = "rbac.role.grant"
	ActionRoleRevoke Action = "rbac.role.revoke"

	// -------------------------------------------------------------------
	// Identity lifecycle (AOID domain — added in TRD 02-03).
	//
	// Emitted by AOID's admin API for account, group, role, entitlement,
	// and tenant lifecycle events. AC-2 evidence — see doc.go for the
	// sub-control mapping table.
	//
	// Naming note: the role constants are Identity-prefixed to avoid Go
	// identifier collision with the pre-existing rbac.* ActionRoleRevoke.
	// String values follow the canonical "identity.<resource>.<verb>"
	// convention; downstream consumers (AOAudit, Obj 9) parse on strings.
	// -------------------------------------------------------------------

	// Account lifecycle.
	ActionAccountCreate  Action = "identity.account.create"
	ActionAccountUpdate  Action = "identity.account.update"
	ActionAccountSuspend Action = "identity.account.suspend"
	ActionAccountRecover Action = "identity.account.recover"
	ActionAccountDelete  Action = "identity.account.delete"
	ActionAccountExpire  Action = "identity.account.expire" // system-triggered (expiration sweep)

	// Groups + group membership.
	ActionGroupCreate       Action = "identity.group.create"
	ActionGroupDelete       Action = "identity.group.delete"
	ActionGroupMemberAdd    Action = "identity.group.member.add"
	ActionGroupMemberRemove Action = "identity.group.member.remove"

	// Roles + role bindings (Identity-prefixed to avoid RBAC collision).
	ActionIdentityRoleCreate Action = "identity.role.create"
	ActionIdentityRoleAssign Action = "identity.role.assign"
	ActionIdentityRoleRevoke Action = "identity.role.revoke"

	// Entitlement attributes.
	ActionEntitlementSet    Action = "identity.entitlement.set"
	ActionEntitlementDelete Action = "identity.entitlement.delete"

	// Tenants (super-admin only).
	ActionTenantCreate Action = "identity.tenant.create"
)

// Standard detail keys. Use these so cross-product queries on audit details
// work without per-product schema drift.
const (
	DetailBefore    = "before"
	DetailAfter     = "after"
	DetailReason    = "reason"
	DetailRequestID = "request_id"
	DetailUserAgent = "user_agent"
)

// WithDetail attaches a single key/value pair to the event's Details map,
// allocating the map on first use. Returns the event so calls chain.
func (e Event) WithDetail(key string, value any) Event {
	if e.Details == nil {
		e.Details = make(map[string]any, 4)
	}
	e.Details[key] = value
	return e
}

// WithBeforeAfter attaches before/after snapshots — the standard shape for
// update events. Either snapshot may be nil.
func (e Event) WithBeforeAfter(before, after any) Event {
	if e.Details == nil {
		e.Details = make(map[string]any, 4)
	}
	e.Details[DetailBefore] = before
	e.Details[DetailAfter] = after
	return e
}

// WithRequestID attaches a request ID to Details for correlation with logs.
// Empty IDs are ignored.
func (e Event) WithRequestID(id string) Event {
	if id == "" {
		return e
	}
	return e.WithDetail(DetailRequestID, id)
}

// WithReason attaches a human-readable reason — common on revoke/delete events.
// Empty reasons are ignored.
func (e Event) WithReason(reason string) Event {
	if reason == "" {
		return e
	}
	return e.WithDetail(DetailReason, reason)
}

// WithAction sets the action and returns the event for chaining.
func (e Event) WithAction(a Action) Event {
	e.Action = a.String()
	return e
}

// EventFromHTTP populates an Event with HTTP-derived metadata: client IP,
// user-agent (in Details), and X-Request-ID (in Details). Caller fills the
// remaining fields (CompanyID, ActorID, Action, Resource, ResourceID).
//
// IP precedence: X-Forwarded-For (first hop) → X-Real-IP → RemoteAddr (host
// portion). Returns an empty IP if none can be parsed. A nil request returns
// the zero Event.
func EventFromHTTP(r *http.Request) Event {
	if r == nil {
		return Event{}
	}
	e := Event{
		IPAddress: clientIP(r),
	}
	if ua := r.Header.Get("User-Agent"); ua != "" {
		e = e.WithDetail(DetailUserAgent, ua)
	}
	if rid := r.Header.Get("X-Request-ID"); rid != "" {
		e = e.WithRequestID(rid)
	}
	return e
}

// clientIP picks the most-likely client IP from forwarding headers, falling
// back to RemoteAddr's host portion.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// First hop is the originating client per X-Forwarded-For convention.
		if idx := strings.IndexByte(xff, ','); idx > 0 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}
	if xrip := r.Header.Get("X-Real-IP"); xrip != "" {
		return strings.TrimSpace(xrip)
	}
	if r.RemoteAddr == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// LogSync writes the event to the underlying store synchronously, bypassing
// the async batch buffer. Use when the caller cannot tolerate dropped events
// (e.g. security-relevant audits, tests). Returns an error on store failure
// or invalid IDs. A nil store returns an error so callers can detect
// misconfiguration without silent loss.
func (l *Logger) LogSync(ctx context.Context, e Event) error {
	if l.store == nil {
		return fmt.Errorf("audit: nil store")
	}
	companyID, err := uuid.Parse(e.CompanyID)
	if err != nil {
		return fmt.Errorf("audit: parse company id %q: %w", e.CompanyID, err)
	}
	actorID, err := uuid.Parse(e.ActorID)
	if err != nil {
		return fmt.Errorf("audit: parse actor id %q: %w", e.ActorID, err)
	}
	detailsJSON, err := json.Marshal(e.Details)
	if err != nil {
		return fmt.Errorf("audit: marshal details: %w", err)
	}
	if detailsJSON == nil {
		detailsJSON = []byte("{}")
	}
	return l.store.CreateAuditLog(ctx, companyID, actorID, e.Action, e.Resource, e.ResourceID, e.IPAddress, detailsJSON)
}
