package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
)

func (m *memPlanRepo) ListSeats(_ context.Context, subID string) ([]*types.UsageSeat, error) {
	out := []*types.UsageSeat{}
	for _, s := range m.seats[subID] {
		c := *s
		out = append(out, &c)
	}
	return out, nil
}

func (m *memPlanRepo) CountSeats(_ context.Context, subID string) (int64, error) {
	return int64(len(m.seats[subID])), nil
}

func (m *memPlanRepo) AddSeat(_ context.Context, s *types.UsageSeat) error {
	if m.seats == nil {
		m.seats = map[string][]*types.UsageSeat{}
	}
	c := *s
	m.seats[s.SubscriptionID] = append(m.seats[s.SubscriptionID], &c)
	return nil
}

// paidPool gives tenant 10003 a pool subscription ending at end, paid by an
// order at unit/month with discount off subtotal.
func (f *billingFixture) paidPool(t *testing.T, end time.Time, unit, subtotal, discount int64) {
	t.Helper()
	f.plans.active["tenant/10003"] = &types.UsageSubscription{ID: "sub-1", ScopeType: "tenant", ScopeID: "10003",
		PlanID: "pool", Mode: "pool", StartsAt: billingNow.AddDate(0, -1, 0), EndsAt: &end,
		Status: types.UsageSubscriptionActive}
	subID, paid := "sub-1", billingNow.AddDate(0, -1, 0)
	require.NoError(t, f.repo.CreateOrder(context.Background(), &types.UsageOrder{
		ID: "o-old", OrderCode: "WROLD", ScopeType: "tenant", ScopeID: "10003", PlanID: "pool", Kind: "plan",
		Months: 1, UnitPriceVND: unit, SubtotalVND: subtotal, DiscountVND: discount, TotalVND: subtotal - discount,
		Status: types.UsageOrderPaid, PaidAt: &paid, SubscriptionID: &subID,
	}))
}

// H2: another plan bought while the current one runs credits its unused
// paid days (whole days after today, at the price paid, after the promo).
func TestPlanChangeCreditsUnusedPaidDays(t *testing.T) {
	f := newBillingFixture(t)
	ctx := context.Background()
	end := endOfVN(2026, 11, 12) // billingNow is 13/10 17:00 VN → 30 days after today
	f.paidPool(t, end, 100_000, 100_000, 0)

	q, err := f.svc.Quote(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "seat", Months: 3, Seats: 2}, "10003")
	require.NoError(t, err)
	require.NotNil(t, q.Replaces)
	require.Equal(t, "Pool", q.Replaces.PlanName)
	require.Len(t, q.CreditLines, 1)
	require.Equal(t, 30, q.CreditLines[0].Days)
	require.True(t, q.CreditLines[0].Paid)
	require.Equal(t, int64(100_000), q.CreditVND)
	require.Equal(t, int64(49_000*3*2-100_000), q.TotalVND)

	// a promo applies to what is left after the credit
	f.promo(t, types.UsagePromoCode{Code: "GIAM10", DiscountType: types.UsagePromoPercent, DiscountValue: 10, IsActive: true})
	q, err = f.svc.Quote(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "seat", Months: 3, Seats: 2,
		PromoCode: "GIAM10"}, "10003")
	require.NoError(t, err)
	require.Equal(t, int64(19_400), q.DiscountVND)
	require.Equal(t, int64(194_000-19_400), q.TotalVND)

	// a credit above the order is capped; the rest is reported as lost
	q, err = f.svc.Quote(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "seat", Months: 1, Seats: 1}, "10003")
	require.NoError(t, err)
	require.Equal(t, int64(49_000), q.CreditVND)
	require.Equal(t, int64(51_000), q.CreditUnusedVND)
	require.Zero(t, q.TotalVND)

	// the order keeps the credit and what it replaces
	o, err := f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "seat", Months: 3, Seats: 2}, "10003", "u1")
	require.NoError(t, err)
	require.Equal(t, int64(100_000), o.CreditVND)
	require.Equal(t, "sub-1", *o.ReplacesSubscriptionID)
	require.Equal(t, end, *o.ReplacesEndsAt)
}

// The paid price counts: a 50 % code halves the credit; a package nobody
// paid for (SuperAdmin grant, sign-up gift) is not credited.
func TestPlanChangeCreditFollowsWhatWasPaid(t *testing.T) {
	f := newBillingFixture(t)
	ctx := context.Background()
	end := endOfVN(2026, 11, 12)
	f.paidPool(t, end, 100_000, 100_000, 50_000)
	q, err := f.svc.Quote(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "seat", Months: 3, Seats: 2}, "10003")
	require.NoError(t, err)
	require.Equal(t, int64(50_000), q.CreditVND)

	g := newBillingFixture(t)
	g.plans.active["tenant/10003"] = &types.UsageSubscription{ID: "sub-g", ScopeType: "tenant", ScopeID: "10003",
		PlanID: "pool", Mode: "pool", StartsAt: billingNow, EndsAt: &end, Status: types.UsageSubscriptionActive}
	q, err = g.svc.Quote(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "seat", Months: 1, Seats: 1}, "10003")
	require.NoError(t, err)
	require.NotNil(t, q.Replaces)
	require.Len(t, q.CreditLines, 1)
	require.False(t, q.CreditLines[0].Paid)
	require.Zero(t, q.CreditVND)

	// a renewal is not a plan change
	q, err = f.svc.Quote(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "pool", Months: 1}, "10003")
	require.NoError(t, err)
	require.Nil(t, q.Replaces)
	require.Zero(t, q.CreditVND)
	require.Equal(t, endOfVN(2026, 12, 12), *q.SubscriptionEndsAt)
}

