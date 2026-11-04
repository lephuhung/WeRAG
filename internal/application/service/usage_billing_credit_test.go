package service

import (
	"context"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
)

// e2e review (dev, 03/11/2026): a credit never gives back more than was
// paid. The old credit priced the unused days at the latest order's month
// × days / 30, so a 31-day month was worth 31/30 of its price: 61 days left
// of a 1-month purchase + 1-month renewal of 245.000 credited 498.166.

// buy creates and confirms req for tenant 10003 at the fixture's clock.
func (f *billingFixture) buy(t *testing.T, req types.UsageQuoteRequest) *types.UsageOrderView {
	t.Helper()
	ctx := context.Background()
	req.ScopeType = "tenant"
	o, err := f.svc.CreateOrder(ctx, req, "10003", "u1")
	require.NoError(t, err)
	v, err := f.svc.ConfirmOrder(ctx, o.ID, "sa")
	require.NoError(t, err)
	return v
}

// changeCredit quotes a plan change of tenant 10003 to the pool plan and
// returns its credit lines.
func (f *billingFixture) changeCredit(t *testing.T) types.UsageCreditLines {
	t.Helper()
	none := []string{}
	q, err := f.svc.Quote(context.Background(), types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "pool", Months: 12,
		KeepAddonAgentIDs: &none}, "10003")
	require.NoError(t, err)
	require.NotNil(t, q.Replaces)
	return q.CreditLines
}

func paidTotal(f *billingFixture) int64 {
	var sum int64
	for _, o := range f.repo.orders {
		if o.Status == types.UsageOrderPaid {
			sum += o.TotalVND
		}
	}
	return sum
}

// The dev case: 5 seats × 49.000 bought 13/10 (period 13/10–13/11, 32
// calendar days counting today), renewed for a month the same day (14/11–
// 13/12, 30 days). 61 days are left after today; 490.000 was paid.
// Each order is valued by the share of its own period still unused:
// 245.000 × 31/32 + 245.000 × 30/30 = 237.343 + 245.000 = 482.343
// (the old rule: 245.000 × 61/30 = 498.166).
func TestPlanCreditOfPurchaseAndRenewalNeverExceedsPaid(t *testing.T) {
	f := newBillingFixture(t)
	withAddonCatalogue(f)
	first := f.buy(t, types.UsageQuoteRequest{PlanID: "seat", Months: 1, Seats: 4, AddonAgentIDs: []string{"thue"}})
	require.Equal(t, int64(4*49_000+4*99_000), first.TotalVND)
	require.Equal(t, billingNow, *first.PeriodStartsAt)
	require.Equal(t, endOfVN(2026, 11, 13), *first.ResultEndsAt)
	renew := f.buy(t, types.UsageQuoteRequest{PlanID: "seat", Months: 1, Seats: 4, AddonAgentIDs: []string{"thue"}})
	require.Equal(t, endOfVN(2026, 11, 13), *renew.PeriodStartsAt)
	require.Equal(t, endOfVN(2026, 12, 13), *renew.ResultEndsAt)
	require.Equal(t, int64(4*49_000+4*99_000), renew.TotalVND)

	lines := f.changeCredit(t)
	require.Len(t, lines, 2)
	plan, addon := lines[0], lines[1]
	require.Equal(t, 61, plan.Days)
	require.Equal(t, int64(196_000*31/32+196_000), plan.AmountVND, "189.875 + 196.000, not 196.000 × 61/30 = 398.533")
	require.LessOrEqual(t, plan.AmountVND, int64(2*196_000))
	// 4 chỗ × 99.000: 396.000 × 31/32 + 396.000 = 779.625 (old rule 805.200)
	require.Equal(t, types.UsageCreditAddon, addon.Kind)
	require.Equal(t, 61, addon.Days)
	require.Equal(t, int64(383_625+396_000), addon.AmountVND)
	require.LessOrEqual(t, addon.AmountVND, int64(2*396_000))
	require.LessOrEqual(t, lines.Total(), paidTotal(f))
}

