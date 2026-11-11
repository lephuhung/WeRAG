package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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
// digest per SuperAdmin and queue, updated by each batch (count + newest
// items), closed for everyone once the queue is empty: by the action that
// emptied it, or by the periodic sweep (orders expire on their own, and a
// few rarer paths change the queues without telling the notifier).
//
// Every method returns at once; the work runs in the background, re-reads
// what is still pending before writing, and only logs failures.

// SuperAdminQueueSweepInterval is how often the sweep closes the digests
// of empty queues.
const SuperAdminQueueSweepInterval = 10 * time.Minute

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
)

// reviewQueue is one SuperAdmin review queue: its digest kind, page, the
// stored (Vietnamese) wording and the count of its pending items.
type reviewQueue struct {
	kind  string
	link  string
	title func(count int) string
	item  func(it types.UserNotificationQueueItem) string
	// more words the items the digest does not name; nil when the items
	// carry their own counts (links: count links, items name documents)
	more    func(n int) string
	pending func(ctx context.Context, db *gorm.DB, now time.Time) (int64, error)
}

// SuperAdminQueueNotifier writes the review-queue digests.
type SuperAdminQueueNotifier struct {
	digests *reviewDigests
	db      *gorm.DB
	queues  map[string]*reviewQueue
}

// NewSuperAdminQueueNotifier creates the notifier.
func NewSuperAdminQueueNotifier(
	users interfaces.UserRepository, notifications interfaces.UserNotificationRepository, db *gorm.DB,
) *SuperAdminQueueNotifier {
	n := &SuperAdminQueueNotifier{digests: newReviewDigests("QueueNotice", users, notifications), db: db,
		queues: map[string]*reviewQueue{}}
	for _, q := range superAdminQueues() {
		n.queues[q.kind] = q
	}
	return n
}

func superAdminQueues() []*reviewQueue {
	countWhere := func(model any, query string, args ...any) func(context.Context, *gorm.DB, time.Time) (int64, error) {
		return func(ctx context.Context, db *gorm.DB, _ time.Time) (int64, error) {
			var n int64
			err := db.WithContext(ctx).Model(model).Where(query, args...).Limit(1).Count(&n).Error
			return n, err
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
			more:    func(n int) string { return fmt.Sprintf("và %d đơn khác", n) },
			pending: pendingOrders,
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
			pending: pendingPersonRequests,
		},
		{
			kind: types.UserNotificationLegalDraftReady, link: LegalDraftReviewLink,
			title:   func(n int) string { return fmt.Sprintf("Có %d bản nháp OCR mới chờ duyệt", n) },
			item:    queueRefTitle,
			more:    func(n int) string { return fmt.Sprintf("và %d bản nháp khác", n) },
			pending: countWhere(&types.LegalDocumentDraft{}, "status = ?", types.LegalDraftReady),
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
			pending: countWhere(&types.LegalProvisionLinkCandidate{}, "review_status = ?", types.LegalLinkReviewPending),
		},
		{
			kind: types.UserNotificationLegalIdentityPending, link: LegalIdentityReviewLink,
			title: func(n int) string { return fmt.Sprintf("Có %d văn bản mới chờ duyệt định danh", n) },
			item: func(it types.UserNotificationQueueItem) string {
				return queueEllipsis(it.Ref, superAdminQueueItemRunes)
			},
			more:    func(n int) string { return fmt.Sprintf("và %d văn bản khác", n) },
			pending: countWhere(&types.LegalDocumentLink{}, "review_status = ?", types.LegalReviewPending),
		},
	}
}

// pendingOrders counts the orders still waiting for their money: pending
// and not past their deadline (expiry is applied lazily on reads, so a
// pending row past it is already expired).
func pendingOrders(ctx context.Context, db *gorm.DB, now time.Time) (int64, error) {
	var n int64
	err := db.WithContext(ctx).Model(&types.UsageOrder{}).
		Where("status = ? AND (expires_at IS NULL OR expires_at > ?)", types.UsageOrderPending, now).
		Limit(1).Count(&n).Error
	return n, err
}

// personRequestsSQL keeps the open requests a person waits on: requested,
// with someone who asked in chat and was not told it was added (feed,
// import and no-text queue rows have no such requester).
const personRequestsSQL = "status = ? AND EXISTS (SELECT 1 FROM legal_request_requesters q " +
	"WHERE q.request_id = legal_document_requests.id AND q.notified_at IS NULL)"

