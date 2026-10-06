package session

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

// attachmentSpyService captures the request the QA service receives.
type attachmentSpyService struct {
	*abbrevStubSessionService
	got  *types.QARequest
	done chan struct{}
}

func (s *attachmentSpyService) AgentQA(_ context.Context, req *types.QARequest, _ *event.EventBus) error {
	s.got = req
	close(s.done)
	return nil
}

func (s *attachmentSpyService) KnowledgeQA(_ context.Context, req *types.QARequest, _ *event.EventBus) error {
	s.got = req
	close(s.done)
	return nil
}

// liveRunStreams adds the live-run bookkeeping agent turns use.
type liveRunStreams struct{ *abbrevStubStreamManager }

func (liveRunStreams) GetLiveRun(context.Context, string) (string, string, error) { return "", "", nil }
func (liveRunStreams) SetLiveRun(context.Context, string, string, string) error   { return nil }
func (liveRunStreams) ClaimLiveRun(context.Context, string, string, string) error { return nil }
func (liveRunStreams) ClearLiveRun(context.Context, string, string) error         { return nil }
func (liveRunStreams) GetSteerEvents(context.Context, string, string, int) ([]interfaces.StreamEvent, int, error) {
	return nil, 0, nil
}

// storedMessages lets the resolved content be persisted onto the user message.
type storedMessages struct{ *abbrevStubMessageService }

func (storedMessages) GetMessage(_ context.Context, sessionID, id string) (*types.Message, error) {
	return &types.Message{ID: id, SessionID: sessionID, Role: "user"}, nil
}

// The request is built before the abbreviation gate, while attachment
// content is hydrated after it: the hydrated content must still reach the
// service. Regression: the agent answered "no attachment found" for an
// uploaded, parsed .docx and summarized an unrelated knowledge-base law.
func TestExecuteQA_HydratedAttachmentsReachTheService(t *testing.T) {
	for _, mode := range []qaMode{qaModeAgent, qaModeNormal} {
		h, svc, msgs, streams := newAbbrevGateHandler()
		h.streamManager = liveRunStreams{streams}
		h.messageService = storedMessages{msgs}
		spy := &attachmentSpyService{abbrevStubSessionService: svc, done: make(chan struct{})}
		h.sessionService = spy
		h.temporaryDocuments = &visionStubDocuments{
			docs: map[string]*types.TemporaryDocument{"doc-1": {
				ID: "doc-1", TenantID: 7, SessionID: "s1", FileName: "Báo cáo PV01.docx",
				FileType: ".docx", Status: types.TemporaryDocumentStatusReady,
			}},
			prompt: map[string]*types.TemporaryDocumentPromptResult{"doc-1": {
				Attachments: types.MessageAttachments{{
					ID: "doc-1", FileName: "Báo cáo PV01.docx", FileType: ".docx",
					Content: "CÔNG AN TỈNH HÀ TĨNH — Kính gửi: Phòng PV01", TokenCount: 12,
				}},
				AttachmentImageURLs: [][]string{{}},
			}},
		}
		reqCtx := newAbbrevReqCtx("Tóm tắt văn bản này")
		reqCtx.attachmentIDs = []string{"doc-1"}

		h.executeQA(reqCtx, mode, false)
		<-spy.done
		require.NotNil(t, spy.got)
		require.Len(t, spy.got.Attachments, 1, "mode %v", mode)
		require.Contains(t, spy.got.Attachments[0].Content, "Kính gửi: Phòng PV01")
	}
}
