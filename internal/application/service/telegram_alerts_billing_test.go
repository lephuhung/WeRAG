package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// watchBilling answers GetOrder from views, expiring a due pending order
// like the billing service's read does.
type watchBilling struct {
	interfaces.UsageBillingService
	views map[string]*types.UsageOrderView
	now   time.Time
	reads int
}

func (b *watchBilling) GetOrder(_ context.Context, id string) (*types.UsageOrderView, error) {
	b.reads++
	v := b.views[id]
	if v.Status == types.UsageOrderPending && v.ExpiresAt != nil && !v.ExpiresAt.After(b.now) {
		v.Status = types.UsageOrderExpired
	}
	return v, nil
}

func (b *watchBilling) ConfirmOrder(_ context.Context, id, _ string) (*types.UsageOrderView, error) {
	v := b.views[id]
	v.Status = types.UsageOrderPaid
	return v, nil
}

func TestTelegramOrderWatch(t *testing.T) {
	e := newTelegramEnv(t)
	ctx := context.Background()
	require.NoError(t, e.db.AutoMigrate(&types.UsageOrder{}))
	e.link(t, 1001, "admin1")
	now := time.Now()
	at := func(d time.Duration) *time.Time { x := now.Add(d); return &x }
	orders := []*types.UsageOrder{
		// created 64 h ago, expires in 8 h: about to expire
		{ID: "soon", OrderCode: "WRSOON", PlanName: "Cơ bản", ScopeType: types.UsageScopeTenant, TotalVND: 199000,
			Status: types.UsageOrderPending, ExpiresAt: at(8 * time.Hour), CreatedAt: now.Add(-64 * time.Hour)},
		// created 1 h ago with a 6 h TTL: not old enough to remind
		{ID: "young", OrderCode: "WRYOUNG", TotalVND: 99000, Status: types.UsageOrderPending,
			ExpiresAt: at(5 * time.Hour), CreatedAt: now.Add(-time.Hour)},
		// deadline passed 10 minutes ago, still pending in the table (lazy expiry)
		{ID: "late", OrderCode: "WRLATE", PlanName: "Nâng cao", ScopeType: types.UsageScopeUser, TotalVND: 499000,
			Status: types.UsageOrderPending, ExpiresAt: at(-10 * time.Minute), CreatedAt: now.Add(-72 * time.Hour)},
		// expired two days ago: too old to tell
		{ID: "old", OrderCode: "WROLD", TotalVND: 99000, Status: types.UsageOrderExpired,
			ExpiresAt: at(-48 * time.Hour), CreatedAt: now.Add(-120 * time.Hour)},
		// 0-đồng order: never expires, never told
		{ID: "free", OrderCode: "WRFREE", TotalVND: 0, Status: types.UsageOrderPending, CreatedAt: now.Add(-100 * time.Hour)},
	}
	views := map[string]*types.UsageOrderView{}
	for _, o := range orders {
		require.NoError(t, e.db.Create(o).Error)
		cp := *o
		views[o.ID] = &types.UsageOrderView{UsageOrder: &cp, ScopeName: "Sở Tư pháp"}
	}
	views["late"].ReceivedVND = 100000
	billing := &watchBilling{views: views, now: now}
	w := NewTelegramOrderWatch(e.alerts, billing, e.db, nil)
	w.now = func() time.Time { return now }

	require.NoError(t, w.Check(ctx))
	msgs := e.messages(t)
	require.Len(t, msgs, 2)
	texts := map[string]string{}
	for _, m := range msgs {
		id, _, _ := strings.Cut(strings.SplitN(m.EventKey, ":", 2)[1], ":")
		texts[strings.SplitN(m.EventKey, ":", 2)[0]+":"+id] = m.Text
	}
	require.Contains(t, texts["order_expiring:soon"], "⏰ Đơn WRSOON sắp hết hạn thanh toán\nTổ chức Sở Tư pháp")
	require.Contains(t, texts["order_expiring:soon"], "status=pending")
	require.Contains(t, texts["order_expired:late"], "Đơn WRLATE quá hạn thanh toán, đã tự hủy")
	require.Contains(t, texts["order_expired:late"], "Đơn đã nhận 100.000 đ — cần SuperAdmin xử lý")
	require.Contains(t, texts["order_expired:late"], "status=expired")

	// told once: the next look reads no order again
	reads := billing.reads
	require.NoError(t, w.Check(ctx))
	require.Len(t, e.messages(t), 2)
	require.Equal(t, reads, billing.reads)

	// reopened with a new deadline, expired again: told again
	newDeadline := now.Add(-5 * time.Minute)
	require.NoError(t, e.db.Model(&types.UsageOrder{}).Where("id = ?", "late").Update("expires_at", newDeadline).Error)
	billing.views["late"].ExpiresAt = &newDeadline
	billing.views["late"].Status = types.UsageOrderPending
	require.NoError(t, w.Check(ctx))
	require.Len(t, e.messages(t), 3)

	// an order paid meanwhile is not told expiring
	billing.views["soon2"] = &types.UsageOrderView{UsageOrder: &types.UsageOrder{ID: "soon2", Status: types.UsageOrderPaid}}
	require.NoError(t, e.db.Create(&types.UsageOrder{ID: "soon2", OrderCode: "WRS2", TotalVND: 1, Status: types.UsageOrderPending,
		ExpiresAt: at(time.Hour), CreatedAt: now.Add(-70 * time.Hour)}).Error)
	require.NoError(t, w.Check(ctx))
	require.Len(t, e.messages(t), 3)
}

func TestBillingTelegramPaidAlert(t *testing.T) {
	e := newTelegramEnv(t)
	ctx := context.Background()
	e.link(t, 1001, "admin1")
	inner := &watchBilling{views: map[string]*types.UsageOrderView{
		"o1": {UsageOrder: &types.UsageOrder{ID: "o1", OrderCode: "WR1", PlanName: "Cơ bản", ScopeType: types.UsageScopeTenant,
			TotalVND: 199000, Status: types.UsageOrderPending}, ScopeName: "Sở Nội vụ"},
	}}
	b := NewBillingTelegramAlerts(inner, e.alerts)
	_, err := b.ConfirmOrder(ctx, "o1", "admin1")
	require.NoError(t, err)
	require.Eventually(t, func() bool { return len(e.messages(t)) == 1 }, 2*time.Second, 10*time.Millisecond)
	m := e.messages(t)[0]
	require.Equal(t, "order_paid:o1:0", m.EventKey)
	require.Contains(t, m.Text, "💰 Đơn WR1 đã thanh toán\nTổ chức Sở Nội vụ\nGói: Cơ bản")
	require.Contains(t, m.Text, "Số tiền: 199.000 đ\nCách xác nhận: SuperAdmin xác nhận")
}
