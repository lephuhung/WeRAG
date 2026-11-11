package service

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// countingNotices counts the digest writes, to check one write per
// SuperAdmin per batch.
type countingNotices struct {
	interfaces.UserNotificationRepository
	upserts atomic.Int32
}

func (c *countingNotices) UpsertOpenDigest(
	ctx context.Context, n *types.UserNotification, merge func(prev, next *types.UserNotification),
) (bool, error) {
	c.upserts.Add(1)
	return c.UserNotificationRepository.UpsertOpenDigest(ctx, n, merge)
}

type queueNoticeEnv struct {
	db       *gorm.DB
	notices  *countingNotices
	notifier *SuperAdminQueueNotifier
}

// newQueueNoticeEnv: SuperAdmins sa1 and sa2 are told; sa-off (disabled),
// system-7 (synthetic) and member are not.
func newQueueNoticeEnv(t *testing.T, db *gorm.DB) *queueNoticeEnv {
	t.Helper()
	if db == nil {
		var err error
		db, err = gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
		require.NoError(t, err)
	}
	require.NoError(t, db.AutoMigrate(&types.Tenant{}, &types.User{}, &types.UserNotification{}, &types.UsageOrder{},
		&types.LegalDocumentRequest{}, &types.LegalRequestRequester{}, &types.LegalDocumentDraft{},
		&types.LegalProvisionLinkCandidate{}, &types.LegalDocumentLink{}, &types.KnowledgeBase{}, &types.Knowledge{}))
	for _, u := range []types.User{
		{ID: "sa1", Username: "sa1", Email: "sa1@x", IsActive: true, IsSystemAdmin: true},
		{ID: "sa2", Username: "sa2", Email: "sa2@x", IsActive: true, IsSystemAdmin: true},
		{ID: "sa-off", Username: "sa-off", Email: "off@x", IsActive: true, IsSystemAdmin: true},
		{ID: "system-7", Username: "synthetic", Email: "syn@x", IsActive: true, IsSystemAdmin: true},
		{ID: "member", Username: "member", Email: "m@x", IsActive: true},
	} {
		u := u
		require.NoError(t, db.Create(&u).Error)
	}
	require.NoError(t, db.Model(&types.User{}).Where("id = ?", "sa-off").Update("is_active", false).Error)
	notices := &countingNotices{UserNotificationRepository: repository.NewUserNotificationRepository(db)}
	return &queueNoticeEnv{db: db, notices: notices,
		notifier: NewSuperAdminQueueNotifier(repository.NewUserRepository(db), notices, db)}
}

// unread returns the unread digest of kind for user (nil: none).
func (e *queueNoticeEnv) unread(t *testing.T, user, kind string) *types.UserNotification {
	t.Helper()
	e.notifier.Wait()
	rows, err := e.notices.List(context.Background(), user, true, nil, 100)
	require.NoError(t, err)
	var found *types.UserNotification
	for _, n := range rows {
		if n.Kind == kind {
			require.Nil(t, found, "one unread digest per kind")
			found = n
		}
	}
	return found
}

func (e *queueNoticeEnv) payload(t *testing.T, user, kind string) types.UserNotificationQueuePayload {
	t.Helper()
	n := e.unread(t, user, kind)
	require.NotNil(t, n, "%s has an unread %s", user, kind)
	var p types.UserNotificationQueuePayload
	require.NoError(t, json.Unmarshal(n.Payload, &p))
	return p
}

// nobodyElse: the accounts that are no SuperAdmin to tell got nothing.
func (e *queueNoticeEnv) nobodyElse(t *testing.T) {
	t.Helper()
	e.notifier.Wait()
	for _, u := range []string{"member", "sa-off", "system-7"} {
		rows, err := e.notices.List(context.Background(), u, false, nil, 100)
		require.NoError(t, err)
		require.Empty(t, rows, u)
	}
}

func (e *queueNoticeEnv) closed(t *testing.T, kind string) {
	t.Helper()
	for _, sa := range []string{"sa1", "sa2"} {
		require.Nil(t, e.unread(t, sa, kind), "%s: %s closed", sa, kind)
	}
}

// ---------------------------------------------------------------------------
// Payment orders

// queueBilling creates and settles orders in the test database.
type queueBilling struct {
	interfaces.UsageBillingService
	db *gorm.DB
}

func (b *queueBilling) CreateOrder(_ context.Context, req types.UsageQuoteRequest, scopeID, createdBy string) (*types.UsageOrderView, error) {
	o := &types.UsageOrder{ID: uuid.NewString(), OrderCode: "DH" + scopeID, ScopeType: "tenant", ScopeID: scopeID,
		PlanName: "Gói Go", TotalVND: 1250000, Status: types.UsageOrderPending, CreatedBy: createdBy}
	switch scopeID {
	case "zero":
		// 0 đồng: never confirmed by a transfer, waits for a SuperAdmin
		o.TotalVND = 0
	case "credit":
		// a plan change the credit of the replaced subscription pays: paid
		// at once
		o.TotalVND, o.Status = 0, types.UsageOrderPaid
	}
	if err := b.db.Create(o).Error; err != nil {
		return nil, err
	}
	return &types.UsageOrderView{UsageOrder: o, ScopeName: "Sở Tư pháp " + scopeID}, nil
}

