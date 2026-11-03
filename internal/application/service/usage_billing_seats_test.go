package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
)

// errCodeReason is an AppError's HTTP code and details reason.
func errCodeReason(t *testing.T, err error) (int, string) {
	t.Helper()
	appErr, ok := err.(*apperrors.AppError)
	require.True(t, ok, "%v", err)
	if d, ok := appErr.Details.(map[string]string); ok {
		return appErr.HTTPCode, d["reason"]
	}
	return appErr.HTTPCode, ""
}

// paidSeats gives tenant 10003 a subscription of planID (seat mode) with
// seats seats ending at end, paid by a months-long order at unit/seat/month.
func (f *billingFixture) paidSeats(t *testing.T, planID string, seats int, end time.Time, months int, unit int64) {
	t.Helper()
	f.plans.active["tenant/10003"] = &types.UsageSubscription{ID: "sub-s", ScopeType: "tenant", ScopeID: "10003",
		PlanID: planID, Mode: "seat", Seats: seats, StartsAt: billingNow.AddDate(0, -1, 0), EndsAt: &end,
		Status: types.UsageSubscriptionActive}
	subID, paid := "sub-s", billingNow.AddDate(0, -1, 0)
	sub := unit * int64(months*seats)
	require.NoError(t, f.repo.CreateOrder(context.Background(), &types.UsageOrder{
		ID: "o-seat", OrderCode: "WRSEAT", ScopeType: "tenant", ScopeID: "10003", PlanID: planID, Kind: "plan",
		Mode: "seat", Seats: seats, Months: months, UnitPriceVND: unit, SubtotalVND: sub, TotalVND: sub,
		Status: types.UsageOrderPaid, PaidAt: &paid, SubscriptionID: &subID,
	}))
}

// heldThue gives the seat subscription "thue" (99.000/seat/month) bought
// by its paid order, running with the subscription.
func (f *billingFixture) heldThue(t *testing.T) {
	t.Helper()
	oid := "o-seat"
	require.NoError(t, f.plans.UpsertSubscriptionAddon(context.Background(), &types.UsageSubscriptionAddon{
		SubscriptionID: "sub-s", AgentID: "thue", AgentName: "Thuế HKD", StartsAt: billingNow.AddDate(0, -1, 0),
		PriceVND: 99_000, OrderID: &oid}))
}

func seatsReq(k int) types.UsageQuoteRequest {
	return types.UsageQuoteRequest{ScopeType: "tenant", Kind: types.UsageOrderKindSeats, Seats: k}
}

// M10: k extra seats for the rest of the period, at the plan's per-seat
// month × k × days left / 30 (the renewal proration), plus every held
// add-on × k by the half/full-month rule.
func TestSeatOrderPricesTheRestOfThePeriod(t *testing.T) {
	f := newBillingFixture(t)
	withAddonCatalogue(f)
	ctx := context.Background()
	end := endOfVN(2026, 11, 12) // 13/10 17:00 VN → 31 days touched
	f.paidSeats(t, "seat", 3, end, 1, 49_000)

	q, err := f.svc.Quote(ctx, seatsReq(2), "10003")
	require.NoError(t, err)
	require.Equal(t, types.UsageOrderKindSeats, q.Kind)
	require.Equal(t, 2, q.Seats)
	require.Equal(t, 3, q.CurrentSeats)
	require.Equal(t, 31, q.ProratedDays)
	require.Equal(t, int64(49_000), q.UnitPriceVND)
	require.Equal(t, int64(101_267), q.ProrationVND, "49.000 × 2 × 31 / 30, rounded up")
	require.Empty(t, q.Addons)
	require.Equal(t, int64(101_267), q.TotalVND)
	require.Equal(t, end, *q.SubscriptionEndsAt, "no extra month")
	require.Equal(t, "sub-s", q.TargetSubscriptionID)

	// a held add-on: 2 seats × 99.000 for 31 days (> 15) = a full month
	f.heldThue(t)
	q, err = f.svc.Quote(ctx, seatsReq(2), "10003")
	require.NoError(t, err)
	require.Len(t, q.Addons, 1)
	l := q.Addons[0]
	require.True(t, l.SeatsOnly)
	require.Equal(t, 2, l.ExtraUnits)
	require.Equal(t, int64(198_000), l.AmountVND)
	require.Equal(t, end, *l.CoveredUntil)
	require.Equal(t, int64(101_267+198_000), q.SubtotalVND)

	// a promo code applies as on an add-on-only order
	f.promo(t, types.UsagePromoCode{Code: "GIAM10", DiscountType: types.UsagePromoPercent, DiscountValue: 10, IsActive: true})
	r := seatsReq(2)
	r.PromoCode = "giam10"
	q, err = f.svc.Quote(ctx, r, "10003")
	require.NoError(t, err)
	require.Equal(t, int64(29_926), q.DiscountVND)
	require.Equal(t, int64(299_267-29_926), q.TotalVND)

	// ≤ 15 days left: the add-on is half a month
	g := newBillingFixture(t)
	withAddonCatalogue(g)
	g.paidSeats(t, "seat", 3, endOfVN(2026, 10, 22), 1, 49_000)
	g.heldThue(t)
	q, err = g.svc.Quote(ctx, seatsReq(1), "10003")
	require.NoError(t, err)
	require.Equal(t, 10, q.ProratedDays)
	require.Equal(t, int64(16_334), q.ProrationVND, "49.000 × 10 / 30, rounded up")
	require.Equal(t, int64(49_500), q.Addons[0].AmountVND)
}

