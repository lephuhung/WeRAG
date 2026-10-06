package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type turnDictionary struct {
	interfaces.AbbreviationService
	active       []*types.Abbreviation
	listCalls    int
	suggestCalls int
	listErr      error
	suggestErr   error
}

func (*turnDictionary) DetectCandidates(context.Context, string) types.AbbreviationDetection {
	return types.AbbreviationDetection{}
}

func (d *turnDictionary) ListActive(context.Context) ([]*types.Abbreviation, error) {
	d.listCalls++
	return d.active, d.listErr
}

func (d *turnDictionary) Suggest(_ context.Context, req *types.AbbreviationCreateRequest) (*types.Abbreviation, error) {
	d.suggestCalls++
	if d.suggestErr != nil {
		return nil, d.suggestErr
	}
	return &types.Abbreviation{ID: "suggested", ShortForm: req.ShortForm, FullForm: req.FullForm}, nil
}

func newTurnCoordinatorFixture(t *testing.T) (*gorm.DB, interfaces.AbbreviationTurnRepository, *turnDictionary, types.AbbreviationOwner) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared&_foreign_keys=on"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE sessions (id VARCHAR(36) PRIMARY KEY, tenant_id INTEGER NOT NULL, user_id VARCHAR(512), deleted_at DATETIME)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE messages (id VARCHAR(36) PRIMARY KEY, session_id VARCHAR(36) NOT NULL, role VARCHAR(50) NOT NULL, deleted_at DATETIME)`).Error)
	path := filepath.Join("..", "..", "..", "migrations", "sqlite", "000031_abbreviation_turn_states.up.sql")
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var sqlLines []string
	for _, line := range strings.Split(string(raw), "\n") {
		if idx := strings.Index(line, "--"); idx >= 0 {
			line = line[:idx]
		}
		sqlLines = append(sqlLines, line)
	}
	for _, stmt := range strings.Split(strings.Join(sqlLines, "\n"), ";") {
		if strings.TrimSpace(stmt) != "" {
			require.NoError(t, db.Exec(stmt).Error)
		}
	}
	require.NoError(t, db.Exec(`INSERT INTO sessions (id, tenant_id, user_id) VALUES ('s',7,'alice'),('other',7,'bob')`).Error)
	require.NoError(t, db.Exec(`INSERT INTO messages (id, session_id, role) VALUES ('u1','s','user'),('a1','s','assistant'),('u2','s','user'),('a2','s','assistant'),('u3','s','user'),('a3','s','assistant'),('foreign-a','other','assistant')`).Error)
	return db, repository.NewAbbreviationTurnRepository(db), &turnDictionary{}, types.AbbreviationOwner{TenantID: 7, SessionID: "s", OwnerID: "alice", PrincipalID: "web_user:alice"}
}

func turnInput(owner types.AbbreviationOwner, userID, assistantID, query string) AbbreviationPrepareInput {
	return AbbreviationPrepareInput{Binding: types.AbbreviationBinding{Owner: owner, UserMessageID: userID, AssistantMessageID: assistantID, RawQuery: query}}
}

func TestAbbreviationTurnResumesOriginalQuestion(t *testing.T) {
	_, store, dict, owner := newTurnCoordinatorFixture(t)
	c := NewAbbreviationTurnCoordinator(store, dict, nil)
	ctx := context.Background()
	first, err := c.Prepare(ctx, turnInput(owner, "u1", "a1", "ATTT có yêu cầu gì"))
	require.NoError(t, err)
	require.Equal(t, types.AbbreviationStatusNeedsDefinition, first.Status)
	require.Equal(t, []string{"ATTT"}, first.UnknownTerms)
	require.Equal(t, 1, dict.listCalls)
	v := first.Version
	input := turnInput(owner, "u2", "a2", "An toàn thông tin")
	input.ContinuationID, input.ExpectedVersion = first.RequestID, &v
	second, err := c.Prepare(ctx, input)
	require.NoError(t, err)
	require.Equal(t, types.AbbreviationStatusReady, second.Status)
	require.Equal(t, "An toàn thông tin (ATTT) có yêu cầu gì", second.EffectiveQuery)
	require.Equal(t, types.AbbreviationSourceUserCurrent, second.Terms[0].Source)
	require.Equal(t, types.AbbreviationSuggestionPendingReview, second.Terms[0].SuggestionStatus)
	require.Equal(t, 1, dict.suggestCalls)
	require.Equal(t, 1, dict.listCalls)
	_, err = c.Prepare(ctx, input)
	require.ErrorIs(t, err, types.ErrAbbreviationConflict)
	require.Equal(t, 1, dict.suggestCalls)
}

