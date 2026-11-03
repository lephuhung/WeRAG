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
	_, err = f.svc.CancelOrder(ctx, first.ID, false)
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
	n, err := f.svc.CountOrdersAwaitingAdmin(ctx)
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

	_, err = f.svc.ReopenOrder(ctx, o.ID, false)
	require.Equal(t, "order_not_expired", appErrReason(t, err)["reason"])

	later := billingNow.Add(49 * time.Hour)
	f.svc.now = func() time.Time { return later }
	_, err = f.svc.ListOrders(ctx, types.UsageOrderFilter{})
	require.NoError(t, err)
	require.Equal(t, types.UsageOrderExpired, f.repo.orders[o.ID].Status)
	for _, p := range f.repo.promos {
		require.Zero(t, p.UsedCount)
	}

	v, err := f.svc.ReopenOrder(ctx, o.ID, false)
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

	_, err = f.svc.ReopenOrder(ctx, o.ID, false)
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

	_, err = f.svc.ReopenOrder(ctx, change.ID, false)
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

// H1: the transfer completing an order is refused at confirmation: the
// money counts as received, nothing more is asked for (no QR), and the
// refused transfer is shown so the page says a SuperAdmin handles it.
func TestRefusedCompletingTransferCountsAsReceived(t *testing.T) {
	f := newBillingFixture(t)
	ctx := context.Background()
	first, err := f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "seat", Months: 1, Seats: 5}, "10003", "u1")
	require.NoError(t, err)
	_, err = f.svc.ConfirmOrder(ctx, first.ID, "sa")
	require.NoError(t, err)
	small, err := f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "seat", Months: 1, Seats: 5}, "10003", "u1")
	require.NoError(t, err)
	big, err := f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "seat", Months: 1, Seats: 50, ConfirmDuplicate: true}, "10003", "u1")
	require.NoError(t, err)
	_, err = f.svc.ConfirmOrder(ctx, big.ID, "sa")
	require.NoError(t, err)

	part := small.TotalVND / 2
	t1, t2 := billingNow.Add(time.Hour), billingNow.Add(2*time.Hour)
	res := f.pay(t, types.UsagePaymentWebhookRequest{TransactionID: "h1", AmountVND: part, Content: small.OrderCode, TransactedAt: &t1})
	require.Equal(t, types.UsagePaymentUnderpaid, res.Transaction.Status)
	res = f.pay(t, types.UsagePaymentWebhookRequest{TransactionID: "h2", AmountVND: small.TotalVND - part, Content: small.OrderCode, TransactedAt: &t2})
	require.Equal(t, types.UsagePaymentConfirmFailed, res.Transaction.Status)

	v, err := f.svc.GetOrderByCode(ctx, small.OrderCode)
	require.NoError(t, err)
	require.Equal(t, types.UsageOrderPending, v.Status)
	require.Equal(t, small.TotalVND, v.ReceivedVND)
	require.Equal(t, types.UsagePaymentConfirmFailed, v.LastPayment.Status)
	require.Empty(t, v.QRURL, "nothing more to transfer")
	n, err := f.svc.CountOrdersAwaitingAdmin(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
}