// The per-seat month follows the term the subscription was bought with,
// like a seat renewal at that term.
func TestSeatOrderUsesTheTermBoughtWith(t *testing.T) {
	f := newBillingFixture(t)
	ctx := context.Background()
	f.paidSeats(t, "term", 4, endOfVN(2026, 11, 12), 3, 45_000)
	q, err := f.svc.Quote(ctx, seatsReq(3), "10003")
	require.NoError(t, err)
	require.Equal(t, types.UsageTermQuarter, q.BillingTerm)
	require.Equal(t, int64(45_000), q.UnitPriceVND, "135.000 a quarter")
	require.Equal(t, int64(139_500), q.ProrationVND, "45.000 × 3 × 31 / 30")

	// a subscription nobody paid for (a grant) is priced at the month
	g := newBillingFixture(t)
	end := endOfVN(2026, 11, 12)
	g.plans.active["tenant/10003"] = &types.UsageSubscription{ID: "sub-g", ScopeType: "tenant", ScopeID: "10003",
		PlanID: "term", Mode: "seat", Seats: 2, StartsAt: billingNow, EndsAt: &end, Status: types.UsageSubscriptionActive}
	q, err = g.svc.Quote(ctx, seatsReq(1), "10003")
	require.NoError(t, err)
	require.Equal(t, int64(50_000), q.UnitPriceVND)
}

// Confirmed: the seats grow by k, the end and the add-on rights stay.
func TestSeatOrderConfirmAddsSeatsOnly(t *testing.T) {
	f := newBillingFixture(t)
	withAddonCatalogue(f)
	ctx := context.Background()
	end := endOfVN(2026, 11, 12)
	f.paidSeats(t, "seat", 3, end, 1, 49_000)
	f.heldThue(t)
	before := *f.right("sub-s", "thue")

	o, err := f.svc.CreateOrder(ctx, seatsReq(2), "10003", "u1")
	require.NoError(t, err)
	require.Equal(t, types.UsageOrderKindSeats, o.Kind)
	require.Equal(t, 2, o.Seats)
	require.Zero(t, o.Months)
	require.Equal(t, end, *o.TargetEndsAt)
	require.Equal(t, int64(101_267), o.ProrationVND)
	require.Equal(t, 31, o.ProratedDays)
	require.Equal(t, int64(299_267), o.TotalVND)
	require.NotNil(t, o.ExpiresAt)

	v, err := f.svc.ConfirmOrder(ctx, o.ID, "sa")
	require.NoError(t, err)
	require.Equal(t, types.UsageOrderPaid, v.Status)
	sub := f.plans.active["tenant/10003"]
	require.Equal(t, "sub-s", sub.ID)
	require.Equal(t, 5, sub.Seats)
	require.Equal(t, end, *sub.EndsAt, "the end does not move")
	require.Equal(t, before, *f.right("sub-s", "thue"), "the add-on row is unchanged")
	require.Equal(t, "sub-s", *v.SubscriptionID)

	// a later plan-change credit is valued at the plan order, not this one
	last, err := f.svc.lastPaidPlanOrder(ctx, sub)
	require.NoError(t, err)
	require.Equal(t, "o-seat", last.ID)
}

