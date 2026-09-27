package repository

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// migrateAbbreviationTables runs the REAL abbreviation turn migration (never
// bare AutoMigrate) for fixtures that only build the message schema —
// hydrateAbbreviationState reads these tables on every message read path, so
// a fixture without them fails closed like production would without the
// migration applied.
func migrateAbbreviationTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	root := abbreviationTurnRepoRoot(t)
	for _, stmt := range splitMigrationStatements(t, filepath.Join(
		root, "migrations", "sqlite", "000031_abbreviation_turn_states.up.sql")) {
		require.NoError(t, db.Exec(stmt).Error, "migration statement: %s", stmt)
	}
}

// newMessageAbbreviationRepo builds the real messages/sessions schema plus the
// REAL abbreviation turn migration so hydration tests exercise production SQL.
func newMessageAbbreviationRepo(t *testing.T) (*messageRepository, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(
		"file:"+uuid.NewString()+"?mode=memory&cache=shared&_fk=1"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.Session{}, &types.Message{}, &types.MessageArtifactRecord{}))
	migrateAbbreviationTables(t, db)
	return &messageRepository{db: db}, db
}

func rawJSON(t *testing.T, v interface{}) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}

func seedAbbreviationTurn(
	t *testing.T, db *gorm.DB, state *types.AbbreviationTurnState, links []types.AbbreviationMessageLink,
) {
	t.Helper()
	require.NoError(t, db.Create(state).Error)
	for i := range links {
		require.NoError(t, db.Create(&links[i]).Error)
	}
}

func abbreviationTestPayload() types.AbbreviationTurnPayload {
	return types.AbbreviationTurnPayload{
		Resolution: types.AbbreviationResolution{
			RequestID:         "req-1",
			RootUserMessageID: "u1",
			OriginalQuery:     "ATTT là gì",
			Status:            types.AbbreviationStatusNeedsDefinition,
			UnknownTerms:      []string{"ATTT"},
			Terms: []types.AbbreviationTerm{
				{ShortForm: "ATTT", Key: "attt",
					Definition: &types.AbbreviationDefinition{ShortForm: "ATTT", FullForm: "secret-full"}},
			},
			Version:   2,
			ExpiresAt: time.Now().Add(24 * time.Hour),
		},
	}
}

func TestMessageHydrationAttachesPublicState(t *testing.T) {
	repo, db := newMessageAbbreviationRepo(t)
	ctx := context.Background()
	at := time.Now()

	require.NoError(t, db.Session(&gorm.Session{SkipHooks: true}).
		Create(&types.Session{ID: "s", TenantID: 7, UserID: "alice"}).Error)
	seedMessage(t, db, "u1", "s", "user", at)
	seedMessage(t, db, "a1", "s", "assistant", at.Add(time.Second))
	seedMessage(t, db, "u2", "s", "user", at.Add(2*time.Second))

	seedAbbreviationTurn(t, db, &types.AbbreviationTurnState{
		ID: "req-1", TenantID: 7, SessionID: "s", OwnerID: "alice",
		PrincipalID: "web_user:alice", RootUserMessageID: "u1",
		ClarificationMessageID: "a1",
		State:                  types.AbbreviationTurnAwaitingDefinition,
		Version:                3, ExpiresAt: time.Now().Add(24 * time.Hour),
		Payload: abbreviationTestPayload(),
	}, []types.AbbreviationMessageLink{
		{MessageID: "u1", RequestID: "req-1", Role: types.AbbreviationMessageRoleRoot},
		{MessageID: "a1", RequestID: "req-1", Role: types.AbbreviationMessageRoleClarification},
	})

	msg, err := repo.GetMessage(ctx, "s", "a1")
	require.NoError(t, err)
	require.NotNil(t, msg.Abbreviation)
	require.Equal(t, types.AbbreviationStatusNeedsDefinition, msg.Abbreviation.Status)
	require.Equal(t, "req-1", msg.Abbreviation.RequestID)
	require.Equal(t, "u1", msg.Abbreviation.RootUserMessageID)
	require.Equal(t, uint64(3), msg.Abbreviation.Version)
	require.Equal(t, []string{"ATTT"}, msg.Abbreviation.UnknownTerms)
	// Public projection must not leak private evidence.
	require.NotContains(t, rawJSON(t, msg.Abbreviation), "secret-full")
	require.NotContains(t, rawJSON(t, msg.Abbreviation), "alice")
	require.NotContains(t, rawJSON(t, msg.Abbreviation), "web_user")

	unrelated, err := repo.GetMessage(ctx, "s", "u2")
	require.NoError(t, err)
	require.Nil(t, unrelated.Abbreviation)
}

