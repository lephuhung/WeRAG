package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
)

func appErrReason(t *testing.T, err error) map[string]string {
	t.Helper()
	appErr, ok := err.(*apperrors.AppError)
	require.True(t, ok, "%v", err)
	d, ok := appErr.Details.(map[string]string)
	require.True(t, ok, "%v", appErr.Details)
	return d
}

// M2: a second plan order for a scope with one waiting for payment needs
// the buyer's confirmation; paying both would apply both.
func TestPlanOrderWhileOneIsPendingNeedsConfirmation(t *testing.T) {
	f := newBillingFixture(t)
	ctx := context.Background()
	first := f.poolOrder(t)

	_, err := f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "seat", Months: 1, Seats: 2}, "10003", "u2")
	require.Equal(t, 409, appErrHTTP(t, err))
	d := appErrReason(t, err)
	require.Equal(t, "plan_order_pending", d["reason"])
	require.Equal(t, first.OrderCode, d["order_code"])
	require.Len(t, f.repo.orders, 1)

	// another scope is not concerned
	_, err = f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "user", PlanID: "me", Months: 1}, "u1", "u1")
	require.NoError(t, err)

	second, err := f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "pool", Months: 1, ConfirmDuplicate: true}, "10003", "u1")
	require.NoError(t, err)
	require.NotEqual(t, first.OrderCode, second.OrderCode)

	// once the pending ones are gone (cancelled, expired) no question is asked
	_, err = f.svc.CancelOrder(ctx, first.ID)
	require.NoError(t, err)
	f.svc.now = func() time.Time { return billingNow.Add(49 * time.Hour) }
	_, err = f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "pool", Months: 1}, "10003", "u1")
	require.NoError(t, err)
	require.Equal(t, types.UsageOrderExpired, f.repo.orders[second.ID].Status)
}

// M3: a 0-đồng order waits for a SuperAdmin and never expires, even one
// written before with an expires_at.
func TestFreeOrderNeverExpires(t *testing.T) {
	f := newBillingFixture(t)
	ctx := context.Background()
	f.promo(t, types.UsagePromoCode{Code: "ALL", DiscountType: "percent", DiscountValue: 100, IsActive: true})
	o, err := f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "pool", Months: 1, PromoCode: "ALL"}, "10003", "u1")
	require.NoError(t, err)
	require.Zero(t, o.TotalVND)
	require.Nil(t, o.ExpiresAt)

	old := billingNow.Add(time.Hour)
	f.repo.orders[o.ID].ExpiresAt = &old
	f.svc.now = func() time.Time { return billingNow.Add(1000 * time.Hour) }
	_, err = f.svc.ListOrders(ctx, types.UsageOrderFilter{})
	require.NoError(t, err)
	require.Equal(t, types.UsageOrderPending, f.repo.orders[o.ID].Status)
	n, err := f.svc.CountPendingOrders(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, n)

	// an order to pay still expires
	paid, err := f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "pool", Months: 1, ConfirmDuplicate: true}, "10003", "u1")
	require.NoError(t, err)
	require.NotNil(t, paid.ExpiresAt)
}

// M8: the order keeps the quote's prorated seats and term savings.
func TestOrderKeepsQuoteBreakdown(t *testing.T) {
	f := newBillingFixture(t)
	ctx := context.Background()
	o, err := f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "seat", Months: 1, Seats: 10}, "10003", "u1")
	require.NoError(t, err)
	_, err = f.svc.ConfirmOrder(ctx, o.ID, "sa")
	require.NoError(t, err)

	req := types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "seat", Months: 1, Seats: 15}
	q, err := f.svc.Quote(ctx, req, "10003")
	require.NoError(t, err)
	require.NotZero(t, q.ProrationVND)
	more, err := f.svc.CreateOrder(ctx, req, "10003", "u1")
	require.NoError(t, err)
	stored := f.repo.orders[more.ID]
	require.Equal(t, q.ProrationVND, stored.ProrationVND)
	require.Equal(t, q.ProratedDays, stored.ProratedDays)
	require.Equal(t, int64(49_000*15)+stored.ProrationVND, stored.SubtotalVND)

	// a quarter of the term plan, for another workspace
	termReq := types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "term", Months: 3, Seats: 2}
	tq, err := f.svc.Quote(ctx, termReq, "10004")
	require.NoError(t, err)
	require.NotZero(t, tq.SavingsVND)
	term, err := f.svc.CreateOrder(ctx, termReq, "10004", "u1")
	require.NoError(t, err)
	require.Equal(t, tq.SavingsVND, f.repo.orders[term.ID].SavingsVND)
}