// A 0-đồng seat order (a 100 % code) waits for a SuperAdmin like any other.
func TestFreeSeatOrderWaitsForConfirmation(t *testing.T) {
	f := newBillingFixture(t)
	ctx := context.Background()
	f.paidSeats(t, "seat", 3, endOfVN(2026, 11, 12), 1, 49_000)
	f.promo(t, types.UsagePromoCode{Code: "FREE", DiscountType: types.UsagePromoPercent, DiscountValue: 100, IsActive: true})
	r := seatsReq(1)
	r.PromoCode = "FREE"
	o, err := f.svc.CreateOrder(ctx, r, "10003", "u1")
	require.NoError(t, err)
	require.Zero(t, o.TotalVND)
	require.Equal(t, types.UsageOrderPending, o.Status)
	require.Nil(t, o.ExpiresAt)
	require.Equal(t, 3, f.plans.active["tenant/10003"].Seats, "not applied before a SuperAdmin confirms")
}

func TestSeatOrderGuards(t *testing.T) {
	ctx := context.Background()
	end := endOfVN(2026, 11, 12)
	cases := []struct {
		name   string
		setup  func(f *billingFixture)
		req    types.UsageQuoteRequest
		reason string
	}{
		{"no subscription", func(*billingFixture) {}, seatsReq(1), "no_subscription"},
		{"no seats", func(f *billingFixture) { f.paidSeats(t, "seat", 3, end, 1, 49_000) }, seatsReq(0), "seats_none"},
		{"personal scope", func(*billingFixture) {}, types.UsageQuoteRequest{ScopeType: "user", Kind: "seats", Seats: 1}, "not_seat_plan"},
		{"pool plan", func(f *billingFixture) { f.paidPool(t, end, 100_000, 100_000, 0) }, seatsReq(1), "not_seat_plan"},
		{"open-ended", func(f *billingFixture) {
			f.plans.active["tenant/10003"] = &types.UsageSubscription{ID: "sub-o", ScopeType: "tenant", ScopeID: "10003",
				PlanID: "seat", Mode: "seat", Seats: 3, StartsAt: billingNow.AddDate(0, -1, 0), Status: types.UsageSubscriptionActive}
		}, seatsReq(1), "open_ended_subscription"},
		{"expired", func(f *billingFixture) { f.paidSeats(t, "seat", 3, endOfVN(2026, 10, 1), 1, 49_000) }, seatsReq(1), "no_subscription"},
		{"not started", func(f *billingFixture) {
			f.paidSeats(t, "seat", 3, end, 1, 49_000)
			f.plans.active["tenant/10003"].StartsAt = billingNow.AddDate(0, 0, 2)
		}, seatsReq(1), "no_subscription"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newBillingFixture(t)
			c.setup(f)
			_, err := f.svc.Quote(ctx, c.req, func() string {
				if c.req.ScopeType == "user" {
					return "u1"
				}
				return "10003"
			}())
			require.Equal(t, c.reason, badRequestReason(t, err))
		})
	}
}

// One seat order waits at a time (409 seats_order_pending, no override).
func TestSeatOrderPendingDuplicateRefused(t *testing.T) {
	f := newBillingFixture(t)
	ctx := context.Background()
	f.paidSeats(t, "seat", 3, endOfVN(2026, 11, 12), 1, 49_000)
	first, err := f.svc.CreateOrder(ctx, seatsReq(1), "10003", "u1")
	require.NoError(t, err)
	r := seatsReq(2)
	r.ConfirmDuplicate = true
	_, err = f.svc.CreateOrder(ctx, r, "10003", "u1")
	code, reason := errCodeReason(t, err)
	require.Equal(t, 409, code)
	require.Equal(t, "seats_order_pending", reason)

	// once the first is cancelled another may be placed
	_, err = f.svc.CancelOrder(ctx, first.ID, false)
	require.NoError(t, err)
	_, err = f.svc.CreateOrder(ctx, seatsReq(2), "10003", "u1")
	require.NoError(t, err)
}

