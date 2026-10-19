package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
)

// withAddonCatalogue sells "thue" (99.000 VNĐ/month) with every plan and
// "kt" (50.000) with the seat plan only.
func withAddonCatalogue(f *billingFixture) {
	f.plans.catalogue = []*types.UsageAgentAddon{
		{AgentID: "thue", AgentName: "Thuế HKD", PriceVND: 99_000, IsActive: true, AgentAvailable: true},
		{AgentID: "kt", AgentName: "Kế toán", PriceVND: 50_000, PlanIDs: types.UsagePlanIDList{"seat"}, IsActive: true, AgentAvailable: true},
		{AgentID: "off", AgentName: "Tắt", PriceVND: 1, IsActive: false, AgentAvailable: true},
	}
}

func (f *billingFixture) right(subID, agentID string) *types.UsageSubscriptionAddon {
	return f.plans.addons[subID+"/"+agentID]
}

func badRequestReason(t *testing.T, err error) string {
	t.Helper()
	appErr, ok := err.(*apperrors.AppError)
	require.True(t, ok, "%v", err)
	require.Equal(t, 400, appErr.HTTPCode)
	if d, ok := appErr.Details.(map[string]string); ok {
		return d["reason"]
	}
	return ""
}

func TestQuotePlanWithAddons(t *testing.T) {
	f := newBillingFixture(t)
	withAddonCatalogue(f)
	ctx := context.Background()

	// seat plan: add-on × months × seats (decision 7.2)
	q, err := f.svc.Quote(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "seat", Months: 3, Seats: 5,
		AddonAgentIDs: []string{"thue", "kt", "thue"}}, "10003")
	require.NoError(t, err)
	require.Len(t, q.Addons, 2, "duplicates dropped")
	require.Equal(t, int64(99_000*3*5), q.Addons[0].AmountVND)
	require.Equal(t, 5, q.Addons[0].Units)
	require.Equal(t, int64(99_000*15+50_000*15), q.AddonsVND)
	require.Equal(t, int64(49_000*3*5)+q.AddonsVND, q.SubtotalVND)
	require.Equal(t, q.SubtotalVND, q.TotalVND)

	// pool plan: once for the whole Tổ chức; personal plans too (7.5)
	q, err = f.svc.Quote(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "pool", Months: 2,
		AddonAgentIDs: []string{"thue"}}, "10003")
	require.NoError(t, err)
	require.Equal(t, int64(100_000*2+99_000*2), q.SubtotalVND)
	q, err = f.svc.Quote(ctx, types.UsageQuoteRequest{ScopeType: "user", PlanID: "me", Months: 1,
		AddonAgentIDs: []string{"thue"}}, "u1")
	require.NoError(t, err)
	require.Equal(t, int64(20_000+99_000), q.TotalVND)

	// not sold with this plan, switched off, unknown
	for _, id := range []string{"kt", "off", "ghost"} {
		_, err = f.svc.Quote(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "pool", Months: 1,
			AddonAgentIDs: []string{id}}, "10003")
		require.Equal(t, "addon_not_sold", badRequestReason(t, err), id)
	}

	// a promo code discounts the whole subtotal, add-ons included
	f.promo(t, types.UsagePromoCode{Code: "GIAM10", DiscountType: types.UsagePromoPercent, DiscountValue: 10, IsActive: true})
	q, err = f.svc.Quote(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "pool", Months: 1,
		AddonAgentIDs: []string{"thue"}, PromoCode: "giam10"}, "10003")
	require.NoError(t, err)
	require.Equal(t, int64(199_000), q.SubtotalVND)
	require.Equal(t, int64(19_900), q.DiscountVND)
	require.Equal(t, int64(179_100), q.TotalVND)
}