func (b *queueBilling) setStatus(id, status string) error {
	return b.db.Model(&types.UsageOrder{}).Where("id = ?", id).Update("status", status).Error
}

func (b *queueBilling) ConfirmOrder(_ context.Context, id, _ string) (*types.UsageOrderView, error) {
	return nil, b.setStatus(id, types.UsageOrderPaid)
}

func (b *queueBilling) CancelOrder(_ context.Context, id string, _ bool) (*types.UsageOrderView, error) {
	return nil, b.setStatus(id, types.UsageOrderCancelled)
}

func TestQueueNoticePaymentOrders(t *testing.T) {
	e := newQueueNoticeEnv(t, nil)
	inner := &queueBilling{db: e.db}
	billing := NewBillingQueueNotifications(inner, e.notifier)
	ctx := context.Background()
	kind := types.UserNotificationPaymentOrderPending

	a, err := billing.CreateOrder(ctx, types.UsageQuoteRequest{}, "a", "member")
	require.NoError(t, err)
	for _, sa := range []string{"sa1", "sa2"} {
		n := e.unread(t, sa, kind)
		require.NotNil(t, n, sa)
		require.Equal(t, "Có 1 đơn hàng mới chờ xác nhận thanh toán", n.Title)
		require.Equal(t, "DHa · Gói Go · 1.250.000 đ · Sở Tư pháp a", n.Body)
		require.Equal(t, PaymentOrderReviewLink, n.Link)
		require.Equal(t, types.PlatformTenantID, n.TenantID)
		p := e.payload(t, sa, kind)
		require.Equal(t, types.UserNotificationQueuePayload{Count: 1, Items: []types.UserNotificationQueueItem{
			{Ref: "DHa", Plan: "Gói Go", AmountVND: 1250000, Org: "Sở Tư pháp a"}}}, p)
	}
	e.nobodyElse(t)

	// a SuperAdmin's own order does not notify them; the count accumulates
	b, err := billing.CreateOrder(ctx, types.UsageQuoteRequest{}, "b", "sa1")
	require.NoError(t, err)
	require.Equal(t, 1, e.payload(t, "sa1", kind).Count)
	p := e.payload(t, "sa2", kind)
	require.Equal(t, 2, p.Count)
	require.Equal(t, "DHb", p.Items[0].Ref)
	require.Equal(t, "Có 2 đơn hàng mới chờ xác nhận thanh toán", e.unread(t, "sa2", kind).Title)

	// an order paid at once waits for nobody
	_, err = billing.CreateOrder(ctx, types.UsageQuoteRequest{}, "credit", "member")
	require.NoError(t, err)
	require.Equal(t, 2, e.payload(t, "sa2", kind).Count)

	// a 0-đồng order waits for a SuperAdmin most: told, and never expires
	zero, err := billing.CreateOrder(ctx, types.UsageQuoteRequest{}, "zero", "member")
	require.NoError(t, err)
	p = e.payload(t, "sa2", kind)
	require.Equal(t, 3, p.Count)
	require.Equal(t, types.UserNotificationQueueItem{Ref: "DHzero", Plan: "Gói Go", Org: "Sở Tư pháp zero"}, p.Items[0])
	require.Equal(t, "DHzero · Gói Go · Sở Tư pháp zero", e.unread(t, "sa2", kind).Body[:len("DHzero · Gói Go · Sở Tư pháp zero")])
	longAgo := time.Now().Add(-72 * time.Hour)
	require.NoError(t, e.db.Model(&types.UsageOrder{}).Where("id = ?", zero.ID).Update("expires_at", longAgo).Error)

	// an order paid before the notice was written is no news
	c, err := inner.CreateOrder(ctx, types.UsageQuoteRequest{}, "c", "member")
	require.NoError(t, err)
	require.NoError(t, inner.setStatus(c.ID, types.UsageOrderPaid))
	e.notifier.OrderPending(ctx, c, "member")
	require.Equal(t, 3, e.payload(t, "sa2", kind).Count)

	// confirming / cancelling keeps the digest while an order still waits
	_, err = billing.ConfirmOrder(ctx, a.ID, "sa1")
	require.NoError(t, err)
	require.NotNil(t, e.unread(t, "sa2", kind))
	_, err = billing.CancelOrder(ctx, b.ID, false)
	require.NoError(t, err)
	e.notifier.Wait()
	require.NotNil(t, e.unread(t, "sa2", kind), "the 0-đồng order past its deadline still waits")
	_, err = billing.ConfirmOrder(ctx, zero.ID, "sa1")
	require.NoError(t, err)
	e.closed(t, kind)

	// an order past its deadline counts as expired: the sweep closes
	_, err = billing.CreateOrder(ctx, types.UsageQuoteRequest{}, "d", "member")
	require.NoError(t, err)
	require.Equal(t, 1, e.payload(t, "sa2", kind).Count, "a fresh digest after the closing")
	past := time.Now().Add(-time.Hour)
	require.NoError(t, e.db.Model(&types.UsageOrder{}).Where("order_code = ?", "DHd").Update("expires_at", past).Error)
	e.notifier.Sweep(ctx)
	e.closed(t, kind)
}

