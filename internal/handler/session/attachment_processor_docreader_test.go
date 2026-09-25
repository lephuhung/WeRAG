package session

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubDocumentReader struct {
	result *types.ReadResult
	err    error
}

func (s *stubDocumentReader) Read(_ context.Context, _ *types.ReadRequest) (*types.ReadResult, error) {
	return s.result, s.err
}

func (s *stubDocumentReader) Reconnect(_ string) error { return nil }

func (s *stubDocumentReader) IsConnected() bool { return true }

func (s *stubDocumentReader) ListEngines(_ context.Context, _ map[string]string) ([]types.ParserEngineInfo, error) {
	return nil, nil
}

func TestProcessWithDocumentReaderSurfacesResultError(t *testing.T) {
	p := &AttachmentProcessor{documentReader: &stubDocumentReader{
		result: &types.ReadResult{Error: "docreader boom"},
	}}
	attachment := &types.MessageAttachment{}
	err := p.processWithDocumentReader(context.Background(), []byte("%PDF-1.4"), "doc.pdf", ".pdf", attachment, 7)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "docreader boom")
	assert.Empty(t, attachment.Content)
}

func TestProcessWithDocumentReaderNilResultIsError(t *testing.T) {
	p := &AttachmentProcessor{documentReader: &stubDocumentReader{result: nil, err: nil}}
	attachment := &types.MessageAttachment{}
	require.NotPanics(t, func() {
		err := p.processWithDocumentReader(context.Background(), []byte("%PDF-1.4"), "doc.pdf", ".pdf", attachment, 7)
		require.Error(t, err)
	})
	assert.Empty(t, attachment.Content)
}

func TestProcessWithDocumentReaderSuccessStillStored(t *testing.T) {
	p := &AttachmentProcessor{documentReader: &stubDocumentReader{
		result: &types.ReadResult{MarkdownContent: "hello"},
	}}
	attachment := &types.MessageAttachment{}
	require.NoError(t, p.processWithDocumentReader(context.Background(), []byte("%PDF-1.4"), "doc.pdf", ".pdf", attachment, 7))
	assert.Equal(t, "hello", attachment.Content)
}
