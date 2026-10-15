package interfaces

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

// UsageCounterRepository persists the usage_counters table.
type UsageCounterRepository interface {
	// Add applies every increment atomically (insert-or-add). Increments
	// with the same key are merged before the write.
	Add(ctx context.Context, increments []types.UsageIncrement) error
	// Get returns the rows of one scope for the given period keys.
	Get(ctx context.Context, scopeType, scopeID string, periodKeys []string) ([]*types.UsageCounter, error)
	// SumByPeriod sums every scope of scopeType for one period, per metric.
	SumByPeriod(ctx context.Context, scopeType, periodKey string) ([]*types.UsageCounter, error)
	// DeleteScope removes every counter row of one scope. Only lifetime
	// scopes are reset this way (a session whose messages were cleared);
	// workspace and person rows are history and never deleted.
	DeleteScope(ctx context.Context, scopeType, scopeID string) error
	// ClaimMessageUsage marks messageID's token usage as counted and reports
	// whether this call was the one that claimed it. A message saved several
	// times is thereby counted once.
	ClaimMessageUsage(ctx context.Context, messageID string) (bool, error)
}

// UsageMeter records usage as it happens and answers usage queries.
// Recording is best-effort: a metering failure is logged and never fails
// the chat turn that triggered it.
type UsageMeter interface {
	// RecordQuestion counts one question for the workspace, for the person
	// when userID is a real one, and for the session when sessionID is set.
	RecordQuestion(ctx context.Context, tenantID uint64, userID, sessionID string)
	// RecordMessageUsage folds an assistant message's token usage into the
	// counters, once per message. Nil usage or non-assistant rows are ignored.
	RecordMessageUsage(ctx context.Context, msg *types.Message)
	// ResetSession zeroes a session's lifetime counters, after its messages
	// were cleared: the per-session caps then count from the new start.
	ResetSession(ctx context.Context, sessionID string)
	// Summary returns today's and this month's counters for the person and
	// the workspace.
	Summary(ctx context.Context, tenantID uint64, userID string) (*types.UsageSummary, error)
	// PlatformTotals sums every workspace for today and this month.
	PlatformTotals(ctx context.Context) (*types.UsageStats, error)
}

// UsagePlanRepository persists usage_plans, usage_subscriptions and
// usage_seats.
type UsagePlanRepository interface {
	ListPlans(ctx context.Context, includeInactive bool) ([]*types.UsagePlan, error)
	GetPlan(ctx context.Context, id string) (*types.UsagePlan, error)
	CreatePlan(ctx context.Context, plan *types.UsagePlan) error
	UpdatePlan(ctx context.Context, plan *types.UsagePlan) error
	DeletePlan(ctx context.Context, id string) error
	CountSubscriptionsByPlan(ctx context.Context, planID string) (int64, error)

	ListSubscriptions(ctx context.Context, filter types.UsageSubscriptionFilter) ([]*types.UsageSubscription, error)
	GetSubscription(ctx context.Context, id string) (*types.UsageSubscription, error)
	// GetActiveSubscription returns the active subscription of one scope
	// with its plan loaded, or nil when there is none.
	GetActiveSubscription(ctx context.Context, scopeType, scopeID string) (*types.UsageSubscription, error)
	CreateSubscription(ctx context.Context, sub *types.UsageSubscription) error
	UpdateSubscription(ctx context.Context, sub *types.UsageSubscription) error
	DeleteSubscription(ctx context.Context, id string) error

	ListSeats(ctx context.Context, subscriptionID string) ([]*types.UsageSeat, error)
	CountSeats(ctx context.Context, subscriptionID string) (int64, error)
	HasSeat(ctx context.Context, subscriptionID, userID string) (bool, error)
	AddSeat(ctx context.Context, seat *types.UsageSeat) error
	RemoveSeat(ctx context.Context, subscriptionID, userID string) error
}

// UsageGuard decides whether a chat turn may start.
type UsageGuard interface {
	// CheckTurn returns the grant a turn runs under (the plan's per-turn
	// caps), or a 429 *errors.AppError whose details are a
	// types.UsageDenial. While usage.enforce is off, and for SystemAdmins,
	// it grants without caps. A storage failure is logged and lets the turn
	// through uncapped: a broken meter must not take chat down.
	//
	// sessionID ("" = none) is checked against the plan's per-session caps:
	// with SessionCapHard a used-up session is refused with reason
	// session_limit; with SessionCapSoft the turn goes through and the
	// grant's Session says the cap is reached. Either way the grant's
	// Session carries the 80% warning.
	CheckTurn(
		ctx context.Context, tenantID uint64, userID, sessionID string, capMode types.SessionCapMode,
	) (*types.UsageTurnGrant, error)
	// Entitlement resolves the subscription governing the caller with its
	// current consumption; nil when there is none.
	Entitlement(ctx context.Context, tenantID uint64, userID string) (*types.UsageEntitlement, error)
	// Enforced reports the usage.enforce setting.
	Enforced(ctx context.Context) bool
	// PreviewSession resolves the plan CheckTurn would apply to a turn of
	// sessionID, and the session's lifetime counters, without counting,
	// refusing or creating anything (read-only; for display). capMode only
	// sets the preview's Session.Soft.
	PreviewSession(
		ctx context.Context, tenantID uint64, userID, sessionID string, capMode types.SessionCapMode,
	) (*types.UsageSessionPreview, error)
}

