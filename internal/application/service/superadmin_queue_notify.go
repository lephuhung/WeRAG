package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// SuperAdmins get an in-app digest (reviewDigests) when work lands in one
// of their review queues: payment orders waiting for confirmation, legal
// documents people asked for in chat, OCR drafts ready for review, legal
// feed links and public-KB identities waiting for review. One unread
// digest per SuperAdmin and queue, closed for everyone once the queue is
// empty: by the action that emptied it, or by the periodic sweep (orders
// expire on their own, and a few rarer paths change the queues without
// telling the notifier).
//
// Events are coalesced per queue: a batch is written once no event came
// for the quiet period (at most maxWait after its first event), so a feed
// run of N documents is one digest update (one toast) per SuperAdmin.
// A close check waits closeDelay, so the decisions made within it share
// one check. Every method returns at once; the writes run in the
// background, re-read what is still pending, and log failures (a failed
// write is not retried; the next batch or the sweep catches up). Shutdown
// (WaitTimeout) writes the batches still waiting and drops events that
// arrive after it started.

const (
	// SuperAdminQueueSweepInterval is how often the sweep closes the
	// digests of empty queues.
	SuperAdminQueueSweepInterval = 10 * time.Minute
	superAdminQueueQuiet         = 45 * time.Second
	superAdminQueueMaxWait       = 2 * time.Minute
	superAdminQueueCloseDelay    = 2 * time.Second
	// superAdminQueueFlushTimeout bounds one write of a batch (all events,
	// all recipients).
	superAdminQueueFlushTimeout = time.Minute
	// superAdminReviewingStale: a draft in review whose row was not updated
	// (opened again, structure checked) for this long waits for a reviewer
	// again; edits in the editor do not touch the row.
	superAdminReviewingStale = 24 * time.Hour
)

// Links of the review queues (SystemAdmin console, filtered to the
// pending items).
const (
	PaymentOrderReviewLink   = "/platform/admin?section=billing&status=pending"
	LegalRequestReviewLink   = "/platform/admin?section=legal-documents&view=requests"
	LegalDraftReviewLink     = "/platform/admin?section=legal-documents&view=drafts&status=ready"
	LegalLinkReviewLink      = "/platform/admin?section=legal-documents&view=links"
	LegalIdentityReviewLink  = "/platform/admin?section=legal-documents&view=review"
	superAdminQueueItemRunes = 60
	superAdminQueueBodyRunes = 300
	// superAdminQueueIDChunk bounds the ids of one IN (...) re-read.
	superAdminQueueIDChunk = 500
	// superAdminQueueUntitled names an item without số hiệu or title.
	superAdminQueueUntitled = "văn bản không số"
)

// reviewQueue is one SuperAdmin review queue: its digest kind, page, the
// stored (Vietnamese) wording and whether any item is pending.
type reviewQueue struct {
	kind  string
	link  string
	title func(count int) string
	item  func(it types.UserNotificationQueueItem) string
	// more words the items the digest does not name; nil when the items
	// carry their own counts (links: count links, items name documents)
	more    func(n int) string
	pending func(ctx context.Context, db *gorm.DB, now time.Time) (bool, error)
}

// queueCollect re-reads one event in the background: the keys of its items
// still pending (order / draft / knowledge id, số hiệu key, link key) and
// the items to name (newest first); no key = handled. A batch counts the
// distinct keys of its events, so the same item queued twice in one batch
// (two people asking at once, a re-push) counts once.
type queueCollect func(ctx context.Context, db *gorm.DB) ([]string, []types.UserNotificationQueueItem, error)

type queueEvent struct {
	ctx     context.Context
	actor   string
	collect queueCollect
}

type queueBatch struct {
	events []queueEvent
	first  time.Time
	timer  *time.Timer
}

// SuperAdminQueueNotifier writes the review-queue digests.
type SuperAdminQueueNotifier struct {
	digests *reviewDigests
	db      *gorm.DB
	queues  map[string]*reviewQueue
	// quiet, maxWait and closeDelay: see the coalescing above (tests
	// shorten them)
	quiet, maxWait, closeDelay time.Duration

	mu      sync.Mutex
	batches map[string]*queueBatch
	closing map[string]*time.Timer
	stopped bool
	wg      sync.WaitGroup
}

