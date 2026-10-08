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
	for _, name := range []string{"000032_document_workspaces.up.sql", "000036_document_workspaces_multi.up.sql", "000037_document_workspace_roles.up.sql"} {
		up, err := os.ReadFile(filepath.Join(root, "migrations", "sqlite", name))
		require.NoError(t, err)
		require.NoError(t, db.Exec(string(up)).Error, name)
	}
	return db
}

func TestDocumentWorkspaceRepository(t *testing.T) {
	ctx := context.Background()
	repo := NewDocumentWorkspaceRepository(newDocumentWorkspaceTestDB(t))

	ws := &types.DocumentWorkspace{
		TenantID: 1, SessionID: "s1", AttachmentID: "att-a", Position: 1, UserID: "u1",
		OriginalRef: "resource://a", CurrentRef: "resource://a", FileName: "a.docx", FileSize: 10,
	}
	require.NoError(t, repo.Create(ctx, ws))
	require.NotEmpty(t, ws.ID)
	require.Equal(t, types.DocumentWorkspaceStatusOpen, ws.Status)

	dup := &types.DocumentWorkspace{TenantID: 1, SessionID: "s1", AttachmentID: "att-a", OriginalRef: "x", CurrentRef: "x", FileName: "b.docx"}
	require.Error(t, repo.Create(ctx, dup), "one live document per upload")

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
	// The partial unique index lets the session open the upload again.
	require.NoError(t, repo.Create(ctx, &types.DocumentWorkspace{
		TenantID: 1, SessionID: "s1", AttachmentID: "att-a", OriginalRef: "c", CurrentRef: "c", FileName: "c.docx",
	}))
}

func TestDocumentWorkspaceRepositorySeveralDocuments(t *testing.T) {
	ctx := context.Background()
	repo := NewDocumentWorkspaceRepository(newDocumentWorkspaceTestDB(t))

	pos, err := repo.NextPosition(ctx, 1, "s1")
	require.NoError(t, err)
	require.Equal(t, 1, pos)

	earlier := time.Now().Add(-time.Minute)
	a := &types.DocumentWorkspace{TenantID: 1, SessionID: "s1", AttachmentID: "a", Position: 1, ActiveAt: &earlier,
		OriginalRef: "a", CurrentRef: "a", FileName: "a.docx"}
	b := &types.DocumentWorkspace{TenantID: 1, SessionID: "s1", AttachmentID: "b", Position: 2,
		OriginalRef: "b", CurrentRef: "b", FileName: "b.docx"}
	require.NoError(t, repo.Create(ctx, a))
	require.NoError(t, repo.Create(ctx, b))

	list, err := repo.ListBySession(ctx, 1, "s1")
	require.NoError(t, err)
	require.Len(t, list, 2)
	require.Equal(t, a.ID, list[0].ID, "tab order is the position")

	active, err := repo.GetBySession(ctx, 1, "s1")
	require.NoError(t, err)
	require.Equal(t, a.ID, active.ID, "a document never activated ranks after an activated one")
	require.NoError(t, repo.SetActive(ctx, b.ID, time.Now()))
	active, err = repo.GetBySession(ctx, 1, "s1")
	require.NoError(t, err)
	require.Equal(t, b.ID, active.ID)

	require.NoError(t, repo.DeleteByID(ctx, 1, "s1", b.ID))
	list, err = repo.ListBySession(ctx, 1, "s1")
	require.NoError(t, err)
	require.Len(t, list, 1)
	pos, err = repo.NextPosition(ctx, 1, "s1")
	require.NoError(t, err)
	require.Equal(t, 3, pos, "positions of closed documents are not reused")
}