func TestConfirmPlanOrderGrantsAddonsAndRenewalExtendsThem(t *testing.T) {
	f := newBillingFixture(t)
	withAddonCatalogue(f)
	ctx := context.Background()

	o, err := f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "seat", Months: 2, Seats: 3,
		AddonAgentIDs: []string{"thue", "kt"}}, "10003", "u1")
	require.NoError(t, err)
	require.Equal(t, types.UsageOrderKindPlan, o.Kind)
	require.Len(t, o.Addons, 2, "frozen add-on lines on the order")
	_, err = f.svc.ConfirmOrder(ctx, o.ID, "sa")
	require.NoError(t, err)
	sub := f.plans.active["tenant/10003"]
	end := vnEnd(2)
	r := f.right(sub.ID, "thue")
	require.NotNil(t, r)
	require.Equal(t, end, *r.EndsAt, "ends with the subscription")
	require.Equal(t, o.ID, *r.OrderID)
	require.Equal(t, int64(99_000), r.PriceVND)

	// renewal of the same plan with "thue" only: "thue" follows the new
	// end, "kt" keeps the end it was paid to
	o2, err := f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "seat", Months: 1, Seats: 3,
		AddonAgentIDs: []string{"thue"}}, "10003", "u1")
	require.NoError(t, err)
	require.Equal(t, int64(49_000*3+99_000*3), o2.TotalVND)
	_, err = f.svc.ConfirmOrder(ctx, o2.ID, "sa")
	require.NoError(t, err)
	newEnd := vnEnd(3)
	require.Equal(t, newEnd, *f.right(sub.ID, "thue").EndsAt)
	require.Equal(t, billingNow, f.right(sub.ID, "thue").StartsAt, "an extended right keeps its start")
	require.Equal(t, end, *f.right(sub.ID, "kt").EndsAt)
}

func TestRenewalFreezesOpenEndedAddonItDoesNotPayFor(t *testing.T) {
	f := newBillingFixture(t)
	withAddonCatalogue(f)
	ctx := context.Background()
	o, err := f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "pool", Months: 1}, "10003", "u1")
	require.NoError(t, err)
	_, err = f.svc.ConfirmOrder(ctx, o.ID, "sa")
	require.NoError(t, err)
	sub := f.plans.active["tenant/10003"]
	// a SuperAdmin grant without an end
	require.NoError(t, f.plans.UpsertSubscriptionAddon(ctx, &types.UsageSubscriptionAddon{
		SubscriptionID: sub.ID, AgentID: "thue", StartsAt: billingNow}))
	o2, err := f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "pool", Months: 1}, "10003", "u1")
	require.NoError(t, err)
	_, err = f.svc.ConfirmOrder(ctx, o2.ID, "sa")
	require.NoError(t, err)
	require.Equal(t, vnEnd(1), *f.right(sub.ID, "thue").EndsAt,
		"not extended for free by the renewal")
}

func TestPlanChangeCarriesAddonsToTheirPaidEnd(t *testing.T) {
	f := newBillingFixture(t)
	withAddonCatalogue(f)
	ctx := context.Background()
	o, err := f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "seat", Months: 3, Seats: 2,
		AddonAgentIDs: []string{"thue", "kt"}}, "10003", "u1")
	require.NoError(t, err)
	_, err = f.svc.ConfirmOrder(ctx, o.ID, "sa")
	require.NoError(t, err)
	paidEnd := vnEnd(3)

	// to the pool plan for 1 month, ticking "thue" again: it is already paid
	// to +3 months, beyond the new period, so the line is refused
	_, err = f.svc.Quote(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "pool", Months: 1,
		AddonAgentIDs: []string{"thue"}}, "10003")
	require.Equal(t, "addon_held", badRequestReason(t, err))

	// for 6 months: only the 3 months after the paid end are charged
	// (pool plan: one unit; the held right was for 2 seats)
	o2, err := f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "pool", Months: 6,
		AddonAgentIDs: []string{"thue"}}, "10003", "u1")
	require.NoError(t, err)
	line := o2.Addons[0]
	require.Equal(t, paidEnd, *line.CoveredUntil)
	require.Equal(t, 3, line.Months)
	require.Equal(t, int64(99_000*3), line.AmountVND)
	require.Zero(t, line.ExtraUnits, "fewer units than held: nothing extra")
	_, err = f.svc.ConfirmOrder(ctx, o2.ID, "sa")
	require.NoError(t, err)
	next := f.plans.active["tenant/10003"]
	require.Equal(t, "pool", next.PlanID)
	require.Equal(t, vnEnd(6), *f.right(next.ID, "thue").EndsAt)
	// "kt" (not sold with pool) is carried to its paid end
	kt := f.right(next.ID, "kt")
	require.NotNil(t, kt, "carried over")
	require.Equal(t, paidEnd, *kt.EndsAt)
	require.Equal(t, o.ID, *kt.OrderID)
}

