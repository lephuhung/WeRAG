package service

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"time"

	"gorm.io/gorm"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
)

// Plan change: credit for the unused paid days of the replaced package,
// seats kept on a seat plan bought again (types/usage_plan_change.go).

// creditDays are the whole Vietnamese calendar days after today up to end:
// today has started and is not given back.
func creditDays(now, end time.Time) int {
	d := types.CalendarDaysVN(now, end) - 1
	if d < 0 {
		return 0
	}
	return d
}

// paidFactor is the share of an order's list price that was paid: a promo
// code took (discount / what it applied to) off. A credit is value paid
// earlier, so it does not lower the factor.
func paidFactor(o *types.UsageOrder) (num, den int64) {
	base := o.SubtotalVND - o.CreditVND
	if base <= 0 || o.DiscountVND <= 0 {
		return 1, 1
	}
	if o.DiscountVND >= base {
		return 0, 1
	}
	return base - o.DiscountVND, base
}

// creditAmount is monthly × units × days / 30 × num / den, rounded down:
// a credit never gives back more than was paid.
func creditAmount(monthly int64, units, days int, num, den int64) int64 {
	if monthly <= 0 || units <= 0 || days <= 0 || num <= 0 || den <= 0 {
		return 0
	}
	v := new(big.Int).SetInt64(monthly)
	v.Mul(v, big.NewInt(int64(units)))
	v.Mul(v, big.NewInt(int64(days)))
	v.Mul(v, big.NewInt(num))
	v.Quo(v, big.NewInt(int64(usageProrationDaysPerMonth)*den))
	if !v.IsInt64() {
		return 0
	}
	return v.Int64()
}

// lastPaidPlanOrder is the latest paid plan order applied to subscription
// subID, nil when none (a SuperAdmin grant, a sign-up gift).
func (s *usageBillingService) lastPaidPlanOrder(ctx context.Context, sub *types.UsageSubscription) (*types.UsageOrder, error) {
	orders, err := s.repo.ListOrders(ctx, types.UsageOrderFilter{
		ScopeType: sub.ScopeType, ScopeID: sub.ScopeID, Status: types.UsageOrderPaid,
	})
	if err != nil {
		return nil, err
	}
	var best *types.UsageOrder
	for _, o := range orders {
		if o.Kind == types.UsageOrderKindAddon || o.SubscriptionID == nil || *o.SubscriptionID != sub.ID {
			continue
		}
		if best == nil || paidAt(o).After(paidAt(best)) {
			best = o
		}
	}
	return best, nil
}

func paidAt(o *types.UsageOrder) time.Time {
	if o.PaidAt != nil {
		return *o.PaidAt
	}
	return o.CreatedAt
}

// planCreditLine values the unused days of cur at the per-month price its
// last paid plan order charged (after its promo code).
func (s *usageBillingService) planCreditLine(
	ctx context.Context, cur *types.UsageSubscription, planName string, now time.Time,
) (types.UsageCreditLine, error) {
	units := addonUnits(cur.Mode, cur.Seats)
	line := types.UsageCreditLine{Kind: types.UsageCreditPlan, Name: planName, Units: units}
	if cur.EndsAt == nil {
		return line, nil
	}
	line.Days = creditDays(now, *cur.EndsAt)
	o, err := s.lastPaidPlanOrder(ctx, cur)
	if err != nil || o == nil {
		return line, err
	}
	line.Paid = true
	num, den := paidFactor(o)
	line.AmountVND = creditAmount(o.UnitPriceVND, units, line.Days, num, den)
	return line, nil
}

// addonCreditLine values the unused days of a held add-on the order drops,
// at the price frozen on its row (after the promo of the order that sold it).
func (s *usageBillingService) addonCreditLine(
	ctx context.Context, h *types.UsageSubscriptionAddon, units int, end, now time.Time,
) (types.UsageCreditLine, error) {
	line := types.UsageCreditLine{
		Kind: types.UsageCreditAddon, AgentID: h.AgentID, Name: h.AgentName, Units: units,
		Days: creditDays(now, end),
	}
	if h.OrderID == nil {
		return line, nil
	}
	o, err := s.repo.GetOrder(ctx, *h.OrderID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return line, nil
	}
	if err != nil {
		return line, err
	}
	if o.Status != types.UsageOrderPaid {
		return line, nil
	}
	line.Paid = true
	num, den := paidFactor(o)
	line.AmountVND = creditAmount(h.PriceVND, units, line.Days, num, den)
	return line, nil
}

