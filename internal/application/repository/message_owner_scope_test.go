package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// Sessions are per-user state and the session list has always been scoped that
// way. Conversation search was not, which meant a workspace viewer could type a
// colleague's project name into the search box and read their chats. These
// tests pin the scoping so that cannot come back.

// Session.BeforeCreate assigns a fresh UUID, so the ids are read back rather
// than assumed.
func newOwnerScopeDB(t *testing.T, name string) (*gorm.DB, map[string]string) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+name+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&types.Session{}, &types.Message{}, &types.MessageArtifactRecord{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	migrateAbbreviationTables(t, db)
	sessions := map[string]*types.Session{
		"alice":  {TenantID: 7, UserID: "web_user:alice", Title: "Cuộc trò chuyện của Alice"},
		"bob":    {TenantID: 7, UserID: "web_user:bob", Title: "Cuộc trò chuyện của Bob"},
		"legacy": {TenantID: 7, Title: "Cuộc trò chuyện tạo qua API"},
		"other":  {TenantID: 8, UserID: "web_user:alice", Title: "Không gian làm việc khác"},
	}
	ids := make(map[string]string, len(sessions))
	for label, session := range sessions {
		if err := db.Create(session).Error; err != nil {
			t.Fatalf("insert session: %v", err)
		}
		ids[label] = session.ID
	}
	return db, ids
}

func TestOwnedSessionIDsExcludesOtherPeople(t *testing.T) {
	db, ids := newOwnerScopeDB(t, "owner-scope-owned")
	repo := NewMessageRepository(db)

	owned, err := repo.OwnedSessionIDs(context.Background(), 7, "web_user:alice",
		[]string{ids["alice"], ids["bob"], ids["legacy"], ids["other"]})
	if err != nil {
		t.Fatalf("owned session ids: %v", err)
	}

	if !owned[ids["alice"]] {
		t.Error("alice must be able to search her own conversations")
	}
	if owned[ids["bob"]] {
		t.Error("alice must not be able to reach bob's conversations")
	}
	if owned[ids["legacy"]] {
		t.Error("ownerless sessions (legacy API rows, IM chats) are nobody's to search")
	}
	if owned[ids["other"]] {
		t.Error("a workspace boundary is not something an owner check may cross")
	}
}

func TestOwnedSessionIDsWithoutAnOwnerFindsNothing(t *testing.T) {
	db, ids := newOwnerScopeDB(t, "owner-scope-noowner")
	repo := NewMessageRepository(db)

	// No owner means nothing is owned: the search fails closed instead of
	// widening to the whole workspace.
	owned, err := repo.OwnedSessionIDs(context.Background(), 7, "",
		[]string{ids["alice"], ids["bob"], ids["legacy"], ids["other"]})
	if err != nil {
		t.Fatalf("owned session ids: %v", err)
	}
	if len(owned) != 0 {
		t.Errorf("without an owner nothing qualifies, got %v", owned)
	}
}

// IM sessions are created without an owner; their messages must never be
// searchable by a member, nor by a search with no owner at all.
func TestSearchMessagesByKeywordOwnSessionsOnly(t *testing.T) {
	db, ids := newOwnerScopeDB(t, "owner-scope-keyword")
	repo := NewMessageRepository(db)
	for _, label := range []string{"alice", "bob", "legacy", "other"} {
		if err := db.Create(&types.Message{SessionID: ids[label], Role: "user", Content: "hỏi về nghị định 100"}).Error; err != nil {
			t.Fatalf("insert message: %v", err)
		}
	}
	results, err := repo.SearchMessagesByKeyword(context.Background(), 7, "web_user:alice", "nghị định", nil, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 || results[0].SessionID != ids["alice"] {
		t.Fatalf("alice must find only her own session, got %d results", len(results))
	}
	results, err = repo.SearchMessagesByKeyword(context.Background(), 7, "", "nghị định", nil, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("no owner, no results; got %d", len(results))
	}
}
