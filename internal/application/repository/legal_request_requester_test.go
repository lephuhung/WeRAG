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
	require.NoError(t, db.AutoMigrate(&types.LegalRequestRequester{}))
	return repo
}

func strPtr(s string) *string { return &s }

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

	// told, then asks again: pending again
	ok, err := repo.MarkRequesterNotified(ctx, rows[0].ID, types.LegalRequesterNotifySent, time.Now())
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = repo.MarkRequesterNotified(ctx, rows[0].ID, types.LegalRequesterNotifySent, time.Now())
	require.NoError(t, err)
	require.False(t, ok, "a requester is marked once")
	add("u1", "s4", "")
	var again types.LegalRequestRequester
	require.NoError(t, repo.db.Where("id = ?", rows[0].ID).Take(&again).Error)
	require.Nil(t, again.NotifiedAt)
	require.Empty(t, again.NotifyStatus)
	require.Equal(t, "s4", again.SessionID)

	require.Error(t, repo.AddRequester(ctx, &types.LegalRequestRequester{RequestID: row.ID}), "a user is required")
}

// Only the requesters of requests closed as added in the window, not told
// yet, are due a notice.
func TestLegalRequestPendingAddedNotices(t *testing.T) {
	repo := newRequesterRepo(t)
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
	added := record("A", "1")
	rejected := record("B", "2")
	open := record("C", "3")
	stale := record("D", "4")
	_, err := repo.Decide(ctx, added.ID, types.LegalRequestAdded, "sa", "", "k1", "kb1")
	require.NoError(t, err)
	_, err = repo.Decide(ctx, rejected.ID, types.LegalRequestRejected, "sa", "", "", "")
	require.NoError(t, err)
	_, err = repo.Decide(ctx, stale.ID, types.LegalRequestAdded, "sa", "", "k4", "kb1")
	require.NoError(t, err)
	require.NoError(t, repo.db.Model(&types.LegalDocumentRequest{}).Where("id = ?", stale.ID).
		Update("decided_at", time.Now().Add(-30*24*time.Hour)).Error)

	since := time.Now().Add(-types.LegalRequestNoticeWindow)
	notices, err := repo.PendingAddedNotices(ctx, since, 10)
	require.NoError(t, err)
	require.Len(t, notices, 1)
	n := notices[0]
	require.Equal(t, added.ID, n.RequestID)
	require.Equal(t, "u-A", n.UserID)
	require.Equal(t, "s-A", n.SessionID)
	require.Equal(t, uint64(7), n.TenantID)
	require.Equal(t, "A", n.DocumentNumber)
	require.Equal(t, "Văn bản A", n.Title)
	require.False(t, n.DecidedAt.IsZero())

	ok, err := repo.MarkRequesterNotified(ctx, n.RequesterID, types.LegalRequesterNotifySent, time.Now())
	require.NoError(t, err)
	require.True(t, ok)
	notices, err = repo.PendingAddedNotices(ctx, since, 10)
	require.NoError(t, err)
	require.Empty(t, notices)

	// an ingest closing every open request of the document counts too
	n2, err := repo.MarkAddedByDocument(ctx, "3", "k3", "kb1", "feed")
	require.NoError(t, err)
	require.EqualValues(t, 1, n2)
	notices, err = repo.PendingAddedNotices(ctx, since, 10)
	require.NoError(t, err)
	require.Len(t, notices, 1)
	require.Equal(t, open.ID, notices[0].RequestID)
}