// Money paths of a plan order that renews or replaces the current
// subscription (review items 1, 2, 6): only time not yet paid is charged.
func TestQuoteAddonLinesAgainstWhatIsHeld(t *testing.T) {
	type held struct {
		end  *time.Time // nil: with the subscription
		none bool
	}
	at := func(m, d int) *time.Time { v := types.EndOfDayVN(billingNow.AddDate(0, m, d)); return &v }
	cases := []struct {
		name      string
		subPlan   string
		subSeats  int
		subEnd    *time.Time
		held      held
		req       types.UsageQuoteRequest
		want      types.UsageOrderAddonLine
		heldError bool
	}{
		{
			name:    "renewal + new add-on pays the rest of the current period too",
			subPlan: "seat", subSeats: 3, subEnd: at(11, 0), held: held{none: true},
			req: types.UsageQuoteRequest{PlanID: "seat", Months: 1, Seats: 3},
			want: types.UsageOrderAddonLine{Units: 3, Months: 1, GapMonths: 11, GapVND: 99_000 * 3 * 11,
				AmountVND: 99_000 * 3 * 12},
		},
		{
			name:    "renewal of a right that ends early: the uncovered gap (2 months 11 days → ½)",
			subPlan: "pool", subEnd: at(5, 0), held: held{end: at(2, 20)},
			req: types.UsageQuoteRequest{PlanID: "pool", Months: 1},
			want: types.UsageOrderAddonLine{Units: 1, Months: 1, CoveredUntil: at(2, 20), GapMonths: 2,
				GapLeftoverDays: 11, GapLeftoverFactor: types.UsageAddonLeftoverHalf,
				GapVND: 99_000*2 + 49_500, AmountVND: 99_000*2 + 49_500 + 99_000},
		},
		{
			name:    "renewal of a held right: just the renewal months",
			subPlan: "pool", subEnd: at(2, 0), held: held{},
			req:  types.UsageQuoteRequest{PlanID: "pool", Months: 3},
			want: types.UsageOrderAddonLine{Units: 1, Months: 3, CoveredUntil: at(2, 0), AmountVND: 99_000 * 3},
		},
		{
			name:    "seats added at renewal pay the added seats for the rest of the period (2 months 10 days)",
			subPlan: "seat", subSeats: 3, subEnd: at(2, 10), held: held{},
			req: types.UsageQuoteRequest{PlanID: "seat", Months: 1, Seats: 5},
			want: types.UsageOrderAddonLine{Units: 5, Months: 1, CoveredUntil: at(2, 10), ExtraUnits: 2,
				ExtraUnitsVND: 99_000*2*2 + 99_000, AmountVND: 99_000*5 + 99_000*2*2 + 99_000},
		},
		{
			name:    "plan change: only after the carried paid end",
			subPlan: "seat", subSeats: 1, subEnd: at(3, 0), held: held{},
			req:  types.UsageQuoteRequest{PlanID: "pool", Months: 6},
			want: types.UsageOrderAddonLine{Units: 1, Months: 3, CoveredUntil: at(3, 0), AmountVND: 99_000 * 3},
		},
		{
			name:    "plan change to more seats also pays the added seats while the carried right runs",
			subPlan: "pool", subEnd: at(1, 0), held: held{},
			req: types.UsageQuoteRequest{PlanID: "seat", Months: 2, Seats: 4},
			want: types.UsageOrderAddonLine{Units: 4, Months: 1, CoveredUntil: at(1, 0), ExtraUnits: 3,
				ExtraUnitsVND: 99_000 * 3, AmountVND: 99_000*4 + 99_000*3},
		},
		{
			name:    "plan change inside the carried period: refused",
			subPlan: "seat", subSeats: 1, subEnd: at(3, 0), held: held{},
			req: types.UsageQuoteRequest{PlanID: "pool", Months: 1}, heldError: true,
		},
		{
			name: "no subscription: plain months",
			held: held{none: true},
			req:  types.UsageQuoteRequest{PlanID: "pool", Months: 2},
			want: types.UsageOrderAddonLine{Units: 1, Months: 2, AmountVND: 99_000 * 2},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newBillingFixture(t)
			withAddonCatalogue(f)
			if c.subPlan != "" {
				mode := "pool"
				if c.subPlan == "seat" {
					mode = "seat"
				}
				f.plans.active["tenant/10003"] = &types.UsageSubscription{ID: "sub-1", ScopeType: "tenant", ScopeID: "10003",
					PlanID: c.subPlan, Mode: mode, Seats: c.subSeats, StartsAt: billingNow.AddDate(0, -1, 0),
					EndsAt: c.subEnd, Status: types.UsageSubscriptionActive}
				if !c.held.none {
					require.NoError(t, f.plans.UpsertSubscriptionAddon(context.Background(), &types.UsageSubscriptionAddon{
						SubscriptionID: "sub-1", AgentID: "thue", StartsAt: billingNow.AddDate(0, -1, 0), EndsAt: c.held.end}))
				}
			}
			req := c.req
			req.ScopeType = "tenant"
			req.AddonAgentIDs = []string{"thue"}
			q, err := f.svc.Quote(context.Background(), req, "10003")
			if c.heldError {
				require.Equal(t, "addon_held", badRequestReason(t, err))
				return
			}
			require.NoError(t, err)
			require.Len(t, q.Addons, 1)
			got := q.Addons[0]
			c.want.AgentID, c.want.Name, c.want.UnitPriceVND = "thue", "Thuế HKD", 99_000
			if c.subPlan != "" && c.subPlan == c.req.PlanID {
				// a renewal line records the end it was priced against
				c.want.SubscriptionEnd = c.subEnd
			}
			require.Equal(t, c.want, got)
			require.Equal(t, got.AmountVND, q.AddonsVND)
		})
	}
}

