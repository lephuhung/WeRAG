package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// usagePlanService implements interfaces.UsagePlanService.
type usagePlanService struct {
	repo     interfaces.UsagePlanRepository
	counters interfaces.UsageCounterRepository
	tenants  interfaces.TenantRepository
	users    interfaces.UserRepository
	members  interfaces.TenantMemberRepository
	guard    *usageGuard
	settings interfaces.SystemSettingService
	now      func() time.Time
}

// NewUsagePlanService creates the service.
func NewUsagePlanService(
	repo interfaces.UsagePlanRepository,
	counters interfaces.UsageCounterRepository,
	tenants interfaces.TenantRepository,
	users interfaces.UserRepository,
	members interfaces.TenantMemberRepository,
	guard interfaces.UsageGuard,
	settings interfaces.SystemSettingService,
) interfaces.UsagePlanService {
	g, _ := guard.(*usageGuard)
	if g == nil {
		g = &usageGuard{plans: repo, counters: counters, settings: settings, now: time.Now}
	}
	return &usagePlanService{
		repo: repo, counters: counters, tenants: tenants, users: users, members: members,
		guard: g, settings: settings, now: time.Now,
	}
}

func normalizePlan(plan *types.UsagePlan) error {
	plan.Code = strings.TrimSpace(plan.Code)
	plan.Name = strings.TrimSpace(plan.Name)
	if plan.Code == "" || plan.Name == "" {
		return apperrors.NewBadRequestError("plan code and name are required")
	}
	if plan.ScopeKind == "" {
		plan.ScopeKind = types.UsagePlanScopeTenant
	}
	if plan.ScopeKind != types.UsagePlanScopeTenant && plan.ScopeKind != types.UsagePlanScopeUser {
		return apperrors.NewBadRequestError("scope_kind must be tenant or user")
	}
	if plan.Mode == "" {
		plan.Mode = types.UsageModePool
	}
	if plan.Mode != types.UsageModePool && plan.Mode != types.UsageModeSeat {
		return apperrors.NewBadRequestError("mode must be pool or seat")
	}
	if plan.ScopeKind == types.UsagePlanScopeUser {
		plan.Mode = types.UsageModePool
	}
	if plan.Limits == nil {
		plan.Limits = types.UsageLimits{}
	}
	if err := plan.Limits.Validate(); err != nil {
		return apperrors.NewBadRequestError(err.Error())
	}
	if len(plan.Features) == 0 || string(plan.Features) == "null" {
		plan.Features = types.JSON("{}")
	}
	// Only the known keys are checked; anything else is stored untouched.
	if _, err := types.ParseUsagePlanFeatures(plan.Features); err != nil {
		return apperrors.NewBadRequestError(err.Error())
	}
	return nil
}

func (s *usagePlanService) ListPlans(ctx context.Context, includeInactive bool) ([]*types.UsagePlan, error) {
	return s.repo.ListPlans(ctx, includeInactive)
}

func (s *usagePlanService) GetPlan(ctx context.Context, id string) (*types.UsagePlan, error) {
	plan, err := s.repo.GetPlan(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperrors.NewNotFoundError("plan not found")
	}
	return plan, err
}

func (s *usagePlanService) CreatePlan(ctx context.Context, plan *types.UsagePlan) (*types.UsagePlan, error) {
	if err := normalizePlan(plan); err != nil {
		return nil, err
	}
	plan.ID = uuid.New().String()
	plan.CreatedAt = s.now()
	plan.UpdatedAt = plan.CreatedAt
	if err := s.repo.CreatePlan(ctx, plan); err != nil {
		if isUniqueViolation(err) {
			return nil, apperrors.NewConflictError("a plan with this code already exists")
		}
		return nil, err
	}
	return plan, nil
}

func (s *usagePlanService) UpdatePlan(ctx context.Context, plan *types.UsagePlan) (*types.UsagePlan, error) {
	existing, err := s.GetPlan(ctx, plan.ID)
	if err != nil {
		return nil, err
	}
	if err := normalizePlan(plan); err != nil {
		return nil, err
	}
	plan.CreatedAt = existing.CreatedAt
	if err := s.repo.UpdatePlan(ctx, plan); err != nil {
		if isUniqueViolation(err) {
			return nil, apperrors.NewConflictError("a plan with this code already exists")
		}
		return nil, err
	}
	return plan, nil
}