func pendingPersonRequests(ctx context.Context, db *gorm.DB, _ time.Time) (int64, error) {
	var n int64
	err := db.WithContext(ctx).Model(&types.LegalDocumentRequest{}).
		Where(personRequestsSQL, types.LegalRequestRequested).Limit(1).Count(&n).Error
	return n, err
}

// Wait blocks until the background runs started so far are done (tests).
func (n *SuperAdminQueueNotifier) Wait() {
	if n != nil {
		n.digests.wait()
	}
}

// WaitTimeout waits like Wait, at most d (shutdown).
func (n *SuperAdminQueueNotifier) WaitTimeout(d time.Duration) bool {
	if n == nil {
		return true
	}
	return n.digests.waitTimeout(d)
}

// notify adds one batch to the digests of kind for every SuperAdmin but
// actor. collect runs in the background and re-reads the batch: how many
// of its items are still pending and the items to name; 0 writes nothing
// (handled meanwhile).
func (n *SuperAdminQueueNotifier) notify(ctx context.Context, kind, actor string,
	collect func(ctx context.Context, db *gorm.DB) (int, []types.UserNotificationQueueItem, error),
) {
	if n == nil || n.db == nil {
		return
	}
	q := n.queues[kind]
	n.digests.background(ctx, "notifying "+kind, func(ctx context.Context) error {
		delta, items, err := collect(ctx, n.db)
		if err != nil {
			return fmt.Errorf("re-reading the batch: %w", err)
		}
		if delta <= 0 {
			return nil
		}
		if len(items) > types.UserNotificationQueueMaxItems {
			items = items[:types.UserNotificationQueueMaxItems]
		}
		first := types.UserNotificationQueuePayload{Count: delta, Items: items}
		writeErr := n.digests.write(ctx, actor, func(userID string, at time.Time) *types.UserNotification {
			return q.notification(userID, first, at)
		}, q.merge(delta, items))
		// the queue may have emptied while the digests were written
		closeErr := n.closeIfEmpty(ctx, q)
		if writeErr != nil {
			return writeErr
		}
		return closeErr
	})
}

// changed closes the digests of kind once its queue is empty (called after
// an action that may have emptied it).
func (n *SuperAdminQueueNotifier) changed(ctx context.Context, kind string) {
	if n == nil || n.db == nil {
		return
	}
	q := n.queues[kind]
	n.digests.background(ctx, "closing "+kind, func(ctx context.Context) error { return n.closeIfEmpty(ctx, q) })
}

