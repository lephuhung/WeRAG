package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/application/access"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// Service reads accept exact upstream grants. Otherwise cross-tenant reads
// require a user and a tenant access grant resolved for the original caller.
// Scope lookups stay enabled for userless principals so 'public' visibility
// still applies to API-key and integration callers.
func kbReadPermissions(ctx context.Context, lookup access.KBGrantLookup) *access.KBPermissions {
	p := access.NewKBPermissions(ctx, lookup)
	if types.CallerFromContext(ctx).UserID == "" {
		return p.WithoutGrantExpansion()
	}
	return p
}

// kbWritableIDs returns the target KBs the caller may modify: only those of
// its own workspace. Tenant access grants are viewer-only — a foreign grant
// never makes a KB writable.
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
	} else if roleEnforced && !caller.Role.HasPermission(types.TenantRoleMember) {
		return nil
	}
	var scopes map[string]*types.KBScope
	scopeOf := func(kbID string) *types.KBScope {
		if lookup == nil {
			return nil
		}
		if scopes == nil {
			scopes = make(map[string]*types.KBScope)
		}
		if s, ok := scopes[kbID]; ok {
			return s
		}
		s, err := lookup.GetKBScope(ctx, kbID)
		if err != nil {
			scopes[kbID] = nil
			return nil
		}
		scopes[kbID] = s
		return s
	}
	seen := make(map[string]bool, len(targets))
	var ids []string
	for _, target := range targets {
		if target == nil || target.KnowledgeBaseID == "" || seen[target.KnowledgeBaseID] {
			continue
		}
		seen[target.KnowledgeBaseID] = true
		writable := caller.TenantID != 0 && target.TenantID == caller.TenantID
		if writable {
			if scope := scopeOf(target.KnowledgeBaseID); scope != nil && scope.Visibility == types.KBVisibilityPublic {
				// Public corpus writes stay with the owning tenant's
				// Owner, system admins and tenant-level API keys.
				_, isKey := types.TenantAPIKeyScopeFromContext(ctx)
				writable = isKey || types.IsSystemAdminFromContext(ctx) ||
					caller.Role.HasPermission(types.TenantRoleOwner)
			}
		}
		if writable {
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
	return 0, apperrors.NewForbiddenError("无权访问该知识库")
}

func requireKBWrite(ctx context.Context, kb *types.KnowledgeBase) (context.Context, error) {
	if err := access.RequireKBWrite(ctx, kb); err != nil {
		return ctx, apperrors.NewForbiddenError("无权修改该知识库")
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
		return ctx, apperrors.NewServiceUnavailableError("无法获取知识库所属空间")
	}
	tenant, err := tenants.GetTenantByID(ctx, kb.TenantID)
	if err != nil {
		return ctx, err
	}
	if tenant == nil || tenant.ID != kb.TenantID {
		return ctx, apperrors.NewNotFoundError("知识库所属空间不存在")
	}
	return context.WithValue(ctx, types.TenantInfoContextKey, tenant), nil
}