func TestMidTermAddonPurchase(t *testing.T) {
	f := newBillingFixture(t)
	withAddonCatalogue(f)
	ctx := context.Background()

	// no subscription yet
	_, err := f.svc.Quote(ctx, types.UsageQuoteRequest{ScopeType: "tenant", Kind: "addon",
		AddonAgentIDs: []string{"thue"}}, "10003")
	require.Equal(t, "no_subscription", badRequestReason(t, err))

	// a 4-seat subscription with 2 months and 10 days left
	end := types.EndOfDayVN(billingNow.AddDate(0, 2, 10))
	f.plans.active["tenant/10003"] = &types.UsageSubscription{ID: "sub-1", ScopeType: "tenant", ScopeID: "10003",
		PlanID: "seat", Mode: "seat", Seats: 4, StartsAt: billingNow.AddDate(0, -1, 0), EndsAt: &end,
		Status: types.UsageSubscriptionActive}

	q, err := f.svc.Quote(ctx, types.UsageQuoteRequest{ScopeType: "tenant", Kind: "addon",
		AddonAgentIDs: []string{"thue"}}, "10003")
	require.NoError(t, err)
	require.Equal(t, types.UsageOrderKindAddon, q.Kind)
	line := q.Addons[0]
	require.Equal(t, 2, line.Months)
	require.Equal(t, 10, line.LeftoverDays)
	require.Equal(t, types.UsageAddonLeftoverHalf, line.LeftoverFactor)
	require.Equal(t, 4, line.Units)
	require.Equal(t, int64(99_000*4*2+99_000*4/2), line.AmountVND)
	require.Equal(t, line.AmountVND, q.TotalVND)

	o, err := f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "tenant", Kind: "addon",
		AddonAgentIDs: []string{"thue"}}, "10003", "u1")
	require.NoError(t, err)
	require.Equal(t, "sub-1", *o.TargetSubscriptionID)
	// the subscription is renewed before the transfer arrives: the right
	// still ends where it was paid to
	longer := end.AddDate(0, 1, 0)
	f.plans.active["tenant/10003"].EndsAt = &longer
	_, err = f.svc.ConfirmOrder(ctx, o.ID, "sa")
	require.NoError(t, err)
	r := f.right("sub-1", "thue")
	require.Equal(t, end, *r.EndsAt)
	require.Equal(t, longer, *f.plans.active["tenant/10003"].EndsAt, "the subscription itself is untouched")

	// held to the (old) end: buying again prices only from there
	q, err = f.svc.Quote(ctx, types.UsageQuoteRequest{ScopeType: "tenant", Kind: "addon",
		AddonAgentIDs: []string{"thue"}}, "10003")
	require.NoError(t, err)
	require.Equal(t, 1, q.Addons[0].Months)
	require.Equal(t, 0, q.Addons[0].LeftoverDays)
	// held to the end: refused
	f.plans.active["tenant/10003"].EndsAt = &end
	_, err = f.svc.Quote(ctx, types.UsageQuoteRequest{ScopeType: "tenant", Kind: "addon",
		AddonAgentIDs: []string{"thue"}}, "10003")
	require.Equal(t, "addon_held", badRequestReason(t, err))
}