// NewSuperAdminQueueNotifier creates the notifier.
func NewSuperAdminQueueNotifier(
	users interfaces.UserRepository, notifications interfaces.UserNotificationRepository, db *gorm.DB,
) *SuperAdminQueueNotifier {
	n := &SuperAdminQueueNotifier{digests: newReviewDigests("QueueNotice", users, notifications), db: db,
		queues: map[string]*reviewQueue{}, quiet: superAdminQueueQuiet, maxWait: superAdminQueueMaxWait,
		closeDelay: superAdminQueueCloseDelay, batches: map[string]*queueBatch{}, closing: map[string]*time.Timer{}}
	for _, q := range superAdminQueues() {
		n.queues[q.kind] = q
	}
	return n
}

// queueHasRows reports whether q (a filtered model query) matches a row:
// SELECT EXISTS, not a count.
func queueHasRows(ctx context.Context, db, q *gorm.DB) (bool, error) {
	var found bool
	err := db.WithContext(ctx).Raw("SELECT EXISTS (?)", q.Select("1")).Scan(&found).Error
	return found, err
}

func superAdminQueues() []*reviewQueue {
	where := func(model any, query string, args ...any) func(context.Context, *gorm.DB, time.Time) (bool, error) {
		return func(ctx context.Context, db *gorm.DB, _ time.Time) (bool, error) {
			return queueHasRows(ctx, db, db.Model(model).Where(query, args...))
		}
	}
	return []*reviewQueue{
		{
			kind: types.UserNotificationPaymentOrderPending, link: PaymentOrderReviewLink,
			title: func(n int) string { return fmt.Sprintf("Có %d đơn hàng mới chờ xác nhận thanh toán", n) },
			item: func(it types.UserNotificationQueueItem) string {
				parts := []string{it.Ref}
				if it.Plan != "" {
					parts = append(parts, queueEllipsis(it.Plan, superAdminQueueItemRunes))
				}
				if it.AmountVND > 0 {
					parts = append(parts, formatVND(it.AmountVND))
				}
				if it.Org != "" {
					parts = append(parts, queueEllipsis(it.Org, superAdminQueueItemRunes))
				}
				return strings.Join(parts, " · ")
			},
			more: func(n int) string { return fmt.Sprintf("và %d đơn khác", n) },
			pending: func(ctx context.Context, db *gorm.DB, now time.Time) (bool, error) {
				return queueHasRows(ctx, db, pendingOrders(db, now))
			},
		},
		{
			kind: types.UserNotificationLegalRequestPending, link: LegalRequestReviewLink,
			title: func(n int) string { return fmt.Sprintf("Có %d văn bản mới được đề nghị bổ sung", n) },
			item: func(it types.UserNotificationQueueItem) string {
				s := queueRefTitle(it)
				if it.Count > 1 {
					s += fmt.Sprintf(" (%d lượt đề nghị)", it.Count)
				}
				return s
			},
			more:    func(n int) string { return fmt.Sprintf("và %d văn bản khác", n) },
			pending: where(&types.LegalDocumentRequest{}, personRequestsSQL, types.LegalRequestRequested),
		},
		{
			kind: types.UserNotificationLegalDraftReady, link: LegalDraftReviewLink,
			title: func(n int) string { return fmt.Sprintf("Có %d bản nháp OCR mới chờ duyệt", n) },
			item:  queueRefTitle,
			more:  func(n int) string { return fmt.Sprintf("và %d bản nháp khác", n) },
			// ready, or opened for review and left alone for a day
			pending: func(ctx context.Context, db *gorm.DB, now time.Time) (bool, error) {
				return queueHasRows(ctx, db, db.Model(&types.LegalDocumentDraft{}).
					Where("status = ? OR (status = ? AND updated_at < ?)", types.LegalDraftReady,
						types.LegalDraftReviewing, now.Add(-superAdminReviewingStale)))
			},
		},
		{
			kind: types.UserNotificationLegalLinkPending, link: LegalLinkReviewLink,
			title: func(n int) string { return fmt.Sprintf("Có %d liên kết sửa đổi mới chờ duyệt", n) },
			item: func(it types.UserNotificationQueueItem) string {
				if it.Count > 0 {
					return fmt.Sprintf("%s (%d liên kết)", it.Ref, it.Count)
				}
				return it.Ref
			},
			pending: where(&types.LegalProvisionLinkCandidate{}, "review_status = ?", types.LegalLinkReviewPending),
		},
		{
			kind: types.UserNotificationLegalIdentityPending, link: LegalIdentityReviewLink,
			title: func(n int) string { return fmt.Sprintf("Có %d văn bản mới chờ duyệt định danh", n) },
			item: func(it types.UserNotificationQueueItem) string {
				if it.Untitled || it.Ref == "" {
					return superAdminQueueUntitled
				}
				return queueEllipsis(it.Ref, superAdminQueueItemRunes)
			},
			more:    func(n int) string { return fmt.Sprintf("và %d văn bản khác", n) },
			pending: where(&types.LegalDocumentLink{}, "review_status = ?", types.LegalReviewPending),
		},
	}
}

