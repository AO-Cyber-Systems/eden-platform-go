package connectapi

import (
	"context"
	"errors"

	connect "connectrpc.com/connect"
	platformv1 "github.com/aocybersystems/eden-platform-go/gen/go/platform/v1"
	"github.com/aocybersystems/eden-platform-go/platform/auth"
)

type AuthHandler struct {
	service *auth.Service
}

func NewAuthHandler(service *auth.Service) *AuthHandler {
	return &AuthHandler{service: service}
}

func (h *AuthHandler) SignUp(ctx context.Context, req *connect.Request[platformv1.SignUpRequest]) (*connect.Response[platformv1.AuthResponse], error) {
	response, err := h.service.SignUp(ctx, req.Msg.GetEmail(), req.Msg.GetPassword(), req.Msg.GetDisplayName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(authResponseFromDomain(response)), nil
}

func (h *AuthHandler) Login(ctx context.Context, req *connect.Request[platformv1.LoginRequest]) (*connect.Response[platformv1.AuthResponse], error) {
	response, err := h.service.Login(ctx, req.Msg.GetEmail(), req.Msg.GetPassword())
	if err != nil {
		code := connect.CodeInvalidArgument
		if errors.Is(err, context.Canceled) {
			code = connect.CodeCanceled
		}
		return nil, connect.NewError(code, err)
	}
	return connect.NewResponse(authResponseFromDomain(response)), nil
}

func (h *AuthHandler) RefreshToken(ctx context.Context, req *connect.Request[platformv1.RefreshTokenRequest]) (*connect.Response[platformv1.AuthResponse], error) {
	response, err := h.service.RefreshToken(ctx, req.Msg.GetRefreshToken())
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}
	return connect.NewResponse(authResponseFromDomain(response)), nil
}

func (h *AuthHandler) Logout(ctx context.Context, req *connect.Request[platformv1.LogoutRequest]) (*connect.Response[platformv1.LogoutResponse], error) {
	if err := h.service.Logout(ctx, req.Msg.GetRefreshToken()); err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}
	return connect.NewResponse(&platformv1.LogoutResponse{}), nil
}

func (h *AuthHandler) InitiateOIDC(ctx context.Context, req *connect.Request[platformv1.InitiateOIDCRequest]) (*connect.Response[platformv1.InitiateOIDCResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("oidc not implemented in dev server"))
}

func (h *AuthHandler) InitiateSAML(ctx context.Context, req *connect.Request[platformv1.InitiateSAMLRequest]) (*connect.Response[platformv1.InitiateSAMLResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("saml not implemented in dev server"))
}
