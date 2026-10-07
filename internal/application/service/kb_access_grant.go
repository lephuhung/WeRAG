package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
)

// Sentinel errors returned by kbAccessGrantService. Handlers map them to
// HTTP statuses; callers compare with errors.Is.
var (
	// ErrGrantNotFound: no grant row matches the supplied id (or it does
	// not belong to the caller's tenant).
	ErrGrantNotFound = errors.New("kb access grant not found")
	// ErrGrantExists: a live (pending or approved) grant already covers
	// this (kb, tenant) pair.
	ErrGrantExists = errors.New("a live access grant already exists for this knowledge base")
	// ErrGrantNotPending: review/revoke arrived after the row was already
	// finalised. Maps to 409.
	ErrGrantNotPending = errors.New("access grant is no longer pending")
	// ErrGrantSelfTarget: a tenant cannot grant its own KB to itself.
	ErrGrantSelfTarget = errors.New("cannot request access to a knowledge base your tenant owns")
)

// ErrGrantDisabled: the request/approve flow is retired. Tenant-wide grants
// are issued only by the owning tenant's admin or a SuperAdmin
// (GrantTenantAccess); a tenant can no longer ask for access.
var ErrGrantDisabled = errors.New("access requests are retired; the owning tenant grants access directly")

// ErrGrantNotManager: the caller is neither a Tenant Admin of the KB's
// owning tenant nor a SuperAdmin.
var ErrGrantNotManager = errors.New("only the owning tenant's admin or a system admin may share this knowledge base")

// ErrGrantPlatformKB: platform-owned KBs are public and need no grant.
var ErrGrantPlatformKB = errors.New("platform knowledge bases are readable by everyone and cannot be granted")

// ErrGrantTenantNotFound: the grantee tenant does not exist.
var ErrGrantTenantNotFound = errors.New("grantee tenant not found")

// kbAccessGrantService implements interfaces.KBAccessGrantService. Route
// gates (admin on the grantee side for requests, admin/owner on the owner
// side for review) are enforced by middleware; the service still verifies
// tenant ownership on every mutation.
//
// Grants are owner-issued: GrantTenantAccess writes an approved row
// directly and Revoke withdraws it. RequestAccess and Review (the old
// request/approve flow) stay retired and return ErrGrantDisabled.
type kbAccessGrantService struct {
	grants  interfaces.KBAccessGrantRepository
	kbs     interfaces.KnowledgeBaseRepository
	tenants interfaces.TenantRepository
	audit   interfaces.AuditLogService
}

// NewKBAccessGrantService wires the grant service into the dig container.
func NewKBAccessGrantService(
	grants interfaces.KBAccessGrantRepository,
	kbs interfaces.KnowledgeBaseRepository,
	tenants interfaces.TenantRepository,
	audit interfaces.AuditLogService,
) interfaces.KBAccessGrantService {
	return &kbAccessGrantService{grants: grants, kbs: kbs, tenants: tenants, audit: audit}
}

func (s *kbAccessGrantService) emitAudit(ctx context.Context, tenantID uint64, action types.AuditAction,
	caller types.Caller, grant *types.KBAccessGrant,
) {
	if s.audit == nil || grant == nil {
		return
	}
	details, _ := json.Marshal(map[string]any{
		"grant_id":          grant.ID,
		"owner_tenant_id":   grant.OwnerTenantID,
		"grantee_tenant_id": grant.GranteeTenantID,
		"permission":        string(grant.Permission),
	})
	_ = s.audit.Log(ctx, &types.AuditLog{
		TenantID:    tenantID,
		ActorUserID: caller.UserID,
		ActorRole:   auditActorRole(ctx),
		Action:      action,
		ScopeType:   "knowledge_base",
		ScopeID:     grant.KBID,
		TargetType:  "kb_access_grant",
		TargetID:    grant.ID,
		Outcome:     types.AuditOutcomeSuccess,
		Details:     types.JSON(details),
	})
}

// RequestAccess creates a pending grant from the caller's tenant on the
// given KB. The KB must exist, belong to another tenant, and not already
// be covered by a live grant.
func (s *kbAccessGrantService) RequestAccess(
	ctx context.Context, caller types.Caller, kbID string, req *types.RequestKBAccessRequest,
) (*types.KBAccessGrant, error) {
	return nil, ErrGrantDisabled
}

// Review is retired: tenant-wide grants can no longer be approved or
// rejected. Always returns ErrGrantDisabled.
func (s *kbAccessGrantService) Review(
	ctx context.Context, caller types.Caller, grantID string, req *types.ReviewKBAccessGrantRequest,
) (*types.KBAccessGrant, error) {
	return nil, ErrGrantDisabled
}

