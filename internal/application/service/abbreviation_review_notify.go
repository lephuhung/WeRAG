package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// SuperAdmins (the reviewers of the shared abbreviation dictionary) get an
// in-app notification (user_notifications) when a new suggestion waits for
// review. It is a digest, not one notice per suggestion: each SuperAdmin
// has at most one unread abbreviation_pending notice, which every new
// suggestion updates (count, newest short forms, toasted again). Reading
// it frees the slot, so the next suggestion opens a new one. Once the
// pending queue is empty (the last suggestion approved or deleted) the open
// digests are read for everyone, so no stale "3 từ chờ duyệt" stays.

const (
	// abbreviationNoticeTimeout bounds one background run (all recipients).
	abbreviationNoticeTimeout = 30 * time.Second
	// abbreviationNoticeConcurrency bounds the runs at once; the others
	// wait (within their timeout).
	abbreviationNoticeConcurrency = 2
	// abbreviationNoticeFullFormRunes / BodyRunes cap the stored text.
	abbreviationNoticeFullFormRunes = 40
	abbreviationNoticeBodyRunes     = 300
	// abbreviationNoticeMaxRecipients: SuperAdmins read per run (a handful
	// in practice).
	abbreviationNoticeMaxRecipients = 200
)

// AbbreviationReviewLink is the abbreviation page of the settings (the
// /platform/system catch-all opens the settings section) showing only the
// suggestions waiting for review.
const AbbreviationReviewLink = "/platform/system/extensions/abbreviations?status=pending"

// AbbreviationReviewNotifier writes the SuperAdmins' abbreviation_pending
// digests. Every method returns at once; the work runs in the background
// and only logs failures, so a suggestion or a review never waits for it.
type AbbreviationReviewNotifier struct {
	users         interfaces.UserRepository
	notifications interfaces.UserNotificationRepository
	abbreviations interfaces.AbbreviationRepository
	now           func() time.Time
	slots         chan struct{}
	wg            sync.WaitGroup
}

// NewAbbreviationReviewNotifier creates the notifier.
func NewAbbreviationReviewNotifier(
	users interfaces.UserRepository,
	notifications interfaces.UserNotificationRepository,
	abbreviations interfaces.AbbreviationRepository,
) *AbbreviationReviewNotifier {
	return &AbbreviationReviewNotifier{
		users: users, notifications: notifications, abbreviations: abbreviations,
		now: time.Now, slots: make(chan struct{}, abbreviationNoticeConcurrency),
	}
}

// Wait blocks until the background runs started so far are done (tests).
func (a *AbbreviationReviewNotifier) Wait() {
	if a != nil {
		a.wg.Wait()
	}
}

// WaitTimeout waits like Wait, at most d (shutdown); false when runs were
// still going (each ends on its own timeout).
func (a *AbbreviationReviewNotifier) WaitTimeout(d time.Duration) bool {
	if a == nil {
		return true
	}
	done := make(chan struct{})
	go func() {
		a.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}

// background runs fn with the request's values but not its cancellation,
// under the notifier's timeout and concurrency bound.
func (a *AbbreviationReviewNotifier) background(ctx context.Context, what string, fn func(context.Context) error) {
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		run, cancel := context.WithTimeout(context.WithoutCancel(ctx), abbreviationNoticeTimeout)
		defer cancel()
		defer func() {
			if r := recover(); r != nil {
				logger.Errorf(run, "[AbbrNotice] %s panicked: %v", what, r)
			}
		}()
		select {
		case a.slots <- struct{}{}:
			defer func() { <-a.slots }()
		case <-run.Done():
			logger.Warnf(run, "[AbbrNotice] %s skipped: %v", what, run.Err())
			return
		}
		if err := fn(run); err != nil {
			logger.Warnf(run, "[AbbrNotice] %s: %v", what, err)
		}
	}()
}

// SuggestionCreated tells every SuperAdmin but the suggester about row, a
// pending suggestion just inserted. Anything else (an existing row, an
// active one) notifies nobody.
func (a *AbbreviationReviewNotifier) SuggestionCreated(ctx context.Context, row *types.Abbreviation) {
	if a == nil || row == nil || row.IsActive {
		return
	}
	item := types.UserNotificationAbbreviationItem{
		ShortForm: strings.TrimSpace(row.ShortForm), FullForm: strings.TrimSpace(row.FullForm), ProperName: row.IsProperName,
	}
	id, suggester := row.ID, row.SuggestedBy
	a.background(ctx, "notifying a new suggestion", func(ctx context.Context) error {
		return a.notifySuggestion(ctx, id, item, suggester)
	})
}

// QueueChanged closes the open digests once no suggestion is pending
// (called after an approval or a delete).
func (a *AbbreviationReviewNotifier) QueueChanged(ctx context.Context) {
	if a == nil {
		return
	}
	a.background(ctx, "closing digests", a.closeIfEmpty)
}

