package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
)

// Specialist agents bought with a plan or on a running subscription
// (docs/agent-addon-plans.md §5, decisions 7.2/7.3/7.5).

// addonUnits is what an add-on price is multiplied by: the seats of a seat
// plan (decision 7.2), else 1.
func addonUnits(mode string, seats int) int {
	if mode == types.UsageModeSeat && seats > 0 {
		return seats
	}
	return 1
}

// sellableAddons maps the requested agent ids to their catalogue rows,
// refusing any that is not on sale with planID. Duplicates are dropped.
func (s *usageBillingService) sellableAddons(
	ctx context.Context, planID string, agentIDs []string,
) ([]*types.UsageAgentAddon, error) {
	if len(agentIDs) == 0 {
		return nil, nil
	}
	catalogue, err := s.planRepo.ListAgentAddons(ctx, false)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]*types.UsageAgentAddon, len(catalogue))
	for _, a := range catalogue {
		byID[a.AgentID] = a
	}
	out := make([]*types.UsageAgentAddon, 0, len(agentIDs))
	seen := map[string]bool{}
	for _, id := range agentIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		a := byID[id]
		if a == nil || !a.IsActive || !a.AgentAvailable || !a.PlanIDs.Includes(planID) {
			return nil, apperrors.NewBadRequestError("this specialist agent is not sold with the plan").
				WithDetails(map[string]string{"reason": "addon_not_sold", "agent_id": id})
		}
		out = append(out, a)
	}
	return out, nil
}