func TestDocumentWorkspaceRepositoryRolesAndSourceText(t *testing.T) {
	ctx := context.Background()
	repo := NewDocumentWorkspaceRepository(newDocumentWorkspaceTestDB(t))

	target := &types.DocumentWorkspace{TenantID: 1, SessionID: "s1", AttachmentID: "a", Position: 1,
		OriginalRef: "a", CurrentRef: "a", FileName: "a.docx"}
	require.NoError(t, repo.Create(ctx, target))
	require.Equal(t, types.DocumentWorkspaceRoleTarget, target.Role, "a row without a role is a target")
	now := time.Now()
	source := &types.DocumentWorkspace{TenantID: 1, SessionID: "s1", AttachmentID: "b", Position: 2, ActiveAt: &now,
		OriginalRef: "b", CurrentRef: "b", FileName: "b.pdf", FileType: "pdf",
		Role: types.DocumentWorkspaceRoleSource, TextStatus: types.DocumentSourceTextProcessing}
	require.NoError(t, repo.Create(ctx, source))

	active, err := repo.GetBySession(ctx, 1, "s1")
	require.NoError(t, err)
	require.Equal(t, target.ID, active.ID, "a source is never the active tab")
	list, err := repo.ListBySession(ctx, 1, "s1")
	require.NoError(t, err)
	require.Len(t, list, 2)
	require.Equal(t, types.DocumentWorkspaceRoleSource, list[1].Role)
	require.Equal(t, "pdf", list[1].FileType)

	onlySource := &types.DocumentWorkspace{TenantID: 1, SessionID: "s2", AttachmentID: "c", Position: 1,
		OriginalRef: "c", CurrentRef: "c", FileName: "c.xlsx", Role: types.DocumentWorkspaceRoleSource}
	require.NoError(t, repo.Create(ctx, onlySource))
	none, err := repo.GetBySession(ctx, 1, "s2")
	require.NoError(t, err)
	require.Nil(t, none, "a session holding only sources has no active document")

	missing, err := repo.GetText(ctx, source.ID)
	require.NoError(t, err)
	require.Nil(t, missing)
	require.NoError(t, repo.SaveText(ctx, &types.DocumentWorkspaceText{WorkspaceID: source.ID, TenantID: 1,
		Content: "bản đầu", Chunks: types.JSON(`[{"seq":0,"content":"bản đầu"}]`), ChunkCount: 1}))
	require.NoError(t, repo.SaveText(ctx, &types.DocumentWorkspaceText{WorkspaceID: source.ID, TenantID: 1,
		Content: "bản sau", ChunkCount: 0}))
	text, err := repo.GetText(ctx, source.ID)
	require.NoError(t, err)
	require.Equal(t, "bản sau", text.Content, "saving again replaces the text")
	require.JSONEq(t, `[]`, string(text.Chunks))

	// the role and text status are written by Update (promotion, parse end)
	source.Role = types.DocumentWorkspaceRoleTarget
	source.TextStatus = ""
	require.NoError(t, repo.Update(ctx, source))
	got, err := repo.GetByID(ctx, source.ID)
	require.NoError(t, err)
	require.Equal(t, types.DocumentWorkspaceRoleTarget, got.Role)
	require.Equal(t, "", got.TextStatus)
}

func TestDocumentWorkspaceRolesMigrationRollsBack(t *testing.T) {
	db := newDocumentWorkspaceTestDB(t)
	_, file, _, _ := runtime.Caller(0)
	dir := filepath.Join(filepath.Dir(file), "..", "..", "..", "migrations", "sqlite")
	require.NoError(t, db.Exec(`INSERT INTO document_workspaces (id, tenant_id, session_id, original_ref, current_ref, file_name, status, role)
		VALUES ('t', 1, 's', 'r', 'r', 'a.docx', 'open', 'target'), ('src', 1, 's', 'r2', 'r2', 'b.pdf', 'open', 'source')`).Error)
	down, err := os.ReadFile(filepath.Join(dir, "000037_document_workspace_roles.down.sql"))
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(down)).Error)
	var live int64
	require.NoError(t, db.Raw(`SELECT COUNT(*) FROM document_workspaces WHERE deleted_at IS NULL`).Scan(&live).Error)
	require.EqualValues(t, 1, live, "the source is dropped with the role column")
	up, err := os.ReadFile(filepath.Join(dir, "000037_document_workspace_roles.up.sql"))
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(up)).Error, "up again after down")
}