// Review H1/H2: a seat order and a plan order priced on the same seats are
// never pending together, either way round, and confirm_duplicate does not
// override it.
func TestSeatOrderAndPlanOrderRefuseEachOther(t *testing.T) {
	ctx := context.Background()
	end := endOfVN(2026, 11, 12)
	renewal := types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "seat", Months: 1, Seats: 5, ConfirmDuplicate: true}
	change := types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "term", Months: 1, Seats: 2, ConfirmDuplicate: true}
	toPool := types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "pool", Months: 1, ConfirmDuplicate: true}

	f := newBillingFixture(t)
	f.paidSeats(t, "seat", 3, end, 1, 49_000)
	seats, err := f.svc.CreateOrder(ctx, seatsReq(2), "10003", "u1")
	require.NoError(t, err)
	for _, req := range []types.UsageQuoteRequest{renewal, change, toPool} {
		_, err = f.svc.CreateOrder(ctx, req, "10003", "u1")
		code, reason := errCodeReason(t, err)
		require.Equal(t, 409, code, req.PlanID)
		require.Equal(t, "seats_order_pending", reason, req.PlanID)
	}
	_, err = f.svc.CancelOrder(ctx, seats.ID, false)
	require.NoError(t, err)

	plan, err := f.svc.CreateOrder(ctx, renewal, "10003", "u1")
	require.NoError(t, err)
	require.Equal(t, 3, *plan.PricedSeats)
	r := seatsReq(1)
	r.ConfirmDuplicate = true
	_, err = f.svc.CreateOrder(ctx, r, "10003", "u1")
	code, reason := errCodeReason(t, err)
	require.Equal(t, 409, code)
	require.Equal(t, "plan_order_pending", reason)
	// no override exists for a seats order: the message and details say so
	appErr := err.(*apperrors.AppError)
	require.NotContains(t, appErr.Message, "confirm")
	require.Contains(t, appErr.Message, "pay or cancel it first")
	require.Equal(t, "true", appErr.Details.(map[string]string)["no_override"])
}

// sneakOrder places req although orders are pending (as a race past the
// cross refusal between seat and plan orders, or a SuperAdmin reopen with
// force, would) and confirms it.
func (f *billingFixture) sneakOrder(t *testing.T, req types.UsageQuoteRequest) *types.UsageOrderView {
	t.Helper()
	ctx := context.Background()
	hidden := map[string]bool{}
	for id, o := range f.repo.orders {
		if o.Status == types.UsageOrderPending {
			o.Status = types.UsageOrderExpired
			hidden[id] = true
		}
	}
	o, err := f.svc.CreateOrder(ctx, req, "10003", "u1")
	require.NoError(t, err)
	for id := range hidden {
		f.repo.orders[id].Status = types.UsageOrderPending
	}
	v, err := f.svc.ConfirmOrder(ctx, o.ID, "sa")
	require.NoError(t, err)
	return v
}

// sneakSeatOrder: sneakOrder for k seats with promo.
func (f *billingFixture) sneakSeatOrder(t *testing.T, k int, promo string) *types.UsageOrderView {
	t.Helper()
	r := seatsReq(k)
	r.PromoCode = promo
	return f.sneakOrder(t, r)
}

// Review H1: a renewal priced before a seat order is refused once the seats
// grew, instead of charging the added seats a second time.
func TestRenewalAfterSeatOrderIsRefused(t *testing.T) {
	f := newBillingFixture(t)
	withAddonCatalogue(f)
	ctx := context.Background()
	end := endOfVN(2026, 11, 12)
	f.paidSeats(t, "seat", 3, end, 1, 49_000)
	f.heldThue(t)
	renew, err := f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "seat", Months: 1, Seats: 5}, "10003", "u1")
	require.NoError(t, err)
	require.Equal(t, int64(101_267), renew.ProrationVND, "it also pays the 2 added seats for the rest of the period")
	f.sneakSeatOrder(t, 2, "")
	require.Equal(t, 5, f.plans.active["tenant/10003"].Seats)

	_, err = f.svc.ConfirmOrder(ctx, renew.ID, "sa")
	code, reason := errCodeReason(t, err)
	require.Equal(t, 409, code)
	require.Equal(t, "subscription_changed", reason)
	require.Equal(t, end, *f.plans.active["tenant/10003"].EndsAt, "not extended")

	// a legacy order (no priced seats) keeps the old "fewer seats" check
	legacy, err := f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "seat", Months: 1, Seats: 5, ConfirmDuplicate: true}, "10003", "u1")
	require.NoError(t, err)
	f.repo.orders[legacy.ID].PricedSeats = nil
	f.repo.orders[legacy.ID].Seats = 4
	_, err = f.svc.ConfirmOrder(ctx, legacy.ID, "sa")
	require.Equal(t, 409, appErrHTTP(t, err))
}

