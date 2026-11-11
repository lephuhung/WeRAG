package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func addMessageForTest(t *testing.T, db *gorm.DB, sessionID, role, content string, at time.Time) *types.Message {
	t.Helper()
	m := &types.Message{
		ID: uuid.NewString(), SessionID: sessionID, RequestID: uuid.NewString(),
		Role: role, Content: content, IsCompleted: true, CreatedAt: at, UpdatedAt: at,
	}
	require.NoError(t, db.Create(m).Error)
	return m
}

func hitIDs(hits []*types.SessionContentHit) []string {
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.SessionID)
	}
	return out
}

// Content search shows exactly the sessions the list would: the caller's
// own (and legacy tenant rows), not deleted, not temporary, no Word add-in
// session on the web (and only those for "word"), sub-chats judged by their
// root; one hit per session, newest match first, paged.
func TestSessionRepositorySearchContentScope(t *testing.T) {
	repo, db := newSessionRepositoryForTest(t)
	require.NoError(t, db.AutoMigrate(&testIMChannelSession{}, &types.Message{}))
	ctx := context.Background()
	base := time.Date(2026, 11, 1, 9, 0, 0, 0, time.Local)

	own := createSessionForTest(t, db, 1, "alice")
	older := createSessionForTest(t, db, 1, "alice")
	bobs := createSessionForTest(t, db, 1, "bob")
	otherTenant := createSessionForTest(t, db, 2, "alice")
	word := createSessionForTest(t, db, 1, "alice")
	temp := createSessionForTest(t, db, 1, "alice")
	deleted := createSessionForTest(t, db, 1, "alice")
	wordSub := &types.Session{TenantID: 1, UserID: "alice", Title: "word (tiếp)", ChatRootID: word.ID}
	require.NoError(t, db.Create(wordSub).Error)
	webSub := &types.Session{TenantID: 1, UserID: "alice", Title: "own (tiếp)", ChatRootID: own.ID}
	require.NoError(t, db.Create(webSub).Error)
	// is_temporary is create-only on the model
	require.NoError(t, db.Exec("UPDATE sessions SET is_temporary = ? WHERE id = ?", true, temp.ID).Error)
	require.NoError(t, db.Create(&types.DocumentWorkspace{
		TenantID: 1, SessionID: word.ID, FileName: "a.docx", FileType: "docx",
		Status: types.DocumentWorkspaceStatusOpen, EditorKind: types.DocumentEditorKindWordAddin,
	}).Error)

	const text = "Mức phạt khi vi phạm Nghị định 100 là bao nhiêu?"
	addMessageForTest(t, db, older.ID, "user", text, base)
	addMessageForTest(t, db, own.ID, "user", text, base.Add(time.Hour))
	newest := addMessageForTest(t, db, own.ID, "assistant", "Theo NGHị định 100, mức phạt …", base.Add(2*time.Hour))
	addMessageForTest(t, db, own.ID, "user", "không liên quan", base.Add(3*time.Hour))
	addMessageForTest(t, db, own.ID, "system", "nghị định 100 (system prompt)", base.Add(4*time.Hour))
	addMessageForTest(t, db, webSub.ID, "user", "nghị định 100 ở chat nhỏ", base.Add(30*time.Minute))
	for _, s := range []*types.Session{bobs, otherTenant, word, temp, deleted, wordSub} {
		addMessageForTest(t, db, s.ID, "user", text, base.Add(5*time.Hour))
	}
	gone := addMessageForTest(t, db, older.ID, "assistant", "nghị định 100 (deleted message)", base.Add(6*time.Hour))
	require.NoError(t, db.Delete(gone).Error)
	require.NoError(t, db.Delete(&types.Session{}, "id = ?", deleted.ID).Error)

	search := func(q, source string, offset, limit int) ([]*types.SessionContentHit, bool) {
		hits, more, err := repo.SearchContent(ctx, &types.SessionContentSearchQuery{
			TenantID: 1, UserID: "alice", Query: q, Source: source, Offset: offset, Limit: limit,
		})
		require.NoError(t, err)
		return hits, more
	}

	hits, more := search("nghị định 100", "", 0, 10)
	require.False(t, more)
	require.Equal(t, []string{own.ID, webSub.ID, older.ID}, hitIDs(hits), "newest match first, own sessions only")
	require.Equal(t, newest.ID, hits[0].MessageID, "the session's newest matching message")
	require.Equal(t, "assistant", hits[0].Role)
	require.Equal(t, 2, hits[0].MatchCount, "system rows are not searched")
	require.Equal(t, own.ID, hits[1].ChatRootID)
	require.Equal(t, text, hits[2].Content, "the deleted message is not the hit")

	hits, _ = search("nghị định 100", "word", 0, 10)
	require.ElementsMatch(t, []string{word.ID, wordSub.ID}, hitIDs(hits), "word: the add-in's sessions only")

	// pagination
	hits, more = search("nghị định 100", "", 0, 2)
	require.True(t, more)
	require.Equal(t, []string{own.ID, webSub.ID}, hitIDs(hits))
	hits, more = search("nghị định 100", "", 2, 2)
	require.False(t, more)
	require.Equal(t, []string{older.ID}, hitIDs(hits))

	// LIKE wildcards are literal; accents matter (ILIKE, like search_conversations).
	// SQLite's LOWER folds ASCII only (Postgres ILIKE folds "Ị" too), so the
	// newest message above differs from the query in ASCII case only.
	hits, _ = search("100%", "", 0, 10)
	require.Empty(t, hits)
	hits, _ = search("nghi dinh", "", 0, 10)
	require.Empty(t, hits)

	// legacy tenant-level rows stay visible, as in the list
	legacy := createSessionForTest(t, db, 1, "")
	addMessageForTest(t, db, legacy.ID, "user", "nghị định 100 cũ", base.Add(7*time.Hour))
	hits, _ = search("nghị định 100", "", 0, 1)
	require.Equal(t, []string{legacy.ID}, hitIDs(hits))
}
