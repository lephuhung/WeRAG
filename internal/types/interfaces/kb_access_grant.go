package interfaces

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// KBAccessGrantRepository persists tenant-to-tenant knowledge-base access
// grants. Live-state uniqueness (at most one pending/approved row per
// (kb_id, grantee_tenant_id)) is enforced by the partial unique index
// uniq_kb_grant_pending.
type KBAccessGrantRepository interface {
	// Create inserts a grant row. Implementations should surface a
	// duplicate-live-grant conflict as ErrKBAccessGrantExists.
	Create(ctx context.Context, grant *types.KBAccessGrant) error
	// GetByID returns the grant row or (nil, nil) when missing.
	GetByID(ctx context.Context, id string) (*types.KBAccessGrant, error)
	// GetLiveByPair returns the pending or approved grant for
	// (kb_id, grantee_tenant_id), or (nil, nil).
	GetLiveByPair(ctx context.Context, kbID string, granteeTenantID uint64) (*types.KBAccessGrant, error)
	// ListByOwnerTenant lists grants where the tenant owns the KB.
	// statuses filters when non-empty.
	ListByOwnerTenant(ctx context.Context, tenantID uint64, statuses []types.GrantStatus) ([]*types.KBAccessGrant, error)
	// ListByGranteeTenant lists grants where the tenant is the grantee.
	ListByGranteeTenant(ctx context.Context, tenantID uint64, statuses []types.GrantStatus) ([]*types.KBAccessGrant, error)
	// ListApprovedKBIDs returns the KB IDs granted to tenantID (approved
	// and unexpired).
	ListApprovedKBIDs(ctx context.Context, tenantID uint64) ([]string, error)
	// UpdateStatus transitions the row's status and stamps the reviewer
	// fields. Conditional updates (e.g. pending-only) belong to
	// MarkStatusIfPending.
	UpdateStatus(ctx context.Context, grant *types.KBAccessGrant) error
	// MarkStatusIfPending atomically transitions a pending row to status;
	// returns gorm.ErrRecordNotFound when the row already left pending.
	MarkStatusIfPending(ctx context.Context, id string, status types.GrantStatus, approvedBy string, message string) error
	// DeleteByKnowledgeBaseID soft-deletes all grant rows for a KB; called
	// when the KB itself is deleted.
	DeleteByKnowledgeBaseID(ctx context.Context, kbID string) error
	// CountByKBIDs returns live (pending/approved) grant counts keyed by KB id.
	CountByKBIDs(ctx context.Context, kbIDs []string) (map[string]int, error)
}

// KBAccessGrantService manages the grant lifecycle: grantee-side requests
// and owner-side review/revoke.
type KBAccessGrantService interface {
	// RequestAccess creates a pending grant for (kbID, callerTenantID).
	RequestAccess(ctx context.Context, caller types.Caller, kbID string, req *types.RequestKBAccessRequest) (*types.KBAccessGrant, error)
	// Review approves or rejects a pending grant owned by caller's tenant.
	Review(ctx context.Context, caller types.Caller, grantID string, req *types.ReviewKBAccessGrantRequest) (*types.KBAccessGrant, error)
	// Revoke withdraws an approved grant owned by caller's tenant.
	Revoke(ctx context.Context, caller types.Caller, grantID string) (*types.KBAccessGrant, error)
	// ListIncoming lists grants where caller's tenant owns the KB.
	ListIncoming(ctx context.Context, caller types.Caller, statuses []types.GrantStatus) ([]*types.KBAccessGrantResponse, error)
	// ListOutgoing lists grants where caller's tenant is the grantee.
	ListOutgoing(ctx context.Context, caller types.Caller, statuses []types.GrantStatus) ([]*types.KBAccessGrantResponse, error)
	// GetKBScope returns the visibility scope of one KB (implements
	// access.KBGrantLookup).
	GetKBScope(ctx context.Context, kbID string) (*types.KBScope, error)
	// ApprovedKBPermission returns the live grant permission for
	// (kbID, granteeTenantID) (implements access.KBGrantLookup).
	ApprovedKBPermission(ctx context.Context, kbID string, granteeTenantID uint64) (types.KBPermission, bool, error)
	// GrantedKBIDs returns the KB IDs with a live approved grant to
	// tenantID — used when assembling a tenant's readable KB set.
	GrantedKBIDs(ctx context.Context, tenantID uint64) ([]string, error)
	// DeleteAllForKB removes every grant on a KB; called when the KB
	// itself is deleted.
	DeleteAllForKB(ctx context.Context, kbID string) error
	// CountGrantsByKnowledgeBaseIDs returns live grant counts keyed by KB id
	// for the "shared" badge on KB list rows.
	CountGrantsByKnowledgeBaseIDs(ctx context.Context, kbIDs []string) (map[string]int, error)
}
