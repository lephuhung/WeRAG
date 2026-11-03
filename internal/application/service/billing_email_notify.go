package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// billingEmailNotify decorates the billing service with the email
// notifications: a new order (to the SuperAdmins) and a paid one (to the
// buyer), whether confirmed by a SuperAdmin or by the payment webhook.
// The notifier sends in the background, so the decorated calls never wait
// for or fail on a mail.
type billingEmailNotify struct {
	interfaces.UsageBillingService
	notifier interfaces.BillingNotifier
}

// NewBillingEmailNotifications wraps the billing service (dig.Decorate).
func NewBillingEmailNotifications(
	inner interfaces.UsageBillingService, notifier *EmailNotifier,
) interfaces.UsageBillingService {
	return &billingEmailNotify{UsageBillingService: inner, notifier: notifier}
}

func (b *billingEmailNotify) CreateOrder(
	ctx context.Context, req types.UsageQuoteRequest, scopeID, createdBy string,
) (*types.UsageOrderView, error) {
	v, err := b.UsageBillingService.CreateOrder(ctx, req, scopeID, createdBy)
	if err == nil && v != nil {
		b.notifier.OrderCreated(ctx, v)
		if v.Status == types.UsageOrderPaid {
			b.notifier.OrderPaid(ctx, v)
		}
	}
	return v, err
}

func (b *billingEmailNotify) ConfirmOrder(ctx context.Context, id, confirmedBy string) (*types.UsageOrderView, error) {
	v, err := b.UsageBillingService.ConfirmOrder(ctx, id, confirmedBy)
	if err == nil && v != nil {
		b.notifier.OrderPaid(ctx, v)
	}
	return v, err
}

// RecordPayment: a transfer the webhook matched confirmed its order (a
// resend reports the stored result and is not mailed again).
func (b *billingEmailNotify) RecordPayment(
	ctx context.Context, req types.UsagePaymentWebhookRequest, raw []byte,
) (*types.UsagePaymentResult, error) {
	r, err := b.UsageBillingService.RecordPayment(ctx, req, raw)
	if err == nil && r != nil && !r.Duplicate && r.Transaction != nil &&
		r.Transaction.Status == types.UsagePaymentMatched && r.Order != nil {
		b.notifier.OrderPaid(ctx, r.Order)
	}
	return r, err
}
