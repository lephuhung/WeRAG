package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
)

// A meaning picked among several is reused for the same short form later in
// the conversation, with a note; "ask again" brings the choice back, and
// another conversation is asked as before.
func TestAbbreviationTurnReusesMeaningPickedInConversation(t *testing.T) {
	db, store, dict, owner := newTurnCoordinatorFixture(t)
	dict.active = []*types.Abbreviation{
		{ID: "m-tt", ShortForm: "ATTT", FullForm: "An toàn thông tin", IsActive: true},
		{ID: "m-tp", ShortForm: "ATTT", FullForm: "An toàn thực phẩm", IsActive: true},
	}
	require.NoError(t, db.Exec(`INSERT INTO messages (id, session_id, role) VALUES ('u4','s','user'),('a4','s','assistant'),('u5','other','user'),('a5','other','assistant')`).Error)
	c := NewAbbreviationTurnCoordinator(store, dict, nil)
	ctx := context.Background()

	first, err := c.Prepare(ctx, turnInput(owner, "u1", "a1", "ATTT là gì"))
	require.NoError(t, err)
	require.Equal(t, types.AbbreviationStatusNeedsDefinition, first.Status, "several meanings: the user picks")
	v := first.Version
	pick := turnInput(owner, "u2", "a2", "ATTT = An toàn thực phẩm")
	pick.ContinuationID, pick.ExpectedVersion = first.RequestID, &v
	picked, err := c.Prepare(ctx, pick)
	require.NoError(t, err)
	require.Equal(t, types.AbbreviationStatusReady, picked.Status)
	require.Equal(t, "m-tp", picked.Terms[0].SelectedID)
	require.Empty(t, picked.RememberedSelections)

	again, err := c.Prepare(ctx, turnInput(owner, "u3", "a3", "ATTT có yêu cầu gì"))
	require.NoError(t, err)
	require.Equal(t, types.AbbreviationStatusReady, again.Status, "the earlier pick is reused")
	require.Equal(t, "An toàn thực phẩm (ATTT) có yêu cầu gì", again.EffectiveQuery)
	require.Equal(t, []types.AbbreviationTypo{{Wrong: "ATTT", Correct: "An toàn thực phẩm"}}, again.RememberedSelections)
	require.Equal(t, again.RememberedSelections, again.PublicState().RememberedSelections)

	// "Không phải?" re-sends the question with the word to ask about
	recheck := turnInput(owner, "u4", "a4", "ATTT có yêu cầu gì")
	recheck.AskWords = []string{"ATTT"}
	asked, err := c.Prepare(ctx, recheck)
	require.NoError(t, err)
	require.Equal(t, types.AbbreviationStatusNeedsDefinition, asked.Status)
	require.Equal(t, []string{"ATTT"}, asked.UnknownTerms)
	require.Empty(t, asked.RememberedSelections)

	// another conversation knows nothing of it
	other := types.AbbreviationOwner{TenantID: 7, SessionID: "other", OwnerID: "bob", PrincipalID: "web_user:bob"}
	fresh, err := c.Prepare(ctx, turnInput(other, "u5", "a5", "ATTT có yêu cầu gì"))
	require.NoError(t, err)
	require.Equal(t, types.AbbreviationStatusNeedsDefinition, fresh.Status)
}

// A remembered meaning that is no longer active is not reused, and a
// deleted question no longer counts as a pick.
func TestAbbreviationTurnRememberedMeaningMustStillBeActive(t *testing.T) {
	db, store, dict, owner := newTurnCoordinatorFixture(t)
	dict.active = []*types.Abbreviation{
		{ID: "m-tt", ShortForm: "ATTT", FullForm: "An toàn thông tin", IsActive: true},
		{ID: "m-tp", ShortForm: "ATTT", FullForm: "An toàn thực phẩm", IsActive: true},
	}
	c := NewAbbreviationTurnCoordinator(store, dict, nil)
	ctx := context.Background()
	first, err := c.Prepare(ctx, turnInput(owner, "u1", "a1", "ATTT là gì"))
	require.NoError(t, err)
	v := first.Version
	pick := turnInput(owner, "u2", "a2", "ATTT = An toàn thực phẩm")
	pick.ContinuationID, pick.ExpectedVersion = first.RequestID, &v
	_, err = c.Prepare(ctx, pick)
	require.NoError(t, err)

	dict.active = []*types.Abbreviation{
		{ID: "m-tt", ShortForm: "ATTT", FullForm: "An toàn thông tin", IsActive: true},
		{ID: "m-new", ShortForm: "ATTT", FullForm: "An toàn thiết bị", IsActive: true},
	}
	r, err := c.Prepare(ctx, turnInput(owner, "u3", "a3", "ATTT có yêu cầu gì"))
	require.NoError(t, err)
	require.Equal(t, types.AbbreviationStatusNeedsDefinition, r.Status)

	dict.active = []*types.Abbreviation{
		{ID: "m-tt", ShortForm: "ATTT", FullForm: "An toàn thông tin", IsActive: true},
		{ID: "m-tp", ShortForm: "ATTT", FullForm: "An toàn thực phẩm", IsActive: true},
	}
	require.NoError(t, db.Exec(`UPDATE messages SET deleted_at = CURRENT_TIMESTAMP WHERE id = 'u1'`).Error)
	require.NoError(t, db.Exec(`INSERT INTO messages (id, session_id, role) VALUES ('u4','s','user'),('a4','s','assistant')`).Error)
	r, err = c.Prepare(ctx, turnInput(owner, "u4", "a4", "ATTT có yêu cầu gì"))
	require.NoError(t, err)
	require.Equal(t, types.AbbreviationStatusNeedsDefinition, r.Status, "the pick's question was deleted")
}

