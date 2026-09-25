package session

import (
	"context"
	"fmt"
	"testing"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

// visionStubDocuments serves canned temporary documents and prompt results so
// resolveTemporaryAttachments can be exercised without a database.
type visionStubDocuments struct {
	interfaces.TemporaryDocumentService
	docs      map[string]*types.TemporaryDocument
	prompt    map[string]*types.TemporaryDocumentPromptResult
	failBatch bool
}

func (d *visionStubDocuments) Get(_ context.Context, _ uint64, _ string, id string) (*types.TemporaryDocument, error) {
	return d.docs[id], nil
}

func (d *visionStubDocuments) ResolveForPrompt(
	_ context.Context, _ uint64, _ string, ids []string, _ string,
) (*types.TemporaryDocumentPromptResult, error) {
	if d.failBatch && len(ids) > 1 {
		return nil, fmt.Errorf("attachment %s is still being processed", ids[0])
	}
	out := &types.TemporaryDocumentPromptResult{}
	for _, id := range ids {
		res, ok := d.prompt[id]
		if !ok {
			return nil, fmt.Errorf("attachment %s was not found in this session", id)
		}
		out.Attachments = append(out.Attachments, res.Attachments...)
		out.ImageURLs = append(out.ImageURLs, res.ImageURLs...)
		out.AttachmentImageURLs = append(out.AttachmentImageURLs, res.AttachmentImageURLs...)
	}
	return out, nil
}

func visionTestDocs() (map[string]*types.TemporaryDocument, map[string]*types.TemporaryDocumentPromptResult) {
	docs := map[string]*types.TemporaryDocument{
		"doc-img": {
			ID: "doc-img", TenantID: 7, SessionID: "s1",
			FileName: "photo.png", FileType: ".png", FileSize: 10,
			Status: types.TemporaryDocumentStatusReady,
		},
		"doc-pdf": {
			ID: "doc-pdf", TenantID: 7, SessionID: "s1",
			FileName: "report.pdf", FileType: ".pdf", FileSize: 20,
			Status: types.TemporaryDocumentStatusReady,
		},
	}
	prompt := map[string]*types.TemporaryDocumentPromptResult{
		"doc-img": {
			Attachments: types.MessageAttachments{{
				ID: "doc-img", FileName: "photo.png", FileType: ".png", FileSize: 10,
				Content:    "![photo](local://img1)",
				TokenCount: 5,
			}},
			ImageURLs:           []string{"https://cdn/img1.png"},
			AttachmentImageURLs: [][]string{{"https://cdn/img1.png"}},
		},
		"doc-pdf": {
			Attachments: types.MessageAttachments{{
				ID: "doc-pdf", FileName: "report.pdf", FileType: ".pdf", FileSize: 20,
				Content:    "",
				TokenCount: 0,
			}},
			AttachmentImageURLs: [][]string{{}},
		},
	}
	return docs, prompt
}

func visionTestContexts(agent *types.CustomAgent) (*sseStreamContext, *qaRequestContext) {
	streamCtx := &sseStreamContext{
		eventBus: event.NewEventBus(),
		asyncCtx: context.Background(),
	}
	reqCtx := &qaRequestContext{
		ctx:           context.Background(),
		sessionID:     "s1",
		requestID:     "r1",
		query:         "summarize the documents",
		session:       &types.Session{ID: "s1", TenantID: 7},
		customAgent:   agent,
		attachmentIDs: []string{"doc-img", "doc-pdf"},
		attachmentMetas: types.MessageAttachments{
			{ID: "doc-img", FileName: "photo.png", FileType: ".png", FileSize: 10},
			{ID: "doc-pdf", FileName: "report.pdf", FileType: ".pdf", FileSize: 20},
		},
	}
	return streamCtx, reqCtx
}

func visionAgent(imageUpload bool) *types.CustomAgent {
	return &types.CustomAgent{
		ID:       "agent-1",
		TenantID: 7,
		Config:   types.CustomAgentConfig{ImageUploadEnabled: imageUpload},
	}
}

func attachmentByID(attachments types.MessageAttachments, id string) *types.MessageAttachment {
	for i := range attachments {
		if attachments[i].ID == id {
			return &attachments[i]
		}
	}
	return nil
}

// TestResolveTemporaryAttachmentsFlagsOnlyImagelessUnreadable is the
// regression test for the request-wide visionForwarded bug: with image upload
// enabled and one document forwarding an image, an unrelated unreadable
// document must still be flagged instead of having its failure suppressed.
func TestResolveTemporaryAttachmentsFlagsOnlyImagelessUnreadable(t *testing.T) {
	docs, prompt := visionTestDocs()
	h := &Handler{temporaryDocuments: &visionStubDocuments{docs: docs, prompt: prompt}}
	streamCtx, reqCtx := visionTestContexts(visionAgent(true))

	h.resolveTemporaryAttachments(streamCtx, reqCtx)

	require.Len(t, reqCtx.attachments, 2)
	img := attachmentByID(reqCtx.attachments, "doc-img")
	require.NotNil(t, img)
	require.Empty(t, img.ParseError, "the image document is vision-served and must not be flagged")
	pdf := attachmentByID(reqCtx.attachments, "doc-pdf")
	require.NotNil(t, pdf)
	require.NotEmpty(t, pdf.ParseError, "the unreadable document owns no images and must be flagged")
	require.Contains(t, pdf.ParseError, "no readable text")
	require.Len(t, reqCtx.images, 1)
	require.Equal(t, "https://cdn/img1.png", reqCtx.images[0].URL)
}

// TestResolveTemporaryAttachmentsWithoutVisionFlagsBoth pins the other side:
// with image upload disabled no document is vision-served, so every
// text-less attachment is flagged.
func TestResolveTemporaryAttachmentsWithoutVisionFlagsBoth(t *testing.T) {
	docs, prompt := visionTestDocs()
	h := &Handler{temporaryDocuments: &visionStubDocuments{docs: docs, prompt: prompt}}
	streamCtx, reqCtx := visionTestContexts(visionAgent(false))

	h.resolveTemporaryAttachments(streamCtx, reqCtx)

	require.Len(t, reqCtx.attachments, 2)
	for _, id := range []string{"doc-img", "doc-pdf"} {
		att := attachmentByID(reqCtx.attachments, id)
		require.NotNil(t, att)
		require.NotEmpty(t, att.ParseError, "%s has no usable text and vision is off", id)
	}
	require.Empty(t, reqCtx.images)
}

// TestResolveTemporaryAttachmentsSalvagesHealthyDocuments covers the
// per-document salvage path: when the batch resolution fails (e.g. a raced
// status change), healthy documents still resolve with their image ownership
// and only the broken one is marked unavailable.
func TestResolveTemporaryAttachmentsSalvagesHealthyDocuments(t *testing.T) {
	docs, prompt := visionTestDocs()
	delete(prompt, "doc-pdf")
	h := &Handler{temporaryDocuments: &visionStubDocuments{docs: docs, prompt: prompt, failBatch: true}}
	streamCtx, reqCtx := visionTestContexts(visionAgent(true))

	h.resolveTemporaryAttachments(streamCtx, reqCtx)

	require.Len(t, reqCtx.attachments, 2)
	img := attachmentByID(reqCtx.attachments, "doc-img")
	require.NotNil(t, img)
	require.Empty(t, img.ParseError)
	require.Equal(t, "![photo](local://img1)", img.Content)
	pdf := attachmentByID(reqCtx.attachments, "doc-pdf")
	require.NotNil(t, pdf)
	require.NotEmpty(t, pdf.ParseError, "the unresolvable document must surface as a failure entry")
	require.Len(t, reqCtx.images, 1)
	require.Equal(t, "https://cdn/img1.png", reqCtx.images[0].URL)
}

// TestResolveTemporaryAttachmentsDropsFilteredDocumentImages pins that a
// document excluded by the agent's SupportedFileTypes loses its image URLs
// too: the filtered document must not contribute attachments or vision
// images to the model.
func TestResolveTemporaryAttachmentsDropsFilteredDocumentImages(t *testing.T) {
	docs := map[string]*types.TemporaryDocument{
		"doc-a": {
			ID: "doc-a", TenantID: 7, SessionID: "s1",
			FileName: "photo.png", FileType: ".png", FileSize: 10,
			Status: types.TemporaryDocumentStatusReady,
		},
		"doc-b": {
			ID: "doc-b", TenantID: 7, SessionID: "s1",
			FileName: "scan.jpg", FileType: ".jpg", FileSize: 10,
			Status: types.TemporaryDocumentStatusReady,
		},
	}
	prompt := map[string]*types.TemporaryDocumentPromptResult{
		"doc-a": {
			Attachments: types.MessageAttachments{{
				ID: "doc-a", FileName: "photo.png", FileType: ".png", FileSize: 10,
				Content:    "![photo](local://imga)",
				TokenCount: 5,
			}},
			ImageURLs:           []string{"https://cdn/img-a.png"},
			AttachmentImageURLs: [][]string{{"https://cdn/img-a.png"}},
		},
		"doc-b": {
			Attachments: types.MessageAttachments{{
				ID: "doc-b", FileName: "scan.jpg", FileType: ".jpg", FileSize: 10,
				Content:    "![scan](local://imgb)",
				TokenCount: 5,
			}},
			ImageURLs:           []string{"https://cdn/img-b.png"},
			AttachmentImageURLs: [][]string{{"https://cdn/img-b.png"}},
		},
	}
	h := &Handler{temporaryDocuments: &visionStubDocuments{docs: docs, prompt: prompt}}
	streamCtx := &sseStreamContext{
		eventBus: event.NewEventBus(),
		asyncCtx: context.Background(),
	}
	agent := visionAgent(true)
	agent.Config.SupportedFileTypes = []string{"png"}
	reqCtx := &qaRequestContext{
		ctx:           context.Background(),
		sessionID:     "s1",
		requestID:     "r1",
		query:         "summarize the documents",
		session:       &types.Session{ID: "s1", TenantID: 7},
		customAgent:   agent,
		attachmentIDs: []string{"doc-a", "doc-b"},
		attachmentMetas: types.MessageAttachments{
			{ID: "doc-a", FileName: "photo.png", FileType: ".png", FileSize: 10},
			{ID: "doc-b", FileName: "scan.jpg", FileType: ".jpg", FileSize: 10},
		},
	}

	h.resolveTemporaryAttachments(streamCtx, reqCtx)

	require.Len(t, reqCtx.attachments, 1)
	require.Equal(t, "doc-a", reqCtx.attachments[0].ID)
	require.Len(t, reqCtx.images, 1)
	require.Equal(t, "https://cdn/img-a.png", reqCtx.images[0].URL)
	for _, img := range reqCtx.images {
		require.NotEqual(t, "https://cdn/img-b.png", img.URL,
			"the filtered-out document's image must not reach the model")
	}
}

// TestResolveTemporaryAttachmentsSalvagePreservesTotalImageCap covers the
// per-document salvage path with more than 4 images across documents: the
// TOTAL aggregate cap of 4 still applies, ownership stays aligned with the
// salvaged attachments, and only actually forwarded URLs are recorded.
func TestResolveTemporaryAttachmentsSalvagePreservesTotalImageCap(t *testing.T) {
	docs := map[string]*types.TemporaryDocument{
		"doc-a": {
			ID: "doc-a", TenantID: 7, SessionID: "s1",
			FileName: "a.png", FileType: ".png", FileSize: 10,
			Status: types.TemporaryDocumentStatusReady,
		},
		"doc-b": {
			ID: "doc-b", TenantID: 7, SessionID: "s1",
			FileName: "b.png", FileType: ".png", FileSize: 10,
			Status: types.TemporaryDocumentStatusReady,
		},
	}
	prompt := map[string]*types.TemporaryDocumentPromptResult{
		"doc-a": {
			Attachments: types.MessageAttachments{{
				ID: "doc-a", FileName: "a.png", FileType: ".png", FileSize: 10,
				Content:    "![a](local://a)",
				TokenCount: 5,
			}},
			ImageURLs: []string{
				"https://cdn/a1.png", "https://cdn/a2.png", "https://cdn/a3.png",
			},
			AttachmentImageURLs: [][]string{{
				"https://cdn/a1.png", "https://cdn/a2.png", "https://cdn/a3.png",
			}},
		},
		"doc-b": {
			Attachments: types.MessageAttachments{{
				ID: "doc-b", FileName: "b.png", FileType: ".png", FileSize: 10,
				Content:    "![b](local://b)",
				TokenCount: 5,
			}},
			ImageURLs: []string{
				"https://cdn/b1.png", "https://cdn/b2.png", "https://cdn/b3.png",
			},
			AttachmentImageURLs: [][]string{{
				"https://cdn/b1.png", "https://cdn/b2.png", "https://cdn/b3.png",
			}},
		},
	}
	h := &Handler{temporaryDocuments: &visionStubDocuments{docs: docs, prompt: prompt, failBatch: true}}
	streamCtx := &sseStreamContext{
		eventBus: event.NewEventBus(),
		asyncCtx: context.Background(),
	}
	reqCtx := &qaRequestContext{
		ctx:           context.Background(),
		sessionID:     "s1",
		requestID:     "r1",
		query:         "summarize the documents",
		session:       &types.Session{ID: "s1", TenantID: 7},
		customAgent:   visionAgent(true),
		attachmentIDs: []string{"doc-a", "doc-b"},
		attachmentMetas: types.MessageAttachments{
			{ID: "doc-a", FileName: "a.png", FileType: ".png", FileSize: 10},
			{ID: "doc-b", FileName: "b.png", FileType: ".png", FileSize: 10},
		},
	}

	h.resolveTemporaryAttachments(streamCtx, reqCtx)

	require.Len(t, reqCtx.attachments, 2)
	require.Len(t, reqCtx.images, 4, "the total image cap of 4 must survive per-document salvage")
	require.Equal(t, "https://cdn/a1.png", reqCtx.images[0].URL)
	require.Equal(t, "https://cdn/a2.png", reqCtx.images[1].URL)
	require.Equal(t, "https://cdn/a3.png", reqCtx.images[2].URL)
	require.Equal(t, "https://cdn/b1.png", reqCtx.images[3].URL)
	for _, id := range []string{"doc-a", "doc-b"} {
		att := attachmentByID(reqCtx.attachments, id)
		require.NotNil(t, att)
		require.Empty(t, att.ParseError, "%s forwards an image and must stay vision-served", id)
	}
}