// Review H2: a plan change priced before a seat order is refused: its credit
// ignores the seats just paid.
func TestPlanChangeAfterSeatOrderIsRefused(t *testing.T) {
	f := newBillingFixture(t)
	ctx := context.Background()
	f.paidSeats(t, "seat", 3, endOfVN(2026, 11, 12), 1, 49_000)
	change, err := f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "pool", Months: 1}, "10003", "u1")
	require.NoError(t, err)
	require.Equal(t, 3, *change.PricedSeats)
	f.sneakSeatOrder(t, 2, "")
	_, err = f.svc.ConfirmOrder(ctx, change.ID, "sa")
	_, reason := errCodeReason(t, err)
	require.Equal(t, "subscription_changed", reason)
	require.Equal(t, "seat", f.plans.active["tenant/10003"].PlanID)
}

// Confirming refuses a package that moved since the quote: renewed (another
// end), replaced, or holding an add-on the order did not price.
func TestSeatOrderAgainstAChangedPackageIsRefused(t *testing.T) {
	ctx := context.Background()
	end := endOfVN(2026, 11, 12)

	// a real plan order confirmed while the seat order waits
	t.Run("renewed", func(t *testing.T) {
		f := newBillingFixture(t)
		f.paidSeats(t, "seat", 3, end, 1, 49_000)
		o, err := f.svc.CreateOrder(ctx, seatsReq(2), "10003", "u1")
		require.NoError(t, err)
		f.sneakOrder(t, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "seat", Months: 1, Seats: 3})
		require.Equal(t, endOfVN(2026, 12, 12), *f.plans.active["tenant/10003"].EndsAt)
		_, err = f.svc.ConfirmOrder(ctx, o.ID, "sa")
		code, reason := errCodeReason(t, err)
		require.Equal(t, 409, code)
		require.Equal(t, "subscription_changed", reason)
		require.Equal(t, 3, f.plans.active["tenant/10003"].Seats)
	})
	t.Run("replaced", func(t *testing.T) {
		f := newBillingFixture(t)
		f.paidSeats(t, "seat", 3, end, 1, 49_000)
		o, err := f.svc.CreateOrder(ctx, seatsReq(2), "10003", "u1")
		require.NoError(t, err)
		f.sneakOrder(t, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "term", Months: 1, Seats: 3})
		require.Equal(t, "term", f.plans.active["tenant/10003"].PlanID)
		_, err = f.svc.ConfirmOrder(ctx, o.ID, "sa")
		code, reason := errCodeReason(t, err)
		require.Equal(t, 409, code)
		require.Equal(t, "subscription_changed", reason)
	})
	t.Run("add-on bought since", func(t *testing.T) {
		f := newBillingFixture(t)
		withAddonCatalogue(f)
		f.paidSeats(t, "seat", 3, end, 1, 49_000)
		o, err := f.svc.CreateOrder(ctx, seatsReq(2), "10003", "u1")
		require.NoError(t, err)
		add, err := f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "tenant", Kind: "addon", AddonAgentIDs: []string{"kt"}}, "10003", "u1")
		require.NoError(t, err)
		_, err = f.svc.ConfirmOrder(ctx, add.ID, "sa")
		require.NoError(t, err)
		_, err = f.svc.ConfirmOrder(ctx, o.ID, "sa")
		_, reason := errCodeReason(t, err)
		require.Equal(t, "subscription_changed", reason)
	})
	// the other way round: an add-on order priced for 3 seats is refused
	// once a seat order has made them 5
	t.Run("seats added before an add-on order", func(t *testing.T) {
		f := newBillingFixture(t)
		withAddonCatalogue(f)
		f.paidSeats(t, "seat", 3, end, 1, 49_000)
		add, err := f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "tenant", Kind: "addon", AddonAgentIDs: []string{"kt"}}, "10003", "u1")
		require.NoError(t, err)
		o, err := f.svc.CreateOrder(ctx, seatsReq(2), "10003", "u1")
		require.NoError(t, err)
		_, err = f.svc.ConfirmOrder(ctx, o.ID, "sa")
		require.NoError(t, err)
		_, err = f.svc.ConfirmOrder(ctx, add.ID, "sa")
		_, reason := errCodeReason(t, err)
		require.Equal(t, "subscription_changed", reason)
		require.Nil(t, f.right("sub-s", "kt"))
	})
}

