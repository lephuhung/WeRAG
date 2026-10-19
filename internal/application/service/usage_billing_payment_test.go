package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/types"
)

func (m *memBillingRepo) GetOrderByCode(_ context.Context, code string) (*types.UsageOrder, error) {
	for _, o := range m.orders {
		if o.OrderCode == code {
			cp := *o
			return &cp, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}
func (m *memBillingRepo) CreatePaymentTransaction(_ context.Context, t *types.UsagePaymentTransaction) (bool, error) {
	key := t.Source + "/" + t.ExternalID
	if _, ok := m.payments[key]; ok {
		return false, nil
	}
	cp := *t
	m.payments[key] = &cp
	return true, nil
}
func (m *memBillingRepo) GetPaymentTransactionByExternalID(_ context.Context, source, externalID string) (*types.UsagePaymentTransaction, error) {
	if t, ok := m.payments[source+"/"+externalID]; ok {
		cp := *t
		return &cp, nil
	}
	return nil, gorm.ErrRecordNotFound
}
func (m *memBillingRepo) UpdatePaymentTransaction(_ context.Context, t *types.UsagePaymentTransaction) error {
	cp := *t
	m.payments[t.Source+"/"+t.ExternalID] = &cp
	return nil
}

func (f *billingFixture) poolOrder(t *testing.T) *types.UsageOrderView {
	t.Helper()
	o, err := f.svc.CreateOrder(context.Background(), types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "pool", Months: 1}, "10003", "u1")
	require.NoError(t, err)
	return o
}

func (f *billingFixture) pay(t *testing.T, req types.UsagePaymentWebhookRequest) *types.UsagePaymentResult {
	t.Helper()
	if req.Source == "" {
		req.Source = "bank-poller"
	}
	res, err := f.svc.RecordPayment(context.Background(), req, []byte(`{"k":1}`))
	require.NoError(t, err)
	return res
}

func TestOrderCodesInSurvivesBankRewriting(t *testing.T) {
	require.Equal(t, []string{"WRABCD2345"}, orderCodesIn("wrabcd2345 chuyen tien"))
	require.Equal(t, []string{"WRABCD2345"}, orderCodesIn("MBVCB.123.WRABCD2345CHUYENTIEN"),
		"the code runs into the next word")
	require.Equal(t, []string{"WRWRABCD23", "WRABCD2345"}, orderCodesIn("WRWRABCD2345"),
		"overlapping candidates are all tried")
	require.Empty(t, orderCodesIn("WRABCD0345"), "0 is not in the code alphabet")
	require.Empty(t, orderCodesIn("WRABC"))
}

func TestPaymentConfirmsMatchingOrder(t *testing.T) {
	f := newBillingFixture(t)
	o := f.poolOrder(t)
	res := f.pay(t, types.UsagePaymentWebhookRequest{
		TransactionID: "FT1", AmountVND: 100_000, AccountNo: "0011 002 233",
		Content: "IBFT " + o.OrderCode + "CHUYEN KHOAN",
	})
	require.False(t, res.Duplicate)
	require.Equal(t, types.UsagePaymentMatched, res.Transaction.Status)
	require.Equal(t, o.OrderCode, res.Transaction.OrderCode)
	require.Equal(t, types.UsageOrderPaid, res.Order.Status)
	require.Equal(t, "payment:bank-poller", res.Order.ConfirmedBy)
	require.NotNil(t, f.plans.active["tenant/10003"])

	// the checker resends: nothing applied twice
	again := f.pay(t, types.UsagePaymentWebhookRequest{TransactionID: "FT1", AmountVND: 100_000, Content: o.OrderCode})
	require.True(t, again.Duplicate)
	require.Equal(t, types.UsagePaymentMatched, again.Transaction.Status)
	require.Equal(t, types.UsageOrderPaid, again.Order.Status)
	require.Equal(t, vnEnd(1), *f.plans.active["tenant/10003"].EndsAt)
	require.Len(t, f.repo.payments, 1)
}

func TestPaymentThatCannotConfirmIsKeptWithReason(t *testing.T) {
	f := newBillingFixture(t)
	o := f.poolOrder(t)

	res := f.pay(t, types.UsagePaymentWebhookRequest{TransactionID: "a", AmountVND: 100_000, Content: "tien hoc phi"})
	require.Equal(t, types.UsagePaymentUnmatched, res.Transaction.Status)
	require.Nil(t, res.Order)

	res = f.pay(t, types.UsagePaymentWebhookRequest{TransactionID: "b", AmountVND: 99_000, Content: o.OrderCode})
	require.Equal(t, types.UsagePaymentUnderpaid, res.Transaction.Status)
	require.Equal(t, types.UsageOrderPending, f.repo.orders[o.ID].Status)

	res = f.pay(t, types.UsagePaymentWebhookRequest{TransactionID: "c", AmountVND: 100_000, Content: o.OrderCode, Direction: "out"})
	require.Equal(t, types.UsagePaymentIgnored, res.Transaction.Status)
	res = f.pay(t, types.UsagePaymentWebhookRequest{TransactionID: "d", AmountVND: 100_000, Content: o.OrderCode, AccountNo: "999"})
	require.Equal(t, types.UsagePaymentIgnored, res.Transaction.Status)
	require.Equal(t, types.UsageOrderPending, f.repo.orders[o.ID].Status)

	// paid by the next transfer, then a second payment of the same order
	res = f.pay(t, types.UsagePaymentWebhookRequest{TransactionID: "e", AmountVND: 120_000, Content: o.OrderCode})
	require.Equal(t, types.UsagePaymentMatched, res.Transaction.Status)
	require.Equal(t, "overpaid by 20000 VND", res.Transaction.Reason)
	res = f.pay(t, types.UsagePaymentWebhookRequest{TransactionID: "f", AmountVND: 100_000, Content: o.OrderCode})
	require.Equal(t, types.UsagePaymentOrderClosed, res.Transaction.Status)
	require.Equal(t, vnEnd(1), *f.plans.active["tenant/10003"].EndsAt)
}

func TestPaymentMadeBeforeExpiryReopensExpiredOrder(t *testing.T) {
	f := newBillingFixture(t)
	f.promo(t, types.UsagePromoCode{Code: "ONCE", DiscountType: "amount", DiscountValue: 10_000, IsActive: true, MaxUses: 1})
	o, err := f.svc.CreateOrder(context.Background(), types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "pool", Months: 1, PromoCode: "ONCE"}, "10003", "u1")
	require.NoError(t, err)
	// expired (and its promo use released) before the checker reported it
	f.svc.now = func() time.Time { return billingNow.Add(49 * time.Hour) }
	_, err = f.svc.ListOrders(context.Background(), types.UsageOrderFilter{})
	require.NoError(t, err)
	require.Equal(t, types.UsageOrderExpired, f.repo.orders[o.ID].Status)

	paidAt := billingNow.Add(47 * time.Hour)
	res := f.pay(t, types.UsagePaymentWebhookRequest{TransactionID: "x", AmountVND: 90_000, Content: o.OrderCode, TransactedAt: &paidAt})
	require.Equal(t, types.UsagePaymentMatched, res.Transaction.Status)
	require.Equal(t, types.UsageOrderPaid, f.repo.orders[o.ID].Status)
	for _, p := range f.repo.promos {
		require.Equal(t, 1, p.UsedCount, "the promo use is taken again")
	}

	// past expires_at but not flipped yet: still open, so it is paid
	o2 := f.poolOrder(t)
	late := billingNow.Add(100 * time.Hour)
	res = f.pay(t, types.UsagePaymentWebhookRequest{TransactionID: "y", AmountVND: 100_000, Content: o2.OrderCode, TransactedAt: &late})
	require.Equal(t, types.UsagePaymentMatched, res.Transaction.Status)

	// a transfer made after an expired order's expiry is left for SuperAdmin
	o3 := f.poolOrder(t)
	f.svc.now = func() time.Time { return billingNow.Add(200 * time.Hour) }
	_, err = f.svc.ListOrders(context.Background(), types.UsageOrderFilter{})
	require.NoError(t, err)
	res = f.pay(t, types.UsagePaymentWebhookRequest{TransactionID: "w", AmountVND: 100_000, Content: o3.OrderCode})
	require.Equal(t, types.UsagePaymentOrderClosed, res.Transaction.Status)
	require.Equal(t, types.UsageOrderExpired, f.repo.orders[o3.ID].Status)
}

