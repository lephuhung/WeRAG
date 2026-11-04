package service

import (
	"context"
	"fmt"
	"time"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
)

// Extra seats (chỗ) bought mid-term on the scope's current seat
// subscription (Fable billing UX review M10, user decision 03/11/2026): k
// seats for the rest of the current period only, no extra month.
//
//   - The seats are priced like the extra-seat line of a seat renewal
//     (priceSeatRenewal): the per-seat month of the plan's current price, at
//     the term the subscription was last bought with, × k × the Vietnamese
//     calendar days left (a started day counting) / 30, rounded up.
//   - Every add-on the subscription holds is priced for the k seats over the
//     time its right still runs, by the add-on remainder rule (whole months,
//     then ≤ 15 days = ½ month, more = a full month), at the price frozen on
//     its row. The rights themselves are unchanged: they are per subscription.
//   - Promo codes follow the add-on-only orders (addonOrderPromoAllowed).
//   - Confirming adds k seats; the end does not move. It is refused when the
//     subscription is no longer the one, or no longer ends where, the order
//     was priced against, or when it holds an add-on (or a longer right) the
//     order did not price.

// seatOrderUnitPrice is the per-seat month the extra seats of cur are
// charged at: the plan's current price for the term the subscription was
// last bought with (the average month of that term, rounded up, as a seat
// renewal charges), else the monthly price, else the first priced term.
func (s *usageBillingService) seatOrderUnitPrice(
	ctx context.Context, plan *types.UsagePlan, cur *types.UsageSubscription,
) (unit int64, term string, err error) {
	var tries []int
	last, err := s.lastPaidPlanOrder(ctx, cur)
	if err != nil {
		return 0, "", err
	}
	if last != nil && last.Months > 0 {
		tries = append(tries, last.Months)
	}
	tries = append(tries, 1, 3, 12)
	for _, m := range tries {
		total, t, ok := plan.PriceForMonths(m)
		if ok && total > 0 {
			return (total + int64(m) - 1) / int64(m), t, nil
		}
	}
	return 0, "", apperrors.NewBadRequestError("this plan has no seat price; ask the system administrator to add seats").
		WithDetails(map[string]string{"reason": "plan_not_sold"})
}

// currentSeatSubscription is the scope's current seat subscription with an
// end date, or the refusal a seat order gets.
func (s *usageBillingService) currentSeatSubscription(
	ctx context.Context, scopeType, scopeID string, now time.Time,
) (*types.UsageSubscription, error) {
	cur, err := s.planRepo.GetActiveSubscription(ctx, scopeType, scopeID)
	if err != nil {
		return nil, err
	}
	if cur == nil || !cur.IsCurrentAt(now) {
		return nil, apperrors.NewBadRequestError("there is no current package to add seats to").
			WithDetails(map[string]string{"reason": "no_subscription"})
	}
	if cur.Mode != types.UsageModeSeat {
		return nil, apperrors.NewBadRequestError("the current package is not sold per seat").
			WithDetails(map[string]string{"reason": "not_seat_plan"})
	}
	if cur.EndsAt == nil {
		return nil, apperrors.NewBadRequestError("this package has no end date; ask the system administrator to add seats").
			WithDetails(map[string]string{"reason": "open_ended_subscription"})
	}
	return cur, nil
}

// quoteSeatsOnly prices req.Seats more seats on the scope's current seat
// subscription for the rest of its period.
func (s *usageBillingService) quoteSeatsOnly(
	ctx context.Context, req types.UsageQuoteRequest, scopeID string,
) (*types.UsageQuote, error) {
	if scopeID == "" {
		return nil, apperrors.NewBadRequestError("scope_id is required")
	}
	if req.ScopeType != types.UsageScopeTenant {
		return nil, apperrors.NewBadRequestError("seats are sold to an organization only").
			WithDetails(map[string]string{"reason": "not_seat_plan"})
	}
	if req.Seats <= 0 {
		return nil, apperrors.NewBadRequestError("choose at least one seat").
			WithDetails(map[string]string{"reason": "seats_none"})
	}
	now := s.now()
	cur, err := s.currentSeatSubscription(ctx, req.ScopeType, scopeID, now)
	if err != nil {
		return nil, err
	}
	if cur.Seats+req.Seats > usageOrderMaxSeats {
		return nil, apperrors.NewBadRequestError(fmt.Sprintf("seats must be at most %d", usageOrderMaxSeats))
	}
	plan := cur.Plan
	if plan == nil {
		if plan, err = s.plans.GetPlan(ctx, cur.PlanID); err != nil {
			return nil, err
		}
	}
	unit, term, err := s.seatOrderUnitPrice(ctx, plan, cur)
	if err != nil {
		return nil, err
	}
	end := *cur.EndsAt
	k := req.Seats
	days := types.CalendarDaysVN(now, end)
	q := &types.UsageQuote{
		Plan: plan, Mode: types.UsageModeSeat, Seats: k, UnitPriceVND: unit, BillingTerm: term,
		CurrentSeats: cur.Seats, ProratedDays: days, Kind: types.UsageOrderKindSeats,
		TargetSubscriptionID: cur.ID, SubscriptionEndsAt: &end,
	}
	// the same day proration as the extra seats of a renewal, rounded up
	q.ProrationVND = (unit*int64(k)*int64(days) + usageProrationDaysPerMonth - 1) / usageProrationDaysPerMonth
	held, err := s.planRepo.ListSubscriptionAddons(ctx, cur.ID)
	if err != nil {
		return nil, err
	}
	// A right SuperAdmin removes or shortens after this quote keeps what the
	// order paid for it (the overpayment is not refunded automatically); a
	// right added or lengthened is refused at confirmation instead.
	for _, h := range held {
		covered := seatAddonEnd(h, cur)
		if !h.IsCurrentAt(now) || covered == nil || !covered.After(now) {
			continue
		}
		c := *covered
		line := types.UsageOrderAddonLine{
			AgentID: h.AgentID, Name: h.AgentName, UnitPriceVND: h.PriceVND, SeatsOnly: true,
			CoveredUntil: &c, ExtraUnits: k,
		}
		_, _, _, line.ExtraUnitsVND = types.PriceAddonRemainder(h.PriceVND, k, now, c)
		line.AmountVND = line.ExtraUnitsVND
		q.Addons = append(q.Addons, line)
	}
	q.AddonsVND = q.Addons.Total()
	q.SubtotalVND = q.ProrationVND + q.AddonsVND
	q.TotalVND = q.SubtotalVND
	return q, nil
}