// M1: short transfers add up within one source, and the same transfer
// reported again (another checker, a new id) counts once.
func TestShortTransfersAddUpOncePerSource(t *testing.T) {
	f := newBillingFixture(t)
	ctx := context.Background()
	o := f.poolOrder(t)
	at := billingNow.Add(time.Hour)

	res := f.pay(t, types.UsagePaymentWebhookRequest{Source: "a", TransactionID: "1", AmountVND: 60_000, Content: o.OrderCode, TransactedAt: &at})
	require.Equal(t, types.UsagePaymentUnderpaid, res.Transaction.Status)
	// another checker reports the same transfer: it does not add up
	res = f.pay(t, types.UsagePaymentWebhookRequest{Source: "b", TransactionID: "x", AmountVND: 60_000, Content: o.OrderCode, TransactedAt: &at})
	require.Equal(t, types.UsagePaymentUnderpaid, res.Transaction.Status)
	require.Equal(t, types.UsageOrderPending, f.repo.orders[o.ID].Status)

	v, err := f.svc.GetOrderByCode(ctx, o.OrderCode)
	require.NoError(t, err)
	require.EqualValues(t, 60_000, v.ReceivedVND)
	require.Contains(t, v.QRURL, "amount=40000")

	later := at.Add(time.Hour)
	res = f.pay(t, types.UsagePaymentWebhookRequest{Source: "a", TransactionID: "3", AmountVND: 40_000, Content: o.OrderCode, TransactedAt: &later})
	require.Equal(t, types.UsagePaymentMatched, res.Transaction.Status)
	require.Equal(t, types.UsageOrderPaid, f.repo.orders[o.ID].Status)
}

// Two real transfers of the same amount booked in the same minute by one
// source both count (a bank app capping one transfer).
func TestSameSourceTwinTransfersBothCount(t *testing.T) {
	f := newBillingFixture(t)
	o := f.poolOrder(t)
	at := billingNow.Add(time.Hour)

	res := f.pay(t, types.UsagePaymentWebhookRequest{Source: "a", TransactionID: "t1", AmountVND: 50_000, Content: o.OrderCode, TransactedAt: &at})
	require.Equal(t, types.UsagePaymentUnderpaid, res.Transaction.Status)
	res = f.pay(t, types.UsagePaymentWebhookRequest{Source: "a", TransactionID: "t2", AmountVND: 50_000, Content: o.OrderCode, TransactedAt: &at})
	require.Equal(t, types.UsagePaymentMatched, res.Transaction.Status)
	require.Equal(t, types.UsageOrderPaid, f.repo.orders[o.ID].Status)
	require.Equal(t, "completed by transfer t2", f.repo.payments["a/t1"].Reason)
}

// M2: an order that received money is not cancelled by its buyer; a
// SuperAdmin may force it.
func TestCancelRefusedOnceMoneyIsReceived(t *testing.T) {
	f := newBillingFixture(t)
	ctx := context.Background()
	o := f.poolOrder(t)
	f.pay(t, types.UsagePaymentWebhookRequest{TransactionID: "c1", AmountVND: 60_000, Content: o.OrderCode})

	_, err := f.svc.CancelOrder(ctx, o.ID, false)
	require.Equal(t, 409, appErrHTTP(t, err))
	d, ok := err.(*apperrors.AppError).Details.(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "payment_received", d["reason"])
	require.EqualValues(t, 60_000, d["received_vnd"])
	require.Equal(t, types.UsageOrderPending, f.repo.orders[o.ID].Status)

	v, err := f.svc.CancelOrder(ctx, o.ID, true)
	require.NoError(t, err)
	require.Equal(t, types.UsageOrderCancelled, v.Status)
}

// M3: transfers that came after the order expired are counted again when
// a SuperAdmin reopens it; the listing shows the money received meanwhile.
func TestReopenSettlesTransfersAlreadyRecorded(t *testing.T) {
	f := newBillingFixture(t)
	ctx := context.Background()
	o := f.poolOrder(t)
	f.svc.now = func() time.Time { return billingNow.Add(49 * time.Hour) }
	_, err := f.svc.ListOrders(ctx, types.UsageOrderFilter{})
	require.NoError(t, err)

	res := f.pay(t, types.UsagePaymentWebhookRequest{TransactionID: "r1", AmountVND: 50_000, Content: o.OrderCode})
	require.Equal(t, types.UsagePaymentOrderClosed, res.Transaction.Status)
	t2 := billingNow.Add(50 * time.Hour)
	res = f.pay(t, types.UsagePaymentWebhookRequest{TransactionID: "r2", AmountVND: 50_000, Content: o.OrderCode, TransactedAt: &t2})
	require.Equal(t, types.UsagePaymentOrderClosed, res.Transaction.Status)

	list, err := f.svc.ListOrders(ctx, types.UsageOrderFilter{})
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.EqualValues(t, 100_000, list[0].ReceivedVND)

	v, err := f.svc.ReopenOrder(ctx, o.ID, false)
	require.NoError(t, err)
	require.Equal(t, types.UsageOrderPaid, v.Status)
	require.Equal(t, types.UsagePaymentMatched, f.repo.payments["bank-poller/r1"].Status)
	require.Equal(t, types.UsagePaymentMatched, f.repo.payments["bank-poller/r2"].Status)
	require.NotNil(t, f.plans.active["tenant/10003"])
}