// ---------------------------------------------------------------------------
// Legal document requests

func TestQueueNoticeLegalRequests(t *testing.T) {
	e := newQueueNoticeEnv(t, nil)
	ctx := context.Background()
	kind := types.UserNotificationLegalRequestPending
	requests := repository.NewLegalRequestRepository(e.db)
	nd53 := &types.LegalDocument{ID: "155366", DocumentNumber: "53/2022/NĐ-CP", DocType: "Nghị định",
		Title: "Quy định chi tiết Luật An ninh mạng", Status: "Còn hiệu lực"}
	tt05 := &types.LegalDocument{ID: "160001", DocumentNumber: "05/2023/TT-BNV", DocType: "Thông tư",
		Title: "Hướng dẫn công tác văn thư", Status: "Còn hiệu lực"}
	catalog := &gateCatalog{reports: map[string][]*types.LegalStatusReport{
		"53/2022/ND-CP": {{Document: nd53}}, "05/2023/TT-BNV": {{Document: tt05}},
		"12/2025/ND-CP": {{Document: &types.LegalDocument{ID: "170000", DocumentNumber: "12/2025/NĐ-CP", DocType: "Nghị định",
			Status: "Còn hiệu lực"}}}}}
	s := &sessionService{legalCatalog: catalog}
	s.SetLegalRequests(requests, &gateReviews{approved: map[string]bool{}})
	s.SetQueueNotices(e.notifier)
	ask := func(user, session, query string) {
		t.Helper()
		req := &types.QARequest{Query: query, Session: &types.Session{ID: session, TenantID: 10003, UserID: user},
			CustomAgent: &types.CustomAgent{ID: types.BuiltinSmartReasoningID}}
		_, _, err := s.legalMissingDocumentGate(ctx, req, event.NewEventBus(), []string{"kb-own"})
		require.NoError(t, err)
		e.notifier.Wait()
	}

	ask("member", "s1", "Nghị định 53/2022/NĐ-CP quy định gì?")
	for _, sa := range []string{"sa1", "sa2"} {
		n := e.unread(t, sa, kind)
		require.NotNil(t, n, sa)
		require.Equal(t, "Có 1 văn bản mới được đề nghị bổ sung", n.Title)
		require.Equal(t, "53/2022/NĐ-CP – Quy định chi tiết Luật An ninh mạng", n.Body)
		require.Equal(t, LegalRequestReviewLink, n.Link)
		require.Equal(t, []types.UserNotificationQueueItem{{Ref: "53/2022/NĐ-CP", Title: nd53.Title, Count: 1}},
			e.payload(t, sa, kind).Items)
	}
	e.nobodyElse(t)

	// asked again while open (another person): the request is not new
	before := e.notices.upserts.Load()
	ask("sa2", "s2", "Nghị định 53/2022/NĐ-CP quy định gì?")
	require.Equal(t, before, e.notices.upserts.Load())
	require.Equal(t, 1, e.payload(t, "sa1", kind).Count)

	// a SuperAdmin's own request does not notify them
	ask("sa1", "s3", "Thông tư 05/2023/TT-BNV quy định gì?")
	require.Equal(t, 1, e.payload(t, "sa1", kind).Count)
	p := e.payload(t, "sa2", kind)
	require.Equal(t, 2, p.Count)
	require.Equal(t, "05/2023/TT-BNV", p.Items[0].Ref)

	// an API key asking is no person
	ask(types.SessionOwnerAPITenantKeyPrefix+"7", "s4", "Thông tư 99/2024/TT-BTC quy định gì?")
	require.Equal(t, 2, e.payload(t, "sa2", kind).Count)

	// a feed / import / no-text queue row (Record without a person) notifies
	// nobody and does not keep the digests open
	feedRow, err := requests.Record(ctx, &types.LegalDocumentRequest{NumberKey: vietnamese_legal.CatalogNumberKey("12/2025/NĐ-CP"),
		DocumentNumber: "12/2025/NĐ-CP", LegalDocumentID: "170000", Source: types.LegalRequestSourceNoFullText})
	require.NoError(t, err)
	require.Equal(t, 1, feedRow.RequestCount)
	require.Equal(t, 2, e.payload(t, "sa2", kind).Count)

	// a request handled before the notice was written is no news
	var rows []*types.LegalDocumentRequest
	require.NoError(t, e.db.Where("legal_document_id IN ?", []string{"155366", "160001"}).Find(&rows).Error)
	require.Len(t, rows, 2)
	for _, r := range rows {
		_, err := requests.Decide(ctx, r.ID, types.LegalRequestAdded, "sa1", "", "k-"+r.ID, "kb-pub")
		require.NoError(t, err)
	}
	before = e.notices.upserts.Load()
	e.notifier.LegalRequestsOpened(ctx, "member", []string{rows[0].ID})
	e.notifier.Wait()
	require.Equal(t, before, e.notices.upserts.Load(), "nothing written")
	// both added: only the feed's row is left, nobody waits on it, and the
	// batch's own check closed the digests
	e.closed(t, kind)

	// a person's first ask on the feed's open row: now someone waits on it
	ask("member", "s6", "Nghị định 12/2025/NĐ-CP quy định gì?")
	p = e.payload(t, "sa2", kind)
	require.Equal(t, 1, p.Count)
	require.Equal(t, "12/2025/NĐ-CP", p.Items[0].Ref)
	require.Equal(t, 2, p.Items[0].Count, "the feed's request and the person's")
	// the same person asking again while waiting is no news
	before = e.notices.upserts.Load()
	ask("member", "s7", "Nghị định 12/2025/NĐ-CP quy định gì?")
	require.Equal(t, before, e.notices.upserts.Load())
	_, err = requests.Decide(ctx, feedRow.ID, types.LegalRequestRejected, "sa1", "", "", "")
	require.NoError(t, err)
	e.notifier.LegalRequestsChanged(ctx)
	e.closed(t, kind)

	// asked for again after it was added (its askers told): reopened
	require.NoError(t, e.db.Model(&types.LegalRequestRequester{}).Where("1 = 1").Update("notified_at", time.Now()).Error)
	ask("member", "s5", "Nghị định 53/2022/NĐ-CP quy định gì?")
	p = e.payload(t, "sa1", kind)
	require.Equal(t, 1, p.Count)
	require.Equal(t, "53/2022/NĐ-CP", p.Items[0].Ref)
}

