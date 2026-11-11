package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// The hooks of the SuperAdmin review-queue digests on services the queues
// do not own (decorators).

// billingQueueNotify decorates the billing service with the SuperAdmins'
// payment_order_pending digest (in-app, beside the order email of
// billingEmailNotify): a new pending order updates it; a paid, cancelled
// or reopened order closes it once no order waits any more (an expired one
// by the notifier's sweep). The notifier works in the background.
type billingQueueNotify struct {
	interfaces.UsageBillingService
	notices *SuperAdminQueueNotifier
}

// NewBillingQueueNotifications wraps the billing service (dig.Decorate).
func NewBillingQueueNotifications(
	inner interfaces.UsageBillingService, notices *SuperAdminQueueNotifier,
) interfaces.UsageBillingService {
	return &billingQueueNotify{UsageBillingService: inner, notices: notices}
}

func (b *billingQueueNotify) CreateOrder(
	ctx context.Context, req types.UsageQuoteRequest, scopeID, createdBy string,
) (*types.UsageOrderView, error) {
	v, err := b.UsageBillingService.CreateOrder(ctx, req, scopeID, createdBy)
	if err == nil && v != nil && v.UsageOrder != nil {
		if v.Status == types.UsageOrderPending {
			b.notices.OrderPending(ctx, v, createdBy)
		} else {
			b.notices.OrdersChanged(ctx)
		}
	}
	return v, err
}

func (b *billingQueueNotify) ConfirmOrder(ctx context.Context, id, confirmedBy string) (*types.UsageOrderView, error) {
	v, err := b.UsageBillingService.ConfirmOrder(ctx, id, confirmedBy)
	if err == nil {
		b.notices.OrdersChanged(ctx)
	}
	return v, err
}

func (b *billingQueueNotify) CancelOrder(ctx context.Context, id string, force bool) (*types.UsageOrderView, error) {
	v, err := b.UsageBillingService.CancelOrder(ctx, id, force)
	if err == nil {
		b.notices.OrdersChanged(ctx)
	}
	return v, err
}

// RecordPayment: a matched transfer may have confirmed its order.
func (b *billingQueueNotify) RecordPayment(
	ctx context.Context, req types.UsagePaymentWebhookRequest, raw []byte,
) (*types.UsagePaymentResult, error) {
	r, err := b.UsageBillingService.RecordPayment(ctx, req, raw)
	if err == nil && r != nil && !r.Duplicate {
		b.notices.OrdersChanged(ctx)
	}
	return r, err
}

// ReopenOrder: a reopened order waits for its money again (or was
// confirmed at once by the transfers it already had).
func (b *billingQueueNotify) ReopenOrder(ctx context.Context, id string, force bool) (*types.UsageOrderView, error) {
	v, err := b.UsageBillingService.ReopenOrder(ctx, id, force)
	if err == nil && v != nil && v.UsageOrder != nil {
		if v.Status == types.UsageOrderPending {
			b.notices.OrderPending(ctx, v, types.CallerFromContext(ctx).UserID)
		} else {
			b.notices.OrdersChanged(ctx)
		}
	}
	return v, err
}

// legalRequestQueueNotify decorates the legal request service: a decision
// or a claim may empty the SuperAdmins' legal_request_pending queue.
type legalRequestQueueNotify struct {
	interfaces.LegalRequestService
	notices *SuperAdminQueueNotifier
}

// NewLegalRequestQueueNotifications wraps the legal request service
// (dig.Decorate).
func NewLegalRequestQueueNotifications(
	inner interfaces.LegalRequestService, notices *SuperAdminQueueNotifier,
) interfaces.LegalRequestService {
	return &legalRequestQueueNotify{LegalRequestService: inner, notices: notices}
}

func (d *legalRequestQueueNotify) Decide(
	ctx context.Context, id string, decision *types.LegalRequestDecision,
) (*types.LegalRequestItem, error) {
	item, err := d.LegalRequestService.Decide(ctx, id, decision)
	if err == nil {
		d.notices.LegalRequestsChanged(ctx)
	}
	return item, err
}

func (d *legalRequestQueueNotify) Claim(ctx context.Context, id, action, note string) (*types.LegalRequestItem, error) {
	item, err := d.LegalRequestService.Claim(ctx, id, action, note)
	if err == nil {
		d.notices.LegalRequestsChanged(ctx)
	}
	return item, err
}
