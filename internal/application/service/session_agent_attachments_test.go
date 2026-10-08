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

	kept := svc.attachmentsOutsideOpenDocuments(context.Background(), 7, "s", attachments)
	require.Len(t, kept, 1)
	require.Equal(t, "att-pdf", kept[0].ID)
	prompt := kept.BuildPrompt()
	require.NotContains(t, prompt, "Nội dung tờ trình")
	require.Contains(t, prompt, "Nội dung nguồn")
	require.Len(t, attachments, 2, "the request's attachments are left as they are")
}

func TestAgentQueryKeepsAttachmentsWithoutTabs(t *testing.T) {
	attachments := types.MessageAttachments{{ID: "att-1", FileName: "a.docx", Content: "x"}}

	// no workspace service (editor not configured)
	require.Equal(t, attachments, (&sessionService{}).attachmentsOutsideOpenDocuments(context.Background(), 7, "s", attachments))
	// a session without open documents
	noTabs := &sessionService{documentWorkspaces: &fakeDocumentWorkspaces{}}
	require.Equal(t, attachments, noTabs.attachmentsOutsideOpenDocuments(context.Background(), 7, "s", attachments))
	// a tab opened from another upload
	other := &sessionService{documentWorkspaces: &fakeDocumentWorkspaces{
		ws: &types.DocumentWorkspace{ID: "ws", AttachmentID: "att-2"},
	}}
	require.Equal(t, attachments, other.attachmentsOutsideOpenDocuments(context.Background(), 7, "s", attachments))
}
