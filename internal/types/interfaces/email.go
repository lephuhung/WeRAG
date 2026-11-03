package interfaces

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

// EmailNotificationRepository stores the once-only notifications
// (email_notifications) and finds the packages that are due a reminder.
type EmailNotificationRepository interface {
	// Claim inserts the row unless one with the same DedupeKey exists;
	// false = already claimed (sent, or being sent by another backend).
	Claim(ctx context.Context, n *types.EmailNotification) (bool, error)
	// Release deletes the claim of dedupeKey so a later run retries it.
	Release(ctx context.Context, dedupeKey string) error
	// ReleaseStale deletes the claims of kind still pending
	// (recipients = 0) that were made before `before`: their backend died
	// before sending finished. Returns how many were released.
	ReleaseStale(ctx context.Context, kind string, before time.Time) (int64, error)
	// SetRecipients records how many people the claimed mail went to
	// (types.EmailRecipientsNone when nobody).
	SetRecipients(ctx context.Context, dedupeKey string, n int) error
	// ListSubscriptionsEndingBetween lists active subscriptions whose end
	// falls in (from, to], with their plan.
	ListSubscriptionsEndingBetween(ctx context.Context, from, to time.Time) ([]*types.UsageSubscription, error)
}

// BillingNotifier mails the people concerned by billing events. Every
// method returns at once: the mail is sent in the background, and a
// failure is only logged.
type BillingNotifier interface {
	// OrderCreated tells the SuperAdmins a new order waits for payment.
	OrderCreated(ctx context.Context, order *types.UsageOrderView)
	// OrderPaid tells the buyer the order was confirmed.
	OrderPaid(ctx context.Context, order *types.UsageOrderView)
}
