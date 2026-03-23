package connectapi

import (
	"encoding/json"
	"time"

	platformv1 "github.com/aocybersystems/eden-platform-go/gen/go/platform/v1"
	"github.com/aocybersystems/eden-platform-go/platform/auth"
	"github.com/aocybersystems/eden-platform-go/platform/company"
)

func authResponseFromDomain(response *auth.AuthResponse) *platformv1.AuthResponse {
	return &platformv1.AuthResponse{
		AccessToken:  response.AccessToken,
		RefreshToken: response.RefreshToken,
		User: &platformv1.User{
			Id:          response.User.ID.String(),
			Email:       response.User.Email,
			DisplayName: response.User.DisplayName,
			IsActive:    response.User.IsActive,
			CreatedAt:   response.User.CreatedAt.Format(time.RFC3339),
		},
	}
}

func companyResponseFromDomain(c company.Company) *platformv1.CompanyResponse {
	response := &platformv1.CompanyResponse{
		Id:           c.ID.String(),
		Name:         c.Name,
		Slug:         c.Slug,
		CompanyType:  string(c.CompanyType),
		SettingsJson: string(c.Settings),
		IsActive:     c.IsActive,
		CreatedAt:    c.CreatedAt.Format(time.RFC3339),
		UpdatedAt:    c.UpdatedAt.Format(time.RFC3339),
	}
	if c.ParentCompanyID != nil {
		value := c.ParentCompanyID.String()
		response.ParentCompanyId = &value
	}
	if c.InheritedRoleCap != nil {
		value := int32(*c.InheritedRoleCap)
		response.InheritedRoleCap = &value
	}
	if c.InheritedAccessLvl != nil {
		value := *c.InheritedAccessLvl
		response.InheritedAccessLevel = &value
	}
	return response
}

func listCompaniesResponse(companies []company.Company) *platformv1.ListCompaniesResponse {
	items := make([]*platformv1.CompanyResponse, 0, len(companies))
	for _, companyRecord := range companies {
		items = append(items, companyResponseFromDomain(companyRecord))
	}
	return &platformv1.ListCompaniesResponse{Companies: items}
}

func parseSettingsJSON(raw string) json.RawMessage {
	if raw == "" {
		return nil
	}
	return json.RawMessage(raw)
}
