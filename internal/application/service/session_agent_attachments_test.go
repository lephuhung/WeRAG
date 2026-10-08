package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestAgentQueryDropsAttachmentsOpenAsTabs(t *testing.T) {
	attachments := types.MessageAttachments{
		{ID: "att-tab", FileName: "to-trinh.docx", FileType: ".docx", Content: "Nội dung tờ trình"},
		{ID: "att-pdf", FileName: "nguon.pdf", FileType: ".pdf", Content: "Nội dung nguồn"},
	}
	svc := &sessionService{documentWorkspaces: &fakeDocumentWorkspaces{
		ws: &types.DocumentWorkspace{ID: "ws", SessionID: "s", FileName: "to-trinh.docx", AttachmentID: "att-tab"},
	}}

	kept, openDocs, _ := svc.attachmentsOutsideOpenDocuments(context.Background(), 7, "s", attachments)
	require.Len(t, kept, 1)
	require.Equal(t, "att-pdf", kept[0].ID)
	require.Equal(t, []string{"ws"}, openDocs)
	prompt := kept.BuildPrompt()
	require.NotContains(t, prompt, "Nội dung tờ trình")
	require.Contains(t, prompt, "Nội dung nguồn")
	require.Len(t, attachments, 2, "the request's attachments are left as they are")
}

// An attached file that is open as a tab counts as named, next to the
// documents the user @-named: its text comes from the tab, and an edit may
// target it.
func TestAttachedOpenTabJoinsTheNamedDocuments(t *testing.T) {
	attachments := types.MessageAttachments{
		{ID: "att-a", FileName: "a.docx", FileType: ".docx", Content: "Nội dung văn bản A"},
	}
	svc := &sessionService{documentWorkspaces: &fakeDocumentWorkspaces{
		ws: &types.DocumentWorkspace{ID: "ws-a", SessionID: "s", FileName: "a.docx", AttachmentID: "att-a", Position: 1},
	}}
	kept, openDocs, _ := svc.attachmentsOutsideOpenDocuments(context.Background(), 7, "s", attachments)
	require.NotContains(t, kept.BuildPrompt(), "Nội dung văn bản A")

	mentioned := mergeDocumentIDs([]string{"ws-b", "ws-a"}, openDocs)
	require.Equal(t, []string{"ws-b", "ws-a"}, mentioned, "no duplicate when the tab was also @-named")
	mentioned = mergeDocumentIDs([]string{"ws-b"}, openDocs)
	ctx := types.WithMentionedDocuments(context.Background(), mentioned)
	require.Equal(t, []string{"ws-b", "ws-a"}, types.MentionedDocumentsFromContext(ctx))
}

func TestAgentQueryKeepsAttachmentsWithoutTabs(t *testing.T) {
	attachments := types.MessageAttachments{{ID: "att-1", FileName: "a.docx", Content: "x"}}
	check := func(svc *sessionService) {
		t.Helper()
		kept, openDocs, _ := svc.attachmentsOutsideOpenDocuments(context.Background(), 7, "s", attachments)
		require.Equal(t, attachments, kept)
		require.Empty(t, openDocs)
	}
	// no workspace service (editor not configured)
	check(&sessionService{})
	// a session without open documents
	check(&sessionService{documentWorkspaces: &fakeDocumentWorkspaces{}})
	// a tab opened from another upload
	check(&sessionService{documentWorkspaces: &fakeDocumentWorkspaces{
		ws: &types.DocumentWorkspace{ID: "ws", AttachmentID: "att-2"},
	}})
	require.Nil(t, mergeDocumentIDs(nil, nil))
}

// An upload recorded as a source keeps its attachment text for the turn it
// is attached to, and is neither named nor dropped.
func TestAttachedSourceKeepsItsTextAndIsNotNamed(t *testing.T) {
	attachments := types.MessageAttachments{
		{ID: "att-src", FileName: "so-lieu.pdf", FileType: ".pdf", Content: "Số liệu nguồn 2025"},
	}
	svc := &sessionService{documentWorkspaces: &fakeDocumentWorkspaces{
		ws: &types.DocumentWorkspace{ID: "ws-src", SessionID: "s", FileName: "so-lieu.pdf", AttachmentID: "att-src",
			Position: 3, Role: types.DocumentWorkspaceRoleSource},
	}}
	kept, openDocs, sources := svc.attachmentsOutsideOpenDocuments(context.Background(), 7, "s", attachments)
	require.Equal(t, attachments, kept)
	require.Contains(t, kept.BuildPrompt(), "Số liệu nguồn 2025")
	require.Empty(t, openDocs, "a source is not a named document")
	require.Equal(t, []string{"ws-src"}, sources)
}