// ---------------------------------------------------------------------------
// Legal OCR drafts

func TestQueueNoticeLegalDrafts(t *testing.T) {
	e := newQueueNoticeEnv(t, nil)
	ctx := context.Background()
	kind := types.UserNotificationLegalDraftReady
	drafts := repository.NewLegalDraftRepository(e.db)
	svc := &LegalDraftService{drafts: drafts, Notices: e.notifier,
		OCR: func(context.Context, []byte, string) (string, int, string, error) {
			return "<!-- page 1 -->\nCHÍNH PHỦ\nNghị định số 53/2022/NĐ-CP quy định chi tiết một số điều của Luật An ninh mạng. " +
				"Điều 1. Phạm vi điều chỉnh. Nghị định này quy định chi tiết các điều khoản.", 1, "test", nil
		}}
	draft := func(id, number, by string) *types.LegalDocumentDraft {
		d := &types.LegalDocumentDraft{ID: id, LegalDocumentID: "doc-" + id, DocumentNumber: number, DocType: "Nghị định",
			Title: "Quy định chi tiết " + number, Status: types.LegalDraftOCR, CreatedBy: by}
		require.NoError(t, drafts.Create(ctx, d))
		return d
	}

	// a SuperAdmin's single fetch: the others are told
	svc.runOCR(ctx, draft("d1", "53/2022/NĐ-CP", "sa1"), []byte("%PDF"))
	require.Nil(t, e.unread(t, "sa1", kind))
	n := e.unread(t, "sa2", kind)
	require.NotNil(t, n)
	require.Equal(t, "Có 1 bản nháp OCR mới chờ duyệt", n.Title)
	require.Equal(t, "53/2022/NĐ-CP – Quy định chi tiết 53/2022/NĐ-CP", n.Body)
	require.Equal(t, LegalDraftReviewLink, n.Link)
	e.nobodyElse(t)

	// a feed's scan: everyone; the count accumulates
	svc.runOCR(ctx, draft("d2", "12/2025/NĐ-CP", "mcp:feed"), []byte("%PDF"))
	require.Equal(t, 1, e.payload(t, "sa1", kind).Count)
	p := e.payload(t, "sa2", kind)
	require.Equal(t, 2, p.Count)
	require.Equal(t, "12/2025/NĐ-CP", p.Items[0].Ref)

	// opened for review before the notice was written: no news
	d3 := draft("d3", "13/2025/NĐ-CP", "mcp:feed")
	require.NoError(t, drafts.Update(ctx, d3.ID, map[string]any{"status": types.LegalDraftReviewing}))
	e.notifier.LegalDraftReady(ctx, d3)
	require.Equal(t, 2, e.payload(t, "sa2", kind).Count)

	// discarding the ready ones empties the queue (d3 is under review)
	require.NoError(t, svc.Discard(ctx, "d1"))
	e.notifier.Wait()
	require.NotNil(t, e.unread(t, "sa2", kind))
	require.NoError(t, svc.Discard(ctx, "d2"))
	e.closed(t, kind)

	// a feed run of three scans is one write per SuperAdmin; a draft with
	// neither số hiệu nor title is named by its catalogue id
	before := e.notices.upserts.Load()
	svc.runOCR(ctx, draft("d4", "14/2025/NĐ-CP", "mcp:feed"), []byte("%PDF"))
	svc.runOCR(ctx, draft("d5", "15/2025/NĐ-CP", "mcp:feed"), []byte("%PDF"))
	bare := &types.LegalDocumentDraft{ID: "d6", LegalDocumentID: "tvpl-77", Status: types.LegalDraftOCR, CreatedBy: "mcp:feed"}
	require.NoError(t, drafts.Create(ctx, bare))
	svc.runOCR(ctx, bare, []byte("%PDF"))
	p = e.payload(t, "sa2", kind)
	require.EqualValues(t, 2, e.notices.upserts.Load()-before, "one write each for sa1 and sa2")
	require.Equal(t, 3, p.Count)
	require.Equal(t, []string{"tvpl-77", "15/2025/NĐ-CP", "14/2025/NĐ-CP"}, []string{p.Items[0].Ref, p.Items[1].Ref, p.Items[2].Ref})

	// a draft opened for review and left alone for a day waits again: the
	// digest stays open when the ready ones are gone
	for _, id := range []string{"d4", "d5", "d6"} {
		require.NoError(t, svc.Discard(ctx, id))
	}
	require.NoError(t, e.db.Model(&types.LegalDocumentDraft{}).Where("id = ?", "d3").
		Update("updated_at", time.Now().Add(-25*time.Hour)).Error)
	e.notifier.LegalDraftsChanged(ctx)
	e.notifier.Wait()
	require.NotNil(t, e.unread(t, "sa2", kind))
	require.NoError(t, svc.Discard(ctx, "d3"))
	e.closed(t, kind)
}

