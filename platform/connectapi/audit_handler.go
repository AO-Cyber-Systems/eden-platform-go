package connectapi

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	connect "connectrpc.com/connect"
	platformv1 "github.com/aocybersystems/eden-platform-go/gen/go/platform/v1"
	platformv1connect "github.com/aocybersystems/eden-platform-go/gen/go/platform/v1/platformv1connect"
	"github.com/google/uuid"
)

var _ platformv1connect.AuditServiceHandler = (*AuditHandler)(nil)

// breakGlassMaxAge bounds how old a replayed break-glass event may be. Older
// values are rejected as suspicious (the replay file should be ingested
// promptly after an outage).
const breakGlassMaxAge = 30 * 24 * time.Hour

// breakGlassClockSkew tolerates minor clock drift when rejecting future
// original_issued_at values.
const breakGlassClockSkew = 5 * time.Minute

// Valid break-glass manifest scopes (see audit.proto IngestBreakGlassEventRequest).
const (
	breakGlassScopeAdmin   = "admin.recovery"
	breakGlassScopeService = "service.recovery"
)

// AuditLogQuerier provides query access to audit logs.
type AuditLogQuerier interface {
	QueryAuditLogs(ctx context.Context, companyID uuid.UUID, limit, offset int, actorID *uuid.UUID, action, resource *string) ([]*platformv1.AuditLogEntry, int, error)
}

// BreakGlassEvent is the validated, domain-shaped form of one replayed
// break-glass audit event. The handler builds this from the wire request
// after validation and hands it to a BreakGlassIngester for persistence.
type BreakGlassEvent struct {
	OriginalJTI       uuid.UUID
	OriginalIssuedAt  time.Time
	Subject           string
	Tenant            string
	Scope             string
	Reason            string
	Operator          string
	ManifestDigestB64 string
}

// BreakGlassIngester persists replayed break-glass events.
//
// Implementations MUST:
//   - be idempotent on OriginalJTI — a repeated event returns the original
//     event_id with created=false and does NOT write a duplicate row;
//   - set the audit row's emitted_at to ev.OriginalIssuedAt (NOT arrival time)
//     so the AOAudit timeline reconstructs the outage window faithfully.
type BreakGlassIngester interface {
	IngestBreakGlass(ctx context.Context, ev BreakGlassEvent) (eventID string, created bool, err error)
}

// AuditHandler implements the AuditService Connect handler.
type AuditHandler struct {
	querier  AuditLogQuerier
	ingester BreakGlassIngester
}

// AuditHandlerOption configures an AuditHandler.
type AuditHandlerOption func(*AuditHandler)

// WithBreakGlassIngester wires the persistence backend for replayed
// break-glass events. Without it, IngestBreakGlassEvent returns
// CodeUnimplemented.
func WithBreakGlassIngester(ing BreakGlassIngester) AuditHandlerOption {
	return func(h *AuditHandler) { h.ingester = ing }
}

// NewAuditHandler creates a new audit handler.
func NewAuditHandler(querier AuditLogQuerier, opts ...AuditHandlerOption) *AuditHandler {
	h := &AuditHandler{querier: querier}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

func (h *AuditHandler) ListAuditLogs(ctx context.Context, req *connect.Request[platformv1.ListAuditLogsRequest]) (*connect.Response[platformv1.ListAuditLogsResponse], error) {
	companyID, err := uuid.Parse(req.Msg.GetCompanyId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	limit := int(req.Msg.GetLimit())
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}

	offset := int(req.Msg.GetOffset())
	if offset < 0 {
		offset = 0
	}

	var actorID *uuid.UUID
	if req.Msg.ActorId != nil {
		parsed, err := uuid.Parse(req.Msg.GetActorId())
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		actorID = &parsed
	}

	var action *string
	if req.Msg.Action != nil {
		v := req.Msg.GetAction()
		action = &v
	}

	var resource *string
	if req.Msg.Resource != nil {
		v := req.Msg.GetResource()
		resource = &v
	}

	entries, total, err := h.querier.QueryAuditLogs(ctx, companyID, limit, offset, actorID, action, resource)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&platformv1.ListAuditLogsResponse{
		Entries: entries,
		Total:   int32(total),
	}), nil
}

// IngestBreakGlassEvent records one replayed break-glass audit event from the
// aoidemergency replay file. Fields are taken verbatim (the server does not
// recompute original_jti / original_issued_at) so the AOAudit timeline
// reconstructs the outage window faithfully. Ingestion is idempotent on
// original_jti: re-replaying a line returns the original event_id.
func (h *AuditHandler) IngestBreakGlassEvent(ctx context.Context, req *connect.Request[platformv1.IngestBreakGlassEventRequest]) (*connect.Response[platformv1.IngestBreakGlassEventResponse], error) {
	msg := req.Msg

	jti, err := uuid.Parse(msg.GetOriginalJti())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("original_jti must be a UUID: %w", err))
	}

	ts := msg.GetOriginalIssuedAt()
	if ts == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("original_issued_at is required"))
	}
	if err := ts.CheckValid(); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("original_issued_at invalid: %w", err))
	}
	issuedAt := ts.AsTime()
	now := time.Now()
	if issuedAt.After(now.Add(breakGlassClockSkew)) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("original_issued_at is in the future"))
	}
	if now.Sub(issuedAt) > breakGlassMaxAge {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("original_issued_at is older than %s (suspicious replay)", breakGlassMaxAge))
	}

	switch msg.GetScope() {
	case breakGlassScopeAdmin, breakGlassScopeService:
		// ok
	default:
		return nil, connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("scope must be %q or %q", breakGlassScopeAdmin, breakGlassScopeService))
	}

	if strings.TrimSpace(msg.GetSubject()) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("subject is required"))
	}
	if strings.TrimSpace(msg.GetTenant()) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("tenant is required"))
	}

	if h.ingester == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("break-glass ingest is not configured"))
	}

	eventID, _, err := h.ingester.IngestBreakGlass(ctx, BreakGlassEvent{
		OriginalJTI:       jti,
		OriginalIssuedAt:  issuedAt,
		Subject:           msg.GetSubject(),
		Tenant:            msg.GetTenant(),
		Scope:             msg.GetScope(),
		Reason:            msg.GetReason(),
		Operator:          msg.GetOperator(),
		ManifestDigestB64: msg.GetManifestDigestB64(),
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("ingest break-glass event: %w", err))
	}

	return connect.NewResponse(&platformv1.IngestBreakGlassEventResponse{
		Accepted: true,
		EventId:  eventID,
	}), nil
}