// M5: SuperAdmin reopens an expired order: pending again with a fresh
// window and its promo use taken again; only expired orders reopen.
func TestReopenExpiredOrder(t *testing.T) {
	f := newBillingFixture(t)
	ctx := context.Background()
	f.promo(t, types.UsagePromoCode{Code: "ONCE", DiscountType: "amount", DiscountValue: 10_000, IsActive: true, MaxUses: 1})
	o, err := f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "pool", Months: 1, PromoCode: "ONCE"}, "10003", "u1")
	require.NoError(t, err)

	_, err = f.svc.ReopenOrder(ctx, o.ID)
	require.Equal(t, "order_not_expired", appErrReason(t, err)["reason"])

	later := billingNow.Add(49 * time.Hour)
	f.svc.now = func() time.Time { return later }
	_, err = f.svc.ListOrders(ctx, types.UsageOrderFilter{})
	require.NoError(t, err)
	require.Equal(t, types.UsageOrderExpired, f.repo.orders[o.ID].Status)
	for _, p := range f.repo.promos {
		require.Zero(t, p.UsedCount)
	}

	v, err := f.svc.ReopenOrder(ctx, o.ID)
	require.NoError(t, err)
	require.Equal(t, types.UsageOrderPending, v.Status)
	require.True(t, v.ExpiresAt.After(later), "a fresh payment window")
	for _, p := range f.repo.promos {
		require.Equal(t, 1, p.UsedCount, "the promo use is taken again")
	}
	// it stays open on the next read and can be confirmed
	got, err := f.svc.GetOrder(ctx, o.ID)
	require.NoError(t, err)
	require.Equal(t, types.UsageOrderPending, got.Status)
	_, err = f.svc.ConfirmOrder(ctx, o.ID, "sa")
	require.NoError(t, err)
}

func TestReopenRefusesWhenPromoIsUsedUp(t *testing.T) {
	f := newBillingFixture(t)
	ctx := context.Background()
	f.promo(t, types.UsagePromoCode{Code: "ONCE", DiscountType: "amount", DiscountValue: 10_000, IsActive: true, MaxUses: 1})
	o, err := f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "pool", Months: 1, PromoCode: "ONCE"}, "10003", "u1")
	require.NoError(t, err)
	f.svc.now = func() time.Time { return billingNow.Add(49 * time.Hour) }
	_, err = f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "user", PlanID: "me", Months: 1, PromoCode: "ONCE"}, "u1", "u1")
	require.NoError(t, err)
	require.Equal(t, types.UsageOrderExpired, f.repo.orders[o.ID].Status)

	_, err = f.svc.ReopenOrder(ctx, o.ID)
	require.Equal(t, 409, appErrHTTP(t, err))
	require.Equal(t, types.UsageOrderExpired, f.repo.orders[o.ID].Status)
}

// A reopened plan change still meets the guard at confirmation: the
// package it was credited against has been renewed meanwhile.
func TestReopenedPlanChangeAgainstAMovedPackageIsRefused(t *testing.T) {
	f := newBillingFixture(t)
	ctx := context.Background()
	f.paidPool(t, endOfVN(2026, 11, 12), 100_000, 100_000, 0)
	change, err := f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "seat", Months: 1, Seats: 3}, "10003", "u1")
	require.NoError(t, err)
	f.svc.now = func() time.Time { return billingNow.Add(49 * time.Hour) }
	renew, err := f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "pool", Months: 1}, "10003", "u1")
	require.NoError(t, err)
	_, err = f.svc.ConfirmOrder(ctx, renew.ID, "sa")
	require.NoError(t, err)

	_, err = f.svc.ReopenOrder(ctx, change.ID)
	require.NoError(t, err)
	_, err = f.svc.ConfirmOrder(ctx, change.ID, "sa")
	require.Equal(t, 409, appErrHTTP(t, err))
}

// M4 + M6: the order view tells what short transfers brought (the QR asks
// for the rest) and, once paid, how long the package runs.
func TestOrderViewShowsShortTransfersAndNewEnd(t *testing.T) {
	f := newBillingFixture(t)
	ctx := context.Background()
	o := f.poolOrder(t)

	res := f.pay(t, types.UsagePaymentWebhookRequest{TransactionID: "p1", AmountVND: 60_000, Content: o.OrderCode})
	require.Equal(t, types.UsagePaymentUnderpaid, res.Transaction.Status)
	v, err := f.svc.GetOrderByCode(ctx, o.OrderCode)
	require.NoError(t, err)
	require.EqualValues(t, 60_000, v.ReceivedVND)
	require.NotNil(t, v.LastPayment)
	require.Equal(t, types.UsagePaymentUnderpaid, v.LastPayment.Status)
	require.Contains(t, v.QRURL, "amount=40000")
	require.Nil(t, v.SubscriptionEndsAt)

	res = f.pay(t, types.UsagePaymentWebhookRequest{TransactionID: "p2", AmountVND: 40_000, Content: o.OrderCode})
	require.Equal(t, types.UsagePaymentMatched, res.Transaction.Status)
	v, err = f.svc.GetOrderByCode(ctx, o.OrderCode)
	require.NoError(t, err)
	require.Equal(t, types.UsageOrderPaid, v.Status)
	require.Zero(t, v.ReceivedVND)
	require.Equal(t, vnEnd(1), *v.SubscriptionEndsAt)
}
