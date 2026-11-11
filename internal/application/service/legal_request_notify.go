package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// The people who asked for a legal document the system lacked (the
// grounding gate recorded them in legal_request_requesters) get an in-app
// notification (user_notifications) once it is added and usable. Every
// path that closes a request as added (the SuperAdmin queue, an ingest from
// vbpl.vn, the auto-ingest worker, a legal feed push, an approved OCR
// draft) only has to set the status: a sweep finds the requesters not told
// yet. A rejected request notifies nobody.

const (
	// LegalRequestNoticeInterval: how often the sweep runs besides the
	// immediate runs after a request was closed (a document still parsing
	// is picked up by the next sweep after parsing finished).
	LegalRequestNoticeInterval = 5 * time.Minute
	// legalNoticeBatch caps the requesters one sweep handles.
	legalNoticeBatch = 500
	// legalNoticeRunTimeout bounds one sweep.
	legalNoticeRunTimeout = 2 * time.Minute
	// userNotificationPurgeEvery / Batch: the read-notification purge runs
	// once a day, deleting this many rows per statement.
	userNotificationPurgeEvery = 24 * time.Hour
	userNotificationPurgeBatch = 1000
)

// legalChatPath is the conversation page of frontend-next
// (app/platform/chat/[id]); a Guest's portal chat uses it too.
func legalChatPath(sessionID string) string {
	return "/platform/chat/" + url.PathEscape(sessionID)
}

// legalLookupPath is the legal lookup page searching for number.
func legalLookupPath(number string) string {
	return "/platform/legal-lookup?q=" + url.QueryEscape(number)
}

// legalNoticeKey keys a notice to the ask (the requester row's updated_at
// moves only when the person asks again), not to the closing: a request
// stamped added twice (an ingest then the decision) notifies once.
func legalNoticeKey(n *types.LegalRequestAddedNotice) string {
	return fmt.Sprintf("%s:%s:%d", types.UserNotificationLegalRequestAdded, n.RequesterID, n.AskedAt.UnixMicro())
}

// legalRequestAddedNotification builds the notice of one requester. It
// names only that person's request and conversation, nothing of the other
// people who asked.
func legalRequestAddedNotification(n *types.LegalRequestAddedNotice) *types.UserNotification {
	number := strings.TrimSpace(n.DocumentNumber)
	title := strings.TrimSpace(n.Title)
	label := number
	if label == "" {
		label = title
	}
	var hint, link string
	switch {
	case !n.SessionLive || n.SessionID == "":
		hint = "Bạn có thể tra cứu và hỏi về văn bản này ngay bây giờ."
		link = legalLookupPath(label)
	case n.Channel == types.LegalRequesterChannelWordAddin:
		hint = "Bạn đã hỏi trong tiện ích Word: hãy mở lại tệp trong Word để hỏi tiếp, hoặc xem cuộc trò chuyện trên web."
		link = legalChatPath(n.SessionID)
	default:
		hint = "Bạn có thể hỏi lại ngay trong cuộc trò chuyện trước đó."
		link = legalChatPath(n.SessionID)
	}
	body := hint
	if title != "" && title != label {
		body = title + ". " + hint
	}
	payload, _ := json.Marshal(types.UserNotificationLegalRequestPayload{
		RequestID: n.RequestID, DocumentNumber: number, Title: title,
		SessionID: n.SessionID, Channel: n.Channel, WordFileID: n.WordFileID,
	})
	return &types.UserNotification{
		TenantID: n.TenantID, UserID: n.UserID, Kind: types.UserNotificationLegalRequestAdded,
		Title: fmt.Sprintf("Văn bản %s đã được bổ sung", label), Body: body, Link: link,
		Payload: types.JSON(payload), DedupeKey: legalNoticeKey(n),
	}
}

// LegalRequestNotifier writes the in-app notices of the requesters of the
// legal document requests closed as added. It also purges the read
// notifications older than types.UserNotificationRetention once a day.
type LegalRequestNotifier struct {
	requests      interfaces.LegalRequestRepository
	notifications interfaces.UserNotificationRepository
	now           func() time.Time
	// running keeps one sweep at a time per backend (the transactional
	// write keeps several backends apart)
	running sync.Mutex

	// mu guards closed and the WaitGroup's Add: no sweep starts once Stop
	// began, so Stop's Wait never races an Add.
	mu        sync.Mutex
	closed    bool
	base      context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	lastPurge time.Time
}

// NewLegalRequestNotifier creates the notifier.
func NewLegalRequestNotifier(
	requests interfaces.LegalRequestRepository, notifications interfaces.UserNotificationRepository,
) *LegalRequestNotifier {
	base, cancel := context.WithCancel(context.Background())
	return &LegalRequestNotifier{requests: requests, notifications: notifications, now: time.Now, base: base, cancel: cancel}
}

// begin registers a background run; false once Stop began.
func (l *LegalRequestNotifier) begin() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return false
	}
	l.wg.Add(1)
	return true
}

