package types

import (
	"time"

	"gorm.io/gorm"
)

// KBPermission is the access level attached to a KB access grant (and,
// historically, to org shares). It is the cross-tenant counterpart of
// TenantRole: while TenantRole governs authority inside a tenant,
// KBPermission expresses what a grantee tenant is allowed to do with a
// knowledge base owned by another tenant.
type KBPermission string

const (
	// KBPermissionAdmin has full control over the shared knowledge base.
	KBPermissionAdmin KBPermission = "admin"
	// KBPermissionEditor can edit shared knowledge base content but cannot
	// manage settings.
	KBPermissionEditor KBPermission = "editor"
	// KBPermissionViewer can only view and search shared knowledge bases.
	KBPermissionViewer KBPermission = "viewer"
)

// IsValid checks if the permission level is valid.
func (r KBPermission) IsValid() bool {
	switch r {
	case KBPermissionAdmin, KBPermissionEditor, KBPermissionViewer:
		return true
	default:
		return false
	}
}

// HasPermission checks if this permission has at least the required level.
func (r KBPermission) HasPermission(required KBPermission) bool {
	roleLevel := map[KBPermission]int{
		KBPermissionAdmin:  3,
		KBPermissionEditor: 2,
		KBPermissionViewer: 1,
	}
	return roleLevel[r] >= roleLevel[required]
}

// MinKBPermission returns whichever of a / b is the lower permission on the
// admin > editor > viewer ladder. A zero/empty permission is treated as
// "less than viewer" so it short-circuits to whatever the other argument is.
func MinKBPermission(a, b KBPermission) KBPermission {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	if a.HasPermission(b) {
		return b
	}
	return a
}

// GrantStatus is the lifecycle state of a KB access grant.
type GrantStatus string

const (
	// GrantStatusPending: the grantee tenant's admin requested access and is
	// waiting for the owning tenant to review it.
	GrantStatusPending GrantStatus = "pending"
	// GrantStatusApproved: access is live — members of the grantee tenant
	// can read the KB.
	GrantStatusApproved GrantStatus = "approved"
	// GrantStatusRejected: the owning tenant declined the request. Terminal.
	GrantStatusRejected GrantStatus = "rejected"
	// GrantStatusRevoked: the owning tenant withdrew a previously approved
	// grant. Terminal.
	GrantStatusRevoked GrantStatus = "revoked"
	// GrantStatusExpired: an approved grant whose ExpiresAt has passed.
	// Terminal; a fresh request is needed for renewed access.
	GrantStatusExpired GrantStatus = "expired"
)

// IsTerminal reports whether the status can no longer grant access and no
// further transitions are possible from it.
func (s GrantStatus) IsTerminal() bool {
	switch s {
	case GrantStatusRejected, GrantStatusRevoked, GrantStatusExpired:
		return true
	default:
		return false
	}
}

// KBAccessGrant is a direct tenant-to-tenant read grant on one knowledge
// base. It replaces the former Organization + kb_shares indirection: the
// grantee tenant requests access, the owning tenant approves or rejects,
// and members of the grantee tenant read the KB while the grant is
// approved. Grants target tenants, not users — a member loses access
// automatically when removed from the grantee tenant.
type KBAccessGrant struct {
	ID string `json:"id" gorm:"type:varchar(36);primaryKey"`
	// KBID references knowledge_bases.id.
	KBID string `json:"kb_id" gorm:"column:kb_id;type:varchar(36);not null;index"`
	// OwnerTenantID is denormalized from knowledge_bases.tenant_id at
	// request time so owner-side listing needs no join.
	OwnerTenantID uint64 `json:"owner_tenant_id" gorm:"not null;index"`
	// GranteeTenantID is the tenant whose members gain access while the
	// grant is approved.
	GranteeTenantID uint64 `json:"grantee_tenant_id" gorm:"not null;index"`
	// Permission granted on the KB. Only KBPermissionViewer is exercised
	// today; the ladder stays for future write grants.
	Permission KBPermission `json:"permission" gorm:"type:varchar(16);not null;default:'viewer'"`
	// Status of the grant lifecycle; see GrantStatus constants.
	Status GrantStatus `json:"status" gorm:"type:varchar(16);not null;default:'pending';index"`
	// RequestedBy is the user ID of the grantee-side admin who created the
	// request. Empty for rows backfilled from legacy shares.
	RequestedBy string `json:"requested_by" gorm:"type:varchar(36);not null;default:''"`
	// ApprovedBy is the user ID of the owner-side admin/owner who last
	// reviewed the request (approve/reject/revoke).
	ApprovedBy *string `json:"approved_by,omitempty" gorm:"type:varchar(36)"`
	// Message is a free-form note from the requester or reviewer.
	Message string `json:"message,omitempty" gorm:"type:varchar(500)"`
	// ExpiresAt optionally ends an approved grant; nil means no expiry.
	ExpiresAt *time.Time `json:"expires_at,omitempty" gorm:"type:timestamp with time zone"`
	// RespondedAt records when the request was last reviewed.
	RespondedAt *time.Time     `json:"responded_at,omitempty" gorm:"type:timestamp with time zone"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `json:"deleted_at" gorm:"index"`
}

// TableName binds KBAccessGrant to the kb_access_grants table.
func (KBAccessGrant) TableName() string {
	return "kb_access_grants"
}

// IsLive reports whether the grant currently confers access: approved and
// not past its expiry.
func (g *KBAccessGrant) IsLive() bool {
	if g == nil || g.Status != GrantStatusApproved {
		return false
	}
	return g.ExpiresAt == nil || g.ExpiresAt.After(time.Now())
}

// RequestKBAccessRequest is the body of POST /knowledge-bases/:id/access-requests.
// An admin of the requesting tenant creates a pending grant.
type RequestKBAccessRequest struct {
	Message   string     `json:"message" binding:"omitempty,max=500"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// ReviewKBAccessGrantRequest is the body of PUT
// /tenants/:id/access-grants/:grant_id. An admin/owner of the owning
// tenant approves or rejects a pending request.
type ReviewKBAccessGrantRequest struct {
	Approved bool   `json:"approved"`
	Message  string `json:"message" binding:"omitempty,max=500"`
}

// KBAccessGrantResponse is the API projection of a grant row, enriched
// with display names for the management UI.
type KBAccessGrantResponse struct {
	ID              string       `json:"id"`
	KBID            string       `json:"kb_id"`
	KBName          string       `json:"kb_name,omitempty"`
	OwnerTenantID   uint64       `json:"owner_tenant_id"`
	OwnerTenantName string       `json:"owner_tenant_name,omitempty"`
	GranteeTenantID uint64       `json:"grantee_tenant_id"`
	GranteeName     string       `json:"grantee_tenant_name,omitempty"`
	Permission      KBPermission `json:"permission"`
	Status          GrantStatus  `json:"status"`
	RequestedBy     string       `json:"requested_by"`
	ApprovedBy      *string      `json:"approved_by,omitempty"`
	Message         string       `json:"message,omitempty"`
	ExpiresAt       *time.Time   `json:"expires_at,omitempty"`
	RespondedAt     *time.Time   `json:"responded_at,omitempty"`
	CreatedAt       time.Time    `json:"created_at"`
}