func TestMidTermOrderForAReplacedSubscriptionIsRefused(t *testing.T) {
	f := newBillingFixture(t)
	withAddonCatalogue(f)
	ctx := context.Background()
	end := billingNow.AddDate(0, 1, 0)
	f.plans.active["user/u1"] = &types.UsageSubscription{ID: "sub-me", ScopeType: "user", ScopeID: "u1",
		PlanID: "me", Mode: "pool", StartsAt: billingNow.AddDate(0, -1, 0), EndsAt: &end,
		Status: types.UsageSubscriptionActive}
	o, err := f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "user", Kind: "addon",
		AddonAgentIDs: []string{"thue"}}, "u1", "u1")
	require.NoError(t, err)
	require.Equal(t, int64(99_000), o.TotalVND, "personal plans buy add-ons too, one unit")
	f.plans.active["user/u1"] = &types.UsageSubscription{ID: "sub-other", ScopeType: "user", ScopeID: "u1",
		PlanID: "me", Mode: "pool", StartsAt: billingNow, EndsAt: &end, Status: types.UsageSubscriptionActive}
	_, err = f.svc.ConfirmOrder(ctx, o.ID, "sa")
	require.Error(t, err)
	require.Nil(t, f.right("sub-other", "thue"))
}

func TestPaymentWebhookGrantsOrderAddons(t *testing.T) {
	f := newBillingFixture(t)
	withAddonCatalogue(f)
	o, err := f.svc.CreateOrder(context.Background(), types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "pool",
		Months: 1, AddonAgentIDs: []string{"thue"}}, "10003", "u1")
	require.NoError(t, err)
	res := f.pay(t, types.UsagePaymentWebhookRequest{TransactionID: "FT9", AmountVND: o.TotalVND,
		Content: "CK " + o.OrderCode})
	require.Equal(t, types.UsagePaymentMatched, res.Transaction.Status)
	sub := f.plans.active["tenant/10003"]
	require.NotNil(t, f.right(sub.ID, "thue"))
}

func TestScopeAddonsForTheBuyPage(t *testing.T) {
	f := newBillingFixture(t)
	withAddonCatalogue(f)
	ctx := context.Background()
	out, err := f.svc.ScopeAddons(ctx, "tenant", "10003")
	require.NoError(t, err)
	require.Nil(t, out.Subscription)
	require.Empty(t, out.Purchasable)

	o, err := f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "seat", Months: 1, Seats: 2,
		AddonAgentIDs: []string{"thue"}}, "10003", "u1")
	require.NoError(t, err)
	_, err = f.svc.ConfirmOrder(ctx, o.ID, "sa")
	require.NoError(t, err)
	out, err = f.svc.ScopeAddons(ctx, "tenant", "10003")
	require.NoError(t, err)
	require.NotNil(t, out.Subscription)
	require.Len(t, out.Held, 1)
	require.Equal(t, "thue", out.Held[0].AgentID)
	require.Len(t, out.Purchasable, 1, "held to the end: not offered again")
	require.Equal(t, "kt", out.Purchasable[0].AgentID)
	require.True(t, out.Purchasable[0].PerSeat)
	_ = time.Now
}

