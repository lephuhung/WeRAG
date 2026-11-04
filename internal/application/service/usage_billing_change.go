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

// A credit values what each paid order bought, by the share of it still
// unused (user decision 03/11/2026, e2e review): every order is split into
// pieces — units [lo, hi) (seats, 1 for a pool package) paid net VND for
// the period [start, end) — and a piece gives back
//
//	net × units taken / units of the piece × unused days / days of the period
//
// rounded down. net is what the order paid for the piece in cash or
// applied credit, after its promo code; unused days are the Vietnamese
// calendar days of the period after today (today has started and is not
// given back) up to the end being credited; the period's days count its
// first day (charges count today). A piece never returns more than its
// net, and the pieces of one line cover disjoint time, so a credit never
// gives back more than was paid (in cash or applied credit) for the
// remaining time — whatever the length of the months (a
// 31-day month is not worth 31/30 of its price) or the promo code.

// paidPiece is money an order paid for units [lo, hi) over [start, end),
// in cash or applied credit (a plan change's credit is money paid earlier),
// after its promo code.
// minDays: the days the piece was priced for when that is longer than the
// span (a proration quoted before it was confirmed).
// upTo (when > 0) keeps only units below it creditable without changing
// the piece's per-unit value: a piece of a replaced package counts only
// the units carried over (the others were credited at that change).
type paidPiece struct {
	lo, hi     int
	net        int64
	start, end time.Time
	minDays    int
	upTo       int
}

// netPaid is part of order o after its promo code (paidFactor), rounded
// down: what was paid for it in cash or applied credit.
func netPaid(o *types.UsageOrder, part int64) int64 {
	if part <= 0 {
		return 0
	}
	num, den := paidFactor(o)
	return mulDivFloor(part, num, den)
}

// mulDivFloor is a × b / c rounded down (0 when it does not fit, c ≤ 0).
func mulDivFloor(a, b, c int64) int64 {
	if a <= 0 || b <= 0 || c <= 0 {
		return 0
	}
	v := new(big.Int).Mul(big.NewInt(a), big.NewInt(b))
	v.Quo(v, big.NewInt(c))
	if !v.IsInt64() {
		return 0
	}
	return v.Int64()
}

// creditFrom is where credited time starts: the next Vietnamese midnight
// (today is not given back).
func creditFrom(now time.Time) time.Time {
	return types.EndOfDayVN(now.Add(time.Nanosecond))
}

// piecesCredit values the newest `take` of `total` units ([total-take,
// total)) from tomorrow to until. Units above total no longer exist; units
// no piece covers (a SuperAdmin grant, a gift) are worth nothing. paid
// reports whether a paid order covers any of the units taken.
func piecesCredit(pieces []paidPiece, total, take int, now, until time.Time) (amount int64, paid bool) {
	if take > total {
		take = total
	}
	if take <= 0 {
		return 0, false
	}
	tLo := total - take
	from := creditFrom(now)
	for _, p := range pieces {
		if p.hi <= p.lo {
			continue
		}
		lo, hi := p.lo, p.hi
		if lo < tLo {
			lo = tLo
		}
		if hi > total {
			hi = total
		}
		if p.upTo > 0 && hi > p.upTo {
			hi = p.upTo
		}
		if hi <= lo {
			continue
		}
		paid = true
		periodDays := types.CalendarDaysVN(p.start, p.end)
		if p.minDays > periodDays {
			periodDays = p.minDays
		}
		if periodDays <= 0 || p.net <= 0 {
			continue
		}
		start, end := p.start, p.end
		if from.After(start) {
			start = from
		}
		if until.Before(end) {
			end = until
		}
		days := types.CalendarDaysVN(start, end)
		if days <= 0 {
			continue
		}
		if days > periodDays {
			days = periodDays
		}
		amount += mulDivFloor(p.net, int64(hi-lo)*int64(days), int64(p.hi-p.lo)*int64(periodDays))
	}
	return amount, paid
}