func (s *usagePlanService) DeletePlan(ctx context.Context, id string) error {
	if _, err := s.GetPlan(ctx, id); err != nil {
		return err
	}
	n, err := s.repo.CountSubscriptionsByPlan(ctx, id)
	if err != nil {
		return err
	}
	if n > 0 {
		return apperrors.NewConflictError(
			fmt.Sprintf("%d subscription(s) use this plan; deactivate it instead", n))
	}
	return s.repo.DeletePlan(ctx, id)
}

func (s *usagePlanService) normalizeSubscription(ctx context.Context, sub *types.UsageSubscription) error {
	sub.ScopeType = strings.TrimSpace(sub.ScopeType)
	sub.ScopeID = strings.TrimSpace(sub.ScopeID)
	if sub.ScopeType != types.UsageScopeTenant && sub.ScopeType != types.UsageScopeUser {
		return apperrors.NewBadRequestError("scope_type must be tenant or user")
	}
	if sub.ScopeID == "" {
		return apperrors.NewBadRequestError("scope_id is required")
	}
	plan, err := s.GetPlan(ctx, sub.PlanID)
	if err != nil {
		return err
	}
	if plan.ScopeKind != sub.ScopeType {
		return apperrors.NewBadRequestError(fmt.Sprintf("plan %s is a %s plan", plan.Code, plan.ScopeKind))
	}
	if sub.Mode == "" {
		sub.Mode = plan.Mode
	}
	if sub.ScopeType == types.UsageScopeUser {
		sub.Mode = types.UsageModePool
	}
	if sub.Mode != types.UsageModePool && sub.Mode != types.UsageModeSeat {
		return apperrors.NewBadRequestError("mode must be pool or seat")
	}
	if sub.Mode == types.UsageModeSeat && sub.Seats <= 0 {
		return apperrors.NewBadRequestError("seat mode needs seats > 0")
	}
	if sub.Mode == types.UsageModePool {
		sub.Seats = 0
	}
	if sub.LimitsOverride != nil {
		if err := sub.LimitsOverride.Validate(); err != nil {
			return apperrors.NewBadRequestError(err.Error())
		}
	}
	if sub.Status == "" {
		sub.Status = types.UsageSubscriptionActive
	}
	if sub.Status != types.UsageSubscriptionActive && sub.Status != types.UsageSubscriptionCancelled {
		return apperrors.NewBadRequestError("status must be active or cancelled")
	}
	if sub.StartsAt.IsZero() {
		sub.StartsAt = s.now()
	}
	if sub.EndsAt != nil && !sub.EndsAt.After(sub.StartsAt) {
		return apperrors.NewBadRequestError("ends_at must be after starts_at")
	}
	sub.Plan = plan
	return nil
}

func (s *usagePlanService) ListSubscriptions(
	ctx context.Context, filter types.UsageSubscriptionFilter,
) ([]*types.UsageSubscriptionView, error) {
	subs, err := s.repo.ListSubscriptions(ctx, filter)
	if err != nil {
		return nil, err
	}
	views := make([]*types.UsageSubscriptionView, 0, len(subs))
	for _, sub := range subs {
		v, err := s.view(ctx, sub)
		if err != nil {
			return nil, err
		}
		views = append(views, v)
	}
	return views, nil
}

func (s *usagePlanService) GetSubscription(ctx context.Context, id string) (*types.UsageSubscriptionView, error) {
	sub, err := s.repo.GetSubscription(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperrors.NewNotFoundError("subscription not found")
	}
	if err != nil {
		return nil, err
	}
	return s.view(ctx, sub)
}

// view decorates a subscription with the scope name and the consumption
// of the counters it governs (pool: the workspace; personal: the person;
// seat mode: the workspace totals, as seats are per person).
func (s *usagePlanService) view(ctx context.Context, sub *types.UsageSubscription) (*types.UsageSubscriptionView, error) {
	now := s.now()
	v := &types.UsageSubscriptionView{
		UsageSubscription: sub,
		ScopeName:         s.scopeName(ctx, sub.ScopeType, sub.ScopeID),
		Current:           sub.IsCurrentAt(now),
		Limits:            []types.UsageLimitStatus{},
	}
	statuses, err := s.guard.limitStatus(ctx, sub.EffectiveLimits(), sub.ScopeType, sub.ScopeID, now)
	if err != nil {
		return nil, err
	}
	v.Limits = statuses
	if sub.Mode == types.UsageModeSeat {
		n, err := s.repo.CountSeats(ctx, sub.ID)
		if err != nil {
			return nil, err
		}
		v.SeatsUsed = n
	}
	return v, nil
}

