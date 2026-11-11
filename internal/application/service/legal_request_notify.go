package service

import (
	"context"
	"encoding/json"
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
// legal document requests closed as added.
type LegalRequestNotifier struct {
	requests interfaces.LegalRequestRepository
	now      func() time.Time
	// running keeps one sweep at a time per backend (the transactional
	// write keeps several backends apart)
	running sync.Mutex
	wg      sync.WaitGroup
}

// NewLegalRequestNotifier creates the notifier.
func NewLegalRequestNotifier(requests interfaces.LegalRequestRepository) *LegalRequestNotifier {
	return &LegalRequestNotifier{requests: requests, now: time.Now}
}

// Trigger runs a sweep in the background (after a request was closed as
// added); it returns at once.
func (l *LegalRequestNotifier) Trigger(ctx context.Context) {
	if l == nil {
		return
	}
	bg := context.WithoutCancel(ctx)
	l.wg.Add(1)
	go func() {
		defer l.wg.Done()
		ctx, cancel := context.WithTimeout(bg, legalNoticeRunTimeout)
		defer cancel()
		defer func() {
			if r := recover(); r != nil {
				logger.Errorf(ctx, "[LegalNotice] sweep panicked: %v", r)
			}
		}()
		l.runLogged(ctx)
	}()
}

// Wait blocks until the triggered sweeps are done (tests and shutdown).
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
// again only until it is notified.
func (l *LegalRequestNotifier) Run(ctx context.Context) (int, error) {
	l.running.Lock()
	defer l.running.Unlock()
	notices, err := l.requests.PendingAddedNotices(ctx, l.now().Add(-types.LegalRequestNoticeWindow), legalNoticeBatch)
	if err != nil {
		return 0, err
	}
	sent := 0
	for _, n := range notices {
		ok, err := l.requests.DeliverAddedNotice(ctx, n.RequesterID, legalRequestAddedNotification(n))
		if err != nil {
			return sent, fmt.Errorf("requester %s of %s: %w", n.RequesterID, n.RequestID, err)
		}
		if ok {
			sent++
		}
	}
	return sent, nil
}

// Start runs Run every interval (first run after a minute); stop ends the
// loop.
func (l *LegalRequestNotifier) Start(interval time.Duration) (stop func()) {
	done := make(chan struct{})
	go func() {
		timer := time.NewTimer(time.Minute)
		defer timer.Stop()
		for {
			select {
			case <-done:
				return
			case <-timer.C:
			}
			ctx, cancel := context.WithTimeout(context.Background(), legalNoticeRunTimeout)
			l.runLogged(ctx)
			cancel()
			timer.Reset(interval)
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(done) }) }
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