// Review F5: a held add-on kept on a plan change to fewer seats credits the
// seats not carried over; the add-on itself is still carried.
func TestPlanChangeKeptAddonCreditsDroppedSeats(t *testing.T) {
	f := newBillingFixture(t)
	withAddonCatalogue(f)
	ctx := context.Background()
	end := endOfVN(2026, 11, 12) // 30 days after today
	f.paidSeats(t, "seat", 5, end, 1, 49_000)
	f.heldThue(t)

	// seat (5) → term (2 seats): "thue" costs nothing to carry → kept by
	// default, 3 seats × 99.000 × 30 / 30 credited
	q, err := f.svc.Quote(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "term", Months: 3, Seats: 2}, "10003")
	require.NoError(t, err)
	require.Len(t, q.HeldAddons, 1)
	h := q.HeldAddons[0]
	require.True(t, h.Kept)
	require.Equal(t, 3, h.DroppedUnits)
	require.Equal(t, int64(297_000), h.UnitsCreditVND)
	var units *types.UsageCreditLine
	for i := range q.CreditLines {
		if q.CreditLines[i].Kind == types.UsageCreditAddonUnits {
			units = &q.CreditLines[i]
		}
	}
	require.NotNil(t, units)
	require.Equal(t, "thue", units.AgentID)
	require.Equal(t, 3, units.Units)
	require.Equal(t, 30, units.Days)
	require.Equal(t, int64(297_000), units.AmountVND)
	require.Empty(t, q.CreditLines.DroppedAddons(), "kept, not dropped")
	plan := q.CreditLines[0]
	require.Equal(t, types.UsageCreditPlan, plan.Kind)
	require.Equal(t, int64(49_000*5), plan.AmountVND)
	require.Equal(t, plan.AmountVND+297_000, q.CreditLines.Total())

	// dropped instead: the whole add-on (5 seats) is credited, no units line
	none := []string{}
	q, err = f.svc.Quote(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "term", Months: 3, Seats: 2,
		KeepAddonAgentIDs: &none}, "10003")
	require.NoError(t, err)
	require.False(t, q.HeldAddons[0].Kept)
	require.Zero(t, q.HeldAddons[0].DroppedUnits)
	require.Equal(t, int64(495_000), q.HeldAddons[0].CreditVND)
	for _, c := range q.CreditLines {
		require.NotEqual(t, types.UsageCreditAddonUnits, c.Kind)
	}

	// bought again with the new plan: the dropped seats are credited too
	q, err = f.svc.Quote(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "term", Months: 3, Seats: 2,
		AddonAgentIDs: []string{"thue"}}, "10003")
	require.NoError(t, err)
	require.True(t, q.HeldAddons[0].BoughtAgain)
	require.Equal(t, 3, q.HeldAddons[0].DroppedUnits)

	// as many seats or more: nothing to credit
	q, err = f.svc.Quote(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "term", Months: 3, Seats: 5}, "10003")
	require.NoError(t, err)
	require.Zero(t, q.HeldAddons[0].DroppedUnits)

	// confirmed with the default: the add-on moves to the new package
	o, err := f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "term", Months: 3, Seats: 2}, "10003", "u1")
	require.NoError(t, err)
	_, err = f.svc.ConfirmOrder(ctx, o.ID, "sa")
	require.NoError(t, err)
	next := f.plans.active["tenant/10003"]
	require.Equal(t, "term", next.PlanID)
	r := f.right(next.ID, "thue")
	require.NotNil(t, r, "kept add-on carried over")
	require.Equal(t, end, *r.EndsAt)
}

func TestSeatOrderEmailLabel(t *testing.T) {
	o := &types.UsageOrderView{UsageOrder: &types.UsageOrder{Kind: types.UsageOrderKindSeats, PlanName: "Seat", Seats: 2,
		Mode: "seat", Addons: types.UsageOrderAddonLines{{AgentID: "thue", Name: "Thuế HKD", SeatsOnly: true}}}}
	require.Equal(t, "Seat · thêm 2 chỗ đến hết kỳ hiện tại · Trợ lý chuyên ngành tính thêm cho chỗ mới: Thuế HKD", orderWhatLabel(o))
}

