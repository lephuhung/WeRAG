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
	pay   *types.UsagePaymentResult
}

func (b *watchBilling) GetOrder(_ context.Context, id string) (*types.UsageOrderView, error) {
	b.reads++
	v := b.views[id]
	if v.Status == types.UsageOrderPending && v.ExpiresAt != nil && !v.ExpiresAt.After(b.now) {
		v.Status = types.UsageOrderExpired
	}
	return v, nil
}

func (b *watchBilling) RecordPayment(context.Context, types.UsagePaymentWebhookRequest, []byte) (*types.UsagePaymentResult, error) {
	return b.pay, nil
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
	// a 72-hour payment window: told in its last 12 hours
	e.settings.values[types.BillingOrderTTLMinutesSettingKey] = int64(72 * 60)
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

func TestTelegramOrderWatchThirtyMinuteWindow(t *testing.T) {
	e := newTelegramEnv(t)
	ctx := context.Background()
	require.NoError(t, e.db.AutoMigrate(&types.UsageOrder{}))
	e.link(t, 1001, "admin1")
	now := time.Now()
	at := func(d time.Duration) *time.Time { x := now.Add(d); return &x }
	orders := []*types.UsageOrder{
		// 21 minutes old, 9 left: in the last third (10 minutes) → told
		{ID: "late20", OrderCode: "WR20", TotalVND: 49000, Status: types.UsageOrderPending,
			ExpiresAt: at(9 * time.Minute), CreatedAt: now.Add(-21 * time.Minute)},
		// 5 minutes old, 25 left: not yet
		{ID: "fresh", OrderCode: "WR05", TotalVND: 49000, Status: types.UsageOrderPending,
			ExpiresAt: at(25 * time.Minute), CreatedAt: now.Add(-5 * time.Minute)},
	}
	views := map[string]*types.UsageOrderView{}
	for _, o := range orders {
		require.NoError(t, e.db.Create(o).Error)
		cp := *o
		views[o.ID] = &types.UsageOrderView{UsageOrder: &cp}
	}
	w := NewTelegramOrderWatch(e.alerts, &watchBilling{views: views, now: now}, e.db, nil)
	w.now = func() time.Time { return now }
	require.NoError(t, w.Check(ctx))
	msgs := e.messages(t)
	require.Len(t, msgs, 1)
	require.Contains(t, msgs[0].Text, "Đơn WR20 sắp hết hạn thanh toán")
}

func TestTelegramOrderExpiringWithin(t *testing.T) {
	require.Equal(t, 10*time.Minute, types.TelegramOrderExpiringWithin(30*time.Minute))
	require.Equal(t, 12*time.Hour, types.TelegramOrderExpiringWithin(72*time.Hour))
	require.Equal(t, 12*time.Hour, types.TelegramOrderExpiringWithin(0))
}

func TestBillingTelegramMoneyForClosedOrder(t *testing.T) {
	e := newTelegramEnv(t)
	ctx := context.Background()
	e.link(t, 1001, "admin1")
	o := &types.UsageOrderView{UsageOrder: &types.UsageOrder{ID: "o9", OrderCode: "WR9", PlanName: "Cơ bản",
		ScopeType: types.UsageScopeTenant, TotalVND: 49000, Status: types.UsageOrderExpired}, ScopeName: "Sở Y tế"}
	inner := &watchBilling{pay: &types.UsagePaymentResult{
		Transaction: &types.UsagePaymentTransaction{ID: "tx9", AmountVND: 49000, Status: types.UsagePaymentOrderClosed},
		Order:       o,
	}}
	_, err := NewBillingTelegramAlerts(inner, e.alerts).RecordPayment(ctx, types.UsagePaymentWebhookRequest{}, nil)
	require.NoError(t, err)
	require.Eventually(t, func() bool { return len(e.messages(t)) == 1 }, 2*time.Second, 10*time.Millisecond)
	m := e.messages(t)[0]
	require.Equal(t, "order_payment_held:tx9", m.EventKey)
	require.Contains(t, m.Text, "⚠️ Có tiền chuyển vào đơn WR9 đã đóng — cần SuperAdmin xử lý")
	require.Contains(t, m.Text, "Trạng thái đơn: quá hạn, đã tự hủy\nVừa nhận: 49.000 đ")
}

// An order keeps its own window: a 72-hour order created before the
// setting became 30 minutes is told in its last 12 hours, not 10 minutes.
func TestTelegramOrderWatchUsesEachOrdersWindow(t *testing.T) {
	e := newTelegramEnv(t)
	ctx := context.Background()
	require.NoError(t, e.db.AutoMigrate(&types.UsageOrder{}))
	e.link(t, 1001, "admin1")
	now := time.Now()
	exp := now.Add(11 * time.Hour)
	o := &types.UsageOrder{ID: "old72", OrderCode: "WR72", TotalVND: 99000, Status: types.UsageOrderPending,
		ExpiresAt: &exp, CreatedAt: now.Add(-61 * time.Hour)}
	require.NoError(t, e.db.Create(o).Error)
	cp := *o
	w := NewTelegramOrderWatch(e.alerts, &watchBilling{views: map[string]*types.UsageOrderView{"old72": {UsageOrder: &cp}}, now: now}, e.db, nil)
	w.now = func() time.Time { return now }
	require.NoError(t, w.Check(ctx))
	require.Len(t, e.messages(t), 1)
}