// Batches are written on their own once the queue is quiet, and on
// shutdown at once.
func TestQueueNoticeCoalescingTimers(t *testing.T) {
	e := newQueueNoticeEnv(t, nil)
	ctx := context.Background()
	kind := types.UserNotificationLegalDraftReady
	drafts := repository.NewLegalDraftRepository(e.db)
	mk := func(id string) *types.LegalDocumentDraft {
		d := &types.LegalDocumentDraft{ID: id, LegalDocumentID: "doc-" + id, DocumentNumber: id + "/2025/NĐ-CP",
			Status: types.LegalDraftReady, CreatedBy: "mcp:feed"}
		require.NoError(t, drafts.Create(ctx, d))
		return d
	}
	unread := func(user string) *types.UserNotification {
		rows, err := e.notices.List(ctx, user, true, nil, 10)
		require.NoError(t, err)
		for _, n := range rows {
			if n.Kind == kind {
				return n
			}
		}
		return nil
	}

	e.notifier.quiet, e.notifier.maxWait = 30*time.Millisecond, time.Second
	e.notifier.LegalDraftReady(ctx, mk("1"))
	e.notifier.LegalDraftReady(ctx, mk("2"))
	require.Nil(t, unread("sa1"), "not before the queue is quiet")
	require.Eventually(t, func() bool { return unread("sa1") != nil }, 2*time.Second, 10*time.Millisecond)
	require.EqualValues(t, 2, e.notices.upserts.Load(), "both events in one write per SuperAdmin")

	// shutdown writes what still waits
	e.notifier.quiet, e.notifier.maxWait = time.Hour, time.Hour
	e.notifier.LegalDraftReady(ctx, mk("3"))
	require.True(t, e.notifier.WaitTimeout(5*time.Second))
	var p types.UserNotificationQueuePayload
	require.NoError(t, json.Unmarshal(unread("sa2").Payload, &p))
	require.Equal(t, 3, p.Count)
}

// ---------------------------------------------------------------------------
// Legal feed links

type queueSettings struct{ auto bool }

func (s queueSettings) GetBool(context.Context, string, string, bool) bool { return s.auto }

