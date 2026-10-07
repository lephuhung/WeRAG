package repository

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestDocumentRevisionRepository(t *testing.T) {
	ctx := context.Background()
	db := newDocumentWorkspaceTestDB(t)
	_, file, _, _ := runtime.Caller(0)
	up, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..",
		"migrations", "sqlite", "000035_document_revisions.up.sql"))
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(up)).Error)
	repo := NewDocumentRevisionRepository(db)

	latest, err := repo.Latest(ctx, "ws1")
	require.NoError(t, err)
	require.Nil(t, latest)

	for i, label := range []string{"a", "b", "c"} {
		rev := &types.DocumentRevision{WorkspaceID: "ws1", TenantID: 1, Ref: "resource://" + label, Label: label, Source: "manual"}
		require.NoError(t, repo.Create(ctx, rev))
		require.Equal(t, i+1, rev.Seq)
		require.NotEmpty(t, rev.ID)
	}
	other := &types.DocumentRevision{WorkspaceID: "ws2", TenantID: 1, Ref: "resource://z", Source: "ai"}
	require.NoError(t, repo.Create(ctx, other))
	require.Equal(t, 1, other.Seq, "seq is per workspace")

	list, err := repo.ListByWorkspace(ctx, "ws1")
	require.NoError(t, err)
	require.Len(t, list, 3)
	require.Equal(t, []string{"c", "b", "a"}, []string{list[0].Label, list[1].Label, list[2].Label})

	got, err := repo.GetBySeq(ctx, "ws1", 2)
	require.NoError(t, err)
	require.Equal(t, "resource://b", got.Ref)
	missing, err := repo.GetBySeq(ctx, "ws1", 9)
	require.NoError(t, err)
	require.Nil(t, missing)

	latest, err = repo.Latest(ctx, "ws1")
	require.NoError(t, err)
	require.Equal(t, 3, latest.Seq)

	bad := &types.DocumentRevision{WorkspaceID: "ws1", TenantID: 1, Ref: "x", Source: "bogus"}
	require.Error(t, repo.Create(ctx, bad), "source CHECK constraint")
}