// seatAddonEnd is how long a held right serves extra seats: to its own end
// (else the subscription's), never past the subscription's end — the seats
// stop there (review L1).
func seatAddonEnd(h *types.UsageSubscriptionAddon, sub *types.UsageSubscription) *time.Time {
	end := h.EndsAt
	if end == nil {
		end = sub.EndsAt
	}
	if end != nil && sub.EndsAt != nil && end.After(*sub.EndsAt) {
		end = sub.EndsAt
	}
	return end
}

// refusePendingSeatOrder refuses a seat order while the scope has another
// one waiting for payment: the second was priced without the first's
// seats, and paying both by mistake would buy twice as many.
func (s *usageBillingService) refusePendingSeatOrder(ctx context.Context, scopeType, scopeID string) error {
	pending, err := s.repo.ListOrders(ctx, types.UsageOrderFilter{
		ScopeType: scopeType, ScopeID: scopeID,
		Status: types.UsageOrderPending, Kind: types.UsageOrderKindSeats, Limit: 1,
	})
	if err != nil {
		return err
	}
	if len(pending) == 0 {
		return nil
	}
	return apperrors.NewConflictError("order " + pending[0].OrderCode + " for more seats is already waiting for payment; pay or cancel it first").
		WithDetails(map[string]string{"reason": "seats_order_pending", "order_code": pending[0].OrderCode})
}

// applySeatsOrder adds the order's seats to the subscription it was priced
// on; the end does not move and the add-on rights stay as they are.
func (s *usageBillingService) applySeatsOrder(ctx context.Context, o *types.UsageOrder, now time.Time) (string, error) {
	cur, err := s.planRepo.GetActiveSubscription(ctx, o.ScopeType, o.ScopeID)
	if err != nil {
		return "", err
	}
	changed := func() error {
		return apperrors.NewConflictError(fmt.Sprintf(
			"the package changed since order %s was priced; cancel it and order again", o.OrderCode)).
			WithDetails(map[string]string{"reason": "subscription_changed"})
	}
	if cur == nil || o.TargetSubscriptionID == nil || cur.ID != *o.TargetSubscriptionID || !cur.IsCurrentAt(now) ||
		cur.Mode != types.UsageModeSeat || cur.EndsAt == nil || o.TargetEndsAt == nil || !cur.EndsAt.Equal(*o.TargetEndsAt) {
		return "", changed()
	}
	// every right the subscription holds now must have been priced for the
	// new seats, to no shorter end: one bought or extended since would give
	// the added seats time nobody paid for
	priced := map[string]*time.Time{}
	for i := range o.Addons {
		priced[o.Addons[i].AgentID] = o.Addons[i].CoveredUntil
	}
	held, err := s.planRepo.ListSubscriptionAddons(ctx, cur.ID)
	if err != nil {
		return "", err
	}
	for _, h := range held {
		end := seatAddonEnd(h, cur)
		if !h.IsCurrentAt(now) || end == nil || !end.After(now) {
			continue
		}
		p, ok := priced[h.AgentID]
		if !ok || p == nil || end.After(*p) {
			return "", changed()
		}
	}
	if o.Seats <= 0 {
		return "", apperrors.NewBadRequestError("the order adds no seats")
	}
	cur.Seats += o.Seats
	cur.Note = appendOrderNote(cur.Note, o.OrderCode)
	o.PeriodStartsAt, o.ResultEndsAt = timePtr(now), timePtr(*cur.EndsAt)
	if err := s.planRepo.UpdateSubscription(ctx, cur); err != nil {
		return "", err
	}
	return cur.ID, nil
}
