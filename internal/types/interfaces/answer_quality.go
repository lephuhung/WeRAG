package interfaces

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// AnswerQualityTarget is the message a review is written on.
type AnswerQualityTarget struct {
	MessageID string
	TenantID  uint64 // organization of the message's session
	Role      string
	Feedback  int
	// Ungrounded: the message carries an ungrounded marker and no failure.
	Ungrounded bool
}

// AnswerQualityRepository reads the answer-quality lists (types.AnswerQualityRow)
// and stores the admins' reviews (migration 000197). Every read is scoped by
// the filter's TenantID unless AllTenants is set; the caller (the service)
// decides that from who asks.
type AnswerQualityRepository interface {
	// ListDisliked pages the answers voted down, newest vote first.
	ListDisliked(ctx context.Context, f types.AnswerQualityFilter) ([]*types.AnswerQualityRow, int64, error)
	// ListUngrounded pages the turns a gate answered without evidence
	// (failed turns excluded), newest first.
	ListUngrounded(ctx context.Context, f types.AnswerQualityFilter) ([]*types.AnswerQualityRow, int64, error)
	// Agents lists the agents of a list's rows under f, AgentID ignored.
	Agents(ctx context.Context, kind string, f types.AnswerQualityFilter) ([]*types.AnswerQualityAgent, error)
	// UngroundedMarkers returns the newest ungrounded rows under f (paging
	// ignored), at most limit, with only MessageID, CreatedAt, TenantID,
	// Question (the marker's), Reason and LegalRequest filled.
	UngroundedMarkers(ctx context.Context, f types.AnswerQualityFilter, limit int) ([]*types.AnswerQualityRow, error)
	// Target loads the message a review is written on; nil when it does not
	// exist (or its session was deleted).
	Target(ctx context.Context, messageID string) (*AnswerQualityTarget, error)
	// GetReview returns the review of (messageID, kind), nil when none.
	GetReview(ctx context.Context, messageID, kind string) (*types.AnswerQualityReview, error)
	// SaveReview inserts or replaces the review of (MessageID, Kind).
	SaveReview(ctx context.Context, r *types.AnswerQualityReview) error
}

// AnswerQualityService serves the answer-quality inbox. platform=true is the
// SuperAdmin console (every organization, tenantFilter narrowing to one);
// platform=false is the Tenant Admin's own organization (the caller's
// tenant, tenantFilter ignored). Members, guests and API keys are refused.
type AnswerQualityService interface {
	ListDisliked(ctx context.Context, platform bool, f types.AnswerQualityFilter) (*types.AnswerQualityPage, error)
	ListUngrounded(ctx context.Context, platform bool, f types.AnswerQualityFilter) (*types.AnswerQualityPage, error)
	// GroupUngrounded groups the ungrounded questions of the period by
	// normalized text, most frequent first.
	GroupUngrounded(ctx context.Context, platform bool, f types.AnswerQualityFilter) (*types.UngroundedGroupPage, error)
	// Review sets the cause tag / note / reviewed mark of one row.
	Review(ctx context.Context, platform bool, messageID string, in types.AnswerQualityReviewInput) (*types.AnswerQualityReview, error)
}