// The dev numbers on a 5-seat pool of 245.000: a 31-day renewal is worth
// exactly what was paid for it, never 31/30 of it.
func TestPlanCreditDevCase(t *testing.T) {
	f := newBillingFixture(t)
	f.buy(t, types.UsageQuoteRequest{PlanID: "seat", Months: 1, Seats: 5})
	f.buy(t, types.UsageQuoteRequest{PlanID: "seat", Months: 1, Seats: 5})
	plan := f.changeCredit(t)[0]
	require.Equal(t, 61, plan.Days)
	require.Equal(t, int64(237_343+245_000), plan.AmountVND)
	require.Less(t, plan.AmountVND, int64(490_000))

	// a 31-day month bought on its last day: 31/10 → end of 30/11 = 31 days
	// counting today, 30 left: 245.000 × 30/31, not × 30/30
	g := newBillingFixture(t)
	day := time.Date(2026, 10, 31, 3, 0, 0, 0, time.UTC) // 10:00 VN
	g.svc.now = func() time.Time { return day }
	g.buy(t, types.UsageQuoteRequest{PlanID: "seat", Months: 1, Seats: 5})
	plan = g.changeCredit(t)[0]
	require.Equal(t, 30, plan.Days)
	require.Equal(t, int64(245_000*30/31), plan.AmountVND)
}

// A promo code lowers each order's value; the credit follows what was paid.
func TestPlanCreditFollowsPromoPerOrder(t *testing.T) {
	f := newBillingFixture(t)
	f.promo(t, types.UsagePromoCode{Code: "HALF", DiscountType: types.UsagePromoPercent, DiscountValue: 50, IsActive: true})
	half := f.buy(t, types.UsageQuoteRequest{PlanID: "seat", Months: 1, Seats: 5, PromoCode: "HALF"})
	require.Equal(t, int64(122_500), half.TotalVND)
	f.buy(t, types.UsageQuoteRequest{PlanID: "seat", Months: 1, Seats: 5})
	plan := f.changeCredit(t)[0]
	require.Equal(t, int64(122_500*31/32+245_000), plan.AmountVND)
	require.LessOrEqual(t, plan.AmountVND, paidTotal(f))
}

// A seat order bought mid-period is valued by its own proration over the
// days it was charged for; a renewal after it covers the next period.
func TestPlanCreditSeatOrderTranche(t *testing.T) {
	f := newBillingFixture(t)
	f.buy(t, types.UsageQuoteRequest{PlanID: "seat", Months: 1, Seats: 3}) // 147.000, 13/10–13/11 (32 days)
	clock := billingNow.AddDate(0, 0, 10)                                  // 23/10
	f.svc.now = func() time.Time { return clock }
	seats := f.buy(t, seatsReq(2))
	require.Equal(t, 22, seats.ProratedDays, "23/10–13/11")
	require.Equal(t, int64((49_000*2*22+29)/30), seats.TotalVND) // 71.867
	clock = clock.Add(time.Hour)
	f.buy(t, types.UsageQuoteRequest{PlanID: "seat", Months: 1, Seats: 5})
	plan := f.changeCredit(t)[0]
	require.Equal(t, 5, plan.Units)
	// 147.000 × 21/32 + 71.867 × 21/22 + 245.000 × 30/30
	want := int64(147_000*21/32) + int64(71_867*21/22) + 245_000
	require.Equal(t, want, plan.AmountVND)
	require.LessOrEqual(t, plan.AmountVND, paidTotal(f))
}

// The receipt shows the end each order bought, not the package's current end.
func TestPaidOrderViewShowsTheEndItBought(t *testing.T) {
	f := newBillingFixture(t)
	first := f.buy(t, types.UsageQuoteRequest{PlanID: "seat", Months: 1, Seats: 2})
	f.buy(t, types.UsageQuoteRequest{PlanID: "seat", Months: 2, Seats: 2})
	v, err := f.svc.detailView(context.Background(), f.repo.orders[first.ID])
	require.NoError(t, err)
	require.Equal(t, endOfVN(2026, 11, 13), *v.SubscriptionEndsAt, "not 13/1")

	// an older order without the stored end falls back to the package's
	old := *f.repo.orders[first.ID]
	old.ResultEndsAt, old.PeriodStartsAt = nil, nil
	v, err = f.svc.detailView(context.Background(), &old)
	require.NoError(t, err)
	require.Equal(t, endOfVN(2027, 1, 13), *v.SubscriptionEndsAt)
}