// UsagePlanService is the SuperAdmin management surface for packages.
type UsagePlanService interface {
	ListPlans(ctx context.Context, includeInactive bool) ([]*types.UsagePlan, error)
	GetPlan(ctx context.Context, id string) (*types.UsagePlan, error)
	CreatePlan(ctx context.Context, plan *types.UsagePlan) (*types.UsagePlan, error)
	UpdatePlan(ctx context.Context, plan *types.UsagePlan) (*types.UsagePlan, error)
	// DeletePlan removes a plan nobody subscribes to; otherwise it fails
	// and the caller should deactivate the plan instead.
	DeletePlan(ctx context.Context, id string) error

	ListSubscriptions(ctx context.Context, filter types.UsageSubscriptionFilter) ([]*types.UsageSubscriptionView, error)
	GetSubscription(ctx context.Context, id string) (*types.UsageSubscriptionView, error)
	// CreateSubscription attaches a plan to a scope. An existing active
	// subscription of that scope is cancelled first (plan change).
	CreateSubscription(ctx context.Context, sub *types.UsageSubscription) (*types.UsageSubscriptionView, error)
	UpdateSubscription(ctx context.Context, sub *types.UsageSubscription) (*types.UsageSubscriptionView, error)
	DeleteSubscription(ctx context.Context, id string) error
	// TenantOverview lists every workspace with its subscription and usage.
	TenantOverview(ctx context.Context) ([]*types.UsageTenantOverview, error)

	ListSeats(ctx context.Context, subscriptionID string) ([]*types.UsageSeat, error)
	AssignSeat(ctx context.Context, subscriptionID, userID, assignedBy string) error
	RevokeSeat(ctx context.Context, subscriptionID, userID string) error

	// TenantUsage is the workspace-facing view: its active subscription
	// with consumption and its seats. Enforced mirrors usage.enforce.
	TenantUsage(ctx context.Context, tenantID uint64) (*types.UsageTenantUsage, error)
	// AssignTenantSeat gives an active member of the workspace a seat of
	// the workspace's seat-mode subscription (Tenant Admin action).
	AssignTenantSeat(ctx context.Context, tenantID uint64, userID, assignedBy string) error
	// RevokeTenantSeat takes a seat of the workspace's subscription back.
	RevokeTenantSeat(ctx context.Context, tenantID uint64, userID string) error
}

// UsageBillingRepository stores promo codes and orders.
type UsageBillingRepository interface {
	ListPromos(ctx context.Context, includeInactive bool) ([]*types.UsagePromoCode, error)
	GetPromo(ctx context.Context, id string) (*types.UsagePromoCode, error)
	// GetPromoByCode returns nil, nil when no such code exists.
	GetPromoByCode(ctx context.Context, code string) (*types.UsagePromoCode, error)
	CreatePromo(ctx context.Context, promo *types.UsagePromoCode) error
	UpdatePromo(ctx context.Context, promo *types.UsagePromoCode) error
	DeletePromo(ctx context.Context, id string) error
	// AddPromoUse changes used_count by delta (+1 on order, -1 on cancel).
	AddPromoUse(ctx context.Context, id string, delta int) error

	CreateOrder(ctx context.Context, order *types.UsageOrder) error
	GetOrder(ctx context.Context, id string) (*types.UsageOrder, error)
	GetOrderByCode(ctx context.Context, code string) (*types.UsageOrder, error)
	ListOrders(ctx context.Context, filter types.UsageOrderFilter) ([]*types.UsageOrder, error)
	UpdateOrder(ctx context.Context, order *types.UsageOrder) error
	// ExpirePendingOrders flips pending orders past their expires_at and
	// returns them so promo uses can be released.
	ExpirePendingOrders(ctx context.Context, now time.Time) ([]*types.UsageOrder, error)
}

// UsageBillingService quotes, takes and settles orders for usage packages.
type UsageBillingService interface {
	BillingInfo(ctx context.Context) *types.UsageBillingInfo
	// PurchasablePlans lists active plans of a scope kind that carry a price.
	PurchasablePlans(ctx context.Context, scopeKind string) ([]*types.UsagePlan, error)
	Quote(ctx context.Context, req types.UsageQuoteRequest) (*types.UsageQuote, error)
	// CreateOrder freezes a quote for scopeID. A promo that cannot be used
	// is an error here (the quote already told the buyer). Total 0 orders
	// are confirmed on the spot.
	CreateOrder(ctx context.Context, req types.UsageQuoteRequest, scopeID, createdBy string) (*types.UsageOrderView, error)
	GetOrder(ctx context.Context, id string) (*types.UsageOrderView, error)
	GetOrderByCode(ctx context.Context, code string) (*types.UsageOrderView, error)
	ListOrders(ctx context.Context, filter types.UsageOrderFilter) ([]*types.UsageOrderView, error)
	// CancelOrder voids a pending order and releases its promo use.
	CancelOrder(ctx context.Context, id string) (*types.UsageOrderView, error)
	// ConfirmOrder marks a pending order paid and applies it: the scope's
	// current subscription of the same plan is extended by the order's
	// months (seats raised to the order's), otherwise a new subscription
	// replaces whatever the scope had.
	ConfirmOrder(ctx context.Context, id, confirmedBy string) (*types.UsageOrderView, error)

	ListPromos(ctx context.Context, includeInactive bool) ([]*types.UsagePromoCode, error)
	CreatePromo(ctx context.Context, promo *types.UsagePromoCode) (*types.UsagePromoCode, error)
	UpdatePromo(ctx context.Context, promo *types.UsagePromoCode) (*types.UsagePromoCode, error)
	DeletePromo(ctx context.Context, id string) error
}