func TestQueueNoticeLegalLinks(t *testing.T) {
	kind := types.UserNotificationLegalLinkPending
	ctx := context.Background()
	caller := types.Caller{TenantID: types.PlatformTenantID}
	push := func(t *testing.T, auto bool) (*queueNoticeEnv, *LegalPushService, *LegalPushResult) {
		t.Helper()
		svc, _ := legalPushTestService(t)
		svc.settings = queueSettings{auto: auto}
		e := newQueueNoticeEnv(t, svc.db)
		svc.Notices = e.notifier
		res, err := svc.Push(ctx, LegalPushInput{Content: []byte(nd68Push), Metadata: legalPushMetaFor("68/2026/NĐ-CP")},
			caller, LegalFeedActor{UserID: "sa1"})
		require.NoError(t, err)
		e.notifier.Wait()
		return e, svc, res
	}

	// every link left to a person: one digest write per SuperAdmin for the push
	e, svc, res := push(t, false)
	require.Positive(t, res.Pending)
	require.Zero(t, res.AutoApproved)
	require.EqualValues(t, 1, e.notices.upserts.Load(), "one write (sa2; the pusher sa1 is not told)")
	require.Nil(t, e.unread(t, "sa1", kind))
	n := e.unread(t, "sa2", kind)
	require.NotNil(t, n)
	p := e.payload(t, "sa2", kind)
	require.Equal(t, res.Pending, p.Count)
	require.Equal(t, []types.UserNotificationQueueItem{{Ref: "68/2026/NĐ-CP", Count: res.Pending}}, p.Items)
	require.Equal(t, LegalLinkReviewLink, n.Link)
	e.nobodyElse(t)

	// a re-push of the unchanged document adds nothing
	before := e.notices.upserts.Load()
	res2, err := svc.Push(ctx, LegalPushInput{Content: []byte(nd68Push), Metadata: legalPushMetaFor("68/2026/NĐ-CP")},
		caller, LegalFeedActor{UserID: "sa1"})
	require.NoError(t, err)
	require.Equal(t, res.Pending, res2.Pending)
	e.notifier.Wait()
	require.Equal(t, before, e.notices.upserts.Load())
	require.Equal(t, res.Pending, e.payload(t, "sa2", kind).Count)

	// deciding every pending link closes the digests
	var ids []int64
	require.NoError(t, svc.db.Model(&types.LegalProvisionLinkCandidate{}).
		Where("review_status = ?", types.LegalLinkReviewPending).Pluck("id", &ids).Error)
	for _, id := range ids {
		_, err := svc.DecideCandidate(ctx, id, LegalLinkDecision{Action: "reject"}, types.Caller{UserID: "sa2"})
		require.NoError(t, err)
	}
	e.closed(t, kind)

	// auto-approved links need nobody: only the rest count
	e, svc, res = push(t, true)
	t.Logf("auto-approve on: %d pending, %d auto-approved", res.Pending, res.AutoApproved)
	require.Positive(t, res.AutoApproved)
	require.Positive(t, res.Pending)
	require.Equal(t, res.Pending, e.payload(t, "sa2", kind).Count)

	// links decided before the notice was written are no news
	var approved []*types.LegalProvisionLinkCandidate
	require.NoError(t, svc.db.Where("review_status = ?", types.LegalLinkReviewApproved).Find(&approved).Error)
	require.NotEmpty(t, approved)
	var approvedKeys []string
	for _, c := range approved {
		approvedKeys = append(approvedKeys, LegalLinkNoticeKey(c))
	}
	e.notifier.LegalLinksPending(ctx, "", "68/2026/NĐ-CP", "tvpl", "68", approvedKeys)
	e.notifier.Wait()
	require.Equal(t, res.Pending, e.payload(t, "sa2", kind).Count)
}

// A re-push within the batch recreates the pending rows with new ids: the
// first push's links still count, once.
func TestQueueNoticeLegalLinksRepushInBatch(t *testing.T) {
	kind := types.UserNotificationLegalLinkPending
	ctx := context.Background()
	caller := types.Caller{TenantID: types.PlatformTenantID}
	svc, _ := legalPushTestService(t)
	e := newQueueNoticeEnv(t, svc.db)
	svc.Notices = e.notifier
	pushOnce := func() *LegalPushResult {
		res, err := svc.Push(ctx, LegalPushInput{Content: []byte(nd68Push), Metadata: legalPushMetaFor("68/2026/NĐ-CP")},
			caller, LegalFeedActor{UserID: "sa1"})
		require.NoError(t, err)
		return res
	}
	first := pushOnce()
	var ids1 []int64
	require.NoError(t, svc.db.Model(&types.LegalProvisionLinkCandidate{}).Where("review_status = ?", types.LegalLinkReviewPending).
		Pluck("id", &ids1).Error)
	second := pushOnce() // no Wait: both pushes are in one batch
	var ids2 []int64
	require.NoError(t, svc.db.Model(&types.LegalProvisionLinkCandidate{}).Where("review_status = ?", types.LegalLinkReviewPending).
		Pluck("id", &ids2).Error)
	require.Equal(t, first.Pending, second.Pending)
	require.NotEqual(t, ids1, ids2, "the re-push recreated the rows")
	// an adoption before the batch is written moves the amender to its
	// adopting row (adoptCandidates rewrites amender_doc_id)
	require.NoError(t, svc.db.Model(&types.LegalProvisionLinkCandidate{}).Where("review_status = ?", types.LegalLinkReviewPending).
		Update("amender_doc_id", "adopted-row").Error)
	e.notifier.Wait()
	require.EqualValues(t, 1, e.notices.upserts.Load(), "one write (sa2)")
	require.Equal(t, first.Pending, e.payload(t, "sa2", kind).Count)
}

