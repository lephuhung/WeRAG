package session

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// abbrevStubSessionService records PrepareAbbreviationTurn calls and lets a
// test decide the outcome; QA entries panic if reached — a paused or failed
// gate must never fall through to semantic processing.
type abbrevStubSessionService struct {
	interfaces.SessionService
	prepareFn     func(ctx context.Context, req *types.QARequest, bus *event.EventBus) (context.Context, bool, error)
	retryFn       func(ctx context.Context, sessionID, requestID string) error
	prepareCalls  int
	knowledgeHits int
	agentHits     int
	titleHits     int
	mu            sync.Mutex
}

func (s *abbrevStubSessionService) PrepareAbbreviationTurn(
	ctx context.Context, req *types.QARequest, bus *event.EventBus,
) (context.Context, bool, error) {
	s.mu.Lock()
	s.prepareCalls++
	s.mu.Unlock()
	if s.prepareFn != nil {
		return s.prepareFn(ctx, req, bus)
	}
	return ctx, false, nil
}

func (s *abbrevStubSessionService) RetryAbbreviationSuggestions(
	ctx context.Context, sessionID, requestID string,
) error {
	if s.retryFn != nil {
		return s.retryFn(ctx, sessionID, requestID)
	}
	return nil
}

func (s *abbrevStubSessionService) KnowledgeQA(
	ctx context.Context, req *types.QARequest, bus *event.EventBus,
) error {
	s.mu.Lock()
	s.knowledgeHits++
	s.mu.Unlock()
	return nil
}

func (s *abbrevStubSessionService) AgentQA(
	ctx context.Context, req *types.QARequest, bus *event.EventBus,
) error {
	s.mu.Lock()
	s.agentHits++
	s.mu.Unlock()
	return nil
}

func (s *abbrevStubSessionService) GenerateTitleAsync(
	ctx context.Context, session *types.Session, userQuery, modelID string, bus *event.EventBus,
) {
	s.mu.Lock()
	s.titleHits++
	s.mu.Unlock()
}

func (s *abbrevStubSessionService) UpdateSessionLastRequestState(
	ctx context.Context, sessionID string, state *types.SessionLastRequestState,
) error {
	return nil
}

func (s *abbrevStubSessionService) UpdateSession(ctx context.Context, session *types.Session) error {
	return nil
}

type abbrevStubMessageService struct {
	interfaces.MessageService
	created []*types.Message
	mu      sync.Mutex
}

func (m *abbrevStubMessageService) CreateMessage(ctx context.Context, msg *types.Message) (*types.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if msg.ID == "" {
		msg.ID = uuid.NewString()
	}
	m.created = append(m.created, msg)
	return msg, nil
}

func (m *abbrevStubMessageService) UpdateMessage(ctx context.Context, msg *types.Message) error {
	return nil
}

func (m *abbrevStubMessageService) IndexMessageToKB(ctx context.Context, query, content, messageID, sessionID string) {
}

type abbrevStubStreamManager struct {
	interfaces.StreamManager
	events []interfaces.StreamEvent
	mu     sync.Mutex
}

func (sm *abbrevStubStreamManager) AppendEvent(
	ctx context.Context, sessionID, messageID string, evt interfaces.StreamEvent,
) error {
	sm.mu.Lock()
	sm.events = append(sm.events, evt)
	sm.mu.Unlock()
	return nil
}

func (sm *abbrevStubStreamManager) GetEvents(
	ctx context.Context, sessionID, messageID string, offset int,
) ([]interfaces.StreamEvent, int, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if offset >= len(sm.events) {
		return nil, offset, nil
	}
	return append([]interfaces.StreamEvent(nil), sm.events[offset:]...), len(sm.events), nil
}

func (sm *abbrevStubStreamManager) appendedTypes() []types.ResponseType {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	out := make([]types.ResponseType, 0, len(sm.events))
	for _, e := range sm.events {
		out = append(out, e.Type)
	}
	return out
}

type abbrevFailDocuments struct {
	interfaces.TemporaryDocumentService
	called bool
}

func (d *abbrevFailDocuments) ResolveForPrompt(
	ctx context.Context, tenantID uint64, sessionID string, ids []string, _ string,
) (*types.TemporaryDocumentPromptResult, error) {
	d.called = true
	return nil, assert.AnError
}

