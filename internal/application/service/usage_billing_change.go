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
		// add-on and seat orders price part of a period, not the plan's month
		if o.Kind == types.UsageOrderKindAddon || o.Kind == types.UsageOrderKindSeats ||
			o.SubscriptionID == nil || *o.SubscriptionID != sub.ID {
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

// creditTranche is a part of the units (seats) of a package or add-on
// valued at the price one order paid for them: monthly per unit, after that
// order's promo (num/den). paid is false for units no paid order covers (a
// SuperAdmin grant): they are worth nothing.
type creditTranche struct {
	units    int
	monthly  int64
	num, den int64
	paid     bool
}

// orderTranche is the tranche of units an order paid for.
func orderTranche(o *types.UsageOrder, units int, monthly int64) creditTranche {
	num, den := paidFactor(o)
	return creditTranche{units: units, monthly: monthly, num: num, den: den, paid: true}
}

// trancheCredit values the last `take` of `total` units for days. The
// tranches are oldest first and capped at total; the units above them are
// unpaid. Units given up are taken from the newest (unpaid ones first).
// paid reports whether any of them was paid for.
func trancheCredit(tranches []creditTranche, total, take, days int) (amount int64, paid bool) {
	var list []creditTranche
	left := total
	for _, t := range tranches {
		if left <= 0 {
			break
		}
		if t.units > left {
			t.units = left
		}
		if t.units <= 0 {
			continue
		}
		list = append(list, t)
		left -= t.units
	}
	if left > 0 {
		list = append(list, creditTranche{units: left})
	}
	if take > total {
		take = total
	}
	for i := len(list) - 1; i >= 0 && take > 0; i-- {
		t := list[i]
		n := t.units
		if n > take {
			n = take
		}
		take -= n
		if t.paid {
			paid = true
			amount += creditAmount(t.monthly, n, days, t.num, t.den)
		}
	}
	return amount, paid
}

// paidOrdersAfter are the paid orders applied to subscription sub, paid
// after `after` (all of them when zero), oldest first.
func (s *usageBillingService) paidOrdersAfter(
	ctx context.Context, sub *types.UsageSubscription, after time.Time,
) ([]*types.UsageOrder, error) {
	orders, err := s.repo.ListOrders(ctx, types.UsageOrderFilter{
		ScopeType: sub.ScopeType, ScopeID: sub.ScopeID, Status: types.UsageOrderPaid,
	})
	if err != nil {
		return nil, err
	}
	var out []*types.UsageOrder
	for _, o := range orders {
		if o.SubscriptionID == nil || *o.SubscriptionID != sub.ID || !paidAt(o).After(after) {
			continue
		}
		out = append(out, o)
	}
	sort.SliceStable(out, func(i, j int) bool { return paidAt(out[i]).Before(paidAt(out[j])) })
	return out, nil
}

// planCreditLine values the unused days of cur in tranches (review M1): the
// seats of its last paid plan order at the per-month price that order
// charged (after its promo code), then the seats each later seat order
// bought at its own price and promo; seats above them (a SuperAdmin grant)
// are worth nothing. A pool package is one unit at the plan order's price.
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
	if err != nil {
		return line, err
	}
	var tranches []creditTranche
	var after time.Time
	if o != nil {
		n := units
		if cur.Mode == types.UsageModeSeat && o.Mode == types.UsageModeSeat && o.Seats > 0 && o.Seats < n {
			n = o.Seats
		}
		tranches = append(tranches, orderTranche(o, n, o.UnitPriceVND))
		after = paidAt(o)
	}
	if cur.Mode == types.UsageModeSeat {
		later, err := s.paidOrdersAfter(ctx, cur, after)
		if err != nil {
			return line, err
		}
		for _, so := range later {
			if so.Kind == types.UsageOrderKindSeats {
				tranches = append(tranches, orderTranche(so, so.Seats, so.UnitPriceVND))
			}
		}
	}
	line.AmountVND, line.Paid = trancheCredit(tranches, units, units, line.Days)
	return line, nil
}

