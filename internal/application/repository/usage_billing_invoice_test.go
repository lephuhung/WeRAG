package repository

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
)

// The invoice columns are stored with the order; the SuperAdmin count is
// the paid orders asking for an invoice not issued yet, and the prefill
// lists only the orders that asked for one, newest first.
func TestUsageOrderInvoiceColumns(t *testing.T) {
	db := setupModelUsageTestDB(t)
	require.NoError(t, db.AutoMigrate(&types.UsageOrder{}))
	repo := NewUsageBillingRepository(db)
	ctx := context.Background()
	base := time.Date(2026, 11, 3, 8, 0, 0, 0, time.UTC)
	issued := base
	inv := func(name string) types.UsageOrderInvoice {
		return types.UsageOrderInvoice{InvoiceRequested: true, InvoiceCompanyName: name, InvoiceTaxCode: "0101234567",
			InvoiceAddress: "Huế", InvoiceEmail: "kt@a.vn"}
	}
	issuedInv := inv("C")
	issuedInv.InvoiceIssuedAt, issuedInv.InvoiceNo = &issued, "0000123"
	for i, o := range []*types.UsageOrder{
		{ID: "o1", OrderCode: "WR1", ScopeType: "tenant", ScopeID: "1", Status: types.UsageOrderPaid, UsageOrderInvoice: inv("A")},
		{ID: "o2", OrderCode: "WR2", ScopeType: "tenant", ScopeID: "1", Status: types.UsageOrderPending, UsageOrderInvoice: inv("B")},
		{ID: "o3", OrderCode: "WR3", ScopeType: "tenant", ScopeID: "1", Status: types.UsageOrderPaid, UsageOrderInvoice: issuedInv},
		{ID: "o4", OrderCode: "WR4", ScopeType: "tenant", ScopeID: "1", Status: types.UsageOrderPaid},
	} {
		o.CreatedAt = base.Add(time.Duration(i) * time.Hour)
		require.NoError(t, repo.CreateOrder(ctx, o))
	}
	n, err := repo.CountInvoicesToIssue(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, n)

	list, err := repo.ListOrders(ctx, types.UsageOrderFilter{ScopeType: "tenant", ScopeID: "1", InvoiceRequested: true})
	require.NoError(t, err)
	require.Len(t, list, 3)
	require.Equal(t, "o3", list[0].ID)
	require.Equal(t, "0000123", list[0].InvoiceNo)
	require.NotNil(t, list[0].InvoiceIssuedAt)

	// UpdateOrder writes the invoice columns too (withdrawing clears them)
	o, err := repo.GetOrder(ctx, "o2")
	require.NoError(t, err)
	require.Equal(t, "B", o.InvoiceCompanyName)
	o.ClearInvoiceRequest()
	require.NoError(t, repo.UpdateOrder(ctx, o))
	o, err = repo.GetOrder(ctx, "o2")
	require.NoError(t, err)
	require.False(t, o.InvoiceRequested)
	require.Empty(t, o.InvoiceCompanyName)
}