// planCredit is the plan line of a plan change quote to the pool plan.
func planCredit(t *testing.T, f *billingFixture) types.UsageCreditLine {
	t.Helper()
	none := []string{}
	q, err := f.svc.Quote(context.Background(), types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "pool", Months: 12,
		KeepAddonAgentIDs: &none}, "10003")
	require.NoError(t, err)
	require.Equal(t, types.UsageCreditPlan, q.CreditLines[0].Kind)
	return q.CreditLines[0]
}

// Review M1: the plan-change credit values the package in tranches — the
// plan order's seats at its price and promo, each later seat order's at its
// own, seats nobody paid for (a grant) at nothing.
func TestPlanChangeCreditInSeatTranches(t *testing.T) {
	end := endOfVN(2026, 11, 12) // 30 days credited

	// seats bought with a 100 % code are not credited
	f := newBillingFixture(t)
	f.paidSeats(t, "seat", 3, end, 1, 49_000)
	f.promo(t, types.UsagePromoCode{Code: "FREE", DiscountType: types.UsagePromoPercent, DiscountValue: 100, IsActive: true})
	f.sneakSeatOrder(t, 2, "FREE")
	c := planCredit(t, f)
	require.Equal(t, 5, c.Units)
	require.True(t, c.Paid)
	require.Equal(t, int64(49_000*3), c.AmountVND, "3 × 49.000 × 30/30; the 2 free seats count 0 (not 245.000)")

	// the price changed between the plan order and the seat order
	g := newBillingFixture(t)
	g.paidSeats(t, "seat", 3, end, 1, 49_000)
	g.svc.plans.(*planCatalog).plans["seat"].PriceVND = 60_000
	so := g.sneakSeatOrder(t, 2, "")
	require.Equal(t, int64(60_000), so.UnitPriceVND)
	require.Equal(t, int64(49_000*3+60_000*2), planCredit(t, g).AmountVND)

	// a half-price seat order, then a SuperAdmin grant of 2 more seats
	h := newBillingFixture(t)
	h.paidSeats(t, "seat", 3, end, 1, 49_000)
	h.promo(t, types.UsagePromoCode{Code: "HALF", DiscountType: types.UsagePromoPercent, DiscountValue: 50, IsActive: true})
	h.sneakSeatOrder(t, 2, "HALF")
	h.plans.active["tenant/10003"].Seats = 7
	c = planCredit(t, h)
	require.Equal(t, 7, c.Units)
	require.Equal(t, int64(49_000*3+49_000), c.AmountVND, "2 seats at half price = 49.000, granted ones 0")

	// a seat order older than the last plan order is covered by that order
	k := newBillingFixture(t)
	k.paidSeats(t, "seat", 3, end, 1, 49_000)
	k.sneakSeatOrder(t, 2, "")
	ord := k.repo.orders["o-seat"]
	later := billingNow.Add(time.Hour)
	ord.PaidAt, ord.Seats = &later, 5
	require.Equal(t, int64(49_000*5), planCredit(t, k).AmountVND)
}

// Review M1 for F5: the add-on seats not carried over are taken newest
// first, each at the price its order paid.
func TestKeptAddonDroppedSeatsCreditInTranches(t *testing.T) {
	f := newBillingFixture(t)
	withAddonCatalogue(f)
	ctx := context.Background()
	end := endOfVN(2026, 11, 12)
	f.paidSeats(t, "seat", 3, end, 1, 49_000)
	f.repo.orders["o-seat"].Addons = types.UsageOrderAddonLines{{AgentID: "thue", Name: "Thuế HKD", UnitPriceVND: 99_000, Units: 3, Months: 1}}
	f.heldThue(t)
	f.promo(t, types.UsagePromoCode{Code: "FREE", DiscountType: types.UsagePromoPercent, DiscountValue: 100, IsActive: true})
	f.sneakSeatOrder(t, 2, "FREE") // thue for the 2 new seats, free

	// 5 → 2 seats: 3 seats not carried = the 2 free ones (0) + 1 paid (99.000)
	q, err := f.svc.Quote(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "term", Months: 3, Seats: 2}, "10003")
	require.NoError(t, err)
	require.True(t, q.HeldAddons[0].Kept)
	require.Equal(t, 3, q.HeldAddons[0].DroppedUnits)
	require.Equal(t, int64(99_000), q.HeldAddons[0].UnitsCreditVND)

	// dropped entirely: the 3 paid seats only (297.000), not 5 × 99.000
	none := []string{}
	q, err = f.svc.Quote(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "term", Months: 3, Seats: 2,
		KeepAddonAgentIDs: &none}, "10003")
	require.NoError(t, err)
	require.Equal(t, int64(297_000), q.HeldAddons[0].CreditVND)
}