// pricePlanChange completes a plan quote for scopeID: the new end, the
// seats a seat plan keeps, and — when the order replaces a current package
// of another plan — what it replaces and the credit for its unused days
// (add-on credit lines were added by priceAddonsWithPlan). Runs after the
// add-ons are priced, before the promo code.
func (s *usageBillingService) pricePlanChange(ctx context.Context, q *types.UsageQuote, scopeID string) error {
	now := s.now()
	end := types.SubscriptionEndVN(now, q.Months)
	q.SubscriptionEndsAt = &end
	if scopeID == "" {
		return nil
	}
	cur, err := s.planRepo.GetActiveSubscription(ctx, q.Plan.ScopeKind, scopeID)
	if err != nil || cur == nil {
		return err
	}
	if renewsSubscription(cur, q.Plan.ID, q.Mode, now) {
		if cur.EndsAt != nil && cur.EndsAt.After(now) {
			e := types.SubscriptionEndVN(*cur.EndsAt, q.Months)
			q.SubscriptionEndsAt = &e
		}
		return nil
	}
	if q.Mode == types.UsageModeSeat && cur.Mode == types.UsageModeSeat {
		n, err := s.planRepo.CountSeats(ctx, cur.ID)
		if err != nil {
			return err
		}
		q.SeatsCarried = int(n)
		if q.SeatsCarried > q.Seats {
			q.SeatsCarried = q.Seats
		}
	}
	if !cur.IsCurrentAt(now) {
		return nil
	}
	name := ""
	if cur.Plan != nil {
		name = cur.Plan.Name
	} else if p, err := s.plans.GetPlan(ctx, cur.PlanID); err == nil && p != nil {
		name = p.Name
	}
	q.Replaces = &types.UsageReplacedSubscription{
		SubscriptionID: cur.ID, PlanID: cur.PlanID, PlanName: name, EndsAt: cur.EndsAt,
	}
	if cur.EndsAt != nil {
		line, err := s.planCreditLine(ctx, cur, name, now)
		if err != nil {
			return err
		}
		q.CreditLines = append(types.UsageCreditLines{line}, q.CreditLines...)
	}
	total := q.CreditLines.Total()
	q.CreditVND = total
	if q.CreditVND > q.SubtotalVND {
		q.CreditVND = q.SubtotalVND
	}
	q.CreditUnusedVND = total - q.CreditVND
	return nil
}

// refuseReplacedMoved refuses a plan-change order whose credit was priced
// against another subscription (or another end) than the scope has now.
func refuseReplacedMoved(o *types.UsageOrder, cur *types.UsageSubscription) error {
	if o.ReplacesSubscriptionID == nil {
		return nil
	}
	same := cur != nil && cur.ID == *o.ReplacesSubscriptionID
	if same {
		switch {
		case cur.EndsAt == nil && o.ReplacesEndsAt == nil:
		case cur.EndsAt != nil && o.ReplacesEndsAt != nil && cur.EndsAt.Equal(*o.ReplacesEndsAt):
		default:
			same = false
		}
	}
	if same {
		return nil
	}
	return apperrors.NewConflictError(fmt.Sprintf(
		"the package changed since order %s was priced; cancel it and order again", o.OrderCode)).
		WithDetails(map[string]string{"reason": "subscription_changed"})
}

// carrySeats gives the new seat subscription the seats assigned on the
// previous one, earliest first, up to the seats it has.
func (s *usageBillingService) carrySeats(ctx context.Context, prev, next *types.UsageSubscription) error {
	if prev == nil || prev.Mode != types.UsageModeSeat || next.Mode != types.UsageModeSeat || next.Seats <= 0 {
		return nil
	}
	seats, err := s.planRepo.ListSeats(ctx, prev.ID)
	if err != nil {
		return err
	}
	sort.SliceStable(seats, func(i, j int) bool { return seats[i].AssignedAt.Before(seats[j].AssignedAt) })
	for i, st := range seats {
		if i >= next.Seats {
			break
		}
		if err := s.planRepo.AddSeat(ctx, &types.UsageSeat{
			SubscriptionID: next.ID, UserID: st.UserID, AssignedBy: st.AssignedBy, AssignedAt: st.AssignedAt,
		}); err != nil {
			return err
		}
	}
	return nil
}