// Trigger runs a sweep in the background (after a request was closed as
// added); it returns at once and does nothing once Stop began.
func (l *LegalRequestNotifier) Trigger(ctx context.Context) {
	if l == nil || !l.begin() {
		return
	}
	go func() {
		defer l.wg.Done()
		// the request's values (logging) but the notifier's lifetime
		run, cancel := context.WithTimeout(context.WithoutCancel(ctx), legalNoticeRunTimeout)
		defer cancel()
		stop := context.AfterFunc(l.base, cancel)
		defer stop()
		defer func() {
			if r := recover(); r != nil {
				logger.Errorf(run, "[LegalNotice] sweep panicked: %v", r)
			}
		}()
		l.runLogged(run)
	}()
}

// Stop ends the periodic loop, cancels running sweeps, refuses new ones
// and waits for them.
func (l *LegalRequestNotifier) Stop() {
	l.mu.Lock()
	l.closed = true
	l.mu.Unlock()
	l.cancel()
	l.wg.Wait()
}

// Wait blocks until the background runs started so far are done (tests).
func (l *LegalRequestNotifier) Wait() { l.wg.Wait() }

func (l *LegalRequestNotifier) runLogged(ctx context.Context) {
	if n, err := l.Run(ctx); err != nil {
		logger.Warnf(ctx, "[LegalNotice] sweep: %v", err)
	} else if n > 0 {
		logger.Infof(ctx, "[LegalNotice] %d requester(s) notified", n)
	}
}

// Run notifies the requesters not told yet of requests closed as added in
// the last types.LegalRequestNoticeWindow whose document is usable, and
// returns how many were notified. Each notice and its requester's
// notified_at are written in one transaction, so a requester is selected
// again only until it is notified. A requester that fails is logged and
// skipped (the next sweep retries it); the errors are returned joined.
func (l *LegalRequestNotifier) Run(ctx context.Context) (int, error) {
	l.running.Lock()
	defer l.running.Unlock()
	notices, err := l.requests.PendingAddedNotices(ctx, l.now().Add(-types.LegalRequestNoticeWindow), legalNoticeBatch)
	if err != nil {
		return 0, err
	}
	sent := 0
	var errs []error
	for _, n := range notices {
		if ctx.Err() != nil {
			errs = append(errs, ctx.Err())
			break
		}
		ok, err := l.requests.DeliverAddedNotice(ctx, n.RequesterID, legalRequestAddedNotification(n))
		if err != nil {
			logger.Warnf(ctx, "[LegalNotice] requester %s of %s: %v", n.RequesterID, n.RequestID, err)
			errs = append(errs, fmt.Errorf("requester %s of %s: %w", n.RequesterID, n.RequestID, err))
			continue
		}
		if ok {
			sent++
		}
	}
	return sent, errors.Join(errs...)
}

// purgeDaily deletes the read notifications past the retention, at most
// once a day.
func (l *LegalRequestNotifier) purgeDaily(ctx context.Context) {
	if l.notifications == nil {
		return
	}
	now := l.now()
	if !l.lastPurge.IsZero() && now.Sub(l.lastPurge) < userNotificationPurgeEvery {
		return
	}
	l.lastPurge = now
	n, err := l.notifications.PurgeRead(ctx, now.Add(-types.UserNotificationRetention), userNotificationPurgeBatch)
	if err != nil {
		logger.Warnf(ctx, "[Notifications] purging read notifications: %v", err)
	} else if n > 0 {
		logger.Infof(ctx, "[Notifications] %d read notification(s) older than %s purged", n, types.UserNotificationRetention)
	}
}

// Start runs Run (and the daily purge) every interval, the first run after
// a minute, until Stop; it returns Stop.
func (l *LegalRequestNotifier) Start(interval time.Duration) (stop func()) {
	if !l.begin() {
		return func() {}
	}
	go func() {
		defer l.wg.Done()
		timer := time.NewTimer(time.Minute)
		defer timer.Stop()
		for {
			select {
			case <-l.base.Done():
				return
			case <-timer.C:
			}
			ctx, cancel := context.WithTimeout(l.base, legalNoticeRunTimeout)
			l.runLogged(ctx)
			l.purgeDaily(ctx)
			cancel()
			timer.Reset(interval)
		}
	}()
	var once sync.Once
	return func() { once.Do(l.Stop) }
}

// legalRequestAddedNotify decorates the SuperAdmin queue: a decision that
// closes a request as added (by hand or by an ingest from vbpl.vn)
// notifies its requesters right away instead of at the next sweep.
type legalRequestAddedNotify struct {
	interfaces.LegalRequestService
	notifier *LegalRequestNotifier
}

// NewLegalRequestAddedNotifications wraps the legal request service
// (dig.Decorate).
func NewLegalRequestAddedNotifications(
	inner interfaces.LegalRequestService, notifier *LegalRequestNotifier,
) interfaces.LegalRequestService {
	return &legalRequestAddedNotify{LegalRequestService: inner, notifier: notifier}
}

func (d *legalRequestAddedNotify) Decide(
	ctx context.Context, id string, decision *types.LegalRequestDecision,
) (*types.LegalRequestItem, error) {
	item, err := d.LegalRequestService.Decide(ctx, id, decision)
	if err == nil && item != nil && item.LegalDocumentRequest != nil && item.Status == types.LegalRequestAdded {
		d.notifier.Trigger(ctx)
	}
	return item, err
}