// ---------------------------------------------------------------------------
// Legal identity review

func TestQueueNoticeLegalIdentities(t *testing.T) {
	e := newQueueNoticeEnv(t, nil)
	db := e.db
	require.NoError(t, db.AutoMigrate(&types.Chunk{}, &types.LegalDocument{}, &types.LegalDocumentRelation{}))
	kind := types.UserNotificationLegalIdentityPending
	ctx := context.WithValue(context.Background(), types.UserIDContextKey, "sa1")
	catalog := repository.NewLegalCatalogRepository(db)
	svc := NewLegalReviewService(catalog, repository.NewLegalReviewRepository(db), nil, nil)
	svc.(*legalReviewService).SetQueueNotices(e.notifier)

	require.NoError(t, catalog.UpsertDocuments(ctx, []*types.LegalDocument{
		{ID: "vb-1", Source: types.LegalDocumentSourceVBPL, DocumentNumber: "01/2024/QH15", NumberKey: "01/2024/QH15", Title: "Luật 1"},
	}))
	require.NoError(t, db.Create(&types.KnowledgeBase{ID: "kb-pub", Name: "Luật", Visibility: types.KBVisibilityPublic}).Error)
	meta, _ := json.Marshal(map[string]string{"external_id": "vb-1"})
	for i, k := range []types.Knowledge{
		{ID: "k1", KnowledgeBaseID: "kb-pub", Title: "29.2018QH14.pdf", ParseStatus: types.ParseStatusCompleted},
		{ID: "k2", KnowledgeBaseID: "kb-pub", Title: "", ParseStatus: types.ParseStatusCompleted},
		{ID: "k3", KnowledgeBaseID: "kb-pub", Title: "luat-1.pdf", ParseStatus: types.ParseStatusCompleted, Metadata: meta},
	} {
		k := k
		require.NoError(t, db.Create(&k).Error)
		content := []string{"QUỐC HỘI Luật số: 29/2018/QH14\n\nLUẬT BẢO VỆ BÍ MẬT NHÀ NƯỚC", "Kính gửi các đơn vị", "QUỐC HỘI Luật số: 01/2024/QH15"}[i]
		require.NoError(t, db.Create(&types.Chunk{ID: "c-" + k.ID, KnowledgeID: k.ID, KnowledgeBaseID: "kb-pub",
			ChunkType: types.ChunkTypeText, IsEnabled: true, Content: content}).Error)
	}

	// one scan, one write per SuperAdmin: two pending, the vbpl.vn import
	// approved at once is not counted
	created, err := svc.Scan(ctx)
	require.NoError(t, err)
	require.Equal(t, 3, created)
	e.notifier.Wait()
	require.EqualValues(t, 1, e.notices.upserts.Load(), "sa2 only; the scanning sa1 is not told")
	require.Nil(t, e.unread(t, "sa1", kind))
	n := e.unread(t, "sa2", kind)
	require.NotNil(t, n)
	require.Equal(t, "Có 2 văn bản mới chờ duyệt định danh", n.Title)
	require.Equal(t, LegalIdentityReviewLink, n.Link)
	p := e.payload(t, "sa2", kind)
	require.Equal(t, 2, p.Count)
	require.ElementsMatch(t, []types.UserNotificationQueueItem{{Ref: "29/2018/QH14"}, {Untitled: true}}, p.Items)
	require.Contains(t, n.Body, "văn bản không số", "a document with neither số hiệu nor title is still named")
	e.nobodyElse(t)

	// a link reviewed before the notice was written is no news
	e.notifier.LegalIdentitiesPending(ctx, "", []string{"k3"}, nil)
	e.notifier.Wait()
	require.Equal(t, 2, e.payload(t, "sa2", kind).Count)

	// rejecting both empties the queue
	for _, k := range []string{"k1", "k2"} {
		_, err := svc.Decide(ctx, k, &types.LegalReviewDecision{Action: "reject"})
		require.NoError(t, err)
		e.notifier.Wait()
	}
	e.closed(t, kind)
}

