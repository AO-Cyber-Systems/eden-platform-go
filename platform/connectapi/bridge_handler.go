package connectapi

import (
	"context"
	"errors"

	connect "connectrpc.com/connect"
	platformv1 "github.com/aocybersystems/eden-platform-go/gen/go/platform/v1"
	platformv1connect "github.com/aocybersystems/eden-platform-go/gen/go/platform/v1/platformv1connect"
	"github.com/aocybersystems/eden-platform-go/platform/bridge"
)

var _ platformv1connect.BridgeServiceHandler = (*BridgeHandler)(nil)

// BridgeHandler implements the BridgeService Connect handler.
type BridgeHandler struct {
	registry *bridge.AdapterRegistry
}

// NewBridgeHandler creates a new bridge handler.
func NewBridgeHandler(registry *bridge.AdapterRegistry) *BridgeHandler {
	return &BridgeHandler{registry: registry}
}

func (h *BridgeHandler) ListAdapters(_ context.Context, _ *connect.Request[platformv1.ListAdaptersRequest]) (*connect.Response[platformv1.ListAdaptersResponse], error) {
	adapters := h.registry.ListAll()

	protoAdapters := make([]*platformv1.AdapterInfo, 0, len(adapters))
	for prefix, adapter := range adapters {
		actions := adapter.ActionTypes()
		protoActions := make([]*platformv1.ActionSchema, 0, len(actions))
		for _, a := range actions {
			protoActions = append(protoActions, actionSchemaToProto(a))
		}

		protoAdapters = append(protoAdapters, &platformv1.AdapterInfo{
			Prefix:     prefix,
			EventTypes: adapter.EventTypes(),
			Actions:    protoActions,
		})
	}

	return connect.NewResponse(&platformv1.ListAdaptersResponse{Adapters: protoAdapters}), nil
}

func (h *BridgeHandler) DispatchAction(_ context.Context, req *connect.Request[platformv1.DispatchActionRequest]) (*connect.Response[platformv1.DispatchActionResponse], error) {
	actionType := req.Msg.GetActionType()

	_, found := h.registry.FindAdapterForAction(actionType)
	if !found {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("no adapter supports action: "+actionType))
	}

	// Note: actual dispatch to external systems is handled by bridge.BridgeService via NATS.
	// The handler provides a synchronous API for the UI; in dev mode we return simulated success.
	return connect.NewResponse(&platformv1.DispatchActionResponse{
		Success: true,
		Message: "Action dispatched",
	}), nil
}

func (h *BridgeHandler) ListActions(_ context.Context, req *connect.Request[platformv1.ListActionsRequest]) (*connect.Response[platformv1.ListActionsResponse], error) {
	eventType := req.Msg.GetEventType()
	adapters := h.registry.ListAll()

	var protoActions []*platformv1.ActionSchema
	for _, adapter := range adapters {
		for _, et := range adapter.EventTypes() {
			if et == eventType {
				for _, a := range adapter.ActionTypes() {
					protoActions = append(protoActions, actionSchemaToProto(a))
				}
				break
			}
		}
	}

	return connect.NewResponse(&platformv1.ListActionsResponse{Actions: protoActions}), nil
}

func actionSchemaToProto(a bridge.ActionSchema) *platformv1.ActionSchema {
	return &platformv1.ActionSchema{
		Type:          a.Type,
		Label:         a.Label,
		RequiresInput: a.RequiresInput,
		InputHint:     a.InputHint,
		Destructive:   a.Destructive,
	}
}