func (n *SuperAdminQueueNotifier) closeIfEmpty(ctx context.Context, q *reviewQueue) error {
	return n.digests.closeIfEmpty(ctx, q.kind, func(ctx context.Context) (int64, error) {
		return q.pending(ctx, n.db, n.digests.now())
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
// its transfer. Anything but a pending order notifies nobody.
func (n *SuperAdminQueueNotifier) OrderPending(ctx context.Context, v *types.UsageOrderView, actor string) {
	if v == nil || v.UsageOrder == nil || v.Status != types.UsageOrderPending {
		return
	}
	id := v.ID
	item := types.UserNotificationQueueItem{Ref: v.OrderCode, Plan: strings.TrimSpace(v.PlanName), AmountVND: v.TotalVND,
		Org: strings.TrimSpace(v.ScopeName)}
	n.notify(ctx, types.UserNotificationPaymentOrderPending, actor,
		func(ctx context.Context, db *gorm.DB) (int, []types.UserNotificationQueueItem, error) {
			c, err := pendingOrders(ctx, db.Where("id = ?", id), n.digests.now())
			if err != nil || c == 0 {
				return 0, nil, err
			}
			return 1, []types.UserNotificationQueueItem{item}, nil
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
		func(ctx context.Context, db *gorm.DB) (int, []types.UserNotificationQueueItem, error) {
			var rows []*types.LegalDocumentRequest
			if err := db.WithContext(ctx).Where("id IN ?", ids).Where(personRequestsSQL, types.LegalRequestRequested).
				Order("request_count DESC").Find(&rows).Error; err != nil {
				return 0, nil, err
			}
			// the rows of one số hiệu (several catalogue matches) are one item
			var items []types.UserNotificationQueueItem
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
				ref := strings.TrimSpace(r.DocumentNumber)
				if ref == "" {
					ref = r.NumberKey
				}
				items = append(items, types.UserNotificationQueueItem{Ref: ref, Title: strings.TrimSpace(r.Title), Count: r.RequestCount})
			}
			return len(items), items, nil
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
	item := types.UserNotificationQueueItem{Ref: strings.TrimSpace(d.DocumentNumber), Title: strings.TrimSpace(d.Title)}
	if item.Ref == "" {
		item.Ref, item.Title = item.Title, ""
	}
	n.notify(ctx, types.UserNotificationLegalDraftReady, d.CreatedBy,
		func(ctx context.Context, db *gorm.DB) (int, []types.UserNotificationQueueItem, error) {
			var c int64
			if err := db.WithContext(ctx).Model(&types.LegalDocumentDraft{}).
				Where("id = ? AND status = ?", id, types.LegalDraftReady).Count(&c).Error; err != nil || c == 0 {
				return 0, nil, err
			}
			return 1, []types.UserNotificationQueueItem{item}, nil
		})
}

// LegalDraftsChanged: a draft was opened, approved or discarded.
func (n *SuperAdminQueueNotifier) LegalDraftsChanged(ctx context.Context) {
	n.changed(ctx, types.UserNotificationLegalDraftReady)
}

// ---------------------------------------------------------------------------
// Legal feed links

// LegalLinksPending: one push of document ref left candidates ids waiting
// for review (auto-approved ones are not among them).
func (n *SuperAdminQueueNotifier) LegalLinksPending(ctx context.Context, actor, ref string, ids []int64) {
	if len(ids) == 0 {
		return
	}
	ids = append([]int64(nil), ids...)
	ref = strings.TrimSpace(ref)
	n.notify(ctx, types.UserNotificationLegalLinkPending, actor,
		func(ctx context.Context, db *gorm.DB) (int, []types.UserNotificationQueueItem, error) {
			var c int64
			for start := 0; start < len(ids); start += superAdminQueueIDChunk {
				var part int64
				end := min(start+superAdminQueueIDChunk, len(ids))
				if err := db.WithContext(ctx).Model(&types.LegalProvisionLinkCandidate{}).
					Where("id IN ? AND review_status = ?", ids[start:end], types.LegalLinkReviewPending).
					Count(&part).Error; err != nil {
					return 0, nil, err
				}
				c += part
			}
			if c == 0 {
				return 0, nil, nil
			}
			return int(c), []types.UserNotificationQueueItem{{Ref: ref, Count: int(c)}}, nil
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
		func(ctx context.Context, db *gorm.DB) (int, []types.UserNotificationQueueItem, error) {
			var still []string
			for start := 0; start < len(ids); start += superAdminQueueIDChunk {
				var part []string
				end := min(start+superAdminQueueIDChunk, len(ids))
				if err := db.WithContext(ctx).Model(&types.LegalDocumentLink{}).
					Where("knowledge_id IN ? AND review_status = ?", ids[start:end], types.LegalReviewPending).
					Pluck("knowledge_id", &part).Error; err != nil {
					return 0, nil, err
				}
				still = append(still, part...)
			}
			if len(still) == 0 {
				return 0, nil, nil
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
				if ref := strings.TrimSpace(names[named[i]]); ref != "" {
					items = append(items, types.UserNotificationQueueItem{Ref: ref})
				}
			}
			return len(still), items, nil
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
		merged := types.UserNotificationQueuePayload{Count: count + delta, Items: append([]types.UserNotificationQueueItem(nil), items...)}
		for _, it := range old.Items {
			if len(merged.Items) >= types.UserNotificationQueueMaxItems {
				break
			}
			dup := false
			for _, n := range items {
				if strings.EqualFold(n.Ref, it.Ref) {
					dup = true
					break
				}
			}
			if !dup {
				merged.Items = append(merged.Items, it)
			}
		}
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
	if it.Title == "" || strings.EqualFold(it.Title, it.Ref) {
		return queueEllipsis(it.Ref, superAdminQueueItemRunes)
	}
	return it.Ref + " – " + queueEllipsis(it.Title, superAdminQueueItemRunes)
}

func queueEllipsis(s string, n int) string { return abbrNoticeEllipsis(s, n) }