func (s *usagePlanService) scopeName(ctx context.Context, scopeType, scopeID string) string {
	switch scopeType {
	case types.UsageScopeTenant:
		var id uint64
		if _, err := fmt.Sscanf(scopeID, "%d", &id); err == nil && s.tenants != nil {
			if t, err := s.tenants.GetTenantByID(ctx, id); err == nil && t != nil {
				return t.Name
			}
		}
	case types.UsageScopeUser:
		if s.users != nil {
			if u, err := s.users.GetUserByID(ctx, scopeID); err == nil && u != nil {
				if u.Username != "" {
					return u.Username
				}
				return u.Email
			}
		}
	}
	return scopeID
}

func (s *usagePlanService) CreateSubscription(
	ctx context.Context, sub *types.UsageSubscription,
) (*types.UsageSubscriptionView, error) {
	if err := s.normalizeSubscription(ctx, sub); err != nil {
		return nil, err
	}
	if sub.Status == types.UsageSubscriptionActive {
		// Changing plan: the previous active subscription of the scope is
		// closed, its seats go with it.
		prev, err := s.repo.GetActiveSubscription(ctx, sub.ScopeType, sub.ScopeID)
		if err != nil {
			return nil, err
		}
		if prev != nil {
			prev.Status = types.UsageSubscriptionCancelled
			if err := s.repo.UpdateSubscription(ctx, prev); err != nil {
				return nil, err
			}
		}
	}
	sub.ID = uuid.New().String()
	sub.CreatedAt = s.now()
	sub.UpdatedAt = sub.CreatedAt
	if err := s.repo.CreateSubscription(ctx, sub); err != nil {
		return nil, err
	}
	return s.view(ctx, sub)
}

func (s *usagePlanService) UpdateSubscription(
	ctx context.Context, sub *types.UsageSubscription,
) (*types.UsageSubscriptionView, error) {
	existing, err := s.repo.GetSubscription(ctx, sub.ID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperrors.NewNotFoundError("subscription not found")
	}
	if err != nil {
		return nil, err
	}
	// The scope never changes on update; assign a new subscription instead.
	sub.ScopeType, sub.ScopeID = existing.ScopeType, existing.ScopeID
	sub.CreatedAt = existing.CreatedAt
	if err := s.normalizeSubscription(ctx, sub); err != nil {
		return nil, err
	}
	if sub.Status == types.UsageSubscriptionActive && existing.Status != types.UsageSubscriptionActive {
		other, err := s.repo.GetActiveSubscription(ctx, sub.ScopeType, sub.ScopeID)
		if err != nil {
			return nil, err
		}
		if other != nil && other.ID != sub.ID {
			return nil, apperrors.NewConflictError("the scope already has an active subscription")
		}
	}
	if err := s.repo.UpdateSubscription(ctx, sub); err != nil {
		return nil, err
	}
	return s.view(ctx, sub)
}

func (s *usagePlanService) DeleteSubscription(ctx context.Context, id string) error {
	if _, err := s.repo.GetSubscription(ctx, id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return apperrors.NewNotFoundError("subscription not found")
		}
		return err
	}
	return s.repo.DeleteSubscription(ctx, id)
}

func (s *usagePlanService) TenantOverview(ctx context.Context) ([]*types.UsageTenantOverview, error) {
	tenants, err := s.tenants.ListTenants(ctx)
	if err != nil {
		return nil, err
	}
	keys := types.UsagePeriodKeysAt(s.now())
	out := make([]*types.UsageTenantOverview, 0, len(tenants))
	for _, t := range tenants {
		if t.Kind == types.TenantKindConfigProfile {
			continue
		}
		scopeID := types.UsageTenantScopeID(t.ID)
		row := &types.UsageTenantOverview{
			TenantID:   t.ID,
			TenantName: t.Name,
			Today:      types.UsagePeriodTotals{PeriodKey: keys.Day},
			Month:      types.UsagePeriodTotals{PeriodKey: keys.Month},
		}
		counters, err := s.counters.Get(ctx, types.UsageScopeTenant, scopeID, []string{keys.Day, keys.Month})
		if err != nil {
			return nil, err
		}
		for _, c := range counters {
			switch c.PeriodKey {
			case keys.Day:
				row.Today.AddMetric(c.Metric, c.Value)
			case keys.Month:
				row.Month.AddMetric(c.Metric, c.Value)
			}
		}
		sub, err := s.repo.GetActiveSubscription(ctx, types.UsageScopeTenant, scopeID)
		if err != nil {
			return nil, err
		}
		if sub != nil {
			v, err := s.view(ctx, sub)
			if err != nil {
				return nil, err
			}
			v.ScopeName = t.Name
			row.Subscription = v
		}
		out = append(out, row)
	}
	return out, nil
}