// Not enough after reopening: the order is pending with the money counted.
func TestReopenWithShortTransfersStaysPending(t *testing.T) {
	f := newBillingFixture(t)
	ctx := context.Background()
	o := f.poolOrder(t)
	f.svc.now = func() time.Time { return billingNow.Add(49 * time.Hour) }
	_, err := f.svc.ListOrders(ctx, types.UsageOrderFilter{})
	require.NoError(t, err)
	f.pay(t, types.UsagePaymentWebhookRequest{TransactionID: "s1", AmountVND: 30_000, Content: o.OrderCode})
	require.Equal(t, types.UsageOrderExpired, f.repo.orders[o.ID].Status)

	v, err := f.svc.ReopenOrder(ctx, o.ID, false)
	require.NoError(t, err)
	require.Equal(t, types.UsageOrderPending, v.Status)
	require.EqualValues(t, 30_000, v.ReceivedVND)
	require.Equal(t, types.UsagePaymentUnderpaid, f.repo.payments["bank-poller/s1"].Status)
}

// L1: a short transfer made in time but reported after expiry keeps the
// order open.
func TestShortTransferInTimeRevivesExpiredOrder(t *testing.T) {
	f := newBillingFixture(t)
	o := f.poolOrder(t)
	f.svc.now = func() time.Time { return billingNow.Add(49 * time.Hour) }
	_, err := f.svc.ListOrders(context.Background(), types.UsageOrderFilter{})
	require.NoError(t, err)
	require.Equal(t, types.UsageOrderExpired, f.repo.orders[o.ID].Status)

	inTime := billingNow.Add(47 * time.Hour)
	res := f.pay(t, types.UsagePaymentWebhookRequest{TransactionID: "l1", AmountVND: 60_000, Content: o.OrderCode, TransactedAt: &inTime})
	require.Equal(t, types.UsagePaymentUnderpaid, res.Transaction.Status)
	require.Equal(t, types.UsageOrderPending, f.repo.orders[o.ID].Status)
	require.True(t, f.repo.orders[o.ID].ExpiresAt.After(billingNow.Add(49*time.Hour)), "a fresh payment window")
}

// L5: reopening a plan order while a newer one is pending needs force.
func TestReopenRefusedWhileAnotherPlanOrderIsPending(t *testing.T) {
	f := newBillingFixture(t)
	ctx := context.Background()
	old := f.poolOrder(t)
	f.svc.now = func() time.Time { return billingNow.Add(49 * time.Hour) }
	newer := f.poolOrder(t)
	require.Equal(t, types.UsageOrderExpired, f.repo.orders[old.ID].Status)

	_, err := f.svc.ReopenOrder(ctx, old.ID, false)
	require.Equal(t, 409, appErrHTTP(t, err))
	d := appErrReason(t, err)
	require.Equal(t, "plan_order_pending", d["reason"])
	require.Equal(t, newer.OrderCode, d["order_code"])
	require.Equal(t, types.UsageOrderExpired, f.repo.orders[old.ID].Status)

	v, err := f.svc.ReopenOrder(ctx, old.ID, true)
	require.NoError(t, err)
	require.Equal(t, types.UsageOrderPending, v.Status)
}
