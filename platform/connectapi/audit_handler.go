package connectapi

import (
	"context"

	connect "connectrpc.com/connect"
	platformv1 "github.com/aocybersystems/eden-platform-go/gen/go/platform/v1"
	platformv1connect "github.com/aocybersystems/eden-platform-go/gen/go/platform/v1/platformv1connect"
	"github.com/google/uuid"
)

var _ platformv1connect.AuditServiceHandler = (*AuditHandler)(nil)

// AuditLogQuerier provides query access to audit logs.
type AuditLogQuerier interface {
	QueryAuditLogs(ctx context.Context, companyID uuid.UUID, limit, offset int, actorID *uuid.UUID, action, resource *string) ([]*platformv1.AuditLogEntry, int, error)
}

// AuditHandler implements the AuditService Connect handler.
type AuditHandler struct {
	querier AuditLogQuerier
}

// NewAuditHandler creates a new audit handler.
func NewAuditHandler(querier AuditLogQuerier) *AuditHandler {
	return &AuditHandler{querier: querier}
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