func (s *usagePlanService) ListSeats(ctx context.Context, subscriptionID string) ([]*types.UsageSeat, error) {
	if _, err := s.GetSubscription(ctx, subscriptionID); err != nil {
		return nil, err
	}
	return s.repo.ListSeats(ctx, subscriptionID)
}

func (s *usagePlanService) AssignSeat(ctx context.Context, subscriptionID, userID, assignedBy string) error {
	sub, err := s.repo.GetSubscription(ctx, subscriptionID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return apperrors.NewNotFoundError("subscription not found")
	}
	if err != nil {
		return err
	}
	if sub.Mode != types.UsageModeSeat {
		return apperrors.NewBadRequestError("subscription is not in seat mode")
	}
	has, err := s.repo.HasSeat(ctx, subscriptionID, userID)
	if err != nil {
		return err
	}
	if has {
		return nil
	}
	n, err := s.repo.CountSeats(ctx, subscriptionID)
	if err != nil {
		return err
	}
	if n >= int64(sub.Seats) {
		return apperrors.NewConflictError(fmt.Sprintf("all %d seats are assigned", sub.Seats))
	}
	return s.repo.AddSeat(ctx, &types.UsageSeat{
		SubscriptionID: subscriptionID, UserID: userID, AssignedBy: assignedBy, AssignedAt: s.now(),
	})
}

func (s *usagePlanService) RevokeSeat(ctx context.Context, subscriptionID, userID string) error {
	return s.repo.RemoveSeat(ctx, subscriptionID, userID)
}

func (s *usagePlanService) TenantUsage(ctx context.Context, tenantID uint64) (*types.UsageTenantUsage, error) {
	out := &types.UsageTenantUsage{Enforced: s.guard.Enforced(ctx), Seats: []*types.UsageSeat{},
		Addons: []*types.UsageSubscriptionAddon{}}
	sub, err := s.repo.GetActiveSubscription(ctx, types.UsageScopeTenant, types.UsageTenantScopeID(tenantID))
	if err != nil {
		return nil, err
	}
	if sub == nil {
		return out, nil
	}
	v, err := s.view(ctx, sub)
	if err != nil {
		return nil, err
	}
	out.Subscription = v
	addons, err := s.repo.ListSubscriptionAddons(ctx, sub.ID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	for _, a := range addons {
		if a.IsCurrentAt(now) {
			if a.EndsAt == nil {
				a.EndsAt = sub.EndsAt // shown as "ends with the subscription"
			}
			out.Addons = append(out.Addons, a)
		}
	}
	if sub.Mode == types.UsageModeSeat {
		seats, err := s.repo.ListSeats(ctx, sub.ID)
		if err != nil {
			return nil, err
		}
		out.Seats = seats
	}
	return out, nil
}

// tenantSeatSubscription returns the workspace's current seat-mode
// subscription or the error the Tenant Admin should see.
func (s *usagePlanService) tenantSeatSubscription(ctx context.Context, tenantID uint64) (*types.UsageSubscription, error) {
	sub, err := s.repo.GetActiveSubscription(ctx, types.UsageScopeTenant, types.UsageTenantScopeID(tenantID))
	if err != nil {
		return nil, err
	}
	if sub == nil {
		return nil, apperrors.NewNotFoundError("the organization has no usage package")
	}
	if sub.Mode != types.UsageModeSeat {
		return nil, apperrors.NewBadRequestError("the organization package is not in seat mode")
	}
	return sub, nil
}

func (s *usagePlanService) AssignTenantSeat(ctx context.Context, tenantID uint64, userID, assignedBy string) error {
	sub, err := s.tenantSeatSubscription(ctx, tenantID)
	if err != nil {
		return err
	}
	if s.members != nil {
		m, err := s.members.Get(ctx, userID, tenantID)
		if err != nil || m == nil || m.Status != types.TenantMemberStatusActive {
			return apperrors.NewBadRequestError("the account is not an active member of the organization")
		}
	}
	return s.AssignSeat(ctx, sub.ID, userID, assignedBy)
}

func (s *usagePlanService) RevokeTenantSeat(ctx context.Context, tenantID uint64, userID string) error {
	sub, err := s.tenantSeatSubscription(ctx, tenantID)
	if err != nil {
		return err
	}
	return s.repo.RemoveSeat(ctx, sub.ID, userID)
}

func (s *usagePlanService) ListSubscriptionAddons(
	ctx context.Context, subscriptionID string,
) ([]*types.UsageSubscriptionAddon, error) {
	if _, err := s.repo.GetSubscription(ctx, subscriptionID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperrors.NewNotFoundError("subscription not found")
		}
		return nil, err
	}
	return s.repo.ListSubscriptionAddons(ctx, subscriptionID)
}

