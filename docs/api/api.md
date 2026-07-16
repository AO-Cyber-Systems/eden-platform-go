# Protocol Documentation
<a name="top"></a>

## Table of Contents

- [experience/v1/experience.proto](#experience_v1_experience-proto)
    - [ActionGate](#experience-v1-ActionGate)
    - [AgentNode](#experience-v1-AgentNode)
    - [AgentSpec](#experience-v1-AgentSpec)
    - [AppDefinition](#experience-v1-AppDefinition)
    - [AppMeta](#experience-v1-AppMeta)
    - [AudienceBinding](#experience-v1-AudienceBinding)
    - [BudgetPolicy](#experience-v1-BudgetPolicy)
    - [CredentialRef](#experience-v1-CredentialRef)
    - [DeepLinkSpec](#experience-v1-DeepLinkSpec)
    - [ExperienceSpec](#experience-v1-ExperienceSpec)
    - [ExperienceSpec.CustomFieldsEntry](#experience-v1-ExperienceSpec-CustomFieldsEntry)
    - [ExperienceSpec.FlagOverridesEntry](#experience-v1-ExperienceSpec-FlagOverridesEntry)
    - [ExperienceSpec.RulePolicyEntry](#experience-v1-ExperienceSpec-RulePolicyEntry)
    - [ExperienceSpec.SurfaceOfflineEntry](#experience-v1-ExperienceSpec-SurfaceOfflineEntry)
    - [HitlPolicy](#experience-v1-HitlPolicy)
    - [KnowledgePolicy](#experience-v1-KnowledgePolicy)
    - [LocaleSpec](#experience-v1-LocaleSpec)
    - [LockedSurface](#experience-v1-LockedSurface)
    - [NavEdge](#experience-v1-NavEdge)
    - [NavEdge.ParamBindingsEntry](#experience-v1-NavEdge-ParamBindingsEntry)
    - [NavGraph](#experience-v1-NavGraph)
    - [NavSlot](#experience-v1-NavSlot)
    - [OfflineSpec](#experience-v1-OfflineSpec)
    - [ResolutionContext](#experience-v1-ResolutionContext)
    - [ResolveSpecRequest](#experience-v1-ResolveSpecRequest)
    - [ResolveSpecResponse](#experience-v1-ResolveSpecResponse)
    - [ServiceTransportBinding](#experience-v1-ServiceTransportBinding)
    - [SigningSpec](#experience-v1-SigningSpec)
    - [SigningSpec.ByPlatformEntry](#experience-v1-SigningSpec-ByPlatformEntry)
    - [StoreSpecRequest](#experience-v1-StoreSpecRequest)
    - [StoreSpecResponse](#experience-v1-StoreSpecResponse)
    - [SurfaceRegistryManifest](#experience-v1-SurfaceRegistryManifest)
    - [TelemetryEnvelope](#experience-v1-TelemetryEnvelope)
    - [TermSet](#experience-v1-TermSet)
    - [TermSet.OverridesEntry](#experience-v1-TermSet-OverridesEntry)
    - [ThemeSpec](#experience-v1-ThemeSpec)
    - [ThemeSpec.ColorOverridesEntry](#experience-v1-ThemeSpec-ColorOverridesEntry)
    - [ToolDefinition](#experience-v1-ToolDefinition)
    - [ValidateSpecRequest](#experience-v1-ValidateSpecRequest)
    - [ValidateSpecResponse](#experience-v1-ValidateSpecResponse)
    - [ValidationProblem](#experience-v1-ValidationProblem)
  
    - [AgentAudience](#experience-v1-AgentAudience)
    - [ConflictPolicy](#experience-v1-ConflictPolicy)
    - [OfflinePolicy](#experience-v1-OfflinePolicy)
    - [Operation](#experience-v1-Operation)
    - [PaginationKind](#experience-v1-PaginationKind)
    - [Placement](#experience-v1-Placement)
    - [ScopeAuthority](#experience-v1-ScopeAuthority)
    - [SideEffect](#experience-v1-SideEffect)
    - [ToolVisibility](#experience-v1-ToolVisibility)
    - [TransportKind](#experience-v1-TransportKind)
    - [UnknownSurfacePolicy](#experience-v1-UnknownSurfacePolicy)
  
    - [ExperienceService](#experience-v1-ExperienceService)
  
- [platform/v1/ac2_evidence.proto](#platform_v1_ac2_evidence-proto)
    - [AC2EvidenceServiceGetAccountInventoryRequest](#platform-v1-AC2EvidenceServiceGetAccountInventoryRequest)
    - [AC2EvidenceServiceGetAccountInventoryResponse](#platform-v1-AC2EvidenceServiceGetAccountInventoryResponse)
    - [AC2EvidenceServiceGetDormantAccountsRequest](#platform-v1-AC2EvidenceServiceGetDormantAccountsRequest)
    - [AC2EvidenceServiceGetDormantAccountsResponse](#platform-v1-AC2EvidenceServiceGetDormantAccountsResponse)
    - [AC2EvidenceServiceGetLifecycleDecisionsRequest](#platform-v1-AC2EvidenceServiceGetLifecycleDecisionsRequest)
    - [AC2EvidenceServiceGetLifecycleDecisionsResponse](#platform-v1-AC2EvidenceServiceGetLifecycleDecisionsResponse)
    - [AC2EvidenceServiceGetRecertificationStatusRequest](#platform-v1-AC2EvidenceServiceGetRecertificationStatusRequest)
    - [AC2EvidenceServiceGetRecertificationStatusResponse](#platform-v1-AC2EvidenceServiceGetRecertificationStatusResponse)
    - [AC2EvidenceServiceGetRoleBindingsRequest](#platform-v1-AC2EvidenceServiceGetRoleBindingsRequest)
    - [AC2EvidenceServiceGetRoleBindingsResponse](#platform-v1-AC2EvidenceServiceGetRoleBindingsResponse)
    - [AccountInventoryRow](#platform-v1-AccountInventoryRow)
    - [DormantAccountRow](#platform-v1-DormantAccountRow)
    - [LifecycleDecisionRow](#platform-v1-LifecycleDecisionRow)
    - [RecertificationStatusRow](#platform-v1-RecertificationStatusRow)
    - [RoleBindingRow](#platform-v1-RoleBindingRow)
  
    - [AC2EvidenceService](#platform-v1-AC2EvidenceService)
  
- [platform/v1/account_self_service.proto](#platform_v1_account_self_service-proto)
    - [AccountSelfServiceGetMyProfileRequest](#platform-v1-AccountSelfServiceGetMyProfileRequest)
    - [AccountSelfServiceListMyApiKeysRequest](#platform-v1-AccountSelfServiceListMyApiKeysRequest)
    - [AccountSelfServiceListMyIdPLinksRequest](#platform-v1-AccountSelfServiceListMyIdPLinksRequest)
    - [AccountSelfServiceListMyMFAFactorsRequest](#platform-v1-AccountSelfServiceListMyMFAFactorsRequest)
    - [AccountSelfServiceListMyOAuthGrantsRequest](#platform-v1-AccountSelfServiceListMyOAuthGrantsRequest)
    - [AccountSelfServiceRemoveMyMFARequest](#platform-v1-AccountSelfServiceRemoveMyMFARequest)
    - [AccountSelfServiceRemoveMyMFAResponse](#platform-v1-AccountSelfServiceRemoveMyMFAResponse)
    - [AccountSelfServiceRevokeMyApiKeyRequest](#platform-v1-AccountSelfServiceRevokeMyApiKeyRequest)
    - [AccountSelfServiceRevokeMyApiKeyResponse](#platform-v1-AccountSelfServiceRevokeMyApiKeyResponse)
    - [AccountSelfServiceRevokeMyOAuthGrantRequest](#platform-v1-AccountSelfServiceRevokeMyOAuthGrantRequest)
    - [AccountSelfServiceRevokeMyOAuthGrantResponse](#platform-v1-AccountSelfServiceRevokeMyOAuthGrantResponse)
    - [AccountSelfServiceUnlinkMyIdPRequest](#platform-v1-AccountSelfServiceUnlinkMyIdPRequest)
    - [AccountSelfServiceUnlinkMyIdPResponse](#platform-v1-AccountSelfServiceUnlinkMyIdPResponse)
    - [AccountSelfServiceUpdateMyProfileRequest](#platform-v1-AccountSelfServiceUpdateMyProfileRequest)
    - [ApiKeySummary](#platform-v1-ApiKeySummary)
    - [CommunicationPrefs](#platform-v1-CommunicationPrefs)
    - [IdPLink](#platform-v1-IdPLink)
    - [ListMyApiKeysResponse](#platform-v1-ListMyApiKeysResponse)
    - [ListMyIdPLinksResponse](#platform-v1-ListMyIdPLinksResponse)
    - [ListMyMFAFactorsResponse](#platform-v1-ListMyMFAFactorsResponse)
    - [ListMyOAuthGrantsResponse](#platform-v1-ListMyOAuthGrantsResponse)
    - [MFAFactor](#platform-v1-MFAFactor)
    - [MyProfile](#platform-v1-MyProfile)
    - [OAuthGrant](#platform-v1-OAuthGrant)
  
    - [AccountSelfService](#platform-v1-AccountSelfService)
  
- [platform/v1/aoedge.proto](#platform_v1_aoedge-proto)
    - [BackendEntry](#platform-v1-BackendEntry)
    - [GetBackendHealthRequest](#platform-v1-GetBackendHealthRequest)
    - [GetBackendHealthResponse](#platform-v1-GetBackendHealthResponse)
    - [GetBuildInfoRequest](#platform-v1-GetBuildInfoRequest)
    - [GetBuildInfoResponse](#platform-v1-GetBuildInfoResponse)
    - [HealthCheckRequest](#platform-v1-HealthCheckRequest)
    - [HealthCheckResponse](#platform-v1-HealthCheckResponse)
    - [ListRoutesRequest](#platform-v1-ListRoutesRequest)
    - [ListRoutesResponse](#platform-v1-ListRoutesResponse)
    - [RouteEntry](#platform-v1-RouteEntry)
  
    - [BackendHealthState](#platform-v1-BackendHealthState)
  
    - [AOEdgeAdminService](#platform-v1-AOEdgeAdminService)
  
- [platform/v1/aoedge_audit.proto](#platform_v1_aoedge_audit-proto)
    - [AuditBatch](#platform-v1-AuditBatch)
    - [BundleReloadEvent](#platform-v1-BundleReloadEvent)
    - [ConnectionLog](#platform-v1-ConnectionLog)
    - [DDoSEvent](#platform-v1-DDoSEvent)
    - [DLPFinding](#platform-v1-DLPFinding)
    - [DLPMatch](#platform-v1-DLPMatch)
    - [GeoDecision](#platform-v1-GeoDecision)
    - [IdentityMintEvent](#platform-v1-IdentityMintEvent)
    - [IdentityValidationEvent](#platform-v1-IdentityValidationEvent)
    - [MatchedRuleEntry](#platform-v1-MatchedRuleEntry)
    - [PolicyDecisionEvent](#platform-v1-PolicyDecisionEvent)
    - [StepUpChallengeEvent](#platform-v1-StepUpChallengeEvent)
    - [WAFEvent](#platform-v1-WAFEvent)
  
- [platform/v1/api_key_validation.proto](#platform_v1_api_key_validation-proto)
    - [ApiKeyValidationServiceValidateApiKeyRequest](#platform-v1-ApiKeyValidationServiceValidateApiKeyRequest)
    - [ApiKeyValidationServiceValidateApiKeyResponse](#platform-v1-ApiKeyValidationServiceValidateApiKeyResponse)
  
    - [ApiKeyValidationService](#platform-v1-ApiKeyValidationService)
  
- [platform/v1/audit.proto](#platform_v1_audit-proto)
    - [AuditLogEntry](#platform-v1-AuditLogEntry)
    - [IngestBreakGlassEventRequest](#platform-v1-IngestBreakGlassEventRequest)
    - [IngestBreakGlassEventResponse](#platform-v1-IngestBreakGlassEventResponse)
    - [ListAuditLogsRequest](#platform-v1-ListAuditLogsRequest)
    - [ListAuditLogsResponse](#platform-v1-ListAuditLogsResponse)
  
    - [AuditService](#platform-v1-AuditService)
  
- [platform/v1/audit_query.proto](#platform_v1_audit_query-proto)
    - [AuditEvent](#platform-v1-AuditEvent)
    - [AuditQueryServiceGetAuditEventRequest](#platform-v1-AuditQueryServiceGetAuditEventRequest)
    - [AuditQueryServiceGetAuditEventResponse](#platform-v1-AuditQueryServiceGetAuditEventResponse)
    - [AuditQueryServiceQueryAuditEventsRequest](#platform-v1-AuditQueryServiceQueryAuditEventsRequest)
    - [AuditQueryServiceQueryAuditEventsResponse](#platform-v1-AuditQueryServiceQueryAuditEventsResponse)
  
    - [AuditQueryService](#platform-v1-AuditQueryService)
  
- [platform/v1/auth.proto](#platform_v1_auth-proto)
    - [AuthData](#platform-v1-AuthData)
    - [InitiateOIDCRequest](#platform-v1-InitiateOIDCRequest)
    - [InitiateOIDCResponse](#platform-v1-InitiateOIDCResponse)
    - [InitiateSAMLRequest](#platform-v1-InitiateSAMLRequest)
    - [InitiateSAMLResponse](#platform-v1-InitiateSAMLResponse)
    - [InitiateSocialLoginRequest](#platform-v1-InitiateSocialLoginRequest)
    - [InitiateSocialLoginResponse](#platform-v1-InitiateSocialLoginResponse)
    - [LoginRequest](#platform-v1-LoginRequest)
    - [LoginResponse](#platform-v1-LoginResponse)
    - [LogoutRequest](#platform-v1-LogoutRequest)
    - [LogoutResponse](#platform-v1-LogoutResponse)
    - [RefreshTokenRequest](#platform-v1-RefreshTokenRequest)
    - [RefreshTokenResponse](#platform-v1-RefreshTokenResponse)
    - [SignUpRequest](#platform-v1-SignUpRequest)
    - [SignUpResponse](#platform-v1-SignUpResponse)
    - [UpdateProfileRequest](#platform-v1-UpdateProfileRequest)
    - [UpdateProfileResponse](#platform-v1-UpdateProfileResponse)
    - [User](#platform-v1-User)
  
    - [AuthService](#platform-v1-AuthService)
  
- [platform/v1/authn.proto](#platform_v1_authn-proto)
    - [AuthnServiceLogoutRequest](#platform-v1-AuthnServiceLogoutRequest)
    - [AuthnServiceLogoutResponse](#platform-v1-AuthnServiceLogoutResponse)
    - [BeginDiscoverableWebAuthnLoginRequest](#platform-v1-BeginDiscoverableWebAuthnLoginRequest)
    - [BeginDiscoverableWebAuthnLoginResponse](#platform-v1-BeginDiscoverableWebAuthnLoginResponse)
    - [BeginStepUpRequest](#platform-v1-BeginStepUpRequest)
    - [BeginStepUpResponse](#platform-v1-BeginStepUpResponse)
    - [BeginWebAuthnLoginRequest](#platform-v1-BeginWebAuthnLoginRequest)
    - [BeginWebAuthnLoginResponse](#platform-v1-BeginWebAuthnLoginResponse)
    - [BeginWebAuthnRegistrationRequest](#platform-v1-BeginWebAuthnRegistrationRequest)
    - [BeginWebAuthnRegistrationResponse](#platform-v1-BeginWebAuthnRegistrationResponse)
    - [ChangePasswordRequest](#platform-v1-ChangePasswordRequest)
    - [ChangePasswordResponse](#platform-v1-ChangePasswordResponse)
    - [CompleteTOTPEnrollRequest](#platform-v1-CompleteTOTPEnrollRequest)
    - [CompleteTOTPEnrollResponse](#platform-v1-CompleteTOTPEnrollResponse)
    - [CredentialSummary](#platform-v1-CredentialSummary)
    - [EnrollTOTPRequest](#platform-v1-EnrollTOTPRequest)
    - [EnrollTOTPResponse](#platform-v1-EnrollTOTPResponse)
    - [FinishDiscoverableWebAuthnLoginRequest](#platform-v1-FinishDiscoverableWebAuthnLoginRequest)
    - [FinishDiscoverableWebAuthnLoginResponse](#platform-v1-FinishDiscoverableWebAuthnLoginResponse)
    - [FinishStepUpRequest](#platform-v1-FinishStepUpRequest)
    - [FinishStepUpResponse](#platform-v1-FinishStepUpResponse)
    - [FinishWebAuthnLoginRequest](#platform-v1-FinishWebAuthnLoginRequest)
    - [FinishWebAuthnLoginResponse](#platform-v1-FinishWebAuthnLoginResponse)
    - [FinishWebAuthnRegistrationRequest](#platform-v1-FinishWebAuthnRegistrationRequest)
    - [FinishWebAuthnRegistrationResponse](#platform-v1-FinishWebAuthnRegistrationResponse)
    - [ListMyCredentialsRequest](#platform-v1-ListMyCredentialsRequest)
    - [ListMyCredentialsResponse](#platform-v1-ListMyCredentialsResponse)
    - [ListMySessionsRequest](#platform-v1-ListMySessionsRequest)
    - [ListMySessionsResponse](#platform-v1-ListMySessionsResponse)
    - [LoginNextStep](#platform-v1-LoginNextStep)
    - [LoginWithPIVRequest](#platform-v1-LoginWithPIVRequest)
    - [LoginWithPIVResponse](#platform-v1-LoginWithPIVResponse)
    - [LoginWithTOTPRequest](#platform-v1-LoginWithTOTPRequest)
    - [LoginWithTOTPResponse](#platform-v1-LoginWithTOTPResponse)
    - [PasswordLoginCompleteRequest](#platform-v1-PasswordLoginCompleteRequest)
    - [PasswordLoginCompleteResponse](#platform-v1-PasswordLoginCompleteResponse)
    - [PasswordLoginStartRequest](#platform-v1-PasswordLoginStartRequest)
    - [PasswordLoginStartResponse](#platform-v1-PasswordLoginStartResponse)
    - [ResolveWorkspace](#platform-v1-ResolveWorkspace)
    - [ResolveWorkspacesByEmailRequest](#platform-v1-ResolveWorkspacesByEmailRequest)
    - [ResolveWorkspacesByEmailResponse](#platform-v1-ResolveWorkspacesByEmailResponse)
    - [RevokeMyCredentialRequest](#platform-v1-RevokeMyCredentialRequest)
    - [RevokeMyCredentialResponse](#platform-v1-RevokeMyCredentialResponse)
    - [RevokeMySessionRequest](#platform-v1-RevokeMySessionRequest)
    - [RevokeMySessionResponse](#platform-v1-RevokeMySessionResponse)
    - [SessionSummary](#platform-v1-SessionSummary)
    - [WhoAmIRequest](#platform-v1-WhoAmIRequest)
    - [WhoAmIResponse](#platform-v1-WhoAmIResponse)
  
    - [ResolveStatus](#platform-v1-ResolveStatus)
  
    - [AuthnService](#platform-v1-AuthnService)
    - [EndUserSessionService](#platform-v1-EndUserSessionService)
  
- [platform/v1/bridge.proto](#platform_v1_bridge-proto)
    - [ActionSchema](#platform-v1-ActionSchema)
    - [AdapterInfo](#platform-v1-AdapterInfo)
    - [DispatchActionRequest](#platform-v1-DispatchActionRequest)
    - [DispatchActionResponse](#platform-v1-DispatchActionResponse)
    - [ListActionsRequest](#platform-v1-ListActionsRequest)
    - [ListActionsResponse](#platform-v1-ListActionsResponse)
    - [ListAdaptersRequest](#platform-v1-ListAdaptersRequest)
    - [ListAdaptersResponse](#platform-v1-ListAdaptersResponse)
  
    - [BridgeService](#platform-v1-BridgeService)
  
- [platform/v1/company.proto](#platform_v1_company-proto)
    - [CompanyData](#platform-v1-CompanyData)
    - [CreateCompanyRequest](#platform-v1-CreateCompanyRequest)
    - [CreateCompanyResponse](#platform-v1-CreateCompanyResponse)
    - [GetAncestorsRequest](#platform-v1-GetAncestorsRequest)
    - [GetAncestorsResponse](#platform-v1-GetAncestorsResponse)
    - [GetCompanyRequest](#platform-v1-GetCompanyRequest)
    - [GetCompanyResponse](#platform-v1-GetCompanyResponse)
    - [GetDescendantsRequest](#platform-v1-GetDescendantsRequest)
    - [GetDescendantsResponse](#platform-v1-GetDescendantsResponse)
    - [GetEffectiveSettingsRequest](#platform-v1-GetEffectiveSettingsRequest)
    - [GetEffectiveSettingsResponse](#platform-v1-GetEffectiveSettingsResponse)
    - [ListCompaniesRequest](#platform-v1-ListCompaniesRequest)
    - [ListCompaniesResponse](#platform-v1-ListCompaniesResponse)
    - [UpdateCompanyRequest](#platform-v1-UpdateCompanyRequest)
    - [UpdateCompanyResponse](#platform-v1-UpdateCompanyResponse)
  
    - [CompanyService](#platform-v1-CompanyService)
  
- [platform/v1/credential_admin.proto](#platform_v1_credential_admin-proto)
    - [ApiKey](#platform-v1-ApiKey)
    - [CertificateSummary](#platform-v1-CertificateSummary)
    - [CredentialAdminServiceGetApiKeyRequest](#platform-v1-CredentialAdminServiceGetApiKeyRequest)
    - [CredentialAdminServiceGetApiKeyResponse](#platform-v1-CredentialAdminServiceGetApiKeyResponse)
    - [CredentialAdminServiceListApiKeysRequest](#platform-v1-CredentialAdminServiceListApiKeysRequest)
    - [CredentialAdminServiceListApiKeysResponse](#platform-v1-CredentialAdminServiceListApiKeysResponse)
    - [CredentialAdminServiceMintApiKeyRequest](#platform-v1-CredentialAdminServiceMintApiKeyRequest)
    - [CredentialAdminServiceMintApiKeyResponse](#platform-v1-CredentialAdminServiceMintApiKeyResponse)
    - [CredentialAdminServiceRevokeApiKeyRequest](#platform-v1-CredentialAdminServiceRevokeApiKeyRequest)
    - [CredentialAdminServiceRevokeApiKeyResponse](#platform-v1-CredentialAdminServiceRevokeApiKeyResponse)
    - [CredentialAdminServiceRotateApiKeyRequest](#platform-v1-CredentialAdminServiceRotateApiKeyRequest)
    - [CredentialAdminServiceRotateApiKeyResponse](#platform-v1-CredentialAdminServiceRotateApiKeyResponse)
    - [GetCertificateRequest](#platform-v1-GetCertificateRequest)
    - [GetCertificateResponse](#platform-v1-GetCertificateResponse)
    - [IssueCertificateRequest](#platform-v1-IssueCertificateRequest)
    - [IssueCertificateResponse](#platform-v1-IssueCertificateResponse)
    - [ListCertificatesRequest](#platform-v1-ListCertificatesRequest)
    - [ListCertificatesResponse](#platform-v1-ListCertificatesResponse)
    - [RenewCertificateRequest](#platform-v1-RenewCertificateRequest)
    - [RenewCertificateResponse](#platform-v1-RenewCertificateResponse)
    - [RevokeCertificateRequest](#platform-v1-RevokeCertificateRequest)
    - [RevokeCertificateResponse](#platform-v1-RevokeCertificateResponse)
  
    - [CredentialAdminService](#platform-v1-CredentialAdminService)
  
- [platform/v1/federation_types.proto](#platform_v1_federation_types-proto)
    - [AttributeMapping](#platform-v1-AttributeMapping)
    - [AttributeMapping.CustomAttrsEntry](#platform-v1-AttributeMapping-CustomAttrsEntry)
    - [DownstreamSP](#platform-v1-DownstreamSP)
    - [ExternalIdP](#platform-v1-ExternalIdP)
    - [FederationPolicy](#platform-v1-FederationPolicy)
  
- [platform/v1/oauth_admin.proto](#platform_v1_oauth_admin-proto)
    - [OAuthAdminServiceAddRedirectURIRequest](#platform-v1-OAuthAdminServiceAddRedirectURIRequest)
    - [OAuthAdminServiceAddRedirectURIResponse](#platform-v1-OAuthAdminServiceAddRedirectURIResponse)
    - [OAuthAdminServiceCreateClientRequest](#platform-v1-OAuthAdminServiceCreateClientRequest)
    - [OAuthAdminServiceCreateClientResponse](#platform-v1-OAuthAdminServiceCreateClientResponse)
    - [OAuthAdminServiceDeleteClientRequest](#platform-v1-OAuthAdminServiceDeleteClientRequest)
    - [OAuthAdminServiceDeleteClientResponse](#platform-v1-OAuthAdminServiceDeleteClientResponse)
    - [OAuthAdminServiceGetClientRequest](#platform-v1-OAuthAdminServiceGetClientRequest)
    - [OAuthAdminServiceGetClientResponse](#platform-v1-OAuthAdminServiceGetClientResponse)
    - [OAuthAdminServiceListClientsRequest](#platform-v1-OAuthAdminServiceListClientsRequest)
    - [OAuthAdminServiceListClientsResponse](#platform-v1-OAuthAdminServiceListClientsResponse)
    - [OAuthAdminServiceRemoveRedirectURIRequest](#platform-v1-OAuthAdminServiceRemoveRedirectURIRequest)
    - [OAuthAdminServiceRemoveRedirectURIResponse](#platform-v1-OAuthAdminServiceRemoveRedirectURIResponse)
    - [OAuthAdminServiceRotateClientSecretRequest](#platform-v1-OAuthAdminServiceRotateClientSecretRequest)
    - [OAuthAdminServiceRotateClientSecretResponse](#platform-v1-OAuthAdminServiceRotateClientSecretResponse)
    - [OAuthAdminServiceUpdateClientRequest](#platform-v1-OAuthAdminServiceUpdateClientRequest)
    - [OAuthAdminServiceUpdateClientResponse](#platform-v1-OAuthAdminServiceUpdateClientResponse)
    - [OAuthClient](#platform-v1-OAuthClient)
  
    - [OAuthAdminService](#platform-v1-OAuthAdminService)
  
- [platform/v1/federation_admin.proto](#platform_v1_federation_admin-proto)
    - [ClientIdPOption](#platform-v1-ClientIdPOption)
    - [FederationAdminServiceAddClientIdPOptionRequest](#platform-v1-FederationAdminServiceAddClientIdPOptionRequest)
    - [FederationAdminServiceAddClientIdPOptionResponse](#platform-v1-FederationAdminServiceAddClientIdPOptionResponse)
    - [FederationAdminServiceCreateAttributeMappingRequest](#platform-v1-FederationAdminServiceCreateAttributeMappingRequest)
    - [FederationAdminServiceCreateAttributeMappingRequest.CustomAttrsEntry](#platform-v1-FederationAdminServiceCreateAttributeMappingRequest-CustomAttrsEntry)
    - [FederationAdminServiceCreateAttributeMappingResponse](#platform-v1-FederationAdminServiceCreateAttributeMappingResponse)
    - [FederationAdminServiceCreateExternalIdPRequest](#platform-v1-FederationAdminServiceCreateExternalIdPRequest)
    - [FederationAdminServiceCreateExternalIdPResponse](#platform-v1-FederationAdminServiceCreateExternalIdPResponse)
    - [FederationAdminServiceDeleteAttributeMappingRequest](#platform-v1-FederationAdminServiceDeleteAttributeMappingRequest)
    - [FederationAdminServiceDeleteAttributeMappingResponse](#platform-v1-FederationAdminServiceDeleteAttributeMappingResponse)
    - [FederationAdminServiceDeleteDownstreamSPRequest](#platform-v1-FederationAdminServiceDeleteDownstreamSPRequest)
    - [FederationAdminServiceDeleteDownstreamSPResponse](#platform-v1-FederationAdminServiceDeleteDownstreamSPResponse)
    - [FederationAdminServiceDeleteExternalIdPRequest](#platform-v1-FederationAdminServiceDeleteExternalIdPRequest)
    - [FederationAdminServiceDeleteExternalIdPResponse](#platform-v1-FederationAdminServiceDeleteExternalIdPResponse)
    - [FederationAdminServiceDeleteFederationPolicyRequest](#platform-v1-FederationAdminServiceDeleteFederationPolicyRequest)
    - [FederationAdminServiceDeleteFederationPolicyResponse](#platform-v1-FederationAdminServiceDeleteFederationPolicyResponse)
    - [FederationAdminServiceGetExternalIdPRequest](#platform-v1-FederationAdminServiceGetExternalIdPRequest)
    - [FederationAdminServiceGetExternalIdPResponse](#platform-v1-FederationAdminServiceGetExternalIdPResponse)
    - [FederationAdminServiceGetFederationPolicyRequest](#platform-v1-FederationAdminServiceGetFederationPolicyRequest)
    - [FederationAdminServiceGetFederationPolicyResponse](#platform-v1-FederationAdminServiceGetFederationPolicyResponse)
    - [FederationAdminServiceListAttributeMappingsRequest](#platform-v1-FederationAdminServiceListAttributeMappingsRequest)
    - [FederationAdminServiceListAttributeMappingsResponse](#platform-v1-FederationAdminServiceListAttributeMappingsResponse)
    - [FederationAdminServiceListDownstreamSPsRequest](#platform-v1-FederationAdminServiceListDownstreamSPsRequest)
    - [FederationAdminServiceListDownstreamSPsResponse](#platform-v1-FederationAdminServiceListDownstreamSPsResponse)
    - [FederationAdminServiceListExternalIdPsRequest](#platform-v1-FederationAdminServiceListExternalIdPsRequest)
    - [FederationAdminServiceListExternalIdPsResponse](#platform-v1-FederationAdminServiceListExternalIdPsResponse)
    - [FederationAdminServiceRegisterDownstreamClientRequest](#platform-v1-FederationAdminServiceRegisterDownstreamClientRequest)
    - [FederationAdminServiceRegisterDownstreamClientResponse](#platform-v1-FederationAdminServiceRegisterDownstreamClientResponse)
    - [FederationAdminServiceRegisterDownstreamSPRequest](#platform-v1-FederationAdminServiceRegisterDownstreamSPRequest)
    - [FederationAdminServiceRegisterDownstreamSPResponse](#platform-v1-FederationAdminServiceRegisterDownstreamSPResponse)
    - [FederationAdminServiceRemoveClientIdPOptionRequest](#platform-v1-FederationAdminServiceRemoveClientIdPOptionRequest)
    - [FederationAdminServiceRemoveClientIdPOptionResponse](#platform-v1-FederationAdminServiceRemoveClientIdPOptionResponse)
    - [FederationAdminServiceUpdateAttributeMappingRequest](#platform-v1-FederationAdminServiceUpdateAttributeMappingRequest)
    - [FederationAdminServiceUpdateAttributeMappingRequest.CustomAttrsEntry](#platform-v1-FederationAdminServiceUpdateAttributeMappingRequest-CustomAttrsEntry)
    - [FederationAdminServiceUpdateAttributeMappingResponse](#platform-v1-FederationAdminServiceUpdateAttributeMappingResponse)
    - [FederationAdminServiceUpdateExternalIdPRequest](#platform-v1-FederationAdminServiceUpdateExternalIdPRequest)
    - [FederationAdminServiceUpdateExternalIdPResponse](#platform-v1-FederationAdminServiceUpdateExternalIdPResponse)
    - [FederationAdminServiceUpsertFederationPolicyRequest](#platform-v1-FederationAdminServiceUpsertFederationPolicyRequest)
    - [FederationAdminServiceUpsertFederationPolicyResponse](#platform-v1-FederationAdminServiceUpsertFederationPolicyResponse)
  
    - [FederationAdminService](#platform-v1-FederationAdminService)
  
- [platform/v1/identity_admin.proto](#platform_v1_identity_admin-proto)
    - [AccountAdminServiceAssignRoleRequest](#platform-v1-AccountAdminServiceAssignRoleRequest)
    - [AccountAdminServiceAssignRoleResponse](#platform-v1-AccountAdminServiceAssignRoleResponse)
    - [AccountAdminServiceClearAccountMFAFactorsRequest](#platform-v1-AccountAdminServiceClearAccountMFAFactorsRequest)
    - [AccountAdminServiceClearAccountMFAFactorsResponse](#platform-v1-AccountAdminServiceClearAccountMFAFactorsResponse)
    - [AccountAdminServiceGetRecertificationHistoryRequest](#platform-v1-AccountAdminServiceGetRecertificationHistoryRequest)
    - [AccountAdminServiceGetRecertificationHistoryResponse](#platform-v1-AccountAdminServiceGetRecertificationHistoryResponse)
    - [AccountAdminServiceListPendingRecertificationsRequest](#platform-v1-AccountAdminServiceListPendingRecertificationsRequest)
    - [AccountAdminServiceListPendingRecertificationsResponse](#platform-v1-AccountAdminServiceListPendingRecertificationsResponse)
    - [AccountAdminServiceListRolesRequest](#platform-v1-AccountAdminServiceListRolesRequest)
    - [AccountAdminServiceListRolesResponse](#platform-v1-AccountAdminServiceListRolesResponse)
    - [AccountAdminServiceSubmitRecertificationDecisionRequest](#platform-v1-AccountAdminServiceSubmitRecertificationDecisionRequest)
    - [AccountAdminServiceSubmitRecertificationDecisionResponse](#platform-v1-AccountAdminServiceSubmitRecertificationDecisionResponse)
    - [AccountData](#platform-v1-AccountData)
    - [AddAccountToGroupRequest](#platform-v1-AddAccountToGroupRequest)
    - [AddAccountToGroupResponse](#platform-v1-AddAccountToGroupResponse)
    - [AssistedAccountRecoveryRequest](#platform-v1-AssistedAccountRecoveryRequest)
    - [AssistedAccountRecoveryResponse](#platform-v1-AssistedAccountRecoveryResponse)
    - [ClearanceInfo](#platform-v1-ClearanceInfo)
    - [CreateTenantRequest](#platform-v1-CreateTenantRequest)
    - [CreateTenantResponse](#platform-v1-CreateTenantResponse)
    - [DefineGroupRequest](#platform-v1-DefineGroupRequest)
    - [DefineGroupResponse](#platform-v1-DefineGroupResponse)
    - [DefineRoleRequest](#platform-v1-DefineRoleRequest)
    - [DefineRoleResponse](#platform-v1-DefineRoleResponse)
    - [DeleteEntitlementRequest](#platform-v1-DeleteEntitlementRequest)
    - [DeleteEntitlementResponse](#platform-v1-DeleteEntitlementResponse)
    - [DeprovisionAccountRequest](#platform-v1-DeprovisionAccountRequest)
    - [DeprovisionAccountResponse](#platform-v1-DeprovisionAccountResponse)
    - [Entitlement](#platform-v1-Entitlement)
    - [GetAccountRequest](#platform-v1-GetAccountRequest)
    - [GetAccountResponse](#platform-v1-GetAccountResponse)
    - [GetTenantRequest](#platform-v1-GetTenantRequest)
    - [GetTenantResponse](#platform-v1-GetTenantResponse)
    - [Group](#platform-v1-Group)
    - [ListAccountsRequest](#platform-v1-ListAccountsRequest)
    - [ListAccountsResponse](#platform-v1-ListAccountsResponse)
    - [ListEntitlementsRequest](#platform-v1-ListEntitlementsRequest)
    - [ListEntitlementsResponse](#platform-v1-ListEntitlementsResponse)
    - [ListGroupsRequest](#platform-v1-ListGroupsRequest)
    - [ListGroupsResponse](#platform-v1-ListGroupsResponse)
    - [ListTenantsRequest](#platform-v1-ListTenantsRequest)
    - [ListTenantsResponse](#platform-v1-ListTenantsResponse)
    - [PendingRecertificationRow](#platform-v1-PendingRecertificationRow)
    - [ProvisionAccountRequest](#platform-v1-ProvisionAccountRequest)
    - [ProvisionAccountResponse](#platform-v1-ProvisionAccountResponse)
    - [ProvisionServiceAccountRequest](#platform-v1-ProvisionServiceAccountRequest)
    - [ProvisionServiceAccountResponse](#platform-v1-ProvisionServiceAccountResponse)
    - [RecertificationHistoryRow](#platform-v1-RecertificationHistoryRow)
    - [RecoverAccountRequest](#platform-v1-RecoverAccountRequest)
    - [RecoverAccountResponse](#platform-v1-RecoverAccountResponse)
    - [RemoveAccountFromGroupRequest](#platform-v1-RemoveAccountFromGroupRequest)
    - [RemoveAccountFromGroupResponse](#platform-v1-RemoveAccountFromGroupResponse)
    - [RevokeRoleRequest](#platform-v1-RevokeRoleRequest)
    - [RevokeRoleResponse](#platform-v1-RevokeRoleResponse)
    - [Role](#platform-v1-Role)
    - [SetEntitlementRequest](#platform-v1-SetEntitlementRequest)
    - [SetEntitlementResponse](#platform-v1-SetEntitlementResponse)
    - [SuspendAccountRequest](#platform-v1-SuspendAccountRequest)
    - [SuspendAccountResponse](#platform-v1-SuspendAccountResponse)
    - [Tenant](#platform-v1-Tenant)
    - [UpdateAccountRequest](#platform-v1-UpdateAccountRequest)
    - [UpdateAccountResponse](#platform-v1-UpdateAccountResponse)
  
    - [AccountAdminService](#platform-v1-AccountAdminService)
  
- [platform/v1/rbac.proto](#platform_v1_rbac-proto)
    - [AssignRoleRequest](#platform-v1-AssignRoleRequest)
    - [AssignRoleResponse](#platform-v1-AssignRoleResponse)
    - [CheckPermissionRequest](#platform-v1-CheckPermissionRequest)
    - [CheckPermissionResponse](#platform-v1-CheckPermissionResponse)
    - [CreateRoleRequest](#platform-v1-CreateRoleRequest)
    - [CreateRoleResponse](#platform-v1-CreateRoleResponse)
    - [GetUserPermissionsRequest](#platform-v1-GetUserPermissionsRequest)
    - [GetUserPermissionsResponse](#platform-v1-GetUserPermissionsResponse)
    - [ListPermissionsRequest](#platform-v1-ListPermissionsRequest)
    - [ListPermissionsResponse](#platform-v1-ListPermissionsResponse)
    - [ListRolesRequest](#platform-v1-ListRolesRequest)
    - [ListRolesResponse](#platform-v1-ListRolesResponse)
    - [PermissionResponse](#platform-v1-PermissionResponse)
    - [RemoveRoleRequest](#platform-v1-RemoveRoleRequest)
    - [RemoveRoleResponse](#platform-v1-RemoveRoleResponse)
    - [ResolveMembershipRequest](#platform-v1-ResolveMembershipRequest)
    - [ResolveMembershipResponse](#platform-v1-ResolveMembershipResponse)
    - [RoleData](#platform-v1-RoleData)
  
    - [RBACService](#platform-v1-RBACService)
  
- [platform/v1/recovery.proto](#platform_v1_recovery-proto)
    - [SelfRecoveryServiceConsumeRecoveryTokenRequest](#platform-v1-SelfRecoveryServiceConsumeRecoveryTokenRequest)
    - [SelfRecoveryServiceConsumeRecoveryTokenResponse](#platform-v1-SelfRecoveryServiceConsumeRecoveryTokenResponse)
    - [SelfRecoveryServiceRequestRecoveryRequest](#platform-v1-SelfRecoveryServiceRequestRecoveryRequest)
    - [SelfRecoveryServiceRequestRecoveryResponse](#platform-v1-SelfRecoveryServiceRequestRecoveryResponse)
  
    - [SelfRecoveryService](#platform-v1-SelfRecoveryService)
  
- [platform/v1/registry.proto](#platform_v1_registry-proto)
    - [GetBadgeCountsRequest](#platform-v1-GetBadgeCountsRequest)
    - [GetBadgeCountsResponse](#platform-v1-GetBadgeCountsResponse)
    - [GetBadgeCountsResponse.CountsEntry](#platform-v1-GetBadgeCountsResponse-CountsEntry)
    - [GetNavItemsRequest](#platform-v1-GetNavItemsRequest)
    - [GetNavItemsResponse](#platform-v1-GetNavItemsResponse)
    - [GetSearchScopesRequest](#platform-v1-GetSearchScopesRequest)
    - [GetSearchScopesResponse](#platform-v1-GetSearchScopesResponse)
    - [GetWidgetsRequest](#platform-v1-GetWidgetsRequest)
    - [GetWidgetsResponse](#platform-v1-GetWidgetsResponse)
    - [NavItem](#platform-v1-NavItem)
    - [SearchScope](#platform-v1-SearchScope)
    - [Widget](#platform-v1-Widget)
  
    - [RegistryService](#platform-v1-RegistryService)
  
- [platform/v1/svid.proto](#platform_v1_svid-proto)
    - [IssueSVIDRequest](#platform-v1-IssueSVIDRequest)
    - [IssueSVIDResponse](#platform-v1-IssueSVIDResponse)
    - [ListMySVIDsRequest](#platform-v1-ListMySVIDsRequest)
    - [ListMySVIDsResponse](#platform-v1-ListMySVIDsResponse)
    - [RevokeSVIDRequest](#platform-v1-RevokeSVIDRequest)
    - [RevokeSVIDResponse](#platform-v1-RevokeSVIDResponse)
    - [SVIDSummary](#platform-v1-SVIDSummary)
  
    - [SvidService](#platform-v1-SvidService)
  
- [platform/v1/webhook.proto](#platform_v1_webhook-proto)
    - [DeleteWebhookRequest](#platform-v1-DeleteWebhookRequest)
    - [DeleteWebhookResponse](#platform-v1-DeleteWebhookResponse)
    - [DeliveryResponse](#platform-v1-DeliveryResponse)
    - [ListDeliveriesRequest](#platform-v1-ListDeliveriesRequest)
    - [ListDeliveriesResponse](#platform-v1-ListDeliveriesResponse)
    - [ListWebhooksRequest](#platform-v1-ListWebhooksRequest)
    - [ListWebhooksResponse](#platform-v1-ListWebhooksResponse)
    - [RegisterWebhookRequest](#platform-v1-RegisterWebhookRequest)
    - [RegisterWebhookResponse](#platform-v1-RegisterWebhookResponse)
    - [WebhookData](#platform-v1-WebhookData)
  
    - [WebhookService](#platform-v1-WebhookService)
  
- [Scalar Value Types](#scalar-value-types)



<a name="experience_v1_experience-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## experience/v1/experience.proto



<a name="experience-v1-ActionGate"></a>

### ActionGate
ActionGate gates a single action_id behind an entitlement_key. Reserved-cheap
seam on ExperienceSpec.action_gates (field 80) -- typed up front so the frozen
proto never has to migrate to add per-action entitlement gating.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| action_id | [string](#string) |  |  |
| entitlement_key | [string](#string) |  |  |






<a name="experience-v1-AgentNode"></a>

### AgentNode
AgentNode is an agent step binding a set of tool ids under a TYPED
io_envelope_schema. The io-envelope is the SWAP-STABLE seam: the stub
dispatcher AND the real LLM dispatcher (obj 144) both read/write THIS envelope,
so the dispatcher swap is envelope-preserving with NO contract change.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tool_ids | [string](#string) | repeated | tool ids this node may call |
| io_envelope_schema | [string](#string) |  | JSON-Schema io envelope (swap-stable seam) |






<a name="experience-v1-AgentSpec"></a>

### AgentSpec
AgentSpec is the reusable, versioned agent contract. A flow&#39;s agent node
references an AgentSpec by id (wired in 160-03); the spec itself is authored
once and reused across every channel/flow.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  | stable spec id (authored in eden-biz) |
| version | [string](#string) |  | versioned contract (semver-ish, e.g. &#34;1.0.0&#34;) |
| company_id | [string](#string) |  | owning tenant scope -- OVERWRITTEN by principal at store (non-leaking) |
| persona | [string](#string) |  | system prompt / grounding persona (TYPED, not a config blob) |
| model_ref | [string](#string) |  | AOCore /v1/models catalog asset id (first-class model selection) |
| node | [AgentNode](#experience-v1-AgentNode) |  | COMPOSES the frozen 140 AgentNode (tool_ids &#43; io_envelope_schema) |
| tools | [ToolDefinition](#experience-v1-ToolDefinition) | repeated | resolved typed tool contracts (frozen 140 type) |
| knowledge | [KnowledgePolicy](#experience-v1-KnowledgePolicy) |  |  |
| hitl | [HitlPolicy](#experience-v1-HitlPolicy) |  |  |
| budget | [BudgetPolicy](#experience-v1-BudgetPolicy) |  |  |
| lifecycle | [string](#string) |  | draft|active|retired |
| audience | [AgentAudience](#experience-v1-AgentAudience) |  | 161-01 additive: the AUDIENCE dimension. One spec serves internal staff AND external customers (NOT two agents) -- per-audience tools/knowledge/persona/ escalation ride the bindings. UNSPECIFIED audience is back-compat INTERNAL (external is NEVER implicit). Machine-checked in agentspec.go: an external binding may only reference visibility=EXTERNAL_SAFE tools (deny-by-default). |
| audience_bindings | [AudienceBinding](#experience-v1-AudienceBinding) | repeated |  |






<a name="experience-v1-AppDefinition"></a>

### AppDefinition
AppDefinition: build-time definition feeding BOTH the runtime resolver AND
the per-company native build pipeline (layered output).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  |  |
| meta | [AppMeta](#experience-v1-AppMeta) |  |  |
| spec | [ExperienceSpec](#experience-v1-ExperienceSpec) |  |  |
| min_binary_version | [string](#string) |  | Min app binary version that can render specs from this definition (IRREVERSIBLE floor). |
| contract_version | [string](#string) |  | Version of the experience.v1 contract this definition conforms to. |
| deep_link | [DeepLinkSpec](#experience-v1-DeepLinkSpec) |  | 140-05 un-reserved field 10 from the 10-19 nav range for DeepLinkSpec; 11..19 stay reserved. DeepLink lives HERE (not on the spec) because a store binary commits to its url scheme &#43; route templates at submit time and can&#39;t change them post-submit -- it is a BUILD-time, not a resolved-spec, value. |
| signing | [SigningSpec](#experience-v1-SigningSpec) |  | 140-07 un-reserved field 20 from the 20-29 signing range for SigningSpec; 21..29 stay reserved. Signing lives HERE (BUILD-time, like DeepLinkSpec) -- a store binary commits to its signing identity at submit time. SigningSpec stores a per-platform CredentialRef (ref &#43; custody), NEVER inline material. |
| app_service_slots | [string](#string) | repeated | 140-07 un-reserved field 30 from the 30-39 app-service-slot range for the build-time service-slot refs (search/notify/export/attach/print/audit); 31..39 stay reserved. |






<a name="experience-v1-AppMeta"></a>

### AppMeta



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| bundle_id | [string](#string) |  |  |






<a name="experience-v1-AudienceBinding"></a>

### AudienceBinding
AudienceBinding is the per-audience override set an AgentSpec carries
(161-01): which tools/knowledge the audience may use, the persona voice, and
where the agent escalates. tool_ids reference the spec&#39;s tools by adapter_id
(binding subset-of spec tools -- validated); an EXTERNAL binding may reference
ONLY visibility=EXTERNAL_SAFE tools (deny-by-default, agentspec.go).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| audience | [AgentAudience](#experience-v1-AgentAudience) |  | which audience this binding configures |
| tool_ids | [string](#string) | repeated | subset of the spec&#39;s tools (by adapter_id) |
| knowledge_ids | [string](#string) | repeated | subset of knowledge source refs for this audience |
| persona | [string](#string) |  | audience-voice persona override (e.g. brand voice) |
| escalation_target | [string](#string) |  | where this audience escalates (e.g. human-support) |






<a name="experience-v1-BudgetPolicy"></a>

### BudgetPolicy
BudgetPolicy caps an agent run: max reasoning/tool steps and max tokens.
max_steps is validated into (0, ceiling] by ValidateAgentSpec (fail-closed:
an unset/zero budget is NOT dispatchable).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| max_steps | [int32](#int32) |  | max agent steps per run (validator: 0 &lt; n &lt;= ceiling) |
| max_tokens | [int32](#int32) |  | max tokens per run (0 = runtime default) |






<a name="experience-v1-CredentialRef"></a>

### CredentialRef
CredentialRef is a REFERENCE to signing material held in a custody system
(1Password / KMS), NEVER the material itself. ref is the pointer (e.g.
op://AOCyber/ios-dist-cert); custody names the system holding it.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| ref | [string](#string) |  | pointer into the custody system (op://..., kms://...) |
| custody | [string](#string) |  | custody system name (e.g. &#34;1password&#34;, &#34;kms&#34;) |






<a name="experience-v1-DeepLinkSpec"></a>

### DeepLinkSpec
DeepLinkSpec is the url scheme &#43; route templates a store binary commits to at
submit time. It lives on AppDefinition (BUILD-time), never on a resolved
ExperienceSpec, because a published store binary CANNOT change these
post-submit -- they are frozen the moment the binary ships.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| url_scheme | [string](#string) |  | e.g. &#34;edenbiz&#34; (edenbiz://...) |
| route_templates | [string](#string) | repeated | e.g. &#34;/customers/:id&#34;, &#34;/invoices/:id/pay&#34; |






<a name="experience-v1-ExperienceSpec"></a>

### ExperienceSpec
ExperienceSpec: server-authoritative, content-hashed, client-cacheable spec
resolved per {role,entitlements,form_factor,tenant,org}. The 3 version axes
are ORTHOGONAL; bumping one must not change another&#39;s serialized value.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| spec_schema_version | [string](#string) |  | schema version of THIS message |
| surface_contract_version | [string](#string) |  | FeatureSurface contract version |
| content_hash | [string](#string) |  | hash of resolved spec (rollback id &#43; cache key) |
| contract_version | [string](#string) |  | experience.v1 contract version (140-12 compat signal) |
| min_binary_version | [string](#string) |  | floor binary version that can render this spec |
| tenant_id | [string](#string) |  | Tenancy: RESERVED here, ENFORCED in 140-08/09 (company scope &#43; aocore org scope). |
| org_id | [string](#string) |  |  |
| referenced_surface_ids | [string](#string) | repeated | 140-03 un-reserved 10..11 from the &#34;SurfaceRegistryManifest &#43; granted surfaces&#34; range; 12..19 stay reserved for granted-surface refs.

surface ids this spec references (negotiated vs the binary&#39;s manifest) |
| unknown_surface_policy | [UnknownSurfacePolicy](#experience-v1-UnknownSurfacePolicy) |  | how the binary handles a referenced surface it does not know |
| nav_graph | [NavGraph](#experience-v1-NavGraph) |  | 140-05 un-reserved field 20 from the 20-29 nav range for the NavGraph; 21..29 stay reserved for later nav seams. |
| theme | [ThemeSpec](#experience-v1-ThemeSpec) |  | 140-06 un-reserved 30/31/32 from the 30-39 presentation range for the typed presentation surface (ThemeSpec &#43; TermSet &#43; LocaleSpec); 33..39 stay reserved. LocaleSpec.timezone is the net-new LOAD-BEARING field (absent everywhere today, required for scheduling/field). TermSet is PRESENTATION- ONLY -- a Job-&gt;Visit relabel for display; logic keys off entity/surface ids, never the displayed term.

brand preset &#43; logo &#43; color overrides &#43; density |
| terms | [TermSet](#experience-v1-TermSet) |  | presentation-only term overrides (e.g. job-&gt;visit) |
| locale | [LocaleSpec](#experience-v1-LocaleSpec) |  | locale &#43; currency &#43; IANA timezone (tz load-bearing) |
| surface_offline | [ExperienceSpec.SurfaceOfflineEntry](#experience-v1-ExperienceSpec-SurfaceOfflineEntry) | repeated | 140-06 un-reserved field 40 from the 40-49 offline range for the PER-SURFACE OfflineSpec map (keyed by surface_id); 41..49 stay reserved. Offline is per-surface and STRUCTURED (policy/cache_ttl/conflict_policy/grace), NEVER a bare global offlineCapable bool -- gates G1/G5 reason over the structure.

per-surface offline policy, keyed by surface_id |
| bindings | [ServiceTransportBinding](#experience-v1-ServiceTransportBinding) | repeated | 140-04 un-reserved field 50 from the binding range; 51..59 stay reserved.

service&lt;-&gt;transport bindings (transport- AND scope-agnostic) |
| tools | [ToolDefinition](#experience-v1-ToolDefinition) | repeated | 140-07 un-reserved 60/61 from the 60-69 tool/agent range; 62..69 stay reserved. ToolDefinition is TYPED (adapter allowlist FK &#43; JSON-Schema envelopes &#43; side_effect &#43; idempotency_key), NEVER a config_json blob. AgentNode carries a typed io_envelope_schema -- the swap-stable seam the real LLM dispatcher (obj 144) plugs into without a contract change.

curated-allowlist-bound tools |
| agent_nodes | [AgentNode](#experience-v1-AgentNode) | repeated | agent nodes (io-envelope-preserving) |
| resolution_context | [ResolutionContext](#experience-v1-ResolutionContext) |  | 140-07 un-reserved 70/71 from the 70-79 telemetry/resolution range; 72..79 stay reserved. ResolutionContext is the provenance of THIS resolution (tenant/org &#43; resolved_at &#43; resolver_version); LockedSurface carries an upsell_reason for a surface gated behind an entitlement. The TelemetryEnvelope message itself is a top-level message (emitted on the runtime wire, not a resolved-spec field) -- only resolution_context &#43; locked_surfaces live here.

provenance of this resolution |
| locked_surfaces | [LockedSurface](#experience-v1-LockedSurface) | repeated | surfaces gated behind an upsell |
| action_gates | [ActionGate](#experience-v1-ActionGate) | repeated | 140-07 un-reserved 80..86 from the 80-89 reserved-cheap range; 87..89 stay reserved. These are reserved-NOW-cheap seams: typed-but-empty-friendly fields a frozen-forever proto must reserve up front rather than migrate to add. server_killable (86) is the REQUIRED fast-rollback kill flag for the generated fleet (a customer-own store account gives no kill switch otherwise).

per-action entitlement gates |
| flag_overrides | [ExperienceSpec.FlagOverridesEntry](#experience-v1-ExperienceSpec-FlagOverridesEntry) | repeated | feature-flag overrides |
| variant | [string](#string) |  | A/B or cohort variant |
| declared_states | [string](#string) | repeated | render states (populated/empty/error/...) |
| custom_fields | [ExperienceSpec.CustomFieldsEntry](#experience-v1-ExperienceSpec-CustomFieldsEntry) | repeated | vertical/builder custom fields |
| rule_policy | [ExperienceSpec.RulePolicyEntry](#experience-v1-ExperienceSpec-RulePolicyEntry) | repeated | rule-policy hints (e.g. refund -&gt; approval) |
| server_killable | [bool](#bool) |  | REQUIRED fast-rollback kill flag |






<a name="experience-v1-ExperienceSpec-CustomFieldsEntry"></a>

### ExperienceSpec.CustomFieldsEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |






<a name="experience-v1-ExperienceSpec-FlagOverridesEntry"></a>

### ExperienceSpec.FlagOverridesEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |






<a name="experience-v1-ExperienceSpec-RulePolicyEntry"></a>

### ExperienceSpec.RulePolicyEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |






<a name="experience-v1-ExperienceSpec-SurfaceOfflineEntry"></a>

### ExperienceSpec.SurfaceOfflineEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [OfflineSpec](#experience-v1-OfflineSpec) |  |  |






<a name="experience-v1-HitlPolicy"></a>

### HitlPolicy
HitlPolicy is the human-in-the-loop escalation contract: escalate on low
confidence (below confidence_floor) and on any WRITE tool call beyond the
allowlisted set (escalate_on_write_beyond names the writes the agent MAY
perform autonomously; every other write escalates to a human).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| escalate_on_low_confidence | [bool](#bool) |  | escalate when confidence &lt; floor |
| confidence_floor | [double](#double) |  | [0,1] confidence threshold |
| escalate_on_write_beyond | [string](#string) | repeated | writes allowed WITHOUT escalation |






<a name="experience-v1-KnowledgePolicy"></a>

### KnowledgePolicy
KnowledgePolicy grounds the agent: which knowledge sources it may retrieve
from (source_refs) and whether it may ONLY answer from retrieved grounding
(grounded_only = the helpdesk &#34;answer from KB or escalate&#34; posture).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| source_refs | [string](#string) | repeated | knowledge source refs (e.g. kb://helpdesk/articles) |
| grounded_only | [bool](#bool) |  | true = answer only from retrieved grounding |






<a name="experience-v1-LocaleSpec"></a>

### LocaleSpec
LocaleSpec is the per-resolution locale surface. timezone is the net-new
LOAD-BEARING field: it is absent everywhere in the product today and is
required to resolve local appointment times on scheduling/field surfaces.
Modeled as an IANA tz string (e.g. &#34;America/New_York&#34;). currency is a field
here -- it is NEVER hardcoded to USD at the contract level.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| locale | [string](#string) |  | BCP-47 locale (e.g. &#34;en-US&#34;) |
| currency | [string](#string) |  | ISO-4217 currency (e.g. &#34;USD&#34;) -- NOT hardcoded |
| timezone | [string](#string) |  | IANA timezone (e.g. &#34;America/New_York&#34;) -- load-bearing for scheduling |






<a name="experience-v1-LockedSurface"></a>

### LockedSurface
LockedSurface is a surface gated behind an entitlement upsell: the surface_id
the requesting scope is NOT entitled to &#43; the upsell_reason shown to the user.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| surface_id | [string](#string) |  |  |
| upsell_reason | [string](#string) |  |  |






<a name="experience-v1-NavEdge"></a>

### NavEdge
NavEdge is a TYPED transition from one surface to another carrying the
selection passed across the hop. param_bindings maps a target input name to a
source selection expression (e.g. {&#34;customerId&#34;: &#34;$selection.id&#34;}); trigger
names the gesture/event that fires the edge (e.g. &#34;onSelect&#34;). This typed
param-passing is what makes the graph compose FLOWS, not a launcher.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| from_surface_id | [string](#string) |  | source surface |
| to_surface_id | [string](#string) |  | target surface |
| param_bindings | [NavEdge.ParamBindingsEntry](#experience-v1-NavEdge-ParamBindingsEntry) | repeated | target input &lt;- source selection expr |
| trigger | [string](#string) |  | gesture/event that fires the edge |






<a name="experience-v1-NavEdge-ParamBindingsEntry"></a>

### NavEdge.ParamBindingsEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |






<a name="experience-v1-NavGraph"></a>

### NavGraph
NavGraph is the typed navigation graph for a resolved ExperienceSpec.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| landing_surface_id | [string](#string) |  | the surface shown first (must be a slot) |
| slots | [NavSlot](#experience-v1-NavSlot) | repeated | placed surfaces (the nav chrome) |
| edges | [NavEdge](#experience-v1-NavEdge) | repeated | typed inter-surface transitions |






<a name="experience-v1-NavSlot"></a>

### NavSlot
NavSlot places one surface in the navigation at a Placement &#43; order.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| surface_id | [string](#string) |  | the FeatureSurface this slot shows |
| placement | [Placement](#experience-v1-Placement) |  | primary / more / detail-only |
| order | [int32](#int32) |  | sort order within the placement |






<a name="experience-v1-OfflineSpec"></a>

### OfflineSpec
OfflineSpec is the STRUCTURED per-surface offline policy -- NOT a bare bool. A
bare offlineCapable bool cannot express a conflict strategy or a grace window;
gates G1/G5 reason over these four typed fields. Attached PER-SURFACE via
ExperienceSpec.surface_offline (keyed by surface_id), never one global flag.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| policy | [OfflinePolicy](#experience-v1-OfflinePolicy) |  | offline behavior for this surface |
| cache_ttl_seconds | [uint32](#uint32) |  | how long cached reads stay valid offline |
| conflict_policy | [ConflictPolicy](#experience-v1-ConflictPolicy) |  | how a queued write reconciles at sync |
| read_only_grace_seconds | [uint32](#uint32) |  | grace window the surface stays read-only after going offline |






<a name="experience-v1-ResolutionContext"></a>

### ResolutionContext
ResolutionContext is the provenance of a resolved ExperienceSpec: which
tenant/org it was resolved for, when, and by which resolver version. Lives on
ExperienceSpec.resolution_context (field 70).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| org_id | [string](#string) |  |  |
| resolved_at | [string](#string) |  | RFC3339 resolution timestamp |
| resolver_version | [string](#string) |  | resolver build/version that produced the spec |






<a name="experience-v1-ResolveSpecRequest"></a>

### ResolveSpecRequest
ResolveSpecRequest asks the server to resolve a stored spec for the
principal&#39;s scope, role, and form_factor. The tuple&#39;s tenant/org come from the
authenticated principal -- NOT from this message. role &#43; form_factor are the
non-authority resolution axes the caller legitimately supplies.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| app_def_id | [string](#string) |  |  |
| role | [string](#string) |  |  |
| form_factor | [string](#string) |  |  |






<a name="experience-v1-ResolveSpecResponse"></a>

### ResolveSpecResponse
ResolveSpecResponse carries the filtered &#43; content-hashed ResolvedSpec (only
the surfaces the principal&#39;s entitlement set GRANTS; ungranted surfaces become
locked_surfaces on the spec).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| resolved_spec | [ExperienceSpec](#experience-v1-ExperienceSpec) |  |  |






<a name="experience-v1-ServiceTransportBinding"></a>

### ServiceTransportBinding
ServiceTransportBinding binds one entity&#39;s service to a transport &#43; scope.
It is the anchor every later message references for identity &#43; scope. It is
transport- AND scope-agnostic so the SAME ExperienceSpec can drive eden-biz
(CONNECT/COMPANY) and aocore (REST_OPENAPI/ORG) through one Repository
abstraction (the runtime two-transport proof lands in 140-11).

NO authority field (company_id/org_id) lives here that a REST handler could
bind from the request BODY -- scope_authority selects WHICH scope the
verified AOID identity projects to; the scope VALUE is resolved at runtime
from the identity context, never the body (binding.ResolveScope).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| entity | [string](#string) |  | logical entity (e.g. &#34;Invoice&#34;, &#34;Tenant&#34;) |
| service_package | [string](#string) |  | backend service package / namespace |
| service_name | [string](#string) |  | backend service name |
| operations | [Operation](#experience-v1-Operation) | repeated | reads AND writes this binding exposes |
| transport_kind | [TransportKind](#experience-v1-TransportKind) |  | CONNECT (biz) | REST_OPENAPI (aocore) |
| scope_authority | [ScopeAuthority](#experience-v1-ScopeAuthority) |  | COMPANY (biz) | ORG (aocore) projection target |
| pagination | [PaginationKind](#experience-v1-PaginationKind) |  | LIST pagination contract |
| repo_interface_id | [string](#string) |  | Repository abstraction id (140-11 two-transport proof) |






<a name="experience-v1-SigningSpec"></a>

### SigningSpec
SigningSpec is the per-platform signing identity, keyed by platform
(ios|android). It carries CredentialRefs (ref &#43; custody) for BOTH platforms,
NEVER inline cert/key bytes. Lives on AppDefinition (BUILD-time) -- a store
binary commits to its signing identity at submit time.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| by_platform | [SigningSpec.ByPlatformEntry](#experience-v1-SigningSpec-ByPlatformEntry) | repeated | platform -&gt; credential reference |






<a name="experience-v1-SigningSpec-ByPlatformEntry"></a>

### SigningSpec.ByPlatformEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [CredentialRef](#experience-v1-CredentialRef) |  |  |






<a name="experience-v1-StoreSpecRequest"></a>

### StoreSpecRequest
StoreSpecRequest stores an AppDefinition (carrying its ExperienceSpec) under
the AUTHENTICATED principal&#39;s scope. The app_definition&#39;s tenant_id/org_id (on
its nested spec) are OVERWRITTEN by the principal scope server-side -- a body
value cannot plant a spec under another tenant.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| app_definition | [AppDefinition](#experience-v1-AppDefinition) |  |  |






<a name="experience-v1-StoreSpecResponse"></a>

### StoreSpecResponse
StoreSpecResponse returns the stored spec&#39;s identity &#43; content hash/version so
the caller can address it later (ResolveSpec / ValidateSpec by app_def_id).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| app_def_id | [string](#string) |  |  |
| content_hash | [string](#string) |  |  |
| spec_schema_version | [string](#string) |  |  |






<a name="experience-v1-SurfaceRegistryManifest"></a>

### SurfaceRegistryManifest
SurfaceRegistryManifest: the artifact a BINARY compiles in and negotiates a
resolved ExperienceSpec against. It is NOT served on the spec — it is the
binary&#39;s own statement of which surfaces it can render &#43; which
surface_contract_version it speaks. version_negotiation reads
known_surface_ids to decide ignore/block/degrade for unknown surfaces.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| contract_version | [string](#string) |  | surface contract version the binary speaks |
| known_surface_ids | [string](#string) | repeated | surfaces this binary can render |






<a name="experience-v1-TelemetryEnvelope"></a>

### TelemetryEnvelope
TelemetryEnvelope carries every observability ID in-schema (NOT a free blob).
Emitted on the runtime telemetry wire. tenant_id is the REQUESTING tenant only
-- it is never an other-tenant id (no cross-tenant telemetry bleed).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| spec_id | [string](#string) |  |  |
| spec_version | [string](#string) |  |  |
| surface_id | [string](#string) |  |  |
| binding_id | [string](#string) |  |  |
| entitlement_set_hash | [string](#string) |  |  |
| brand | [string](#string) |  |  |
| theme_profile | [string](#string) |  |  |
| form_factor | [string](#string) |  |  |
| build_sha | [string](#string) |  |  |
| compliance_profile | [string](#string) |  |  |
| tenant_id | [string](#string) |  | REQUESTING tenant only -- never another tenant&#39;s id |






<a name="experience-v1-TermSet"></a>

### TermSet
TermSet is the PRESENTATION-ONLY term-override map (e.g. {&#34;job&#34;: &#34;visit&#34;}).
IMPORTANT: it is a DISPLAY relabel only -- it MUST NEVER be load-bearing for
logic. The runtime keys off the logical entity/surface id (the map KEY), and
the value is purely the label shown to the user. The TermResolver (build-spec
section 9) routes every displayed string through this map; no branch ever
keys off the resolved label.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| overrides | [TermSet.OverridesEntry](#experience-v1-TermSet-OverridesEntry) | repeated | logical term -&gt; displayed label (presentation-only) |






<a name="experience-v1-TermSet-OverridesEntry"></a>

### TermSet.OverridesEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |






<a name="experience-v1-ThemeSpec"></a>

### ThemeSpec
ThemeSpec is the per-company brand surface: a named brand_preset, a logo_ref,
an arbitrary color-override map (token -&gt; hex, no fixed schema so a builder can
override any subset of brand tokens), and a density token.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| brand_preset | [string](#string) |  | named brand preset the build starts from |
| logo_ref | [string](#string) |  | asset ref for the company logo |
| color_overrides | [ThemeSpec.ColorOverridesEntry](#experience-v1-ThemeSpec-ColorOverridesEntry) | repeated | brand token -&gt; value (e.g. &#34;primary&#34; -&gt; &#34;#0A84FF&#34;) |
| density | [string](#string) |  | density token (e.g. &#34;comfortable&#34; | &#34;compact&#34;) |






<a name="experience-v1-ThemeSpec-ColorOverridesEntry"></a>

### ThemeSpec.ColorOverridesEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |






<a name="experience-v1-ToolDefinition"></a>

### ToolDefinition
ToolDefinition is the TYPED tool contract. adapter_id is a curated-allowlist FK
(NO arbitrary binding). input_schema/output_schema are JSON-Schema STRINGS (a
typed envelope), NOT a free config_json blob. side_effect gates the dispatcher.
idempotency_key is load-bearing for WRITE/EXTERNAL replays.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| adapter_id | [string](#string) |  | curated-allowlist FK (no arbitrary RPC/SQL) |
| input_schema | [string](#string) |  | JSON-Schema string (typed envelope, not a blob) |
| output_schema | [string](#string) |  | JSON-Schema string (typed envelope, not a blob) |
| side_effect | [SideEffect](#experience-v1-SideEffect) |  | dispatcher gate (read/write/external) |
| idempotency_key | [string](#string) |  | replay key (load-bearing for write/external) |
| visibility | [ToolVisibility](#experience-v1-ToolVisibility) |  | 161-01 additive: audience exposure class. UNSPECIFIED is fail-closed -- interpreted internal_only; a tool is NEVER implicitly external-safe. |






<a name="experience-v1-ValidateSpecRequest"></a>

### ValidateSpecRequest
ValidateSpecRequest validates an AppDefinition&#39;s spec for coherence (140-05
nav rules) &#43; version conformance (140-03) under the principal&#39;s scope. The
spec&#39;s scope is overwritten by the principal scope before validation.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| app_definition | [AppDefinition](#experience-v1-AppDefinition) |  |  |






<a name="experience-v1-ValidateSpecResponse"></a>

### ValidateSpecResponse
ValidateSpecResponse returns valid &#43; the accumulated problems (empty when
coherent -- the validator never short-circuits, so the builder sees every
problem at once).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| valid | [bool](#bool) |  |  |
| problems | [ValidationProblem](#experience-v1-ValidationProblem) | repeated |  |






<a name="experience-v1-ValidationProblem"></a>

### ValidationProblem
ValidationProblem is one machine-checked coherence/version violation. code is
a stable token (e.g. nav.too_many_primary) so clients branch without string
matching; surface_id names the offending surface when surface-scoped.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| code | [string](#string) |  |  |
| surface_id | [string](#string) |  |  |
| message | [string](#string) |  |  |





 


<a name="experience-v1-AgentAudience"></a>

### AgentAudience
AgentAudience declares WHO an agent serves (161-01). UNSPECIFIED=0 is the
back-compat value: a 160-era spec with no audience field means INTERNAL --
external exposure is always an explicit authoring decision, never implicit.

| Name | Number | Description |
| ---- | ------ | ----------- |
| AGENT_AUDIENCE_UNSPECIFIED | 0 | back-compat: treated as INTERNAL (external is NEVER implicit) |
| AGENT_AUDIENCE_INTERNAL | 1 | staff assist (company-wide data scope) |
| AGENT_AUDIENCE_EXTERNAL | 2 | customer-facing (customer-scoped tools only) |
| AGENT_AUDIENCE_BOTH | 3 | one spec, both audiences via bindings |



<a name="experience-v1-ConflictPolicy"></a>

### ConflictPolicy
ConflictPolicy is how a queued offline write reconciles against server state
at sync time. UNSPECIFIED=0 fails SAFE (treated as MANUAL_RECONCILE -- never
silently drop a conflicting write). LAST_WRITE_WINS = the later timestamp
wins. SERVER_WINS = the server copy wins, the queued write is discarded.
MANUAL_RECONCILE = surface the conflict to the user.

| Name | Number | Description |
| ---- | ------ | ----------- |
| CONFLICT_POLICY_UNSPECIFIED | 0 | fail-safe -&gt; treated as MANUAL_RECONCILE |
| CONFLICT_POLICY_LAST_WRITE_WINS | 1 | later write wins |
| CONFLICT_POLICY_SERVER_WINS | 2 | server copy wins, queued write discarded |
| CONFLICT_POLICY_MANUAL_RECONCILE | 3 | surface the conflict to the user |



<a name="experience-v1-OfflinePolicy"></a>

### OfflinePolicy
OfflinePolicy is the per-surface offline behavior. UNSPECIFIED=0 fails SAFE
(treated as NONE -- no offline). NONE = online-only. READ_CACHE = serve cached
reads offline, no writes. READ_WRITE_QUEUE = cache reads AND queue writes for
later sync (the field-service offline-first mode).

| Name | Number | Description |
| ---- | ------ | ----------- |
| OFFLINE_POLICY_UNSPECIFIED | 0 | fail-safe -&gt; treated as NONE |
| OFFLINE_POLICY_NONE | 1 | online-only, no offline behavior |
| OFFLINE_POLICY_READ_CACHE | 2 | serve cached reads offline; no writes |
| OFFLINE_POLICY_READ_WRITE_QUEUE | 3 | cache reads &#43; queue writes for later sync |



<a name="experience-v1-Operation"></a>

### Operation
Operation is a single read or write the binding exposes. The binding is NOT
read-only: CREATE/UPDATE/DELETE are first-class alongside GET/LIST.

| Name | Number | Description |
| ---- | ------ | ----------- |
| OPERATION_UNSPECIFIED | 0 |  |
| OPERATION_GET | 1 | read one |
| OPERATION_LIST | 2 | read many |
| OPERATION_CREATE | 3 | write (create) |
| OPERATION_UPDATE | 4 | write (update) |
| OPERATION_DELETE | 5 | write (delete) |



<a name="experience-v1-PaginationKind"></a>

### PaginationKind
PaginationKind is the pagination contract a LIST operation honors, independent
of transport (a CONNECT and a REST_OPENAPI binding can each use any kind).

| Name | Number | Description |
| ---- | ------ | ----------- |
| PAGINATION_KIND_UNSPECIFIED | 0 |  |
| PAGINATION_KIND_NONE | 1 | no pagination |
| PAGINATION_KIND_CURSOR | 2 | opaque page_token cursor |
| PAGINATION_KIND_OFFSET | 3 | numeric offset/limit |



<a name="experience-v1-Placement"></a>

### Placement
Placement is where a NavSlot&#39;s surface sits in the navigation chrome.
UNSPECIFIED=0 is fail-safe (an unplaced slot is treated as not-shown rather
than silently promoted into primary nav).

| Name | Number | Description |
| ---- | ------ | ----------- |
| PLACEMENT_UNSPECIFIED | 0 | unset -- not shown in primary/overflow nav |
| PLACEMENT_PRIMARY | 1 | primary navigation (the &lt;=5-slot rail/tab bar) |
| PLACEMENT_MORE | 2 | overflow / &#34;More&#34; menu |
| PLACEMENT_DETAIL_ONLY | 3 | reachable only via an edge, never in the chrome |



<a name="experience-v1-ScopeAuthority"></a>

### ScopeAuthority
ScopeAuthority is how a single AOID identity projects to a backend&#39;s tenant
scope. COMPANY = eden-biz company scope; ORG = aocore org scope. UNSPECIFIED
is fail-closed (not bindable). This enum is the proto-level mapping of the
one identity to each backend -- the projection logic is binding.ResolveScope.

| Name | Number | Description |
| ---- | ------ | ----------- |
| SCOPE_AUTHORITY_UNSPECIFIED | 0 | reserved / fail-closed |
| SCOPE_AUTHORITY_COMPANY | 1 | eden-biz company scope |
| SCOPE_AUTHORITY_ORG | 2 | aocore org scope |



<a name="experience-v1-SideEffect"></a>

### SideEffect
SideEffect gates the tool dispatcher. UNSPECIFIED=0 is fail-closed (an
unclassified tool is not dispatchable). READ = pure read. WRITE = mutates
tenant data (idempotency_key load-bearing). EXTERNAL = outbound (webhooks etc.)
-- representable but DEFERRED (out of scope; ValidateTooling warn-flags it).

| Name | Number | Description |
| ---- | ------ | ----------- |
| SIDE_EFFECT_UNSPECIFIED | 0 | fail-closed -- not dispatchable |
| SIDE_EFFECT_READ | 1 | pure read |
| SIDE_EFFECT_WRITE | 2 | mutates tenant data (idempotency_key matters) |
| SIDE_EFFECT_EXTERNAL | 3 | outbound (webhooks) -- DEFERRED, representable |



<a name="experience-v1-ToolVisibility"></a>

### ToolVisibility
ToolVisibility classifies a tool&#39;s audience exposure (161-01). UNSPECIFIED=0
is fail-closed: an unclassified tool is INTERNAL_ONLY -- external exposure is
always an explicit authoring decision, never a default. EXTERNAL_SAFE means
the adapter is customer-scoped (filters by the requesting customer identity,
not just company) and may be bound on an external-audience agent.

| Name | Number | Description |
| ---- | ------ | ----------- |
| TOOL_VISIBILITY_UNSPECIFIED | 0 | fail-closed -- internal_only |
| TOOL_VISIBILITY_INTERNAL_ONLY | 1 | staff-facing only (company-scoped) |
| TOOL_VISIBILITY_EXTERNAL_SAFE | 2 | customer-scoped; bindable on external audiences |



<a name="experience-v1-TransportKind"></a>

### TransportKind
TransportKind is the wire protocol a binding speaks. Room is reserved for
future transports (e.g. GraphQL) by keeping UNSPECIFIED=0 fail-closed and
numbering known transports sparsely-safe.

| Name | Number | Description |
| ---- | ------ | ----------- |
| TRANSPORT_KIND_UNSPECIFIED | 0 | reserved / not-yet-bindable (forward-compat) |
| TRANSPORT_KIND_CONNECT | 1 | eden-biz Connect RPC |
| TRANSPORT_KIND_REST_OPENAPI | 2 | aocore REST / OpenAPI |



<a name="experience-v1-UnknownSurfacePolicy"></a>

### UnknownSurfacePolicy
UnknownSurfacePolicy: how a running binary handles a resolved spec that
references a FeatureSurface it does NOT know (i.e. not in the binary&#39;s
compiled-in SurfaceRegistryManifest.known_surface_ids). IRREVERSIBLE: a v1
spec carries this, and an old binary&#39;s behavior on a newer spec is frozen
the moment a device caches that spec. UNSPECIFIED fails SAFE (block) so an
unset policy can never silently render a surface the binary cannot handle.

| Name | Number | Description |
| ---- | ------ | ----------- |
| UNKNOWN_SURFACE_POLICY_UNSPECIFIED | 0 | fail-safe -&gt; treated as BLOCK_UPGRADE |
| UNKNOWN_SURFACE_POLICY_IGNORE | 1 | drop the unknown surface, render the rest |
| UNKNOWN_SURFACE_POLICY_BLOCK_UPGRADE | 2 | block &#43; prompt the user to upgrade the binary |
| UNKNOWN_SURFACE_POLICY_RENDER_DEGRADED | 3 | render the unknown surface with a degraded marker |


 

 


<a name="experience-v1-ExperienceService"></a>

### ExperienceService
ExperienceService is the M0 server surface every consumer calls: store a spec,
resolve it per principal scope, validate it. Tenancy is enforced at the
service chokepoint (principal-derived scope), never from a request body.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| StoreSpec | [StoreSpecRequest](#experience-v1-StoreSpecRequest) | [StoreSpecResponse](#experience-v1-StoreSpecResponse) |  |
| ResolveSpec | [ResolveSpecRequest](#experience-v1-ResolveSpecRequest) | [ResolveSpecResponse](#experience-v1-ResolveSpecResponse) |  |
| ValidateSpec | [ValidateSpecRequest](#experience-v1-ValidateSpecRequest) | [ValidateSpecResponse](#experience-v1-ValidateSpecResponse) |  |

 



<a name="platform_v1_ac2_evidence-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## platform/v1/ac2_evidence.proto



<a name="platform-v1-AC2EvidenceServiceGetAccountInventoryRequest"></a>

### AC2EvidenceServiceGetAccountInventoryRequest
AC2EvidenceServiceGetAccountInventoryRequest fetches a page of the
account inventory. `as_of` enables point-in-time evidence.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| as_of | [google.protobuf.Timestamp](#google-protobuf-Timestamp) | optional |  |
| page_size | [int32](#int32) |  |  |
| page_token | [string](#string) |  |  |
| kind_filter | [string](#string) | optional | kind_filter narrows the report. Empty = all kinds. |






<a name="platform-v1-AC2EvidenceServiceGetAccountInventoryResponse"></a>

### AC2EvidenceServiceGetAccountInventoryResponse
AC2EvidenceServiceGetAccountInventoryResponse returns a page of inventory rows.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| rows | [AccountInventoryRow](#platform-v1-AccountInventoryRow) | repeated |  |
| next_page_token | [string](#string) |  |  |






<a name="platform-v1-AC2EvidenceServiceGetDormantAccountsRequest"></a>

### AC2EvidenceServiceGetDormantAccountsRequest
AC2EvidenceServiceGetDormantAccountsRequest fetches a page of dormant
accounts. `dormancy_threshold_days` overrides tenant policy for ad-hoc
reports.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| dormancy_threshold_days | [int32](#int32) | optional |  |
| page_size | [int32](#int32) |  |  |
| page_token | [string](#string) |  |  |






<a name="platform-v1-AC2EvidenceServiceGetDormantAccountsResponse"></a>

### AC2EvidenceServiceGetDormantAccountsResponse
AC2EvidenceServiceGetDormantAccountsResponse returns a page of dormant
account rows.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| rows | [DormantAccountRow](#platform-v1-DormantAccountRow) | repeated |  |
| next_page_token | [string](#string) |  |  |






<a name="platform-v1-AC2EvidenceServiceGetLifecycleDecisionsRequest"></a>

### AC2EvidenceServiceGetLifecycleDecisionsRequest
AC2EvidenceServiceGetLifecycleDecisionsRequest fetches a page of audit
rows whose `action` matches one of `action_prefix` over [start_at, end_at].


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| start_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) | optional |  |
| end_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) | optional |  |
| action_prefix | [string](#string) | repeated | Filter by action prefix(es), e.g. &#34;identity.account.&#34; or &#34;identity.recertification.&#34;. Empty = all actions. |
| page_size | [int32](#int32) |  |  |
| page_token | [string](#string) |  |  |






<a name="platform-v1-AC2EvidenceServiceGetLifecycleDecisionsResponse"></a>

### AC2EvidenceServiceGetLifecycleDecisionsResponse
AC2EvidenceServiceGetLifecycleDecisionsResponse returns a page of lifecycle
decision rows.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| rows | [LifecycleDecisionRow](#platform-v1-LifecycleDecisionRow) | repeated |  |
| next_page_token | [string](#string) |  |  |






<a name="platform-v1-AC2EvidenceServiceGetRecertificationStatusRequest"></a>

### AC2EvidenceServiceGetRecertificationStatusRequest
AC2EvidenceServiceGetRecertificationStatusRequest fetches a page of
recertification posture. `only_overdue=true` returns just past-due
accounts.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| campaign_id | [string](#string) | optional |  |
| only_overdue | [bool](#bool) |  |  |
| page_size | [int32](#int32) |  |  |
| page_token | [string](#string) |  |  |






<a name="platform-v1-AC2EvidenceServiceGetRecertificationStatusResponse"></a>

### AC2EvidenceServiceGetRecertificationStatusResponse
AC2EvidenceServiceGetRecertificationStatusResponse returns a page of
recertification posture rows.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| rows | [RecertificationStatusRow](#platform-v1-RecertificationStatusRow) | repeated |  |
| next_page_token | [string](#string) |  |  |






<a name="platform-v1-AC2EvidenceServiceGetRoleBindingsRequest"></a>

### AC2EvidenceServiceGetRoleBindingsRequest
AC2EvidenceServiceGetRoleBindingsRequest fetches a page of role bindings.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| as_of | [google.protobuf.Timestamp](#google-protobuf-Timestamp) | optional |  |
| page_size | [int32](#int32) |  |  |
| page_token | [string](#string) |  |  |






<a name="platform-v1-AC2EvidenceServiceGetRoleBindingsResponse"></a>

### AC2EvidenceServiceGetRoleBindingsResponse
AC2EvidenceServiceGetRoleBindingsResponse returns a page of binding rows.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| rows | [RoleBindingRow](#platform-v1-RoleBindingRow) | repeated |  |
| next_page_token | [string](#string) |  |  |






<a name="platform-v1-AccountInventoryRow"></a>

### AccountInventoryRow
AccountInventoryRow is one row of the account inventory (AC-2(a)/(b)).
`kind` values: &#34;human&#34; | &#34;service&#34; | &#34;workload&#34; | &#34;admin&#34;.
`owner_account_id` is set for service accounts and empty for humans.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| account_id | [string](#string) |  |  |
| tenant_id | [string](#string) |  |  |
| email | [string](#string) |  |  |
| display_name | [string](#string) |  |  |
| kind | [string](#string) |  |  |
| status | [string](#string) |  |  |
| created_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| last_login_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| expires_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| owner_account_id | [string](#string) |  |  |






<a name="platform-v1-DormantAccountRow"></a>

### DormantAccountRow
DormantAccountRow is one row of the dormant-account report (AC-2(3)).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| account_id | [string](#string) |  |  |
| tenant_id | [string](#string) |  |  |
| email | [string](#string) |  |  |
| last_login_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| days_dormant | [int32](#int32) |  |  |
| will_be_suspended | [bool](#bool) |  |  |
| warning_sent_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |






<a name="platform-v1-LifecycleDecisionRow"></a>

### LifecycleDecisionRow
LifecycleDecisionRow is one row of the lifecycle decision log
(AC-2(d)/(e)/(f)). Derived from the signed audit buffer; `event_id` is
the jti from the JWS payload.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| event_id | [string](#string) |  |  |
| emitted_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| action | [string](#string) |  |  |
| actor_id | [string](#string) |  |  |
| actor_kind | [string](#string) |  |  |
| subject_id | [string](#string) |  |  |
| decision | [string](#string) |  |  |
| reason | [string](#string) |  |  |
| details_json | [string](#string) |  |  |






<a name="platform-v1-RecertificationStatusRow"></a>

### RecertificationStatusRow
RecertificationStatusRow is one row of the per-account recertification
posture (AC-2(j)). `last_decision` values: &#34;approved&#34; | &#34;revoked&#34; |
&#34;deferred&#34; | &#34;&#34; (never reviewed).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| account_id | [string](#string) |  |  |
| tenant_id | [string](#string) |  |  |
| campaign_id | [string](#string) |  |  |
| campaign_name | [string](#string) |  |  |
| last_reviewed_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| last_decision | [string](#string) |  |  |
| next_due_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| days_overdue | [int32](#int32) |  |  |






<a name="platform-v1-RoleBindingRow"></a>

### RoleBindingRow
RoleBindingRow is one row of the role-binding inventory (AC-2(j)/(k)).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| binding_id | [string](#string) |  |  |
| tenant_id | [string](#string) |  |  |
| account_id | [string](#string) |  |  |
| role_id | [string](#string) |  |  |
| role_name | [string](#string) |  |  |
| bound_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| bound_by_actor | [string](#string) |  |  |





 

 

 


<a name="platform-v1-AC2EvidenceService"></a>

### AC2EvidenceService
AC2EvidenceService produces NIST SP 800-53 AC-2 evidence reports for an
admin&#39;s tenant scope. All 5 RPCs are tenant-scoped (admin session
interceptor) and emit a self-audit event (identity.ac2_evidence.read) on
every call.

Reports are computed on demand from the live aoid.* tables &#43; aoid.audit_buffer.
`as_of` enables point-in-time evidence (uses created_at &lt;= as_of clause).

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| GetAccountInventory | [AC2EvidenceServiceGetAccountInventoryRequest](#platform-v1-AC2EvidenceServiceGetAccountInventoryRequest) | [AC2EvidenceServiceGetAccountInventoryResponse](#platform-v1-AC2EvidenceServiceGetAccountInventoryResponse) | GetAccountInventory returns the account inventory for the tenant (AC-2(a)/(b) evidence). Optional `kind_filter` narrows to human / service / workload / admin. |
| GetRoleBindings | [AC2EvidenceServiceGetRoleBindingsRequest](#platform-v1-AC2EvidenceServiceGetRoleBindingsRequest) | [AC2EvidenceServiceGetRoleBindingsResponse](#platform-v1-AC2EvidenceServiceGetRoleBindingsResponse) | GetRoleBindings returns the role-binding inventory for the tenant (AC-2(j)/(k) evidence). |
| GetRecertificationStatus | [AC2EvidenceServiceGetRecertificationStatusRequest](#platform-v1-AC2EvidenceServiceGetRecertificationStatusRequest) | [AC2EvidenceServiceGetRecertificationStatusResponse](#platform-v1-AC2EvidenceServiceGetRecertificationStatusResponse) | GetRecertificationStatus returns the recertification posture per account (AC-2(j) evidence): when last reviewed, by whom, next due, days overdue. |
| GetDormantAccounts | [AC2EvidenceServiceGetDormantAccountsRequest](#platform-v1-AC2EvidenceServiceGetDormantAccountsRequest) | [AC2EvidenceServiceGetDormantAccountsResponse](#platform-v1-AC2EvidenceServiceGetDormantAccountsResponse) | GetDormantAccounts returns accounts past their dormancy threshold (AC-2(3) evidence). `dormancy_threshold_days` overrides the tenant policy threshold for ad-hoc reports. |
| GetLifecycleDecisions | [AC2EvidenceServiceGetLifecycleDecisionsRequest](#platform-v1-AC2EvidenceServiceGetLifecycleDecisionsRequest) | [AC2EvidenceServiceGetLifecycleDecisionsResponse](#platform-v1-AC2EvidenceServiceGetLifecycleDecisionsResponse) | GetLifecycleDecisions returns the audit-derived lifecycle decision log (AC-2(d)/(e)/(f) evidence). Filters by action prefix &#43; time range. |

 



<a name="platform_v1_account_self_service-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## platform/v1/account_self_service.proto



<a name="platform-v1-AccountSelfServiceGetMyProfileRequest"></a>

### AccountSelfServiceGetMyProfileRequest







<a name="platform-v1-AccountSelfServiceListMyApiKeysRequest"></a>

### AccountSelfServiceListMyApiKeysRequest







<a name="platform-v1-AccountSelfServiceListMyIdPLinksRequest"></a>

### AccountSelfServiceListMyIdPLinksRequest







<a name="platform-v1-AccountSelfServiceListMyMFAFactorsRequest"></a>

### AccountSelfServiceListMyMFAFactorsRequest







<a name="platform-v1-AccountSelfServiceListMyOAuthGrantsRequest"></a>

### AccountSelfServiceListMyOAuthGrantsRequest







<a name="platform-v1-AccountSelfServiceRemoveMyMFARequest"></a>

### AccountSelfServiceRemoveMyMFARequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| credential_id | [string](#string) |  |  |






<a name="platform-v1-AccountSelfServiceRemoveMyMFAResponse"></a>

### AccountSelfServiceRemoveMyMFAResponse







<a name="platform-v1-AccountSelfServiceRevokeMyApiKeyRequest"></a>

### AccountSelfServiceRevokeMyApiKeyRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key_id | [string](#string) |  |  |






<a name="platform-v1-AccountSelfServiceRevokeMyApiKeyResponse"></a>

### AccountSelfServiceRevokeMyApiKeyResponse







<a name="platform-v1-AccountSelfServiceRevokeMyOAuthGrantRequest"></a>

### AccountSelfServiceRevokeMyOAuthGrantRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| client_id | [string](#string) |  |  |






<a name="platform-v1-AccountSelfServiceRevokeMyOAuthGrantResponse"></a>

### AccountSelfServiceRevokeMyOAuthGrantResponse







<a name="platform-v1-AccountSelfServiceUnlinkMyIdPRequest"></a>

### AccountSelfServiceUnlinkMyIdPRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| external_idp_id | [string](#string) |  |  |






<a name="platform-v1-AccountSelfServiceUnlinkMyIdPResponse"></a>

### AccountSelfServiceUnlinkMyIdPResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| remaining | [IdPLink](#platform-v1-IdPLink) | repeated | remaining lists the caller&#39;s IdP links AFTER the unlink succeeds. |






<a name="platform-v1-AccountSelfServiceUpdateMyProfileRequest"></a>

### AccountSelfServiceUpdateMyProfileRequest
PATCH semantics: only set (present) fields are updated.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| display_name | [string](#string) | optional |  |
| contact_email | [string](#string) | optional |  |
| communication_prefs | [CommunicationPrefs](#platform-v1-CommunicationPrefs) | optional |  |






<a name="platform-v1-ApiKeySummary"></a>

### ApiKeySummary



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key_id | [string](#string) |  |  |
| name | [string](#string) |  |  |
| key_prefix | [string](#string) |  | First 12 chars only — safe to display. Full secret is never returned. |
| scopes | [string](#string) | repeated |  |
| issued_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| expires_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| last_used_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |






<a name="platform-v1-CommunicationPrefs"></a>

### CommunicationPrefs
CommunicationPrefs. security_alerts is documented as &#34;always-on by policy&#34; —
the backend ignores attempts to set false (returns OK but persists true).
product_updates &#43; marketing honor the user&#39;s setting.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| security_alerts | [bool](#bool) |  |  |
| product_updates | [bool](#bool) |  |  |
| marketing | [bool](#bool) |  |  |






<a name="platform-v1-IdPLink"></a>

### IdPLink
IdPLink is one external-IdP login method linked to the caller&#39;s global
identity (Obj 14). idp_subject &#43; idp_email are the values the IdP asserted
at link time (recorded on the link row, never promoted to the identity).
created_at is an RFC3339 string — the link rows are global (no per-tenant
timestamptz coercion the portal would need to localize differently).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| external_idp_id | [string](#string) |  |  |
| idp_name | [string](#string) |  |  |
| idp_subject | [string](#string) |  |  |
| idp_email | [string](#string) |  |  |
| created_at | [string](#string) |  |  |






<a name="platform-v1-ListMyApiKeysResponse"></a>

### ListMyApiKeysResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| keys | [ApiKeySummary](#platform-v1-ApiKeySummary) | repeated |  |






<a name="platform-v1-ListMyIdPLinksResponse"></a>

### ListMyIdPLinksResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| links | [IdPLink](#platform-v1-IdPLink) | repeated |  |






<a name="platform-v1-ListMyMFAFactorsResponse"></a>

### ListMyMFAFactorsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| factors | [MFAFactor](#platform-v1-MFAFactor) | repeated |  |






<a name="platform-v1-ListMyOAuthGrantsResponse"></a>

### ListMyOAuthGrantsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| grants | [OAuthGrant](#platform-v1-OAuthGrant) | repeated |  |






<a name="platform-v1-MFAFactor"></a>

### MFAFactor



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| credential_id | [string](#string) |  |  |
| kind | [string](#string) |  | &#34;totp&#34; | &#34;webauthn&#34; | &#34;piv&#34; |
| label | [string](#string) |  | User-supplied label (e.g., &#34;iPhone YubiKey&#34;) |
| enrolled_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| last_used_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| is_last_factor | [bool](#bool) |  | Server-computed UX hint: if true, removing this factor would leave the user with zero MFA factors. The portal disables the &#34;Remove&#34; button when this is true and the tenant requires MFA. Service-layer enforcement is the source of truth (TRD 08-03 — MGMT-02). |






<a name="platform-v1-MyProfile"></a>

### MyProfile



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| account_id | [string](#string) |  |  |
| tenant_id | [string](#string) |  |  |
| tenant_display_name | [string](#string) |  | Resolved display name from aoid.tenants.display_name — avoids the portal needing a separate GetMyTenantSummary RPC. |
| display_name | [string](#string) |  |  |
| contact_email | [string](#string) |  |  |
| communication_prefs | [CommunicationPrefs](#platform-v1-CommunicationPrefs) |  |  |
| updated_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |






<a name="platform-v1-OAuthGrant"></a>

### OAuthGrant



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| client_id | [string](#string) |  |  |
| client_name | [string](#string) |  |  |
| scopes | [string](#string) | repeated | UNION of scopes across non-revoked refresh_tokens for this (account, client) pair. |
| first_granted_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| last_used_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| active_token_count | [int32](#int32) |  |  |





 

 

 


<a name="platform-v1-AccountSelfService"></a>

### AccountSelfService
AccountSelfService provides end-user self-service surfaces for an authenticated
session subject. All RPCs derive the actor&#39;s account_id &#43; tenant_id from the
session context (set by the AOID Connect interceptor stack — TRD 08-04); no
RPC accepts account_id or tenant_id as an input field. The portal (TRD 08-06&#43;)
is the canonical client.

MFA enrollment continues to use AuthnService (Obj 3, TRD 03-05). MFA REMOVAL
is here because the last-factor policy gate (MGMT-02) is service-layer logic
that doesn&#39;t naturally belong on AuthnService. Session listing &#43; revoke is on
EndUserSessionService (Obj 3, TRD 03-05) and is NOT duplicated here.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| GetMyProfile | [AccountSelfServiceGetMyProfileRequest](#platform-v1-AccountSelfServiceGetMyProfileRequest) | [MyProfile](#platform-v1-MyProfile) | MGMT-01 — Profile management. Both Get and Update return MyProfile (the canonical projection of the caller&#39;s account). Lint exceptions disable RPC_RESPONSE_STANDARD_NAME / RPC_REQUEST_RESPONSE_UNIQUE for these two because UpdateMyProfile is semantically an upsert that returns the same shape as Get; introducing wrapper Response messages would force the portal (Dart) to unwrap on every call for no contract benefit. buf:lint:ignore RPC_RESPONSE_STANDARD_NAME buf:lint:ignore RPC_REQUEST_RESPONSE_UNIQUE |
| UpdateMyProfile | [AccountSelfServiceUpdateMyProfileRequest](#platform-v1-AccountSelfServiceUpdateMyProfileRequest) | [MyProfile](#platform-v1-MyProfile) | buf:lint:ignore RPC_RESPONSE_STANDARD_NAME buf:lint:ignore RPC_REQUEST_RESPONSE_UNIQUE |
| ListMyMFAFactors | [AccountSelfServiceListMyMFAFactorsRequest](#platform-v1-AccountSelfServiceListMyMFAFactorsRequest) | [ListMyMFAFactorsResponse](#platform-v1-ListMyMFAFactorsResponse) | MGMT-02 — MFA factor listing &#43; removal (with last-factor policy gate) |
| RemoveMyMFA | [AccountSelfServiceRemoveMyMFARequest](#platform-v1-AccountSelfServiceRemoveMyMFARequest) | [AccountSelfServiceRemoveMyMFAResponse](#platform-v1-AccountSelfServiceRemoveMyMFAResponse) |  |
| ListMyOAuthGrants | [AccountSelfServiceListMyOAuthGrantsRequest](#platform-v1-AccountSelfServiceListMyOAuthGrantsRequest) | [ListMyOAuthGrantsResponse](#platform-v1-ListMyOAuthGrantsResponse) | MGMT-04 — OAuth grant listing &#43; revoke (one row per (account, client) pair) |
| RevokeMyOAuthGrant | [AccountSelfServiceRevokeMyOAuthGrantRequest](#platform-v1-AccountSelfServiceRevokeMyOAuthGrantRequest) | [AccountSelfServiceRevokeMyOAuthGrantResponse](#platform-v1-AccountSelfServiceRevokeMyOAuthGrantResponse) |  |
| ListMyApiKeys | [AccountSelfServiceListMyApiKeysRequest](#platform-v1-AccountSelfServiceListMyApiKeysRequest) | [ListMyApiKeysResponse](#platform-v1-ListMyApiKeysResponse) | MGMT-04 — API key listing &#43; self-revoke |
| RevokeMyApiKey | [AccountSelfServiceRevokeMyApiKeyRequest](#platform-v1-AccountSelfServiceRevokeMyApiKeyRequest) | [AccountSelfServiceRevokeMyApiKeyResponse](#platform-v1-AccountSelfServiceRevokeMyApiKeyResponse) |  |
| ListMyIdPLinks | [AccountSelfServiceListMyIdPLinksRequest](#platform-v1-AccountSelfServiceListMyIdPLinksRequest) | [ListMyIdPLinksResponse](#platform-v1-ListMyIdPLinksResponse) | MGMT-05 — Linked IdP listing &#43; self-unlink (Obj 14 GID-08). The caller&#39;s identity is resolved from the session; UnlinkMyIdP refuses to remove the last remaining link (CodeFailedPrecondition) to prevent login lockout. |
| UnlinkMyIdP | [AccountSelfServiceUnlinkMyIdPRequest](#platform-v1-AccountSelfServiceUnlinkMyIdPRequest) | [AccountSelfServiceUnlinkMyIdPResponse](#platform-v1-AccountSelfServiceUnlinkMyIdPResponse) |  |

 



<a name="platform_v1_aoedge-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## platform/v1/aoedge.proto



<a name="platform-v1-BackendEntry"></a>

### BackendEntry
BackendEntry is one upstream backend&#39;s runtime health snapshot,
reported by GetBackendHealth.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| url | [string](#string) |  | Upstream backend URL, e.g. &#34;https://backend-a:8443&#34;. |
| weight | [int32](#int32) |  | Configured SWRR weight for this backend. |
| health_state | [BackendHealthState](#platform-v1-BackendHealthState) |  | Current circuit breaker state. |
| consecutive_failures | [int32](#int32) |  | Number of consecutive probe failures in the current window. |
| last_probe_time | [string](#string) |  | RFC 3339 timestamp of the last health probe attempt. Empty string if no probe has been run yet. |






<a name="platform-v1-GetBackendHealthRequest"></a>

### GetBackendHealthRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| hostname_filter | [string](#string) |  | Optional: filter to a specific hostname. Empty = return all backends. |






<a name="platform-v1-GetBackendHealthResponse"></a>

### GetBackendHealthResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| backends | [BackendEntry](#platform-v1-BackendEntry) | repeated | All backends across all routes (or filtered by hostname_filter). |
| snapshot_time | [string](#string) |  | Timestamp when this snapshot was taken (RFC 3339). |






<a name="platform-v1-GetBuildInfoRequest"></a>

### GetBuildInfoRequest







<a name="platform-v1-GetBuildInfoResponse"></a>

### GetBuildInfoResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| version | [string](#string) |  | Semantic version of this AOEdge binary, e.g. &#34;1.0.0&#34;. |
| commit | [string](#string) |  | Git commit SHA the binary was built from. |
| built_at | [string](#string) |  | RFC 3339 timestamp of the build. |
| go_version | [string](#string) |  | Go toolchain version, e.g. &#34;go1.26.1 (FIPS)&#34;. |
| fips_module | [string](#string) |  | Identifier of the FIPS-validated cryptographic module in use. |
| eden_proto_schema_version | [string](#string) |  | Eden proto schema version (or commit) this binary was generated against. Lets operators detect drift between the AOEdge binary&#39;s expected contracts and what AOID / AOCore / AOAudit are running. |






<a name="platform-v1-HealthCheckRequest"></a>

### HealthCheckRequest







<a name="platform-v1-HealthCheckResponse"></a>

### HealthCheckResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| alive | [bool](#bool) |  | Liveness: process is up and able to serve admin RPCs. |
| ready | [bool](#bool) |  | Readiness: data plane is ready to accept external traffic (TLS listener bound, upstream connection pool healthy, policy bundle loaded if Objective 10 has shipped). |
| fips_mode | [bool](#bool) |  | FIPS mode flag — true when the binary was built with BoringCrypto and the runtime has verified the FIPS module is the active crypto provider. Required to be true for FedRAMP Moderate / High / IL4 / IL5 deployments. |
| status_detail | [string](#string) |  | Human-readable status detail, e.g. &#34;ready&#34;, &#34;draining&#34;, &#34;upstream_unreachable&#34;, &#34;policy_bundle_stale&#34;. |






<a name="platform-v1-ListRoutesRequest"></a>

### ListRoutesRequest







<a name="platform-v1-ListRoutesResponse"></a>

### ListRoutesResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| routes | [RouteEntry](#platform-v1-RouteEntry) | repeated | Routes in the live route table, one entry per (hostname, path_pattern) pair. |
| total_routes | [int32](#int32) |  | Total number of routes in the current table. |






<a name="platform-v1-RouteEntry"></a>

### RouteEntry
RouteEntry is one (hostname, path_pattern) route, reported by ListRoutes.
Carries only observable operational data; Tenant / Identity / Policy
fields are intentionally absent (those are Obj 9 / Obj 10 concerns).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| hostname | [string](#string) |  | The hostname this route is keyed on (e.g., &#34;api.example.com&#34;). |
| path_pattern | [string](#string) |  | The path pattern this route matches (e.g., &#34;/v2/*&#34;, &#34;/api/users&#34;). |
| backend_count | [int32](#int32) |  | Number of backends configured for this route. |
| healthy_backend_count | [int32](#int32) |  | Number of backends currently healthy (circuit breaker CLOSED). |





 


<a name="platform-v1-BackendHealthState"></a>

### BackendHealthState
BackendHealthState mirrors the gobreaker v2 circuit breaker states.
CLOSED = healthy, in rotation. OPEN = unhealthy, not in rotation.
HALF_OPEN = recovering, limited probe requests allowed.

| Name | Number | Description |
| ---- | ------ | ----------- |
| BACKEND_HEALTH_STATE_UNSPECIFIED | 0 |  |
| BACKEND_HEALTH_STATE_CLOSED | 1 | healthy |
| BACKEND_HEALTH_STATE_OPEN | 2 | unhealthy |
| BACKEND_HEALTH_STATE_HALF_OPEN | 3 | recovering |


 

 


<a name="platform-v1-AOEdgeAdminService"></a>

### AOEdgeAdminService
AOEdgeAdminService exposes the headless boundary proxy&#39;s operational
surface: health, FIPS mode reporting, and build/version metadata.
The data plane (TLS termination, WAF, DDoS, geo, DLP, identity proxy,
policy evaluation) is NOT exposed here — those are handled at the
TCP/HTTP listener, not via Connect RPC. This service is reachable
only from the platform&#39;s internal admin network over mTLS.

AOEdge owns: SC-7 boundary protection, identity-aware proxy, WAF,
DDoS, geo, egress DLP. AOEdge does NOT own: credential issuance
(AOID), AI-aware policy (AOCore), audit storage (AOAudit).

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| HealthCheck | [HealthCheckRequest](#platform-v1-HealthCheckRequest) | [HealthCheckResponse](#platform-v1-HealthCheckResponse) | HealthCheck reports liveness, readiness, and FIPS posture for a single AOEdge instance. Used by L4 LBs for backend selection and by operators verifying FIPS mode is active in regulated deployments. |
| GetBuildInfo | [GetBuildInfoRequest](#platform-v1-GetBuildInfoRequest) | [GetBuildInfoResponse](#platform-v1-GetBuildInfoResponse) | GetBuildInfo returns the binary&#39;s build metadata: version, commit, build timestamp, Go version, FIPS module identifier, and the eden proto schema version this binary was generated against. Used for compliance evidence (which build is enforcing the boundary right now) and for cross-fleet drift detection. |
| ListRoutes | [ListRoutesRequest](#platform-v1-ListRoutesRequest) | [ListRoutesResponse](#platform-v1-ListRoutesResponse) | ListRoutes returns the current active route table as seen by this instance. Used by operators and dashboards to inspect which hostnames and paths AOEdge is currently routing, and the health status of each route&#39;s backends. This reflects the live atomic.Pointer[RouteTable] — always consistent. |
| GetBackendHealth | [GetBackendHealthRequest](#platform-v1-GetBackendHealthRequest) | [GetBackendHealthResponse](#platform-v1-GetBackendHealthResponse) | GetBackendHealth returns the health state of all registered upstream backends. Exposes the circuit breaker state machine so operators can observe which backends are in rotation without needing direct access to the data plane. |

 



<a name="platform_v1_aoedge_audit-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## platform/v1/aoedge_audit.proto



<a name="platform-v1-AuditBatch"></a>

### AuditBatch
AuditBatch wraps a set of events with signing metadata.
Emitted as a single OTLP log record once per dispatcher batch; raw event
bytes travel as their own OTLP log records and are referenced by the
merkle_root signature.

NOTE: AuditBatch.event_count is int32; bumping to int64 is deferred to
Obj 11 (operational-posture objective). A 2^31 event-count batch is far
beyond v1 dispatch sizing.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| schema_version | [int32](#int32) |  |  |
| timestamp | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| batch_id | [string](#string) |  | rs/xid |
| seq_first | [int64](#int64) |  |  |
| seq_last | [int64](#int64) |  |  |
| merkle_root | [string](#string) |  | hex SHA-256 root of batch leaf hashes |
| root_signature | [string](#string) |  | Ed25519 or KMS signature over merkle_root (hex) |
| signing_key_id | [string](#string) |  | key identifier (local or KMS ARN) |
| event_count | [int32](#int32) |  |  |






<a name="platform-v1-BundleReloadEvent"></a>

### BundleReloadEvent
BundleReloadEvent — emitted by the signed-bundle reloader on EVERY reload
attempt (AUTHZ-06): a successful atomic-swap (applied), a rejected bad
signature, a Rego compile failure, or a Git pull failure. This is the
audit record of the policy-plane control loop (SC6).

PII-safety: this message MUST NOT carry the signing private key, the
verification public key bytes, or the bundle file contents. Only the
commit_sha &#43; signer_identity (operator string from the signature&#39;s trusted
comment) &#43; bundle_digest (the hex digest that WAS verified). A compromised
bundle must not be able to log/assert its own trust root.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| schema_version | [int32](#int32) |  |  |
| timestamp | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| request_id | [string](#string) |  | may be &#34;&#34; — system event, not request-scoped |
| trace_id | [string](#string) |  |  |
| span_id | [string](#string) |  |  |
| tenant_id | [string](#string) |  | may be &#34;&#34; — deployment-wide single bundle in Obj 10 (per-tenant bundles are Obj 11) |
| commit_sha | [string](#string) |  | Git HEAD SHA of the pulled bundle |
| previous_commit_sha | [string](#string) |  | the SHA that was active before this reload (for rollback correlation) |
| signer_identity | [string](#string) |  | operator/key identity from the detached-signature trusted comment; never the key bytes |
| bundle_digest | [string](#string) |  | hex of the deterministic bundle digest that crypto/ed25519.Verify ran against |
| outcome | [string](#string) |  | &#34;applied&#34; | &#34;rejected_signature&#34; | &#34;compile_error&#34; | &#34;pull_failed&#34; |
| outcome_detail | [string](#string) |  | operator-readable detail (e.g. &#34;rego parse error in admin.rego:14&#34;); never raw input/key |
| rego_module_count | [int32](#int32) |  | number of .rego modules compiled into the prepared query |
| reload_latency_ms | [int64](#int64) |  | pull &#43; verify &#43; compile &#43; swap wallclock |
| chain_hash | [string](#string) |  |  |






<a name="platform-v1-ConnectionLog"></a>

### ConnectionLog
ConnectionLog is emitted once per request (allowed or blocked).
schema_version: increment when adding non-optional fields.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| schema_version | [int32](#int32) |  | always 1 for v1; bump on breaking change |
| timestamp | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| request_id | [string](#string) |  | rs/xid (20 chars) |
| trace_id | [string](#string) |  | W3C traceparent trace-id (hex) |
| span_id | [string](#string) |  | W3C traceparent span-id (hex) |
| source_ip | [string](#string) |  |  |
| identity | [string](#string) |  | &#34;anonymous&#34; or actor_id |
| route_id | [string](#string) |  |  |
| upstream_backend | [string](#string) |  |  |
| decision | [string](#string) |  | &#34;allow&#34; | &#34;block&#34; | &#34;error&#34; |
| status_code | [int32](#int32) |  |  |
| latency_ms | [int64](#int64) |  |  |
| response_bytes | [int64](#int64) |  |  |
| block_reason | [string](#string) |  | empty when decision=allow |
| tenant_id | [string](#string) |  |  |
| chain_hash | [string](#string) |  | SHA-256 hex of (prev_chain_hash || canonical(this event)) |






<a name="platform-v1-DDoSEvent"></a>

### DDoSEvent
DDoSEvent is emitted on every rate-limit or slow-loris rejection.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| schema_version | [int32](#int32) |  |  |
| timestamp | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| request_id | [string](#string) |  |  |
| trace_id | [string](#string) |  |  |
| span_id | [string](#string) |  |  |
| limit_type | [string](#string) |  | &#34;l4_conn&#34; | &#34;l7_ip&#34; | &#34;l7_identity&#34; | &#34;l7_tenant&#34; | &#34;slow_loris&#34; | &#34;smuggling&#34; |
| source_identifier | [string](#string) |  | IP | identity | tenant |
| counter_value | [int64](#int64) |  |  |
| threshold | [int64](#int64) |  |  |
| tenant_id | [string](#string) |  |  |
| chain_hash | [string](#string) |  |  |






<a name="platform-v1-DLPFinding"></a>

### DLPFinding
DLPFinding is emitted when DLP scanning detects a pattern match.

HARD INVARIANT (PII-safety): this message and its DLPMatch entries
MUST NEVER store the matched payload bytes. Only the pattern_id,
operator-readable pattern_name, and byte offset&#43;length are recorded.
A schema-lint test enforces this contract — see the top-of-file note
for the prohibited field names.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| schema_version | [int32](#int32) |  |  |
| timestamp | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| request_id | [string](#string) |  |  |
| trace_id | [string](#string) |  |  |
| span_id | [string](#string) |  |  |
| route_id | [string](#string) |  |  |
| tenant_id | [string](#string) |  |  |
| mode | [string](#string) |  | &#34;block&#34; | &#34;redact&#34; | &#34;audit&#34; |
| matches | [DLPMatch](#platform-v1-DLPMatch) | repeated |  |
| chain_hash | [string](#string) |  |  |






<a name="platform-v1-DLPMatch"></a>

### DLPMatch
DLPMatch carries a single pattern hit inside a DLPFinding. NO PII payload
bytes — only pattern identifiers and byte coordinates (offset&#43;length).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| pattern_id | [string](#string) |  |  |
| pattern_name | [string](#string) |  |  |
| offset | [int64](#int64) |  |  |
| length | [int64](#int64) |  |  |






<a name="platform-v1-GeoDecision"></a>

### GeoDecision
GeoDecision is emitted on every geo enforcement evaluation.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| schema_version | [int32](#int32) |  |  |
| timestamp | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| request_id | [string](#string) |  |  |
| trace_id | [string](#string) |  |  |
| span_id | [string](#string) |  |  |
| source_ip | [string](#string) |  |  |
| country_iso | [string](#string) |  |  |
| registered_country_iso | [string](#string) |  |  |
| asn | [uint32](#uint32) |  |  |
| as_org | [string](#string) |  |  |
| subdivisions | [string](#string) | repeated |  |
| route_id | [string](#string) |  |  |
| tenant_id | [string](#string) |  |  |
| decision | [string](#string) |  | &#34;allow&#34; | &#34;block&#34; |
| block_reason | [string](#string) |  |  |
| is_tor_exit | [bool](#bool) |  |  |
| is_vpn | [bool](#bool) |  |  |
| is_public_proxy | [bool](#bool) |  |  |
| us_only_mode_active | [bool](#bool) |  |  |
| chain_hash | [string](#string) |  |  |






<a name="platform-v1-IdentityMintEvent"></a>

### IdentityMintEvent
IdentityMintEvent — emitted by the AOEdge IAP middleware on every
successful X-AOEdge-Identity-Context JWT mint (IAP-04). One event per
request that crosses to upstream with a freshly-signed identity context.

PII-safety: the signed JWT itself is NOT included — it travels in the
X-AOEdge-Identity-Context request header. The audit stream logs only
metadata (sub, tnt, aal, kid, tok_ref, entitlements_count). The
schema-lint test forbids signed_jwt | jwt | id_token | access_token
field names on this message.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| schema_version | [int32](#int32) |  |  |
| timestamp | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| request_id | [string](#string) |  |  |
| trace_id | [string](#string) |  |  |
| span_id | [string](#string) |  |  |
| kid | [string](#string) |  | AOEdge active signing key id |
| sub | [string](#string) |  | AOID account UUID from validated credential |
| tnt | [string](#string) |  | tenant SLUG (NOT UUID — see Obj 9 RESEARCH §6 pitfall) |
| aal | [string](#string) |  |  |
| tok_ref | [string](#string) |  | bound to validated credential (16 chars hex) |
| entitlements_count | [int32](#int32) |  | count only — entitlement strings not logged for size |
| ttl_seconds | [int64](#int64) |  | identity-context exp - iat at mint time |
| alg | [string](#string) |  | &#34;ES256&#34; | &#34;RS256&#34; |
| tenant_id | [string](#string) |  | tenant UUID (resolved from tnt slug); for audit-stream filtering |
| route_id | [string](#string) |  |  |
| chain_hash | [string](#string) |  |  |






<a name="platform-v1-IdentityValidationEvent"></a>

### IdentityValidationEvent
IdentityValidationEvent — emitted by the AOEdge IAP middleware on every
credential validation: JWT (IAP-01), opaque token via RFC 7662
introspection (IAP-02), API key (IAP-03), or anonymous-route admission
(IAP-07). BOTH accept and reject paths emit one event.

PII-safety: this message MUST NOT carry the raw credential bytes. Only
`tok_ref` (first-8-byte SHA-256 hex of the raw credential, 16 chars) is
included — same pattern as the AOID identity-context tok_ref. The
schema-lint test in aoedge_audit_lint_test.go asserts the forbidden
field names raw_token | raw_key | password | plaintext | authorization
and bare token | secret are absent file-wide.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| schema_version | [int32](#int32) |  |  |
| timestamp | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| request_id | [string](#string) |  |  |
| trace_id | [string](#string) |  |  |
| span_id | [string](#string) |  |  |
| cred_type | [string](#string) |  | &#34;jwt&#34; | &#34;opaque&#34; | &#34;api_key&#34; | &#34;anonymous&#34; |
| tok_ref | [string](#string) |  | hex(sha256(raw_credential))[:8] =&gt; 16 chars; &#34;&#34; for anonymous |
| outcome | [string](#string) |  | &#34;accepted&#34; | &#34;rejected_invalid&#34; | &#34;rejected_expired&#34; | &#34;rejected_wrong_audience&#34; | &#34;rejected_wrong_issuer&#34; | &#34;rejected_aoid_unreachable&#34; | &#34;rejected_inactive&#34; |
| outcome_reason | [string](#string) |  | operator-readable detail; never includes credential bytes |
| iss | [string](#string) |  | expected issuer (configured AOID URL); &#34;&#34; when cred_type=api_key/anonymous |
| sub | [string](#string) |  | accepted: AOID account UUID; rejected: &#34;&#34; |
| tenant_id | [string](#string) |  | tenant UUID resolved by hostname routing (Obj 3); always populated for non-anonymous |
| aal | [string](#string) |  | resolved AAL (&#34;AAL1&#34;/&#34;AAL2&#34;/&#34;AAL3&#34;); &#34;&#34; on reject |
| latency_ms | [int64](#int64) |  | wallclock validation latency incl. any AOID round-trip |
| cache_hit | [bool](#bool) |  | true when validation served from in-process introspection/API-key cache |
| route_id | [string](#string) |  | chi.RouteContext.RoutePattern() at validation time |
| chain_hash | [string](#string) |  |  |






<a name="platform-v1-MatchedRuleEntry"></a>

### MatchedRuleEntry
MatchedRuleEntry describes a single Coraza rule match inside a WAFEvent.
The fragment_b64 field carries an operator-bounded fragment for incident
triage; v1 is base64-encoded and capped at ~256 bytes by the producer.
DLPFinding / DLPMatch must NOT include any analogue of this field —
see the PII-safety invariant at the top of this file.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| rule_id | [int32](#int32) |  |  |
| severity | [string](#string) |  |  |
| message | [string](#string) |  |  |
| tags | [string](#string) | repeated |  |
| matched_var | [string](#string) |  |  |
| fragment_b64 | [string](#string) |  | base64 (waf only); capped at producer |






<a name="platform-v1-PolicyDecisionEvent"></a>

### PolicyDecisionEvent
PolicyDecisionEvent — emitted by the authz middleware on EVERY policy
evaluation: allow, deny, or engine error (AUTHZ-01). Mirrors the way Geo
emits GeoDecision unconditionally. The allow/deny ratio per route is
derived from these events &#43; the aoedge_authz_* Prometheus counters.

PII-safety: this message MUST NOT carry the raw policy input document, the
raw identity-claims blob, or any credential. The full input is
reconstructible by correlating on request_id with the IdentityValidationEvent
(Obj 9) &#43; GeoDecision (Obj 8) events. Schema-lint asserts policy_input /
raw_input / token / secret / etc. are absent file-wide.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| schema_version | [int32](#int32) |  |  |
| timestamp | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| request_id | [string](#string) |  |  |
| trace_id | [string](#string) |  |  |
| span_id | [string](#string) |  |  |
| tenant_id | [string](#string) |  | tenant UUID resolved by hostname routing (Obj 3); credential-authoritative tenant for cross-tenant decisions |
| route_id | [string](#string) |  | chi.RouteContext.RoutePattern() at evaluation time |
| sub | [string](#string) |  | identity subject (account UUID from Obj 9 iap.Identity); &#34;&#34; for anonymous |
| decision | [string](#string) |  | &#34;allow&#34; | &#34;deny&#34; | &#34;error&#34; |
| deny_reason | [string](#string) |  | &#34;wrong_tenant&#34; | &#34;missing_entitlement&#34; | &#34;outside_business_hours&#34; | &#34;geo_blocked&#34; | &#34;risk_exceeded&#34; | &#34;engine_error&#34; | &#34;&#34;; operator-readable, never raw input |
| policy_query | [string](#string) |  | the Rego rule path queried for this route (e.g. &#34;data.aoedge.authz.decision&#34;) |
| backend_group | [string](#string) |  | split-horizon output (AUTHZ-04); &#34;&#34; when not identity-routed |
| required_entitlement | [string](#string) |  | the entitlement the route required (AUTHZ-02); &#34;&#34; when none |
| require_stepup | [bool](#bool) |  | policy output: higher AAL required (AUTHZ-05 step-up interplay) |
| required_aal | [string](#string) |  | &#34;AAL2&#34;|&#34;AAL3&#34; when require_stepup=true; &#34;&#34; otherwise |
| bundle_revision | [string](#string) |  | active bundle commit SHA at evaluation time (correlates to BundleReloadEvent) |
| latency_ms | [int64](#int64) |  | policy eval wallclock latency (microsecond budget; reported in ms) |
| fail_open | [bool](#bool) |  | true when decision=allow was produced by AOEDGE_AUTHZ_FAIL_OPEN on an engine error |
| chain_hash | [string](#string) |  |  |






<a name="platform-v1-StepUpChallengeEvent"></a>

### StepUpChallengeEvent
StepUpChallengeEvent — emitted by the AOEdge IAP middleware when per-route
policy requires a higher AAL than the validated credential carries
(IAP-06). The middleware issues either a browser 302 to AOID
/authorize?acr_values=... or a non-browser 403 with RFC 9470
WWW-Authenticate: Bearer error=&#34;insufficient_user_authentication&#34;.

PII-safety: redirect_uri_host carries only scheme&#43;host&#43;path of the AOID
authorize endpoint — query parameters (state, nonce, redirect_uri) are
NOT logged. The schema-lint test forbids the bare `redirect_uri` field.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| schema_version | [int32](#int32) |  |  |
| timestamp | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| request_id | [string](#string) |  |  |
| trace_id | [string](#string) |  |  |
| span_id | [string](#string) |  |  |
| required_aal | [string](#string) |  | &#34;AAL2&#34; | &#34;AAL3&#34; |
| current_aal | [string](#string) |  | AAL from validated credential (&#34;AAL1&#34; typical) |
| channel | [string](#string) |  | &#34;browser&#34; (302) | &#34;api&#34; (403) |
| redirect_uri_host | [string](#string) |  | scheme://host/path of AOID /authorize; NO query string |
| status_code | [int32](#int32) |  | 302 | 403 |
| tenant_id | [string](#string) |  |  |
| route_id | [string](#string) |  |  |
| sub | [string](#string) |  | current account UUID (already authenticated at lower AAL) |
| chain_hash | [string](#string) |  |  |






<a name="platform-v1-WAFEvent"></a>

### WAFEvent
WAFEvent is emitted on every WAF rule trigger.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| schema_version | [int32](#int32) |  |  |
| timestamp | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| request_id | [string](#string) |  |  |
| trace_id | [string](#string) |  |  |
| span_id | [string](#string) |  |  |
| tx_id | [string](#string) |  | Coraza transaction ID |
| action | [string](#string) |  | &#34;block&#34; | &#34;would_block&#34; | &#34;pass&#34; |
| rule_id | [int32](#int32) |  |  |
| anomaly_score | [int32](#int32) |  |  |
| inbound_threshold | [int32](#int32) |  |  |
| method | [string](#string) |  |  |
| path | [string](#string) |  |  |
| host | [string](#string) |  |  |
| tenant_id | [string](#string) |  |  |
| route_id | [string](#string) |  |  |
| shadow_mode | [bool](#bool) |  |  |
| client_ip | [string](#string) |  |  |
| matched_rules | [MatchedRuleEntry](#platform-v1-MatchedRuleEntry) | repeated |  |
| chain_hash | [string](#string) |  |  |





 

 

 

 



<a name="platform_v1_api_key_validation-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## platform/v1/api_key_validation.proto



<a name="platform-v1-ApiKeyValidationServiceValidateApiKeyRequest"></a>

### ApiKeyValidationServiceValidateApiKeyRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  | Owning tenant UUID. AOEdge knows which tenant the request is for (from upstream OAuth context) and supplies it here; the AOID server uses it as the lookup parameter on the (tenant_id, key_prefix) index. |
| raw_key | [string](#string) |  | The raw API key under validation. Format: &#34;aoid_&lt;env&gt;_&lt;52-char-base32&gt;&#34;. |






<a name="platform-v1-ApiKeyValidationServiceValidateApiKeyResponse"></a>

### ApiKeyValidationServiceValidateApiKeyResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| valid | [bool](#bool) |  | True only when the key passes every check. |
| api_key_id | [string](#string) |  | Only set when valid=true. |
| account_id | [string](#string) |  | Only set when valid=true. |
| tenant_id | [string](#string) |  | Echoes the request tenant_id for caller-side consistency check. |
| scopes | [string](#string) | repeated | Only set when valid=true. |
| reason | [string](#string) |  | Coarse reason when valid=false. One of: &#34;unknown&#34; — prefix not found OR revoked OR hash mismatch &#34;expired&#34; — prefix found, hash matched, but past expires_at &#34;invalid_format&#34; — raw_key doesn&#39;t start with &#34;aoid_&#34; or is too short Empty when valid=true. |





 

 

 


<a name="platform-v1-ApiKeyValidationService"></a>

### ApiKeyValidationService
ApiKeyValidationService is the AOEdge-facing API-key validation
surface per VAL-03. It is a SEPARATE service from
CredentialAdminService because the Connect interceptor stack differs:

  - CredentialAdminService: mTLS &#43; adminauth &#43; tenant-scope &#43; audit
  - ApiKeyValidationService: mTLS &#43; validation_api_consumers
    allow-list &#43; audit

Calls without mTLS or with an unknown mTLS peer-cert SPIFFE ID are
rejected by the interceptor BEFORE reaching the handler (per AOID&#39;s
success criterion #5 of Objective 5 &#43; VAL-03 spec). The handler
itself enforces a defense-in-depth allow-list re-check.

Wire shape is INTENTIONALLY narrow — only the raw_key and tenant_id
cross the boundary. The response distinguishes valid keys from
invalid ones with a coarse reason string (forward-compat: future
reasons can be added without breaking wire compat — adopters parse
on known string set &#43; treat unknown as &#34;invalid&#34;).

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| ValidateApiKey | [ApiKeyValidationServiceValidateApiKeyRequest](#platform-v1-ApiKeyValidationServiceValidateApiKeyRequest) | [ApiKeyValidationServiceValidateApiKeyResponse](#platform-v1-ApiKeyValidationServiceValidateApiKeyResponse) | ValidateApiKey validates a raw API key. Response.valid is true only when the key is well-formed, not revoked, not expired, and its stored hash matches the submitted raw key under platform/auth/secrethasher.Verify. The response does NOT leak revocation state — revoked keys return reason=&#34;unknown&#34;. |

 



<a name="platform_v1_audit-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## platform/v1/audit.proto



<a name="platform-v1-AuditLogEntry"></a>

### AuditLogEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  |  |
| company_id | [string](#string) |  |  |
| actor_id | [string](#string) |  |  |
| action | [string](#string) |  |  |
| resource | [string](#string) |  |  |
| resource_id | [string](#string) |  |  |
| details_json | [string](#string) |  |  |
| ip_address | [string](#string) |  |  |
| created_at | [string](#string) |  |  |






<a name="platform-v1-IngestBreakGlassEventRequest"></a>

### IngestBreakGlassEventRequest
IngestBreakGlassEventRequest carries the contents of one JSONL line
from the aoidemergency replay file. The server uses these fields
verbatim (it does NOT recompute or override original_jti /
original_issued_at) — this is by design so the AOAudit timeline
reconstructs the outage window faithfully.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| original_jti | [string](#string) |  | original_jti is the UUID minted at issuance time. Must be uuid-shaped. Server dedups on this field: re-replay is a no-op. |
| original_issued_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | original_issued_at is the timestamp the CLI recorded at JWT issuance. Server SETS the audit row&#39;s emitted_at to this value (NOT request-arrival time). Server rejects values older than 30 days (sanity bound — older replays are suspicious). |
| subject | [string](#string) |  | subject is the JWT &#39;sub&#39; claim — the principal the break-glass credential authenticated. |
| tenant | [string](#string) |  | tenant is the tenant slug the credential was scoped to (or &#39;*&#39; for chassis-wide recovery operations). |
| scope | [string](#string) |  | scope is the manifest scope: &#39;admin.recovery&#39; | &#39;service.recovery&#39;. |
| reason | [string](#string) |  | reason is the operator-provided human-readable reason from the authorization manifest (≥ 20 chars enforced at issuance time). |
| operator | [string](#string) |  | operator is the requested_by email from the manifest — the operator who drafted the request (NOT the ≥ 2 signers; see manifest_digest_b64 to recover the full signer set offline). |
| manifest_digest_b64 | [string](#string) |  | manifest_digest_b64 is the base64-std-encoded SHA-256 digest of the raw manifest bytes the CLI consumed. The replay tool can produce the manifest file alongside the replay log; downstream observers can recompute and confirm the manifest wasn&#39;t substituted post-hoc. |






<a name="platform-v1-IngestBreakGlassEventResponse"></a>

### IngestBreakGlassEventResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| accepted | [bool](#bool) |  | accepted is true if the event landed (or was idempotently de-duped). false (with a non-OK status) indicates a hard rejection. |
| event_id | [string](#string) |  | event_id is the server-side UUID assigned to the audit row. Stable across replays — re-replay returns the original event_id. |






<a name="platform-v1-ListAuditLogsRequest"></a>

### ListAuditLogsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| company_id | [string](#string) |  |  |
| limit | [int32](#int32) |  |  |
| offset | [int32](#int32) |  |  |
| actor_id | [string](#string) | optional |  |
| action | [string](#string) | optional |  |
| resource | [string](#string) | optional |  |






<a name="platform-v1-ListAuditLogsResponse"></a>

### ListAuditLogsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| entries | [AuditLogEntry](#platform-v1-AuditLogEntry) | repeated |  |
| total | [int32](#int32) |  |  |





 

 

 


<a name="platform-v1-AuditService"></a>

### AuditService
AuditService provides audit log queries &#43; a break-glass replay endpoint
for the aoidemergency tool (AOID Obj 11 TRD 11-03).

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| ListAuditLogs | [ListAuditLogsRequest](#platform-v1-ListAuditLogsRequest) | [ListAuditLogsResponse](#platform-v1-ListAuditLogsResponse) |  |
| IngestBreakGlassEvent | [IngestBreakGlassEventRequest](#platform-v1-IngestBreakGlassEventRequest) | [IngestBreakGlassEventResponse](#platform-v1-IngestBreakGlassEventResponse) | IngestBreakGlassEvent receives a previously-issued break-glass credential event from the aoidemergency `replay` subcommand AFTER recovery completes. The server preserves the ORIGINAL issuance timestamp (from the request body) when inserting into the local audit buffer — incident-response timeline reconstruction depends on this fidelity. Idempotent by original_jti: replays of the same JSONL file return accepted=true without inserting duplicate rows.

ACL (enforced by the host service interceptor chain): admin role required. Tenant-admin actors MAY replay events scoped to their tenant (original operator may not retain super-admin credentials post-recovery); super-admin actors may replay any tenant&#39;s events.

Write-capability gating: this RPC is classified WRITE by the AOID write-capability interceptor (Obj 11 TRD 11-01), so replay against a replica region returns Unavailable — operators must direct replay at the post-recovery primary. |

 



<a name="platform_v1_audit_query-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## platform/v1/audit_query.proto



<a name="platform-v1-AuditEvent"></a>

### AuditEvent
AuditEvent is the admin-facing projection of a single audit-buffer row.
`jws_signature` is the full JWS Compact string for downstream replay &#43;
verify; the raw signed payload bytes are NOT surfaced (the verifier
reconstructs the payload from the typed fields).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| event_id | [string](#string) |  |  |
| emitted_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| tenant_id | [string](#string) |  |  |
| actor_id | [string](#string) |  |  |
| actor_kind | [string](#string) |  |  |
| subject_id | [string](#string) |  |  |
| subject_kind | [string](#string) |  |  |
| action | [string](#string) |  |  |
| resource | [string](#string) |  |  |
| resource_id | [string](#string) |  |  |
| source_ip | [string](#string) |  |  |
| decision | [string](#string) |  |  |
| risk_score | [int32](#int32) |  |  |
| details_json | [string](#string) |  |  |
| jws_signature | [string](#string) |  |  |






<a name="platform-v1-AuditQueryServiceGetAuditEventRequest"></a>

### AuditQueryServiceGetAuditEventRequest
AuditQueryServiceGetAuditEventRequest fetches one event by jti.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| event_id | [string](#string) |  |  |






<a name="platform-v1-AuditQueryServiceGetAuditEventResponse"></a>

### AuditQueryServiceGetAuditEventResponse
AuditQueryServiceGetAuditEventResponse wraps the requested event.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| event | [AuditEvent](#platform-v1-AuditEvent) |  |  |






<a name="platform-v1-AuditQueryServiceQueryAuditEventsRequest"></a>

### AuditQueryServiceQueryAuditEventsRequest
AuditQueryServiceQueryAuditEventsRequest filters the local audit buffer.
All filters are AND-combined. Empty filters match everything.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| start_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) | optional |  |
| end_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) | optional |  |
| action_prefix | [string](#string) | repeated | Match any action with one of these prefixes, e.g. &#34;auth.&#34; or &#34;identity.account.&#34;. |
| actor_id | [string](#string) | optional |  |
| subject_id | [string](#string) | optional |  |
| source_ip_cidr | [string](#string) | optional | CIDR string, e.g. &#34;203.0.113.0/24&#34;. |
| decision | [string](#string) | optional | &#34;allow&#34; | &#34;deny&#34; | &#34;partial&#34;. |
| min_risk_score | [int32](#int32) | optional |  |
| page_size | [int32](#int32) |  |  |
| page_token | [string](#string) |  |  |






<a name="platform-v1-AuditQueryServiceQueryAuditEventsResponse"></a>

### AuditQueryServiceQueryAuditEventsResponse
AuditQueryServiceQueryAuditEventsResponse returns a page of events plus the
earliest timestamp still in the local buffer.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| events | [AuditEvent](#platform-v1-AuditEvent) | repeated |  |
| next_page_token | [string](#string) |  |  |
| retention_horizon | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | Earliest emitted_at the local buffer still retains. Events older than this require AOAudit-side lookup. |





 

 

 


<a name="platform-v1-AuditQueryService"></a>

### AuditQueryService
AuditQueryService surfaces tenant-scoped audit events to admins (MGMT-06).
Reads from the local aoid.audit_buffer; events older than the buffer
retention window MUST be queried via AOAudit directly (this service returns
what&#39;s available locally and includes a `retention_horizon` hint).

Every successful call emits a self-audit event (identity.audit_query.read).

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| QueryAuditEvents | [AuditQueryServiceQueryAuditEventsRequest](#platform-v1-AuditQueryServiceQueryAuditEventsRequest) | [AuditQueryServiceQueryAuditEventsResponse](#platform-v1-AuditQueryServiceQueryAuditEventsResponse) | QueryAuditEvents returns a page of audit events matching the supplied filter. All filter fields are optional except tenant_id. |
| GetAuditEvent | [AuditQueryServiceGetAuditEventRequest](#platform-v1-AuditQueryServiceGetAuditEventRequest) | [AuditQueryServiceGetAuditEventResponse](#platform-v1-AuditQueryServiceGetAuditEventResponse) | GetAuditEvent returns a single audit event by event_id (jti). |

 



<a name="platform_v1_auth-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## platform/v1/auth.proto



<a name="platform-v1-AuthData"></a>

### AuthData
AuthData is the shared authentication payload returned by SignUp,
Login, and RefreshToken. Wrapped in per-RPC response types per
buf STANDARD lint convention (RPC_REQUEST_RESPONSE_UNIQUE).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| access_token | [string](#string) |  |  |
| refresh_token | [string](#string) |  |  |
| user | [User](#platform-v1-User) |  |  |






<a name="platform-v1-InitiateOIDCRequest"></a>

### InitiateOIDCRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| company_id | [string](#string) |  |  |






<a name="platform-v1-InitiateOIDCResponse"></a>

### InitiateOIDCResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| auth_url | [string](#string) |  |  |
| state | [string](#string) |  |  |






<a name="platform-v1-InitiateSAMLRequest"></a>

### InitiateSAMLRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| company_id | [string](#string) |  |  |






<a name="platform-v1-InitiateSAMLResponse"></a>

### InitiateSAMLResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| redirect_url | [string](#string) |  |  |






<a name="platform-v1-InitiateSocialLoginRequest"></a>

### InitiateSocialLoginRequest
InitiateSocialLoginRequest starts a consumer social-login flow. provider is
one of &#34;google&#34;|&#34;apple&#34;|&#34;microsoft&#34;|&#34;facebook&#34;|&#34;x&#34;. redirect_uri is where the
user is sent after auth (Flutter deep-link or web origin) — it MUST match the
server&#39;s redirect allowlist.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| provider | [string](#string) |  |  |
| redirect_uri | [string](#string) |  |  |






<a name="platform-v1-InitiateSocialLoginResponse"></a>

### InitiateSocialLoginResponse
InitiateSocialLoginResponse carries the provider authorization URL the client
must open, plus the opaque state JWT for client-side correlation. Tokens are
NOT returned here — the HTTP callback delivers them via redirect.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| auth_url | [string](#string) |  |  |
| state | [string](#string) |  |  |






<a name="platform-v1-LoginRequest"></a>

### LoginRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| email | [string](#string) |  |  |
| password | [string](#string) |  |  |






<a name="platform-v1-LoginResponse"></a>

### LoginResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| auth | [AuthData](#platform-v1-AuthData) |  |  |






<a name="platform-v1-LogoutRequest"></a>

### LogoutRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| refresh_token | [string](#string) |  |  |






<a name="platform-v1-LogoutResponse"></a>

### LogoutResponse







<a name="platform-v1-RefreshTokenRequest"></a>

### RefreshTokenRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| refresh_token | [string](#string) |  |  |






<a name="platform-v1-RefreshTokenResponse"></a>

### RefreshTokenResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| auth | [AuthData](#platform-v1-AuthData) |  |  |






<a name="platform-v1-SignUpRequest"></a>

### SignUpRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| email | [string](#string) |  |  |
| password | [string](#string) |  |  |
| display_name | [string](#string) |  |  |






<a name="platform-v1-SignUpResponse"></a>

### SignUpResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| auth | [AuthData](#platform-v1-AuthData) |  |  |






<a name="platform-v1-UpdateProfileRequest"></a>

### UpdateProfileRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| display_name | [string](#string) |  |  |
| avatar_url | [string](#string) |  |  |






<a name="platform-v1-UpdateProfileResponse"></a>

### UpdateProfileResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| user | [User](#platform-v1-User) |  |  |






<a name="platform-v1-User"></a>

### User



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  |  |
| email | [string](#string) |  |  |
| display_name | [string](#string) |  |  |
| is_active | [bool](#bool) |  |  |
| created_at | [string](#string) |  |  |
| avatar_url | [string](#string) |  |  |





 

 

 


<a name="platform-v1-AuthService"></a>

### AuthService
AuthService handles authentication operations.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| SignUp | [SignUpRequest](#platform-v1-SignUpRequest) | [SignUpResponse](#platform-v1-SignUpResponse) |  |
| Login | [LoginRequest](#platform-v1-LoginRequest) | [LoginResponse](#platform-v1-LoginResponse) |  |
| RefreshToken | [RefreshTokenRequest](#platform-v1-RefreshTokenRequest) | [RefreshTokenResponse](#platform-v1-RefreshTokenResponse) |  |
| Logout | [LogoutRequest](#platform-v1-LogoutRequest) | [LogoutResponse](#platform-v1-LogoutResponse) |  |
| InitiateOIDC | [InitiateOIDCRequest](#platform-v1-InitiateOIDCRequest) | [InitiateOIDCResponse](#platform-v1-InitiateOIDCResponse) |  |
| InitiateSAML | [InitiateSAMLRequest](#platform-v1-InitiateSAMLRequest) | [InitiateSAMLResponse](#platform-v1-InitiateSAMLResponse) |  |
| InitiateSocialLogin | [InitiateSocialLoginRequest](#platform-v1-InitiateSocialLoginRequest) | [InitiateSocialLoginResponse](#platform-v1-InitiateSocialLoginResponse) | InitiateSocialLogin starts a consumer social-login flow (Google, Apple, Microsoft, Facebook, X). User-scoped — NO company_id. Tokens are delivered out-of-band by the GET /auth/social/callback HTTP handler via redirect. |
| UpdateProfile | [UpdateProfileRequest](#platform-v1-UpdateProfileRequest) | [UpdateProfileResponse](#platform-v1-UpdateProfileResponse) |  |

 



<a name="platform_v1_authn-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## platform/v1/authn.proto



<a name="platform-v1-AuthnServiceLogoutRequest"></a>

### AuthnServiceLogoutRequest
AuthnServiceLogoutRequest is service-prefixed to avoid collision with
auth.proto&#39;s LogoutRequest in the same package.






<a name="platform-v1-AuthnServiceLogoutResponse"></a>

### AuthnServiceLogoutResponse
AuthnServiceLogoutResponse is service-prefixed to avoid collision with
auth.proto&#39;s LogoutResponse in the same package.






<a name="platform-v1-BeginDiscoverableWebAuthnLoginRequest"></a>

### BeginDiscoverableWebAuthnLoginRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  | No email — passwordless username-less flow. |






<a name="platform-v1-BeginDiscoverableWebAuthnLoginResponse"></a>

### BeginDiscoverableWebAuthnLoginResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| options_json | [bytes](#bytes) |  |  |






<a name="platform-v1-BeginStepUpRequest"></a>

### BeginStepUpRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| stepup_token | [string](#string) |  | continuation token from the original error |






<a name="platform-v1-BeginStepUpResponse"></a>

### BeginStepUpResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| available_methods | [string](#string) | repeated | &#34;webauthn&#34;, &#34;piv&#34; |
| required_aal | [string](#string) |  |  |






<a name="platform-v1-BeginWebAuthnLoginRequest"></a>

### BeginWebAuthnLoginRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| email | [string](#string) |  |  |






<a name="platform-v1-BeginWebAuthnLoginResponse"></a>

### BeginWebAuthnLoginResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| options_json | [bytes](#bytes) |  |  |






<a name="platform-v1-BeginWebAuthnRegistrationRequest"></a>

### BeginWebAuthnRegistrationRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| label | [string](#string) |  |  |






<a name="platform-v1-BeginWebAuthnRegistrationResponse"></a>

### BeginWebAuthnRegistrationResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| options_json | [bytes](#bytes) |  | CredentialCreationOptions as raw JSON (browser-direct). |






<a name="platform-v1-ChangePasswordRequest"></a>

### ChangePasswordRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| current_password | [string](#string) |  |  |
| new_password | [string](#string) |  |  |






<a name="platform-v1-ChangePasswordResponse"></a>

### ChangePasswordResponse







<a name="platform-v1-CompleteTOTPEnrollRequest"></a>

### CompleteTOTPEnrollRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| credential_id | [string](#string) |  |  |
| verification_code | [string](#string) |  |  |






<a name="platform-v1-CompleteTOTPEnrollResponse"></a>

### CompleteTOTPEnrollResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| backup_codes | [string](#string) | repeated | plaintext — shown ONCE, never re-fetchable |






<a name="platform-v1-CredentialSummary"></a>

### CredentialSummary



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  |  |
| kind | [string](#string) |  | &#34;password&#34;, &#34;totp&#34;, &#34;webauthn&#34;, &#34;piv&#34; |
| label | [string](#string) |  |  |
| enrolled_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| last_used_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |






<a name="platform-v1-EnrollTOTPRequest"></a>

### EnrollTOTPRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| label | [string](#string) |  |  |






<a name="platform-v1-EnrollTOTPResponse"></a>

### EnrollTOTPResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| credential_id | [string](#string) |  |  |
| provisioning_uri | [string](#string) |  | otpauth://... — client QR-renders |
| secret | [string](#string) |  | raw base32 (for manual entry) |






<a name="platform-v1-FinishDiscoverableWebAuthnLoginRequest"></a>

### FinishDiscoverableWebAuthnLoginRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| assertion_response_json | [bytes](#bytes) |  |  |






<a name="platform-v1-FinishDiscoverableWebAuthnLoginResponse"></a>

### FinishDiscoverableWebAuthnLoginResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| next_step | [LoginNextStep](#platform-v1-LoginNextStep) |  |  |






<a name="platform-v1-FinishStepUpRequest"></a>

### FinishStepUpRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| stepup_token | [string](#string) |  |  |
| webauthn_assertion_json | [bytes](#bytes) |  |  |
| piv | [bool](#bool) |  | PIV requires the mTLS handshake — no inline assertion bytes needed |






<a name="platform-v1-FinishStepUpResponse"></a>

### FinishStepUpResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| new_aal | [string](#string) |  | Server side-effect: marks stepup_token as consumed; the original RPC is re-callable by the client with normal session credentials. |






<a name="platform-v1-FinishWebAuthnLoginRequest"></a>

### FinishWebAuthnLoginRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| assertion_response_json | [bytes](#bytes) |  |  |






<a name="platform-v1-FinishWebAuthnLoginResponse"></a>

### FinishWebAuthnLoginResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| next_step | [LoginNextStep](#platform-v1-LoginNextStep) |  |  |






<a name="platform-v1-FinishWebAuthnRegistrationRequest"></a>

### FinishWebAuthnRegistrationRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| attestation_response_json | [bytes](#bytes) |  | CredentialCreationResponse from browser (raw JSON). |






<a name="platform-v1-FinishWebAuthnRegistrationResponse"></a>

### FinishWebAuthnRegistrationResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| credential_id | [string](#string) |  |  |
| label | [string](#string) |  |  |






<a name="platform-v1-ListMyCredentialsRequest"></a>

### ListMyCredentialsRequest







<a name="platform-v1-ListMyCredentialsResponse"></a>

### ListMyCredentialsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| credentials | [CredentialSummary](#platform-v1-CredentialSummary) | repeated |  |






<a name="platform-v1-ListMySessionsRequest"></a>

### ListMySessionsRequest







<a name="platform-v1-ListMySessionsResponse"></a>

### ListMySessionsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| sessions | [SessionSummary](#platform-v1-SessionSummary) | repeated |  |






<a name="platform-v1-LoginNextStep"></a>

### LoginNextStep
LoginNextStep tells the client what authentication step is required next.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| kind | [string](#string) |  | &#34;mfa_required&#34; | &#34;session_established&#34; | &#34;stepup_required&#34; |
| available_methods | [string](#string) | repeated | &#34;totp&#34;, &#34;webauthn&#34;, &#34;backup_code&#34; |
| current_aal | [string](#string) |  | &#34;AAL1&#34;, &#34;AAL2&#34;, &#34;AAL3&#34; |
| required_aal | [string](#string) |  | for stepup |






<a name="platform-v1-LoginWithPIVRequest"></a>

### LoginWithPIVRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  | No additional fields — UPN extracted from mTLS client cert at the transport layer by TRD 03-07&#39;s PIV interceptor. |






<a name="platform-v1-LoginWithPIVResponse"></a>

### LoginWithPIVResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| next_step | [LoginNextStep](#platform-v1-LoginNextStep) |  |  |






<a name="platform-v1-LoginWithTOTPRequest"></a>

### LoginWithTOTPRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| code | [string](#string) |  | Implicit: requires PasswordLoginStart() to have set two_factor_user_id (cookie session-bound). |






<a name="platform-v1-LoginWithTOTPResponse"></a>

### LoginWithTOTPResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| next_step | [LoginNextStep](#platform-v1-LoginNextStep) |  |  |






<a name="platform-v1-PasswordLoginCompleteRequest"></a>

### PasswordLoginCompleteRequest
No body — session-bound. Returned only when no MFA is required AND
policy permits password-only login (rare; AAL1 only).






<a name="platform-v1-PasswordLoginCompleteResponse"></a>

### PasswordLoginCompleteResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| next_step | [LoginNextStep](#platform-v1-LoginNextStep) |  |  |






<a name="platform-v1-PasswordLoginStartRequest"></a>

### PasswordLoginStartRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| email | [string](#string) |  |  |
| password | [string](#string) |  |  |






<a name="platform-v1-PasswordLoginStartResponse"></a>

### PasswordLoginStartResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| next_step | [LoginNextStep](#platform-v1-LoginNextStep) |  |  |






<a name="platform-v1-ResolveWorkspace"></a>

### ResolveWorkspace



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| display_name | [string](#string) |  |  |






<a name="platform-v1-ResolveWorkspacesByEmailRequest"></a>

### ResolveWorkspacesByEmailRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| email | [string](#string) |  |  |






<a name="platform-v1-ResolveWorkspacesByEmailResponse"></a>

### ResolveWorkspacesByEmailResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| status | [ResolveStatus](#platform-v1-ResolveStatus) |  |  |
| workspaces | [ResolveWorkspace](#platform-v1-ResolveWorkspace) | repeated |  |






<a name="platform-v1-RevokeMyCredentialRequest"></a>

### RevokeMyCredentialRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| credential_id | [string](#string) |  |  |






<a name="platform-v1-RevokeMyCredentialResponse"></a>

### RevokeMyCredentialResponse







<a name="platform-v1-RevokeMySessionRequest"></a>

### RevokeMySessionRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| token_prefix | [string](#string) |  | matched via prefix; current session cannot self-revoke |






<a name="platform-v1-RevokeMySessionResponse"></a>

### RevokeMySessionResponse







<a name="platform-v1-SessionSummary"></a>

### SessionSummary



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| token_prefix | [string](#string) |  | first 8 chars of session token for display |
| ip | [string](#string) |  |  |
| user_agent | [string](#string) |  |  |
| aal | [string](#string) |  |  |
| auth_methods | [string](#string) | repeated |  |
| created_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| last_active_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| expires_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| is_current | [bool](#bool) |  |  |






<a name="platform-v1-WhoAmIRequest"></a>

### WhoAmIRequest







<a name="platform-v1-WhoAmIResponse"></a>

### WhoAmIResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| user_id | [string](#string) |  |  |
| tenant_id | [string](#string) |  |  |
| email | [string](#string) |  |  |
| display_name | [string](#string) |  |  |
| aal | [string](#string) |  |  |
| auth_methods | [string](#string) | repeated |  |
| session_expires_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |





 


<a name="platform-v1-ResolveStatus"></a>

### ResolveStatus


| Name | Number | Description |
| ---- | ------ | ----------- |
| RESOLVE_STATUS_UNSPECIFIED | 0 |  |
| RESOLVE_STATUS_ONE | 1 |  |
| RESOLVE_STATUS_MANY | 2 |  |
| RESOLVE_STATUS_NONE | 3 |  |


 

 


<a name="platform-v1-AuthnService"></a>

### AuthnService
AuthnService provides end-user authentication primitives — password,
TOTP, WebAuthn (registration &#43; login &#43; passwordless discoverable login),
PIV/CAC mTLS login, credential management, and step-up assurance flows.

Tenant scope: login-initiating RPCs (PasswordLoginStart, LoginWith*)
require tenant_id in the request body because no session exists yet.
Once a session is established, tenant is read from the session cookie
by the Connect interceptor and is NOT accepted as a body field.

Step-up: when a downstream service decides the current AAL is
insufficient, it returns a CodePermissionDenied error with a &#34;stepup_token&#34;
header. The client calls BeginStepUp(stepup_token), completes the
challenge, calls FinishStepUp, then retries the original RPC.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| ResolveWorkspacesByEmail | [ResolveWorkspacesByEmailRequest](#platform-v1-ResolveWorkspacesByEmailRequest) | [ResolveWorkspacesByEmailResponse](#platform-v1-ResolveWorkspacesByEmailResponse) | === Pre-login workspace resolution (Obj 16 / LOGIN-01) === UNAUTHENTICATED, pre-login. Given an email, returns the workspaces (tenants) that email&#39;s identity may sign in to, so the client can run the email-first two-step flow. Enumeration-safe: a non-existent email and a legacy/unlinked email are INDISTINGUISHABLE (both RESOLVE_NONE). |
| PasswordLoginStart | [PasswordLoginStartRequest](#platform-v1-PasswordLoginStartRequest) | [PasswordLoginStartResponse](#platform-v1-PasswordLoginStartResponse) | === Password flow === |
| PasswordLoginComplete | [PasswordLoginCompleteRequest](#platform-v1-PasswordLoginCompleteRequest) | [PasswordLoginCompleteResponse](#platform-v1-PasswordLoginCompleteResponse) |  |
| ChangePassword | [ChangePasswordRequest](#platform-v1-ChangePasswordRequest) | [ChangePasswordResponse](#platform-v1-ChangePasswordResponse) |  |
| EnrollTOTP | [EnrollTOTPRequest](#platform-v1-EnrollTOTPRequest) | [EnrollTOTPResponse](#platform-v1-EnrollTOTPResponse) | === TOTP flow === |
| CompleteTOTPEnroll | [CompleteTOTPEnrollRequest](#platform-v1-CompleteTOTPEnrollRequest) | [CompleteTOTPEnrollResponse](#platform-v1-CompleteTOTPEnrollResponse) |  |
| LoginWithTOTP | [LoginWithTOTPRequest](#platform-v1-LoginWithTOTPRequest) | [LoginWithTOTPResponse](#platform-v1-LoginWithTOTPResponse) |  |
| BeginWebAuthnRegistration | [BeginWebAuthnRegistrationRequest](#platform-v1-BeginWebAuthnRegistrationRequest) | [BeginWebAuthnRegistrationResponse](#platform-v1-BeginWebAuthnRegistrationResponse) | === WebAuthn flow === |
| FinishWebAuthnRegistration | [FinishWebAuthnRegistrationRequest](#platform-v1-FinishWebAuthnRegistrationRequest) | [FinishWebAuthnRegistrationResponse](#platform-v1-FinishWebAuthnRegistrationResponse) |  |
| BeginWebAuthnLogin | [BeginWebAuthnLoginRequest](#platform-v1-BeginWebAuthnLoginRequest) | [BeginWebAuthnLoginResponse](#platform-v1-BeginWebAuthnLoginResponse) |  |
| FinishWebAuthnLogin | [FinishWebAuthnLoginRequest](#platform-v1-FinishWebAuthnLoginRequest) | [FinishWebAuthnLoginResponse](#platform-v1-FinishWebAuthnLoginResponse) |  |
| BeginDiscoverableWebAuthnLogin | [BeginDiscoverableWebAuthnLoginRequest](#platform-v1-BeginDiscoverableWebAuthnLoginRequest) | [BeginDiscoverableWebAuthnLoginResponse](#platform-v1-BeginDiscoverableWebAuthnLoginResponse) |  |
| FinishDiscoverableWebAuthnLogin | [FinishDiscoverableWebAuthnLoginRequest](#platform-v1-FinishDiscoverableWebAuthnLoginRequest) | [FinishDiscoverableWebAuthnLoginResponse](#platform-v1-FinishDiscoverableWebAuthnLoginResponse) |  |
| LoginWithPIV | [LoginWithPIVRequest](#platform-v1-LoginWithPIVRequest) | [LoginWithPIVResponse](#platform-v1-LoginWithPIVResponse) | === PIV/CAC flow === The actual mTLS handshake happens at the transport layer; this RPC tells the server &#34;I&#39;m here on mTLS — please establish my session.&#34; |
| BeginStepUp | [BeginStepUpRequest](#platform-v1-BeginStepUpRequest) | [BeginStepUpResponse](#platform-v1-BeginStepUpResponse) | === Step-up flow === |
| FinishStepUp | [FinishStepUpRequest](#platform-v1-FinishStepUpRequest) | [FinishStepUpResponse](#platform-v1-FinishStepUpResponse) |  |
| Logout | [AuthnServiceLogoutRequest](#platform-v1-AuthnServiceLogoutRequest) | [AuthnServiceLogoutResponse](#platform-v1-AuthnServiceLogoutResponse) | === Logout &#43; credential mgmt === Logout uses service-prefixed message names to avoid collision with the existing auth.proto&#39;s LogoutRequest/LogoutResponse in the same package. |
| ListMyCredentials | [ListMyCredentialsRequest](#platform-v1-ListMyCredentialsRequest) | [ListMyCredentialsResponse](#platform-v1-ListMyCredentialsResponse) |  |
| RevokeMyCredential | [RevokeMyCredentialRequest](#platform-v1-RevokeMyCredentialRequest) | [RevokeMyCredentialResponse](#platform-v1-RevokeMyCredentialResponse) |  |


<a name="platform-v1-EndUserSessionService"></a>

### EndUserSessionService
EndUserSessionService provides introspection over the current session.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| WhoAmI | [WhoAmIRequest](#platform-v1-WhoAmIRequest) | [WhoAmIResponse](#platform-v1-WhoAmIResponse) |  |
| ListMySessions | [ListMySessionsRequest](#platform-v1-ListMySessionsRequest) | [ListMySessionsResponse](#platform-v1-ListMySessionsResponse) |  |
| RevokeMySession | [RevokeMySessionRequest](#platform-v1-RevokeMySessionRequest) | [RevokeMySessionResponse](#platform-v1-RevokeMySessionResponse) |  |

 



<a name="platform_v1_bridge-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## platform/v1/bridge.proto



<a name="platform-v1-ActionSchema"></a>

### ActionSchema



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| type | [string](#string) |  |  |
| label | [string](#string) |  |  |
| requires_input | [bool](#bool) |  |  |
| input_hint | [string](#string) |  |  |
| destructive | [bool](#bool) |  |  |






<a name="platform-v1-AdapterInfo"></a>

### AdapterInfo



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| prefix | [string](#string) |  |  |
| event_types | [string](#string) | repeated |  |
| actions | [ActionSchema](#platform-v1-ActionSchema) | repeated |  |






<a name="platform-v1-DispatchActionRequest"></a>

### DispatchActionRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| action_type | [string](#string) |  |  |
| source_id | [string](#string) |  |  |
| company_id | [string](#string) |  |  |
| input | [string](#string) |  |  |






<a name="platform-v1-DispatchActionResponse"></a>

### DispatchActionResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  |  |
| message | [string](#string) |  |  |






<a name="platform-v1-ListActionsRequest"></a>

### ListActionsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| event_type | [string](#string) |  |  |






<a name="platform-v1-ListActionsResponse"></a>

### ListActionsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| actions | [ActionSchema](#platform-v1-ActionSchema) | repeated |  |






<a name="platform-v1-ListAdaptersRequest"></a>

### ListAdaptersRequest







<a name="platform-v1-ListAdaptersResponse"></a>

### ListAdaptersResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| adapters | [AdapterInfo](#platform-v1-AdapterInfo) | repeated |  |





 

 

 


<a name="platform-v1-BridgeService"></a>

### BridgeService
BridgeService handles platform event bridge operations.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| ListAdapters | [ListAdaptersRequest](#platform-v1-ListAdaptersRequest) | [ListAdaptersResponse](#platform-v1-ListAdaptersResponse) |  |
| DispatchAction | [DispatchActionRequest](#platform-v1-DispatchActionRequest) | [DispatchActionResponse](#platform-v1-DispatchActionResponse) |  |
| ListActions | [ListActionsRequest](#platform-v1-ListActionsRequest) | [ListActionsResponse](#platform-v1-ListActionsResponse) |  |

 



<a name="platform_v1_company-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## platform/v1/company.proto



<a name="platform-v1-CompanyData"></a>

### CompanyData
CompanyData is the shared company payload returned by Create/Get/Update
(wrapped in per-RPC responses) and contained in List/GetAncestors/
GetDescendants list responses. Per buf STANDARD lint convention
(RPC_REQUEST_RESPONSE_UNIQUE).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  |  |
| name | [string](#string) |  |  |
| slug | [string](#string) |  |  |
| company_type | [string](#string) |  |  |
| parent_company_id | [string](#string) | optional |  |
| inherited_role_cap | [int32](#int32) | optional |  |
| inherited_access_level | [string](#string) | optional |  |
| settings_json | [string](#string) |  |  |
| is_active | [bool](#bool) |  |  |
| created_at | [string](#string) |  |  |
| updated_at | [string](#string) |  |  |






<a name="platform-v1-CreateCompanyRequest"></a>

### CreateCompanyRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| slug | [string](#string) |  |  |
| company_type | [string](#string) |  | holding, subsidiary, brand, standalone |
| parent_company_id | [string](#string) | optional |  |
| settings_json | [string](#string) | optional |  |






<a name="platform-v1-CreateCompanyResponse"></a>

### CreateCompanyResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| company | [CompanyData](#platform-v1-CompanyData) |  |  |






<a name="platform-v1-GetAncestorsRequest"></a>

### GetAncestorsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| company_id | [string](#string) |  |  |






<a name="platform-v1-GetAncestorsResponse"></a>

### GetAncestorsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| companies | [CompanyData](#platform-v1-CompanyData) | repeated |  |






<a name="platform-v1-GetCompanyRequest"></a>

### GetCompanyRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  |  |






<a name="platform-v1-GetCompanyResponse"></a>

### GetCompanyResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| company | [CompanyData](#platform-v1-CompanyData) |  |  |






<a name="platform-v1-GetDescendantsRequest"></a>

### GetDescendantsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| company_id | [string](#string) |  |  |






<a name="platform-v1-GetDescendantsResponse"></a>

### GetDescendantsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| companies | [CompanyData](#platform-v1-CompanyData) | repeated |  |






<a name="platform-v1-GetEffectiveSettingsRequest"></a>

### GetEffectiveSettingsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| company_id | [string](#string) |  |  |






<a name="platform-v1-GetEffectiveSettingsResponse"></a>

### GetEffectiveSettingsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| settings_json | [string](#string) |  |  |






<a name="platform-v1-ListCompaniesRequest"></a>

### ListCompaniesRequest







<a name="platform-v1-ListCompaniesResponse"></a>

### ListCompaniesResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| companies | [CompanyData](#platform-v1-CompanyData) | repeated |  |






<a name="platform-v1-UpdateCompanyRequest"></a>

### UpdateCompanyRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  |  |
| name | [string](#string) |  |  |
| slug | [string](#string) |  |  |
| company_type | [string](#string) |  |  |
| inherited_role_cap | [int32](#int32) | optional |  |
| inherited_access_level | [string](#string) | optional |  |
| settings_json | [string](#string) | optional |  |






<a name="platform-v1-UpdateCompanyResponse"></a>

### UpdateCompanyResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| company | [CompanyData](#platform-v1-CompanyData) |  |  |





 

 

 


<a name="platform-v1-CompanyService"></a>

### CompanyService
CompanyService manages company hierarchy.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| CreateCompany | [CreateCompanyRequest](#platform-v1-CreateCompanyRequest) | [CreateCompanyResponse](#platform-v1-CreateCompanyResponse) |  |
| GetCompany | [GetCompanyRequest](#platform-v1-GetCompanyRequest) | [GetCompanyResponse](#platform-v1-GetCompanyResponse) |  |
| UpdateCompany | [UpdateCompanyRequest](#platform-v1-UpdateCompanyRequest) | [UpdateCompanyResponse](#platform-v1-UpdateCompanyResponse) |  |
| ListCompanies | [ListCompaniesRequest](#platform-v1-ListCompaniesRequest) | [ListCompaniesResponse](#platform-v1-ListCompaniesResponse) |  |
| GetAncestors | [GetAncestorsRequest](#platform-v1-GetAncestorsRequest) | [GetAncestorsResponse](#platform-v1-GetAncestorsResponse) |  |
| GetDescendants | [GetDescendantsRequest](#platform-v1-GetDescendantsRequest) | [GetDescendantsResponse](#platform-v1-GetDescendantsResponse) |  |
| GetEffectiveSettings | [GetEffectiveSettingsRequest](#platform-v1-GetEffectiveSettingsRequest) | [GetEffectiveSettingsResponse](#platform-v1-GetEffectiveSettingsResponse) |  |

 



<a name="platform_v1_credential_admin-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## platform/v1/credential_admin.proto



<a name="platform-v1-ApiKey"></a>

### ApiKey
ApiKey is the canonical wire shape for an API key record. NO raw_key
field — that material is carried ONLY by MintApiKeyResponse.raw_key
and RotateApiKeyResponse.raw_key, each emitted EXACTLY ONCE.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  | Internal database UUID (primary key). |
| tenant_id | [string](#string) |  | Owning tenant UUID. |
| owner_account_id | [string](#string) |  | Owner account UUID (the account this key acts on behalf of). |
| key_prefix | [string](#string) |  | First 12 chars of the raw key (e.g. &#34;aoid_prod_AB&#34;). Indexed for O(1) lookup at validation time; safe to log &#43; display. |
| name | [string](#string) |  | Human-friendly label supplied at mint time. 1..255 chars; no control chars (server strips them via strings.Map at mint time). |
| scopes | [string](#string) | repeated | Allowed OAuth-shaped scopes (e.g. &#34;read&#34;, &#34;admin:tenant&#34;). |
| issued_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | Issuance timestamp. |
| expires_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | Optional expiry. Absent = never expires. |
| last_used_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | Last successful validation timestamp. Updated cheaply on every ValidateApiKey hit; race-y is acceptable (audit hint, not a security boundary). |
| revoked_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | Revocation timestamp. Absent = active. |
| revoked_reason | [string](#string) |  | Human-readable revocation reason. Empty when revoked_at is absent. |






<a name="platform-v1-CertificateSummary"></a>

### CertificateSummary
CertificateSummary is the wire shape for a single cert row. The
underlying X.509 DER is returned verbatim in cert_der; PEM is
generated on the wire for caller convenience but never persisted
as such (DER is the storage canon).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| cert_id | [string](#string) |  | UUID |
| tenant_id | [string](#string) |  |  |
| owner_account_id | [string](#string) |  | empty for workload |
| serial_number | [string](#string) |  |  |
| subject_dn | [string](#string) |  |  |
| san_uri | [string](#string) | repeated |  |
| san_dns | [string](#string) | repeated |  |
| cert_der | [bytes](#bytes) |  |  |
| issued_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| expires_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| kind | [string](#string) |  |  |
| status | [string](#string) |  | &#39;active&#39; | &#39;revoked&#39; | &#39;expired&#39; |
| revoked_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| revoked_reason | [int32](#int32) |  |  |






<a name="platform-v1-CredentialAdminServiceGetApiKeyRequest"></a>

### CredentialAdminServiceGetApiKeyRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| api_key_id | [string](#string) |  |  |






<a name="platform-v1-CredentialAdminServiceGetApiKeyResponse"></a>

### CredentialAdminServiceGetApiKeyResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| api_key | [ApiKey](#platform-v1-ApiKey) |  |  |






<a name="platform-v1-CredentialAdminServiceListApiKeysRequest"></a>

### CredentialAdminServiceListApiKeysRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| owner_account_id | [string](#string) |  | optional filter |
| page_size | [int32](#int32) |  |  |
| page_token | [string](#string) |  |  |






<a name="platform-v1-CredentialAdminServiceListApiKeysResponse"></a>

### CredentialAdminServiceListApiKeysResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| api_keys | [ApiKey](#platform-v1-ApiKey) | repeated |  |
| next_page_token | [string](#string) |  |  |






<a name="platform-v1-CredentialAdminServiceMintApiKeyRequest"></a>

### CredentialAdminServiceMintApiKeyRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| owner_account_id | [string](#string) |  |  |
| name | [string](#string) |  |  |
| scopes | [string](#string) | repeated |  |
| expires_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | optional |






<a name="platform-v1-CredentialAdminServiceMintApiKeyResponse"></a>

### CredentialAdminServiceMintApiKeyResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| api_key | [ApiKey](#platform-v1-ApiKey) |  |  |
| raw_key | [string](#string) |  | RAW key material. SHOWN EXACTLY ONCE. |






<a name="platform-v1-CredentialAdminServiceRevokeApiKeyRequest"></a>

### CredentialAdminServiceRevokeApiKeyRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| api_key_id | [string](#string) |  |  |
| reason | [string](#string) |  | human-readable; recorded on the row |






<a name="platform-v1-CredentialAdminServiceRevokeApiKeyResponse"></a>

### CredentialAdminServiceRevokeApiKeyResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| api_key | [ApiKey](#platform-v1-ApiKey) |  |  |






<a name="platform-v1-CredentialAdminServiceRotateApiKeyRequest"></a>

### CredentialAdminServiceRotateApiKeyRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| api_key_id | [string](#string) |  |  |
| reason | [string](#string) |  |  |






<a name="platform-v1-CredentialAdminServiceRotateApiKeyResponse"></a>

### CredentialAdminServiceRotateApiKeyResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| revoked | [ApiKey](#platform-v1-ApiKey) |  | Revoked predecessor record. |
| api_key | [ApiKey](#platform-v1-ApiKey) |  | Newly minted replacement record. |
| raw_key | [string](#string) |  | RAW replacement key. SHOWN EXACTLY ONCE. |






<a name="platform-v1-GetCertificateRequest"></a>

### GetCertificateRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| cert_id | [string](#string) |  |  |






<a name="platform-v1-GetCertificateResponse"></a>

### GetCertificateResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| cert | [CertificateSummary](#platform-v1-CertificateSummary) |  |  |






<a name="platform-v1-IssueCertificateRequest"></a>

### IssueCertificateRequest
IssueCertificateRequest carries the admin-controlled inputs for
IssueCertificate. CSR is PEM-encoded (PKCS#10); the server parses to
DER, verifies the self-signature, then applies any override fields
before signing.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| owner_account_id | [string](#string) |  | optional; empty for workload certs |
| csr_pem | [bytes](#bytes) |  | PEM-encoded CSR (PKCS#10) |
| subject_override | [string](#string) |  | optional RFC 2253 DN; overrides CSR |
| san_dns_override | [string](#string) | repeated |  |
| san_uri_override | [string](#string) | repeated |  |
| validity_days | [int32](#int32) |  | optional; server clamps to per-kind max |
| kind | [string](#string) |  | &#39;client&#39; | &#39;server&#39; | &#39;workload&#39; |






<a name="platform-v1-IssueCertificateResponse"></a>

### IssueCertificateResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| cert | [CertificateSummary](#platform-v1-CertificateSummary) |  |  |
| cert_pem | [bytes](#bytes) |  | convenience PEM-encoded form |






<a name="platform-v1-ListCertificatesRequest"></a>

### ListCertificatesRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| page_size | [int32](#int32) |  |  |
| page_token | [string](#string) |  |  |






<a name="platform-v1-ListCertificatesResponse"></a>

### ListCertificatesResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| certs | [CertificateSummary](#platform-v1-CertificateSummary) | repeated |  |
| next_page_token | [string](#string) |  |  |






<a name="platform-v1-RenewCertificateRequest"></a>

### RenewCertificateRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| cert_id | [string](#string) |  | UUID; previous cert (for audit cross-reference) |
| csr_pem | [bytes](#bytes) |  |  |
| validity_days | [int32](#int32) |  |  |






<a name="platform-v1-RenewCertificateResponse"></a>

### RenewCertificateResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| cert | [CertificateSummary](#platform-v1-CertificateSummary) |  |  |
| cert_pem | [bytes](#bytes) |  |  |






<a name="platform-v1-RevokeCertificateRequest"></a>

### RevokeCertificateRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| cert_id | [string](#string) |  |  |
| reason | [int32](#int32) |  | RFC 5280 §5.3.1 reason code 0-10 |






<a name="platform-v1-RevokeCertificateResponse"></a>

### RevokeCertificateResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| cert | [CertificateSummary](#platform-v1-CertificateSummary) |  |  |





 

 

 


<a name="platform-v1-CredentialAdminService"></a>

### CredentialAdminService
CredentialAdminService manages non-OAuth credentials issued by AOID:

  - API keys (TRD 05-05): tenant-scoped opaque bearer tokens for
    server-to-server authentication. Prefix-lookup &#43; Argon2id/PBKDF2
    verification.
  - mTLS client &#43; workload certificates (TRD 05-06): leaf X.509 certs
    signed by AOID&#39;s intermediate CA (root resides in HSM, intermediate
    wrapped via KMS:Encrypt). CSR-based issuance with admin override
    for SAN/Subject. Renewal is policy-neutral (does NOT auto-revoke
    old cert). Revocation atomically flips status &#43; appends to
    aoid.revocations table &#43; notifies the CRL refresh loop.
  - SPIFFE workload SVIDs (TRD 05-07): short-lived workload certs
    issued directly by pubkey (no CSR), 30-day max TTL.

All RPCs share the same Connect handler so admin tooling reaches one
canonical surface for tenant credential lifecycle. Tenant scoping &#43;
audit emission match the AccountAdminService / OAuthAdminService
precedent: every request carries tenant_id as field 1; super admins
may target any tenant; mutations emit platform/audit.Event with the
action constants defined in TRD 05-05 / 05-06 / 05-07.

NOTE (cross-TRD coordination): This file is APPEND-ONLY. TRD 05-05
(apikey) and TRD 05-07 (SVID) extend this service with additional
RPCs; the contract is &#34;first writer creates the file, later writers
append within the same `service CredentialAdminService { ... }`
block.&#34;

---------- mTLS client &#43; workload certificates (CRED-06) ----------

Implemented by TRD 05-06 — see /Users/justin/dev/aoid/internal/pki/.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| IssueCertificate | [IssueCertificateRequest](#platform-v1-IssueCertificateRequest) | [IssueCertificateResponse](#platform-v1-IssueCertificateResponse) | IssueCertificate signs a leaf cert from a caller-supplied CSR. kind: &#39;client&#39; | &#39;server&#39; | &#39;workload&#39;. Workload certs MAY also be issued via the dedicated SVID flow (TRD 05-07) for tighter TTL controls. |
| RenewCertificate | [RenewCertificateRequest](#platform-v1-RenewCertificateRequest) | [RenewCertificateResponse](#platform-v1-RenewCertificateResponse) | RenewCertificate signs a new cert based on a fresh CSR. Old cert is NOT auto-revoked; admin should explicitly revoke if needed. |
| RevokeCertificate | [RevokeCertificateRequest](#platform-v1-RevokeCertificateRequest) | [RevokeCertificateResponse](#platform-v1-RevokeCertificateResponse) | RevokeCertificate marks the cert revoked and adds the serial to the revocation table; CRL refresh &#43; OCSP responder pick up immediately. |
| GetCertificate | [GetCertificateRequest](#platform-v1-GetCertificateRequest) | [GetCertificateResponse](#platform-v1-GetCertificateResponse) | GetCertificate returns metadata for one cert. |
| ListCertificates | [ListCertificatesRequest](#platform-v1-ListCertificatesRequest) | [ListCertificatesResponse](#platform-v1-ListCertificatesResponse) | ListCertificates returns certs in a tenant. |
| MintApiKey | [CredentialAdminServiceMintApiKeyRequest](#platform-v1-CredentialAdminServiceMintApiKeyRequest) | [CredentialAdminServiceMintApiKeyResponse](#platform-v1-CredentialAdminServiceMintApiKeyResponse) | MintApiKey creates a new API key. The raw key is returned EXACTLY ONCE in the response and is never persisted in plaintext. The server generates a 32-byte CSPRNG suffix, encodes as Crockford base32, prepends &#34;aoid_&lt;env&gt;_&#34;, hashes via platform/auth/secrethasher, and stores the prefix &#43; hash. |
| ListApiKeys | [CredentialAdminServiceListApiKeysRequest](#platform-v1-CredentialAdminServiceListApiKeysRequest) | [CredentialAdminServiceListApiKeysResponse](#platform-v1-CredentialAdminServiceListApiKeysResponse) | ListApiKeys returns API keys in the given tenant (admin scope) or optionally narrows to a single owner. |
| GetApiKey | [CredentialAdminServiceGetApiKeyRequest](#platform-v1-CredentialAdminServiceGetApiKeyRequest) | [CredentialAdminServiceGetApiKeyResponse](#platform-v1-CredentialAdminServiceGetApiKeyResponse) | GetApiKey returns metadata for one API key (NOT the raw key — that is only available in the MintApiKey / RotateApiKey responses). |
| RevokeApiKey | [CredentialAdminServiceRevokeApiKeyRequest](#platform-v1-CredentialAdminServiceRevokeApiKeyRequest) | [CredentialAdminServiceRevokeApiKeyResponse](#platform-v1-CredentialAdminServiceRevokeApiKeyResponse) | RevokeApiKey marks an API key as revoked. Subsequent validation calls for the same prefix return reason=&#34;unknown&#34; (we do not leak revocation state to validation callers). |
| RotateApiKey | [CredentialAdminServiceRotateApiKeyRequest](#platform-v1-CredentialAdminServiceRotateApiKeyRequest) | [CredentialAdminServiceRotateApiKeyResponse](#platform-v1-CredentialAdminServiceRotateApiKeyResponse) | RotateApiKey is a convenience composition: revoke the supplied key and mint a fresh one in the same tenant &#43; owner, preserving the scopes &#43; name. The new raw_key appears EXACTLY ONCE in the response. |

 



<a name="platform_v1_federation_types-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## platform/v1/federation_types.proto



<a name="platform-v1-AttributeMapping"></a>

### AttributeMapping
AttributeMapping describes how to extract AOID account fields from an
upstream IdP&#39;s claims / SAML attributes. Paths are dotted JSON-path-lite
(e.g. &#34;email&#34;, &#34;user.email&#34;, &#34;profile.name&#34;). Matches the shape consumed
by Eden oidcrp.ClaimMap (TRD 06-02) and the SAML attribute extractor
(TRD 06-01).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  | id is the mapping UUID. |
| tenant_id | [string](#string) |  | tenant_id is the owning tenant UUID. |
| name | [string](#string) |  | name is the admin-facing label. |
| email_path | [string](#string) |  | email_path locates the user&#39;s email in the upstream payload. |
| email_verified_path | [string](#string) |  | email_verified_path locates the boolean &#34;email verified&#34; signal. Empty means AOID treats the upstream email as verified. |
| sub_path | [string](#string) |  | sub_path locates the upstream stable subject identifier. Required. |
| preferred_username_path | [string](#string) |  | preferred_username_path locates the preferred username (optional). |
| name_path | [string](#string) |  | name_path locates the display name (optional). |
| groups_path | [string](#string) |  | groups_path locates a list-typed groups claim/attribute (optional). |
| assurance_level_path | [string](#string) |  | assurance_level_path locates an upstream-asserted assurance level (e.g. acr, amr; optional). When present, the value passes through the federation policy&#39;s assurance_level_override gate. |
| custom_attrs | [AttributeMapping.CustomAttrsEntry](#platform-v1-AttributeMapping-CustomAttrsEntry) | repeated | custom_attrs maps AOID-side attribute names → upstream paths for tenant-specific extensions (e.g. {&#34;clearance&#34;: &#34;extensions.clearance&#34;}). |
| created_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | created_at is when the mapping was created. |
| updated_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | updated_at is the last mutation timestamp. |






<a name="platform-v1-AttributeMapping-CustomAttrsEntry"></a>

### AttributeMapping.CustomAttrsEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |






<a name="platform-v1-DownstreamSP"></a>

### DownstreamSP
DownstreamSP is a SAML Service Provider that AOID acts as IdP for
(outbound federation, TRD 06-09). For OIDC RPs, AOID uses
OAuthAdminService.CreateClient from Obj 4 — DownstreamSP is the
SAML-specific equivalent.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  | id is the DownstreamSP UUID. |
| tenant_id | [string](#string) |  | tenant_id is the owning tenant UUID. |
| name | [string](#string) |  | name is the admin-facing label. |
| entity_id | [string](#string) |  | entity_id is the SP entityID (SAML EntityDescriptor/@entityID). |
| acs_url | [string](#string) |  | acs_url is the SP&#39;s Assertion Consumer Service URL where AOID POSTs the SAML Response. |
| slo_url | [string](#string) |  | slo_url is the SP&#39;s Single Logout Service URL. Empty if SLO is not wired for this SP. |
| signing_cert_pem | [bytes](#bytes) |  | signing_cert_pem is the SP&#39;s signing certificate, used to verify SAML AuthnRequests when the SP signs them. Empty when the SP does not sign AuthnRequests. |
| nameid_format | [string](#string) |  | nameid_format is the SAML NameID format AOID emits in assertions. Valid values: &#34;email&#34; | &#34;persistent&#34; | &#34;transient&#34;. |
| created_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | created_at is when the SP was first registered. |
| updated_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | updated_at is the last mutation timestamp. |






<a name="platform-v1-ExternalIdP"></a>

### ExternalIdP
ExternalIdP is an upstream identity provider (SAML IdP or OIDC OP) that
a tenant has configured to federate into AOID. Inbound federation
(TRD 06-07 / 06-08) consumes these records.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  | id is the ExternalIdP UUID (database primary key). |
| tenant_id | [string](#string) |  | tenant_id is the owning tenant UUID. |
| name | [string](#string) |  | name is the admin-facing display label. |
| kind | [string](#string) |  | kind is the federation protocol. Valid values: &#34;saml&#34; — uses saml_metadata_xml. &#34;oidc&#34; — uses oidc_issuer_url &#43; oidc_client_id &#43; oidc_client_secret. |
| enabled | [bool](#bool) |  | enabled toggles the IdP without deleting the configuration. |
| saml_metadata_xml | [bytes](#bytes) |  | saml_metadata_xml is the verbatim SAML 2.0 metadata document. Populated only when kind == &#34;saml&#34;. MUST be validated by platform/saml.XSWGuard &#43; ParseAndCacheMetadata (TRD 06-01) before AOID persists. AOID encrypts at rest (TRD 06-05). |
| oidc_issuer_url | [string](#string) |  | oidc_issuer_url is the OIDC issuer (iss claim) URL. Populated only when kind == &#34;oidc&#34;. |
| oidc_client_id | [string](#string) |  | oidc_client_id is AOID&#39;s registered client_id at the upstream OP. Populated only when kind == &#34;oidc&#34;. |
| oidc_client_secret_set | [bool](#bool) |  | oidc_client_secret_set is a write-only signal. On responses it reports whether a secret is configured. The plaintext is never returned on read paths (Get / List). Use the dedicated rotate flow in FederationAdminService to change a secret. |
| oidc_scopes | [string](#string) | repeated | oidc_scopes are the scopes AOID requests on each authorization (e.g. [&#34;openid&#34;, &#34;profile&#34;, &#34;email&#34;]). Populated only when kind == &#34;oidc&#34;. |
| attribute_mapping_id | [string](#string) |  | attribute_mapping_id binds this IdP to an AttributeMapping (below) that translates upstream claims/attributes into AOID account fields. |
| created_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | created_at is when the IdP record was first persisted. |
| updated_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | updated_at is the last mutation timestamp. |






<a name="platform-v1-FederationPolicy"></a>

### FederationPolicy
FederationPolicy gates federation behavior per (tenant, external IdP).
Stored one-per-tenant-per-IdP. Primary key is the (tenant_id, idp_id) pair.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  | tenant_id is the owning tenant UUID. |
| idp_id | [string](#string) |  | idp_id is the ExternalIdP UUID this policy applies to. |
| allowed | [bool](#bool) |  | allowed gates the IdP entirely. When false, federation attempts are rejected regardless of other settings. |
| jit_enabled | [bool](#bool) |  | jit_enabled allows just-in-time provisioning of new accounts on first successful federation. When false, the account must pre-exist. |
| jit_default_role_id | [string](#string) |  | jit_default_role_id is the role assigned to a JIT-provisioned account. Empty means no automatic role assignment. UUID string when present. |
| assurance_level_override | [string](#string) |  | assurance_level_override sets the AAL recorded on the session regardless of the upstream assertion. Empty means &#34;use upstream value when present, else fall back to AAL1&#34;. Valid values: &#34;AAL1&#34; | &#34;AAL2&#34; | &#34;AAL3&#34;. |
| required_attributes | [string](#string) | repeated | required_attributes lists AOID-side attribute names that MUST be resolvable from the upstream assertion via the AttributeMapping. Federation attempts missing a required attribute are rejected. |
| email_domain_allowlist | [string](#string) | repeated | email_domain_allowlist limits federated accounts to email addresses matching one of the listed domains. Empty means &#34;no domain filter&#34;. |
| created_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | created_at is when the policy was first persisted. |
| updated_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | updated_at is the last mutation timestamp. |





 

 

 

 



<a name="platform_v1_oauth_admin-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## platform/v1/oauth_admin.proto



<a name="platform-v1-OAuthAdminServiceAddRedirectURIRequest"></a>

### OAuthAdminServiceAddRedirectURIRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| client_id | [string](#string) |  |  |
| redirect_uri | [string](#string) |  |  |






<a name="platform-v1-OAuthAdminServiceAddRedirectURIResponse"></a>

### OAuthAdminServiceAddRedirectURIResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| client | [OAuthClient](#platform-v1-OAuthClient) |  |  |






<a name="platform-v1-OAuthAdminServiceCreateClientRequest"></a>

### OAuthAdminServiceCreateClientRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| client_name | [string](#string) |  |  |
| client_type | [string](#string) |  |  |
| grant_types | [string](#string) | repeated |  |
| redirect_uris | [string](#string) | repeated |  |
| allowed_scopes | [string](#string) | repeated |  |
| token_endpoint_auth_method | [string](#string) |  |  |
| jwks_uri | [string](#string) |  |  |
| auto_consent | [bool](#bool) |  |  |






<a name="platform-v1-OAuthAdminServiceCreateClientResponse"></a>

### OAuthAdminServiceCreateClientResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| client | [OAuthClient](#platform-v1-OAuthClient) |  |  |
| client_secret | [string](#string) |  | One-time plaintext client_secret. Empty for public clients (token_endpoint_auth_method == &#34;none&#34;) and for private_key_jwt clients (which authenticate via signed JWT assertions). |






<a name="platform-v1-OAuthAdminServiceDeleteClientRequest"></a>

### OAuthAdminServiceDeleteClientRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| client_id | [string](#string) |  |  |






<a name="platform-v1-OAuthAdminServiceDeleteClientResponse"></a>

### OAuthAdminServiceDeleteClientResponse







<a name="platform-v1-OAuthAdminServiceGetClientRequest"></a>

### OAuthAdminServiceGetClientRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| client_id | [string](#string) |  |  |






<a name="platform-v1-OAuthAdminServiceGetClientResponse"></a>

### OAuthAdminServiceGetClientResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| client | [OAuthClient](#platform-v1-OAuthClient) |  |  |






<a name="platform-v1-OAuthAdminServiceListClientsRequest"></a>

### OAuthAdminServiceListClientsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| limit | [int32](#int32) |  | Pagination — same shape as Obj 2 ListAccounts / ListGroups. 0 means server default (100). Server caps at 500. |
| cursor | [string](#string) |  | Opaque, server-issued cursor. Empty for first page. |






<a name="platform-v1-OAuthAdminServiceListClientsResponse"></a>

### OAuthAdminServiceListClientsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| clients | [OAuthClient](#platform-v1-OAuthClient) | repeated |  |
| next_cursor | [string](#string) |  | Empty == no more results. |






<a name="platform-v1-OAuthAdminServiceRemoveRedirectURIRequest"></a>

### OAuthAdminServiceRemoveRedirectURIRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| client_id | [string](#string) |  |  |
| redirect_uri | [string](#string) |  |  |






<a name="platform-v1-OAuthAdminServiceRemoveRedirectURIResponse"></a>

### OAuthAdminServiceRemoveRedirectURIResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| client | [OAuthClient](#platform-v1-OAuthClient) |  |  |






<a name="platform-v1-OAuthAdminServiceRotateClientSecretRequest"></a>

### OAuthAdminServiceRotateClientSecretRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| client_id | [string](#string) |  |  |






<a name="platform-v1-OAuthAdminServiceRotateClientSecretResponse"></a>

### OAuthAdminServiceRotateClientSecretResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| client | [OAuthClient](#platform-v1-OAuthClient) |  |  |
| client_secret | [string](#string) |  | New plaintext client_secret. One-time emission — the server retains only the bcrypt hash. |






<a name="platform-v1-OAuthAdminServiceUpdateClientRequest"></a>

### OAuthAdminServiceUpdateClientRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| client_id | [string](#string) |  |  |
| client_name | [string](#string) |  | Empty string means no change. |
| allowed_scopes | [string](#string) | repeated | Replacement list. Empty means no change. To clear, callers must use a server-side admin tool — proto cannot distinguish unset vs. empty. |
| auto_consent | [bool](#bool) |  | auto_consent always applies — the field is a bool, so callers must send the desired value on every UpdateClient call. To make an &#34;unchanged&#34; semantics possible, AOID treats UpdateClient as a full-replace of the addressable fields documented here. |






<a name="platform-v1-OAuthAdminServiceUpdateClientResponse"></a>

### OAuthAdminServiceUpdateClientResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| client | [OAuthClient](#platform-v1-OAuthClient) |  |  |






<a name="platform-v1-OAuthClient"></a>

### OAuthClient
OAuthClient is the canonical client registration data. NO client_secret
is carried here — secret material appears only in CreateClientResponse
and RotateClientSecretResponse, never read back via GetClient.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  | Internal database UUID (primary key). |
| tenant_id | [string](#string) |  | Owning tenant UUID. |
| client_id | [string](#string) |  | Public OAuth client identifier. Typically a generated UUID-without-dashes or a URL-safe random string (~22 chars). DIFFERENT from id. |
| client_name | [string](#string) |  | Human-friendly label shown in admin UIs and consent screens. |
| client_type | [string](#string) |  | &#34;public&#34; | &#34;confidential&#34;. Confidential clients require a token-endpoint auth method other than &#34;none&#34;; public clients require PKCE. |
| grant_types | [string](#string) | repeated | Allowed OAuth 2.1 grant flows. Valid values: &#34;authorization_code&#34;, &#34;client_credentials&#34;, &#34;device_code&#34;, &#34;refresh_token&#34;. A single client may hold multiple grants (e.g. authorization_code &#43; refresh_token). The server validates each token request&#39;s grant_type against this list. |
| redirect_uris | [string](#string) | repeated | Allowlisted redirect URIs. OAuth 2.1 requires exact-match comparison — the server validates HTTPS scheme and exact equality on each authorize request. Format checking is NOT enforced in proto. |
| allowed_scopes | [string](#string) | repeated | Allowlisted OAuth scopes. The server intersects this list against the scopes requested on each authorize / token call. |
| token_endpoint_auth_method | [string](#string) |  | &#34;client_secret_basic&#34; | &#34;private_key_jwt&#34; | &#34;none&#34;. &#34;none&#34; is valid only for public clients (PKCE-based). The server rejects &#34;none&#34; for confidential clients. |
| jwks_uri | [string](#string) |  | JWKS endpoint URL. Only meaningful when token_endpoint_auth_method == &#34;private_key_jwt&#34;; empty otherwise. The server validates this consistency at create/update time and fetches JWKs on demand to verify client assertions. |
| auto_consent | [bool](#bool) |  | First-party clients (auto_consent=true) skip the user consent screen. Reserved for clients owned by the operating tenant. |
| created_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| updated_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |





 

 

 


<a name="platform-v1-OAuthAdminService"></a>

### OAuthAdminService
OAuthAdminService manages OAuth 2.1 client registrations on AOID.
Bound under super_admin or tenant_admin mTLS CNs (Obj 2 adminauth resolver).
All RPCs scope to tenant_id; super_admin can target any tenant.

Client secrets are emitted exactly once — on CreateClient and
RotateClientSecret. They are NEVER read back via GetClient or any
other RPC; the server persists them bcrypt-hashed (cost 12) per
the Obj 3 password-hashing precedent.

Tenant scoping (enforced by the same TenantScopeInterceptor used by
AccountAdminService):
  - Every request carries tenant_id as field 1.
  - Super admins may target any tenant.
  - Tenant admins are rejected at the request boundary when tenant_id
    does not match their bound tenant.

Audit emission: every successful mutation emits a platform/audit.Event
with the action constants added in TRD 04-05 (oauth.client.*).

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| CreateClient | [OAuthAdminServiceCreateClientRequest](#platform-v1-OAuthAdminServiceCreateClientRequest) | [OAuthAdminServiceCreateClientResponse](#platform-v1-OAuthAdminServiceCreateClientResponse) | CreateClient registers a new OAuth client and returns the canonical record plus the one-time plaintext client_secret (empty for public clients and for private_key_jwt clients). |
| GetClient | [OAuthAdminServiceGetClientRequest](#platform-v1-OAuthAdminServiceGetClientRequest) | [OAuthAdminServiceGetClientResponse](#platform-v1-OAuthAdminServiceGetClientResponse) | GetClient fetches a single client by its public client_id within the tenant. The response never includes client_secret material. |
| UpdateClient | [OAuthAdminServiceUpdateClientRequest](#platform-v1-OAuthAdminServiceUpdateClientRequest) | [OAuthAdminServiceUpdateClientResponse](#platform-v1-OAuthAdminServiceUpdateClientResponse) | UpdateClient mutates non-collection attributes (client_name, allowed_scopes, auto_consent). redirect_uris and grant_types mutate through the dedicated Add/Remove RPCs to preserve per-mutation audit trails. |
| DeleteClient | [OAuthAdminServiceDeleteClientRequest](#platform-v1-OAuthAdminServiceDeleteClientRequest) | [OAuthAdminServiceDeleteClientResponse](#platform-v1-OAuthAdminServiceDeleteClientResponse) | DeleteClient permanently removes a client registration. |
| ListClients | [OAuthAdminServiceListClientsRequest](#platform-v1-OAuthAdminServiceListClientsRequest) | [OAuthAdminServiceListClientsResponse](#platform-v1-OAuthAdminServiceListClientsResponse) | ListClients returns a paginated list of clients in the tenant. |
| RotateClientSecret | [OAuthAdminServiceRotateClientSecretRequest](#platform-v1-OAuthAdminServiceRotateClientSecretRequest) | [OAuthAdminServiceRotateClientSecretResponse](#platform-v1-OAuthAdminServiceRotateClientSecretResponse) | RotateClientSecret generates a new client_secret, persists the bcrypt hash, and returns the plaintext exactly once. |
| AddRedirectURI | [OAuthAdminServiceAddRedirectURIRequest](#platform-v1-OAuthAdminServiceAddRedirectURIRequest) | [OAuthAdminServiceAddRedirectURIResponse](#platform-v1-OAuthAdminServiceAddRedirectURIResponse) | AddRedirectURI appends a redirect_uri to the client&#39;s allowlist. |
| RemoveRedirectURI | [OAuthAdminServiceRemoveRedirectURIRequest](#platform-v1-OAuthAdminServiceRemoveRedirectURIRequest) | [OAuthAdminServiceRemoveRedirectURIResponse](#platform-v1-OAuthAdminServiceRemoveRedirectURIResponse) | RemoveRedirectURI removes a redirect_uri from the client&#39;s allowlist. |

 



<a name="platform_v1_federation_admin-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## platform/v1/federation_admin.proto



<a name="platform-v1-ClientIdPOption"></a>

### ClientIdPOption
ClientIdPOption is the (OAuth client, external IdP) pairing that controls
whether a given IdP is offered in a client&#39;s /authorize IdP picker.
RemoveClientIdPOption returns this with enabled = false (disable, not
delete) to preserve the admin audit trail.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  |  |
| client_id | [string](#string) |  |  |
| idp_id | [string](#string) |  |  |
| enabled | [bool](#bool) |  |  |






<a name="platform-v1-FederationAdminServiceAddClientIdPOptionRequest"></a>

### FederationAdminServiceAddClientIdPOptionRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| client_id | [string](#string) |  |  |
| idp_id | [string](#string) |  |  |






<a name="platform-v1-FederationAdminServiceAddClientIdPOptionResponse"></a>

### FederationAdminServiceAddClientIdPOptionResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| option | [ClientIdPOption](#platform-v1-ClientIdPOption) |  |  |






<a name="platform-v1-FederationAdminServiceCreateAttributeMappingRequest"></a>

### FederationAdminServiceCreateAttributeMappingRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| name | [string](#string) |  |  |
| email_path | [string](#string) |  |  |
| email_verified_path | [string](#string) |  |  |
| sub_path | [string](#string) |  |  |
| preferred_username_path | [string](#string) |  |  |
| name_path | [string](#string) |  |  |
| groups_path | [string](#string) |  |  |
| assurance_level_path | [string](#string) |  |  |
| custom_attrs | [FederationAdminServiceCreateAttributeMappingRequest.CustomAttrsEntry](#platform-v1-FederationAdminServiceCreateAttributeMappingRequest-CustomAttrsEntry) | repeated |  |






<a name="platform-v1-FederationAdminServiceCreateAttributeMappingRequest-CustomAttrsEntry"></a>

### FederationAdminServiceCreateAttributeMappingRequest.CustomAttrsEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |






<a name="platform-v1-FederationAdminServiceCreateAttributeMappingResponse"></a>

### FederationAdminServiceCreateAttributeMappingResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| mapping | [AttributeMapping](#platform-v1-AttributeMapping) |  |  |






<a name="platform-v1-FederationAdminServiceCreateExternalIdPRequest"></a>

### FederationAdminServiceCreateExternalIdPRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  | tenant_id is the owning tenant (canonical field 1 for tenant scope). |
| name | [string](#string) |  | name is the admin-facing display label. |
| kind | [string](#string) |  | kind is &#34;saml&#34; or &#34;oidc&#34;. |
| enabled | [bool](#bool) |  | enabled toggles the IdP at creation time (default false). |
| saml_metadata_xml | [bytes](#bytes) |  | saml_metadata_xml is required when kind == &#34;saml&#34;. MUST validate via platform/saml.XSWGuard &#43; ParseAndCacheMetadata (TRD 06-01). |
| oidc_issuer_url | [string](#string) |  | oidc_issuer_url is required when kind == &#34;oidc&#34;. |
| oidc_client_id | [string](#string) |  | oidc_client_id is required when kind == &#34;oidc&#34;. |
| oidc_client_secret | [string](#string) |  | oidc_client_secret is the plaintext upstream client secret. Required when kind == &#34;oidc&#34;. Stored bcrypt-hashed at rest (TRD 06-05). Never returned on read paths. |
| oidc_scopes | [string](#string) | repeated | oidc_scopes overrides the default [&#34;openid&#34;, &#34;profile&#34;, &#34;email&#34;] when kind == &#34;oidc&#34;. |
| attribute_mapping_id | [string](#string) |  | attribute_mapping_id binds the IdP to a previously-created AttributeMapping. The RPC returns CodeFailedPrecondition if the mapping does not exist in the tenant. |






<a name="platform-v1-FederationAdminServiceCreateExternalIdPResponse"></a>

### FederationAdminServiceCreateExternalIdPResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| idp | [ExternalIdP](#platform-v1-ExternalIdP) |  | idp carries the persisted record. oidc_client_secret is never set on responses; oidc_client_secret_set reflects the configured state. |






<a name="platform-v1-FederationAdminServiceDeleteAttributeMappingRequest"></a>

### FederationAdminServiceDeleteAttributeMappingRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| mapping_id | [string](#string) |  |  |






<a name="platform-v1-FederationAdminServiceDeleteAttributeMappingResponse"></a>

### FederationAdminServiceDeleteAttributeMappingResponse







<a name="platform-v1-FederationAdminServiceDeleteDownstreamSPRequest"></a>

### FederationAdminServiceDeleteDownstreamSPRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| sp_id | [string](#string) |  |  |






<a name="platform-v1-FederationAdminServiceDeleteDownstreamSPResponse"></a>

### FederationAdminServiceDeleteDownstreamSPResponse







<a name="platform-v1-FederationAdminServiceDeleteExternalIdPRequest"></a>

### FederationAdminServiceDeleteExternalIdPRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| idp_id | [string](#string) |  |  |






<a name="platform-v1-FederationAdminServiceDeleteExternalIdPResponse"></a>

### FederationAdminServiceDeleteExternalIdPResponse







<a name="platform-v1-FederationAdminServiceDeleteFederationPolicyRequest"></a>

### FederationAdminServiceDeleteFederationPolicyRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| idp_id | [string](#string) |  |  |






<a name="platform-v1-FederationAdminServiceDeleteFederationPolicyResponse"></a>

### FederationAdminServiceDeleteFederationPolicyResponse







<a name="platform-v1-FederationAdminServiceGetExternalIdPRequest"></a>

### FederationAdminServiceGetExternalIdPRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| idp_id | [string](#string) |  |  |






<a name="platform-v1-FederationAdminServiceGetExternalIdPResponse"></a>

### FederationAdminServiceGetExternalIdPResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| idp | [ExternalIdP](#platform-v1-ExternalIdP) |  |  |






<a name="platform-v1-FederationAdminServiceGetFederationPolicyRequest"></a>

### FederationAdminServiceGetFederationPolicyRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| idp_id | [string](#string) |  |  |






<a name="platform-v1-FederationAdminServiceGetFederationPolicyResponse"></a>

### FederationAdminServiceGetFederationPolicyResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| policy | [FederationPolicy](#platform-v1-FederationPolicy) |  |  |






<a name="platform-v1-FederationAdminServiceListAttributeMappingsRequest"></a>

### FederationAdminServiceListAttributeMappingsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| limit | [int32](#int32) |  |  |
| cursor | [string](#string) |  |  |






<a name="platform-v1-FederationAdminServiceListAttributeMappingsResponse"></a>

### FederationAdminServiceListAttributeMappingsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| mappings | [AttributeMapping](#platform-v1-AttributeMapping) | repeated |  |
| next_cursor | [string](#string) |  |  |






<a name="platform-v1-FederationAdminServiceListDownstreamSPsRequest"></a>

### FederationAdminServiceListDownstreamSPsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| limit | [int32](#int32) |  |  |
| cursor | [string](#string) |  |  |






<a name="platform-v1-FederationAdminServiceListDownstreamSPsResponse"></a>

### FederationAdminServiceListDownstreamSPsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| sps | [DownstreamSP](#platform-v1-DownstreamSP) | repeated |  |
| next_cursor | [string](#string) |  |  |






<a name="platform-v1-FederationAdminServiceListExternalIdPsRequest"></a>

### FederationAdminServiceListExternalIdPsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| limit | [int32](#int32) |  | limit defaults to server-side 100, capped at 500. |
| cursor | [string](#string) |  | cursor is opaque, server-issued. Empty for first page. |
| kind | [string](#string) |  | kind optionally filters to &#34;saml&#34; or &#34;oidc&#34; only. |






<a name="platform-v1-FederationAdminServiceListExternalIdPsResponse"></a>

### FederationAdminServiceListExternalIdPsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| idps | [ExternalIdP](#platform-v1-ExternalIdP) | repeated |  |
| next_cursor | [string](#string) |  | next_cursor is empty when there are no more results. |






<a name="platform-v1-FederationAdminServiceRegisterDownstreamClientRequest"></a>

### FederationAdminServiceRegisterDownstreamClientRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| client_name | [string](#string) |  | client_name is the admin-facing label (forwarded to OAuthAdminService.CreateClient). |
| redirect_uris | [string](#string) | repeated | redirect_uris is the SP&#39;s redirect_uri allowlist. At least one entry required. |
| allowed_scopes | [string](#string) | repeated | allowed_scopes overrides the federation defaults [&#34;openid&#34;, &#34;profile&#34;, &#34;email&#34;]. Empty list means &#34;use defaults&#34;. |






<a name="platform-v1-FederationAdminServiceRegisterDownstreamClientResponse"></a>

### FederationAdminServiceRegisterDownstreamClientResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| client | [OAuthClient](#platform-v1-OAuthClient) |  | client is the canonical OAuthClient record returned by OAuthAdminService.CreateClient. |
| client_secret | [string](#string) |  | client_secret is the one-time plaintext secret emitted exactly once at creation. The server retains only the bcrypt hash. |






<a name="platform-v1-FederationAdminServiceRegisterDownstreamSPRequest"></a>

### FederationAdminServiceRegisterDownstreamSPRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| name | [string](#string) |  |  |
| entity_id | [string](#string) |  | entity_id is the SP entityID (SAML EntityDescriptor/@entityID). |
| acs_url | [string](#string) |  | acs_url is the SP&#39;s Assertion Consumer Service URL. |
| slo_url | [string](#string) |  | slo_url is the SP&#39;s Single Logout Service URL. Empty if SLO is not wired. |
| signing_cert_pem | [bytes](#bytes) |  | signing_cert_pem is the SP&#39;s signing certificate (PEM-encoded), used to verify SAML AuthnRequests when the SP signs them. Empty bytes when the SP does not sign AuthnRequests. |
| nameid_format | [string](#string) |  | nameid_format is &#34;email&#34; | &#34;persistent&#34; | &#34;transient&#34;. |






<a name="platform-v1-FederationAdminServiceRegisterDownstreamSPResponse"></a>

### FederationAdminServiceRegisterDownstreamSPResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| sp | [DownstreamSP](#platform-v1-DownstreamSP) |  |  |






<a name="platform-v1-FederationAdminServiceRemoveClientIdPOptionRequest"></a>

### FederationAdminServiceRemoveClientIdPOptionRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| client_id | [string](#string) |  |  |
| idp_id | [string](#string) |  |  |






<a name="platform-v1-FederationAdminServiceRemoveClientIdPOptionResponse"></a>

### FederationAdminServiceRemoveClientIdPOptionResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| option | [ClientIdPOption](#platform-v1-ClientIdPOption) |  |  |






<a name="platform-v1-FederationAdminServiceUpdateAttributeMappingRequest"></a>

### FederationAdminServiceUpdateAttributeMappingRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| mapping_id | [string](#string) |  |  |
| name | [string](#string) |  | name replaces the label. Empty means no change. |
| email_path | [string](#string) |  | Path fields below replace the stored path when non-empty; empty means no change. To clear a path (e.g. drop email_verified), use the literal sentinel &#34;-&#34; — the application layer translates &#34;-&#34; to &#34;no path / clear&#34;. This sentinel avoids proto&#39;s inability to distinguish unset string from empty string. |
| email_verified_path | [string](#string) |  |  |
| sub_path | [string](#string) |  |  |
| preferred_username_path | [string](#string) |  |  |
| name_path | [string](#string) |  |  |
| groups_path | [string](#string) |  |  |
| assurance_level_path | [string](#string) |  |  |
| custom_attrs | [FederationAdminServiceUpdateAttributeMappingRequest.CustomAttrsEntry](#platform-v1-FederationAdminServiceUpdateAttributeMappingRequest-CustomAttrsEntry) | repeated | custom_attrs is a full-replace when non-empty. Empty map means no change. To clear, callers must use a dedicated tool. |






<a name="platform-v1-FederationAdminServiceUpdateAttributeMappingRequest-CustomAttrsEntry"></a>

### FederationAdminServiceUpdateAttributeMappingRequest.CustomAttrsEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |






<a name="platform-v1-FederationAdminServiceUpdateAttributeMappingResponse"></a>

### FederationAdminServiceUpdateAttributeMappingResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| mapping | [AttributeMapping](#platform-v1-AttributeMapping) |  |  |






<a name="platform-v1-FederationAdminServiceUpdateExternalIdPRequest"></a>

### FederationAdminServiceUpdateExternalIdPRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| idp_id | [string](#string) |  |  |
| name | [string](#string) |  | name replaces the display label. Empty means no change. |
| enabled | [bool](#bool) |  | enabled always applies (the field is a bool — callers must send the intended value on every UpdateExternalIdP call). |
| saml_metadata_xml | [bytes](#bytes) |  | saml_metadata_xml replaces the stored metadata when kind == &#34;saml&#34;. Empty bytes mean no change. Replacement bytes MUST re-validate via platform/saml.XSWGuard &#43; ParseAndCacheMetadata. |
| oidc_issuer_url | [string](#string) |  | oidc_issuer_url replaces the issuer URL when kind == &#34;oidc&#34;. Empty means no change. |
| oidc_client_id | [string](#string) |  | oidc_client_id replaces the client_id when kind == &#34;oidc&#34;. Empty means no change. |
| oidc_client_secret | [string](#string) |  | oidc_client_secret replaces the stored client secret when set. Empty means no change — the existing secret is retained. |
| oidc_scopes | [string](#string) | repeated | oidc_scopes replaces the requested scopes when kind == &#34;oidc&#34;. Empty list means no change. |
| attribute_mapping_id | [string](#string) |  | attribute_mapping_id replaces the mapping binding. Empty means no change. |






<a name="platform-v1-FederationAdminServiceUpdateExternalIdPResponse"></a>

### FederationAdminServiceUpdateExternalIdPResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| idp | [ExternalIdP](#platform-v1-ExternalIdP) |  |  |






<a name="platform-v1-FederationAdminServiceUpsertFederationPolicyRequest"></a>

### FederationAdminServiceUpsertFederationPolicyRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| idp_id | [string](#string) |  |  |
| allowed | [bool](#bool) |  |  |
| jit_enabled | [bool](#bool) |  |  |
| jit_default_role_id | [string](#string) |  | jit_default_role_id is the role assigned to JIT-provisioned accounts. Empty means no automatic role assignment. |
| assurance_level_override | [string](#string) |  | assurance_level_override sets the AAL recorded on federated sessions. Empty means &#34;use upstream value when present, else AAL1&#34;. Valid values: &#34;AAL1&#34; | &#34;AAL2&#34; | &#34;AAL3&#34;. |
| required_attributes | [string](#string) | repeated | required_attributes lists AOID-side attribute names that MUST be resolvable from the upstream assertion. Federation attempts missing a required attribute are rejected. |
| email_domain_allowlist | [string](#string) | repeated | email_domain_allowlist limits federated accounts to email addresses matching one of the listed domains. Empty means &#34;no domain filter&#34;. |






<a name="platform-v1-FederationAdminServiceUpsertFederationPolicyResponse"></a>

### FederationAdminServiceUpsertFederationPolicyResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| policy | [FederationPolicy](#platform-v1-FederationPolicy) |  |  |





 

 

 


<a name="platform-v1-FederationAdminService"></a>

### FederationAdminService
FederationAdminService manages AOID&#39;s federation surface (Obj 6):
inbound external IdPs (SAML / OIDC), attribute mappings, per-tenant
federation policy, and outbound SAML downstream Service Providers.
OIDC RPs (outbound) are managed via Obj 4&#39;s OAuthAdminService;
RegisterDownstreamClient here is a thin convenience wrapper that
fans out to OAuthAdminService.CreateClient with federation-friendly
defaults (response_types code, grant_types authorization_code &#43;
refresh_token, allowed_scopes &#34;openid profile email&#34;).

All RPCs are mTLS-only and bound under super_admin or tenant_admin
CNs by Obj 2&#39;s adminauth resolver and TenantScopeInterceptor:
  - Every request carries tenant_id as field 1.
  - Super admins may target any tenant.
  - Tenant admins are rejected when tenant_id does not match their
    bound tenant.

Audit emission: every successful mutation emits a platform/audit.Event
with the action constants added in TRD 06-03 (federation.*).

Pagination: list RPCs use the (limit, cursor) shape that matches
Obj 4&#39;s OAuthAdminService.ListClients (limit default 100, max 500;
opaque server-issued cursor; empty next_cursor means &#34;no more results&#34;).

----- ExternalIdP -----

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| CreateExternalIdP | [FederationAdminServiceCreateExternalIdPRequest](#platform-v1-FederationAdminServiceCreateExternalIdPRequest) | [FederationAdminServiceCreateExternalIdPResponse](#platform-v1-FederationAdminServiceCreateExternalIdPResponse) | CreateExternalIdP registers a new upstream IdP for the tenant. For OIDC IdPs, oidc_client_secret is consumed and stored bcrypt-hashed (TRD 06-05) and never returned. For SAML IdPs, saml_metadata_xml MUST validate via platform/saml.XSWGuard &#43; ParseAndCacheMetadata (TRD 06-01) or the RPC returns CodeInvalidArgument. |
| GetExternalIdP | [FederationAdminServiceGetExternalIdPRequest](#platform-v1-FederationAdminServiceGetExternalIdPRequest) | [FederationAdminServiceGetExternalIdPResponse](#platform-v1-FederationAdminServiceGetExternalIdPResponse) | GetExternalIdP fetches a single IdP by id within the tenant. The response never includes oidc_client_secret material; oidc_client_secret_set reports configured/not-configured state. |
| UpdateExternalIdP | [FederationAdminServiceUpdateExternalIdPRequest](#platform-v1-FederationAdminServiceUpdateExternalIdPRequest) | [FederationAdminServiceUpdateExternalIdPResponse](#platform-v1-FederationAdminServiceUpdateExternalIdPResponse) | UpdateExternalIdP mutates IdP attributes. Setting oidc_client_secret to a non-empty value replaces the stored secret; empty means &#34;leave unchanged&#34;. To clear an enabled IdP, set enabled = false explicitly. |
| DeleteExternalIdP | [FederationAdminServiceDeleteExternalIdPRequest](#platform-v1-FederationAdminServiceDeleteExternalIdPRequest) | [FederationAdminServiceDeleteExternalIdPResponse](#platform-v1-FederationAdminServiceDeleteExternalIdPResponse) | DeleteExternalIdP permanently removes an IdP configuration along with the attached FederationPolicy. Federated accounts retain their local records; subsequent federation attempts via the deleted IdP are rejected. |
| ListExternalIdPs | [FederationAdminServiceListExternalIdPsRequest](#platform-v1-FederationAdminServiceListExternalIdPsRequest) | [FederationAdminServiceListExternalIdPsResponse](#platform-v1-FederationAdminServiceListExternalIdPsResponse) | ListExternalIdPs returns a paginated list of IdPs in the tenant. |
| CreateAttributeMapping | [FederationAdminServiceCreateAttributeMappingRequest](#platform-v1-FederationAdminServiceCreateAttributeMappingRequest) | [FederationAdminServiceCreateAttributeMappingResponse](#platform-v1-FederationAdminServiceCreateAttributeMappingResponse) | CreateAttributeMapping registers a new attribute mapping in the tenant. Multiple ExternalIdPs may reference the same mapping. |
| UpdateAttributeMapping | [FederationAdminServiceUpdateAttributeMappingRequest](#platform-v1-FederationAdminServiceUpdateAttributeMappingRequest) | [FederationAdminServiceUpdateAttributeMappingResponse](#platform-v1-FederationAdminServiceUpdateAttributeMappingResponse) | UpdateAttributeMapping mutates the mapping. Existing IdPs referencing this mapping pick up the change on their next federation attempt. |
| DeleteAttributeMapping | [FederationAdminServiceDeleteAttributeMappingRequest](#platform-v1-FederationAdminServiceDeleteAttributeMappingRequest) | [FederationAdminServiceDeleteAttributeMappingResponse](#platform-v1-FederationAdminServiceDeleteAttributeMappingResponse) | DeleteAttributeMapping removes the mapping. The RPC returns CodeFailedPrecondition if any ExternalIdP still references it. |
| ListAttributeMappings | [FederationAdminServiceListAttributeMappingsRequest](#platform-v1-FederationAdminServiceListAttributeMappingsRequest) | [FederationAdminServiceListAttributeMappingsResponse](#platform-v1-FederationAdminServiceListAttributeMappingsResponse) | ListAttributeMappings returns a paginated list of mappings in the tenant. |
| UpsertFederationPolicy | [FederationAdminServiceUpsertFederationPolicyRequest](#platform-v1-FederationAdminServiceUpsertFederationPolicyRequest) | [FederationAdminServiceUpsertFederationPolicyResponse](#platform-v1-FederationAdminServiceUpsertFederationPolicyResponse) | UpsertFederationPolicy creates or replaces the federation policy for a (tenant, IdP) pair. Primary key is the (tenant_id, idp_id) pair — there is at most one policy per IdP per tenant. |
| GetFederationPolicy | [FederationAdminServiceGetFederationPolicyRequest](#platform-v1-FederationAdminServiceGetFederationPolicyRequest) | [FederationAdminServiceGetFederationPolicyResponse](#platform-v1-FederationAdminServiceGetFederationPolicyResponse) | GetFederationPolicy fetches the policy for a (tenant, IdP) pair. |
| DeleteFederationPolicy | [FederationAdminServiceDeleteFederationPolicyRequest](#platform-v1-FederationAdminServiceDeleteFederationPolicyRequest) | [FederationAdminServiceDeleteFederationPolicyResponse](#platform-v1-FederationAdminServiceDeleteFederationPolicyResponse) | DeleteFederationPolicy removes the policy. With no policy present, federation attempts default to the safe denial path. |
| RegisterDownstreamSP | [FederationAdminServiceRegisterDownstreamSPRequest](#platform-v1-FederationAdminServiceRegisterDownstreamSPRequest) | [FederationAdminServiceRegisterDownstreamSPResponse](#platform-v1-FederationAdminServiceRegisterDownstreamSPResponse) | RegisterDownstreamSP creates a new SAML SP registration that AOID will issue assertions to. |
| ListDownstreamSPs | [FederationAdminServiceListDownstreamSPsRequest](#platform-v1-FederationAdminServiceListDownstreamSPsRequest) | [FederationAdminServiceListDownstreamSPsResponse](#platform-v1-FederationAdminServiceListDownstreamSPsResponse) | ListDownstreamSPs returns a paginated list of SPs in the tenant. |
| DeleteDownstreamSP | [FederationAdminServiceDeleteDownstreamSPRequest](#platform-v1-FederationAdminServiceDeleteDownstreamSPRequest) | [FederationAdminServiceDeleteDownstreamSPResponse](#platform-v1-FederationAdminServiceDeleteDownstreamSPResponse) | DeleteDownstreamSP removes an SP registration. In-flight assertions already issued remain valid for their lifetime. |
| RegisterDownstreamClient | [FederationAdminServiceRegisterDownstreamClientRequest](#platform-v1-FederationAdminServiceRegisterDownstreamClientRequest) | [FederationAdminServiceRegisterDownstreamClientResponse](#platform-v1-FederationAdminServiceRegisterDownstreamClientResponse) | RegisterDownstreamClient is a thin convenience wrapper around OAuthAdminService.CreateClient (Obj 4). It applies federation-friendly defaults (client_type=confidential, grant_types= [authorization_code, refresh_token], token_endpoint_auth_method= client_secret_basic, default scopes openid&#43;profile&#43;email) and returns the canonical OAuthClient &#43; one-time client_secret. Admin UIs that need finer control should call OAuthAdminService.CreateClient directly instead. |
| AddClientIdPOption | [FederationAdminServiceAddClientIdPOptionRequest](#platform-v1-FederationAdminServiceAddClientIdPOptionRequest) | [FederationAdminServiceAddClientIdPOptionResponse](#platform-v1-FederationAdminServiceAddClientIdPOptionResponse) | AddClientIdPOption enables an (OAuth client, external IdP) pairing so the IdP is offered in that client&#39;s /authorize IdP picker. Idempotent on the (client_id, idp_id) pair. The response carries the resulting option with enabled = true. |
| RemoveClientIdPOption | [FederationAdminServiceRemoveClientIdPOptionRequest](#platform-v1-FederationAdminServiceRemoveClientIdPOptionRequest) | [FederationAdminServiceRemoveClientIdPOptionResponse](#platform-v1-FederationAdminServiceRemoveClientIdPOptionResponse) | RemoveClientIdPOption DISABLES (enabled = false — it does NOT delete the row, preserving the admin audit trail) an (OAuth client, external IdP) pairing. The response carries the option with enabled = false. |

 



<a name="platform_v1_identity_admin-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## platform/v1/identity_admin.proto



<a name="platform-v1-AccountAdminServiceAssignRoleRequest"></a>

### AccountAdminServiceAssignRoleRequest
AccountAdminServiceAssignRoleRequest binds a role to an account. Named `Admin*` to
disambiguate from rbac.proto&#39;s `AssignRoleRequest`.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| account_id | [string](#string) |  |  |
| role_id | [string](#string) |  |  |






<a name="platform-v1-AccountAdminServiceAssignRoleResponse"></a>

### AccountAdminServiceAssignRoleResponse
AccountAdminServiceAssignRoleResponse is empty.






<a name="platform-v1-AccountAdminServiceClearAccountMFAFactorsRequest"></a>

### AccountAdminServiceClearAccountMFAFactorsRequest
AccountAdminServiceClearAccountMFAFactorsRequest clears all MFA factors
for the named account. reason is required and emitted as the
auth.mfa.cleared_by_admin event&#39;s Details.reason.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| account_id | [string](#string) |  |  |
| reason | [string](#string) |  |  |






<a name="platform-v1-AccountAdminServiceClearAccountMFAFactorsResponse"></a>

### AccountAdminServiceClearAccountMFAFactorsResponse
AccountAdminServiceClearAccountMFAFactorsResponse is intentionally empty.
Reserved for forward-compat additions (e.g. cleared_factor_count).






<a name="platform-v1-AccountAdminServiceGetRecertificationHistoryRequest"></a>

### AccountAdminServiceGetRecertificationHistoryRequest
AccountAdminServiceGetRecertificationHistoryRequest paginates the history
for one account.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| account_id | [string](#string) |  |  |
| page_size | [int32](#int32) |  |  |
| page_token | [string](#string) |  |  |






<a name="platform-v1-AccountAdminServiceGetRecertificationHistoryResponse"></a>

### AccountAdminServiceGetRecertificationHistoryResponse
AccountAdminServiceGetRecertificationHistoryResponse returns a page of
history rows.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| rows | [RecertificationHistoryRow](#platform-v1-RecertificationHistoryRow) | repeated |  |
| next_page_token | [string](#string) |  |  |






<a name="platform-v1-AccountAdminServiceListPendingRecertificationsRequest"></a>

### AccountAdminServiceListPendingRecertificationsRequest
AccountAdminServiceListPendingRecertificationsRequest paginates pending
reviews. campaign_id optional filter.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| campaign_id | [string](#string) | optional |  |
| page_size | [int32](#int32) |  |  |
| page_token | [string](#string) |  |  |






<a name="platform-v1-AccountAdminServiceListPendingRecertificationsResponse"></a>

### AccountAdminServiceListPendingRecertificationsResponse
AccountAdminServiceListPendingRecertificationsResponse returns a page of
pending review rows.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| rows | [PendingRecertificationRow](#platform-v1-PendingRecertificationRow) | repeated |  |
| next_page_token | [string](#string) |  |  |






<a name="platform-v1-AccountAdminServiceListRolesRequest"></a>

### AccountAdminServiceListRolesRequest
AccountAdminServiceListRolesRequest paginates roles available in the tenant. Named
`Admin*` to disambiguate from rbac.proto&#39;s `ListRolesRequest` (same proto
package, different service surface).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| include_system | [bool](#bool) |  | include_system includes system roles (e.g. tenant_admin) in the result. |
| page_size | [int32](#int32) |  |  |
| page_token | [string](#string) |  |  |






<a name="platform-v1-AccountAdminServiceListRolesResponse"></a>

### AccountAdminServiceListRolesResponse
AccountAdminServiceListRolesResponse returns a page of roles.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| roles | [Role](#platform-v1-Role) | repeated |  |
| next_page_token | [string](#string) |  |  |






<a name="platform-v1-AccountAdminServiceSubmitRecertificationDecisionRequest"></a>

### AccountAdminServiceSubmitRecertificationDecisionRequest
AccountAdminServiceSubmitRecertificationDecisionRequest records the calling
admin&#39;s decision on a pending review. Reviewer is read from session context.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| review_id | [string](#string) |  |  |
| decision | [string](#string) |  | &#34;approved&#34; | &#34;revoked&#34; | &#34;deferred&#34;. |
| comment | [string](#string) |  |  |






<a name="platform-v1-AccountAdminServiceSubmitRecertificationDecisionResponse"></a>

### AccountAdminServiceSubmitRecertificationDecisionResponse
AccountAdminServiceSubmitRecertificationDecisionResponse is intentionally
empty. Reserved for forward-compat additions.






<a name="platform-v1-AccountData"></a>

### AccountData
AccountData is the canonical wire shape of an account.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  | id is the account UUID. |
| tenant_id | [string](#string) |  | tenant_id is the owning tenant UUID. |
| account_type | [string](#string) |  | account_type is &#34;human&#34; | &#34;service_account&#34; | &#34;workload_identity&#34;. |
| status | [string](#string) |  | status is &#34;active&#34; | &#34;suspended&#34; | &#34;deleted&#34;. |
| display_name | [string](#string) |  | display_name is the human-readable name. |
| email | [string](#string) |  | email is optional for service/workload accounts. |
| org_affiliation | [string](#string) |  | org_affiliation is the subject&#39;s organizational affiliation. |
| description | [string](#string) |  | description is required for service/workload accounts. |
| clearance | [ClearanceInfo](#platform-v1-ClearanceInfo) |  | clearance is the structured clearance information. |
| expires_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | expires_at is unset for human accounts; set for service/workload. |
| created_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | created_at is when the account was provisioned. |
| updated_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | updated_at is the last mutation timestamp. |
| created_by | [string](#string) |  | created_by is the actor identifier (UUID string or CN). |
| last_modified_by | [string](#string) |  | last_modified_by is the actor that last mutated this account. |






<a name="platform-v1-AddAccountToGroupRequest"></a>

### AddAccountToGroupRequest
AddAccountToGroupRequest binds an account to a group.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| account_id | [string](#string) |  |  |
| group_id | [string](#string) |  |  |






<a name="platform-v1-AddAccountToGroupResponse"></a>

### AddAccountToGroupResponse
AddAccountToGroupResponse is empty (membership is acknowledged by absence
of error).






<a name="platform-v1-AssistedAccountRecoveryRequest"></a>

### AssistedAccountRecoveryRequest
AssistedAccountRecoveryRequest — see AssistedAccountRecovery. Carries no
actor, assurance, or step-up field: the admin identity and AAL are derived
from the authenticated session/cert context, never from the wire, matching
every other AccountAdminService mutation.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  | tenant_id is the target tenant (canonical field 1 for tenant scope). |
| account_id | [string](#string) |  | account_id identifies the subject. Email is never an identifier here — it is unique per-tenant only, and it is the very field being changed. |
| new_email | [string](#string) |  | new_email is the address the account is repointed to and where the recovery link is delivered. |
| reason | [string](#string) |  | reason is required and recorded in the audit event. |






<a name="platform-v1-AssistedAccountRecoveryResponse"></a>

### AssistedAccountRecoveryResponse
AssistedAccountRecoveryResponse is intentionally empty: success is the
absence of an error, and no token or recovery URL is ever returned on the
wire. Reserved for forward-compat additions (matches
AccountAdminServiceClearAccountMFAFactorsResponse).






<a name="platform-v1-ClearanceInfo"></a>

### ClearanceInfo
ClearanceInfo is the strongly-typed shape that maps to aoid.accounts.clearance
jsonb. See RESEARCH.md Task 3 for the taxonomy. Application layer validates
field values (level enum, compartment list); proto carries the shape only.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| level | [string](#string) |  | level is the clearance level (e.g. &#34;UNCLASSIFIED&#34;, &#34;SECRET&#34;, &#34;TS&#34;, &#34;TS/SCI&#34;). |
| compartments | [string](#string) | repeated | compartments lists SCI compartments (e.g. [&#34;SI&#34;, &#34;TK&#34;]). |
| caveats | [string](#string) | repeated | caveats lists release caveats (e.g. [&#34;NOFORN&#34;, &#34;RELTO USA, FVEY&#34;]). |
| investigation_type | [string](#string) |  | investigation_type is the background investigation type (e.g. &#34;T5&#34;). |
| adjudication_agency | [string](#string) |  | adjudication_agency is the agency that granted clearance (e.g. &#34;DCSA&#34;). |
| formal_access_date | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | formal_access_date is when the subject was read into the clearance. |
| expiry_date | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | expiry_date is when the clearance lapses (5y/10y per investigation type). |
| verified_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | verified_at is when an admin verified the clearance record. |
| verified_by | [string](#string) |  | verified_by is the admin actor (UUID string) who verified. |






<a name="platform-v1-CreateTenantRequest"></a>

### CreateTenantRequest
CreateTenantRequest creates a new tenant. Super-admin only. No tenant_id
field — this RPC creates the tenant.

AOID Obj 10 TRD 10-05 extension:
  - intended_kms_key_id        (field 5) — KMS key URI for token signing
  - intended_audit_kms_key_id  (field 6) — KMS key URI for audit signing

Both KMS URI fields are REQUIRED when isolation_tier ∈
{&#39;cryptographic&#39;,&#39;physical&#39;}; empty when isolation_tier=&#39;logical&#39;.
Operators pre-provision the KMS keys (AWS GovCloud KMS, Azure Gov
Managed HSM, on-prem HSM); AOID consumes the URIs as opaque secrets.
Adding fields 5&#43;6 is wire-compatible with logical-tier callers that
omit them.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| slug | [string](#string) |  |  |
| display_name | [string](#string) |  |  |
| isolation_tier | [string](#string) |  | isolation_tier defaults to &#34;logical&#34; when empty. |
| settings_json | [string](#string) |  |  |
| intended_kms_key_id | [string](#string) |  | Required iff isolation_tier in (&#39;cryptographic&#39;,&#39;physical&#39;). KMS key URI (awskms://..., azkeys://..., pkcs11://...) for token signing. Opaque to AOID; never logged plaintext. |
| intended_audit_kms_key_id | [string](#string) |  | Required iff isolation_tier in (&#39;cryptographic&#39;,&#39;physical&#39;). KMS key URI for audit-event signing (separate trust domain from tokens). |






<a name="platform-v1-CreateTenantResponse"></a>

### CreateTenantResponse
CreateTenantResponse returns the new tenant.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant | [Tenant](#platform-v1-Tenant) |  |  |






<a name="platform-v1-DefineGroupRequest"></a>

### DefineGroupRequest
DefineGroupRequest creates a group in the tenant.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| name | [string](#string) |  |  |
| description | [string](#string) |  |  |






<a name="platform-v1-DefineGroupResponse"></a>

### DefineGroupResponse
DefineGroupResponse returns the new group.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| group | [Group](#platform-v1-Group) |  |  |






<a name="platform-v1-DefineRoleRequest"></a>

### DefineRoleRequest
DefineRoleRequest creates a role in the tenant.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| name | [string](#string) |  |  |
| description | [string](#string) |  |  |






<a name="platform-v1-DefineRoleResponse"></a>

### DefineRoleResponse
DefineRoleResponse returns the new role.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| role | [Role](#platform-v1-Role) |  |  |






<a name="platform-v1-DeleteEntitlementRequest"></a>

### DeleteEntitlementRequest
DeleteEntitlementRequest removes an entitlement from an account.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| account_id | [string](#string) |  |  |
| namespace | [string](#string) |  |  |
| key | [string](#string) |  |  |






<a name="platform-v1-DeleteEntitlementResponse"></a>

### DeleteEntitlementResponse
DeleteEntitlementResponse is empty.






<a name="platform-v1-DeprovisionAccountRequest"></a>

### DeprovisionAccountRequest
DeprovisionAccountRequest soft-deletes an account.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| account_id | [string](#string) |  |  |
| reason | [string](#string) |  |  |






<a name="platform-v1-DeprovisionAccountResponse"></a>

### DeprovisionAccountResponse
DeprovisionAccountResponse returns the deprovisioned account.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| account | [AccountData](#platform-v1-AccountData) |  |  |






<a name="platform-v1-Entitlement"></a>

### Entitlement
Entitlement is the canonical wire shape of an account entitlement.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  |  |
| account_id | [string](#string) |  |  |
| tenant_id | [string](#string) |  |  |
| namespace | [string](#string) |  | namespace partitions entitlement keys per consumer (e.g. &#34;aoid&#34;, &#34;aocore&#34;). |
| key | [string](#string) |  |  |
| value_json | [string](#string) |  | value_json is the JSON-encoded value (object/scalar/array). |
| set_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| set_by | [string](#string) |  |  |






<a name="platform-v1-GetAccountRequest"></a>

### GetAccountRequest
GetAccountRequest reads a single account by id within the tenant.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| account_id | [string](#string) |  |  |






<a name="platform-v1-GetAccountResponse"></a>

### GetAccountResponse
GetAccountResponse returns the requested account.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| account | [AccountData](#platform-v1-AccountData) |  |  |






<a name="platform-v1-GetTenantRequest"></a>

### GetTenantRequest
GetTenantRequest reads a tenant by id.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |






<a name="platform-v1-GetTenantResponse"></a>

### GetTenantResponse
GetTenantResponse returns the requested tenant.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant | [Tenant](#platform-v1-Tenant) |  |  |






<a name="platform-v1-Group"></a>

### Group
Group is the canonical wire shape of a group.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  |  |
| tenant_id | [string](#string) |  |  |
| name | [string](#string) |  |  |
| description | [string](#string) |  |  |
| created_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |






<a name="platform-v1-ListAccountsRequest"></a>

### ListAccountsRequest
ListAccountsRequest paginates accounts in the tenant.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| page_size | [int32](#int32) |  | page_size defaults to 50, max 500. |
| page_token | [string](#string) |  |  |
| account_type | [string](#string) |  | account_type optional filter. |
| status | [string](#string) |  | status optional filter (default excludes &#34;deleted&#34;). |
| include_deleted | [bool](#bool) |  | include_deleted overrides the default exclusion of deleted accounts. |






<a name="platform-v1-ListAccountsResponse"></a>

### ListAccountsResponse
ListAccountsResponse returns a page of accounts and the next page token.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| accounts | [AccountData](#platform-v1-AccountData) | repeated |  |
| next_page_token | [string](#string) |  |  |






<a name="platform-v1-ListEntitlementsRequest"></a>

### ListEntitlementsRequest
ListEntitlementsRequest returns entitlements for an account, optionally
filtered by namespace.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| account_id | [string](#string) |  |  |
| namespace | [string](#string) |  | namespace optional filter. |






<a name="platform-v1-ListEntitlementsResponse"></a>

### ListEntitlementsResponse
ListEntitlementsResponse returns the matching entitlements.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| entitlements | [Entitlement](#platform-v1-Entitlement) | repeated |  |






<a name="platform-v1-ListGroupsRequest"></a>

### ListGroupsRequest
ListGroupsRequest paginates groups in the tenant.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| page_size | [int32](#int32) |  |  |
| page_token | [string](#string) |  |  |






<a name="platform-v1-ListGroupsResponse"></a>

### ListGroupsResponse
ListGroupsResponse returns a page of groups.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| groups | [Group](#platform-v1-Group) | repeated |  |
| next_page_token | [string](#string) |  |  |






<a name="platform-v1-ListTenantsRequest"></a>

### ListTenantsRequest
ListTenantsRequest paginates all tenants. Super-admin only.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| page_size | [int32](#int32) |  |  |
| page_token | [string](#string) |  |  |
| isolation_tier | [string](#string) |  | isolation_tier optional filter. |






<a name="platform-v1-ListTenantsResponse"></a>

### ListTenantsResponse
ListTenantsResponse returns a page of tenants.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenants | [Tenant](#platform-v1-Tenant) | repeated |  |
| next_page_token | [string](#string) |  |  |






<a name="platform-v1-PendingRecertificationRow"></a>

### PendingRecertificationRow
PendingRecertificationRow is one row of the pending-review queue.
days_until_due is negative when the review is overdue.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| review_id | [string](#string) |  |  |
| account_id | [string](#string) |  |  |
| account_email | [string](#string) |  |  |
| campaign_id | [string](#string) |  |  |
| campaign_name | [string](#string) |  |  |
| due_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| days_until_due | [int32](#int32) |  |  |






<a name="platform-v1-ProvisionAccountRequest"></a>

### ProvisionAccountRequest
ProvisionAccountRequest provisions a human account.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  | tenant_id is the target tenant (canonical field 1 for tenant scope). |
| display_name | [string](#string) |  |  |
| email | [string](#string) |  |  |
| org_affiliation | [string](#string) |  |  |
| clearance | [ClearanceInfo](#platform-v1-ClearanceInfo) |  |  |
| group_ids | [string](#string) | repeated | group_ids attaches the account to the listed groups at provision time. |






<a name="platform-v1-ProvisionAccountResponse"></a>

### ProvisionAccountResponse
ProvisionAccountResponse returns the new account.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| account | [AccountData](#platform-v1-AccountData) |  |  |






<a name="platform-v1-ProvisionServiceAccountRequest"></a>

### ProvisionServiceAccountRequest
ProvisionServiceAccountRequest provisions a service or workload account.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| account_type | [string](#string) |  | account_type is &#34;service_account&#34; or &#34;workload_identity&#34;. |
| display_name | [string](#string) |  |  |
| description | [string](#string) |  |  |
| email | [string](#string) |  | email is optional for service accounts. |
| expires_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | expires_at is required for workload_identity, optional for service_account. |
| group_ids | [string](#string) | repeated |  |






<a name="platform-v1-ProvisionServiceAccountResponse"></a>

### ProvisionServiceAccountResponse
ProvisionServiceAccountResponse returns the new service/workload account.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| account | [AccountData](#platform-v1-AccountData) |  |  |






<a name="platform-v1-RecertificationHistoryRow"></a>

### RecertificationHistoryRow
RecertificationHistoryRow is one row of the per-account decision history.
decision values: &#34;approved&#34; | &#34;revoked&#34; | &#34;deferred&#34;.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| review_id | [string](#string) |  |  |
| campaign_id | [string](#string) |  |  |
| campaign_name | [string](#string) |  |  |
| due_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| reviewed_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| reviewer_id | [string](#string) |  |  |
| decision | [string](#string) |  |  |
| comment | [string](#string) |  |  |






<a name="platform-v1-RecoverAccountRequest"></a>

### RecoverAccountRequest
RecoverAccountRequest transitions a suspended account back to &#34;active&#34;.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| account_id | [string](#string) |  |  |
| reason | [string](#string) |  |  |






<a name="platform-v1-RecoverAccountResponse"></a>

### RecoverAccountResponse
RecoverAccountResponse returns the recovered account.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| account | [AccountData](#platform-v1-AccountData) |  |  |






<a name="platform-v1-RemoveAccountFromGroupRequest"></a>

### RemoveAccountFromGroupRequest
RemoveAccountFromGroupRequest unbinds an account from a group.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| account_id | [string](#string) |  |  |
| group_id | [string](#string) |  |  |






<a name="platform-v1-RemoveAccountFromGroupResponse"></a>

### RemoveAccountFromGroupResponse
RemoveAccountFromGroupResponse is empty.






<a name="platform-v1-RevokeRoleRequest"></a>

### RevokeRoleRequest
RevokeRoleRequest unbinds a role from an account.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| account_id | [string](#string) |  |  |
| role_id | [string](#string) |  |  |






<a name="platform-v1-RevokeRoleResponse"></a>

### RevokeRoleResponse
RevokeRoleResponse is empty.






<a name="platform-v1-Role"></a>

### Role
Role is the canonical wire shape of a role.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  |  |
| tenant_id | [string](#string) |  | tenant_id is empty for system-wide roles (e.g. super_admin). |
| name | [string](#string) |  |  |
| description | [string](#string) |  |  |
| is_system | [bool](#bool) |  |  |
| created_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |






<a name="platform-v1-SetEntitlementRequest"></a>

### SetEntitlementRequest
SetEntitlementRequest creates or replaces an entitlement on an account.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| account_id | [string](#string) |  |  |
| namespace | [string](#string) |  |  |
| key | [string](#string) |  |  |
| value_json | [string](#string) |  |  |






<a name="platform-v1-SetEntitlementResponse"></a>

### SetEntitlementResponse
SetEntitlementResponse returns the upserted entitlement.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| entitlement | [Entitlement](#platform-v1-Entitlement) |  |  |






<a name="platform-v1-SuspendAccountRequest"></a>

### SuspendAccountRequest
SuspendAccountRequest transitions an account to &#34;suspended&#34;.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| account_id | [string](#string) |  |  |
| reason | [string](#string) |  |  |






<a name="platform-v1-SuspendAccountResponse"></a>

### SuspendAccountResponse
SuspendAccountResponse returns the suspended account.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| account | [AccountData](#platform-v1-AccountData) |  |  |






<a name="platform-v1-Tenant"></a>

### Tenant
Tenant is the canonical wire shape of a tenant.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  |  |
| slug | [string](#string) |  |  |
| display_name | [string](#string) |  |  |
| isolation_tier | [string](#string) |  | isolation_tier is &#34;logical&#34; | &#34;cryptographic&#34; | &#34;physical&#34;. |
| settings_json | [string](#string) |  | settings_json carries JSON-encoded MFA / federation / session policies. |
| created_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| updated_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |






<a name="platform-v1-UpdateAccountRequest"></a>

### UpdateAccountRequest
UpdateAccountRequest mutates non-lifecycle attributes. Status changes go
through Suspend/Recover/Deprovision, not Update.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_id | [string](#string) |  |  |
| account_id | [string](#string) |  |  |
| display_name | [string](#string) |  |  |
| email | [string](#string) |  |  |
| org_affiliation | [string](#string) |  |  |
| clearance | [ClearanceInfo](#platform-v1-ClearanceInfo) |  |  |
| expires_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |






<a name="platform-v1-UpdateAccountResponse"></a>

### UpdateAccountResponse
UpdateAccountResponse returns the mutated account.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| account | [AccountData](#platform-v1-AccountData) |  |  |





 

 

 


<a name="platform-v1-AccountAdminService"></a>

### AccountAdminService
AccountAdminService is the admin surface for AOID identity lifecycle:
accounts, groups, roles, entitlements, and tenants. All RPCs are mTLS-only
and require an authenticated admin actor (super_admin or tenant_admin).

Tenant scoping (enforced by server.NewTenantScopeInterceptor):
  - Every mutating request carries tenant_id as field 1.
  - Super admins may target any tenant (including uuid.Nil for tenant ops).
  - Tenant admins are rejected at the request boundary when tenant_id
    does not match their bound tenant.

Audit emission: every successful mutation emits a platform/audit.Event
with the action constants added in TRD 02-03 (identity.*).

See AOID .planning/objectives/02-account-model-admin-lifecycle/ for the
design backing this contract.

-------------------------- Accounts (LIFE-01/02/03/05/08, MGMT-08) -----

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| ProvisionAccount | [ProvisionAccountRequest](#platform-v1-ProvisionAccountRequest) | [ProvisionAccountResponse](#platform-v1-ProvisionAccountResponse) | ProvisionAccount creates a human user account. |
| ProvisionServiceAccount | [ProvisionServiceAccountRequest](#platform-v1-ProvisionServiceAccountRequest) | [ProvisionServiceAccountResponse](#platform-v1-ProvisionServiceAccountResponse) | ProvisionServiceAccount creates a service_account or workload_identity with an optional or required expires_at lifetime. |
| GetAccount | [GetAccountRequest](#platform-v1-GetAccountRequest) | [GetAccountResponse](#platform-v1-GetAccountResponse) | GetAccount retrieves a single account by id within the tenant. |
| ListAccounts | [ListAccountsRequest](#platform-v1-ListAccountsRequest) | [ListAccountsResponse](#platform-v1-ListAccountsResponse) | ListAccounts returns a paginated list of accounts in the tenant. |
| UpdateAccount | [UpdateAccountRequest](#platform-v1-UpdateAccountRequest) | [UpdateAccountResponse](#platform-v1-UpdateAccountResponse) | UpdateAccount mutates non-lifecycle attributes (display_name, email, org_affiliation, clearance). Status transitions go through Suspend/Recover/Deprovision. |
| AssistedAccountRecovery | [AssistedAccountRecoveryRequest](#platform-v1-AssistedAccountRecoveryRequest) | [AssistedAccountRecoveryResponse](#platform-v1-AssistedAccountRecoveryResponse) | AssistedAccountRecovery repoints an account&#39;s sign-in email to a new address and starts a password recovery to it, as ONE operation, for the case where the holder can no longer reach their REGISTERED mailbox and so cannot use the anonymous self-service recovery flow.

This is NOT RecoverAccount: that RPC only flips a suspended account back to &#34;active&#34; (a status change, no credential effect). This one changes the sign-in email and begins a password recovery.

Deliberately NOT UpdateAccount: UpdateAccount also mutates clearance and expires_at, so authorizing a broad admin population for it would grant clearance mutation as a side effect. This RPC cannot touch clearance because its request does not carry the field.

Authorization and assurance are enforced by the AOID handler &#43; interceptors, NOT by this contract: a session- or cert-derived admin, a per-procedure allowlist, an entitlement distinct from any auto-granted &#39;admin&#39;, and — for session callers — an AAL2&#43; session. The assurance level is read from the authenticated session context and is DELIBERATELY NOT a wire field: a body-carried capability/step-up field would be a spoofable authority surface, and authority on this service always comes from the session/cert, never the request body.

Side effects the handler owns: the recovery link goes to new_email; the PRIOR address (and contact_email, if set) is notified out-of-band so the legitimate owner witnesses the change. The response is intentionally empty — no token or recovery URL is ever returned on the wire. |
| SuspendAccount | [SuspendAccountRequest](#platform-v1-SuspendAccountRequest) | [SuspendAccountResponse](#platform-v1-SuspendAccountResponse) | SuspendAccount transitions the account to &#34;suspended&#34;. Attributes retained. |
| RecoverAccount | [RecoverAccountRequest](#platform-v1-RecoverAccountRequest) | [RecoverAccountResponse](#platform-v1-RecoverAccountResponse) | RecoverAccount transitions a suspended account back to &#34;active&#34;. |
| DeprovisionAccount | [DeprovisionAccountRequest](#platform-v1-DeprovisionAccountRequest) | [DeprovisionAccountResponse](#platform-v1-DeprovisionAccountResponse) | DeprovisionAccount transitions the account to &#34;deleted&#34; (soft delete). |
| DefineGroup | [DefineGroupRequest](#platform-v1-DefineGroupRequest) | [DefineGroupResponse](#platform-v1-DefineGroupResponse) | DefineGroup creates a tenant-scoped group. |
| ListGroups | [ListGroupsRequest](#platform-v1-ListGroupsRequest) | [ListGroupsResponse](#platform-v1-ListGroupsResponse) | ListGroups returns groups in the tenant. |
| AddAccountToGroup | [AddAccountToGroupRequest](#platform-v1-AddAccountToGroupRequest) | [AddAccountToGroupResponse](#platform-v1-AddAccountToGroupResponse) | AddAccountToGroup adds an account to a group. |
| RemoveAccountFromGroup | [RemoveAccountFromGroupRequest](#platform-v1-RemoveAccountFromGroupRequest) | [RemoveAccountFromGroupResponse](#platform-v1-RemoveAccountFromGroupResponse) | RemoveAccountFromGroup removes an account from a group. |
| DefineRole | [DefineRoleRequest](#platform-v1-DefineRoleRequest) | [DefineRoleResponse](#platform-v1-DefineRoleResponse) | DefineRole creates a tenant-scoped role. |
| ListRoles | [AccountAdminServiceListRolesRequest](#platform-v1-AccountAdminServiceListRolesRequest) | [AccountAdminServiceListRolesResponse](#platform-v1-AccountAdminServiceListRolesResponse) | ListRoles returns roles available in the tenant. |
| AssignRole | [AccountAdminServiceAssignRoleRequest](#platform-v1-AccountAdminServiceAssignRoleRequest) | [AccountAdminServiceAssignRoleResponse](#platform-v1-AccountAdminServiceAssignRoleResponse) | AssignRole binds a role to an account. |
| RevokeRole | [RevokeRoleRequest](#platform-v1-RevokeRoleRequest) | [RevokeRoleResponse](#platform-v1-RevokeRoleResponse) | RevokeRole unbinds a role from an account. |
| SetEntitlement | [SetEntitlementRequest](#platform-v1-SetEntitlementRequest) | [SetEntitlementResponse](#platform-v1-SetEntitlementResponse) | SetEntitlement creates or replaces an entitlement on an account. |
| DeleteEntitlement | [DeleteEntitlementRequest](#platform-v1-DeleteEntitlementRequest) | [DeleteEntitlementResponse](#platform-v1-DeleteEntitlementResponse) | DeleteEntitlement removes an entitlement from an account. |
| ListEntitlements | [ListEntitlementsRequest](#platform-v1-ListEntitlementsRequest) | [ListEntitlementsResponse](#platform-v1-ListEntitlementsResponse) | ListEntitlements returns entitlements for an account. |
| CreateTenant | [CreateTenantRequest](#platform-v1-CreateTenantRequest) | [CreateTenantResponse](#platform-v1-CreateTenantResponse) | CreateTenant creates a new tenant. Super-admin only. |
| GetTenant | [GetTenantRequest](#platform-v1-GetTenantRequest) | [GetTenantResponse](#platform-v1-GetTenantResponse) | GetTenant retrieves a tenant by id. Super-admin only. |
| ListTenants | [ListTenantsRequest](#platform-v1-ListTenantsRequest) | [ListTenantsResponse](#platform-v1-ListTenantsResponse) | ListTenants enumerates all tenants. Super-admin only (AC-2 evidence). |
| ListPendingRecertifications | [AccountAdminServiceListPendingRecertificationsRequest](#platform-v1-AccountAdminServiceListPendingRecertificationsRequest) | [AccountAdminServiceListPendingRecertificationsResponse](#platform-v1-AccountAdminServiceListPendingRecertificationsResponse) | ListPendingRecertifications returns reviews awaiting an admin decision. |
| SubmitRecertificationDecision | [AccountAdminServiceSubmitRecertificationDecisionRequest](#platform-v1-AccountAdminServiceSubmitRecertificationDecisionRequest) | [AccountAdminServiceSubmitRecertificationDecisionResponse](#platform-v1-AccountAdminServiceSubmitRecertificationDecisionResponse) | SubmitRecertificationDecision records the calling admin&#39;s decision on a pending review (approved | revoked | deferred). Reviewer is read from the session context (no field). |
| GetRecertificationHistory | [AccountAdminServiceGetRecertificationHistoryRequest](#platform-v1-AccountAdminServiceGetRecertificationHistoryRequest) | [AccountAdminServiceGetRecertificationHistoryResponse](#platform-v1-AccountAdminServiceGetRecertificationHistoryResponse) | GetRecertificationHistory returns the historical decision log for one account. |
| ClearAccountMFAFactors | [AccountAdminServiceClearAccountMFAFactorsRequest](#platform-v1-AccountAdminServiceClearAccountMFAFactorsRequest) | [AccountAdminServiceClearAccountMFAFactorsResponse](#platform-v1-AccountAdminServiceClearAccountMFAFactorsResponse) | ClearAccountMFAFactors removes all MFA factors for an account (e.g. lost YubiKey path — LIFE-04 admin assist). reason is required &#43; emitted as auth.mfa.cleared_by_admin Details. |

 



<a name="platform_v1_rbac-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## platform/v1/rbac.proto



<a name="platform-v1-AssignRoleRequest"></a>

### AssignRoleRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| company_id | [string](#string) |  |  |
| user_id | [string](#string) |  |  |
| role_id | [string](#string) |  |  |






<a name="platform-v1-AssignRoleResponse"></a>

### AssignRoleResponse







<a name="platform-v1-CheckPermissionRequest"></a>

### CheckPermissionRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| company_id | [string](#string) |  |  |
| user_id | [string](#string) |  |  |
| feature | [string](#string) |  |  |
| action | [string](#string) |  |  |






<a name="platform-v1-CheckPermissionResponse"></a>

### CheckPermissionResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| allowed | [bool](#bool) |  |  |






<a name="platform-v1-CreateRoleRequest"></a>

### CreateRoleRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| company_id | [string](#string) |  |  |
| name | [string](#string) |  |  |
| description | [string](#string) |  |  |
| level | [int32](#int32) |  |  |
| permission_ids | [string](#string) | repeated |  |






<a name="platform-v1-CreateRoleResponse"></a>

### CreateRoleResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| role | [RoleData](#platform-v1-RoleData) |  |  |






<a name="platform-v1-GetUserPermissionsRequest"></a>

### GetUserPermissionsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| company_id | [string](#string) |  |  |
| user_id | [string](#string) |  |  |






<a name="platform-v1-GetUserPermissionsResponse"></a>

### GetUserPermissionsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| permissions | [PermissionResponse](#platform-v1-PermissionResponse) | repeated |  |
| role | [RoleData](#platform-v1-RoleData) |  |  |






<a name="platform-v1-ListPermissionsRequest"></a>

### ListPermissionsRequest







<a name="platform-v1-ListPermissionsResponse"></a>

### ListPermissionsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| permissions | [PermissionResponse](#platform-v1-PermissionResponse) | repeated |  |






<a name="platform-v1-ListRolesRequest"></a>

### ListRolesRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| company_id | [string](#string) |  |  |






<a name="platform-v1-ListRolesResponse"></a>

### ListRolesResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| roles | [RoleData](#platform-v1-RoleData) | repeated |  |






<a name="platform-v1-PermissionResponse"></a>

### PermissionResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  |  |
| feature | [string](#string) |  |  |
| action | [string](#string) |  |  |
| resource | [string](#string) |  |  |






<a name="platform-v1-RemoveRoleRequest"></a>

### RemoveRoleRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| company_id | [string](#string) |  |  |
| user_id | [string](#string) |  |  |
| role_id | [string](#string) |  |  |






<a name="platform-v1-RemoveRoleResponse"></a>

### RemoveRoleResponse







<a name="platform-v1-ResolveMembershipRequest"></a>

### ResolveMembershipRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| company_id | [string](#string) |  |  |
| user_id | [string](#string) |  |  |






<a name="platform-v1-ResolveMembershipResponse"></a>

### ResolveMembershipResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| company_id | [string](#string) |  |  |
| user_id | [string](#string) |  |  |
| role_level | [int32](#int32) |  |  |
| role_name | [string](#string) |  |  |
| source_company_id | [string](#string) |  |  |
| is_direct | [bool](#bool) |  |  |
| capped_level | [int32](#int32) |  |  |
| access_level | [string](#string) |  |  |






<a name="platform-v1-RoleData"></a>

### RoleData
RoleData is the shared role payload contained in ListRolesResponse and
GetUserPermissionsResponse, and wrapped by CreateRoleResponse. Per buf
STANDARD lint convention (RPC_RESPONSE_STANDARD_NAME).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  |  |
| name | [string](#string) |  |  |
| description | [string](#string) |  |  |
| level | [int32](#int32) |  |  |
| is_system | [bool](#bool) |  |  |





 

 

 


<a name="platform-v1-RBACService"></a>

### RBACService
RBACService manages roles and permissions.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| ListRoles | [ListRolesRequest](#platform-v1-ListRolesRequest) | [ListRolesResponse](#platform-v1-ListRolesResponse) |  |
| CreateRole | [CreateRoleRequest](#platform-v1-CreateRoleRequest) | [CreateRoleResponse](#platform-v1-CreateRoleResponse) |  |
| AssignRole | [AssignRoleRequest](#platform-v1-AssignRoleRequest) | [AssignRoleResponse](#platform-v1-AssignRoleResponse) |  |
| RemoveRole | [RemoveRoleRequest](#platform-v1-RemoveRoleRequest) | [RemoveRoleResponse](#platform-v1-RemoveRoleResponse) |  |
| ListPermissions | [ListPermissionsRequest](#platform-v1-ListPermissionsRequest) | [ListPermissionsResponse](#platform-v1-ListPermissionsResponse) |  |
| GetUserPermissions | [GetUserPermissionsRequest](#platform-v1-GetUserPermissionsRequest) | [GetUserPermissionsResponse](#platform-v1-GetUserPermissionsResponse) |  |
| CheckPermission | [CheckPermissionRequest](#platform-v1-CheckPermissionRequest) | [CheckPermissionResponse](#platform-v1-CheckPermissionResponse) |  |
| ResolveMembership | [ResolveMembershipRequest](#platform-v1-ResolveMembershipRequest) | [ResolveMembershipResponse](#platform-v1-ResolveMembershipResponse) |  |

 



<a name="platform_v1_recovery-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## platform/v1/recovery.proto



<a name="platform-v1-SelfRecoveryServiceConsumeRecoveryTokenRequest"></a>

### SelfRecoveryServiceConsumeRecoveryTokenRequest
SelfRecoveryServiceConsumeRecoveryTokenRequest validates the URL-safe
64-char hex token from the email link &#43; sets the new password. The server
validates new_password per NIST SP 800-63B (TRD 03-N policy).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| token | [string](#string) |  |  |
| new_password | [string](#string) |  |  |






<a name="platform-v1-SelfRecoveryServiceConsumeRecoveryTokenResponse"></a>

### SelfRecoveryServiceConsumeRecoveryTokenResponse
SelfRecoveryServiceConsumeRecoveryTokenResponse returns success &#43; a
one-time login session cookie value the portal sets to log the user in
immediately after recovery. login_session_token is empty on failure.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| success | [bool](#bool) |  |  |
| login_session_token | [string](#string) |  |  |
| expires_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |






<a name="platform-v1-SelfRecoveryServiceRequestRecoveryRequest"></a>

### SelfRecoveryServiceRequestRecoveryRequest
SelfRecoveryServiceRequestRecoveryRequest carries the tenant slug &#43; the
email entered by the user. Email enumeration defense: the server returns
success regardless of whether the account exists.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tenant_slug | [string](#string) |  |  |
| email | [string](#string) |  |  |






<a name="platform-v1-SelfRecoveryServiceRequestRecoveryResponse"></a>

### SelfRecoveryServiceRequestRecoveryResponse
SelfRecoveryServiceRequestRecoveryResponse is intentionally empty for v1.
Reserved for future rate-limit hints.





 

 

 


<a name="platform-v1-SelfRecoveryService"></a>

### SelfRecoveryService
SelfRecoveryService is the public (unauthenticated) self-service account
recovery surface. Two endpoints:

 1. RequestRecovery — initiates an email-verified recovery flow. Returns
    success even for unknown accounts (defense against email enumeration —
    see AOID runbook §H.3).
 2. ConsumeRecoveryToken — validates the token &#43; sets a new password,
    then returns a one-time login session token so the portal can log the
    user in immediately.

LIFE-04. Single-use tokens, default 30-minute TTL. Per-tenant policy
configurable. The mTLS / admin-session interceptor stack is NOT applied
to this service; it&#39;s reachable by anonymous traffic from the Obj 8 portal.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| RequestRecovery | [SelfRecoveryServiceRequestRecoveryRequest](#platform-v1-SelfRecoveryServiceRequestRecoveryRequest) | [SelfRecoveryServiceRequestRecoveryResponse](#platform-v1-SelfRecoveryServiceRequestRecoveryResponse) | RequestRecovery starts the recovery flow. Always returns success. |
| ConsumeRecoveryToken | [SelfRecoveryServiceConsumeRecoveryTokenRequest](#platform-v1-SelfRecoveryServiceConsumeRecoveryTokenRequest) | [SelfRecoveryServiceConsumeRecoveryTokenResponse](#platform-v1-SelfRecoveryServiceConsumeRecoveryTokenResponse) | ConsumeRecoveryToken validates the token &#43; sets a new password. |

 



<a name="platform_v1_registry-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## platform/v1/registry.proto



<a name="platform-v1-GetBadgeCountsRequest"></a>

### GetBadgeCountsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| company_id | [string](#string) |  |  |






<a name="platform-v1-GetBadgeCountsResponse"></a>

### GetBadgeCountsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| counts | [GetBadgeCountsResponse.CountsEntry](#platform-v1-GetBadgeCountsResponse-CountsEntry) | repeated |  |






<a name="platform-v1-GetBadgeCountsResponse-CountsEntry"></a>

### GetBadgeCountsResponse.CountsEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [int32](#int32) |  |  |






<a name="platform-v1-GetNavItemsRequest"></a>

### GetNavItemsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| company_id | [string](#string) |  |  |






<a name="platform-v1-GetNavItemsResponse"></a>

### GetNavItemsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| items | [NavItem](#platform-v1-NavItem) | repeated |  |






<a name="platform-v1-GetSearchScopesRequest"></a>

### GetSearchScopesRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| company_id | [string](#string) |  |  |






<a name="platform-v1-GetSearchScopesResponse"></a>

### GetSearchScopesResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| scopes | [SearchScope](#platform-v1-SearchScope) | repeated |  |






<a name="platform-v1-GetWidgetsRequest"></a>

### GetWidgetsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| company_id | [string](#string) |  |  |






<a name="platform-v1-GetWidgetsResponse"></a>

### GetWidgetsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| widgets | [Widget](#platform-v1-Widget) | repeated |  |






<a name="platform-v1-NavItem"></a>

### NavItem



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  |  |
| label | [string](#string) |  |  |
| icon | [string](#string) |  |  |
| path | [string](#string) |  |  |
| feature | [string](#string) |  |  |
| priority | [int32](#int32) |  |  |
| section | [string](#string) |  |  |






<a name="platform-v1-SearchScope"></a>

### SearchScope



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  |  |
| label | [string](#string) |  |  |
| feature | [string](#string) |  |  |






<a name="platform-v1-Widget"></a>

### Widget



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  |  |
| label | [string](#string) |  |  |
| type | [string](#string) |  |  |
| feature | [string](#string) |  |  |
| priority | [int32](#int32) |  |  |





 

 

 


<a name="platform-v1-RegistryService"></a>

### RegistryService
RegistryService returns sidebar/widgets for authenticated users.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| GetNavItems | [GetNavItemsRequest](#platform-v1-GetNavItemsRequest) | [GetNavItemsResponse](#platform-v1-GetNavItemsResponse) |  |
| GetWidgets | [GetWidgetsRequest](#platform-v1-GetWidgetsRequest) | [GetWidgetsResponse](#platform-v1-GetWidgetsResponse) |  |
| GetSearchScopes | [GetSearchScopesRequest](#platform-v1-GetSearchScopesRequest) | [GetSearchScopesResponse](#platform-v1-GetSearchScopesResponse) |  |
| GetBadgeCounts | [GetBadgeCountsRequest](#platform-v1-GetBadgeCountsRequest) | [GetBadgeCountsResponse](#platform-v1-GetBadgeCountsResponse) |  |

 



<a name="platform_v1_svid-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## platform/v1/svid.proto



<a name="platform-v1-IssueSVIDRequest"></a>

### IssueSVIDRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| requested_path | [string](#string) |  | Workload path component of the target SPIFFE ID. MUST start with /sa/ (service account) or /workload/. Rejected when empty, /, or beginning with /admin/. |
| csr_pem | [bytes](#bytes) |  | EC P-256 CSR PEM (BEGIN/END CERTIFICATE REQUEST). The CA verifies the self-signature before binding the public key. |
| requested_ttl_seconds | [int32](#int32) |  | Optional caller-suggested TTL in seconds. Server clamps to [1, 86400]. 0 = use spiffe.DefaultSVIDTTL (1h). |






<a name="platform-v1-IssueSVIDResponse"></a>

### IssueSVIDResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| svid_id | [string](#string) |  | Persistent UUID for the SVID row (used by RevokeSVID). |
| spiffe_id | [string](#string) |  | Canonical spiffe://&lt;trust-domain&gt;/&lt;path&gt; URI. |
| serial | [string](#string) |  | Decimal serial-number string (matches aoid.svids.serial_number). |
| cert_pem | [bytes](#bytes) |  | Leaf cert in PEM form. |
| cert_der | [bytes](#bytes) |  | Leaf cert in DER form (saves callers a parse step). |
| issued_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | Effective NotBefore. |
| expires_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | Effective NotAfter (post-clamp). |






<a name="platform-v1-ListMySVIDsRequest"></a>

### ListMySVIDsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| page_size | [int32](#int32) |  | Optional page size. Defaults to 100, capped at 500 server-side. |
| page_token | [string](#string) |  | Opaque continuation token; empty = first page. |






<a name="platform-v1-ListMySVIDsResponse"></a>

### ListMySVIDsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| svids | [SVIDSummary](#platform-v1-SVIDSummary) | repeated | Page of SVIDs, newest first. |
| next_page_token | [string](#string) |  | Opaque token to fetch the next page; empty when done. |






<a name="platform-v1-RevokeSVIDRequest"></a>

### RevokeSVIDRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| svid_id | [string](#string) |  | UUID of the SVID row to revoke. |






<a name="platform-v1-RevokeSVIDResponse"></a>

### RevokeSVIDResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| svid | [SVIDSummary](#platform-v1-SVIDSummary) |  | Returns the updated row so the caller can observe revoked_at. |






<a name="platform-v1-SVIDSummary"></a>

### SVIDSummary
SVIDSummary is the wire view of one aoid.svids row.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| svid_id | [string](#string) |  |  |
| spiffe_id | [string](#string) |  |  |
| serial | [string](#string) |  |  |
| issued_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| expires_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| revoked_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | Absent = active; present = revoked. |





 

 

 


<a name="platform-v1-SvidService"></a>

### SvidService
SvidService issues SPIFFE X.509-SVIDs to workloads already attested via
mTLS. This is a SEPARATE service from CredentialAdminService because the
interceptor stack differs:

  - CredentialAdminService: mTLS &#43; adminauth &#43; tenant-scope
  - SvidService:            mTLS &#43; spiffe-id-extraction (no admin RBAC)

The caller&#39;s CURRENT valid mTLS cert (which carries a spiffe:// URI SAN)
IS the attestation in v1. The handler trusts the Connect interceptor to
have validated the chain &#43; extracted the SPIFFE ID into the request
context. A revoked SVID would fail the mTLS handshake before reaching
this handler.

Re-attestation flow: a workload re-issues its SVID by re-handshaking
(mTLS) with its CURRENT still-valid SVID; the renewal request uses the
OLD SVID as the attestation cert. If the old SVID is revoked or
expired, the mTLS handshake fails BEFORE the IssueSVID handler runs --
the handler never sees the request. This is by design and means the
handler does not implement an attestation re-check.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| IssueSVID | [IssueSVIDRequest](#platform-v1-IssueSVIDRequest) | [IssueSVIDResponse](#platform-v1-IssueSVIDResponse) | IssueSVID mints a short-lived (&lt;=24h) X.509-SVID for the caller&#39;s requested workload path within the chassis trust domain. The path MUST start with /sa/ or /workload/ (admin/system paths rejected). |
| ListMySVIDs | [ListMySVIDsRequest](#platform-v1-ListMySVIDsRequest) | [ListMySVIDsResponse](#platform-v1-ListMySVIDsResponse) | ListMySVIDs returns SVIDs issued to the caller&#39;s underlying mTLS cert. Lets a workload audit which credentials were minted from its current attestation. Scoped by caller&#39;s mTLS cert serial (extracted by the interceptor) -- callers may not enumerate other workloads&#39; SVIDs. |
| RevokeSVID | [RevokeSVIDRequest](#platform-v1-RevokeSVIDRequest) | [RevokeSVIDResponse](#platform-v1-RevokeSVIDResponse) | RevokeSVID marks a previously-issued SVID revoked. Append-only on the revocations table; the CRL refresh loop picks up the change within one refresh interval. |

 



<a name="platform_v1_webhook-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## platform/v1/webhook.proto



<a name="platform-v1-DeleteWebhookRequest"></a>

### DeleteWebhookRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  |  |






<a name="platform-v1-DeleteWebhookResponse"></a>

### DeleteWebhookResponse







<a name="platform-v1-DeliveryResponse"></a>

### DeliveryResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  |  |
| webhook_id | [string](#string) |  |  |
| event_type | [string](#string) |  |  |
| status | [string](#string) |  |  |
| status_code | [int32](#int32) |  |  |
| attempts | [int32](#int32) |  |  |
| created_at | [string](#string) |  |  |






<a name="platform-v1-ListDeliveriesRequest"></a>

### ListDeliveriesRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| webhook_id | [string](#string) |  |  |
| limit | [int32](#int32) |  |  |
| offset | [int32](#int32) |  |  |






<a name="platform-v1-ListDeliveriesResponse"></a>

### ListDeliveriesResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| deliveries | [DeliveryResponse](#platform-v1-DeliveryResponse) | repeated |  |






<a name="platform-v1-ListWebhooksRequest"></a>

### ListWebhooksRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| company_id | [string](#string) |  |  |






<a name="platform-v1-ListWebhooksResponse"></a>

### ListWebhooksResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| webhooks | [WebhookData](#platform-v1-WebhookData) | repeated |  |






<a name="platform-v1-RegisterWebhookRequest"></a>

### RegisterWebhookRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| company_id | [string](#string) |  |  |
| url | [string](#string) |  |  |
| events | [string](#string) | repeated |  |






<a name="platform-v1-RegisterWebhookResponse"></a>

### RegisterWebhookResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| webhook | [WebhookData](#platform-v1-WebhookData) |  |  |






<a name="platform-v1-WebhookData"></a>

### WebhookData
WebhookData is the shared webhook payload wrapped by RegisterWebhookResponse
and contained in ListWebhooksResponse. Per buf STANDARD lint convention
(RPC_REQUEST_RESPONSE_UNIQUE).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  |  |
| company_id | [string](#string) |  |  |
| url | [string](#string) |  |  |
| secret | [string](#string) |  |  |
| events | [string](#string) | repeated |  |
| active | [bool](#bool) |  |  |
| created_at | [string](#string) |  |  |





 

 

 


<a name="platform-v1-WebhookService"></a>

### WebhookService
WebhookService manages webhook subscriptions and deliveries.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| RegisterWebhook | [RegisterWebhookRequest](#platform-v1-RegisterWebhookRequest) | [RegisterWebhookResponse](#platform-v1-RegisterWebhookResponse) |  |
| ListWebhooks | [ListWebhooksRequest](#platform-v1-ListWebhooksRequest) | [ListWebhooksResponse](#platform-v1-ListWebhooksResponse) |  |
| DeleteWebhook | [DeleteWebhookRequest](#platform-v1-DeleteWebhookRequest) | [DeleteWebhookResponse](#platform-v1-DeleteWebhookResponse) |  |
| ListDeliveries | [ListDeliveriesRequest](#platform-v1-ListDeliveriesRequest) | [ListDeliveriesResponse](#platform-v1-ListDeliveriesResponse) |  |

 



## Scalar Value Types

| .proto Type | Notes | C++ | Java | Python | Go | C# | PHP | Ruby |
| ----------- | ----- | --- | ---- | ------ | -- | -- | --- | ---- |
| <a name="double" /> double |  | double | double | float | float64 | double | float | Float |
| <a name="float" /> float |  | float | float | float | float32 | float | float | Float |
| <a name="int32" /> int32 | Uses variable-length encoding. Inefficient for encoding negative numbers – if your field is likely to have negative values, use sint32 instead. | int32 | int | int | int32 | int | integer | Bignum or Fixnum (as required) |
| <a name="int64" /> int64 | Uses variable-length encoding. Inefficient for encoding negative numbers – if your field is likely to have negative values, use sint64 instead. | int64 | long | int/long | int64 | long | integer/string | Bignum |
| <a name="uint32" /> uint32 | Uses variable-length encoding. | uint32 | int | int/long | uint32 | uint | integer | Bignum or Fixnum (as required) |
| <a name="uint64" /> uint64 | Uses variable-length encoding. | uint64 | long | int/long | uint64 | ulong | integer/string | Bignum or Fixnum (as required) |
| <a name="sint32" /> sint32 | Uses variable-length encoding. Signed int value. These more efficiently encode negative numbers than regular int32s. | int32 | int | int | int32 | int | integer | Bignum or Fixnum (as required) |
| <a name="sint64" /> sint64 | Uses variable-length encoding. Signed int value. These more efficiently encode negative numbers than regular int64s. | int64 | long | int/long | int64 | long | integer/string | Bignum |
| <a name="fixed32" /> fixed32 | Always four bytes. More efficient than uint32 if values are often greater than 2^28. | uint32 | int | int | uint32 | uint | integer | Bignum or Fixnum (as required) |
| <a name="fixed64" /> fixed64 | Always eight bytes. More efficient than uint64 if values are often greater than 2^56. | uint64 | long | int/long | uint64 | ulong | integer/string | Bignum |
| <a name="sfixed32" /> sfixed32 | Always four bytes. | int32 | int | int | int32 | int | integer | Bignum or Fixnum (as required) |
| <a name="sfixed64" /> sfixed64 | Always eight bytes. | int64 | long | int/long | int64 | long | integer/string | Bignum |
| <a name="bool" /> bool |  | bool | boolean | boolean | bool | bool | boolean | TrueClass/FalseClass |
| <a name="string" /> string | A string must always contain UTF-8 encoded or 7-bit ASCII text. | string | String | str/unicode | string | string | string | String (UTF-8) |
| <a name="bytes" /> bytes | May contain any arbitrary sequence of bytes. | string | ByteString | str | []byte | ByteString | string | String (ASCII-8BIT) |