func TestAbbreviationTurnFreshQuestionDoesNotReuseLocalMeaning(t *testing.T) {
	_, store, dict, owner := newTurnCoordinatorFixture(t)
	c := NewAbbreviationTurnCoordinator(store, dict, nil)
	ctx := context.Background()
	first, err := c.Prepare(ctx, turnInput(owner, "u1", "a1", "ATTT là gì"))
	require.NoError(t, err)
	v := first.Version
	input := turnInput(owner, "u2", "a2", "An toàn thông tin")
	input.ContinuationID, input.ExpectedVersion = first.RequestID, &v
	_, err = c.Prepare(ctx, input)
	require.NoError(t, err)
	fresh, err := c.Prepare(ctx, turnInput(owner, "u3", "a3", "ATTT có yêu cầu gì"))
	require.NoError(t, err)
	require.Equal(t, types.AbbreviationStatusNeedsDefinition, fresh.Status)
	require.Equal(t, []string{"ATTT"}, fresh.UnknownTerms)
	require.NotEqual(t, first.RequestID, fresh.RequestID)
}

func TestAbbreviationTurnNoCandidateSkipsDictionary(t *testing.T) {
	_, store, dict, owner := newTurnCoordinatorFixture(t)
	c := NewAbbreviationTurnCoordinator(store, dict, nil)
	r, err := c.Prepare(context.Background(), turnInput(owner, "u1", "a1", "xin chào"))
	require.NoError(t, err)
	require.Equal(t, types.AbbreviationStatusReady, r.Status)
	require.Equal(t, "xin chào", r.EffectiveQuery)
	require.Zero(t, dict.listCalls)
}

func TestAbbreviationTurnExpiryDoesNotExtend(t *testing.T) {
	_, store, dict, owner := newTurnCoordinatorFixture(t)
	c := NewAbbreviationTurnCoordinator(store, dict, nil)
	fixed := time.Now()
	c.now = func() time.Time { return fixed }
	r, err := c.Prepare(context.Background(), turnInput(owner, "u1", "a1", "ATTT là gì"))
	require.NoError(t, err)
	c.now = func() time.Time { return fixed.Add(25 * time.Hour) }
	v := r.Version
	input := turnInput(owner, "u2", "a2", "An toàn thông tin")
	input.ContinuationID, input.ExpectedVersion = r.RequestID, &v
	_, err = c.Prepare(context.Background(), input)
	require.ErrorIs(t, err, types.ErrAbbreviationConflict)
}

func TestAbbreviationTurnPartialDefinition(t *testing.T) {
	_, store, dict, owner := newTurnCoordinatorFixture(t)
	c := NewAbbreviationTurnCoordinator(store, dict, nil)
	ctx := context.Background()
	first, err := c.Prepare(ctx, turnInput(owner, "u1", "a1", "ATTT và XYZ có yêu cầu gì"))
	require.NoError(t, err)
	require.Equal(t, []string{"ATTT", "XYZ"}, first.UnknownTerms)
	input := turnInput(owner, "u2", "a2", "ATTT = An toàn thông tin")
	v := first.Version
	input.ContinuationID, input.ExpectedVersion = first.RequestID, &v
	partial, err := c.Prepare(ctx, input)
	require.NoError(t, err)
	require.Equal(t, []string{"XYZ"}, partial.UnknownTerms)
	require.True(t, first.ExpiresAt.Equal(partial.ExpiresAt))
	input = turnInput(owner, "u3", "a3", "XYZ = Xây dựng y tế")
	v = partial.Version
	input.ContinuationID, input.ExpectedVersion = first.RequestID, &v
	ready, err := c.Prepare(ctx, input)
	require.NoError(t, err)
	require.Equal(t, types.AbbreviationStatusReady, ready.Status)
	require.Equal(t, 2, dict.suggestCalls)
}