// pendingOrders filters the orders still waiting: pending and not past
// their deadline (expiry is applied lazily on reads, so a pending row past
// it is already expired). A 0-đồng order never expires
// (ExpirePendingOrders): it waits for a SuperAdmin.
func pendingOrders(db *gorm.DB, now time.Time) *gorm.DB {
	return db.Model(&types.UsageOrder{}).
		Where("status = ? AND (total_vnd <= 0 OR expires_at IS NULL OR expires_at >= ?)", types.UsageOrderPending, now)
}

// personRequestsSQL keeps the open requests a person waits on: requested,
// with someone who asked in chat and was not told it was added (feed,
// import and no-text queue rows have no such requester).
const personRequestsSQL = "status = ? AND EXISTS (SELECT 1 FROM legal_request_requesters q " +
	"WHERE q.request_id = legal_document_requests.id AND q.notified_at IS NULL)"

// Wait writes the batches and runs the close checks waiting now, and
// blocks until every write started so far is done (tests).
func (n *SuperAdminQueueNotifier) Wait() {
	if n != nil {
		n.flushAll()
		n.wg.Wait()
	}
}

// WaitTimeout writes the waiting batches and waits like Wait, at most d
// (shutdown); false when writes were still going.
func (n *SuperAdminQueueNotifier) WaitTimeout(d time.Duration) bool {
	if n == nil {
		return true
	}
	n.mu.Lock()
	n.stopped = true // later events are written at once
	n.mu.Unlock()
	n.flushAll()
	done := make(chan struct{})
	go func() {
		n.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}

// notify adds one event to the batch of kind; actor (whose action queued
// the items) is not told.
func (n *SuperAdminQueueNotifier) notify(ctx context.Context, kind, actor string, collect queueCollect) {
	if n == nil || n.db == nil {
		return
	}
	now := n.digests.now()
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.stopped {
		// shutting down: what still waits was written by WaitTimeout
		return
	}
	b := n.batches[kind]
	if b == nil {
		b = &queueBatch{first: now}
		n.batches[kind] = b
	}
	b.events = append(b.events, queueEvent{ctx: context.WithoutCancel(ctx), actor: actor, collect: collect})
	delay := max(0, min(n.quiet, b.first.Add(n.maxWait).Sub(now)))
	if b.timer != nil {
		// if it already fired, its flushKind is waiting for the lock held
		// here and takes this event with the batch; the new timer then
		// finds no batch
		b.timer.Stop()
	}
	b.timer = time.AfterFunc(delay, func() { n.flushKind(kind) })
}

// takeBatch removes the batch of kind (nil when none), counting its write.
func (n *SuperAdminQueueNotifier) takeBatch(kind string) *queueBatch {
	b := n.batches[kind]
	if b == nil {
		return nil
	}
	delete(n.batches, kind)
	if b.timer != nil {
		b.timer.Stop()
	}
	n.wg.Add(1)
	return b
}

func (n *SuperAdminQueueNotifier) flushKind(kind string) {
	n.mu.Lock()
	if n.stopped { // WaitTimeout took every batch
		n.mu.Unlock()
		return
	}
	b := n.takeBatch(kind)
	n.mu.Unlock()
	if b != nil {
		n.write(kind, b.events)
	}
}

// flushAll starts the writes of every waiting batch and close check now
// (each counted in wg).
func (n *SuperAdminQueueNotifier) flushAll() {
	n.mu.Lock()
	var kinds []string
	batches := map[string]*queueBatch{}
	for kind := range n.batches {
		batches[kind] = n.takeBatch(kind)
	}
	for kind, t := range n.closing {
		t.Stop()
		delete(n.closing, kind)
		kinds = append(kinds, kind)
		n.wg.Add(1)
	}
	n.mu.Unlock()
	// one after another: a shutdown or a sweep is a handful of queries
	go func() {
		for kind, b := range batches {
			n.write(kind, b.events)
		}
		for _, kind := range kinds {
			n.runClose(kind)
		}
	}()
}

// write adds the events still pending to every SuperAdmin's digest of
// kind, but not an event to its actor's; then closes the digests if the
// queue emptied meanwhile. Called with the batch counted in wg.
func (n *SuperAdminQueueNotifier) write(kind string, events []queueEvent) {
	defer n.wg.Done()
	q := n.queues[kind]
	ctx, cancel := context.WithTimeout(events[0].ctx, superAdminQueueFlushTimeout)
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf(ctx, "[QueueNotice] writing %s panicked: %v", kind, r)
		}
	}()
	type found struct {
		actor string
		keys  []string
		items []types.UserNotificationQueueItem
	}
	var news []found
	for _, ev := range events {
		keys, items, err := ev.collect(ctx, n.db)
		if err != nil {
			logger.Warnf(ctx, "[QueueNotice] re-reading a %s event: %v", kind, err)
			continue
		}
		if len(keys) > 0 {
			news = append(news, found{actor: ev.actor, keys: keys, items: items})
		}
	}
	if len(news) > 0 {
		recipients, err := n.digests.reviewers(ctx, "")
		if err != nil {
			logger.Warnf(ctx, "[QueueNotice] %s: listing SuperAdmins: %v", kind, err)
		}
		var failed []string
		for _, userID := range recipients {
			if ctx.Err() != nil {
				failed = append(failed, ctx.Err().Error())
				break
			}
			// the distinct items of the events this person did not cause
			keys := map[string]bool{}
			var items []types.UserNotificationQueueItem
			for i := len(news) - 1; i >= 0; i-- { // the newest event first
				if news[i].actor == userID {
					continue
				}
				for _, k := range news[i].keys {
					keys[k] = true
				}
				items = prependQueueItems(items, news[i].items)
			}
			delta := len(keys)
			if delta == 0 {
				continue
			}
			// items so far are newest first: keep that order when merging
			first := types.UserNotificationQueuePayload{Count: delta, Items: items}
			if _, err := n.digests.notifications.UpsertOpenDigest(ctx, q.notification(userID, first, n.digests.now()),
				q.merge(delta, items)); err != nil {
				failed = append(failed, fmt.Sprintf("%s: %v", userID, err))
			}
		}
		if len(failed) > 0 {
			logger.Warnf(ctx, "[QueueNotice] %s digest not written: %s", kind, strings.Join(failed, "; "))
		}
	}
	// the queue may have emptied while the digests were written
	if err := n.closeIfEmpty(ctx, q); err != nil {
		logger.Warnf(ctx, "[QueueNotice] %v", err)
	}
}

