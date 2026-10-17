package dto

import "github.com/Tencent/WeKnora/internal/types"

// AuthLoginResponse is the HTTP-safe login / switch-tenant response shape.
type AuthLoginResponse struct {
	Success           bool               `json:"success"`
	Message           string             `json:"message,omitempty"`
	TwoFactorRequired bool               `json:"two_factor_required,omitempty"`
	User              *types.User        `json:"user,omitempty"`
	ActiveTenant      *TenantResponse    `json:"active_tenant,omitempty"`
	Memberships       []types.Membership `json:"memberships"`
	Token             string             `json:"token,omitempty"`
	RefreshToken      string             `json:"refresh_token,omitempty"`
}

// AuthOIDCCallbackResponse is the HTTP-safe OIDC callback payload shape.
type AuthOIDCCallbackResponse struct {
	Success      bool               `json:"success"`
	Message      string             `json:"message,omitempty"`
	User         *types.User        `json:"user,omitempty"`
	Tenant       *TenantResponse    `json:"tenant,omitempty"`
	Memberships  []types.Membership `json:"memberships"`
	Token        string             `json:"token,omitempty"`
	RefreshToken string             `json:"refresh_token,omitempty"`
	IsNewUser    bool               `json:"is_new_user,omitempty"`
}

// NewAuthLoginResponse converts a service-layer login response for HTTP output.
func NewAuthLoginResponse(resp *types.LoginResponse) *AuthLoginResponse {
	if resp == nil {
		return nil
	}
	var role types.TenantRole
	if resp.ActiveTenant != nil {
		role = authResponseRole(resp.User, resp.Memberships, resp.ActiveTenant.ID)
	}
	return &AuthLoginResponse{
		Success:           resp.Success,
		Message:           resp.Message,
		TwoFactorRequired: resp.TwoFactorRequired,
		User:              resp.User,
		ActiveTenant:      NewTenantResponseWithRole(resp.ActiveTenant, role),
		Memberships:       resp.Memberships,
		Token:             resp.Token,
		RefreshToken:      resp.RefreshToken,
	}
}

// NewAuthOIDCCallbackResponse converts an OIDC callback response for HTTP output.
func NewAuthOIDCCallbackResponse(resp *types.OIDCCallbackResponse) *AuthOIDCCallbackResponse {
	if resp == nil {
		return nil
	}
	var role types.TenantRole
	if resp.Tenant != nil {
		role = authResponseRole(resp.User, resp.Memberships, resp.Tenant.ID)
	}
	return &AuthOIDCCallbackResponse{
		Success:      resp.Success,
		Message:      resp.Message,
		User:         resp.User,
		Tenant:       NewTenantResponseWithRole(resp.Tenant, role),
		Memberships:  resp.Memberships,
		Token:        resp.Token,
		RefreshToken: resp.RefreshToken,
		IsNewUser:    resp.IsNewUser,
	}
}

// authResponseRole is the role the tenant profile is rendered for in a
// login/OIDC response: the membership role, or Admin for an explicit
// SuperAdmin (the login flow authenticates a human, so the user row's
// system-admin flag is the explicit grant), matching NewTenantResponse.
func authResponseRole(user *types.User, memberships []types.Membership, tenantID uint64) types.TenantRole {
	role := membershipRoleForTenant(memberships, tenantID)
	if !role.IsTenantAdmin() && user != nil && user.IsSystemAdmin {
		return types.TenantRoleAdmin
	}
	return role
}

func membershipRoleForTenant(memberships []types.Membership, tenantID uint64) types.TenantRole {
	for _, m := range memberships {
		if m.TenantID == tenantID && m.Role.IsValid() {
			return m.Role
		}
	}
	return ""
}