func TestAbbreviationTurnPartialReplyKeepsOriginalDeadline(t *testing.T) {
	_, store, dict, owner := newTurnCoordinatorFixture(t)
	c := NewAbbreviationTurnCoordinator(store, dict, nil)
	fixed := time.Now()
	c.now = func() time.Time { return fixed }
	first, err := c.Prepare(context.Background(), turnInput(owner, "u1", "a1", "ATTT và XYZ có yêu cầu gì"))
	require.NoError(t, err)
	c.now = func() time.Time { return fixed.Add(time.Hour) }
	v := first.Version
	input := turnInput(owner, "u2", "a2", "ATTT = An toàn thông tin")
	input.ContinuationID, input.ExpectedVersion = first.RequestID, &v
	partial, err := c.Prepare(context.Background(), input)
	require.NoError(t, err)
	require.True(t, first.ExpiresAt.Equal(partial.ExpiresAt))
	c.now = func() time.Time { return fixed.Add(25 * time.Hour) }
	v = partial.Version
	input = turnInput(owner, "u3", "a3", "XYZ = Xây dựng y tế")
	input.ContinuationID, input.ExpectedVersion = first.RequestID, &v
	_, err = c.Prepare(context.Background(), input)
	require.ErrorIs(t, err, types.ErrAbbreviationConflict)
}

func TestAbbreviationTurnSuggestionFailureAndRetry(t *testing.T) {
	_, store, dict, owner := newTurnCoordinatorFixture(t)
	dict.suggestErr = errors.New("dictionary write unavailable")
	c := NewAbbreviationTurnCoordinator(store, dict, nil)
	r, err := c.Prepare(context.Background(), turnInput(owner, "u1", "a1", "ATTT là An toàn thông tin; ATTT có yêu cầu gì"))
	require.NoError(t, err)
	require.Equal(t, types.AbbreviationStatusReady, r.Status)
	require.Equal(t, types.AbbreviationSuggestionSaveFailed, r.Terms[0].SuggestionStatus)
	ctx := context.Background()
	stored, err := store.Get(ctx, owner, r.RequestID)
	require.NoError(t, err)
	stored.State = types.AbbreviationTurnRunning
	ok, err := store.CompareAndSwap(ctx, owner, stored.Version, stored)
	require.NoError(t, err)
	require.True(t, ok)
	stored, err = store.Get(ctx, owner, r.RequestID)
	require.NoError(t, err)
	stored.State = types.AbbreviationTurnCompleted
	ok, err = store.CompareAndSwap(ctx, owner, stored.Version, stored)
	require.NoError(t, err)
	require.True(t, ok)
	dict.suggestErr = nil
	require.NoError(t, c.RetrySuggestions(ctx, owner, r.RequestID))
	stored, err = store.Get(ctx, owner, r.RequestID)
	require.NoError(t, err)
	require.Equal(t, types.AbbreviationTurnCompleted, stored.State)
	require.Equal(t, types.AbbreviationSuggestionPendingReview, stored.Payload.Resolution.Terms[0].SuggestionStatus)
	require.Equal(t, 2, dict.suggestCalls)
}

func TestAbbreviationTurnDictionaryUnavailableBlocksUnconfirmed(t *testing.T) {
	_, store, dict, owner := newTurnCoordinatorFixture(t)
	dict.listErr = errors.New("dictionary read unavailable")
	c := NewAbbreviationTurnCoordinator(store, dict, nil)
	r, err := c.Prepare(context.Background(), turnInput(owner, "u1", "a1", "ATTT có yêu cầu gì"))
	require.NoError(t, err)
	require.Equal(t, types.AbbreviationStatusBlockedError, r.Status)
	require.Equal(t, "dictionary_unavailable", r.ErrorCode)
	require.Zero(t, dict.suggestCalls)
}

func TestAbbreviationTurnUsesSingleActive(t *testing.T) {
	_, store, dict, owner := newTurnCoordinatorFixture(t)
	dict.active = []*types.Abbreviation{{ID: "active", ShortForm: "ATTT", FullForm: "An toàn thông tin", IsActive: true}}
	c := NewAbbreviationTurnCoordinator(store, dict, nil)
	r, err := c.Prepare(context.Background(), turnInput(owner, "u1", "a1", "ATTT có yêu cầu gì"))
	require.NoError(t, err)
	require.Equal(t, types.AbbreviationStatusReady, r.Status)
	require.Equal(t, "An toàn thông tin (ATTT) có yêu cầu gì", r.EffectiveQuery)
	require.Equal(t, types.AbbreviationSourceDictionaryActive, r.Terms[0].Source)
	require.Zero(t, dict.suggestCalls)
}
