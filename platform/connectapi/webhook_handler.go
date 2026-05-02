package connectapi

import (
	"context"
	"time"

	connect "connectrpc.com/connect"
	platformv1 "github.com/aocybersystems/eden-platform-go/gen/go/platform/v1"
	platformv1connect "github.com/aocybersystems/eden-platform-go/gen/go/platform/v1/platformv1connect"
	"github.com/aocybersystems/eden-platform-go/platform/webhook"
	"github.com/google/uuid"
)

var _ platformv1connect.WebhookServiceHandler = (*WebhookHandler)(nil)

// deliveryQuerier provides paginated delivery listing beyond the base WebhookStore interface.
type deliveryQuerier interface {
	ListDeliveriesByWebhook(ctx context.Context, webhookID uuid.UUID, limit, offset int) ([]webhook.WebhookDelivery, error)
}

// WebhookHandler implements the WebhookService Connect handler.
type WebhookHandler struct {
	service          *webhook.Service
	store            webhook.WebhookStore
	deliveryQuerier  deliveryQuerier
}

// NewWebhookHandler creates a new webhook handler.
// The querier parameter should be the concrete devstore.WebhookStore which implements deliveryQuerier.
func NewWebhookHandler(service *webhook.Service, store webhook.WebhookStore, querier deliveryQuerier) *WebhookHandler {
	return &WebhookHandler{service: service, store: store, deliveryQuerier: querier}
}

func (h *WebhookHandler) RegisterWebhook(ctx context.Context, req *connect.Request[platformv1.RegisterWebhookRequest]) (*connect.Response[platformv1.RegisterWebhookResponse], error) {
	companyID, err := uuid.Parse(req.Msg.GetCompanyId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	wh, err := h.service.Register(ctx, companyID, req.Msg.GetUrl(), "", req.Msg.GetEvents())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	return connect.NewResponse(&platformv1.RegisterWebhookResponse{Webhook: webhookToProto(wh)}), nil
}

func (h *WebhookHandler) ListWebhooks(ctx context.Context, req *connect.Request[platformv1.ListWebhooksRequest]) (*connect.Response[platformv1.ListWebhooksResponse], error) {
	companyID, err := uuid.Parse(req.Msg.GetCompanyId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	webhooks, err := h.store.ListWebhooksByCompany(ctx, companyID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	protoWebhooks := make([]*platformv1.WebhookData, 0, len(webhooks))
	for _, wh := range webhooks {
		protoWebhooks = append(protoWebhooks, webhookToProto(wh))
	}

	return connect.NewResponse(&platformv1.ListWebhooksResponse{Webhooks: protoWebhooks}), nil
}

func (h *WebhookHandler) DeleteWebhook(ctx context.Context, req *connect.Request[platformv1.DeleteWebhookRequest]) (*connect.Response[platformv1.DeleteWebhookResponse], error) {
	id, err := uuid.Parse(req.Msg.GetId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	if err := h.store.DeleteWebhook(ctx, id); err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}

	return connect.NewResponse(&platformv1.DeleteWebhookResponse{}), nil
}

func (h *WebhookHandler) ListDeliveries(ctx context.Context, req *connect.Request[platformv1.ListDeliveriesRequest]) (*connect.Response[platformv1.ListDeliveriesResponse], error) {
	webhookID, err := uuid.Parse(req.Msg.GetWebhookId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	limit := int(req.Msg.GetLimit())
	if limit <= 0 {
		limit = 50
	}
	offset := int(req.Msg.GetOffset())
	if offset < 0 {
		offset = 0
	}

	deliveries, err := h.deliveryQuerier.ListDeliveriesByWebhook(ctx, webhookID, limit, offset)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	protoDeliveries := make([]*platformv1.DeliveryResponse, 0, len(deliveries))
	for _, d := range deliveries {
		protoDeliveries = append(protoDeliveries, &platformv1.DeliveryResponse{
			Id:         d.ID.String(),
			WebhookId:  d.WebhookID.String(),
			EventType:  d.EventType,
			Status:     d.Status,
			StatusCode: int32(d.StatusCode),
			Attempts:   int32(d.Attempts),
			CreatedAt:  d.CreatedAt.Format(time.RFC3339),
		})
	}

	return connect.NewResponse(&platformv1.ListDeliveriesResponse{Deliveries: protoDeliveries}), nil
}

func webhookToProto(wh webhook.Webhook) *platformv1.WebhookData {
	return &platformv1.WebhookData{
		Id:        wh.ID.String(),
		CompanyId: wh.CompanyID.String(),
		Url:       wh.URL,
		Secret:    wh.Secret,
		Events:    wh.Events,
		Active:    wh.Active,
		CreatedAt: wh.CreatedAt.Format(time.RFC3339),
	}
}
