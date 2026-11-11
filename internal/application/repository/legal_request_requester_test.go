package repository

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
)

func newRequesterRepo(t *testing.T) *legalRequestRepository {
	t.Helper()
	repo, db := newLegalRequestRepo(t)
	require.NoError(t, db.AutoMigrate(&types.UserNotification{}))
	require.NoError(t, db.Exec(`ALTER TABLE knowledges ADD COLUMN parse_status TEXT`).Error)
	require.NoError(t, db.Exec(`ALTER TABLE knowledges ADD COLUMN updated_at DATETIME`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE sessions (id TEXT, deleted_at DATETIME)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE document_workspaces (id TEXT, session_id TEXT, word_file_id TEXT,
		updated_at DATETIME, deleted_at DATETIME)`).Error)
	return repo
}

func strPtr(s string) *string { return &s }

func notice(n *types.LegalRequestAddedNotice) *types.UserNotification {
	return &types.UserNotification{UserID: n.UserID, Kind: types.UserNotificationLegalRequestAdded,
		Title: "t", DedupeKey: "k:" + n.RequesterID + ":" + n.AskedAt.Format(time.RFC3339Nano)}
}

// The same person asking twice is one requester, moved to the latest
// conversation; asking again after being told makes them pending again.
func TestLegalRequestRequesterDedupe(t *testing.T) {
	repo := newRequesterRepo(t)
	ctx := context.Background()
	row, err := repo.Record(ctx, &types.LegalDocumentRequest{NumberKey: "53/2022/ND-CP", DocumentNumber: "53/2022/NĐ-CP",
		LegalDocumentID: "155366", LastSessionID: "s1", LastUserID: "u1"})
	require.NoError(t, err)

	add := func(user, session, msg string) {
		t.Helper()
		r := &types.LegalRequestRequester{RequestID: row.ID, TenantID: 10003, UserID: user, SessionID: session, Channel: "web"}
		if msg != "" {
			r.MessageID = strPtr(msg)
		}
		require.NoError(t, repo.AddRequester(ctx, r))
	}
	add("u1", "s1", "m1")
	add("u1", "s2", "")
	add("u2", "s3", "m3")

	var rows []types.LegalRequestRequester
	require.NoError(t, repo.db.Order("user_id").Find(&rows).Error)
	require.Len(t, rows, 2)
	require.Equal(t, "s2", rows[0].SessionID, "the latest conversation is kept")
	require.Nil(t, rows[0].MessageID)
	require.Equal(t, "m3", *rows[1].MessageID)

	counts, err := repo.CountRequesters(ctx, []string{row.ID, "other"})
	require.NoError(t, err)
	require.Equal(t, map[string]int{row.ID: 2}, counts)

	// told, then asks again: pending again, with a new ask time
	require.NoError(t, repo.db.Model(&types.LegalRequestRequester{}).Where("id = ?", rows[0].ID).
		Updates(map[string]any{"notified_at": time.Now(), "notify_status": types.LegalRequesterNotifyInApp}).Error)
	time.Sleep(5 * time.Millisecond)
	add("u1", "s4", "")
	var again types.LegalRequestRequester
	require.NoError(t, repo.db.Where("id = ?", rows[0].ID).Take(&again).Error)
	require.Nil(t, again.NotifiedAt)
	require.Empty(t, again.NotifyStatus)
	require.Equal(t, "s4", again.SessionID)
	require.True(t, again.UpdatedAt.After(rows[0].UpdatedAt), "asking again moves the ask time")

	require.Error(t, repo.AddRequester(ctx, &types.LegalRequestRequester{RequestID: row.ID}), "a user is required")
}

// Only the requesters of requests closed as added in the window, not told
// yet, whose document is usable, are due a notice.
func TestLegalRequestPendingAddedNotices(t *testing.T) {
	repo := newRequesterRepo(t)
	db := repo.db
	ctx := context.Background()
	record := func(key, docID string) *types.LegalDocumentRequest {
		t.Helper()
		row, err := repo.Record(ctx, &types.LegalDocumentRequest{NumberKey: key, DocumentNumber: key, LegalDocumentID: docID,
			Title: "Văn bản " + key, LastSessionID: "s-" + key})
		require.NoError(t, err)
		require.NoError(t, repo.AddRequester(ctx, &types.LegalRequestRequester{RequestID: row.ID, UserID: "u-" + key,
			SessionID: "s-" + key, TenantID: 7, Channel: "web"}))
		return row
	}
	require.NoError(t, db.Exec(`INSERT INTO sessions (id) VALUES ('s-A')`).Error)
	require.NoError(t, db.Exec(`INSERT INTO sessions (id, deleted_at) VALUES ('s-E', CURRENT_TIMESTAMP)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO document_workspaces (id, session_id, word_file_id, updated_at)
		VALUES ('w1', 's-A', 'file-1', CURRENT_TIMESTAMP)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO knowledges (id, parse_status) VALUES ('k-done', 'completed'), ('k-parsing', 'processing')`).Error)

	byHand := record("A", "1")   // added by hand, no knowledge: eligible at once
	rejected := record("B", "2") // rejected: never
	open := record("C", "3")     // still open, closed later by an ingest
	stale := record("D", "4")    // closed long ago: out of the window
	parsing := record("E", "5")  // its knowledge is still parsing
	_, err := repo.Decide(ctx, byHand.ID, types.LegalRequestAdded, "sa", "", "", "")
	require.NoError(t, err)
	_, err = repo.Decide(ctx, rejected.ID, types.LegalRequestRejected, "sa", "", "", "")
	require.NoError(t, err)
	_, err = repo.Decide(ctx, stale.ID, types.LegalRequestAdded, "sa", "", "", "")
	require.NoError(t, err)
	require.NoError(t, db.Model(&types.LegalDocumentRequest{}).Where("id = ?", stale.ID).
		Update("decided_at", time.Now().Add(-30*24*time.Hour)).Error)
	_, err = repo.Decide(ctx, parsing.ID, types.LegalRequestAdded, "sa", "", "k-parsing", "kb1")
	require.NoError(t, err)

	since := time.Now().Add(-types.LegalRequestNoticeWindow)
	notices, err := repo.PendingAddedNotices(ctx, since, 10)
	require.NoError(t, err)
	require.Len(t, notices, 1)
	n := notices[0]
	require.Equal(t, byHand.ID, n.RequestID)
	require.Equal(t, "u-A", n.UserID)
	require.Equal(t, "s-A", n.SessionID)
	require.True(t, n.SessionLive)
	require.Equal(t, "file-1", n.WordFileID)
	require.Equal(t, uint64(7), n.TenantID)
	require.Equal(t, "A", n.DocumentNumber)
	require.Equal(t, "Văn bản A", n.Title)
	require.False(t, n.DecidedAt.IsZero())
	require.False(t, n.AskedAt.IsZero())

	// delivered once, in one transaction with the requester mark
	ok, err := repo.DeliverAddedNotice(ctx, n.RequesterID, notice(n))
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = repo.DeliverAddedNotice(ctx, n.RequesterID, notice(n))
	require.NoError(t, err)
	require.False(t, ok, "a requester is notified once")
	var count int64
	require.NoError(t, db.Model(&types.UserNotification{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	notices, err = repo.PendingAddedNotices(ctx, since, 10)
	require.NoError(t, err)
	require.Empty(t, notices, "a notified requester is not selected again")

	// parsing finished: picked up
	require.NoError(t, db.Exec(`UPDATE knowledges SET parse_status = 'completed' WHERE id = 'k-parsing'`).Error)
	notices, err = repo.PendingAddedNotices(ctx, since, 10)
	require.NoError(t, err)
	require.Len(t, notices, 1)
	require.Equal(t, parsing.ID, notices[0].RequestID)
	require.False(t, notices[0].SessionLive, "a deleted conversation is not live")
	ok, err = repo.DeliverAddedNotice(ctx, notices[0].RequesterID, notice(notices[0]))
	require.NoError(t, err)
	require.True(t, ok)

	// an ingest closing every open request of the document counts too
	n2, err := repo.MarkAddedByDocument(ctx, "3", "k-done", "kb1", "feed")
	require.NoError(t, err)
	require.EqualValues(t, 1, n2)
	notices, err = repo.PendingAddedNotices(ctx, since, 10)
	require.NoError(t, err)
	require.Len(t, notices, 1)
	require.Equal(t, open.ID, notices[0].RequestID)
	require.False(t, notices[0].SessionLive)
	require.Empty(t, notices[0].WordFileID)
}

// A failed notification insert leaves the requester pending (one
// transaction).
func TestLegalRequestDeliverAddedNoticeIsAtomic(t *testing.T) {
	repo := newRequesterRepo(t)
	ctx := context.Background()
	row, err := repo.Record(ctx, &types.LegalDocumentRequest{NumberKey: "A", DocumentNumber: "A", LastSessionID: "s"})
	require.NoError(t, err)
	require.NoError(t, repo.AddRequester(ctx, &types.LegalRequestRequester{RequestID: row.ID, UserID: "u", SessionID: "s"}))
	var q types.LegalRequestRequester
	require.NoError(t, repo.db.Take(&q).Error)

	_, err = repo.DeliverAddedNotice(ctx, q.ID, &types.UserNotification{UserID: "u"}) // no kind, no key: refused
	require.Error(t, err)
	require.NoError(t, repo.db.Take(&q).Error)
	require.Nil(t, q.NotifiedAt, "the requester mark is rolled back with the failed insert")
}

// An added request whose earlier askers still wait for the notice, and
// whose copy is usable or still coming, is not reopened by a new ask: the
// asker joins them. Anything else reopens it as before.
func TestLegalRequestRecordKeepsAddedWhileNoticesPending(t *testing.T) {
	ctx := context.Background()
	type setup struct {
		knowledgeSQL string // inserts knowledge 'k1' ('' = none linked)
		decidedAgo   time.Duration
		// after the closing: "all" = everyone told, "failed" = the waiting
		// row closed as failed, "late" = the only waiting row asked after it
		after string
	}
	run := func(t *testing.T, c setup) *types.LegalDocumentRequest {
		t.Helper()
		repo := newRequesterRepo(t)
		ask := func(session string) *types.LegalDocumentRequest {
			t.Helper()
			row, err := repo.Record(ctx, &types.LegalDocumentRequest{NumberKey: "53/2022/ND-CP", DocumentNumber: "53/2022/NĐ-CP",
				LegalDocumentID: "155366", LastSessionID: session})
			require.NoError(t, err)
			return row
		}
		row := ask("s1")
		require.NoError(t, repo.AddRequester(ctx, &types.LegalRequestRequester{RequestID: row.ID, UserID: "u1", SessionID: "s1"}))
		knowledgeID := ""
		if c.knowledgeSQL != "" {
			require.NoError(t, repo.db.Exec(c.knowledgeSQL).Error)
			knowledgeID = "k1"
		}
		time.Sleep(2 * time.Millisecond)
		_, err := repo.Decide(ctx, row.ID, types.LegalRequestAdded, "sa", "", knowledgeID, "kb-public")
		require.NoError(t, err)
		if c.decidedAgo > 0 {
			require.NoError(t, repo.db.Model(&types.LegalDocumentRequest{}).Where("id = ?", row.ID).
				Update("decided_at", time.Now().Add(-c.decidedAgo)).Error)
		}
		switch c.after {
		case "all":
			require.NoError(t, repo.db.Model(&types.LegalRequestRequester{}).Where("request_id = ?", row.ID).
				Update("notified_at", time.Now()).Error)
		case "failed":
			for i := 0; i < types.LegalRequesterMaxNotifyAttempts; i++ {
				var q types.LegalRequestRequester
				require.NoError(t, repo.db.Where("request_id = ?", row.ID).Take(&q).Error)
				_, err := repo.RecordNoticeFailure(ctx, q.ID, types.LegalRequesterMaxNotifyAttempts)
				require.NoError(t, err)
			}
		case "late":
			require.NoError(t, repo.db.Model(&types.LegalRequestRequester{}).Where("request_id = ?", row.ID).
				Update("updated_at", time.Now().Add(time.Minute)).Error)
		}
		return ask("s2")
	}
	parsing := `INSERT INTO knowledges (id, parse_status, updated_at) VALUES ('k1', 'processing', CURRENT_TIMESTAMP)`

	held := run(t, setup{knowledgeSQL: parsing})
	require.Equal(t, types.LegalRequestAdded, held.Status, "kept added: u1 still waits, the copy is parsing")
	require.Equal(t, 2, held.RequestCount)
	require.Equal(t, "k1", held.KnowledgeID)
	require.Equal(t, types.LegalRequestAdded, run(t, setup{}).Status, "added by hand, no copy: kept")
	require.Equal(t, types.LegalRequestAdded, run(t, setup{
		knowledgeSQL: `INSERT INTO knowledges (id, parse_status, updated_at) VALUES ('k1', 'completed', datetime('now', '-7 hours'))`,
	}).Status, "parsed: kept, the sweep delivers the waiting notices")

	for name, c := range map[string]setup{
		"failed parse":           {knowledgeSQL: `INSERT INTO knowledges (id, parse_status, updated_at) VALUES ('k1', 'failed', CURRENT_TIMESTAMP)`},
		"cancelled":              {knowledgeSQL: `INSERT INTO knowledges (id, parse_status, updated_at) VALUES ('k1', 'cancelled', CURRENT_TIMESTAMP)`},
		"deleted":                {knowledgeSQL: `INSERT INTO knowledges (id, parse_status, updated_at, deleted_at) VALUES ('k1', 'processing', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`},
		"stale parse":            {knowledgeSQL: `INSERT INTO knowledges (id, parse_status, updated_at) VALUES ('k1', 'processing', datetime('now', '-7 hours'))`},
		"window expired":         {knowledgeSQL: parsing, decidedAgo: types.LegalRequestNoticeWindow + time.Hour},
		"everyone told":          {knowledgeSQL: parsing, after: "all"},
		"delivery gave up":       {knowledgeSQL: parsing, after: "failed"},
		"only later askers wait": {knowledgeSQL: parsing, after: "late"},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, types.LegalRequestRequested, run(t, c).Status, "reopened")
		})
	}
}

// A delivery that keeps failing is closed as failed after the cap.
func TestLegalRequestRecordNoticeFailureCap(t *testing.T) {
	repo := newRequesterRepo(t)
	ctx := context.Background()
	row, err := repo.Record(ctx, &types.LegalDocumentRequest{NumberKey: "A", DocumentNumber: "A", LastSessionID: "s"})
	require.NoError(t, err)
	require.NoError(t, repo.AddRequester(ctx, &types.LegalRequestRequester{RequestID: row.ID, UserID: "u", SessionID: "s"}))
	var q types.LegalRequestRequester
	require.NoError(t, repo.db.Take(&q).Error)
	for i := 1; i <= 3; i++ {
		gaveUp, err := repo.RecordNoticeFailure(ctx, q.ID, 3)
		require.NoError(t, err)
		require.Equal(t, i == 3, gaveUp)
	}
	require.NoError(t, repo.db.Take(&q).Error)
	require.Equal(t, 3, q.NotifyAttempts)
	require.NotNil(t, q.NotifiedAt)
	require.Equal(t, types.LegalRequesterNotifyFailed, q.NotifyStatus)
}

// A new asker recorded out of scope is stored as already closed; an
// existing row keeps its state.
func TestLegalRequestAddRequesterOutOfScope(t *testing.T) {
	repo := newRequesterRepo(t)
	ctx := context.Background()
	row, err := repo.Record(ctx, &types.LegalDocumentRequest{NumberKey: "A", DocumentNumber: "A", LastSessionID: "s"})
	require.NoError(t, err)
	require.NoError(t, repo.AddRequester(ctx, &types.LegalRequestRequester{RequestID: row.ID, UserID: "waiting", SessionID: "s"}))
	_, err = repo.Decide(ctx, row.ID, types.LegalRequestAdded, "sa", "", "", "kb")
	require.NoError(t, err)
	for _, u := range []string{"stranger", "waiting"} {
		require.NoError(t, repo.AddRequester(ctx, &types.LegalRequestRequester{RequestID: row.ID, UserID: u, SessionID: "s2",
			NotifyStatus: types.LegalRequesterNotifyOutOfScope}))
	}
	var stranger, waiting types.LegalRequestRequester
	require.NoError(t, repo.db.Where("user_id = ?", "stranger").Take(&stranger).Error)
	require.NoError(t, repo.db.Where("user_id = ?", "waiting").Take(&waiting).Error)
	require.NotNil(t, stranger.NotifiedAt)
	require.Equal(t, types.LegalRequesterNotifyOutOfScope, stranger.NotifyStatus)
	require.Nil(t, waiting.NotifiedAt, "an earlier asker keeps their pending notice")
	notices, err := repo.PendingAddedNotices(ctx, time.Now().Add(-time.Hour), 10)
	require.NoError(t, err)
	require.Len(t, notices, 1)
	require.Equal(t, "waiting", notices[0].UserID)
}

// A person already told who asks again while the request stays added is
// not made pending again (nothing new to tell); one not told yet keeps the
// ask time their pending notice is keyed to.
func TestLegalRequestAddRequesterKeepsNoticeStateWhileAdded(t *testing.T) {
	repo := newRequesterRepo(t)
	ctx := context.Background()
	row, err := repo.Record(ctx, &types.LegalDocumentRequest{NumberKey: "A", DocumentNumber: "A", LastSessionID: "s1"})
	require.NoError(t, err)
	add := func(user, session string) {
		t.Helper()
		require.NoError(t, repo.AddRequester(ctx, &types.LegalRequestRequester{RequestID: row.ID, UserID: user, SessionID: session}))
	}
	add("told", "s1")
	add("waiting", "s2")
	_, err = repo.Decide(ctx, row.ID, types.LegalRequestAdded, "sa", "", "", "")
	require.NoError(t, err)
	notifiedAt := time.Now().Add(-time.Minute)
	require.NoError(t, repo.db.Model(&types.LegalRequestRequester{}).Where("user_id = ?", "told").
		Updates(map[string]any{"notified_at": notifiedAt, "notify_status": types.LegalRequesterNotifyInApp}).Error)
	var before types.LegalRequestRequester
	require.NoError(t, repo.db.Where("user_id = ?", "waiting").Take(&before).Error)

	time.Sleep(5 * time.Millisecond)
	add("told", "s3")
	add("waiting", "s4")
	var told, waiting types.LegalRequestRequester
	require.NoError(t, repo.db.Where("user_id = ?", "told").Take(&told).Error)
	require.NoError(t, repo.db.Where("user_id = ?", "waiting").Take(&waiting).Error)
	require.NotNil(t, told.NotifiedAt, "already told: stays told")
	require.Equal(t, types.LegalRequesterNotifyInApp, told.NotifyStatus)
	require.Equal(t, "s3", told.SessionID)
	require.Nil(t, waiting.NotifiedAt)
	require.Equal(t, "s4", waiting.SessionID)
	require.True(t, waiting.UpdatedAt.Equal(before.UpdatedAt), "the pending notice keeps its key")
}