// GrantTenantAccess shares a tenant-owned KB read-only with every member of
// another tenant. The grant is approved on creation; an existing live grant
// for the pair is returned as ErrGrantExists.
func (s *kbAccessGrantService) GrantTenantAccess(
	ctx context.Context, caller types.Caller, kbID string, req *types.GrantKBAccessRequest,
) (*types.KBAccessGrant, error) {
	if req == nil || req.GranteeTenantID == 0 {
		return nil, ErrGrantTenantNotFound
	}
	kb, err := s.kbs.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil || kb == nil {
		return nil, ErrGrantNotFound
	}
	if kb.OwnerTenantID == 0 {
		return nil, ErrGrantPlatformKB
	}
	if !access.CanManageKBSharing(ctx, caller, kb.OwnerTenantID) {
		return nil, ErrGrantNotManager
	}
	if req.GranteeTenantID == kb.OwnerTenantID {
		return nil, ErrGrantSelfTarget
	}
	if s.tenants != nil {
		if tenant, err := s.tenants.GetTenantByID(ctx, req.GranteeTenantID); err != nil || tenant == nil {
			return nil, ErrGrantTenantNotFound
		}
	}
	if req.ExpiresAt != nil && !req.ExpiresAt.After(time.Now()) {
		return nil, errors.New("expires_at must be in the future")
	}
	live, err := s.grants.GetLiveByPair(ctx, kbID, req.GranteeTenantID)
	if err != nil {
		return nil, err
	}
	if live != nil {
		return nil, ErrGrantExists
	}
	// An approved row past its expiry still occupies the one-live-grant
	// index; finalise it so the new grant can be written.
	if stale, err := s.grants.ListByOwnerTenant(ctx, kb.OwnerTenantID,
		[]types.GrantStatus{types.GrantStatusApproved}); err == nil {
		for _, g := range stale {
			if g.KBID == kbID && g.GranteeTenantID == req.GranteeTenantID && !g.IsLive() {
				g.Status = types.GrantStatusExpired
				_ = s.grants.UpdateStatus(ctx, g)
			}
		}
	}

	now := time.Now()
	approver := caller.UserID
	grant := &types.KBAccessGrant{
		ID:              uuid.NewString(),
		KBID:            kbID,
		OwnerTenantID:   kb.OwnerTenantID,
		GranteeTenantID: req.GranteeTenantID,
		Permission:      types.KBPermissionViewer,
		Status:          types.GrantStatusApproved,
		RequestedBy:     caller.UserID,
		ApprovedBy:      &approver,
		Message:         req.Message,
		ExpiresAt:       req.ExpiresAt,
		RespondedAt:     &now,
	}
	if err := s.grants.Create(ctx, grant); err != nil {
		return nil, err
	}
	s.emitAudit(ctx, kb.OwnerTenantID, types.AuditActionKBAccessApproved, caller, grant)
	return grant, nil
}

// Revoke withdraws a grant on a KB the caller manages. Reads stop at once:
// only approved rows authorize.
func (s *kbAccessGrantService) Revoke(
	ctx context.Context, caller types.Caller, grantID string,
) (*types.KBAccessGrant, error) {
	grant, err := s.grants.GetByID(ctx, grantID)
	if err != nil {
		return nil, err
	}
	if grant == nil {
		return nil, ErrGrantNotFound
	}
	if !access.CanManageKBSharing(ctx, caller, grant.OwnerTenantID) {
		return nil, ErrGrantNotFound
	}
	if grant.Status.IsTerminal() {
		return nil, ErrGrantNotPending
	}
	now := time.Now()
	revoker := caller.UserID
	grant.Status = types.GrantStatusRevoked
	grant.ApprovedBy = &revoker
	grant.RespondedAt = &now
	if err := s.grants.UpdateStatus(ctx, grant); err != nil {
		return nil, err
	}
	s.emitAudit(ctx, grant.OwnerTenantID, types.AuditActionKBAccessRevoked, caller, grant)
	return grant, nil
}

// ListByKB lists every grant on one KB for a caller who manages it.
func (s *kbAccessGrantService) ListByKB(
	ctx context.Context, caller types.Caller, kbID string,
) ([]*types.KBAccessGrantResponse, error) {
	kb, err := s.kbs.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil || kb == nil || kb.OwnerTenantID == 0 {
		return nil, ErrGrantNotFound
	}
	if !access.CanManageKBSharing(ctx, caller, kb.OwnerTenantID) {
		return nil, ErrGrantNotManager
	}
	rows, err := s.grants.ListByOwnerTenant(ctx, kb.OwnerTenantID, nil)
	if err != nil {
		return nil, err
	}
	filtered := rows[:0]
	for _, g := range rows {
		if g.KBID == kbID {
			filtered = append(filtered, g)
		}
	}
	return s.toResponses(ctx, filtered), nil
}

// ListIncoming lists grants on KBs the caller's tenant owns.
func (s *kbAccessGrantService) ListIncoming(
	ctx context.Context, caller types.Caller, statuses []types.GrantStatus,
) ([]*types.KBAccessGrantResponse, error) {
	rows, err := s.grants.ListByOwnerTenant(ctx, caller.TenantID, statuses)
	if err != nil {
		return nil, err
	}
	return s.toResponses(ctx, rows), nil
}