// reviewers are the SuperAdmins to notify: explicit human accounts
// (is_system_admin, not a synthetic principal) that are not disabled or
// deleted, minus except (the suggester).
func (a *AbbreviationReviewNotifier) reviewers(ctx context.Context, except string) ([]string, error) {
	admins, _, err := a.users.ListSystemAdmins(ctx, 0, abbreviationNoticeMaxRecipients)
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

func (a *AbbreviationReviewNotifier) notifySuggestion(
	ctx context.Context, id string, item types.UserNotificationAbbreviationItem, suggester string,
) error {
	// the run may have waited for a slot: a suggestion approved, kept as a
	// proper name or deleted meanwhile is no news any more
	row, err := a.abbreviations.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("re-reading suggestion %s: %w", id, err)
	}
	if row == nil || row.IsActive {
		return nil
	}
	recipients, err := a.reviewers(ctx, suggester)
	if err != nil {
		return fmt.Errorf("listing SuperAdmins: %w", err)
	}
	first := types.UserNotificationAbbreviationPendingPayload{Count: 1, Items: []types.UserNotificationAbbreviationItem{item}}
	var failed []string
	for _, userID := range recipients {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		n := abbreviationPendingNotification(userID, first, a.now())
		if _, err := a.notifications.UpsertOpenDigest(ctx, n, mergeAbbreviationDigest(item)); err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", userID, err))
		}
	}
	// the queue may have emptied while the digests were written, after the
	// review's own check ran: close them again then
	closeErr := a.closeIfEmpty(ctx)
	if len(failed) > 0 {
		return fmt.Errorf("digest of %d SuperAdmin(s) not written: %s", len(failed), strings.Join(failed, "; "))
	}
	return closeErr
}

func (a *AbbreviationReviewNotifier) closeIfEmpty(ctx context.Context) error {
	// a digest updated after this instant reports a suggestion the count
	// below may not see yet: it stays open
	before := a.now()
	pending := false
	_, total, err := a.abbreviations.List(ctx, "", &pending, 0, 1)
	if err != nil {
		return fmt.Errorf("counting pending suggestions: %w", err)
	}
	if total > 0 {
		return nil
	}
	n, err := a.notifications.CloseOpenDigests(ctx, types.UserNotificationAbbreviationPending, before, a.now())
	if err != nil {
		return err
	}
	if n > 0 {
		logger.Infof(ctx, "[AbbrNotice] queue empty: %d open digest(s) read", n)
	}
	return nil
}

// mergeAbbreviationDigest adds item to the unread digest prev: one more
// suggestion, item first among the newest named ones.
func mergeAbbreviationDigest(item types.UserNotificationAbbreviationItem) func(prev, next *types.UserNotification) {
	return func(prev, next *types.UserNotification) {
		var old types.UserNotificationAbbreviationPendingPayload
		if len(prev.Payload) > 0 {
			_ = json.Unmarshal(prev.Payload, &old)
		}
		count := old.Count
		if count < len(old.Items) {
			count = len(old.Items)
		}
		merged := types.UserNotificationAbbreviationPendingPayload{Count: count + 1, Items: []types.UserNotificationAbbreviationItem{item}}
		for _, it := range old.Items {
			if len(merged.Items) == types.UserNotificationAbbreviationPendingMaxItems {
				break
			}
			if strings.EqualFold(it.ShortForm, item.ShortForm) && strings.EqualFold(it.FullForm, item.FullForm) {
				continue
			}
			merged.Items = append(merged.Items, it)
		}
		*next = *abbreviationPendingNotification(next.UserID, merged, next.CreatedAt)
	}
}

// abbreviationPendingNotification builds the digest of userID. Title and
// body are the stored (Vietnamese) text; the clients word it from the
// payload in the reader's language.
func abbreviationPendingNotification(
	userID string, p types.UserNotificationAbbreviationPendingPayload, at time.Time,
) *types.UserNotification {
	payload, _ := json.Marshal(p)
	return &types.UserNotification{
		TenantID:  types.PlatformTenantID,
		UserID:    userID,
		Kind:      types.UserNotificationAbbreviationPending,
		Title:     fmt.Sprintf("Có %d từ viết tắt mới chờ duyệt", p.Count),
		Body:      abbreviationPendingBody(p),
		Link:      AbbreviationReviewLink,
		Payload:   types.JSON(payload),
		DedupeKey: types.UserNotificationOpenKey(types.UserNotificationAbbreviationPending, userID),
		CreatedAt: at,
	}
}

// abbreviationPendingBody: "VP → Văn phòng, PV01 (tên riêng), và 3 từ khác".
func abbreviationPendingBody(p types.UserNotificationAbbreviationPendingPayload) string {
	parts := make([]string, 0, len(p.Items)+1)
	for _, it := range p.Items {
		if it.ProperName {
			parts = append(parts, it.ShortForm+" (tên riêng)")
			continue
		}
		parts = append(parts, it.ShortForm+" → "+abbrNoticeEllipsis(it.FullForm, abbreviationNoticeFullFormRunes))
	}
	if more := p.Count - len(p.Items); more > 0 {
		parts = append(parts, fmt.Sprintf("và %d từ khác", more))
	}
	return abbrNoticeEllipsis(strings.Join(parts, ", "), abbreviationNoticeBodyRunes)
}

// abbrNoticeEllipsis cuts s to at most n runes, ending with "…" when cut.
func abbrNoticeEllipsis(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:n-1])) + "…"
}
