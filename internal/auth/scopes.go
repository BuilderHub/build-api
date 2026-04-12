package auth

import buildapiv1 "github.com/builderhub/build-api/api/gen/buildapi/v1"

// Fixed scope strings for API keys (stored in DB and checked by the interceptor).
const (
	ScopeOrganizationsRead  = "organizations:read"
	ScopeOrganizationsWrite = "organizations:write"
	ScopeBuildersRead       = "builders:read"
	ScopeBuildersWrite      = "builders:write"
)

// ValidScopes is the allowlist when creating API keys.
var ValidScopes = map[string]struct{}{
	ScopeOrganizationsRead:  {},
	ScopeOrganizationsWrite: {},
	ScopeBuildersRead:       {},
	ScopeBuildersWrite:      {},
}

// JWTOnlyMethods require a browser session (JWT); API keys are rejected.
var JWTOnlyMethods = map[string]bool{
	buildapiv1.AuthService_GetMe_FullMethodName:            true,
	buildapiv1.AuthService_UpdateProfile_FullMethodName:    true,
	buildapiv1.AuthService_CreateUserApiKey_FullMethodName:   true,
	buildapiv1.AuthService_ListUserApiKeys_FullMethodName:  true,
	buildapiv1.AuthService_RevokeUserApiKey_FullMethodName: true,
}

// MethodRequiredScope maps each protected RPC to the scope an API key must have. JWT sessions bypass (FullAccess).
var MethodRequiredScope = map[string]string{
	buildapiv1.OrganizationService_ListOrganizations_FullMethodName:       ScopeOrganizationsRead,
	buildapiv1.OrganizationService_GetOrganization_FullMethodName:     ScopeOrganizationsRead,
	buildapiv1.OrganizationService_CreateOrganization_FullMethodName:    ScopeOrganizationsWrite,
	buildapiv1.OrganizationService_UpdateOrganization_FullMethodName:    ScopeOrganizationsWrite,
	buildapiv1.OrganizationService_DeleteOrganization_FullMethodName:    ScopeOrganizationsWrite,
	buildapiv1.OrganizationService_ListOrganizationMembers_FullMethodName: ScopeOrganizationsRead,

	buildapiv1.BuildAPI_ListBuilders_FullMethodName:  ScopeBuildersRead,
	buildapiv1.BuildAPI_GetBuilder_FullMethodName:    ScopeBuildersRead,
	buildapiv1.BuildAPI_CreateBuilder_FullMethodName: ScopeBuildersWrite,
	buildapiv1.BuildAPI_UpdateBuilder_FullMethodName: ScopeBuildersWrite,
	buildapiv1.BuildAPI_DeleteBuilder_FullMethodName: ScopeBuildersWrite,
	buildapiv1.BuildAPI_WakeBuilder_FullMethodName:   ScopeBuildersWrite,
}

func hasScope(scopes []string, required string) bool {
	for _, s := range scopes {
		if s == required {
			return true
		}
	}
	return false
}