func TestPaymentSkipsClosedOrderNamedFirst(t *testing.T) {
	f := newBillingFixture(t)
	closed := f.poolOrder(t)
	_, err := f.svc.CancelOrder(context.Background(), closed.ID)
	require.NoError(t, err)
	open := f.poolOrder(t)
	res := f.pay(t, types.UsagePaymentWebhookRequest{TransactionID: "m", AmountVND: 100_000, Content: closed.OrderCode + " " + open.OrderCode})
	require.Equal(t, types.UsagePaymentMatched, res.Transaction.Status)
	require.Equal(t, open.OrderCode, res.Transaction.OrderCode)
}

func TestPaymentConfirmRefusalKeepsTransaction(t *testing.T) {
	f := newBillingFixture(t)
	first, err := f.svc.CreateOrder(context.Background(), types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "seat", Months: 1, Seats: 5}, "10003", "u1")
	require.NoError(t, err)
	_, err = f.svc.ConfirmOrder(context.Background(), first.ID, "sa")
	require.NoError(t, err)
	small, err := f.svc.CreateOrder(context.Background(), types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "seat", Months: 1, Seats: 5}, "10003", "u1")
	require.NoError(t, err)
	big, err := f.svc.CreateOrder(context.Background(), types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "seat", Months: 1, Seats: 50}, "10003", "u1")
	require.NoError(t, err)
	_, err = f.svc.ConfirmOrder(context.Background(), big.ID, "sa")
	require.NoError(t, err)

	res := f.pay(t, types.UsagePaymentWebhookRequest{TransactionID: "s", AmountVND: small.TotalVND, Content: small.OrderCode})
	require.Equal(t, types.UsagePaymentConfirmFailed, res.Transaction.Status)
	require.Contains(t, res.Transaction.Reason, "50 seats")
	require.Equal(t, types.UsageOrderPending, f.repo.orders[small.ID].Status)
	require.Equal(t, types.UsagePaymentConfirmFailed, f.repo.payments["bank-poller/s"].Status)
}