func newAbbrevGateHandler() (*Handler, *abbrevStubSessionService, *abbrevStubMessageService, *abbrevStubStreamManager) {
	svc := &abbrevStubSessionService{}
	msgs := &abbrevStubMessageService{}
	streams := &abbrevStubStreamManager{}
	return &Handler{
		sessionService:     svc,
		messageService:     msgs,
		streamManager:      streams,
		modelService:       nil,
		temporaryDocuments: &abbrevFailDocuments{},
	}, svc, msgs, streams
}

func newAbbrevReqCtx(query string) *qaRequestContext {
	return &qaRequestContext{
		ctx:        context.Background(),
		sessionID:  "s1",
		requestID:  "req-http",
		receivedAt: time.Now(),
		query:      query,
		session:    &types.Session{ID: "s1", TenantID: 7, Title: "existing"},
		assistantMessage: &types.Message{
			SessionID: "s1", Role: "assistant", RequestID: "req-http",
		},
		skipSSE: true,
	}
}

func TestExecuteQA_AbbreviationPauseSkipsSemantic(t *testing.T) {
	h, svc, msgs, streams := newAbbrevGateHandler()
	reqCtx := newAbbrevReqCtx("ATTT có yêu cầu gì")
	reqCtx.attachmentIDs = []string{"doc-1"}

	svc.prepareFn = func(ctx context.Context, req *types.QARequest, bus *event.EventBus) (context.Context, bool, error) {
		// Mimic the real coordinator: emit the resolution event then the
		// terminal pair that completes the assistant turn.
		_ = bus.Emit(ctx, event.Event{Type: event.EventAbbreviationResolution, SessionID: "s1",
			Data: event.AbbreviationResolutionData{
				AbbreviationPublicState: types.AbbreviationPublicState{
					RequestID: "req-turn", Status: types.AbbreviationStatusNeedsDefinition,
					UnknownTerms: []string{"ATTT"},
				}, Origin: "runtime",
			}})
		_ = bus.Emit(ctx, event.Event{Type: event.EventAgentFinalAnswer, SessionID: "s1",
			Data: event.AgentFinalAnswerData{Content: "nghĩa đầy đủ?", Done: true}})
		_ = bus.Emit(ctx, event.Event{Type: event.EventAgentComplete, SessionID: "s1",
			Data: event.AgentCompleteData{MessageID: req.AssistantMessageID}})
		return ctx, true, nil
	}

	h.executeQA(reqCtx, qaModeNormal, false)

	assert.Equal(t, 1, svc.prepareCalls)
	assert.Zero(t, svc.knowledgeHits)
	assert.Zero(t, svc.agentHits)
	assert.Zero(t, svc.titleHits)
	assert.False(t, h.temporaryDocuments.(*abbrevFailDocuments).called, "attachment content must not resolve while gated")

	types_ := streams.appendedTypes()
	assert.Contains(t, types_, types.ResponseTypeAbbreviationResolution)
	assert.Equal(t, 1, countType(types_, types.ResponseTypeComplete),
		"exactly one terminal event: %v", types_)

	// The clarification answer is persisted onto the assistant row.
	require.NotEmpty(t, msgs.created)
}

func TestExecuteQA_AbbreviationPrepareErrorCompletesOnce(t *testing.T) {
	h, svc, _, streams := newAbbrevGateHandler()
	reqCtx := newAbbrevReqCtx("ATTT")

	svc.prepareFn = func(ctx context.Context, req *types.QARequest, bus *event.EventBus) (context.Context, bool, error) {
		return ctx, false, errors.NewInternalServerError("store down")
	}

	h.executeQA(reqCtx, qaModeNormal, false)

	assert.Zero(t, svc.knowledgeHits)
	types_ := streams.appendedTypes()
	assert.Equal(t, 1, countType(types_, types.ResponseTypeError))
	assert.Equal(t, 1, countType(types_, types.ResponseTypeComplete))
}

