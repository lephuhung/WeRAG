package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type turnSelector struct {
	calls  int
	ids    map[string]string
	err    error
	cancel context.CancelFunc
}

func (s *turnSelector) Select(ctx context.Context, _, _ string, _ types.AbbreviationResolution) (map[string]string, error) {
	s.calls++
	if s.cancel != nil {
		s.cancel()
		return nil, ctx.Err()
	}
	return s.ids, s.err
}

func TestAbbreviationTurnWaitsForUnknownBeforeSelector(t *testing.T) {
	_, store, dict, owner := newTurnCoordinatorFixture(t)
	dict.active = []*types.Abbreviation{
		{ID: "a", ShortForm: "ATTT", FullForm: "An toàn thông tin", IsActive: true},
		{ID: "b", ShortForm: "ATTT", FullForm: "An toàn thực phẩm", IsActive: true},
	}
	selector := &turnSelector{ids: map[string]string{"attt": "b"}}
	c := NewAbbreviationTurnCoordinator(store, dict, selector)
	first, err := c.Prepare(context.Background(), turnInput(owner, "u1", "a1", "ATTT và XYZ cần gì"))
	require.NoError(t, err)
	require.Equal(t, []string{"XYZ"}, first.UnknownTerms)
	require.Zero(t, selector.calls)
	v := first.Version
	input := turnInput(owner, "u2", "a2", "XYZ = Xây dựng y tế")
	input.ContinuationID, input.ExpectedVersion = first.RequestID, &v
	ready, err := c.Prepare(context.Background(), input)
	require.NoError(t, err)
	require.Equal(t, types.AbbreviationStatusReady, ready.Status)
	require.Equal(t, "An toàn thực phẩm (ATTT) và Xây dựng y tế (XYZ) cần gì", ready.EffectiveQuery)
	require.Equal(t, 1, selector.calls)
}

func TestAbbreviationTurnSelectorFailureBlocks(t *testing.T) {
	_, store, dict, owner := newTurnCoordinatorFixture(t)
	dict.active = []*types.Abbreviation{
		{ID: "a", ShortForm: "ATTT", FullForm: "An toàn thông tin", IsActive: true},
		{ID: "b", ShortForm: "ATTT", FullForm: "An toàn thực phẩm", IsActive: true},
	}
	selector := &turnSelector{err: errors.New("model unavailable")}
	c := NewAbbreviationTurnCoordinator(store, dict, selector)
	first, err := c.Prepare(context.Background(), turnInput(owner, "u1", "a1", "ATTT và XYZ là gì"))
	require.NoError(t, err)
	require.Equal(t, types.AbbreviationStatusNeedsDefinition, first.Status)
	v := first.Version
	input := turnInput(owner, "u2", "a2", "XYZ = Xây dựng y tế")
	input.ContinuationID, input.ExpectedVersion = first.RequestID, &v
	blocked, err := c.Prepare(context.Background(), input)
	require.NoError(t, err)
	require.Equal(t, types.AbbreviationStatusBlockedError, blocked.Status)
	require.Equal(t, "selector_unavailable", blocked.ErrorCode)
	stored, err := store.Get(context.Background(), owner, first.RequestID)
	require.NoError(t, err)
	require.Equal(t, types.AbbreviationTurnBlockedError, stored.State)
	require.Equal(t, 1, selector.calls)
}

func TestAbbreviationTurnCancelledSelectorPersistsBlockedState(t *testing.T) {
	_, store, dict, owner := newTurnCoordinatorFixture(t)
	dict.active = []*types.Abbreviation{
		{ID: "a", ShortForm: "ATTT", FullForm: "An toàn thông tin", IsActive: true},
		{ID: "b", ShortForm: "ATTT", FullForm: "An toàn thực phẩm", IsActive: true},
	}
	ctx, cancel := context.WithCancel(context.Background())
	selector := &turnSelector{cancel: cancel}
	c := NewAbbreviationTurnCoordinator(store, dict, selector)
	r, err := c.Prepare(ctx, turnInput(owner, "u1", "a1", "ATTT là gì"))
	require.NoError(t, err)
	require.Equal(t, types.AbbreviationStatusBlockedError, r.Status)
	require.Equal(t, "selector_unavailable", r.ErrorCode)
	stored, err := store.Get(context.Background(), owner, r.RequestID)
	require.NoError(t, err)
	require.Equal(t, types.AbbreviationTurnBlockedError, stored.State)
}

func TestAbbreviationTurnInvalidSelectorIDBlocks(t *testing.T) {
	_, store, dict, owner := newTurnCoordinatorFixture(t)
	dict.active = []*types.Abbreviation{
		{ID: "a", ShortForm: "ATTT", FullForm: "An toàn thông tin", IsActive: true},
		{ID: "b", ShortForm: "ATTT", FullForm: "An toàn thực phẩm", IsActive: true},
	}
	c := NewAbbreviationTurnCoordinator(store, dict, &turnSelector{ids: map[string]string{"attt": "invented"}})
	r, err := c.Prepare(context.Background(), turnInput(owner, "u1", "a1", "ATTT là gì"))
	require.NoError(t, err)
	require.Equal(t, types.AbbreviationStatusBlockedError, r.Status)
	require.Equal(t, "invalid_selection", r.ErrorCode)
	require.Empty(t, r.EffectiveQuery)
}

