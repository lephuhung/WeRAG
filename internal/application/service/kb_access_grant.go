package service

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
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

// ErrGrantDisabled: tenant-wide KB grants are retired (migration 000111
// revoked every live grant). Request/review/revoke entry points now
// reject so no tenant-wide grant can ever authorize access again.
// Cross-tenant reads use recipient-bound kb_invitations instead.
var ErrGrantDisabled = errors.New("tenant-wide kb grants are retired; use recipient-bound kb invitations")

// kbAccessGrantService implements interfaces.KBAccessGrantService. Route
// gates (admin on the grantee side for requests, admin/owner on the owner
// side for review) are enforced by middleware; the service still verifies
// tenant ownership on every mutation.
//
// NOTE: tenant-wide grants are retired — RequestAccess, Review and Revoke
// below unconditionally return ErrGrantDisabled (migration 000111 revoked
// all live rows). List/read paths stay for audit visibility of revoked
// rows; they never authorize access because only approved+unexpired rows
// grant, and none can exist anymore.
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

// Revoke is retired: all live tenant-wide grants were revoked by
// migration 000111. Always returns ErrGrantDisabled.
func (s *kbAccessGrantService) Revoke(
	ctx context.Context, caller types.Caller, grantID string,
) (*types.KBAccessGrant, error) {
	return nil, ErrGrantDisabled
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
