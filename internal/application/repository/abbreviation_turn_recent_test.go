package repository

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
)

type recentAnsweredReader interface {
	RecentAnswered(ctx context.Context, owner types.AbbreviationOwner, limit int) ([]*types.AbbreviationTurnState, error)
}

// RecentAnswered lists the owner's turns past the gate, newest question
// first, never a turn still waiting for the user, another owner's turn, a
// turn whose question was deleted, or anything of a deleted session.
func TestAbbreviationTurnRecentAnswered(t *testing.T) {
	db, repo := newAbbreviationTurnRepo(t)
	reader, ok := repo.(recentAnsweredReader)
	require.True(t, ok)
	ctx := context.Background()
	owner := turnTestOwner()
	expires := time.Now().Add(24 * time.Hour)

	add := func(id, root, state string, created time.Time) {
		row := turnTestRow(expires)
		row.ID, row.RootUserMessageID = id, root
		require.NoError(t, repo.Begin(ctx, owner, row))
		for _, next := range map[string][]string{
			"ready":               {"ready"},
			"completed":           {"ready", "running", "completed"},
			"cancelled":           {"ready", "running", "cancelled"},
			"awaiting_definition": {"awaiting_definition"},
		}[state] {
			row.State = next
			swapped, err := repo.CompareAndSwap(ctx, owner, row.Version, row)
			require.NoError(t, err)
			require.True(t, swapped, "%s → %s", id, next)
			row.Version++
		}
		require.NoError(t, db.Exec("UPDATE abbreviation_turn_states SET created_at = ? WHERE id = ?", created, id).Error)
	}
	base := time.Now().Add(-time.Hour)
	add("old", "u1", "completed", base)
	add("new", "u2", "cancelled", base.Add(10*time.Minute))
	add("wait", "def-1", "awaiting_definition", base.Add(20*time.Minute))
	// a later metadata write on the old turn must not make it the newest
	require.NoError(t, db.Exec("UPDATE abbreviation_turn_states SET updated_at = ? WHERE id = 'old'", time.Now()).Error)

	rows, err := reader.RecentAnswered(ctx, owner, 10)
	require.NoError(t, err)
	require.Equal(t, []string{"new", "old"}, turnIDs(rows))

	rows, err = reader.RecentAnswered(ctx, owner, 1)
	require.NoError(t, err)
	require.Equal(t, []string{"new"}, turnIDs(rows))
	rows, err = reader.RecentAnswered(ctx, owner, 0)
	require.NoError(t, err)
	require.Empty(t, rows)

	foreign := owner
	foreign.OwnerID, foreign.PrincipalID = "mallory", "web_user:mallory"
	rows, err = reader.RecentAnswered(ctx, foreign, 10)
	require.NoError(t, err)
	require.Empty(t, rows)
	other := types.AbbreviationOwner{TenantID: 7, SessionID: "s-other", OwnerID: "bob", PrincipalID: "web_user:bob"}
	rows, err = reader.RecentAnswered(ctx, other, 10)
	require.NoError(t, err)
	require.Empty(t, rows)

	require.NoError(t, db.Exec("UPDATE messages SET deleted_at = CURRENT_TIMESTAMP WHERE id = 'u2'").Error)
	rows, err = reader.RecentAnswered(ctx, owner, 10)
	require.NoError(t, err)
	require.Equal(t, []string{"old"}, turnIDs(rows))

	require.NoError(t, db.Exec("UPDATE sessions SET deleted_at = CURRENT_TIMESTAMP WHERE id = 's'").Error)
	rows, err = reader.RecentAnswered(ctx, owner, 10)
	require.NoError(t, err)
	require.Empty(t, rows)
}

func turnIDs(rows []*types.AbbreviationTurnState) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.ID)
	}
	return out
}