func TestMessageHydrationMapsTerminalStates(t *testing.T) {
	repo, db := newMessageAbbreviationRepo(t)
	ctx := context.Background()
	at := time.Now()

	require.NoError(t, db.Session(&gorm.Session{SkipHooks: true}).
		Create(&types.Session{ID: "s", TenantID: 7, UserID: "alice"}).Error)
	seedMessage(t, db, "u1", "s", "user", at)
	seedMessage(t, db, "a1", "s", "assistant", at.Add(time.Second))

	cases := []struct {
		turnState string
		expires   time.Time
		want      string
	}{
		{types.AbbreviationTurnAwaitingDefinition, time.Now().Add(time.Hour), types.AbbreviationStatusNeedsDefinition},
		{types.AbbreviationTurnAwaitingDefinition, time.Now().Add(-time.Hour), types.AbbreviationTurnExpired},
		{types.AbbreviationTurnCompleted, time.Now().Add(time.Hour), types.AbbreviationTurnCompleted},
		{types.AbbreviationTurnCancelled, time.Now().Add(time.Hour), types.AbbreviationTurnCancelled},
		{types.AbbreviationTurnBlockedError, time.Now().Add(time.Hour), types.AbbreviationTurnBlockedError},
		{types.AbbreviationTurnRunning, time.Now().Add(time.Hour), types.AbbreviationTurnRunning},
	}
	for i, tc := range cases {
		reqID := "req-t"
		msgID := "a1"
		db.Exec("DELETE FROM abbreviation_turn_states").Exec("DELETE FROM abbreviation_turn_messages")
		seedAbbreviationTurn(t, db, &types.AbbreviationTurnState{
			ID: reqID, TenantID: 7, SessionID: "s", OwnerID: "alice",
			PrincipalID: "web_user:alice", RootUserMessageID: "u1",
			ClarificationMessageID: msgID,
			State:                  tc.turnState, Version: uint64(i + 1),
			ExpiresAt: tc.expires, Payload: abbreviationTestPayload(),
		}, []types.AbbreviationMessageLink{
			{MessageID: msgID, RequestID: reqID, Role: types.AbbreviationMessageRoleClarification},
		})
		msg, err := repo.GetMessage(ctx, "s", msgID)
		require.NoError(t, err)
		require.NotNil(t, msg.Abbreviation, "state=%s", tc.turnState)
		require.Equal(t, tc.want, msg.Abbreviation.Status, "state=%s", tc.turnState)
	}
}

func TestMessageHydrationRejectsCrossSessionLink(t *testing.T) {
	repo, db := newMessageAbbreviationRepo(t)
	ctx := context.Background()
	at := time.Now()

	require.NoError(t, db.Session(&gorm.Session{SkipHooks: true}).
		Create(&types.Session{ID: "s", TenantID: 7, UserID: "alice"}).Error)
	require.NoError(t, db.Session(&gorm.Session{SkipHooks: true}).
		Create(&types.Session{ID: "s-other", TenantID: 7, UserID: "bob"}).Error)
	seedMessage(t, db, "u1", "s", "user", at)
	seedMessage(t, db, "u-other", "s-other", "user", at)

	seedAbbreviationTurn(t, db, &types.AbbreviationTurnState{
		ID: "req-1", TenantID: 7, SessionID: "s", OwnerID: "alice",
		PrincipalID: "web_user:alice", RootUserMessageID: "u1",
		State:   types.AbbreviationTurnAwaitingDefinition,
		Version: 1, ExpiresAt: time.Now().Add(time.Hour),
		Payload: abbreviationTestPayload(),
	}, []types.AbbreviationMessageLink{
		// A forged link onto a foreign-session message must not project.
		{MessageID: "u-other", RequestID: "req-1", Role: types.AbbreviationMessageRoleDefinition},
	})

	msg, err := repo.GetMessage(ctx, "s-other", "u-other")
	require.NoError(t, err)
	require.Nil(t, msg.Abbreviation)
}

func TestMessageHydrationBatchReadPaths(t *testing.T) {
	repo, db := newMessageAbbreviationRepo(t)
	ctx := context.Background()
	at := time.Now()

	require.NoError(t, db.Session(&gorm.Session{SkipHooks: true}).
		Create(&types.Session{ID: "s", TenantID: 7, UserID: "alice"}).Error)
	seedMessage(t, db, "u1", "s", "user", at)
	seedMessage(t, db, "a1", "s", "assistant", at.Add(time.Second))
	seedAbbreviationTurn(t, db, &types.AbbreviationTurnState{
		ID: "req-1", TenantID: 7, SessionID: "s", OwnerID: "alice",
		PrincipalID: "web_user:alice", RootUserMessageID: "u1",
		State:   types.AbbreviationTurnAwaitingDefinition,
		Version: 2, ExpiresAt: time.Now().Add(time.Hour),
		Payload: abbreviationTestPayload(),
	}, []types.AbbreviationMessageLink{
		{MessageID: "u1", RequestID: "req-1", Role: types.AbbreviationMessageRoleRoot},
		{MessageID: "a1", RequestID: "req-1", Role: types.AbbreviationMessageRoleClarification},
	})

	assertHydrated := func(t *testing.T, msgs []*types.Message) {
		t.Helper()
		byID := map[string]*types.Message{}
		for _, m := range msgs {
			byID[m.ID] = m
		}
		require.NotNil(t, byID["u1"].Abbreviation)
		require.NotNil(t, byID["a1"].Abbreviation)
		require.Equal(t, types.AbbreviationStatusNeedsDefinition, byID["u1"].Abbreviation.Status)
	}

	list, err := repo.GetMessagesBySession(ctx, "s", 1, 10)
	require.NoError(t, err)
	assertHydrated(t, list)

	recent, err := repo.GetRecentMessagesBySession(ctx, "s", 10)
	require.NoError(t, err)
	assertHydrated(t, recent)

	before, err := repo.GetMessagesBySessionBeforeTime(ctx, "s", at.Add(time.Hour), 10)
	require.NoError(t, err)
	assertHydrated(t, before)

	after, err := repo.ListMessagesBySessionAfterTime(ctx, "s", at.Add(-time.Hour), 10)
	require.NoError(t, err)
	assertHydrated(t, after)

	first, err := repo.GetFirstMessageOfUser(ctx, "s")
	require.NoError(t, err)
	require.NotNil(t, first.Abbreviation)

	byRequest, err := repo.GetMessageByRequestID(ctx, "s", "req")
	require.NoError(t, err)
	_ = byRequest // request IDs in fixture are empty; path itself covered elsewhere
}