// GrantSubscriptionAddon gives a subscription the right to use one
// specialist agent, or extends an existing grant (never shortens it; price and grantor are replaced).
// Only an "addon" agent can be granted: a basic one is already everyone's
// and an internal one follows its "Tổ chức áp dụng" list.
func (s *usagePlanService) GrantSubscriptionAddon(
	ctx context.Context, subscriptionID string, grant types.UsageSubscriptionAddonGrant, grantedBy string,
) (*types.UsageSubscriptionAddon, error) {
	if _, err := s.repo.GetSubscription(ctx, subscriptionID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperrors.NewNotFoundError("subscription not found")
		}
		return nil, err
	}
	agentID := strings.TrimSpace(grant.AgentID)
	if agentID == "" {
		return nil, apperrors.NewBadRequestError("agent_id is required")
	}
	distribution, found, err := s.repo.CustomAgentDistribution(ctx, agentID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, apperrors.NewNotFoundError("agent not found")
	}
	if distribution != types.AgentDistributionAddon {
		return nil, apperrors.NewBadRequestError(
			"chỉ cấp quyền dùng cho trợ lý có cách phân phối Chuyên ngành (addon)")
	}
	if grant.PriceVND < 0 {
		return nil, apperrors.NewBadRequestError("price_vnd must be >= 0")
	}
	addon := &types.UsageSubscriptionAddon{
		SubscriptionID: subscriptionID,
		AgentID:        agentID,
		StartsAt:       s.now(),
		EndsAt:         grant.EndsAt,
		PriceVND:       grant.PriceVND,
		GrantedBy:      grantedBy,
	}
	if grant.StartsAt != nil && !grant.StartsAt.IsZero() {
		addon.StartsAt = *grant.StartsAt
	}
	if addon.EndsAt != nil && !addon.EndsAt.After(addon.StartsAt) {
		return nil, apperrors.NewBadRequestError("ends_at must be after starts_at")
	}
	if err := s.repo.UpsertSubscriptionAddon(ctx, addon); err != nil {
		return nil, err
	}
	return addon, nil
}

// RevokeSubscriptionAddon takes an add-on back: the agent disappears from
// the subscription holders' lists and their next turn with it gets 2104.
func (s *usagePlanService) RevokeSubscriptionAddon(ctx context.Context, subscriptionID, agentID string) error {
	deleted, err := s.repo.DeleteSubscriptionAddon(ctx, subscriptionID, strings.TrimSpace(agentID))
	if err != nil {
		return err
	}
	if !deleted {
		return apperrors.NewNotFoundError("add-on not found")
	}
	return nil
}