// ListOutgoing lists grants the caller's tenant requested.
func (s *kbAccessGrantService) ListOutgoing(
	ctx context.Context, caller types.Caller, statuses []types.GrantStatus,
) ([]*types.KBAccessGrantResponse, error) {
	rows, err := s.grants.ListByGranteeTenant(ctx, caller.TenantID, statuses)
	if err != nil {
		return nil, err
	}
	return s.toResponses(ctx, rows), nil
}

// toResponses enriches grant rows with KB and tenant display names.
// Lookup failures degrade to empty names rather than failing the list.
func (s *kbAccessGrantService) toResponses(
	ctx context.Context, rows []*types.KBAccessGrant,
) []*types.KBAccessGrantResponse {
	out := make([]*types.KBAccessGrantResponse, 0, len(rows))
	tenantIDs := make(map[uint64]struct{}, len(rows)*2)
	for _, g := range rows {
		tenantIDs[g.OwnerTenantID] = struct{}{}
		tenantIDs[g.GranteeTenantID] = struct{}{}
	}
	names := make(map[uint64]string, len(tenantIDs))
	if s.tenants != nil {
		ids := make([]uint64, 0, len(tenantIDs))
		for id := range tenantIDs {
			ids = append(ids, id)
		}
		if tenants, err := s.tenants.GetTenantsByIDs(ctx, ids); err == nil {
			for id, t := range tenants {
				if t != nil {
					names[id] = t.Name
				}
			}
		}
	}
	for _, g := range rows {
		resp := &types.KBAccessGrantResponse{
			ID:              g.ID,
			KBID:            g.KBID,
			OwnerTenantID:   g.OwnerTenantID,
			GranteeTenantID: g.GranteeTenantID,
			Permission:      g.Permission,
			Status:          g.Status,
			RequestedBy:     g.RequestedBy,
			ApprovedBy:      g.ApprovedBy,
			Message:         g.Message,
			ExpiresAt:       g.ExpiresAt,
			RespondedAt:     g.RespondedAt,
			CreatedAt:       g.CreatedAt,
			OwnerTenantName: names[g.OwnerTenantID],
			GranteeName:     names[g.GranteeTenantID],
		}
		if s.kbs != nil {
			if kb, err := s.kbs.GetKnowledgeBaseByID(ctx, g.KBID); err == nil && kb != nil {
				resp.KBName = kb.Name
			}
		}
		out = append(out, resp)
	}
	return out
}

// GetKBScope implements access.KBGrantLookup: the lightweight visibility
// projection used by the permission layer.
func (s *kbAccessGrantService) GetKBScope(ctx context.Context, kbID string) (*types.KBScope, error) {
	return s.kbs.GetKBScopeByID(ctx, kbID)
}

// ApprovedKBPermission implements access.KBGrantLookup: the live grant
// check used by ResolveKB / KBPermissions.Check.
func (s *kbAccessGrantService) ApprovedKBPermission(
	ctx context.Context, kbID string, granteeTenantID uint64,
) (types.KBPermission, bool, error) {
	grant, err := s.grants.GetLiveByPair(ctx, kbID, granteeTenantID)
	if err != nil {
		return "", false, err
	}
	if grant == nil || !grant.IsLive() {
		return "", false, nil
	}
	if grant.Status != types.GrantStatusApproved {
		return "", false, nil
	}
	// A grant speaks for the tenant that owned the KB when it was issued;
	// after an ownership change it no longer authorizes.
	if scope, err := s.kbs.GetKBScopeByID(ctx, kbID); err != nil || scope == nil ||
		scope.OwnerTenantID != grant.OwnerTenantID {
		return "", false, err
	}
	return grant.Permission, true, nil
}

// GrantedKBIDs returns the KB IDs granted to tenantID (approved, unexpired).
func (s *kbAccessGrantService) GrantedKBIDs(ctx context.Context, tenantID uint64) ([]string, error) {
	return s.grants.ListApprovedKBIDs(ctx, tenantID)
}

// DeleteAllForKB removes all grant rows on a KB when the KB is deleted.
func (s *kbAccessGrantService) DeleteAllForKB(ctx context.Context, kbID string) error {
	return s.grants.DeleteByKnowledgeBaseID(ctx, kbID)
}

// CountGrantsByKnowledgeBaseIDs returns live grant counts keyed by KB id.
func (s *kbAccessGrantService) CountGrantsByKnowledgeBaseIDs(ctx context.Context, kbIDs []string) (map[string]int, error) {
	return s.grants.CountByKBIDs(ctx, kbIDs)
}

// ensure interface compliance.
var _ interfaces.KBAccessGrantService = (*kbAccessGrantService)(nil)
