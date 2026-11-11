package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// reviewDigests is the machinery the SuperAdmin review-queue digests share
// (abbreviation suggestions, payment orders, legal requests, drafts,
// links, identities): every SuperAdmin has at most one unread digest per
// kind (types.UserNotificationOpenKey), which each new item updates; the
// open digests are read for everyone once the queue is empty. The work
// runs in the background with a bounded concurrency and timeout, so the
// request or job that queued an item never waits for it or fails on it.

const (
	// reviewNoticeTimeout bounds one background run (all recipients).
	reviewNoticeTimeout = 30 * time.Second
	// reviewNoticeConcurrency bounds the runs at once per notifier; the
	// others wait (within their timeout).
	reviewNoticeConcurrency = 2
	// reviewNoticeMaxRecipients: SuperAdmins read per run (a handful in
	// practice).
	reviewNoticeMaxRecipients = 200
)

type reviewDigests struct {
	tag           string
	users         interfaces.UserRepository
	notifications interfaces.UserNotificationRepository
	now           func() time.Time
	slots         chan struct{}
	wg            sync.WaitGroup
}

func newReviewDigests(tag string, users interfaces.UserRepository, notifications interfaces.UserNotificationRepository) *reviewDigests {
	return &reviewDigests{
		tag: tag, users: users, notifications: notifications,
		now: time.Now, slots: make(chan struct{}, reviewNoticeConcurrency),
	}
}

// wait blocks until the background runs started so far are done (tests).
func (d *reviewDigests) wait() { d.wg.Wait() }

// waitTimeout waits like wait, at most dur (shutdown); false when runs
// were still going (each ends on its own timeout).
func (d *reviewDigests) waitTimeout(dur time.Duration) bool {
	done := make(chan struct{})
	go func() {
		d.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(dur):
		return false
	}
}

// background runs fn with the caller's values but not its cancellation,
// under the timeout and the concurrency bound; failures are only logged.
func (d *reviewDigests) background(ctx context.Context, what string, fn func(context.Context) error) {
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		run, cancel := context.WithTimeout(context.WithoutCancel(ctx), reviewNoticeTimeout)
		defer cancel()
		defer func() {
			if r := recover(); r != nil {
				logger.Errorf(run, "[%s] %s panicked: %v", d.tag, what, r)
			}
		}()
		select {
		case d.slots <- struct{}{}:
			defer func() { <-d.slots }()
		case <-run.Done():
			logger.Warnf(run, "[%s] %s skipped: %v", d.tag, what, run.Err())
			return
		}
		if err := fn(run); err != nil {
			logger.Warnf(run, "[%s] %s: %v", d.tag, what, err)
		}
	}()
}

// reviewers are the SuperAdmins to notify: explicit human accounts
// (is_system_admin, not a synthetic principal) that are not disabled or
// deleted, minus except (the person whose action queued the item).
func (d *reviewDigests) reviewers(ctx context.Context, except string) ([]string, error) {
	admins, _, err := d.users.ListSystemAdmins(ctx, 0, reviewNoticeMaxRecipients)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, u := range admins {
		if u == nil || !u.IsActive || !u.IsSystemAdmin || u.ID == "" || types.IsSyntheticUserID(u.ID) || u.ID == except {
			continue
		}
		ids = append(ids, u.ID)
	}
	return ids, nil
}

// write upserts the open digest of every reviewer but except: build makes
// a new digest, merge folds the news into an unread one.
func (d *reviewDigests) write(ctx context.Context, except string,
	build func(userID string, at time.Time) *types.UserNotification, merge func(prev, next *types.UserNotification),
) error {
	recipients, err := d.reviewers(ctx, except)
	if err != nil {
		return fmt.Errorf("listing SuperAdmins: %w", err)
	}
	var failed []string
	for _, userID := range recipients {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if _, err := d.notifications.UpsertOpenDigest(ctx, build(userID, d.now()), merge); err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", userID, err))
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("digest of %d SuperAdmin(s) not written: %s", len(failed), strings.Join(failed, "; "))
	}
	return nil
}

// closeIfEmpty reads the open digests of kind for everyone once pending
// counts no item left.
func (d *reviewDigests) closeIfEmpty(ctx context.Context, kind string, pending func(context.Context) (int64, error)) error {
	// a digest updated after this instant reports an item the count below
	// may not see yet: it stays open
	before := d.now()
	total, err := pending(ctx)
	if err != nil {
		return fmt.Errorf("counting the pending items of %s: %w", kind, err)
	}
	if total > 0 {
		return nil
	}
	n, err := d.notifications.CloseOpenDigests(ctx, kind, before, d.now())
	if err != nil {
		return err
	}
	if n > 0 {
		logger.Infof(ctx, "[%s] %s queue empty: %d open digest(s) read", d.tag, kind, n)
	}
	return nil
}
