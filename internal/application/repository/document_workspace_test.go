package repository

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// newDocumentWorkspaceTestDB applies the real SQLite migration so the test
// also covers the schema (partial unique index on live sessions).
func newDocumentWorkspaceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..", "..")
	up, err := os.ReadFile(filepath.Join(root, "migrations", "sqlite", "000032_document_workspaces.up.sql"))
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(up)).Error)
	return db
}

func TestDocumentWorkspaceRepository(t *testing.T) {
	ctx := context.Background()
	repo := NewDocumentWorkspaceRepository(newDocumentWorkspaceTestDB(t))

	ws := &types.DocumentWorkspace{
		TenantID: 1, SessionID: "s1", UserID: "u1", OriginalRef: "resource://a", CurrentRef: "resource://a",
		FileName: "a.docx", FileSize: 10,
	}
	require.NoError(t, repo.Create(ctx, ws))
	require.NotEmpty(t, ws.ID)
	require.Equal(t, types.DocumentWorkspaceStatusOpen, ws.Status)

	dup := &types.DocumentWorkspace{TenantID: 1, SessionID: "s1", OriginalRef: "x", CurrentRef: "x", FileName: "b.docx"}
	require.Error(t, repo.Create(ctx, dup), "one live workspace per session")

	got, err := repo.GetBySession(ctx, 1, "s1")
	require.NoError(t, err)
	require.Equal(t, ws.ID, got.ID)
	missing, err := repo.GetBySession(ctx, 2, "s1")
	require.NoError(t, err)
	require.Nil(t, missing)

	got.CurrentRef = "resource://b"
	got.Revision = 1
	ok, err := repo.UpdateIfRevision(ctx, got, 0)
	require.NoError(t, err)
	require.True(t, ok)

	stale := *got
	stale.CurrentRef = "resource://stale"
	stale.Revision = 1
	ok, err = repo.UpdateIfRevision(ctx, &stale, 0)
	require.NoError(t, err)
	require.False(t, ok, "expected revision moved")

	now := time.Now()
	got.Status = types.DocumentWorkspaceStatusClosed
	got.ClosedAt = &now
	got.SaveCount = 3
	require.NoError(t, repo.Update(ctx, got))
	byID, err := repo.GetByID(ctx, ws.ID)
	require.NoError(t, err)
	require.Equal(t, "resource://b", byID.CurrentRef)
	require.Equal(t, 1, byID.Revision)
	require.Equal(t, 3, byID.SaveCount)
	require.Equal(t, types.DocumentWorkspaceStatusClosed, byID.Status)
	require.NotNil(t, byID.ClosedAt)

	require.NoError(t, repo.Delete(ctx, 1, "s1"))
	gone, err := repo.GetByID(ctx, ws.ID)
	require.NoError(t, err)
	require.Nil(t, gone)
	// The partial unique index lets the session open a new document.
	require.NoError(t, repo.Create(ctx, &types.DocumentWorkspace{
		TenantID: 1, SessionID: "s1", OriginalRef: "c", CurrentRef: "c", FileName: "c.docx",
	}))
}