// Review item 2: a right is never shortened, an order never pays twice for
// the same time.
func TestAddonRightsAreNeverShortenedOrPaidTwice(t *testing.T) {
	ctx := context.Background()
	end := billingNow.AddDate(0, 2, 0)
	newSub := func(f *billingFixture) {
		f.plans.active["tenant/10003"] = &types.UsageSubscription{ID: "sub-1", ScopeType: "tenant", ScopeID: "10003",
			PlanID: "pool", Mode: "pool", StartsAt: billingNow.AddDate(0, -1, 0), EndsAt: &end,
			Status: types.UsageSubscriptionActive}
	}
	addonReq := types.UsageQuoteRequest{ScopeType: "tenant", Kind: "addon", AddonAgentIDs: []string{"thue"}}

	t.Run("a second pending order for the same add-on is refused", func(t *testing.T) {
		f := newBillingFixture(t)
		withAddonCatalogue(f)
		newSub(f)
		first, err := f.svc.CreateOrder(ctx, addonReq, "10003", "u1")
		require.NoError(t, err)
		_, err = f.svc.CreateOrder(ctx, addonReq, "10003", "u2")
		require.Equal(t, 409, appErrHTTP(t, err))
		// nor a renewal that buys it again
		_, err = f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "pool", Months: 1,
			AddonAgentIDs: []string{"thue"}}, "10003", "u2")
		require.Equal(t, 409, appErrHTTP(t, err))
		// a renewal without it, or another add-on, is fine
		_, err = f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "pool", Months: 1}, "10003", "u2")
		require.NoError(t, err)
		// once the first is cancelled, it can be ordered again
		_, err = f.svc.CancelOrder(ctx, first.ID)
		require.NoError(t, err)
		_, err = f.svc.CreateOrder(ctx, addonReq, "10003", "u1")
		require.NoError(t, err)
	})

	t.Run("confirming an order whose add-on is already covered is refused", func(t *testing.T) {
		f := newBillingFixture(t)
		withAddonCatalogue(f)
		newSub(f)
		o, err := f.svc.CreateOrder(ctx, addonReq, "10003", "u1")
		require.NoError(t, err)
		// meanwhile a SuperAdmin grants it to the end by hand
		require.NoError(t, f.plans.UpsertSubscriptionAddon(ctx, &types.UsageSubscriptionAddon{
			SubscriptionID: "sub-1", AgentID: "thue", StartsAt: billingNow, EndsAt: &end}))
		_, err = f.svc.ConfirmOrder(ctx, o.ID, "sa")
		require.Equal(t, 409, appErrHTTP(t, err))
		// paid through the webhook: kept as confirm_failed for a refund
		res := f.pay(t, types.UsagePaymentWebhookRequest{TransactionID: "FT2", AmountVND: o.TotalVND, Content: o.OrderCode})
		require.Equal(t, types.UsagePaymentConfirmFailed, res.Transaction.Status)
	})

	t.Run("a mid-term order confirmed after a longer right does not pull it back", func(t *testing.T) {
		f := newBillingFixture(t)
		withAddonCatalogue(f)
		newSub(f)
		o, err := f.svc.CreateOrder(ctx, addonReq, "10003", "u1")
		require.NoError(t, err)
		// the right is meanwhile granted from now to the end minus a day,
		// then the subscription is renewed and the right extended past
		// the order's paid_until by a merge
		later := end.AddDate(0, 1, 0)
		short := end.AddDate(0, 0, -1)
		require.NoError(t, f.plans.UpsertSubscriptionAddon(ctx, &types.UsageSubscriptionAddon{
			SubscriptionID: "sub-1", AgentID: "thue", StartsAt: billingNow, EndsAt: &short}))
		_, err = f.svc.ConfirmOrder(ctx, o.ID, "sa")
		require.NoError(t, err, "not yet covered to paid_until: the order applies")
		require.Equal(t, end, *f.right("sub-1", "thue").EndsAt)
		require.NoError(t, f.plans.UpsertSubscriptionAddon(ctx, &types.UsageSubscriptionAddon{
			SubscriptionID: "sub-1", AgentID: "thue", StartsAt: billingNow, EndsAt: &later}))
		require.NoError(t, f.plans.UpsertSubscriptionAddon(ctx, &types.UsageSubscriptionAddon{
			SubscriptionID: "sub-1", AgentID: "thue", StartsAt: billingNow, EndsAt: &end}))
		require.Equal(t, later, *f.right("sub-1", "thue").EndsAt, "an earlier end never shortens a right")
	})
}

