package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
)

// Order lifecycle around the payment page: the payment deadline, the
// pending-plan-order guard, the single-order view and SuperAdmin reopening.

// paymentDeadline is when a pending order of total expires: the order TTL
// from now, none for a 0-đồng order (it waits for a SuperAdmin, the buyer
// has nothing to pay) or when the TTL setting is off.
func (s *usageBillingService) paymentDeadline(ctx context.Context, total int64, now time.Time) *time.Time {
	if total <= 0 {
		return nil
	}
	ttl := s.settings.GetInt(ctx, types.BillingOrderTTLHoursSettingKey, "", 72)
	if ttl <= 0 {
		return nil
	}
	exp := now.Add(time.Duration(ttl) * time.Hour)
	return &exp
}

// refusePendingPlanOrder refuses a plan order while the scope has another
// one waiting for payment: paying both would apply both. The buyer may
// confirm and order again (UsageQuoteRequest.ConfirmDuplicate).
func (s *usageBillingService) refusePendingPlanOrder(ctx context.Context, scopeType, scopeID string) error {
	pending, err := s.repo.ListOrders(ctx, types.UsageOrderFilter{
		ScopeType: scopeType, ScopeID: scopeID,
		Status: types.UsageOrderPending, Kind: types.UsageOrderKindPlan, Limit: 1,
	})
	if err != nil {
		return err
	}
	if len(pending) == 0 {
		return nil
	}
	return apperrors.NewConflictError("order " + pending[0].OrderCode + " is already waiting for payment; confirm to place another one").
		WithDetails(map[string]string{"reason": "plan_order_pending", "order_code": pending[0].OrderCode})
}

// detailView is the single-order view: the end of the package a paid
// order went into, and for a pending one the money its transfers brought.
// While short, the QR code asks for the rest; once a transfer was refused
// at confirmation (or the money covers the total without confirming it)
// there is nothing more to transfer: LastPayment is then that refused
// transfer and the page says a SuperAdmin is handling it.
func (s *usageBillingService) detailView(ctx context.Context, o *types.UsageOrder) (*types.UsageOrderView, error) {
	v := s.view(ctx, o)
	pays, err := s.repo.ListOrderPayments(ctx, o.ID)
	if err != nil {
		return nil, err
	}
	if len(pays) > 0 {
		v.LastPayment = paymentBrief(pays[0])
	}
	if o.Status == types.UsageOrderPending {
		_, v.ReceivedVND = heldPayments(pays, "", types.UsagePaymentHeldStatuses...)
		var failed *types.UsagePaymentTransaction
		for _, p := range pays {
			if p.Status == types.UsagePaymentConfirmFailed {
				failed = p
				break
			}
		}
		switch {
		case failed != nil:
			v.LastPayment = paymentBrief(failed)
			v.QRURL = ""
		case v.ReceivedVND >= o.TotalVND && v.ReceivedVND > 0:
			v.QRURL = ""
		case v.ReceivedVND > 0:
			v.QRURL = vietQRURL(s.BillingInfo(ctx), o.TotalVND-v.ReceivedVND, o.OrderCode)
		}
	}
	if o.Status == types.UsageOrderPaid && o.SubscriptionID != nil {
		sub, err := s.planRepo.GetSubscription(ctx, *o.SubscriptionID)
		switch {
		case err == nil && sub != nil && sub.Status == types.UsageSubscriptionActive && sub.EndsAt != nil:
			e := *sub.EndsAt
			v.SubscriptionEndsAt = &e
		case err != nil && !errors.Is(err, gorm.ErrRecordNotFound):
			return nil, err
		}
	}
	return v, nil
}

func paymentBrief(p *types.UsagePaymentTransaction) *types.UsageOrderPayment {
	return &types.UsageOrderPayment{AmountVND: p.AmountVND, Status: p.Status, TransactedAt: p.TransactedAt}
}

