package types

import (
	"time"

	"gorm.io/gorm"
)

// TenantOrgRole is the role a user holds inside a tenant org. Unlike
// Organization (the cross-tenant sharing entity), a TenantOrg is a
// user group inside one tenant — the unit org-scoped knowledge bases
// are visible to.
type TenantOrgRole string

const (
	// TenantOrgRoleManager can manage the org's member roster, issue
	// invite links, and edit org-scoped knowledge bases.
	TenantOrgRoleManager TenantOrgRole = "manager"
	// TenantOrgRoleMember can read/search org-scoped knowledge bases.
	TenantOrgRoleMember TenantOrgRole = "member"
)

// IsValid checks if the role is valid.
func (r TenantOrgRole) IsValid() bool {
	return r == TenantOrgRoleManager || r == TenantOrgRoleMember
}

// HasPermission reports whether r grants at least the required level.
func (r TenantOrgRole) HasPermission(required TenantOrgRole) bool {
	level := map[TenantOrgRole]int{
		TenantOrgRoleManager: 2,
		TenantOrgRoleMember:  1,
	}
	return level[r] >= level[required]
}

// TenantOrg is a group of users inside one tenant. Org-scoped
// knowledge bases (knowledge_bases.visibility = 'org') are readable
// only by members of the bound org plus tenant Admin/Owner and
// system admins.
type TenantOrg struct {
	ID          uint64 `json:"id" gorm:"primaryKey;autoIncrement"`
	TenantID    uint64 `json:"tenant_id" gorm:"not null;index"`
	Name        string `json:"name" gorm:"type:varchar(255);not null"`
	Description string `json:"description" gorm:"type:text"`
	// CreatedBy is the user id that created the org (informational).
	CreatedBy string         `json:"created_by" gorm:"type:varchar(36);not null;default:''"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `json:"deleted_at" gorm:"index"`
}

// TableName binds TenantOrg to the tenant_orgs table.
func (TenantOrg) TableName() string { return "tenant_orgs" }

// TenantOrgMember is one user's membership in a tenant org. The user
// must also hold a tenant_members row for the org's tenant; invite
// flows create both rows together.
type TenantOrgMember struct {
	ID        uint64        `json:"id" gorm:"primaryKey;autoIncrement"`
	OrgID     uint64        `json:"org_id" gorm:"not null;index"`
	UserID    string        `json:"user_id" gorm:"type:varchar(36);not null;index"`
	Role      TenantOrgRole `json:"role" gorm:"type:varchar(20);not null;default:'member'"`
	CreatedAt time.Time     `json:"created_at"`
}

// TableName binds TenantOrgMember to the tenant_org_members table.
func (TenantOrgMember) TableName() string { return "tenant_org_members" }

// ----------------------
// Request/Response Types
// ----------------------

// CreateTenantOrgRequest creates an org inside the caller's tenant.
type CreateTenantOrgRequest struct {
	Name        string `json:"name" binding:"required,min=1,max=255"`
	Description string `json:"description" binding:"max=1000"`
}

// UpdateTenantOrgRequest renames an org.
type UpdateTenantOrgRequest struct {
	Name        *string `json:"name" binding:"omitempty,min=1,max=255"`
	Description *string `json:"description" binding:"omitempty,max=1000"`
}

// AddTenantOrgMemberRequest adds an existing tenant member to an org.
type AddTenantOrgMemberRequest struct {
	UserID string        `json:"user_id" binding:"required"`
	Role   TenantOrgRole `json:"role" binding:"required"`
}

// UpdateTenantOrgMemberRequest changes a member's org role.
type UpdateTenantOrgMemberRequest struct {
	Role TenantOrgRole `json:"role" binding:"required"`
}

// CreateTenantOrgInviteRequest issues a share-link invitation bound to
// the org. Accepting it registers the account, joins the tenant with
// the requested role, and enrols the user into this org.
type CreateTenantOrgInviteRequest struct {
	// Role is the tenant role the invitee receives (viewer/contributor
	// recommended; admin/owner invites stay with tenant-level APIs).
	Role    TenantRole `json:"role" binding:"required"`
	Message string     `json:"message" binding:"max=500"`
}

// TenantOrgResponse is the API projection of an org with member count.
type TenantOrgResponse struct {
	ID          uint64    `json:"id"`
	TenantID    uint64    `json:"tenant_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedBy   string    `json:"created_by"`
	MemberCount int       `json:"member_count"`
	CreatedAt   time.Time `json:"created_at"`
}

// TenantOrgMemberResponse joins the membership row with user fields.
type TenantOrgMemberResponse struct {
	OrgID     uint64        `json:"org_id"`
	UserID    string        `json:"user_id"`
	Username  string        `json:"username,omitempty"`
	Email     string        `json:"email,omitempty"`
	Role      TenantOrgRole `json:"role"`
	CreatedAt time.Time     `json:"created_at"`
}