// The newest pick wins (question order), a pick whose answer was stopped
// still counts, and on a continuation a remembered term is filled while
// the user is asked only about the other.
func TestAbbreviationTurnRememberedNewestAndPartial(t *testing.T) {
	db, store, dict, owner := newTurnCoordinatorFixture(t)
	dict.active = []*types.Abbreviation{
		{ID: "m-tt", ShortForm: "ATTT", FullForm: "An toàn thông tin", IsActive: true},
		{ID: "m-tp", ShortForm: "ATTT", FullForm: "An toàn thực phẩm", IsActive: true},
		{ID: "p1", ShortForm: "PCCC", FullForm: "Phòng cháy chữa cháy", IsActive: true},
		{ID: "p2", ShortForm: "PCCC", FullForm: "Phòng chống cháy công cộng", IsActive: true},
	}
	require.NoError(t, db.Exec(`INSERT INTO messages (id, session_id, role) VALUES ('u4','s','user'),('a4','s','assistant'),('u5','s','user'),('a5','s','assistant'),('u6','s','user'),('a6','s','assistant')`).Error)
	c := NewAbbreviationTurnCoordinator(store, dict, nil)
	ctx := context.Background()
	pickIn := func(u, a, q, reply, u2, a2 string) types.AbbreviationResolution {
		first, err := c.Prepare(ctx, turnInput(owner, u, a, q))
		require.NoError(t, err)
		require.Equal(t, types.AbbreviationStatusNeedsDefinition, first.Status)
		v := first.Version
		in := turnInput(owner, u2, a2, reply)
		in.ContinuationID, in.ExpectedVersion = first.RequestID, &v
		r, err := c.Prepare(ctx, in)
		require.NoError(t, err)
		return r
	}
	pickIn("u1", "a1", "ATTT là gì", "ATTT = An toàn thực phẩm", "u2", "a2")
	// asked again and picked the other meaning
	again := turnInput(owner, "u3", "a3", "ATTT là gì")
	again.AskWords = []string{"ATTT"}
	first, err := c.Prepare(ctx, again)
	require.NoError(t, err)
	v := first.Version
	in := turnInput(owner, "u4", "a4", "ATTT = An toàn thông tin")
	in.ContinuationID, in.ExpectedVersion = first.RequestID, &v
	second, err := c.Prepare(ctx, in)
	require.NoError(t, err)
	require.Equal(t, types.AbbreviationStatusReady, second.Status)
	// the turn's answer was stopped: the pick still counts
	require.NoError(t, db.Exec(`UPDATE abbreviation_turn_states SET state = 'cancelled' WHERE id = ?`, second.RequestID).Error)

	r, err := c.Prepare(ctx, turnInput(owner, "u5", "a5", "ATTT và PCCC"))
	require.NoError(t, err)
	require.Equal(t, types.AbbreviationStatusNeedsDefinition, r.Status, "PCCC was never picked")
	require.Equal(t, []string{"PCCC"}, r.UnknownTerms)
	require.Equal(t, []types.AbbreviationTypo{{Wrong: "ATTT", Correct: "An toàn thông tin"}}, r.RememberedSelections)

	v = r.Version
	in = turnInput(owner, "u6", "a6", "PCCC = Phòng cháy chữa cháy")
	in.ContinuationID, in.ExpectedVersion = r.RequestID, &v
	done, err := c.Prepare(ctx, in)
	require.NoError(t, err)
	require.Equal(t, types.AbbreviationStatusReady, done.Status)
	require.Equal(t, "An toàn thông tin (ATTT) và Phòng cháy chữa cháy (PCCC)", done.EffectiveQuery)
}