// addReceived fills ReceivedVND on the unpaid orders of a listing (one
// query for the whole page).
func (s *usageBillingService) addReceived(ctx context.Context, views []*types.UsageOrderView) error {
	var ids []string
	for _, v := range views {
		if v.Status != types.UsageOrderPaid {
			ids = append(ids, v.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	received, err := s.repo.ReceivedByOrder(ctx, ids)
	if err != nil {
		return err
	}
	for _, v := range views {
		if v.Status != types.UsageOrderPaid {
			v.ReceivedVND = received[v.ID]
		}
	}
	return nil
}

// refuseCancelWithMoney refuses cancelling an order that has received
// money: the money would be orphaned and the transfer completing it would
// find the order closed. Only a SuperAdmin may force it.
func (s *usageBillingService) refuseCancelWithMoney(ctx context.Context, o *types.UsageOrder) error {
	pays, err := s.repo.ListOrderPayments(ctx, o.ID)
	if err != nil {
		return err
	}
	_, received := heldPayments(pays, "", types.UsagePaymentHeldStatuses...)
	if received <= 0 {
		return nil
	}
	return apperrors.NewConflictError(fmt.Sprintf(
		"order %s has received %d VND and cannot be cancelled; contact the system administrator", o.OrderCode, received,
	)).WithDetails(map[string]interface{}{"reason": "payment_received", "received_vnd": received})
}

func (s *usageBillingService) CountOrdersAwaitingAdmin(ctx context.Context) (int64, error) {
	if err := s.expireDue(ctx); err != nil {
		return 0, err
	}
	return s.repo.CountOrdersAwaitingAdmin(ctx)
}

func (s *usageBillingService) ReopenOrder(ctx context.Context, id string, force bool) (*types.UsageOrderView, error) {
	if err := s.expireDue(ctx); err != nil {
		return nil, err
	}
	err := s.repo.InTransaction(ctx, func(ctx context.Context) error {
		o, err := s.repo.GetOrderForUpdate(ctx, id)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return apperrors.NewNotFoundError("order not found")
		}
		if err != nil {
			return err
		}
		if o.Status != types.UsageOrderExpired {
			return apperrors.NewConflictError("only expired orders can be reopened").
				WithDetails(map[string]string{"reason": "order_not_expired"})
		}
		if err := s.repo.LockScope(ctx, o.ScopeType, o.ScopeID); err != nil {
			return err
		}
		// a newer plan order may be waiting: paying both would apply both
		switch o.Kind {
		case types.UsageOrderKindAddon:
		case types.UsageOrderKindSeats:
			// a newer seat order may be waiting (no override: see CreateOrder)
			if err := s.refusePendingSeatOrder(ctx, o.ScopeType, o.ScopeID); err != nil {
				return err
			}
		default:
			if !force {
				if err := s.refusePendingPlanOrder(ctx, o.ScopeType, o.ScopeID); err != nil {
					return err
				}
			}
		}
		// another pending order may have bought the same add-on since (a
		// seat order's lines only pay its seats)
		if o.Kind != types.UsageOrderKindSeats {
			if err := s.refusePendingAddons(ctx, o.ScopeType, o.ScopeID, o.Addons); err != nil {
				return err
			}
		}
		o.ExpiresAt = s.paymentDeadline(ctx, o.TotalVND, s.now())
		if err := s.reopenExpiredOrder(ctx, o); err != nil {
			return err
		}
		return s.resettle(ctx, o)
	})
	if err != nil {
		return nil, err
	}
	o, err := s.repo.GetOrder(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.detailView(ctx, o)
}

// resettle counts again, for an order just reopened, the transfers
// recorded while it was closed or short: they now count towards it, and
// when one source's add up to the total they confirm it (a refusal is kept
// on the transfers, the order stays pending).
func (s *usageBillingService) resettle(ctx context.Context, o *types.UsageOrder) error {
	pays, err := s.repo.ListOrderPayments(ctx, o.ID)
	if err != nil {
		return err
	}
	var sources []string
	seen := map[string]bool{}
	for _, p := range pays {
		if p.Status == types.UsagePaymentOrderClosed {
			p.Status, p.Reason = types.UsagePaymentUnderpaid, "counted when the order was reopened"
			if err := s.repo.UpdatePaymentTransaction(ctx, p); err != nil {
				return err
			}
		}
		if p.Status == types.UsagePaymentUnderpaid && !seen[p.Source] {
			seen[p.Source] = true
			sources = append(sources, p.Source)
		}
	}
	if o.TotalVND <= 0 {
		return nil
	}
	for _, src := range sources {
		rows, sum := heldPayments(pays, src, types.UsagePaymentUnderpaid)
		if sum >= o.TotalVND {
			// oldest first, so the newest transfer completes the order
			for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
				rows[i], rows[j] = rows[j], rows[i]
			}
			_, err := s.confirmPaid(ctx, o, rows, paymentConfirmedBy(src), false)
			return err
		}
	}
	return nil
}