func TestAbbreviationTurnDictionaryReadFailureUsesConfirmedMeaning(t *testing.T) {
	_, store, dict, owner := newTurnCoordinatorFixture(t)
	dict.listErr = errors.New("read unavailable")
	dict.suggestErr = errors.New("write unavailable")
	c := NewAbbreviationTurnCoordinator(store, dict, nil)
	r, err := c.Prepare(context.Background(), turnInput(owner, "u1", "a1", "ATTT là An toàn thông tin; ATTT có yêu cầu gì"))
	require.NoError(t, err)
	require.Equal(t, types.AbbreviationStatusReady, r.Status)
	require.Equal(t, "An toàn thông tin (ATTT) là An toàn thông tin; An toàn thông tin (ATTT) có yêu cầu gì", r.EffectiveQuery)
	require.Equal(t, types.AbbreviationSuggestionSaveFailed, r.Terms[0].SuggestionStatus)
}

type failingTurnStore struct {
	interfaces.AbbreviationTurnRepository
	err error
}

func (s *failingTurnStore) Awaiting(context.Context, types.AbbreviationOwner, time.Time) (*types.AbbreviationTurnState, error) {
	return nil, nil
}

func (s *failingTurnStore) Begin(context.Context, types.AbbreviationOwner, *types.AbbreviationTurnState) error {
	return s.err
}

func TestAbbreviationTurnStateFailureBlocksSuggestion(t *testing.T) {
	dict := &turnDictionary{}
	failure := errors.New("state unavailable")
	c := NewAbbreviationTurnCoordinator(&failingTurnStore{err: failure}, dict, nil)
	owner := types.AbbreviationOwner{TenantID: 7, SessionID: "s", OwnerID: "alice", PrincipalID: "web_user:alice"}
	_, err := c.Prepare(context.Background(), turnInput(owner, "u1", "a1", "ATTT là An toàn thông tin; ATTT có yêu cầu gì"))
	require.ErrorIs(t, err, failure)
	require.Zero(t, dict.suggestCalls)
}

func TestAbbreviationTurnForeignAssistantNeverSuggests(t *testing.T) {
	_, store, dict, owner := newTurnCoordinatorFixture(t)
	c := NewAbbreviationTurnCoordinator(store, dict, nil)
	_, err := c.Prepare(context.Background(), turnInput(owner, "u1", "foreign-a", "ATTT là An toàn thông tin; ATTT có yêu cầu gì"))
	require.ErrorIs(t, err, types.ErrAbbreviationConflict)
	require.Zero(t, dict.suggestCalls)
}

func TestAbbreviationTurnForeignOwnerSkipsDictionary(t *testing.T) {
	_, store, dict, owner := newTurnCoordinatorFixture(t)
	c := NewAbbreviationTurnCoordinator(store, dict, nil)
	owner.OwnerID = "mallory"
	_, err := c.Prepare(context.Background(), turnInput(owner, "u1", "a1", "ATTT có yêu cầu gì"))
	require.Error(t, err)
	require.Zero(t, dict.listCalls)
	require.Zero(t, dict.suggestCalls)
}

func TestAbbreviationTurnExplicitContinuationRequiresVersion(t *testing.T) {
	_, store, dict, owner := newTurnCoordinatorFixture(t)
	c := NewAbbreviationTurnCoordinator(store, dict, nil)
	first, err := c.Prepare(context.Background(), turnInput(owner, "u1", "a1", "ATTT là gì"))
	require.NoError(t, err)
	input := turnInput(owner, "u2", "a2", "An toàn thông tin")
	input.ContinuationID = first.RequestID
	_, err = c.Prepare(context.Background(), input)
	require.ErrorIs(t, err, types.ErrAbbreviationConflict)
	require.Zero(t, dict.suggestCalls)
}

func TestAbbreviationTurnOtherOwnerCannotResume(t *testing.T) {
	_, store, dict, owner := newTurnCoordinatorFixture(t)
	c := NewAbbreviationTurnCoordinator(store, dict, nil)
	first, err := c.Prepare(context.Background(), turnInput(owner, "u1", "a1", "ATTT là gì"))
	require.NoError(t, err)
	other := owner
	other.OwnerID = "mallory"
	v := first.Version
	input := turnInput(other, "u2", "a2", "An toàn thông tin")
	input.ContinuationID, input.ExpectedVersion = first.RequestID, &v
	_, err = c.Prepare(context.Background(), input)
	require.ErrorIs(t, err, types.ErrAbbreviationNotFound)
	require.Zero(t, dict.suggestCalls)
}