func TestReviewQueueDigestBody(t *testing.T) {
	queues := map[string]*reviewQueue{}
	for _, q := range superAdminQueues() {
		queues[q.kind] = q
	}
	links := queues[types.UserNotificationLegalLinkPending]
	require.Equal(t, "68/2026/NĐ-CP (12 liên kết), 5/2026/NĐ-CP (3 liên kết)", links.body(types.UserNotificationQueuePayload{
		Count: 15, Items: []types.UserNotificationQueueItem{{Ref: "68/2026/NĐ-CP", Count: 12}, {Ref: "5/2026/NĐ-CP", Count: 3}}}),
		"links: no \"and N more\", the items carry the counts")
	req := queues[types.UserNotificationLegalRequestPending]
	require.Equal(t, "53/2022/NĐ-CP (3 lượt đề nghị), và 4 văn bản khác", req.body(types.UserNotificationQueuePayload{
		Count: 5, Items: []types.UserNotificationQueueItem{{Ref: "53/2022/NĐ-CP", Count: 3}}}))

	// merge: the batch first, an item of the same Ref replaced, capped
	prev := links.notification("u", types.UserNotificationQueuePayload{Count: 6, Items: []types.UserNotificationQueueItem{
		{Ref: "a", Count: 1}, {Ref: "b", Count: 1}, {Ref: "c", Count: 1}, {Ref: "d", Count: 1}, {Ref: "e", Count: 2}}}, time.Now())
	next := links.notification("u", types.UserNotificationQueuePayload{}, time.Now())
	links.merge(4, []types.UserNotificationQueueItem{{Ref: "C", Count: 4}})(prev, next)
	var p types.UserNotificationQueuePayload
	require.NoError(t, json.Unmarshal(next.Payload, &p))
	require.Equal(t, 10, p.Count)
	require.Equal(t, []types.UserNotificationQueueItem{{Ref: "C", Count: 5}, {Ref: "a", Count: 1}, {Ref: "b", Count: 1},
		{Ref: "d", Count: 1}, {Ref: "e", Count: 2}}, p.Items, "links of one document add up")

	// other queues keep the newest item of a Ref as it is (a request's count
	// is its total)
	require.Equal(t, []types.UserNotificationQueueItem{{Ref: "x", Count: 3}},
		prependQueueItems([]types.UserNotificationQueueItem{{Ref: "x", Count: 3}}, []types.UserNotificationQueueItem{{Ref: "X", Count: 2}}, req.sumCounts()))
}

// A document a legal feed pushed waits in the identity review: the ingest
// tells the SuperAdmins (a vbpl.vn ingest is approved at once and does not).
func TestQueueNoticeFeedIdentity(t *testing.T) {
	e := newQueueNoticeEnv(t, nil)
	kind := types.UserNotificationLegalIdentityPending
	feed := &LegalFeedService{Reviews: repository.NewLegalReviewRepository(e.db), Notices: e.notifier}
	ctx := context.Background()
	feed.saveReviewLink(ctx, LegalIngestDocument{VBPLID: "155366", DocumentNumber: "53/2022/NĐ-CP"}, "k-vbpl", "kb", true)
	e.notifier.Wait()
	require.Nil(t, e.unread(t, "sa1", kind))
	feed.saveReviewLink(ctx, LegalIngestDocument{VBPLID: "tvpl-1", DocumentNumber: "12/2025/NĐ-CP",
		ReviewedBy: LegalFeedReviewerPrefix + "tvpl"}, "k-feed", "kb", true)
	require.Equal(t, types.UserNotificationQueuePayload{Count: 1, Items: []types.UserNotificationQueueItem{{Ref: "12/2025/NĐ-CP"}}},
		e.payload(t, "sa1", kind))
}

// The same item queued twice in one batch (two people asking for a new
// request at once, a draft announced twice) counts once.
func TestQueueNoticeBatchCountsDistinctItems(t *testing.T) {
	e := newQueueNoticeEnv(t, nil)
	ctx := context.Background()
	req := &types.LegalDocumentRequest{ID: "r1", NumberKey: "53/2022/ND-CP", DocumentNumber: "53/2022/NĐ-CP",
		Status: types.LegalRequestRequested, RequestCount: 2, FirstRequestedAt: time.Now(), LastRequestedAt: time.Now()}
	require.NoError(t, e.db.Create(req).Error)
	for _, u := range []string{"u1", "u2"} {
		require.NoError(t, e.db.Create(&types.LegalRequestRequester{ID: "q-" + u, RequestID: "r1", UserID: u}).Error)
	}
	// both askers saw nobody waiting before them (check-then-insert race)
	e.notifier.LegalRequestsOpened(ctx, "u1", []string{"r1"})
	e.notifier.LegalRequestsOpened(ctx, "u2", []string{"r1"})
	d := &types.LegalDocumentDraft{ID: "d1", LegalDocumentID: "doc-1", DocumentNumber: "12/2025/NĐ-CP", Status: types.LegalDraftReady}
	require.NoError(t, e.db.Create(d).Error)
	e.notifier.LegalDraftReady(ctx, d)
	e.notifier.LegalDraftReady(ctx, d)
	require.Equal(t, 1, e.payload(t, "sa1", types.UserNotificationLegalRequestPending).Count)
	require.Equal(t, 1, e.payload(t, "sa1", types.UserNotificationLegalDraftReady).Count)
}
