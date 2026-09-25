package service

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

// promptImageDoc builds a ready temporary document with the given content and
// image refs for ResolveForPrompt ownership tests.
func promptImageDoc(id, fileType, content string, urls ...string) *types.TemporaryDocument {
	images := make([]types.TemporaryDocumentImage, 0, len(urls))
	for i, url := range urls {
		images = append(images, types.TemporaryDocumentImage{
			OriginalRef: fmt.Sprintf("ref-%d", i),
			URL:         url,
		})
	}
	raw, _ := json.Marshal(images)
	return &types.TemporaryDocument{
		ID:          id,
		TenantID:    7,
		SessionID:   "s1",
		ResourceRef: "local://tenant/" + id,
		FileName:    id,
		FileType:    fileType,
		FileSize:    12,
		Status:      types.TemporaryDocumentStatusReady,
		Content:     content,
		TokenCount:  10,
		ImageRefs:   types.JSON(raw),
		ExpiresAt:   time.Now().Add(time.Hour),
	}
}

func resolvePrompt(t *testing.T, docs []*types.TemporaryDocument, ids []string, query string) *types.TemporaryDocumentPromptResult {
	t.Helper()
	svc := &temporaryDocumentService{repo: &forkAccessDocRepo{docs: docs}}
	got, err := svc.ResolveForPrompt(context.Background(), 7, "s1", ids, query)
	require.NoError(t, err)
	return got
}

// TestResolveForPromptPreservesImageOwnershipPerDocument is the regression
// test for the request-wide visionForwarded bug: the result must record which
// document owns the forwarded images, so an image document stays
// vision-served while an unrelated unreadable document is still flaggable.
func TestResolveForPromptPreservesImageOwnershipPerDocument(t *testing.T) {
	docs := []*types.TemporaryDocument{
		promptImageDoc("doc-img", ".png", "![photo](local://img1)", "https://cdn/img1.png"),
		promptImageDoc("doc-pdf", ".pdf", ""),
	}

	got := resolvePrompt(t, docs, []string{"doc-img", "doc-pdf"}, "summarize the documents")

	require.Len(t, got.Attachments, 2)
	require.Equal(t, []string{"https://cdn/img1.png"}, got.ImageURLs)
	require.Len(t, got.AttachmentImageURLs, 2)
	require.Equal(t, []string{"https://cdn/img1.png"}, got.AttachmentImageURLs[0])
	require.Empty(t, got.AttachmentImageURLs[1])
}

// TestResolveForPromptOwnershipRespectsAggregateCap pins that the existing
// 4-image aggregate cap still applies and that ownership only records images
// actually forwarded: the cut-off document owns nothing.
func TestResolveForPromptOwnershipRespectsAggregateCap(t *testing.T) {
	docs := []*types.TemporaryDocument{
		promptImageDoc("doc-a", ".png", "![a](x)",
			"https://cdn/a1.png", "https://cdn/a2.png", "https://cdn/a3.png"),
		promptImageDoc("doc-b", ".png", "![b](x)",
			"https://cdn/b1.png", "https://cdn/b2.png", "https://cdn/b3.png"),
	}

	got := resolvePrompt(t, docs, []string{"doc-a", "doc-b"}, "summarize")

	require.Len(t, got.ImageURLs, 4)
	require.Len(t, got.AttachmentImageURLs, 2)
	require.Equal(t,
		[]string{"https://cdn/a1.png", "https://cdn/a2.png", "https://cdn/a3.png"},
		got.AttachmentImageURLs[0])
	require.Equal(t, []string{"https://cdn/b1.png"}, got.AttachmentImageURLs[1])
}

// TestResolveForPromptOwnershipRespectsVisualFilter pins the existing
// visual-query filter for text documents: extracted images are only owned
// when the question is visual.
func TestResolveForPromptOwnershipRespectsVisualFilter(t *testing.T) {
	docs := []*types.TemporaryDocument{
		promptImageDoc("doc-text", ".pdf", "some readable text here",
			"https://cdn/p1.png", "https://cdn/p2.png"),
	}

	plain := resolvePrompt(t, docs, []string{"doc-text"}, "summarize the refund policy")
	require.Empty(t, plain.ImageURLs)
	require.Len(t, plain.AttachmentImageURLs, 1)
	require.Empty(t, plain.AttachmentImageURLs[0])

	visual := resolvePrompt(t, docs, []string{"doc-text"}, "解释第三页的图")
	require.Equal(t, []string{"https://cdn/p1.png", "https://cdn/p2.png"}, visual.ImageURLs)
	require.Len(t, visual.AttachmentImageURLs, 1)
	require.Equal(t, []string{"https://cdn/p1.png", "https://cdn/p2.png"}, visual.AttachmentImageURLs[0])
}