func TestPaymentStorageFailureStoresNothing(t *testing.T) {
	f := newBillingFixture(t)
	o := f.poolOrder(t)
	f.repo.failUpdateOrder = true
	_, err := f.svc.RecordPayment(context.Background(),
		types.UsagePaymentWebhookRequest{Source: "p", TransactionID: "z", AmountVND: 100_000, Content: o.OrderCode}, nil)
	require.Error(t, err)
	require.False(t, errors.Is(err, gorm.ErrRecordNotFound))
	require.Empty(t, f.repo.payments, "the checker's retry is processed afresh")
	require.Nil(t, f.plans.active["tenant/10003"])

	res := f.pay(t, types.UsagePaymentWebhookRequest{Source: "p", TransactionID: "z", AmountVND: 100_000, Content: o.OrderCode})
	require.False(t, res.Duplicate)
	require.Equal(t, types.UsagePaymentMatched, res.Transaction.Status)
}

func TestPaymentRequestValidation(t *testing.T) {
	f := newBillingFixture(t)
	for _, req := range []types.UsagePaymentWebhookRequest{
		{Source: "", TransactionID: "1"},
		{Source: "bad source", TransactionID: "1"},
		{Source: "ok", TransactionID: ""},
		{Source: "ok", TransactionID: "1", Direction: "sideways"},
	} {
		_, err := f.svc.RecordPayment(context.Background(), req, nil)
		require.Error(t, err, "%+v", req)
	}
}