func appErrHTTP(t *testing.T, err error) int {
	t.Helper()
	appErr, ok := err.(*apperrors.AppError)
	require.True(t, ok, "%v", err)
	return appErr.HTTPCode
}

// User decision 5: a valid promo code applies to an add-on-only order too,
// with its use counted like any order's.
func TestPromoCodeOnAddonOrder(t *testing.T) {
	f := newBillingFixture(t)
	withAddonCatalogue(f)
	ctx := context.Background()
	end := types.EndOfDayVN(billingNow.AddDate(0, 1, 0))
	f.plans.active["tenant/10003"] = &types.UsageSubscription{ID: "sub-1", ScopeType: "tenant", ScopeID: "10003",
		PlanID: "pool", Mode: "pool", StartsAt: billingNow.AddDate(0, -1, 0), EndsAt: &end,
		Status: types.UsageSubscriptionActive}
	p := f.promo(t, types.UsagePromoCode{Code: "MOT", DiscountType: types.UsagePromoPercent, DiscountValue: 50,
		MaxUses: 1, IsActive: true})
	req := types.UsageQuoteRequest{ScopeType: "tenant", Kind: "addon", AddonAgentIDs: []string{"thue"}, PromoCode: "mot"}

	q, err := f.svc.Quote(ctx, req, "10003")
	require.NoError(t, err)
	require.Equal(t, q.SubtotalVND/2, q.DiscountVND)

	o, err := f.svc.CreateOrder(ctx, req, "10003", "u1")
	require.NoError(t, err)
	require.Equal(t, "MOT", o.PromoCode)
	require.Equal(t, 1, f.repo.promos[p.ID].UsedCount, "the single use is taken")
	// used up: a plan order cannot take it now (quote notes why)
	q, err = f.svc.Quote(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "pool", Months: 1, PromoCode: "MOT"}, "10003")
	require.NoError(t, err)
	require.NotEmpty(t, q.PromoError)
	// cancelling gives it back
	_, err = f.svc.CancelOrder(ctx, o.ID)
	require.NoError(t, err)
	require.Equal(t, 0, f.repo.promos[p.ID].UsedCount)
	q, err = f.svc.Quote(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "pool", Months: 1, PromoCode: "MOT"}, "10003")
	require.NoError(t, err)
	require.Empty(t, q.PromoError)
}

// endOfVN is 24:00 Vietnam time of the given date (as stored).
func endOfVN(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d+1, 0, 0, 0, 0, time.FixedZone("ICT", 7*3600)).UTC()
}

// Review R1: renewing from a stored end keeps the date (end of 30/10 + 1
// month = end of 30/11) and prices an add-on for exactly one month.
func TestRenewalFromAStoredEndKeepsTheDate(t *testing.T) {
	f := newBillingFixture(t)
	withAddonCatalogue(f)
	ctx := context.Background()
	end := endOfVN(2026, 10, 30)
	f.plans.active["tenant/10003"] = &types.UsageSubscription{ID: "sub-1", ScopeType: "tenant", ScopeID: "10003",
		PlanID: "pool", Mode: "pool", StartsAt: billingNow.AddDate(0, -1, 0), EndsAt: &end,
		Status: types.UsageSubscriptionActive}
	require.NoError(t, f.plans.UpsertSubscriptionAddon(ctx, &types.UsageSubscriptionAddon{
		SubscriptionID: "sub-1", AgentID: "thue", StartsAt: billingNow, EndsAt: &end}))

	o, err := f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "pool", Months: 1,
		AddonAgentIDs: []string{"thue"}}, "10003", "u1")
	require.NoError(t, err)
	require.Equal(t, int64(99_000), o.Addons[0].AmountVND, "exactly one month, no leftover")
	_, err = f.svc.ConfirmOrder(ctx, o.ID, "sa")
	require.NoError(t, err)
	want := endOfVN(2026, 11, 30)
	require.Equal(t, want, *f.plans.active["tenant/10003"].EndsAt)
	require.Equal(t, want, *f.right("sub-1", "thue").EndsAt)

	// a right held to the end of 30/10 bought again mid-term up to the end
	// of 30/11 is one month too
	require.NoError(t, f.plans.UpsertSubscriptionAddon(ctx, &types.UsageSubscriptionAddon{
		SubscriptionID: "sub-1", AgentID: "kt", StartsAt: billingNow, EndsAt: &end}))
	f.plans.active["tenant/10003"].PlanID, f.plans.active["tenant/10003"].Mode = "seat", "seat"
	f.plans.active["tenant/10003"].Seats = 1
	q, err := f.svc.Quote(ctx, types.UsageQuoteRequest{ScopeType: "tenant", Kind: "addon",
		AddonAgentIDs: []string{"kt"}}, "10003")
	require.NoError(t, err)
	require.Equal(t, 1, q.Addons[0].Months)
	require.Equal(t, 0, q.Addons[0].LeftoverDays)
	require.Equal(t, int64(50_000), q.Addons[0].AmountVND)
}

