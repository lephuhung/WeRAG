package service

import (
	"context"
	"errors"
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
// order went into, and for a pending one what short transfers brought
// (the QR code then asks for the rest).
func (s *usageBillingService) detailView(ctx context.Context, o *types.UsageOrder) (*types.UsageOrderView, error) {
	v := s.view(ctx, o)
	pays, err := s.repo.ListOrderPayments(ctx, o.ID)
	if err != nil {
		return nil, err
	}
	if len(pays) > 0 {
		p := pays[0]
		v.LastPayment = &types.UsageOrderPayment{AmountVND: p.AmountVND, Status: p.Status, TransactedAt: p.TransactedAt}
	}
	if o.Status == types.UsageOrderPending {
		for _, p := range pays {
			if p.Status == types.UsagePaymentUnderpaid {
				v.ReceivedVND += p.AmountVND
			}
		}
		if v.ReceivedVND > 0 && v.ReceivedVND < o.TotalVND {
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

// shortPayments returns the transfers for orderID that were too short to
// confirm it on their own, and their sum.
func (s *usageBillingService) shortPayments(
	ctx context.Context, orderID string,
) ([]*types.UsagePaymentTransaction, int64, error) {
	pays, err := s.repo.ListOrderPayments(ctx, orderID)
	if err != nil {
		return nil, 0, err
	}
	var out []*types.UsagePaymentTransaction
	var sum int64
	for _, p := range pays {
		if p.Status == types.UsagePaymentUnderpaid {
			out = append(out, p)
			sum += p.AmountVND
		}
	}
	return out, sum, nil
}

func (s *usageBillingService) CountPendingOrders(ctx context.Context) (int64, error) {
	if err := s.expireDue(ctx); err != nil {
		return 0, err
	}
	return s.repo.CountOrders(ctx, types.UsageOrderFilter{Status: types.UsageOrderPending})
}

func (s *usageBillingService) ReopenOrder(ctx context.Context, id string) (*types.UsageOrderView, error) {
	if err := s.expireDue(ctx); err != nil {
		return nil, err
	}
	var o *types.UsageOrder
	err := s.repo.InTransaction(ctx, func(ctx context.Context) error {
		var err error
		o, err = s.repo.GetOrderForUpdate(ctx, id)
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
		// another pending order may have bought the same add-on since
		if err := s.refusePendingAddons(ctx, o.ScopeType, o.ScopeID, o.Addons); err != nil {
			return err
		}
		o.ExpiresAt = s.paymentDeadline(ctx, o.TotalVND, s.now())
		return s.reopenExpiredOrder(ctx, o)
	})
	if err != nil {
		return nil, err
	}
	return s.detailView(ctx, o)
}