// Review L1: an add-on held past the subscription's end is charged for the
// extra seats only up to that end, and the order still confirms.
func TestSeatOrderAddonStopsAtTheSubscriptionEnd(t *testing.T) {
	f := newBillingFixture(t)
	withAddonCatalogue(f)
	ctx := context.Background()
	end := endOfVN(2026, 11, 12)
	f.paidSeats(t, "seat", 3, end, 1, 49_000)
	f.heldThue(t)
	far := endOfVN(2027, 1, 31)
	f.right("sub-s", "thue").EndsAt = &far
	q, err := f.svc.Quote(ctx, seatsReq(2), "10003")
	require.NoError(t, err)
	require.Equal(t, end, *q.Addons[0].CoveredUntil)
	require.Equal(t, int64(198_000), q.Addons[0].AmountVND, "one full month, not 3+")
	o, err := f.svc.CreateOrder(ctx, seatsReq(2), "10003", "u1")
	require.NoError(t, err)
	_, err = f.svc.ConfirmOrder(ctx, o.ID, "sa")
	require.NoError(t, err)
	require.Equal(t, 5, f.plans.active["tenant/10003"].Seats)
}

// Review M2: the seats-only lines of a pending seat order do not count as
// buying the agent.
func TestPendingSeatOrderLinesAreNotAddonPurchases(t *testing.T) {
	f := newBillingFixture(t)
	withAddonCatalogue(f)
	ctx := context.Background()
	f.paidSeats(t, "seat", 3, endOfVN(2026, 11, 12), 1, 49_000)
	f.heldThue(t)
	_, err := f.svc.CreateOrder(ctx, seatsReq(2), "10003", "u1")
	require.NoError(t, err)
	require.NoError(t, f.svc.refusePendingAddons(ctx, "tenant", "10003",
		types.UsageOrderAddonLines{{AgentID: "thue"}}))
}

// Review L4: a renewal whose added seats were charged for a held add-on up
// to the end it had then is refused once an add-on order extended it.
func TestRenewalSeatLineAgainstAnExtendedAddonIsRefused(t *testing.T) {
	f := newBillingFixture(t)
	withAddonCatalogue(f)
	ctx := context.Background()
	end := endOfVN(2026, 11, 12)
	f.paidSeats(t, "seat", 3, end, 1, 49_000)
	f.heldThue(t)
	frozen := endOfVN(2026, 10, 31)
	f.right("sub-s", "thue").EndsAt = &frozen

	renew, err := f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "seat", Months: 1, Seats: 5}, "10003", "u1")
	require.NoError(t, err)
	require.Len(t, renew.Addons, 1)
	require.True(t, renew.Addons[0].SeatsOnly)
	require.Equal(t, frozen, *renew.Addons[0].CoveredUntil)

	// the agent bought mid-term to 12/11 for the 3 seats, confirmed first
	add, err := f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "tenant", Kind: "addon", AddonAgentIDs: []string{"thue"}}, "10003", "u1")
	require.NoError(t, err)
	_, err = f.svc.ConfirmOrder(ctx, add.ID, "sa")
	require.NoError(t, err)
	require.Equal(t, end, *f.right("sub-s", "thue").EndsAt)

	_, err = f.svc.ConfirmOrder(ctx, renew.ID, "sa")
	code, reason := errCodeReason(t, err)
	require.Equal(t, 409, code)
	require.Equal(t, "subscription_changed", reason)
	require.Equal(t, 3, f.plans.active["tenant/10003"].Seats)

	// priced again now, the renewal pays the 2 seats up to 12/11 and goes through
	again, err := f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "seat", Months: 1, Seats: 5, ConfirmDuplicate: true}, "10003", "u1")
	require.NoError(t, err)
	require.Equal(t, end, *again.Addons[0].CoveredUntil)
	_, err = f.svc.ConfirmOrder(ctx, again.ID, "sa")
	require.NoError(t, err)
}
