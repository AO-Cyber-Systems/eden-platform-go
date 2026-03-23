package connectapi

import (
	"context"
	"fmt"

	connect "connectrpc.com/connect"
	platformv1 "github.com/aocybersystems/eden-platform-go/gen/go/platform/v1"
	"github.com/aocybersystems/eden-platform-go/platform/company"
	"github.com/aocybersystems/eden-platform-go/platform/server"
	"github.com/google/uuid"
)

type userCompanyLister interface {
	ListCompaniesForUser(context.Context, uuid.UUID) ([]company.Company, error)
}

type CompanyHandler struct {
	service *company.Service
	lister  userCompanyLister
}

func NewCompanyHandler(service *company.Service, lister userCompanyLister) *CompanyHandler {
	return &CompanyHandler{service: service, lister: lister}
}

func (h *CompanyHandler) CreateCompany(ctx context.Context, req *connect.Request[platformv1.CreateCompanyRequest]) (*connect.Response[platformv1.CompanyResponse], error) {
	var parentID *uuid.UUID
	if req.Msg.ParentCompanyId != nil && req.Msg.GetParentCompanyId() != "" {
		parsed, err := uuid.Parse(req.Msg.GetParentCompanyId())
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		parentID = &parsed
	}
	created, err := h.service.CreateCompany(
		ctx,
		req.Msg.GetName(),
		req.Msg.GetSlug(),
		company.CompanyType(req.Msg.GetCompanyType()),
		parentID,
		parseSettingsJSON(req.Msg.GetSettingsJson()),
	)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(companyResponseFromDomain(created)), nil
}

func (h *CompanyHandler) GetCompany(ctx context.Context, req *connect.Request[platformv1.GetCompanyRequest]) (*connect.Response[platformv1.CompanyResponse], error) {
	id, err := uuid.Parse(req.Msg.GetId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	companyRecord, err := h.service.GetCompany(ctx, id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	return connect.NewResponse(companyResponseFromDomain(companyRecord)), nil
}

func (h *CompanyHandler) UpdateCompany(ctx context.Context, req *connect.Request[platformv1.UpdateCompanyRequest]) (*connect.Response[platformv1.CompanyResponse], error) {
	id, err := uuid.Parse(req.Msg.GetId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	existing, err := h.service.GetCompany(ctx, id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}

	updated := existing
	updated.Name = req.Msg.GetName()
	updated.Slug = req.Msg.GetSlug()
	updated.CompanyType = company.CompanyType(req.Msg.GetCompanyType())
	if req.Msg.InheritedRoleCap != nil {
		value := int(req.Msg.GetInheritedRoleCap())
		updated.InheritedRoleCap = &value
	}
	if req.Msg.InheritedAccessLevel != nil {
		value := req.Msg.GetInheritedAccessLevel()
		updated.InheritedAccessLvl = &value
	}
	if req.Msg.SettingsJson != nil {
		updated.Settings = parseSettingsJSON(req.Msg.GetSettingsJson())
	}

	result, err := h.service.UpdateCompany(ctx, updated)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(companyResponseFromDomain(result)), nil
}

func (h *CompanyHandler) ListCompanies(ctx context.Context, req *connect.Request[platformv1.ListCompaniesRequest]) (*connect.Response[platformv1.ListCompaniesResponse], error) {
	userID, _, _ := server.ExtractClaims(ctx)
	if userID != "" && h.lister != nil {
		parsedUserID, err := uuid.Parse(userID)
		if err != nil {
			return nil, connect.NewError(connect.CodeUnauthenticated, err)
		}
		companies, err := h.lister.ListCompaniesForUser(ctx, parsedUserID)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		return connect.NewResponse(listCompaniesResponse(companies)), nil
	}

	return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("company listing is not configured"))
}

func (h *CompanyHandler) GetAncestors(ctx context.Context, req *connect.Request[platformv1.GetAncestorsRequest]) (*connect.Response[platformv1.ListCompaniesResponse], error) {
	id, err := uuid.Parse(req.Msg.GetCompanyId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	companies, err := h.service.GetAncestors(ctx, id)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(listCompaniesResponse(companies)), nil
}

func (h *CompanyHandler) GetDescendants(ctx context.Context, req *connect.Request[platformv1.GetDescendantsRequest]) (*connect.Response[platformv1.ListCompaniesResponse], error) {
	id, err := uuid.Parse(req.Msg.GetCompanyId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	descendantIDs, err := h.service.GetSelfAndDescendantIDs(ctx, id)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	companies := make([]company.Company, 0, len(descendantIDs))
	for _, descendantID := range descendantIDs {
		companyRecord, err := h.service.GetCompany(ctx, descendantID)
		if err == nil {
			companies = append(companies, companyRecord)
		}
	}
	return connect.NewResponse(listCompaniesResponse(companies)), nil
}

func (h *CompanyHandler) GetEffectiveSettings(ctx context.Context, req *connect.Request[platformv1.GetEffectiveSettingsRequest]) (*connect.Response[platformv1.GetEffectiveSettingsResponse], error) {
	id, err := uuid.Parse(req.Msg.GetCompanyId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	settings, err := h.service.GetEffectiveSettings(ctx, id)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&platformv1.GetEffectiveSettingsResponse{SettingsJson: string(settings)}), nil
}
