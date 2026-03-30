package connectapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"

	connect "connectrpc.com/connect"
	platformv1 "github.com/aocybersystems/eden-platform-go/gen/go/platform/v1"
	"github.com/aocybersystems/eden-platform-go/platform/company"
	"github.com/aocybersystems/eden-platform-go/platform/registry"
	"github.com/aocybersystems/eden-platform-go/platform/server"
	"github.com/google/uuid"
)

type RegistryHandler struct {
	registry  *registry.Registry
	companies company.CompanyStore
}

func NewRegistryHandler(reg *registry.Registry, companies company.CompanyStore) *RegistryHandler {
	return &RegistryHandler{registry: reg, companies: companies}
}

func (h *RegistryHandler) GetNavItems(ctx context.Context, req *connect.Request[platformv1.GetNavItemsRequest]) (*connect.Response[platformv1.GetNavItemsResponse], error) {
	enabledFeatures, err := h.enabledFeatures(ctx, req.Msg.GetCompanyId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	items := h.registry.GetNavItems(enabledFeatures)
	sort.Slice(items, func(i, j int) bool { return items[i].Priority < items[j].Priority })

	response := &platformv1.GetNavItemsResponse{Items: make([]*platformv1.NavItem, 0, len(items))}
	for _, item := range items {
		response.Items = append(response.Items, &platformv1.NavItem{
			Id:       item.ID,
			Label:    item.Label,
			Icon:     item.Icon,
			Path:     item.Path,
			Feature:  item.Feature,
			Priority: int32(item.Priority),
			Section:  item.Section,
		})
	}
	return connect.NewResponse(response), nil
}

func (h *RegistryHandler) GetWidgets(ctx context.Context, req *connect.Request[platformv1.GetWidgetsRequest]) (*connect.Response[platformv1.GetWidgetsResponse], error) {
	enabledFeatures, err := h.enabledFeatures(ctx, req.Msg.GetCompanyId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	widgets := h.registry.GetWidgets(enabledFeatures)
	sort.Slice(widgets, func(i, j int) bool { return widgets[i].Priority < widgets[j].Priority })

	response := &platformv1.GetWidgetsResponse{Widgets: make([]*platformv1.Widget, 0, len(widgets))}
	for _, widget := range widgets {
		response.Widgets = append(response.Widgets, &platformv1.Widget{
			Id:       widget.ID,
			Label:    widget.Label,
			Type:     widget.Type,
			Feature:  widget.Feature,
			Priority: int32(widget.Priority),
		})
	}
	return connect.NewResponse(response), nil
}

func (h *RegistryHandler) GetSearchScopes(ctx context.Context, req *connect.Request[platformv1.GetSearchScopesRequest]) (*connect.Response[platformv1.GetSearchScopesResponse], error) {
	enabledFeatures, err := h.enabledFeatures(ctx, req.Msg.GetCompanyId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	scopes := h.registry.GetSearchScopes(enabledFeatures)

	response := &platformv1.GetSearchScopesResponse{Scopes: make([]*platformv1.SearchScope, 0, len(scopes))}
	for _, scope := range scopes {
		response.Scopes = append(response.Scopes, &platformv1.SearchScope{
			Id:      scope.ID,
			Label:   scope.Label,
			Feature: scope.Feature,
		})
	}
	return connect.NewResponse(response), nil
}

func (h *RegistryHandler) GetBadgeCounts(ctx context.Context, req *connect.Request[platformv1.GetBadgeCountsRequest]) (*connect.Response[platformv1.GetBadgeCountsResponse], error) {
	userID, _, _ := server.ExtractClaims(ctx)
	counts := h.registry.GetBadgeCounts(req.Msg.GetCompanyId(), userID)
	response := &platformv1.GetBadgeCountsResponse{Counts: map[string]int32{}}
	for key, value := range counts {
		response.Counts[key] = int32(value)
	}
	return connect.NewResponse(response), nil
}

func (h *RegistryHandler) enabledFeatures(ctx context.Context, companyID string) (map[string]bool, error) {
	id, err := uuid.Parse(companyID)
	if err != nil {
		return nil, err
	}
	companyRecord, err := h.companies.GetCompany(ctx, id)
	if err != nil {
		return nil, err
	}

	var settings map[string]any
	if err := json.Unmarshal(companyRecord.Settings, &settings); err != nil {
		return map[string]bool{}, nil
	}
	rawFeatures, ok := settings["enabled_features"].([]any)
	if !ok {
		slog.Warn("enabled_features is not an array, using defaults",
			"company_id", companyID,
			"actual_type", fmt.Sprintf("%T", settings["enabled_features"]))
		return map[string]bool{
			"home": true, "projects": true, "activity": true, "settings": true,
		}, nil
	}
	enabled := map[string]bool{}
	for _, feature := range rawFeatures {
		if value, ok := feature.(string); ok {
			enabled[value] = true
		}
	}
	if len(enabled) == 0 {
		return map[string]bool{
			"home":     true,
			"projects": true,
			"activity": true,
			"settings": true,
		}, nil
	}
	return enabled, nil
}