// prependQueueItems appends older after items (newest first), skipping an
// item of a Ref items already names; at most UserNotificationQueueMaxItems.
func prependQueueItems(items, older []types.UserNotificationQueueItem) []types.UserNotificationQueueItem {
	out := append([]types.UserNotificationQueueItem(nil), items...)
	for _, it := range older {
		if len(out) >= types.UserNotificationQueueMaxItems {
			break
		}
		dup := false
		for _, have := range out {
			if !it.Untitled && !have.Untitled && strings.EqualFold(have.Ref, it.Ref) {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, it)
		}
	}
	if len(out) > types.UserNotificationQueueMaxItems {
		out = out[:types.UserNotificationQueueMaxItems]
	}
	return out
}

// changed closes the digests of kind once its queue is empty (called after
// an action that may have emptied it); checks within closeDelay are one.
func (n *SuperAdminQueueNotifier) changed(_ context.Context, kind string) {
	if n == nil || n.db == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.stopped {
		return
	}
	if _, ok := n.closing[kind]; ok {
		return
	}
	n.closing[kind] = time.AfterFunc(n.closeDelay, func() {
		n.mu.Lock()
		if _, ok := n.closing[kind]; !ok || n.stopped { // run by flushAll meanwhile
			n.mu.Unlock()
			return
		}
		delete(n.closing, kind)
		n.wg.Add(1)
		n.mu.Unlock()
		n.runClose(kind)
	})
}