// Property: over random purchase days, terms, renewals, seat orders, promo
// codes and quote days, the credit never exceeds what was paid, and never
// grows as days pass.
func TestPlanCreditNeverExceedsPaidProperty(t *testing.T) {
	rng := rand.New(rand.NewSource(20261103))
	for i := 0; i < 150; i++ {
		f := newBillingFixture(t)
		withAddonCatalogue(f)
		f.promo(t, types.UsagePromoCode{Code: "P30", DiscountType: types.UsagePromoPercent, DiscountValue: 30, IsActive: true})
		clock := billingNow.Add(time.Duration(rng.Intn(365*24*60)) * time.Minute)
		f.svc.now = func() time.Time { return clock }
		promo := func() string {
			if rng.Intn(3) == 0 {
				return "P30"
			}
			return ""
		}
		seats := 1 + rng.Intn(4)
		var addons []string
		if rng.Intn(2) == 0 {
			addons = []string{"thue"}
		}
		f.buy(t, types.UsageQuoteRequest{PlanID: "seat", Months: []int{1, 2, 3}[rng.Intn(3)], Seats: seats,
			AddonAgentIDs: addons, PromoCode: promo()})
		end := func() time.Time { return *f.plans.active["tenant/10003"].EndsAt }
		if rng.Intn(2) == 0 {
			clock = clock.Add(time.Duration(rng.Intn(int(end().Sub(clock).Hours()))) * time.Hour)
			k := 1 + rng.Intn(3)
			f.buy(t, types.UsageQuoteRequest{Kind: types.UsageOrderKindSeats, Seats: k, PromoCode: promo()})
			seats += k
		}
		if rng.Intn(2) == 0 {
			clock = clock.Add(time.Minute)
			add := rng.Intn(2)
			f.buy(t, types.UsageQuoteRequest{PlanID: "seat", Months: 1 + rng.Intn(2), Seats: seats + add,
				AddonAgentIDs: addons, PromoCode: promo()})
			seats += add
		}
		// LOW-1: a chained plan change (the credit then rides on the
		// replaced package's money as applied credit), to a seat plan,
		// keeping or dropping the add-on, sometimes buying it again
		if rng.Intn(2) == 0 {
			clock = clock.Add(time.Duration(1+rng.Intn(48)) * time.Hour)
			if clock.Before(end()) {
				keep := []string{}
				if rng.Intn(2) == 0 {
					keep = []string{"thue"}
				}
				var again []string
				if len(addons) > 0 && rng.Intn(3) == 0 {
					again = []string{"thue"}
				}
				req := types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "term", Months: []int{1, 3}[rng.Intn(2)],
					Seats: 1 + rng.Intn(4), AddonAgentIDs: again, KeepAddonAgentIDs: &keep, PromoCode: promo()}
				if o, err := f.svc.CreateOrder(context.Background(), req, "10003", "u1"); err == nil {
					_, err = f.svc.ConfirmOrder(context.Background(), o.ID, "sa")
					require.NoError(t, err)
				}
			}
		}
		// bounded by the cash of all orders (credit applied on a change is
		// that same money again, not new money)
		paid := paidTotal(f)
		last := int64(-1)
		for clock.Before(end()) {
			lines := f.changeCredit(t)
			total := lines.Total()
			require.LessOrEqual(t, total, paid, "case %d at %s", i, clock)
			for _, l := range lines {
				require.GreaterOrEqual(t, l.AmountVND, int64(0))
			}
			if last >= 0 {
				require.LessOrEqual(t, total, last, "case %d: the credit grew at %s", i, clock)
			}
			last = total
			clock = clock.Add(time.Duration(1+rng.Intn(240)) * time.Hour)
		}
	}
}

