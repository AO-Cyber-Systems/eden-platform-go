package connectapi

import (
	"context"
	"errors"
	"strings"

	connect "connectrpc.com/connect"
	platformv1 "github.com/aocybersystems/eden-platform-go/gen/go/platform/v1"
	"github.com/aocybersystems/eden-platform-go/platform/auth"
	"github.com/aocybersystems/eden-platform-go/platform/server"
	"github.com/google/uuid"
)

type AuthHandler struct {
	service    *auth.Service
	ssoService *auth.SSOService
}

func NewAuthHandler(service *auth.Service, ssoService *auth.SSOService) *AuthHandler {
	return &AuthHandler{service: service, ssoService: ssoService}
}

func (h *AuthHandler) SignUp(ctx context.Context, req *connect.Request[platformv1.SignUpRequest]) (*connect.Response[platformv1.SignUpResponse], error) {
	response, err := h.service.SignUp(ctx, req.Msg.GetEmail(), req.Msg.GetPassword(), req.Msg.GetDisplayName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(&platformv1.SignUpResponse{Auth: authDataFromDomain(response)}), nil
}

func (h *AuthHandler) Login(ctx context.Context, req *connect.Request[platformv1.LoginRequest]) (*connect.Response[platformv1.LoginResponse], error) {
	response, err := h.service.Login(ctx, req.Msg.GetEmail(), req.Msg.GetPassword())
	if err != nil {
		code := connect.CodeInvalidArgument
		if errors.Is(err, context.Canceled) {
			code = connect.CodeCanceled
		}
		return nil, connect.NewError(code, err)
	}
	return connect.NewResponse(&platformv1.LoginResponse{Auth: authDataFromDomain(response)}), nil
}

func (h *AuthHandler) RefreshToken(ctx context.Context, req *connect.Request[platformv1.RefreshTokenRequest]) (*connect.Response[platformv1.RefreshTokenResponse], error) {
	response, err := h.service.RefreshToken(ctx, req.Msg.GetRefreshToken())
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}
	return connect.NewResponse(&platformv1.RefreshTokenResponse{Auth: authDataFromDomain(response)}), nil
}

func (h *AuthHandler) Logout(ctx context.Context, req *connect.Request[platformv1.LogoutRequest]) (*connect.Response[platformv1.LogoutResponse], error) {
	if err := h.service.Logout(ctx, req.Msg.GetRefreshToken()); err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}
	return connect.NewResponse(&platformv1.LogoutResponse{}), nil
}

func (h *AuthHandler) InitiateOIDC(ctx context.Context, req *connect.Request[platformv1.InitiateOIDCRequest]) (*connect.Response[platformv1.InitiateOIDCResponse], error) {
	companyID, err := uuid.Parse(req.Msg.GetCompanyId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid company_id"))
	}

	// TODO: add provider and redirect_uri fields to the proto message
	provider := "oidc"
	redirectURI := ""
	authURL, state, err := h.ssoService.InitiateOIDC(ctx, companyID, provider, redirectURI)
	if err != nil {
		if strings.Contains(err.Error(), "not configured") {
			return nil, connect.NewError(connect.CodeNotFound, err)
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&platformv1.InitiateOIDCResponse{
		AuthUrl: authURL,
		State:   state,
	}), nil
}

func (h *AuthHandler) InitiateSAML(ctx context.Context, req *connect.Request[platformv1.InitiateSAMLRequest]) (*connect.Response[platformv1.InitiateSAMLResponse], error) {
	companyID, err := uuid.Parse(req.Msg.GetCompanyId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid company_id"))
	}

	redirectURL, err := h.ssoService.InitiateSAML(ctx, companyID)
	if err != nil {
		if strings.Contains(err.Error(), "not configured") {
			return nil, connect.NewError(connect.CodeNotFound, err)
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&platformv1.InitiateSAMLResponse{
		RedirectUrl: redirectURL,
	}), nil
}

func (h *AuthHandler) UpdateProfile(ctx context.Context, req *connect.Request[platformv1.UpdateProfileRequest]) (*connect.Response[platformv1.UpdateProfileResponse], error) {
	userIDStr, _, _ := server.ExtractClaims(ctx)
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("invalid user identity"))
	}

	user, err := h.service.UpdateProfile(ctx, userID, req.Msg.GetDisplayName(), req.Msg.GetAvatarUrl())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	return connect.NewResponse(&platformv1.UpdateProfileResponse{
		User: userToProto(user),
	}), nil
}
