package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
)

type countingBillingNotifier struct{ created, paid int }

func (n *countingBillingNotifier) OrderCreated(context.Context, *types.UsageOrderView) { n.created++ }
func (n *countingBillingNotifier) OrderPaid(context.Context, *types.UsageOrderView)    { n.paid++ }

// A SuperAdmin confirming an order by hand settles the transfers that order
// was holding (no stale underpaid/confirm_failed reason left behind); the
// transfers of another order are not touched, and the buyer is mailed once.
func TestAdminConfirmSettlesOnlyThatOrdersHeldTransfers(t *testing.T) {
	f := newBillingFixture(t)
	ctx := context.Background()
	o := f.poolOrder(t)
	other, err := f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "user", PlanID: "me", Months: 1}, "u1", "u1")
	require.NoError(t, err)

	res := f.pay(t, types.UsagePaymentWebhookRequest{TransactionID: "s1", AmountVND: 60_000, Content: o.OrderCode})
	require.Equal(t, types.UsagePaymentUnderpaid, res.Transaction.Status)
	short := "bank-poller/s1"
	res = f.pay(t, types.UsagePaymentWebhookRequest{TransactionID: "x1", AmountVND: 5_000, Content: other.OrderCode})
	require.Equal(t, types.UsagePaymentUnderpaid, res.Transaction.Status)
	otherPay := "bank-poller/x1"

	mail := &countingBillingNotifier{}
	svc := &billingEmailNotify{UsageBillingService: f.svc, notifier: mail}
	v, err := svc.ConfirmOrder(ctx, o.ID, "superadmin-1")
	require.NoError(t, err)
	require.Equal(t, types.UsageOrderPaid, v.Status)
	require.Equal(t, 1, mail.paid)

	got := f.repo.payments[short]
	require.Equal(t, types.UsagePaymentMatched, got.Status)
	require.Equal(t, usagePaymentAdminConfirmedReason, got.Reason)
	require.Equal(t, types.UsagePaymentUnderpaid, f.repo.payments[otherPay].Status, "another order's transfer is left alone")

	n, err := f.svc.CountOrdersAwaitingAdmin(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, n, "only the other order still holds money")
}

// Money that arrives after an order expired raises the SuperAdmin badge
// (and the awaiting filter) and shows on the buyer's order page; reopening
// the order settles it.
func TestExpiredOrderHoldingMoneyAwaitsAdmin(t *testing.T) {
	f := newBillingFixture(t)
	ctx := context.Background()
	o := f.poolOrder(t)
	f.svc.now = func() time.Time { return billingNow.Add(100 * time.Hour) }
	_, err := f.svc.ListOrders(ctx, types.UsageOrderFilter{}) // expires it
	require.NoError(t, err)
	require.Equal(t, types.UsageOrderExpired, f.repo.orders[o.ID].Status)
	late := billingNow.Add(99 * time.Hour)
	res := f.pay(t, types.UsagePaymentWebhookRequest{TransactionID: "late", AmountVND: o.TotalVND, Content: o.OrderCode, TransactedAt: &late})
	require.Equal(t, types.UsagePaymentOrderClosed, res.Transaction.Status)
	require.Equal(t, types.UsageOrderExpired, f.repo.orders[o.ID].Status)

	n, err := f.svc.CountOrdersAwaitingAdmin(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	list, err := f.svc.ListOrders(ctx, types.UsageOrderFilter{AwaitingAdmin: true})
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, o.ID, list[0].ID)

	v, err := f.svc.GetOrderByCode(ctx, o.OrderCode)
	require.NoError(t, err)
	require.Equal(t, o.TotalVND, v.ReceivedVND)
	require.Empty(t, v.QRURL)

	v, err = f.svc.ReopenOrder(ctx, o.ID, false)
	require.NoError(t, err)
	require.Equal(t, types.UsageOrderPaid, v.Status)
	n, err = f.svc.CountOrdersAwaitingAdmin(ctx)
	require.NoError(t, err)
	require.Zero(t, n)
}