// Review R3: two pending renewals, one with an add-on: whichever is
// confirmed second was priced against an end that has moved → 409.
func TestRenewalPricedAgainstAMovedEndIsRefused(t *testing.T) {
	f := newBillingFixture(t)
	withAddonCatalogue(f)
	ctx := context.Background()
	end := endOfVN(2026, 12, 31)
	f.plans.active["tenant/10003"] = &types.UsageSubscription{ID: "sub-1", ScopeType: "tenant", ScopeID: "10003",
		PlanID: "pool", Mode: "pool", StartsAt: billingNow.AddDate(0, -1, 0), EndsAt: &end,
		Status: types.UsageSubscriptionActive}
	withAddon, err := f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "pool", Months: 1,
		AddonAgentIDs: []string{"thue"}}, "10003", "u1")
	require.NoError(t, err)
	require.Equal(t, end, *withAddon.Addons[0].SubscriptionEnd)
	plain, err := f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "pool", Months: 1}, "10003", "u2")
	require.NoError(t, err)
	_, err = f.svc.ConfirmOrder(ctx, plain.ID, "sa")
	require.NoError(t, err)
	_, err = f.svc.ConfirmOrder(ctx, withAddon.ID, "sa")
	require.Equal(t, 409, appErrHTTP(t, err))
	require.Nil(t, f.right("sub-1", "thue"), "no add-on for a period it did not pay for")
	res := f.pay(t, types.UsagePaymentWebhookRequest{TransactionID: "FT3", AmountVND: withAddon.TotalVND, Content: withAddon.OrderCode})
	require.Equal(t, types.UsagePaymentConfirmFailed, res.Transaction.Status)
}

// Review R3/6: a renewal adding seats pays the added seats on a held
// add-on it does not buy again ("seats only"), without extending it.
func TestSeatIncreaseChargesHeldAddonNotBoughtAgain(t *testing.T) {
	f := newBillingFixture(t)
	withAddonCatalogue(f)
	ctx := context.Background()
	end := endOfVN(2026, 12, 23) // 2 months + 10 dates after 13/10 17:00 VN
	held := endOfVN(2026, 12, 23)
	f.plans.active["tenant/10003"] = &types.UsageSubscription{ID: "sub-1", ScopeType: "tenant", ScopeID: "10003",
		PlanID: "seat", Mode: "seat", Seats: 3, StartsAt: billingNow.AddDate(0, -1, 0), EndsAt: &end,
		Status: types.UsageSubscriptionActive}
	require.NoError(t, f.plans.UpsertSubscriptionAddon(ctx, &types.UsageSubscriptionAddon{
		SubscriptionID: "sub-1", AgentID: "thue", StartsAt: billingNow, EndsAt: &held, PriceVND: 99_000}))

	o, err := f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "seat", Months: 1, Seats: 5},
		"10003", "u1")
	require.NoError(t, err)
	require.Len(t, o.Addons, 1)
	line := o.Addons[0]
	require.True(t, line.SeatsOnly)
	require.Equal(t, 2, line.ExtraUnits)
	require.Equal(t, int64(99_000*2*2+99_000), line.AmountVND, "2 seats × (2 months + ½)")
	_, err = f.svc.ConfirmOrder(ctx, o.ID, "sa")
	require.NoError(t, err)
	require.Equal(t, held, *f.right("sub-1", "thue").EndsAt, "the right is not extended by a seats-only line")
}
