package tools

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

// countingWorkspace counts the files read from storage.
type countingWorkspace struct {
	*fakeWorkspace
	opens int
}

func (c *countingWorkspace) OpenCurrent(ctx context.Context, tenantID uint64, sessionID, documentID string) (io.ReadCloser, *types.DocumentWorkspace, error) {
	c.opens++
	return c.fakeWorkspace.OpenCurrent(ctx, tenantID, sessionID, documentID)
}

// freshWorkspaceDocs gives a test an empty document cache.
func freshWorkspaceDocs(t *testing.T) {
	t.Helper()
	prev := workspaceDocs
	workspaceDocs = newWorkspaceDocCache()
	t.Cleanup(func() { workspaceDocs = prev })
}

func TestBuildOpenDocumentPromptReadsEachVersionOnce(t *testing.T) {
	freshWorkspaceDocs(t)
	ws := &countingWorkspace{fakeWorkspace: fakeWorkspaceWithID(testCongVan(t, [4]int{20, 15, 30, 20}), "ws-cache-1")}
	ws.ws.CurrentRef = "tenant/7/ws-cache-1.docx"

	first := BuildOpenDocumentPrompt(context.Background(), ws, 7, "s-cache", "")
	second := BuildOpenDocumentPrompt(context.Background(), ws, 7, "s-cache", "")
	if ws.opens != 1 {
		t.Fatalf("same revision: OpenCurrent called %d times, want 1", ws.opens)
	}
	if first == "" || first != second {
		t.Fatalf("cached prompt differs:\n%s\n---\n%s", first, second)
	}

	// an AI edit bumps the revision: the new file is read
	ws.ws.Revision++
	ws.content = testCongVan(t, [4]int{25, 15, 30, 20})
	third := BuildOpenDocumentPrompt(context.Background(), ws, 7, "s-cache", "")
	if ws.opens != 2 {
		t.Fatalf("bumped revision: OpenCurrent called %d times, want 2", ws.opens)
	}
	if !strings.Contains(third, `revision="4"`) {
		t.Fatalf("prompt must carry the new revision:\n%s", third)
	}

	// an editor save keeps the revision but replaces the file
	ws.ws.SaveCount++
	BuildOpenDocumentPrompt(context.Background(), ws, 7, "s-cache", "")
	if ws.opens != 3 {
		t.Fatalf("editor save: OpenCurrent called %d times, want 3", ws.opens)
	}

	// another session's document with the same ID is not served from it
	BuildOpenDocumentPrompt(context.Background(), ws, 7, "s-other", "")
	if ws.opens != 4 {
		t.Fatalf("other session: OpenCurrent called %d times, want 4", ws.opens)
	}
}

func TestReadWorkspaceDocumentSkipsUnchangedDownload(t *testing.T) {
	freshWorkspaceDocs(t)
	ws := &countingWorkspace{fakeWorkspace: fakeWorkspaceWithID(testCongVan(t, [4]int{20, 15, 30, 20}), "ws-cache-2")}
	ws.ws.CurrentRef = "tenant/7/ws-cache-2.docx"
	ctx := toolCtx()
	a, _, err := readWorkspaceDocument(ctx, ws, "s-cache-2", "ws-cache-2")
	if err != nil {
		t.Fatal(err)
	}
	// a snapshot that changed nothing: the row is the same, the bytes are
	// the cached ones
	b, _, err := readWorkspaceDocument(ctx, ws, "s-cache-2", "ws-cache-2")
	if err != nil || string(a) != string(b) {
		t.Fatalf("second read: %v", err)
	}
	ws.ws.SaveCount++
	ws.content = testCongVan(t, [4]int{25, 15, 30, 20})
	c, _, err := readWorkspaceDocument(ctx, ws, "s-cache-2", "ws-cache-2")
	if err != nil || string(c) == string(a) {
		t.Fatalf("a saved file must be read again: %v", err)
	}
}

func TestWorkspaceDocCacheIsBounded(t *testing.T) {
	c := newWorkspaceDocCache()
	for i := 0; i < workspaceDocCacheEntries+5; i++ {
		ws := &types.DocumentWorkspace{ID: "d", CurrentRef: "ref"}
		c.put(workspaceDocKey(1, "s", string(rune('a'+i))), ws, []byte("x"))
	}
	if c.order.Len() != workspaceDocCacheEntries || len(c.items) != workspaceDocCacheEntries {
		t.Fatalf("cache holds %d entries, want %d", c.order.Len(), workspaceDocCacheEntries)
	}
	// without a stored file reference nothing is cached
	c.put("k", &types.DocumentWorkspace{ID: "d"}, []byte("x"))
	if c.get("k", workspaceDocVersion(&types.DocumentWorkspace{ID: "d"})) != nil {
		t.Fatal("a row without CurrentRef must not be cached")
	}
}
