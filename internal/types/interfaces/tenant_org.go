package interfaces

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// TenantOrgRepository persists tenant orgs and their member roster.
// Tenant orgs are user groups inside one tenant; they are the scope
// unit for org-visibility knowledge bases.
type TenantOrgRepository interface {
	// CreateOrg inserts a new org row.
	CreateOrg(ctx context.Context, org *types.TenantOrg) error
	// GetOrgByID loads one org (no tenant filter; callers check TenantID).
	GetOrgByID(ctx context.Context, id uint64) (*types.TenantOrg, error)
	// ListOrgsByTenant lists non-deleted orgs of a tenant.
	ListOrgsByTenant(ctx context.Context, tenantID uint64) ([]*types.TenantOrg, error)
	// UpdateOrg updates name/description.
	UpdateOrg(ctx context.Context, org *types.TenantOrg) error
	// DeleteOrg soft-deletes the org and removes its member rows.
	DeleteOrg(ctx context.Context, id uint64) error

	// AddMember inserts a membership row. Duplicate (org, user) is an error.
	AddMember(ctx context.Context, member *types.TenantOrgMember) error
	// RemoveMember deletes one membership row.
	RemoveMember(ctx context.Context, orgID uint64, userID string) error
	// UpdateMemberRole changes a member's org role.
	UpdateMemberRole(ctx context.Context, orgID uint64, userID string, role types.TenantOrgRole) error
	// GetMember returns one membership or nil when absent.
	GetMember(ctx context.Context, orgID uint64, userID string) (*types.TenantOrgMember, error)
	// ListMembers returns the roster of one org.
	ListMembers(ctx context.Context, orgID uint64) ([]*types.TenantOrgMember, error)
	// ListOrgIDsForUser returns the org ids of a tenant the user belongs to.
	ListOrgIDsForUser(ctx context.Context, tenantID uint64, userID string) ([]uint64, error)
	// CountMembers returns the member count of one org.
	CountMembers(ctx context.Context, orgID uint64) (int64, error)
}

// TenantOrgService exposes org management to handlers.
type TenantOrgService interface {
	CreateOrg(ctx context.Context, req *types.CreateTenantOrgRequest) (*types.TenantOrg, error)
	GetOrg(ctx context.Context, id uint64) (*types.TenantOrg, error)
	ListOrgs(ctx context.Context) ([]*types.TenantOrgResponse, error)
	UpdateOrg(ctx context.Context, id uint64, req *types.UpdateTenantOrgRequest) (*types.TenantOrg, error)
	DeleteOrg(ctx context.Context, id uint64) error

	AddMember(ctx context.Context, orgID uint64, req *types.AddTenantOrgMemberRequest) error
	RemoveMember(ctx context.Context, orgID uint64, userID string) error
	UpdateMemberRole(ctx context.Context, orgID uint64, userID string, req *types.UpdateTenantOrgMemberRequest) error
	ListMembers(ctx context.Context, orgID uint64) ([]*types.TenantOrgMemberResponse, error)

	// CreateInviteLink mints a share-link invitation bound to the org.
	// Returns the invitation and its plaintext token.
	CreateInviteLink(ctx context.Context, orgID uint64, req *types.CreateTenantOrgInviteRequest) (*types.TenantInvitation, string, error)
	// OrgIDsForUser returns the caller's org ids inside their tenant.
	OrgIDsForUser(ctx context.Context, tenantID uint64, userID string) ([]uint64, error)
	// MemberRole resolves the caller's org role (member/manager).
	MemberRole(ctx context.Context, orgID uint64, userID string) (types.TenantOrgRole, error)
}
