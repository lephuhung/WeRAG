package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/application/access"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// Service reads accept exact upstream grants (including recipient-bound KB
// invitations). Otherwise they remain within the caller's own tenant; public
// visibility and tenant-wide grants do not expand this scope.
func kbReadPermissions(ctx context.Context, lookup access.KBGrantLookup) *access.KBPermissions {
	p := access.NewKBPermissions(ctx, lookup)
	if types.CallerFromContext(ctx).UserID == "" {
		return p.WithoutGrantExpansion()
	}
	return p
}

// kbWritableIDs returns the target KBs the caller may modify through an
// agent turn: KBs of its own workspace (the execution boundary, unchanged)
// on which access.DecideKB grants Contribute — so member_default=view and
// restricted KBs without a manager role stay read-only, and platform-owned
// rows are never written by a member. ACL (invites, tenant grants) never
// makes a KB writable. A KB whose scope cannot be loaded fails closed.
//
// The caller must also be allowed to write KB content at all, as on the HTTP
// write routes: Member+, or for a scoped API key the ingest capability, as in
// access.RequireKBWrite. roleEnforced mirrors the RBAC rollout switch, under
// which role checks only log.
func kbWritableIDs(
	ctx context.Context, lookup access.KBGrantLookup, targets types.SearchTargets, roleEnforced bool,
) []string {
	caller := types.CallerFromContext(ctx)
	if scope, ok := types.TenantAPIKeyScopeFromContext(ctx); ok {
		if !scope.FullAccess && !scope.HasCapability(types.APIKeyCapabilityIngest) {
			return nil
		}
	} else if roleEnforced && !caller.Role.HasPermission(types.TenantRoleMember) &&
		!access.IsExplicitHumanSuperAdmin(ctx, caller) {
		return nil
	}
	if lookup == nil || caller.TenantID == 0 {
		return nil
	}
	seen := make(map[string]bool, len(targets))
	var ids []string
	for _, target := range targets {
		if target == nil || target.KnowledgeBaseID == "" || seen[target.KnowledgeBaseID] {
			continue
		}
		seen[target.KnowledgeBaseID] = true
		if target.TenantID != caller.TenantID {
			continue
		}
		scope, err := lookup.GetKBScope(ctx, target.KnowledgeBaseID)
		if err != nil || scope == nil || scope.TenantID != target.TenantID {
			continue
		}
		kb := &types.KnowledgeBase{
			ID: target.KnowledgeBaseID, TenantID: scope.TenantID, OwnerTenantID: scope.OwnerTenantID,
			Visibility: scope.Visibility, MemberDefault: scope.MemberDefault,
		}
		if _, err := access.DecideKB(ctx, caller, kb, access.ActionContribute, access.KBLookups{}); err == nil {
			ids = append(ids, target.KnowledgeBaseID)
		}
	}
	return ids
}

func resolveKBReadTenant(ctx context.Context, kb *types.KnowledgeBase, lookup access.KBGrantLookup) (uint64, error) {
	if kb != nil {
		allowed, err := kbReadPermissions(ctx, lookup).Check(kb.ID, kb.TenantID, types.KBPermissionViewer)
		if err == nil && allowed {
			return kb.TenantID, nil
		}
	}
	return 0, apperrors.NewForbiddenError("không có quyền truy cập kho tri thức này")
}

func requireKBWrite(ctx context.Context, kb *types.KnowledgeBase) (context.Context, error) {
	if err := access.RequireKBWrite(ctx, kb); err != nil {
		return ctx, apperrors.NewForbiddenError("không có quyền sửa kho tri thức này")
	}
	return types.WithExecutionTenant(ctx, kb.TenantID), nil
}

func (s *knowledgeService) writableFAQKnowledgeBase(
	ctx context.Context,
	kbID string,
) (*types.KnowledgeBase, context.Context, error) {
	kb, err := s.validateFAQKnowledgeBase(ctx, kbID)
	if err != nil {
		return nil, ctx, err
	}
	ctx, err = requireKBWrite(ctx, kb)
	if err == nil {
		ctx, err = withKBWriteTenantInfo(ctx, kb, s.tenantRepo)
	}
	return kb, ctx, err
}

// A shared KB mutation must also resolve models/index backends against its
// owner. The authenticated caller remains unchanged when TenantInfo changes.
func withKBWriteTenantInfo(
	ctx context.Context,
	kb *types.KnowledgeBase,
	tenants interfaces.TenantRepository,
) (context.Context, error) {
	if tenant, ok := types.TenantInfoFromContext(ctx); ok && tenant != nil && tenant.ID == kb.TenantID {
		return ctx, nil
	}
	if tenants == nil {
		return ctx, apperrors.NewServiceUnavailableError("không lấy được đơn vị sở hữu kho tri thức")
	}
	tenant, err := tenants.GetTenantByID(ctx, kb.TenantID)
	if err != nil {
		return ctx, err
	}
	if tenant == nil || tenant.ID != kb.TenantID {
		return ctx, apperrors.NewNotFoundError("không tìm thấy đơn vị sở hữu kho tri thức")
	}
	return context.WithValue(ctx, types.TenantInfoContextKey, tenant), nil
}
