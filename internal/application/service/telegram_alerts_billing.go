package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// Payment-order alerts on the Telegram bot beside the in-app order digest
// (which already tells the new orders): an order paid, a transfer short of
// the amount, an order about to expire, an order expired.

// billingTelegramNotify decorates the billing service with the paid and
// underpaid alerts (composed by NewBillingNotifications).
type billingTelegramNotify struct {
	interfaces.UsageBillingService
	alerts *TelegramAlerts
}

// NewBillingTelegramAlerts wraps the billing service with the Telegram
// order alerts.
func NewBillingTelegramAlerts(inner interfaces.UsageBillingService, alerts *TelegramAlerts) interfaces.UsageBillingService {
	if alerts == nil {
		return inner
	}
	return &billingTelegramNotify{UsageBillingService: inner, alerts: alerts}
}

func (b *billingTelegramNotify) CreateOrder(
	ctx context.Context, req types.UsageQuoteRequest, scopeID, createdBy string,
) (*types.UsageOrderView, error) {
	v, err := b.UsageBillingService.CreateOrder(ctx, req, scopeID, createdBy)
	if err == nil {
		b.paid(ctx, v, "")
	}
	return v, err
}

func (b *billingTelegramNotify) ConfirmOrder(ctx context.Context, id, confirmedBy string) (*types.UsageOrderView, error) {
	v, err := b.UsageBillingService.ConfirmOrder(ctx, id, confirmedBy)
	if err == nil {
		b.paid(ctx, v, "SuperAdmin xác nhận")
	}
	return v, err
}

func (b *billingTelegramNotify) ReopenOrder(ctx context.Context, id string, force bool) (*types.UsageOrderView, error) {
	v, err := b.UsageBillingService.ReopenOrder(ctx, id, force)
	if err == nil {
		b.paid(ctx, v, "mở lại đơn, tiền đã chuyển đủ")
	}
	return v, err
}

// RecordPayment: a transfer matched (the order paid) or short of the
// amount; a resend of a stored transfer is not told again.
func (b *billingTelegramNotify) RecordPayment(
	ctx context.Context, req types.UsagePaymentWebhookRequest, raw []byte,
) (*types.UsagePaymentResult, error) {
	r, err := b.UsageBillingService.RecordPayment(ctx, req, raw)
	if err != nil || r == nil || r.Duplicate || r.Transaction == nil || r.Order == nil {
		return r, err
	}
	switch r.Transaction.Status {
	case types.UsagePaymentMatched:
		b.paid(ctx, r.Order, "chuyển khoản tự khớp")
	case types.UsagePaymentUnderpaid:
		o := r.Order
		// the webhook's order view does not sum the transfers; the order view does
		if full, gerr := b.UsageBillingService.GetOrder(ctx, o.ID); gerr == nil && full != nil && full.UsageOrder != nil {
			o = full
		}
		b.alerts.Send(ctx, TelegramAlert{
			Kind: types.TelegramAlertOrderUnderpaid, Key: "order_underpaid:" + r.Transaction.ID,
			Title: fmt.Sprintf("Đơn %s chuyển thiếu tiền", o.OrderCode),
			Body: telegramOrderLines(o,
				fmt.Sprintf("Vừa nhận: %s", formatVND(r.Transaction.AmountVND)),
				fmt.Sprintf("Đã nhận tổng cộng: %s / %s", formatVND(o.ReceivedVND), formatVND(o.TotalVND))),
			Link: telegramOrderLink(o),
		})
	case types.UsagePaymentOrderClosed:
		// money for an order that is closed (expired past the grace, paid,
		// cancelled): it waits for a SuperAdmin
		o := r.Order
		b.alerts.Send(ctx, TelegramAlert{
			Kind: types.TelegramAlertOrderPaymentHeld, Key: "order_payment_held:" + r.Transaction.ID,
			Title: fmt.Sprintf("Có tiền chuyển vào đơn %s đã đóng — cần SuperAdmin xử lý", o.OrderCode),
			Body: telegramOrderLines(o,
				"Trạng thái đơn: "+telegramOrderStatusVN(o.Status),
				fmt.Sprintf("Vừa nhận: %s", formatVND(r.Transaction.AmountVND)),
				"Mở lại đơn (nút \"Mở lại\") để ghi nhận số tiền này, hoặc hoàn tiền cho người mua."),
			Link: telegramOrderLink(o),
		})
	}
	return r, err
}

// telegramOrderStatusVN names an order status in Vietnamese.
func telegramOrderStatusVN(status string) string {
	switch status {
	case types.UsageOrderPending:
		return "chờ thanh toán"
	case types.UsageOrderPaid:
		return "đã thanh toán"
	case types.UsageOrderExpired:
		return "quá hạn, đã tự hủy"
	case types.UsageOrderCancelled:
		return "đã hủy"
	}
	return status
}

