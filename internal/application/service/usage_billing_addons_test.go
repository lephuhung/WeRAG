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
	end := billingNow.AddDate(0, 2, 0)
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
	newEnd := billingNow.AddDate(0, 3, 0)
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
	require.Equal(t, billingNow.AddDate(0, 1, 0), *f.right(sub.ID, "thue").EndsAt,
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
	paidEnd := billingNow.AddDate(0, 3, 0)

	// to the pool plan, buying "thue" again for 1 month: "thue" follows the
	// new subscription; "kt" (not sold with pool) is kept to its paid end
	o2, err := f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "pool", Months: 1,
		AddonAgentIDs: []string{"thue"}}, "10003", "u1")
	require.NoError(t, err)
	_, err = f.svc.ConfirmOrder(ctx, o2.ID, "sa")
	require.NoError(t, err)
	next := f.plans.active["tenant/10003"]
	require.Equal(t, "pool", next.PlanID)
	require.Equal(t, billingNow.AddDate(0, 1, 0), *f.right(next.ID, "thue").EndsAt)
	kt := f.right(next.ID, "kt")
	require.NotNil(t, kt, "carried over")
	require.Equal(t, paidEnd, *kt.EndsAt)
	require.Equal(t, o.ID, *kt.OrderID)
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
	end := billingNow.AddDate(0, 2, 10)
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