// runClose runs one close check (counted in wg by the caller).
func (n *SuperAdminQueueNotifier) runClose(kind string) {
	defer n.wg.Done()
	ctx, cancel := context.WithTimeout(context.Background(), superAdminQueueFlushTimeout)
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf(ctx, "[QueueNotice] closing %s panicked: %v", kind, r)
		}
	}()
	if err := n.closeIfEmpty(ctx, n.queues[kind]); err != nil {
		logger.Warnf(ctx, "[QueueNotice] %v", err)
	}
}

func (n *SuperAdminQueueNotifier) closeIfEmpty(ctx context.Context, q *reviewQueue) error {
	return n.digests.closeIfEmpty(ctx, q.kind, func(ctx context.Context) (int64, error) {
		found, err := q.pending(ctx, n.db, n.digests.now())
		if found {
			return 1, err
		}
		return 0, err
	})
}

// Sweep closes the digests of every empty queue.
func (n *SuperAdminQueueNotifier) Sweep(ctx context.Context) {
	if n == nil || n.db == nil {
		return
	}
	for _, q := range superAdminQueues() {
		n.changed(ctx, q.kind)
	}
}

// Start runs Sweep every interval until the returned stop is called.
func (n *SuperAdminQueueNotifier) Start(interval time.Duration) (stop func()) {
	if n == nil || interval <= 0 {
		return func() {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				n.Sweep(ctx)
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

// ---------------------------------------------------------------------------
// Payment orders

// OrderPending: order v was created (or reopened) by actor and waits for
// its transfer, or (0 đồng) for a SuperAdmin's confirmation. An order paid
// at once notifies nobody.
func (n *SuperAdminQueueNotifier) OrderPending(ctx context.Context, v *types.UsageOrderView, actor string) {
	if v == nil || v.UsageOrder == nil || v.Status != types.UsageOrderPending {
		return
	}
	id := v.ID
	item := types.UserNotificationQueueItem{Ref: v.OrderCode, Plan: strings.TrimSpace(v.PlanName), AmountVND: v.TotalVND,
		Org: strings.TrimSpace(v.ScopeName)}
	n.notify(ctx, types.UserNotificationPaymentOrderPending, actor,
		func(ctx context.Context, db *gorm.DB) ([]string, []types.UserNotificationQueueItem, error) {
			found, err := queueHasRows(ctx, db, pendingOrders(db, n.digests.now()).Where("id = ?", id))
			if err != nil || !found {
				return nil, nil, err
			}
			return []string{id}, []types.UserNotificationQueueItem{item}, nil
		})
}

// OrdersChanged: an order was paid, cancelled or reopened.
func (n *SuperAdminQueueNotifier) OrdersChanged(ctx context.Context) {
	n.changed(ctx, types.UserNotificationPaymentOrderPending)
}

// ---------------------------------------------------------------------------
// Legal document requests

// LegalRequestsOpened: actor asked in chat for a document and the gate
// opened requests ids (created, or reopened after being added).
func (n *SuperAdminQueueNotifier) LegalRequestsOpened(ctx context.Context, actor string, ids []string) {
	if len(ids) == 0 {
		return
	}
	ids = append([]string(nil), ids...)
	n.notify(ctx, types.UserNotificationLegalRequestPending, actor,
		func(ctx context.Context, db *gorm.DB) ([]string, []types.UserNotificationQueueItem, error) {
			var rows []*types.LegalDocumentRequest
			if err := db.WithContext(ctx).Where("id IN ?", ids).Where(personRequestsSQL, types.LegalRequestRequested).
				Order("request_count DESC").Find(&rows).Error; err != nil {
				return nil, nil, err
			}
			// the rows of one số hiệu (several catalogue matches) are one item
			var items []types.UserNotificationQueueItem
			var keys []string
			seen := map[string]int{}
			for _, r := range rows {
				key := r.NumberKey
				if i, ok := seen[key]; ok {
					if items[i].Title == "" {
						items[i].Title = strings.TrimSpace(r.Title)
					}
					continue
				}
				seen[key] = len(items)
				keys = append(keys, key)
				ref := strings.TrimSpace(r.DocumentNumber)
				if ref == "" {
					ref = r.NumberKey
				}
				items = append(items, types.UserNotificationQueueItem{Ref: ref, Title: strings.TrimSpace(r.Title), Count: r.RequestCount})
			}
			return keys, items, nil
		})
}

// LegalRequestsChanged: a request was decided, claimed or released.
func (n *SuperAdminQueueNotifier) LegalRequestsChanged(ctx context.Context) {
	n.changed(ctx, types.UserNotificationLegalRequestPending)
}

// ---------------------------------------------------------------------------
// Legal OCR drafts

// LegalDraftReady: the OCR of draft d finished. Its creator (the SuperAdmin
// who fetched it, or a feed) is not told.
func (n *SuperAdminQueueNotifier) LegalDraftReady(ctx context.Context, d *types.LegalDocumentDraft) {
	if d == nil || d.ID == "" {
		return
	}
	id := d.ID
	// named by số hiệu, else title, else its catalogue id
	item := types.UserNotificationQueueItem{Ref: strings.TrimSpace(d.DocumentNumber), Title: strings.TrimSpace(d.Title)}
	if item.Ref == "" {
		item.Ref, item.Title = item.Title, ""
	}
	if item.Ref == "" {
		item.Ref = strings.TrimSpace(d.LegalDocumentID)
	}
	if item.Ref == "" {
		item.Untitled = true
	}
	n.notify(ctx, types.UserNotificationLegalDraftReady, d.CreatedBy,
		func(ctx context.Context, db *gorm.DB) ([]string, []types.UserNotificationQueueItem, error) {
			found, err := queueHasRows(ctx, db, db.Model(&types.LegalDocumentDraft{}).
				Where("id = ? AND status = ?", id, types.LegalDraftReady))
			if err != nil || !found {
				return nil, nil, err
			}
			return []string{id}, []types.UserNotificationQueueItem{item}, nil
		})
}

// LegalDraftsChanged: a draft was opened, approved or discarded.
func (n *SuperAdminQueueNotifier) LegalDraftsChanged(ctx context.Context) {
	n.changed(ctx, types.UserNotificationLegalDraftReady)
}

// ---------------------------------------------------------------------------
// Legal feed links

// LegalLinksPending: one push of document ref (feed, feedDocID) left the
// links keys (LegalCandidateKey) newly waiting for review (auto-approved
// ones are not among them). The links are re-read by key, not by row id:
// a re-push within the batch recreates the rows with new ids.
func (n *SuperAdminQueueNotifier) LegalLinksPending(ctx context.Context, actor, ref, feed, feedDocID string, keys []string) {
	if len(keys) == 0 {
		return
	}
	want := make(map[string]bool, len(keys))
	for _, k := range keys {
		want[k] = true
	}
	ref = strings.TrimSpace(ref)
	n.notify(ctx, types.UserNotificationLegalLinkPending, actor,
		func(ctx context.Context, db *gorm.DB) ([]string, []types.UserNotificationQueueItem, error) {
			var rows []*types.LegalProvisionLinkCandidate
			if err := db.WithContext(ctx).Where("feed = ? AND feed_doc_id = ? AND review_status = ?", feed, feedDocID,
				types.LegalLinkReviewPending).Find(&rows).Error; err != nil {
				return nil, nil, err
			}
			var still []string
			seen := map[string]bool{}
			for _, c := range rows {
				if k := LegalCandidateKey(c); want[k] && !seen[k] {
					seen[k] = true
					still = append(still, feed+"|"+feedDocID+"|"+k)
				}
			}
			if len(still) == 0 {
				return nil, nil, nil
			}
			return still, []types.UserNotificationQueueItem{{Ref: ref, Count: len(still)}}, nil
		})
}

// LegalLinksChanged: a candidate was decided.
func (n *SuperAdminQueueNotifier) LegalLinksChanged(ctx context.Context) {
	n.changed(ctx, types.UserNotificationLegalLinkPending)
}

// ---------------------------------------------------------------------------
// Legal identity review

// LegalIdentitiesPending: one scan (or one feed ingest) left the review
// links of knowledgeIDs pending; refs names them (the parsed số hiệu, the
// file title when none).
func (n *SuperAdminQueueNotifier) LegalIdentitiesPending(ctx context.Context, actor string, knowledgeIDs []string, refs map[string]string) {
	if len(knowledgeIDs) == 0 {
		return
	}
	ids := append([]string(nil), knowledgeIDs...)
	names := make(map[string]string, len(refs))
	for k, v := range refs {
		names[k] = v
	}
	n.notify(ctx, types.UserNotificationLegalIdentityPending, actor,
		func(ctx context.Context, db *gorm.DB) ([]string, []types.UserNotificationQueueItem, error) {
			var still []string
			for start := 0; start < len(ids); start += superAdminQueueIDChunk {
				var part []string
				end := min(start+superAdminQueueIDChunk, len(ids))
				if err := db.WithContext(ctx).Model(&types.LegalDocumentLink{}).
					Where("knowledge_id IN ? AND review_status = ?", ids[start:end], types.LegalReviewPending).
					Pluck("knowledge_id", &part).Error; err != nil {
					return nil, nil, err
				}
				still = append(still, part...)
			}
			if len(still) == 0 {
				return nil, nil, nil
			}
			// the newest named first: the last ones of the batch
			named := still[max(0, len(still)-types.UserNotificationQueueMaxItems):]
			var untitled []string
			for _, id := range named {
				if strings.TrimSpace(names[id]) == "" {
					untitled = append(untitled, id)
				}
			}
			if len(untitled) > 0 {
				var rows []struct{ ID, Title string }
				if err := db.WithContext(ctx).Table("knowledges").Select("id, title").
					Where("id IN ?", untitled).Scan(&rows).Error; err != nil {
					logger.Warnf(ctx, "[QueueNotice] titles of %v: %v", untitled, err)
				}
				for _, r := range rows {
					names[r.ID] = strings.TrimSpace(r.Title)
				}
			}
			var items []types.UserNotificationQueueItem
			for i := len(named) - 1; i >= 0; i-- {
				// neither số hiệu nor title: still named, as untitled
				ref := strings.TrimSpace(names[named[i]])
				items = append(items, types.UserNotificationQueueItem{Ref: ref, Untitled: ref == ""})
			}
			return still, items, nil
		})
}

// LegalIdentitiesChanged: an identity was approved or rejected.
func (n *SuperAdminQueueNotifier) LegalIdentitiesChanged(ctx context.Context) {
	n.changed(ctx, types.UserNotificationLegalIdentityPending)
}

// ---------------------------------------------------------------------------
// Digest wording

// merge adds a batch of delta items to the unread digest prev: the batch's
// items first among the newest named ones (an item of the same Ref is
// replaced).
func (q *reviewQueue) merge(delta int, items []types.UserNotificationQueueItem) func(prev, next *types.UserNotification) {
	return func(prev, next *types.UserNotification) {
		var old types.UserNotificationQueuePayload
		if len(prev.Payload) > 0 {
			_ = json.Unmarshal(prev.Payload, &old)
		}
		count := old.Count
		if count < len(old.Items) {
			count = len(old.Items)
		}
		merged := types.UserNotificationQueuePayload{Count: count + delta, Items: prependQueueItems(items, old.Items)}
		*next = *q.notification(next.UserID, merged, next.CreatedAt)
	}
}

// notification builds the digest of userID. Title and body are the stored
// (Vietnamese) text; the clients word it from the payload in the reader's
// language.
func (q *reviewQueue) notification(userID string, p types.UserNotificationQueuePayload, at time.Time) *types.UserNotification {
	payload, _ := json.Marshal(p)
	return &types.UserNotification{
		TenantID:  types.PlatformTenantID,
		UserID:    userID,
		Kind:      q.kind,
		Title:     q.title(p.Count),
		Body:      q.body(p),
		Link:      q.link,
		Payload:   types.JSON(payload),
		DedupeKey: types.UserNotificationOpenKey(q.kind, userID),
		CreatedAt: at,
	}
}

func (q *reviewQueue) body(p types.UserNotificationQueuePayload) string {
	parts := make([]string, 0, len(p.Items)+1)
	for _, it := range p.Items {
		parts = append(parts, q.item(it))
	}
	if more := p.Count - len(p.Items); q.more != nil && more > 0 && len(p.Items) > 0 {
		parts = append(parts, q.more(more))
	}
	return queueEllipsis(strings.Join(parts, ", "), superAdminQueueBodyRunes)
}

// queueRefTitle: "53/2022/NĐ-CP – Quy định chi tiết…".
func queueRefTitle(it types.UserNotificationQueueItem) string {
	if it.Untitled || it.Ref == "" {
		return superAdminQueueUntitled
	}
	if it.Title == "" || strings.EqualFold(it.Title, it.Ref) {
		return queueEllipsis(it.Ref, superAdminQueueItemRunes)
	}
	return it.Ref + " – " + queueEllipsis(it.Title, superAdminQueueItemRunes)
}

func queueEllipsis(s string, n int) string { return abbrNoticeEllipsis(s, n) }