// orderSpan is what a paid order did to its subscription: the period it
// bought and the units (seats) before and after it.
type orderSpan struct {
	start, end    time.Time
	before, after int
}

// orderSpans walks the paid orders of one subscription, oldest first. The
// period is the one stored at confirmation (PeriodStartsAt/ResultEndsAt);
// older orders get it rebuilt the way apply computed it: a plan order
// from its payment, or from the previous end when it renewed; a seat order
// from its payment to the end it was priced to. Units follow the seats each
// order set or added.
func orderSpans(orders []*types.UsageOrder) map[string]orderSpan {
	out := make(map[string]orderSpan, len(orders))
	var prevEnd *time.Time
	units := 0
	for _, o := range orders {
		sp := orderSpan{before: units, start: paidAt(o)}
		switch o.Kind {
		case types.UsageOrderKindAddon:
			sp.after = units
			if o.PeriodStartsAt != nil {
				sp.start = *o.PeriodStartsAt
			}
			out[o.ID] = sp
			continue
		case types.UsageOrderKindSeats:
			units += o.Seats
			sp.after = units
			switch {
			case o.PeriodStartsAt != nil && o.ResultEndsAt != nil:
				sp.start, sp.end = *o.PeriodStartsAt, *o.ResultEndsAt
			case o.TargetEndsAt != nil:
				sp.end = *o.TargetEndsAt
			case prevEnd != nil:
				sp.end = *prevEnd
			}
			out[o.ID] = sp
			continue
		}
		if u := addonUnits(o.Mode, o.Seats); u > units {
			units = u
		}
		sp.after = units
		if o.PeriodStartsAt != nil && o.ResultEndsAt != nil {
			sp.start, sp.end = *o.PeriodStartsAt, *o.ResultEndsAt
		} else {
			if prevEnd != nil && prevEnd.After(sp.start) {
				sp.start = *prevEnd
			}
			sp.end = types.SubscriptionEndVN(sp.start, o.Months)
		}
		e := sp.end
		prevEnd = &e
		out[o.ID] = sp
	}
	return out
}

// planPieces are what the paid orders of one subscription paid for the
// package itself: a plan order's plan line for its units over its period
// and the seats a renewal added for the rest of the period it extended; a
// seat order's seats to the end it was priced to. Add-on lines are left to
// addonPieces.
func planPieces(orders []*types.UsageOrder, spans map[string]orderSpan) []paidPiece {
	var out []paidPiece
	for _, o := range orders {
		sp := spans[o.ID]
		switch o.Kind {
		case types.UsageOrderKindAddon:
		case types.UsageOrderKindSeats:
			out = append(out, paidPiece{lo: sp.before, hi: sp.after, net: netPaid(o, o.ProrationVND),
				start: sp.start, end: sp.end, minDays: o.ProratedDays})
		default:
			part := o.SubtotalVND - o.ProrationVND - o.Addons.Total()
			out = append(out, paidPiece{lo: 0, hi: addonUnits(o.Mode, o.Seats), net: netPaid(o, part),
				start: sp.start, end: sp.end})
			if o.ProrationVND > 0 && o.Mode == types.UsageModeSeat {
				lo := sp.before
				if o.PricedSeats != nil {
					lo = *o.PricedSeats
				}
				out = append(out, paidPiece{lo: lo, hi: o.Seats, net: netPaid(o, o.ProrationVND),
					start: paidAt(o), end: sp.start, minDays: o.ProratedDays})
			}
		}
	}
	return out
}

