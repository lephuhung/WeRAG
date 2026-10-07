package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/Tencent/WeKnora/internal/application/access"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// subscriptionCaller returns the human caller allowed to manage
// subscriptions: an authenticated human with an active tenant.
func subscriptionCaller(ctx context.Context) (types.Caller, error) {
	caller := types.CallerFromContext(ctx)
	if caller.TenantID == 0 || !access.IsAuthenticatedHuman(ctx, caller) {
		return caller, apperrors.NewForbiddenError("subscriptions need a signed-in member of a workspace")
	}
	return caller, nil
}

// subscriptionSubject resolves the (tenant, user) key: tenant-wide rows use
// an empty user and require Tenant Admin authority.
func subscriptionSubject(caller types.Caller, tenantWide bool) (string, error) {
	if !tenantWide {
		return caller.UserID, nil
	}
	if !caller.Role.IsTenantAdmin() {
		return "", apperrors.NewForbiddenError("only a tenant admin may subscribe the whole workspace")
	}
	return "", nil
}

// SubscribeKnowledgeBase puts a published KB into the caller's (or the
// caller's tenant's) default retrieval scope.
func (s *knowledgeBaseService) SubscribeKnowledgeBase(ctx context.Context, kbID string, tenantWide bool) error {
	if s.subscriptionRepo == nil {
		return apperrors.NewInternalServerError("subscriptions unavailable")
	}
	caller, err := subscriptionCaller(ctx)
	if err != nil {
		return err
	}
	userID, err := subscriptionSubject(caller, tenantWide)
	if err != nil {
		return err
	}
	kb, err := s.repo.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil || kb == nil {
		return apperrors.NewNotFoundError("knowledge base not found")
	}
	if !kb.IsTenantPublished() {
		return apperrors.NewBadRequestError("only published knowledge bases can be subscribed")
	}
	if kb.OwnerTenantID == caller.TenantID {
		return apperrors.NewBadRequestError("your workspace already searches its own knowledge bases")
	}
	return s.subscriptionRepo.Subscribe(ctx, &types.KBSubscription{
		ID:        uuid.NewString(),
		KBID:      kbID,
		TenantID:  caller.TenantID,
		UserID:    userID,
		CreatedBy: caller.UserID,
		CreatedAt: time.Now(),
	})
}

// UnsubscribeKnowledgeBase removes the caller's (or tenant's) subscription.
func (s *knowledgeBaseService) UnsubscribeKnowledgeBase(ctx context.Context, kbID string, tenantWide bool) error {
	if s.subscriptionRepo == nil {
		return apperrors.NewInternalServerError("subscriptions unavailable")
	}
	caller, err := subscriptionCaller(ctx)
	if err != nil {
		return err
	}
	userID, err := subscriptionSubject(caller, tenantWide)
	if err != nil {
		return err
	}
	return s.subscriptionRepo.Unsubscribe(ctx, kbID, caller.TenantID, userID)
}

// readerSubscriptions maps each KB the caller's scope subscribes to and
// whether that comes from the caller or the whole tenant. Errors degrade to
// no subscriptions.
func (s *knowledgeBaseService) readerSubscriptions(ctx context.Context) (mine, tenant map[string]bool) {
	mine, tenant = map[string]bool{}, map[string]bool{}
	if s.subscriptionRepo == nil {
		return mine, tenant
	}
	caller := types.CallerFromContext(ctx)
	if caller.TenantID == 0 || !access.IsAuthenticatedHuman(ctx, caller) {
		return mine, tenant
	}
	rows, err := s.subscriptionRepo.ListForReader(ctx, caller.TenantID, caller.UserID)
	if err != nil {
		logger.Warnf(ctx, "Failed to list KB subscriptions for tenant=%d: %v", caller.TenantID, err)
		return mine, tenant
	}
	for _, row := range rows {
		if row.UserID == "" {
			tenant[row.KBID] = true
		} else {
			mine[row.KBID] = true
		}
	}
	return mine, tenant
}

// subscribedPublishedKBs loads the published KBs the caller's scope
// subscribes to, with their subscription flags set. Rows that are no longer
// published are skipped.
func (s *knowledgeBaseService) subscribedPublishedKBs(ctx context.Context) []*types.KnowledgeBase {
	mine, tenant := s.readerSubscriptions(ctx)
	if len(mine)+len(tenant) == 0 {
		return nil
	}
	ids := make([]string, 0, len(mine)+len(tenant))
	for id := range mine {
		ids = append(ids, id)
	}
	for id := range tenant {
		if !mine[id] {
			ids = append(ids, id)
		}
	}
	rows, err := s.repo.GetKnowledgeBaseByIDs(ctx, ids)
	if err != nil {
		logger.Warnf(ctx, "Failed to load subscribed knowledge bases: %v", err)
		return nil
	}
	out := make([]*types.KnowledgeBase, 0, len(rows))
	for _, kb := range rows {
		if !kb.IsTenantPublished() {
			continue
		}
		kb.SubscribedByMe = mine[kb.ID]
		kb.SubscribedByTenant = tenant[kb.ID]
		out = append(out, kb)
	}
	return out
}

// ListPublishedCatalog returns one page of tenant-published KBs for human
// readers, marking which ones the caller or the caller's tenant subscribes.
func (s *knowledgeBaseService) ListPublishedCatalog(
	ctx context.Context, page, pageSize int, keyword string,
) ([]*types.KnowledgeBase, int64, error) {
	caller := types.CallerFromContext(ctx)
	if _, isKey := types.TenantAPIKeyScopeFromContext(ctx); isKey {
		return nil, 0, apperrors.NewForbiddenError("API keys cannot access the public catalog")
	}
	if !access.IsAuthenticatedHuman(ctx, caller) {
		return nil, 0, apperrors.NewUnauthorizedError("Unauthorized")
	}
	if s.subscriptionRepo == nil {
		return nil, 0, nil
	}
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = types.PublicCatalogDefaultPageSize
	}
	if pageSize > types.PublicCatalogMaxPageSize {
		pageSize = types.PublicCatalogMaxPageSize
	}
	items, total, err := s.subscriptionRepo.ListPublishedCatalog(ctx, keyword, pageSize, saturateCatalogOffset(page, pageSize))
	if err != nil {
		return nil, 0, err
	}
	mine, tenant := s.readerSubscriptions(ctx)
	owners := map[uint64]*types.Tenant{}
	if s.tenantRepo != nil && len(items) > 0 {
		ids := make([]uint64, 0, len(items))
		for _, kb := range items {
			if kb != nil {
				ids = append(ids, kb.OwnerTenantID)
			}
		}
		if got, terr := s.tenantRepo.GetTenantsByIDs(ctx, ids); terr == nil {
			owners = got
		}
	}
	for _, kb := range items {
		if kb == nil {
			continue
		}
		kb.EnsureDefaults()
		if owner := owners[kb.OwnerTenantID]; owner != nil {
			kb.OwnerTenantName = owner.Name
		}
		kb.SubscribedByMe = mine[kb.ID]
		kb.SubscribedByTenant = tenant[kb.ID]
		if cerr := s.FillKnowledgeBaseCounts(ctx, kb); cerr != nil {
			logger.Warnf(ctx, "Failed to fill KB counts for %s: %v", kb.ID, cerr)
		}
	}
	return items, total, nil
}
