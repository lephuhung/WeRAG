package interfaces

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// KBSubscriptionRepository persists published-KB subscriptions and lists
// the published-KB catalog.
type KBSubscriptionRepository interface {
	// ListPublishedCatalog lists one page of tenant-published KBs
	// (visibility published, nonzero owner, not temporary), newest first,
	// with the total ignoring limit/offset.
	ListPublishedCatalog(ctx context.Context, keyword string, limit, offset int) ([]*types.KnowledgeBase, int64, error)
	// Subscribe inserts the subscription; an existing identical row is kept.
	Subscribe(ctx context.Context, sub *types.KBSubscription) error
	// Unsubscribe removes the (kb, tenant, user) row; missing rows are fine.
	Unsubscribe(ctx context.Context, kbID string, tenantID uint64, userID string) error
	// ListForReader returns the subscriptions that apply to userID in
	// tenantID: the tenant-wide rows plus the user's own.
	ListForReader(ctx context.Context, tenantID uint64, userID string) ([]*types.KBSubscription, error)
	// DeleteByKnowledgeBaseID drops every subscription of a KB.
	DeleteByKnowledgeBaseID(ctx context.Context, kbID string) error
}