// H3: on a plan change a held add-on that would cost more to carry (added
// seats) is dropped by default and credited; the buyer may keep it.
func TestPlanChangeHeldAddonIsTheBuyersChoice(t *testing.T) {
	f := newBillingFixture(t)
	withAddonCatalogue(f)
	ctx := context.Background()
	end := endOfVN(2026, 11, 12)
	f.paidPool(t, end, 100_000, 199_000, 0)
	oldID := "o-old"
	require.NoError(t, f.plans.UpsertSubscriptionAddon(ctx, &types.UsageSubscriptionAddon{
		SubscriptionID: "sub-1", AgentID: "thue", AgentName: "Thuế HKD", StartsAt: billingNow, PriceVND: 99_000, OrderID: &oldID}))

	// pool → 3 seats: carrying "thue" costs 2 extra seats → dropped by default
	q, err := f.svc.Quote(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "seat", Months: 1, Seats: 3}, "10003")
	require.NoError(t, err)
	require.Empty(t, q.Addons, "no line the buyer did not choose")
	require.Len(t, q.HeldAddons, 1)
	choice := q.HeldAddons[0]
	require.False(t, choice.Kept)
	require.Positive(t, choice.CarryVND)
	require.Equal(t, int64(99_000), choice.CreditVND, "30 days of 99.000/month")
	require.Equal(t, int64(100_000+99_000), q.CreditLines.Total())
	require.Equal(t, int64(49_000*3), q.CreditVND, "capped at the order")

	// kept: the seats-only line is charged, nothing credited for it
	keep := []string{"thue"}
	q, err = f.svc.Quote(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "seat", Months: 1, Seats: 3,
		KeepAddonAgentIDs: &keep}, "10003")
	require.NoError(t, err)
	require.Len(t, q.Addons, 1)
	require.True(t, q.Addons[0].SeatsOnly)
	require.Equal(t, choice.CarryVND, q.Addons[0].AmountVND)
	require.True(t, q.HeldAddons[0].Kept)

	// pool → pool-sized change (1 unit): carrying is free, so kept by default
	q, err = f.svc.Quote(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "seat", Months: 1, Seats: 1}, "10003")
	require.NoError(t, err)
	require.True(t, q.HeldAddons[0].Kept)
	require.Zero(t, q.HeldAddons[0].CarryVND)

	// confirmed with the default: the dropped add-on is not carried over
	o, err := f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "seat", Months: 1, Seats: 3}, "10003", "u1")
	require.NoError(t, err)
	_, err = f.svc.ConfirmOrder(ctx, o.ID, "sa")
	require.NoError(t, err)
	next := f.plans.active["tenant/10003"]
	require.Equal(t, "seat", next.PlanID)
	require.Nil(t, f.right(next.ID, "thue"), "dropped, credited instead")
}

// The credit was priced against the package as it was: renewed in between,
// the plan-change order is refused.
func TestPlanChangeAgainstAMovedPackageIsRefused(t *testing.T) {
	f := newBillingFixture(t)
	ctx := context.Background()
	f.paidPool(t, endOfVN(2026, 11, 12), 100_000, 100_000, 0)
	change, err := f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "seat", Months: 1, Seats: 3}, "10003", "u1")
	require.NoError(t, err)
	renew, err := f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "pool", Months: 1}, "10003", "u1")
	require.NoError(t, err)
	_, err = f.svc.ConfirmOrder(ctx, renew.ID, "sa")
	require.NoError(t, err)
	_, err = f.svc.ConfirmOrder(ctx, change.ID, "sa")
	require.Equal(t, 409, appErrHTTP(t, err))
}

// H7: a seat plan bought again after it lapsed keeps the assigned seats
// (earliest first, up to the seats bought).
func TestSeatPlanBoughtAgainKeepsSeats(t *testing.T) {
	f := newBillingFixture(t)
	ctx := context.Background()
	lapsed := endOfVN(2026, 10, 1)
	f.plans.active["tenant/10003"] = &types.UsageSubscription{ID: "sub-old", ScopeType: "tenant", ScopeID: "10003",
		PlanID: "seat", Mode: "seat", Seats: 3, StartsAt: billingNow.AddDate(0, -2, 0), EndsAt: &lapsed,
		Status: types.UsageSubscriptionActive}
	for i, u := range []string{"a", "b", "c"} {
		require.NoError(t, f.plans.AddSeat(ctx, &types.UsageSeat{SubscriptionID: "sub-old", UserID: u,
			AssignedAt: billingNow.Add(time.Duration(i) * time.Hour)}))
	}
	q, err := f.svc.Quote(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "seat", Months: 1, Seats: 2}, "10003")
	require.NoError(t, err)
	require.Equal(t, 2, q.SeatsCarried)
	require.Nil(t, q.Replaces, "a lapsed package is not replaced, nothing to credit")

	o, err := f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "seat", Months: 1, Seats: 2}, "10003", "u1")
	require.NoError(t, err)
	_, err = f.svc.ConfirmOrder(ctx, o.ID, "sa")
	require.NoError(t, err)
	next := f.plans.active["tenant/10003"]
	require.NotEqual(t, "sub-old", next.ID)
	seats, _ := f.plans.ListSeats(ctx, next.ID)
	require.Len(t, seats, 2)
	require.Equal(t, "a", seats[0].UserID)
	require.Equal(t, "b", seats[1].UserID)
}