// piecesCredit itself: each piece gives back at most its net, whatever the
// days, units taken or period.
func TestPiecesCreditBoundedProperty(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for i := 0; i < 5000; i++ {
		now := billingNow.Add(time.Duration(rng.Intn(2000)) * time.Hour)
		var pieces []paidPiece
		var nets int64
		for j := 0; j < 1+rng.Intn(4); j++ {
			start := billingNow.Add(time.Duration(rng.Intn(3000)-500) * time.Hour)
			p := paidPiece{lo: rng.Intn(3), net: int64(rng.Intn(2_000_000)),
				start: start, end: start.Add(time.Duration(1+rng.Intn(2500)) * time.Hour), minDays: rng.Intn(40)}
			p.hi = p.lo + rng.Intn(5)
			pieces = append(pieces, p)
			nets += p.net
		}
		total := rng.Intn(8)
		amount, _ := piecesCredit(pieces, total, rng.Intn(10), now, now.Add(time.Duration(rng.Intn(4000))*time.Hour))
		require.GreaterOrEqual(t, amount, int64(0))
		require.LessOrEqual(t, amount, nets)
	}
}

// Fable MED-1: a right kept and bought again on a plan change names the new
// order, but the replaced package's paid time still backs it. A (pool, 3
// months + thue) → B (seat, thue kept and bought again to 14/4) → C (term,
// thue carried) → dropped: the credit counts A's unused time too.
func TestAddonCreditFollowsReplacedPackages(t *testing.T) {
	f := newBillingFixture(t)
	withAddonCatalogue(f)
	clock := billingNow
	f.svc.now = func() time.Time { return clock }
	a := f.buy(t, types.UsageQuoteRequest{PlanID: "pool", Months: 3, AddonAgentIDs: []string{"thue"}})
	require.Equal(t, int64(297_000), a.Addons[0].AmountVND) // 13/10–13/1, 93 days
	keep := []string{"thue"}
	clock = clock.AddDate(0, 0, 1)
	b := f.buy(t, types.UsageQuoteRequest{PlanID: "seat", Months: 6, Seats: 1, AddonAgentIDs: keep, KeepAddonAgentIDs: &keep})
	bThue := b.Addons[0].AmountVND // 14/1–14/4 (91 days), priced by the add-on remainder rule
	require.Positive(t, bThue)
	require.Equal(t, b.ID, *f.right(*b.SubscriptionID, "thue").OrderID, "the right now names B's order")
	clock = clock.AddDate(0, 0, 1)
	c := f.buy(t, types.UsageQuoteRequest{PlanID: "term", Months: 1, Seats: 1, KeepAddonAgentIDs: &keep})
	require.Empty(t, c.Addons)
	require.Equal(t, endOfVN(2027, 4, 14), *f.right(*c.SubscriptionID, "thue").EndsAt)

	clock = clock.AddDate(0, 0, 1) // 16/10: 17/10–13/1 = 89 days of A's 93
	var thue *types.UsageCreditLine
	lines := f.changeCredit(t)
	for i := range lines {
		if lines[i].AgentID == "thue" {
			thue = &lines[i]
		}
	}
	require.NotNil(t, thue)
	require.Equal(t, int64(297_000*89/93)+bThue, thue.AmountVND, "A's unused time (284.225) + all of B's, not B's alone")
	require.LessOrEqual(t, thue.AmountVND, 297_000+bThue, "the money behind the right")
	require.LessOrEqual(t, lines.Total(), paidTotal(f), "never more than the cash of all orders")

	// dropped on a change (credited then) and bought again: the old
	// package's time is not counted a second time
	g := newBillingFixture(t)
	withAddonCatalogue(g)
	clock = billingNow
	g.svc.now = func() time.Time { return clock }
	g.buy(t, types.UsageQuoteRequest{PlanID: "pool", Months: 3, AddonAgentIDs: []string{"thue"}})
	none := []string{}
	clock = clock.AddDate(0, 0, 1)
	gb := g.buy(t, types.UsageQuoteRequest{PlanID: "seat", Months: 6, Seats: 1, KeepAddonAgentIDs: &none})
	require.True(t, gb.CreditLines.DroppedAddons()["thue"])
	add := g.buy(t, types.UsageQuoteRequest{Kind: types.UsageOrderKindAddon, AddonAgentIDs: []string{"thue"}})
	clock = clock.AddDate(0, 0, 1)
	for _, l := range g.changeCredit(t) {
		if l.AgentID == "thue" {
			require.LessOrEqual(t, l.AmountVND, add.Addons[0].AmountVND, "only the add-on order bought on B")
			require.Positive(t, l.AmountVND)
		}
	}
}
