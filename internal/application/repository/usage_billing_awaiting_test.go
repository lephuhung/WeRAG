package repository

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
)

// The SuperAdmin badge counts the orders only a SuperAdmin can move on:
// pending at 0 đồng, pending holding money that did not confirm it, and an
// expired order holding money (a transfer that came after it expired is
// order_closed). The admin list's awaiting filter returns the same orders.
func TestOrdersAwaitingAdminCountAndFilterAgree(t *testing.T) {
	db := setupKBTestDB(t)
	require.NoError(t, db.AutoMigrate(&types.UsageOrder{}, &types.UsagePaymentTransaction{}))
	require.NoError(t, db.Exec("CREATE UNIQUE INDEX ux_pay_src_ext ON usage_payment_transactions (source, external_id)").Error)
	repo := NewUsageBillingRepository(db)
	ctx := context.Background()
	base := time.Date(2026, 11, 3, 8, 0, 0, 0, time.UTC)
	orders := []*types.UsageOrder{
		{ID: "free", OrderCode: "WRFREE", Status: types.UsageOrderPending, TotalVND: 0},
		{ID: "open", OrderCode: "WROPEN", Status: types.UsageOrderPending, TotalVND: 100},
		{ID: "short", OrderCode: "WRSHORT", Status: types.UsageOrderPending, TotalVND: 100},
		{ID: "late", OrderCode: "WRLATE", Status: types.UsageOrderExpired, TotalVND: 100},
		{ID: "exp", OrderCode: "WREXP", Status: types.UsageOrderExpired, TotalVND: 100},
		{ID: "paid", OrderCode: "WRPAID", Status: types.UsageOrderPaid, TotalVND: 100},
	}
	for i, o := range orders {
		o.ScopeType, o.ScopeID, o.Kind = "tenant", "1", types.UsageOrderKindPlan
		o.CreatedAt = base.Add(time.Duration(i) * time.Minute)
		require.NoError(t, repo.CreateOrder(ctx, o))
	}
	pay := func(id, orderID, status string) {
		oid := orderID
		_, err := repo.CreatePaymentTransaction(ctx, &types.UsagePaymentTransaction{
			ID: id, Source: "test", ExternalID: id, AmountVND: 50, OrderID: &oid, Status: status,
			TransactedAt: base, CreatedAt: base, UpdatedAt: base,
		})
		require.NoError(t, err)
	}
	pay("t1", "short", types.UsagePaymentUnderpaid)
	pay("t2", "late", types.UsagePaymentOrderClosed)
	pay("t3", "paid", types.UsagePaymentMatched)

	n, err := repo.CountOrdersAwaitingAdmin(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 3, n)

	list, err := repo.ListOrders(ctx, types.UsageOrderFilter{AwaitingAdmin: true})
	require.NoError(t, err)
	var ids []string
	for _, o := range list {
		ids = append(ids, o.ID)
	}
	require.ElementsMatch(t, []string{"free", "short", "late"}, ids)

	// combined with another filter the OR stays grouped
	list, err = repo.ListOrders(ctx, types.UsageOrderFilter{AwaitingAdmin: true, Status: types.UsageOrderExpired})
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, "late", list[0].ID)
}