// addonPieces are what the paid orders of one subscription paid for agent:
// each line's main time (a mid-term purchase: [PaidFrom, PaidUntil); a
// renewal: from the end it extended, or from the end of a right held past
// it, to the order's end; a plan change buying it again: from the carried
// end; else the order's period), a renewal's gap up to the old end, and the
// seats a line added while the held right still ran.
func addonPieces(orders []*types.UsageOrder, spans map[string]orderSpan, agentID string) []paidPiece {
	var out []paidPiece
	for _, o := range orders {
		sp := spans[o.ID]
		paid := paidAt(o)
		for _, l := range o.Addons {
			if l.AgentID != agentID {
				continue
			}
			if l.SeatsOnly {
				if l.ExtraUnits > 0 && l.CoveredUntil != nil {
					out = append(out, paidPiece{lo: sp.after - l.ExtraUnits, hi: sp.after,
						net: netPaid(o, l.ExtraUnitsVND), start: paid, end: *l.CoveredUntil})
				}
				continue
			}
			start, end := sp.start, sp.end
			switch {
			case l.PaidUntil != nil:
				start, end = paid, *l.PaidUntil
				if l.PaidFrom != nil {
					start = *l.PaidFrom
				}
			case l.SubscriptionEnd != nil:
				start = *l.SubscriptionEnd
				if l.CoveredUntil != nil && l.CoveredUntil.After(start) {
					start = *l.CoveredUntil
				}
			case l.CoveredUntil != nil:
				start = *l.CoveredUntil
			}
			main := l.AmountVND - l.GapVND - l.ExtraUnitsVND
			out = append(out, paidPiece{lo: 0, hi: l.Units, net: netPaid(o, main), start: start, end: end})
			if l.GapVND > 0 && l.SubscriptionEnd != nil {
				gs := paid
				if l.CoveredUntil != nil {
					gs = *l.CoveredUntil
				}
				out = append(out, paidPiece{lo: 0, hi: l.Units, net: netPaid(o, l.GapVND), start: gs, end: *l.SubscriptionEnd})
			}
			if l.ExtraUnits > 0 && l.CoveredUntil != nil {
				ee := minTime(*l.CoveredUntil, end)
				out = append(out, paidPiece{lo: l.Units - l.ExtraUnits, hi: l.Units,
					net: netPaid(o, l.ExtraUnitsVND), start: paid, end: ee})
			}
		}
	}
	return out
}

// paidOrdersBySub are the paid orders of sub's scope, oldest first,
// grouped by the subscription they were applied to.
func (s *usageBillingService) paidOrdersBySub(
	ctx context.Context, sub *types.UsageSubscription,
) (map[string][]*types.UsageOrder, error) {
	orders, err := s.repo.ListOrders(ctx, types.UsageOrderFilter{
		ScopeType: sub.ScopeType, ScopeID: sub.ScopeID, Status: types.UsageOrderPaid,
	})
	if err != nil {
		return nil, err
	}
	out := map[string][]*types.UsageOrder{}
	for _, o := range orders {
		if o.SubscriptionID == nil {
			continue
		}
		out[*o.SubscriptionID] = append(out[*o.SubscriptionID], o)
	}
	for id := range out {
		list := out[id]
		sort.SliceStable(list, func(i, j int) bool {
			a, b := paidAt(list[i]), paidAt(list[j])
			if !a.Equal(b) {
				return a.Before(b)
			}
			return list[i].CreatedAt.Before(list[j].CreatedAt)
		})
	}
	return out, nil
}

// addonChainMaxDepth bounds the walk back through replaced packages.
const addonChainMaxDepth = 16

// addonChain is the subscriptions a held right of agent on sub may have
// been paid on, each with the units still creditable there (0 = all): sub
// itself, then — through the plan-change order that started each one
// (ReplacesSubscriptionID) — the packages it replaced, as long as that
// change carried the agent over (a dropped agent was credited then: the
// walk stops). A change to fewer units credited the units not carried, so
// the replaced package's pieces count only up to the units carried. Cycles
// and chains longer than addonChainMaxDepth stop the walk.
func addonChain(bySub map[string][]*types.UsageOrder, subID, agentID string) map[string]int {
	out := map[string]int{subID: 0}
	upTo := 0
	cur := subID
	for depth := 0; depth < addonChainMaxDepth; depth++ {
		var first *types.UsageOrder
		for _, o := range bySub[cur] {
			if o.Kind != types.UsageOrderKindAddon && o.Kind != types.UsageOrderKindSeats {
				first = o
				break
			}
		}
		if first == nil || first.ReplacesSubscriptionID == nil || first.CreditLines.DroppedAddons()[agentID] {
			break
		}
		prev := *first.ReplacesSubscriptionID
		if _, seen := out[prev]; seen {
			break
		}
		if u := addonUnits(first.Mode, first.Seats); upTo == 0 || u < upTo {
			upTo = u
		}
		out[prev] = upTo
		cur = prev
	}
	return out
}

