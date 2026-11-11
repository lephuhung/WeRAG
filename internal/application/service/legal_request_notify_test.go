package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type noticeFixture struct {
	db       *gorm.DB
	requests interfaces.LegalRequestRepository
	notifier *LegalRequestNotifier
}

func newNoticeFixture(t *testing.T) *noticeFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.LegalDocumentRequest{}, &types.LegalRequestRequester{}, &types.UserNotification{}))
	for _, ddl := range []string{
		`CREATE TABLE knowledges (id TEXT, parse_status TEXT, deleted_at DATETIME)`,
		`CREATE TABLE knowledge_bases (id TEXT, name TEXT, owner_tenant_id INTEGER, deleted_at DATETIME)`,
		`CREATE TABLE sessions (id TEXT, deleted_at DATETIME)`,
		`CREATE TABLE document_workspaces (id TEXT, session_id TEXT, word_file_id TEXT, updated_at DATETIME, deleted_at DATETIME)`,
	} {
		require.NoError(t, db.Exec(ddl).Error)
	}
	requests := repository.NewLegalRequestRepository(db)
	return &noticeFixture{db: db, requests: requests,
		notifier: NewLegalRequestNotifier(requests, repository.NewUserNotificationRepository(db))}
}

// ask records a request for number and its requesters (user → session,
// each session existing).
func (f *noticeFixture) ask(t *testing.T, number, docID, channel string, askers map[string]string) *types.LegalDocumentRequest {
	t.Helper()
	ctx := context.Background()
	var row *types.LegalDocumentRequest
	for user, session := range askers {
		var err error
		row, err = f.requests.Record(ctx, &types.LegalDocumentRequest{NumberKey: number, DocumentNumber: number,
			LegalDocumentID: docID, Title: "Quy định chi tiết một số điều của Luật An ninh mạng",
			LastUserID: user, LastSessionID: session})
		require.NoError(t, err)
		require.NoError(t, f.db.Exec(`INSERT INTO sessions (id) VALUES (?)`, session).Error)
		require.NoError(t, f.requests.AddRequester(ctx, &types.LegalRequestRequester{RequestID: row.ID, TenantID: 7,
			UserID: user, SessionID: session, Channel: channel}))
	}
	return row
}

func (f *noticeFixture) notifications(t *testing.T, user string) []types.UserNotification {
	t.Helper()
	var rows []types.UserNotification
	q := f.db.Order("created_at")
	if user != "" {
		q = q.Where("user_id = ?", user)
	}
	require.NoError(t, q.Find(&rows).Error)
	return rows
}

// Every requester of a request closed as added gets one notice of their
// own; another sweep, or the request stamped added again (an ingest, then
// the decision), notifies nobody again.
func TestLegalRequestNoticesOncePerRequester(t *testing.T) {
	f := newNoticeFixture(t)
	ctx := context.Background()
	row := f.ask(t, "53/2022/NĐ-CP", "155366", "web", map[string]string{"an": "s-an", "binh": "s-binh"})

	n, err := f.notifier.Run(ctx)
	require.NoError(t, err)
	require.Zero(t, n, "an open request notifies nobody")

	_, err = f.requests.MarkAddedByDocument(ctx, "155366", "", "", "feed")
	require.NoError(t, err)
	_, err = f.requests.Decide(ctx, row.ID, types.LegalRequestAdded, "sa", "", "", "")
	require.NoError(t, err)
	n, err = f.notifier.Run(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, n)

	an := f.notifications(t, "an")
	require.Len(t, an, 1)
	require.Equal(t, types.UserNotificationLegalRequestAdded, an[0].Kind)
	require.Equal(t, "Văn bản 53/2022/NĐ-CP đã được bổ sung", an[0].Title)
	require.Equal(t, "Quy định chi tiết một số điều của Luật An ninh mạng. Bạn có thể hỏi lại ngay trong cuộc trò chuyện trước đó.", an[0].Body)
	require.Equal(t, "/platform/chat/s-an", an[0].Link, "only the person's own conversation")
	require.Equal(t, uint64(7), an[0].TenantID)
	var p types.UserNotificationLegalRequestPayload
	require.NoError(t, json.Unmarshal(an[0].Payload, &p))
	require.Equal(t, types.UserNotificationLegalRequestPayload{RequestID: row.ID, DocumentNumber: "53/2022/NĐ-CP",
		Title: "Quy định chi tiết một số điều của Luật An ninh mạng", SessionID: "s-an", Channel: "web"}, p)
	require.Len(t, f.notifications(t, "binh"), 1)

	n, err = f.notifier.Run(ctx)
	require.NoError(t, err)
	require.Zero(t, n)
	// the decision endpoint called again (decided_at moves): nothing new
	time.Sleep(5 * time.Millisecond)
	_, err = f.requests.Decide(ctx, row.ID, types.LegalRequestAdded, "sa", "", "", "")
	require.NoError(t, err)
	n, err = f.notifier.Run(ctx)
	require.NoError(t, err)
	require.Zero(t, n)
	require.Len(t, f.notifications(t, ""), 2)
}