func (b *billingTelegramNotify) paid(ctx context.Context, v *types.UsageOrderView, how string) {
	if v == nil || v.UsageOrder == nil || v.Status != types.UsageOrderPaid {
		return
	}
	extra := []string{}
	if how != "" {
		extra = append(extra, "Cách xác nhận: "+how)
	}
	b.alerts.Send(ctx, TelegramAlert{
		Kind: types.TelegramAlertOrderPaid, Key: telegramOrderKey("order_paid", v.ID, v.PaidAt),
		Title: fmt.Sprintf("Đơn %s đã thanh toán", v.OrderCode),
		Body:  telegramOrderLines(v, extra...),
		Link:  telegramOrderLink(v),
	})
}

// telegramOrderKey names an order alert with its episode (the deadline or
// payment time): a reopened order that expires again is told again.
func telegramOrderKey(prefix, id string, at *time.Time) string {
	stamp := int64(0)
	if at != nil {
		stamp = at.Unix()
	}
	return fmt.Sprintf("%s:%s:%d", prefix, id, stamp)
}

// telegramOrderLines: who, what, how much (and extra lines).
func telegramOrderLines(o *types.UsageOrderView, extra ...string) string {
	lines := []string{
		orderScopeLabel(o),
		"Gói: " + orderWhatLabel(o),
		"Số tiền: " + formatVND(o.TotalVND),
	}
	lines = append(lines, extra...)
	return strings.Join(lines, "\n")
}

// telegramOrderLink opens the SuperAdmin order list filtered to the
// order's status (the page has no per-order link).
func telegramOrderLink(o *types.UsageOrderView) string {
	status := o.Status
	if status == "" {
		status = types.UsageOrderPending
	}
	return "/platform/admin?section=billing&status=" + status
}

// TelegramOrderWatch finds the pending orders about to expire and those
// that expired, for the Telegram bot (the billing service expires orders
// lazily, when they are read: the watch reads them through it, so an
// expired order is cancelled on time too).
type TelegramOrderWatch struct {
	alerts  *TelegramAlerts
	billing interfaces.UsageBillingService
	db      *gorm.DB
	rdb     *redis.Client
	now     func() time.Time
}

const (
	// TelegramOrderWatchInterval is how often the watch looks: a payment
	// window is 30 minutes by default, so a minute keeps the alerts on time.
	TelegramOrderWatchInterval = time.Minute
	// telegramOrderExpiredLookback: orders expired longer ago than this
	// are not told (turning the bot on does not report old orders).
	telegramOrderExpiredLookback = 24 * time.Hour
	telegramOrderWatchBatch      = 50
	// telegramOrderWatchScan bounds the orders read per look (told ones
	// are skipped, so newer ones are not starved)
	telegramOrderWatchScan = 1000
)

// NewTelegramOrderWatch creates the watch.
func NewTelegramOrderWatch(alerts *TelegramAlerts, billing interfaces.UsageBillingService, db *gorm.DB, rdb *redis.Client) *TelegramOrderWatch {
	return &TelegramOrderWatch{alerts: alerts, billing: billing, db: db, rdb: rdb, now: time.Now}
}

// Start runs the watch every interval; the returned func stops it.
func (w *TelegramOrderWatch) Start(interval time.Duration) func() {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := w.Check(ctx); err != nil && ctx.Err() == nil {
					logger.Warnf(ctx, "[TelegramOrderWatch] %v", err)
				}
			}
		}
	}()
	return func() { cancel(); <-done }
}

