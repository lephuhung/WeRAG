package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

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
	// abbreviationNoticeFullFormRunes / BodyRunes cap the stored text.
	abbreviationNoticeFullFormRunes = 40
	abbreviationNoticeBodyRunes     = 300
)

// AbbreviationReviewLink is the abbreviation page of the settings (the
// /platform/system catch-all opens the settings section) showing only the
// suggestions waiting for review.
const AbbreviationReviewLink = "/platform/system/extensions/abbreviations?status=pending"

// AbbreviationReviewNotifier writes the SuperAdmins' abbreviation_pending
// digests (reviewDigests). Every method returns at once; the work runs in
// the background and only logs failures, so a suggestion or a review never
// waits for it.
type AbbreviationReviewNotifier struct {
	digests       *reviewDigests
	abbreviations interfaces.AbbreviationRepository
}

// NewAbbreviationReviewNotifier creates the notifier.
func NewAbbreviationReviewNotifier(
	users interfaces.UserRepository,
	notifications interfaces.UserNotificationRepository,
	abbreviations interfaces.AbbreviationRepository,
) *AbbreviationReviewNotifier {
	return &AbbreviationReviewNotifier{digests: newReviewDigests("AbbrNotice", users, notifications), abbreviations: abbreviations}
}

// Wait blocks until the background runs started so far are done (tests).
func (a *AbbreviationReviewNotifier) Wait() {
	if a != nil {
		a.digests.wait()
	}
}

// WaitTimeout waits like Wait, at most d (shutdown); false when runs were
// still going (each ends on its own timeout).
func (a *AbbreviationReviewNotifier) WaitTimeout(d time.Duration) bool {
	if a == nil {
		return true
	}
	return a.digests.waitTimeout(d)
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
	a.digests.background(ctx, "notifying a new suggestion", func(ctx context.Context) error {
		return a.notifySuggestion(ctx, id, item, suggester)
	})
}

// QueueChanged closes the open digests once no suggestion is pending
// (called after an approval or a delete).
func (a *AbbreviationReviewNotifier) QueueChanged(ctx context.Context) {
	if a == nil {
		return
	}
	a.digests.background(ctx, "closing digests", a.closeIfEmpty)
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
	first := types.UserNotificationAbbreviationPendingPayload{Count: 1, Items: []types.UserNotificationAbbreviationItem{item}}
	writeErr := a.digests.write(ctx, suggester, func(userID string, at time.Time) *types.UserNotification {
		return abbreviationPendingNotification(userID, first, at)
	}, mergeAbbreviationDigest(item))
	// the queue may have emptied while the digests were written, after the
	// review's own check ran: close them again then
	closeErr := a.closeIfEmpty(ctx)
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

func (a *AbbreviationReviewNotifier) closeIfEmpty(ctx context.Context) error {
	return a.digests.closeIfEmpty(ctx, types.UserNotificationAbbreviationPending, func(ctx context.Context) (int64, error) {
		pending := false
		_, total, err := a.abbreviations.List(ctx, "", &pending, 0, 1)
		return total, err
	})
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
