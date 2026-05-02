package connectapi

import (
	"encoding/json"
	"time"

	platformv1 "github.com/aocybersystems/eden-platform-go/gen/go/platform/v1"
	"github.com/aocybersystems/eden-platform-go/platform/auth"
	"github.com/aocybersystems/eden-platform-go/platform/company"
)

func authDataFromDomain(response *auth.AuthResponse) *platformv1.AuthData {
	return &platformv1.AuthData{
		AccessToken:  response.AccessToken,
		RefreshToken: response.RefreshToken,
		User:         userToProto(response.User),
	}
}

func userToProto(u auth.User) *platformv1.User {
	return &platformv1.User{
		Id:          u.ID.String(),
		Email:       u.Email,
		DisplayName: u.DisplayName,
		AvatarUrl:   u.AvatarURL,
		IsActive:    u.IsActive,
		CreatedAt:   u.CreatedAt.Format(time.RFC3339),
	}
}

func companyDataFromDomain(c company.Company) *platformv1.CompanyData {
	response := &platformv1.CompanyData{
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

func companyDataList(companies []company.Company) []*platformv1.CompanyData {
	items := make([]*platformv1.CompanyData, 0, len(companies))
	for _, companyRecord := range companies {
		items = append(items, companyDataFromDomain(companyRecord))
	}
	return items
}

func parseSettingsJSON(raw string) json.RawMessage {
	if raw == "" {
		return nil
	}
	return json.RawMessage(raw)
}