// planCreditLine values the unused days of cur: what each paid order on it
// (plan orders, the seats renewals and seat orders added) paid for the time
// after today, at the price and promo of that order; seats no order paid
// for (a SuperAdmin grant) are worth nothing. A pool package is one unit.
func (s *usageBillingService) planCreditLine(
	ctx context.Context, cur *types.UsageSubscription, planName string, now time.Time,
) (types.UsageCreditLine, error) {
	units := addonUnits(cur.Mode, cur.Seats)
	line := types.UsageCreditLine{Kind: types.UsageCreditPlan, Name: planName, Units: units}
	if cur.EndsAt == nil {
		return line, nil
	}
	line.Days = creditDays(now, *cur.EndsAt)
	bySub, err := s.paidOrdersBySub(ctx, cur)
	if err != nil {
		return line, err
	}
	orders := bySub[cur.ID]
	pieces := planPieces(orders, orderSpans(orders))
	line.AmountVND, line.Paid = piecesCredit(pieces, units, units, now, *cur.EndsAt)
	return line, nil
}

// addonCreditLine values the unused days, up to end, of the newest `take`
// of the heldUnits units of a held add-on (all of them when the order drops
// it, the units not carried over when it keeps it on fewer seats): what the
// paid orders of sub and of the packages it replaced that carried the
// agent over (addonChain; a right kept and bought again on a plan change
// names the new order only) paid for it after today, in cash or applied
// credit. Units no order paid for are worth nothing.
func (s *usageBillingService) addonCreditLine(
	ctx context.Context, h *types.UsageSubscriptionAddon, sub *types.UsageSubscription,
	heldUnits, take int, end, now time.Time,
) (types.UsageCreditLine, error) {
	line := types.UsageCreditLine{
		Kind: types.UsageCreditAddon, AgentID: h.AgentID, Name: h.AgentName, Units: take,
		Days: creditDays(now, end),
	}
	if sub == nil {
		return line, nil
	}
	bySub, err := s.paidOrdersBySub(ctx, sub)
	if err != nil {
		return line, err
	}
	// sub, the packages it replaced that carried the agent over (a right
	// kept and bought again on a plan change now names the new order), and
	// the package of the order that last granted it
	chain := addonChain(bySub, sub.ID, h.AgentID)
	if h.OrderID != nil {
		o, err := s.repo.GetOrder(ctx, *h.OrderID)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return line, err
		}
		if err == nil && o.Status == types.UsageOrderPaid && o.SubscriptionID != nil {
			if _, ok := chain[*o.SubscriptionID]; !ok {
				// outside the chain (an older order without the replaced
				// package recorded): only the fewest units carried count
				least := 0
				for id, u := range chain {
					if id != sub.ID && u > 0 && (least == 0 || u < least) {
						least = u
					}
				}
				chain[*o.SubscriptionID] = least
			}
		}
	}
	ids := make([]string, 0, len(chain))
	for id := range chain {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var pieces []paidPiece
	for _, id := range ids {
		orders := bySub[id]
		for _, p := range addonPieces(orders, orderSpans(orders), h.AgentID) {
			p.upTo = chain[id]
			pieces = append(pieces, p)
		}
	}
	line.AmountVND, line.Paid = piecesCredit(pieces, heldUnits, take, now, end)
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