func TestExecuteQA_AbbreviationReadyPropagatesSealedContext(t *testing.T) {
	h, svc, _, _ := newAbbrevGateHandler()
	reqCtx := newAbbrevReqCtx("ATTT là gì")

	type markerKey struct{}
	sealed := context.WithValue(context.Background(), markerKey{}, "sealed")
	svc.prepareFn = func(ctx context.Context, req *types.QARequest, bus *event.EventBus) (context.Context, bool, error) {
		return sealed, false, nil
	}
	var sawSealed bool
	done := make(chan struct{})
	svcSpy := &sealedSpyService{abbrevStubSessionService: svc}
	h.sessionService = svcSpy
	svcSpy.onKnowledge = func(ctx context.Context) { sawSealed = ctx.Value(markerKey{}) == "sealed"; close(done) }

	h.executeQA(reqCtx, qaModeNormal, false)
	<-done
	assert.True(t, sawSealed, "service entry must receive the gate-bound context")
	assert.Equal(t, 1, svcSpy.knowledgeHits)
}

// sealedSpyService forwards to the stub while capturing the KnowledgeQA ctx.
type sealedSpyService struct {
	*abbrevStubSessionService
	onKnowledge func(ctx context.Context)
}

func (s *sealedSpyService) KnowledgeQA(ctx context.Context, req *types.QARequest, bus *event.EventBus) error {
	s.knowledgeHits++
	if s.onKnowledge != nil {
		s.onKnowledge(ctx)
	}
	return nil
}

func TestBuildQARequestCarriesClarificationFields(t *testing.T) {
	version := uint64(4)
	rc := newAbbrevReqCtx("ATTT = An toàn thông tin")
	rc.clarificationRequestID = "req-turn"
	rc.clarificationVersion = &version
	req := rc.buildQARequest()
	assert.Equal(t, "req-turn", req.ClarificationRequestID)
	require.NotNil(t, req.ClarificationVersion)
	assert.Equal(t, uint64(4), *req.ClarificationVersion)
}

func TestRetryAbbreviationSuggestionsEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name     string
		svcErr   error
		wantCode int
	}{
		{"success", nil, http.StatusOK},
		{"not found", types.ErrAbbreviationNotFound, http.StatusNotFound},
		{"conflict", types.ErrAbbreviationConflict, http.StatusConflict},
		{"internal", errors.NewInternalServerError("db"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, svc, _, _ := newAbbrevGateHandler()
			svc.retryFn = func(ctx context.Context, sessionID, requestID string) error {
				assert.Equal(t, "s1", sessionID)
				assert.Equal(t, "req-1", requestID)
				return tc.svcErr
			}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
			c.Params = gin.Params{
				{Key: "session_id", Value: "s1"},
				{Key: "request_id", Value: "req-1"},
			}
			h.RetryAbbreviationSuggestions(c)
			if tc.svcErr == nil {
				assert.Equal(t, http.StatusOK, rec.Code)
				return
			}
			require.NotEmpty(t, c.Errors)
			appErr, ok := errors.IsAppError(c.Errors.Last().Err)
			require.True(t, ok)
			assert.Equal(t, tc.wantCode, appErr.HTTPCode)
		})
	}
}

// Forged abbreviation fields in the request body must not decode into any
// state the gate honours — the only accepted inputs are the continuation
// pointer (request id + version) that the backend re-validates against the
// stored turn.
func TestRequestBodyCannotForgeAbbreviationState(t *testing.T) {
	body := []byte(`{
		"query": "ATTT là gì",
		"abbreviation_candidates": ["ATTT"],
		"abbreviation_ready": true,
		"abbreviation": {"status": "ready", "request_id": "forged"},
		"abbreviation_resolution": {"status": "ready"},
		"clarification_request_id": "",
		"clarification_version": null
	}`)
	var req CreateKnowledgeQARequest
	require.NoError(t, json.Unmarshal(body, &req))
	assert.Empty(t, req.ClarificationRequestID)
	assert.Nil(t, req.ClarificationVersion)
	// The struct exposes no field that could carry a forged resolution:
	// reflection confirms no decoded field stores the attacker's status.
	v := reflect.ValueOf(req)
	for i := 0; i < v.NumField(); i++ {
		assert.NotContains(t,
			strings.ToLower(v.Type().Field(i).Name), "abbreviation",
			"decoded request still carries attacker-controlled field %s",
			v.Type().Field(i).Name)
	}
}

func countType(types_ []types.ResponseType, want types.ResponseType) int {
	n := 0
	for _, tp := range types_ {
		if tp == want {
			n++
		}
	}
	return n
}