// addonCreditLine values the unused days of `take` of the heldUnits units
// of a held add-on (all of them when the order drops it, the units not
// carried over when it keeps it on fewer seats), in tranches like
// planCreditLine: the units the order that sold it paid for, at the price
// frozen on its row, then the units later orders on sub added to it
// (extra-seat parts of seat orders and renewals) at their own price and
// promo; units above them are worth nothing. The newest units go first.
func (s *usageBillingService) addonCreditLine(
	ctx context.Context, h *types.UsageSubscriptionAddon, sub *types.UsageSubscription,
	heldUnits, take int, end, now time.Time,
) (types.UsageCreditLine, error) {
	line := types.UsageCreditLine{
		Kind: types.UsageCreditAddon, AgentID: h.AgentID, Name: h.AgentName, Units: take,
		Days: creditDays(now, end),
	}
	var tranches []creditTranche
	var after time.Time
	if h.OrderID != nil {
		o, err := s.repo.GetOrder(ctx, *h.OrderID)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return line, err
		}
		if err == nil && o.Status == types.UsageOrderPaid {
			n := heldUnits
			for _, l := range o.Addons {
				if l.AgentID == h.AgentID && !l.SeatsOnly && l.Units > 0 {
					n = l.Units
					break
				}
			}
			tranches = append(tranches, orderTranche(o, n, h.PriceVND))
			after = paidAt(o)
		}
	}
	if sub != nil && (h.OrderID == nil || len(tranches) > 0) {
		later, err := s.paidOrdersAfter(ctx, sub, after)
		if err != nil {
			return line, err
		}
		for _, o := range later {
			for _, l := range o.Addons {
				if l.AgentID == h.AgentID && l.ExtraUnits > 0 {
					tranches = append(tranches, orderTranche(o, l.ExtraUnits, l.UnitPriceVND))
				}
			}
		}
	}
	line.AmountVND, line.Paid = trancheCredit(tranches, heldUnits, take, line.Days)
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
		q.SeatsHeld = int(n)
		q.SeatsCarried = int(n)
		if q.SeatsCarried > q.Seats {
			q.SeatsCarried = q.Seats
		}
	}
	if !cur.IsCurrentAt(now) {
		return nil
	}
	// the credit counts the seats the package has now (review H2)
	if cur.Mode == types.UsageModeSeat {
		priced := cur.Seats
		q.PricedSeats = &priced
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
// against another subscription (or another end) than the scope has now,
// and an order priced with nothing to replace (a renewal, a first package)
// that would now replace a running package of another plan without credit.
func refuseReplacedMoved(o *types.UsageOrder, cur *types.UsageSubscription, now time.Time) error {
	// priced on another seat count: a seat order added seats meanwhile, so
	// the credit (or a renewal's proration) no longer matches (review H2)
	if o.PricedSeats != nil && cur != nil && cur.IsCurrentAt(now) && cur.Seats != *o.PricedSeats {
		return seatsChangedError(o, cur)
	}
	if o.ReplacesSubscriptionID == nil {
		if cur != nil && cur.IsCurrentAt(now) && !renewsSubscription(cur, o.PlanID, o.Mode, now) {
			return apperrors.NewConflictError(fmt.Sprintf(
				"the package changed since order %s was priced; cancel it and order again", o.OrderCode)).
				WithDetails(map[string]string{"reason": "subscription_changed"})
		}
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

// seatsChangedError refuses an order priced on another seat count than the
// current subscription has.
func seatsChangedError(o *types.UsageOrder, cur *types.UsageSubscription) error {
	return apperrors.NewConflictError(fmt.Sprintf(
		"the subscription now has %d seats but order %s was priced for %d; cancel it and order again",
		cur.Seats, o.OrderCode, *o.PricedSeats)).
		WithDetails(map[string]string{"reason": "subscription_changed"})
}

// seatSensitivePlanOrder: a plan order whose price depends on the seats of
// the scope's current subscription (a seat-mode order, or one priced
// against a seat subscription it renews or replaces).
func seatSensitivePlanOrder(o *types.UsageOrder) bool {
	return o.Kind != types.UsageOrderKindAddon && o.Kind != types.UsageOrderKindSeats &&
		(o.Mode == types.UsageModeSeat || o.PricedSeats != nil)
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