// priceAddonsWithPlan adds the add-on lines of a plan order: unit × units
// × the order's months, but only for time the scope has not paid for yet.
//   - Renewal of the current subscription: a right that does not yet run to
//     the current end also pays the uncovered gap (from max(now, its end) to
//     the current end), priced like a mid-term purchase; a right that
//     already runs past the current end pays only beyond it.
//   - New subscription replacing a current one (plan change): a right the
//     scope holds is carried over to its paid end, so only the time after
//     that end is charged; one that already covers the new period is
//     refused (addon_held).
//   - Either way, seats added on a held right pay for the added seats while
//     the held right still runs.
func (s *usageBillingService) priceAddonsWithPlan(
	ctx context.Context, q *types.UsageQuote, agentIDs []string, scopeID string,
) error {
	rows, err := s.sellableAddons(ctx, q.Plan.ID, agentIDs)
	if err != nil || len(rows) == 0 {
		return err
	}
	now := s.now()
	var cur *types.UsageSubscription
	held := map[string]*types.UsageSubscriptionAddon{}
	if scopeID != "" {
		if cur, err = s.planRepo.GetActiveSubscription(ctx, q.Plan.ScopeKind, scopeID); err != nil {
			return err
		}
		if cur != nil && cur.IsCurrentAt(now) {
			list, err := s.planRepo.ListSubscriptionAddons(ctx, cur.ID)
			if err != nil {
				return err
			}
			for _, h := range list {
				if h.IsCurrentAt(now) {
					held[h.AgentID] = h
				}
			}
		} else {
			cur = nil
		}
	}
	units := addonUnits(q.Mode, q.Seats)
	renewal := renewsSubscription(cur, q.Plan.ID, q.Mode, now)
	for _, a := range rows {
		line := types.UsageOrderAddonLine{
			AgentID: a.AgentID, Name: a.AgentName, UnitPriceVND: a.PriceVND, Units: units, Months: q.Months,
		}
		h := held[a.AgentID]
		heldUnits := 0
		if cur != nil {
			heldUnits = addonUnits(cur.Mode, cur.Seats)
		}
		switch {
		case renewal && cur.EndsAt != nil && cur.EndsAt.After(now):
			oldEnd := *cur.EndsAt
			newEnd := types.SubscriptionEndVN(oldEnd, q.Months)
			covered := now
			if h != nil {
				covered = oldEnd
				if h.EndsAt != nil && h.EndsAt.After(now) {
					covered = *h.EndsAt
				}
				c := covered
				line.CoveredUntil = &c
			}
			if !covered.Before(newEnd) {
				return addonHeldError(a.AgentID, covered)
			}
			if covered.Before(oldEnd) {
				line.GapMonths, line.GapLeftoverDays, line.GapLeftoverFactor, line.GapVND =
					types.PriceAddonRemainder(a.PriceVND, units, covered, oldEnd)
				line.AmountVND = line.GapVND + a.PriceVND*int64(units)*int64(q.Months)
			} else {
				// held past the current end: only the rest of the renewal
				line.Months, line.LeftoverDays, line.LeftoverFactor, line.AmountVND =
					types.PriceAddonRemainder(a.PriceVND, units, covered, newEnd)
			}
			if h != nil && units > heldUnits {
				_, _, _, line.ExtraUnitsVND = types.PriceAddonRemainder(a.PriceVND, units-heldUnits, now, minTime(covered, newEnd))
				line.ExtraUnits = units - heldUnits
				line.AmountVND += line.ExtraUnitsVND
			}
		case !renewal && h != nil:
			// plan change: the held right is carried over to its paid end
			newEnd := types.SubscriptionEndVN(now, q.Months)
			carried := h.EndsAt
			if carried == nil {
				carried = cur.EndsAt
			}
			if carried == nil || !carried.Before(newEnd) {
				end := newEnd
				if carried != nil {
					end = *carried
				}
				return addonHeldError(a.AgentID, end)
			}
			c := *carried
			line.CoveredUntil = &c
			line.Months, line.LeftoverDays, line.LeftoverFactor, line.AmountVND =
				types.PriceAddonRemainder(a.PriceVND, units, c, newEnd)
			if units > heldUnits {
				_, _, _, line.ExtraUnitsVND = types.PriceAddonRemainder(a.PriceVND, units-heldUnits, now, c)
				line.ExtraUnits = units - heldUnits
				line.AmountVND += line.ExtraUnitsVND
			}
		default:
			line.AmountVND = a.PriceVND * int64(units) * int64(q.Months)
		}
		q.Addons = append(q.Addons, line)
	}
	q.AddonsVND = q.Addons.Total()
	q.SubtotalVND += q.AddonsVND
	return nil
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// addonHeldError refuses an add-on the scope already holds for the whole
// period an order would pay for.
func addonHeldError(agentID string, until time.Time) error {
	return apperrors.NewBadRequestError("the subscription already has this specialist agent for that period").
		WithDetails(map[string]string{"reason": "addon_held", "agent_id": agentID,
			"covered_until": until.Format(time.RFC3339)})
}

// addonOrderPromoAllowed is review item 5 in one place: whether a promo
// code also discounts an add-on-only (mid-term) order. User decision
// 19/10/2026: yes — a code that is still valid (active, inside its dates,
// uses left, and — when it names plans — for the subscription's plan)
// applies to any order at any time; its use is reserved, released on
// cancel/expiry, exactly as for plan orders.
func addonOrderPromoAllowed() bool { return true }

// refusePendingAddons refuses an order for an add-on that another pending
// order of the same scope already buys: confirmed one after the other they
// would charge twice for the same time.
func (s *usageBillingService) refusePendingAddons(ctx context.Context, scopeType, scopeID string, lines types.UsageOrderAddonLines) error {
	if len(lines) == 0 {
		return nil
	}
	pending, err := s.planRepo.PendingOrderAddons(ctx, scopeType, scopeID)
	if err != nil {
		return err
	}
	want := map[string]bool{}
	for _, l := range lines {
		want[l.AgentID] = true
	}
	for _, ids := range pending {
		for _, id := range ids {
			if want[id] {
				return apperrors.NewConflictError("another order waiting for payment already buys this specialist agent; pay or cancel it first").
					WithDetails(map[string]string{"reason": "addon_pending", "agent_id": id})
			}
		}
	}
	return nil
}

// quoteAddonOnly prices add-ons bought on the scope's current subscription:
// from now (or from the end of a right the scope already holds) to the
// subscription's end, by decision 7.3, × seats; they end with it.
func (s *usageBillingService) quoteAddonOnly(
	ctx context.Context, req types.UsageQuoteRequest, scopeID string,
) (*types.UsageQuote, error) {
	if scopeID == "" {
		return nil, apperrors.NewBadRequestError("scope_id is required")
	}
	if len(req.AddonAgentIDs) == 0 {
		return nil, apperrors.NewBadRequestError("choose at least one specialist agent")
	}
	now := s.now()
	cur, err := s.planRepo.GetActiveSubscription(ctx, req.ScopeType, scopeID)
	if err != nil {
		return nil, err
	}
	if cur == nil || !cur.IsCurrentAt(now) {
		return nil, apperrors.NewBadRequestError("there is no current subscription to add agents to").
			WithDetails(map[string]string{"reason": "no_subscription"})
	}
	if cur.EndsAt == nil {
		return nil, apperrors.NewBadRequestError("this subscription has no end date; ask the system administrator to add agents").
			WithDetails(map[string]string{"reason": "open_ended_subscription"})
	}
	plan := cur.Plan
	if plan == nil {
		if plan, err = s.plans.GetPlan(ctx, cur.PlanID); err != nil {
			return nil, err
		}
	}
	rows, err := s.sellableAddons(ctx, plan.ID, req.AddonAgentIDs)
	if err != nil {
		return nil, err
	}
	held, err := s.planRepo.ListSubscriptionAddons(ctx, cur.ID)
	if err != nil {
		return nil, err
	}
	heldBy := map[string]*types.UsageSubscriptionAddon{}
	for _, h := range held {
		heldBy[h.AgentID] = h
	}
	until := *cur.EndsAt
	units := addonUnits(cur.Mode, cur.Seats)
	q := &types.UsageQuote{
		Plan: plan, Mode: cur.Mode, Seats: cur.Seats, Kind: types.UsageOrderKindAddon,
		TargetSubscriptionID: cur.ID,
	}
	for _, a := range rows {
		from := now
		if h := heldBy[a.AgentID]; h != nil {
			if h.EndsAt == nil || !h.EndsAt.Before(until) {
				return nil, apperrors.NewBadRequestError("the subscription already has this specialist agent").
					WithDetails(map[string]string{"reason": "addon_held", "agent_id": a.AgentID})
			}
			if h.EndsAt.After(from) {
				from = *h.EndsAt
			}
		}
		months, days, factor, amount := types.PriceAddonRemainder(a.PriceVND, units, from, until)
		end := until
		q.Addons = append(q.Addons, types.UsageOrderAddonLine{
			AgentID: a.AgentID, Name: a.AgentName, UnitPriceVND: a.PriceVND, Units: units,
			Months: months, LeftoverDays: days, LeftoverFactor: factor, AmountVND: amount, PaidUntil: &end,
		})
	}
	q.AddonsVND = q.Addons.Total()
	q.SubtotalVND = q.AddonsVND
	q.TotalVND = q.SubtotalVND
	return q, nil
}

// applyAddonOrder gives the target subscription the bought add-ons. It must
// still be the scope's current subscription.
func (s *usageBillingService) applyAddonOrder(ctx context.Context, o *types.UsageOrder, now time.Time) (string, error) {
	cur, err := s.planRepo.GetActiveSubscription(ctx, o.ScopeType, o.ScopeID)
	if err != nil {
		return "", err
	}
	if cur == nil || o.TargetSubscriptionID == nil || cur.ID != *o.TargetSubscriptionID || !cur.IsCurrentAt(now) {
		return "", apperrors.NewConflictError(fmt.Sprintf(
			"order %s adds agents to a subscription that is no longer current; cancel it and order again", o.OrderCode))
	}
	if err := s.grantOrderAddons(ctx, o, cur, now, nil); err != nil {
		return "", err
	}
	return cur.ID, nil
}

// grantOrderAddons writes the order's add-on lines on sub: a line's right
// runs to its PaidUntil (mid-term purchase) or to the subscription's end,
// never shortening what the subscription already holds. A paid line whose
// right the subscription already holds to that end is refused (409): the
// money is kept on the order for the SuperAdmin to refund instead of being
// swallowed (the webhook records confirm_failed).
func (s *usageBillingService) grantOrderAddons(
	ctx context.Context, o *types.UsageOrder, sub *types.UsageSubscription, now time.Time,
	held map[string]*types.UsageSubscriptionAddon,
) error {
	if len(o.Addons) == 0 {
		return nil
	}
	if held == nil {
		rows, err := s.planRepo.ListSubscriptionAddons(ctx, sub.ID)
		if err != nil {
			return err
		}
		held = map[string]*types.UsageSubscriptionAddon{}
		for _, h := range rows {
			held[h.AgentID] = h
		}
	}
	orderID := o.ID
	for _, line := range o.Addons {
		end := sub.EndsAt
		if line.PaidUntil != nil {
			e := *line.PaidUntil
			end = &e
		}
		if h := held[line.AgentID]; line.AmountVND > 0 && end != nil && h.CoversUntil(now, *end) {
			return apperrors.NewConflictError(fmt.Sprintf(
				"order %s pays for %s, which the subscription already has to that date; cancel it",
				o.OrderCode, line.AgentID)).WithDetails(map[string]string{"reason": "addon_held", "agent_id": line.AgentID})
		}
		row := &types.UsageSubscriptionAddon{
			SubscriptionID: sub.ID, AgentID: line.AgentID, StartsAt: now, EndsAt: end,
			PriceVND: line.UnitPriceVND, OrderID: &orderID, GrantedBy: "order " + o.OrderCode,
		}
		if err := s.planRepo.UpsertSubscriptionAddon(ctx, row); err != nil {
			return err
		}
	}
	return nil
}

// orderAddonIDs is the set of agents an order buys.
func orderAddonIDs(o *types.UsageOrder) map[string]bool {
	out := make(map[string]bool, len(o.Addons))
	for _, l := range o.Addons {
		out[l.AgentID] = true
	}
	return out
}

// freezeUnrenewedAddons pins the end of every add-on of a renewed
// subscription that the renewal does not pay for: a right with no end date
// would otherwise follow the extended subscription for free. oldEnd is the
// subscription's end before the extension.
func (s *usageBillingService) freezeUnrenewedAddons(
	ctx context.Context, sub *types.UsageSubscription, o *types.UsageOrder, oldEnd *time.Time,
	held []*types.UsageSubscriptionAddon,
) error {
	if oldEnd == nil {
		return nil
	}
	renewed := orderAddonIDs(o)
	for _, h := range held {
		if renewed[h.AgentID] || h.EndsAt != nil {
			continue
		}
		if err := s.planRepo.PinSubscriptionAddonEnd(ctx, sub.ID, h.AgentID, *oldEnd); err != nil {
			return err
		}
	}
	return nil
}

// carryAddonsOver moves the current add-ons of a replaced subscription to
// its successor (plan change): each keeps its paid end (its own end, else
// the old subscription's), whether or not the new plan sells it.
func (s *usageBillingService) carryAddonsOver(
	ctx context.Context, old, next *types.UsageSubscription, o *types.UsageOrder, now time.Time,
) error {
	if old == nil || !old.IsCurrentAt(now) {
		return nil
	}
	held, err := s.planRepo.ListSubscriptionAddons(ctx, old.ID)
	if err != nil {
		return err
	}
	// every current right moves, including one the order buys again: the
	// order's line then only extends it (never-shorten merge)
	for _, h := range held {
		if !h.IsCurrentAt(now) {
			continue
		}
		end := h.EndsAt
		if end == nil {
			end = old.EndsAt
		}
		if end != nil && !end.After(now) {
			continue
		}
		row := &types.UsageSubscriptionAddon{
			SubscriptionID: next.ID, AgentID: h.AgentID, StartsAt: now, EndsAt: end,
			PriceVND: h.PriceVND, OrderID: h.OrderID, GrantedBy: h.GrantedBy,
		}
		if err := s.planRepo.UpsertSubscriptionAddon(ctx, row); err != nil {
			return err
		}
	}
	return nil
}

// ScopeAddons is the buyer's view of a scope: its current subscription,
// the add-ons it holds and those still on sale with its plan for it.
func (s *usageBillingService) ScopeAddons(ctx context.Context, scopeType, scopeID string) (*types.UsageScopeAddons, error) {
	out := &types.UsageScopeAddons{Held: []*types.UsageSubscriptionAddon{}, Purchasable: []types.UsagePlanAddonOffer{}}
	now := s.now()
	cur, err := s.planRepo.GetActiveSubscription(ctx, scopeType, strings.TrimSpace(scopeID))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return out, nil
		}
		return nil, err
	}
	if cur == nil || !cur.IsCurrentAt(now) {
		return out, nil
	}
	out.Subscription = cur
	held, err := s.planRepo.ListSubscriptionAddons(ctx, cur.ID)
	if err != nil {
		return nil, err
	}
	holds := map[string]bool{}
	for _, h := range held {
		if !h.IsCurrentAt(now) {
			continue
		}
		out.Held = append(out.Held, h)
		if cur.EndsAt == nil || h.EndsAt == nil || !h.EndsAt.Before(*cur.EndsAt) {
			holds[h.AgentID] = true
		}
	}
	catalogue, err := s.planRepo.ListAgentAddons(ctx, false)
	if err != nil {
		return nil, err
	}
	for _, a := range catalogue {
		if !a.IsActive || !a.AgentAvailable || !a.PlanIDs.Includes(cur.PlanID) || holds[a.AgentID] {
			continue
		}
		out.Purchasable = append(out.Purchasable, types.UsagePlanAddonOffer{
			AgentID: a.AgentID, Name: a.AgentName, Description: a.Description, PriceVND: a.PriceVND,
			PerSeat: cur.Mode == types.UsageModeSeat,
		})
	}
	return out, nil
}