// A rejected request notifies nobody; a document still parsing waits until
// parsing finished; closings older than the window are no news.
func TestLegalRequestNoticesEligibility(t *testing.T) {
	f := newNoticeFixture(t)
	ctx := context.Background()
	rejected := f.ask(t, "01/2024/TT-BCA", "3003", "web", map[string]string{"an": "s1"})
	parsing := f.ask(t, "53/2022/NĐ-CP", "155366", "web", map[string]string{"binh": "s2"})
	stale := f.ask(t, "12/2020/NĐ-CP", "999", "web", map[string]string{"chi": "s3"})
	require.NoError(t, f.db.Exec(`INSERT INTO knowledges (id, parse_status) VALUES ('k1', 'processing')`).Error)
	_, err := f.requests.Decide(ctx, rejected.ID, types.LegalRequestRejected, "sa", "", "", "")
	require.NoError(t, err)
	_, err = f.requests.Decide(ctx, parsing.ID, types.LegalRequestAdded, "sa", "", "k1", "kb")
	require.NoError(t, err)
	_, err = f.requests.Decide(ctx, stale.ID, types.LegalRequestAdded, "sa", "", "", "")
	require.NoError(t, err)
	require.NoError(t, f.db.Model(&types.LegalDocumentRequest{}).Where("id = ?", stale.ID).
		Update("decided_at", time.Now().Add(-types.LegalRequestNoticeWindow-time.Hour)).Error)

	n, err := f.notifier.Run(ctx)
	require.NoError(t, err)
	require.Zero(t, n)

	require.NoError(t, f.db.Exec(`UPDATE knowledges SET parse_status = 'completed' WHERE id = 'k1'`).Error)
	n, err = f.notifier.Run(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Len(t, f.notifications(t, "binh"), 1)
	require.Empty(t, f.notifications(t, "an"))
	require.Empty(t, f.notifications(t, "chi"))
}

// A person told once who asks again later (the request reopens) is told
// again when it is added again.
func TestLegalRequestNoticesAgainAfterAskingAgain(t *testing.T) {
	f := newNoticeFixture(t)
	ctx := context.Background()
	row := f.ask(t, "53/2022/NĐ-CP", "155366", "web", map[string]string{"an": "s1"})
	_, err := f.requests.Decide(ctx, row.ID, types.LegalRequestAdded, "sa", "", "", "")
	require.NoError(t, err)
	n, _ := f.notifier.Run(ctx)
	require.Equal(t, 1, n)

	time.Sleep(5 * time.Millisecond)
	again := f.ask(t, "53/2022/NĐ-CP", "155366", "web", map[string]string{"an": "s9"})
	require.Equal(t, types.LegalRequestRequested, again.Status, "everyone was told: asking again reopens")
	_, err = f.requests.Decide(ctx, row.ID, types.LegalRequestAdded, "sa", "", "", "")
	require.NoError(t, err)
	n, err = f.notifier.Run(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	rows := f.notifications(t, "an")
	require.Len(t, rows, 2)
	require.Equal(t, "/platform/chat/s9", rows[1].Link)
}

func TestLegalRequestAddedNotificationText(t *testing.T) {
	base := types.LegalRequestAddedNotice{RequesterID: "q1", RequestID: "r1", UserID: "u", TenantID: 7,
		SessionID: "s1", SessionLive: true, Channel: types.LegalRequesterChannelWordAddin, WordFileID: "file-1",
		DocumentNumber: "53/2022/NĐ-CP", Title: "Quy định chi tiết", AskedAt: time.Unix(1700000000, 123000)}
	word := legalRequestAddedNotification(&base)
	require.Equal(t, "/platform/chat/s1", word.Link)
	require.Contains(t, word.Body, "tiện ích Word")
	require.Contains(t, string(word.Payload), `"word_file_id":"file-1"`)
	require.Equal(t, "legal_request_added:q1:1700000000000123", word.DedupeKey)

	gone := base
	gone.SessionLive, gone.Channel, gone.Title = false, "web", ""
	n := legalRequestAddedNotification(&gone)
	require.Equal(t, "/platform/legal-lookup?q=53%2F2022%2FN%C4%90-CP", n.Link, "a deleted conversation links to the lookup")
	require.Equal(t, "Bạn có thể tra cứu và hỏi về văn bản này ngay bây giờ.", n.Body)
}

// The decorated queue notifies right after a decision closing a request as
// added, not after a rejection.
func TestLegalRequestDecisionTriggersNotices(t *testing.T) {
	f := newNoticeFixture(t)
	ctx := types.WithCaller(context.Background(), types.Caller{UserID: "sa"})
	added := f.ask(t, "53/2022/NĐ-CP", "155366", "web", map[string]string{"an": "s1"})
	rejected := f.ask(t, "01/2024/TT-BCA", "3003", "web", map[string]string{"binh": "s2"})
	svc := NewLegalRequestAddedNotifications(NewLegalRequestService(f.requests, queueCatalog{}, nil, nil, nil), f.notifier)

	_, err := svc.Decide(ctx, rejected.ID, &types.LegalRequestDecision{Action: types.LegalRequestActionReject})
	require.NoError(t, err)
	f.notifier.Wait()
	require.Empty(t, f.notifications(t, ""))
	item, err := svc.Decide(ctx, added.ID, &types.LegalRequestDecision{Action: types.LegalRequestActionAdded})
	require.NoError(t, err)
	require.Equal(t, types.LegalRequestAdded, item.Status)
	require.Equal(t, 1, item.RequesterCount)
	f.notifier.Wait()
	require.Len(t, f.notifications(t, "an"), 1)
}

// Every ingest that closes requests (push, draft, MCP, auto-ingest) goes
// through markRequestsAdded, which triggers the sweep.
func TestLegalFeedMarkRequestsAddedTriggers(t *testing.T) {
	reqs := &feedRequests{}
	feed := NewLegalFeedService(nil, nil, reqs)
	calls := 0
	feed.RequestsAdded = func(context.Context) { calls++ }
	feed.markRequestsAdded(context.Background(), &LegalIngestResult{VBPLID: "155366", KnowledgeID: "k", KnowledgeBaseID: "kb"},
		LegalFeedActor{UserID: "sa"})
	require.Equal(t, 1, calls)
	require.Len(t, reqs.marked, 1)
	(*LegalFeedService)(nil).NotifyRequestsAdded(context.Background()) // nil-safe
}

// failingDelivery fails the delivery of one requester.
type failingDelivery struct {
	interfaces.LegalRequestRepository
	failUser string
}

func (f *failingDelivery) DeliverAddedNotice(ctx context.Context, requesterID string, n *types.UserNotification) (bool, error) {
	if n.UserID == f.failUser {
		return false, errors.New("disk full")
	}
	return f.LegalRequestRepository.DeliverAddedNotice(ctx, requesterID, n)
}

// A requester that fails does not keep the others from being notified; the
// error is reported and the next sweep retries it.
func TestLegalRequestNoticesFailingRequesterDoesNotBlock(t *testing.T) {
	f := newNoticeFixture(t)
	ctx := context.Background()
	row := f.ask(t, "53/2022/NĐ-CP", "155366", "web", map[string]string{"an": "s1", "binh": "s2", "chi": "s3"})
	_, err := f.requests.Decide(ctx, row.ID, types.LegalRequestAdded, "sa", "", "", "")
	require.NoError(t, err)
	notifier := NewLegalRequestNotifier(&failingDelivery{LegalRequestRepository: f.requests, failUser: "an"}, nil)
	n, err := notifier.Run(ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "disk full")
	require.Equal(t, 2, n)
	require.Empty(t, f.notifications(t, "an"))
	n, err = f.notifier.Run(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n, "retried by the next sweep")
}

// Stop ends the loop and waits; a Trigger afterwards does nothing.
func TestLegalRequestNotifierStop(t *testing.T) {
	f := newNoticeFixture(t)
	ctx := context.Background()
	row := f.ask(t, "53/2022/NĐ-CP", "155366", "web", map[string]string{"an": "s1"})
	_, err := f.requests.Decide(ctx, row.ID, types.LegalRequestAdded, "sa", "", "", "")
	require.NoError(t, err)
	stop := f.notifier.Start(time.Hour)
	stop()
	stop() // idempotent
	f.notifier.Trigger(ctx)
	f.notifier.Wait()
	require.Empty(t, f.notifications(t, ""), "no sweep after Stop")
}

// The sweep purges read notifications past the retention once a day.
func TestLegalRequestNotifierPurgesDaily(t *testing.T) {
	f := newNoticeFixture(t)
	ctx := context.Background()
	old := time.Now().Add(-types.UserNotificationRetention - time.Hour)
	require.NoError(t, f.db.Create(&types.UserNotification{ID: "x", UserID: "an", Kind: "k", DedupeKey: "x", ReadAt: &old}).Error)
	f.notifier.purgeDaily(ctx)
	require.Empty(t, f.notifications(t, "an"))
	require.NoError(t, f.db.Create(&types.UserNotification{ID: "y", UserID: "an", Kind: "k", DedupeKey: "y", ReadAt: &old}).Error)
	f.notifier.purgeDaily(ctx)
	require.Len(t, f.notifications(t, "an"), 1, "once a day")
}