// includedPlanAgents names the basic agents every plan includes as users
// see them: the configuration profile's stored row when there is one (a
// SuperAdmin may have renamed it), localised like the agent picker, and
// leaving out one that is switched off. A read failure falls back to
// builtin_agents.yaml.
func includedPlanAgents(ctx context.Context, repo interfaces.UsagePlanRepository) []types.UsagePlanAgent {
	stored := map[string]*types.CustomAgent{}
	if repo != nil {
		rows, err := repo.ProfileBuiltinAgents(ctx, types.UsageIncludedAgentIDs)
		if err != nil {
			logger.Warnf(ctx, "usage: included agents read from the profile failed: %v", err)
		}
		for _, r := range rows {
			if _, ok := stored[r.ID]; !ok {
				stored[r.ID] = r
			}
		}
	}
	out := make([]types.UsagePlanAgent, 0, len(types.UsageIncludedAgentIDs))
	for _, id := range types.UsageIncludedAgentIDs {
		a := stored[id]
		if a != nil {
			types.ApplyBuiltinAgentLocalization(ctx, a)
		} else {
			a = types.GetBuiltinAgentWithContext(ctx, id, 0)
		}
		if a == nil || !a.EffectiveEnabled() {
			continue
		}
		out = append(out, types.UsagePlanAgent{ID: id, Name: a.Name})
	}
	return out
}

func (s *usagePlanService) ListAgentAddons(ctx context.Context, includeInactive bool) (*types.UsageAgentAddonCatalogue, error) {
	rows, err := s.repo.ListAgentAddons(ctx, includeInactive)
	if err != nil {
		return nil, err
	}
	return &types.UsageAgentAddonCatalogue{Addons: rows, IncludedAgents: includedPlanAgents(ctx, s.repo)}, nil
}

// validateAgentAddon normalizes a catalogue row and checks it: an addon
// agent, a price >= 0, plans that exist.
func (s *usagePlanService) validateAgentAddon(ctx context.Context, row *types.UsageAgentAddon) error {
	row.AgentID = strings.TrimSpace(row.AgentID)
	row.Description = strings.TrimSpace(row.Description)
	row.PlanIDs = row.PlanIDs.Normalized()
	if row.AgentID == "" {
		return apperrors.NewBadRequestError("agent_id is required")
	}
	if row.PriceVND < 0 {
		return apperrors.NewBadRequestError("price_vnd must be >= 0")
	}
	distribution, found, err := s.repo.CustomAgentDistribution(ctx, row.AgentID)
	if err != nil {
		return err
	}
	if !found {
		return apperrors.NewNotFoundError("agent not found")
	}
	if distribution != types.AgentDistributionAddon {
		return apperrors.NewBadRequestError(
			"chỉ trợ lý có cách phân phối Chuyên ngành (addon) mới bán kèm gói dịch vụ được")
	}
	for _, id := range row.PlanIDs {
		if _, err := s.repo.GetPlan(ctx, id); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return apperrors.NewBadRequestError(fmt.Sprintf("plan %q does not exist", id))
			}
			return err
		}
	}
	return nil
}

func (s *usagePlanService) CreateAgentAddon(ctx context.Context, row *types.UsageAgentAddon) (*types.UsageAgentAddon, error) {
	if err := s.validateAgentAddon(ctx, row); err != nil {
		return nil, err
	}
	if _, err := s.repo.GetAgentAddon(ctx, row.AgentID); err == nil {
		return nil, apperrors.NewConflictError("this agent is already in the add-on catalogue")
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if err := s.repo.CreateAgentAddon(ctx, row); err != nil {
		return nil, err
	}
	return s.repo.GetAgentAddon(ctx, row.AgentID)
}

func (s *usagePlanService) UpdateAgentAddon(ctx context.Context, row *types.UsageAgentAddon) (*types.UsageAgentAddon, error) {
	if _, err := s.repo.GetAgentAddon(ctx, strings.TrimSpace(row.AgentID)); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperrors.NewNotFoundError("add-on not found")
		}
		return nil, err
	}
	if err := s.validateAgentAddon(ctx, row); err != nil {
		return nil, err
	}
	if err := s.repo.UpdateAgentAddon(ctx, row); err != nil {
		return nil, err
	}
	return s.repo.GetAgentAddon(ctx, row.AgentID)
}

// DeleteAgentAddon takes an agent out of the catalogue. Subscriptions that
// already hold it keep their rights until they end (usage_subscription_addons
// is untouched).
func (s *usagePlanService) DeleteAgentAddon(ctx context.Context, agentID string) error {
	deleted, err := s.repo.DeleteAgentAddon(ctx, strings.TrimSpace(agentID))
	if err != nil {
		return err
	}
	if !deleted {
		return apperrors.NewNotFoundError("add-on not found")
	}
	return nil
}