// Check queues the alerts of the orders about to expire and just expired.
func (w *TelegramOrderWatch) Check(ctx context.Context) error {
	if w == nil || !w.alerts.active(ctx) {
		return nil
	}
	release, ok := monitorLock(ctx, w.rdb, "telegram_alerts:order_watch", 2*time.Minute, "TelegramOrderWatch")
	if !ok {
		return nil
	}
	defer release()
	now := w.now()
	type candidate struct {
		ID        string
		ExpiresAt *time.Time
		CreatedAt time.Time
	}
	var soonish, expiring, expired []candidate
	// the widest "about to expire" span is 12 hours; each order's own span
	// is the last third of its window (created → deadline), whatever the
	// setting says now
	if err := w.db.WithContext(ctx).Model(&types.UsageOrder{}).
		Where("status = ? AND total_vnd > 0 AND expires_at > ? AND expires_at <= ?",
			types.UsageOrderPending, now, now.Add(types.TelegramOrderExpiringWithin(0))).
		Order("expires_at").Limit(telegramOrderWatchScan).Select("id", "expires_at", "created_at").Scan(&soonish).Error; err != nil {
		return fmt.Errorf("listing orders about to expire: %w", err)
	}
	for _, c := range soonish {
		if c.ExpiresAt == nil {
			continue
		}
		within := types.TelegramOrderExpiringWithin(c.ExpiresAt.Sub(c.CreatedAt))
		if c.ExpiresAt.Sub(now) <= within && now.Sub(c.CreatedAt) >= within {
			expiring = append(expiring, c)
		}
	}
	if err := w.db.WithContext(ctx).Model(&types.UsageOrder{}).
		Where("status IN ? AND total_vnd > 0 AND expires_at <= ? AND expires_at > ?",
			[]string{types.UsageOrderPending, types.UsageOrderExpired}, now, now.Add(-telegramOrderExpiredLookback)).
		Order("expires_at").Limit(telegramOrderWatchScan).Select("id", "expires_at").Scan(&expired).Error; err != nil {
		return fmt.Errorf("listing expired orders: %w", err)
	}
	// the orders not told yet, at most a batch per look
	todo := func(list []candidate, prefix string) ([]string, error) {
		keys := make([]string, 0, len(list))
		for _, c := range list {
			keys = append(keys, telegramOrderKey(prefix, c.ID, c.ExpiresAt))
		}
		if len(keys) == 0 {
			return nil, nil
		}
		var told []string
		if err := w.db.WithContext(ctx).Model(&types.TelegramAlertMessage{}).
			Where("event_key IN ?", keys).Distinct().Pluck("event_key", &told).Error; err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		for _, k := range told {
			seen[k] = true
		}
		var out []string
		for i, k := range keys {
			if !seen[k] && len(out) < telegramOrderWatchBatch {
				out = append(out, list[i].ID)
			}
		}
		return out, nil
	}
	soon, err := todo(expiring, "order_expiring")
	if err != nil {
		return err
	}
	late, err := todo(expired, "order_expired")
	if err != nil {
		return err
	}
	for _, id := range soon {
		w.tell(ctx, id, false)
	}
	for _, id := range late {
		w.tell(ctx, id, true)
	}
	return nil
}

func (w *TelegramOrderWatch) tell(ctx context.Context, id string, expired bool) {
	kind, prefix := types.TelegramAlertOrderExpiring, "order_expiring"
	if expired {
		kind, prefix = types.TelegramAlertOrderExpired, "order_expired"
	}
	if w.alerts.muted(ctx, kind) {
		return
	}
	// read through the billing service: a due order is expired by the read
	o, err := w.billing.GetOrder(ctx, id)
	if err != nil || o == nil || o.UsageOrder == nil {
		if err != nil {
			logger.Warnf(ctx, "[TelegramOrderWatch] reading order %s: %v", id, err)
		}
		return
	}
	key := telegramOrderKey(prefix, o.ID, o.ExpiresAt)
	alert := TelegramAlert{Kind: kind, Key: key, Link: telegramOrderLink(o)}
	deadline := ""
	if o.ExpiresAt != nil {
		deadline = formatDateTimeVN(*o.ExpiresAt)
	}
	switch {
	case expired && o.Status == types.UsageOrderExpired:
		alert.Title = fmt.Sprintf("Đơn %s quá hạn thanh toán, đã tự hủy", o.OrderCode)
		extra := []string{"Hạn thanh toán: " + deadline, "Tạo lúc: " + formatDateTimeVN(o.CreatedAt)}
		if o.ReceivedVND > 0 {
			extra = append(extra, fmt.Sprintf("Đơn đã nhận %s — cần SuperAdmin xử lý số tiền này.", formatVND(o.ReceivedVND)))
		}
		alert.Body = telegramOrderLines(o, extra...)
	case !expired && o.Status == types.UsageOrderPending:
		alert.Title = fmt.Sprintf("Đơn %s sắp hết hạn thanh toán", o.OrderCode)
		extra := []string{"Hạn thanh toán: " + deadline}
		if o.ReceivedVND > 0 {
			extra = append(extra, fmt.Sprintf("Đã nhận: %s / %s", formatVND(o.ReceivedVND), formatVND(o.TotalVND)))
		}
		alert.Body = telegramOrderLines(o, extra...)
	default:
		return // paid, cancelled or reopened meanwhile
	}
	if _, err := w.alerts.Enqueue(ctx, alert); err != nil {
		logger.Warnf(ctx, "[TelegramOrderWatch] queueing %s: %v", key, err)
	}
}
