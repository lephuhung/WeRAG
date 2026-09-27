package service

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal/abbreviation"
)

func (s *sessionService) abbreviationOwner(ctx context.Context, req *types.QARequest) (types.AbbreviationOwner, error) {
	var zero types.AbbreviationOwner
	if req == nil || req.Session == nil || req.Session.ID == "" || req.UserMessageID == "" || req.AssistantMessageID == "" ||
		req.Query == "" || s.sessionRepo == nil || s.messageRepo == nil {
		return zero, types.ErrAbbreviationBadSelection
	}
	principal, ok := types.PrincipalFromContext(ctx)
	if !ok {
		return zero, types.ErrAbbreviationConflict
	}
	tenantID, ok := types.SessionTenantIDFromContext(ctx)
	if !ok || tenantID != req.Session.TenantID {
		return zero, types.ErrAbbreviationConflict
	}
	owner := types.AbbreviationOwner{TenantID: tenantID, SessionID: req.Session.ID,
		OwnerID: types.SessionOwnerIDFromContext(ctx), PrincipalID: principal.StorageID()}
	session, err := s.sessionRepo.Get(ctx, tenantID, owner.OwnerID, owner.SessionID)
	if err != nil {
		return zero, err
	}
	if session == nil || session.ID != owner.SessionID || session.TenantID != owner.TenantID || session.UserID != owner.OwnerID {
		return zero, types.ErrAbbreviationNotFound
	}
	for _, source := range []struct{ id, role string }{{req.UserMessageID, "user"}, {req.AssistantMessageID, "assistant"}} {
		message, err := s.messageRepo.GetMessage(ctx, owner.SessionID, source.id)
		if err != nil {
			return zero, err
		}
		if message == nil || message.ID != source.id || message.SessionID != owner.SessionID || message.Role != source.role {
			return zero, types.ErrAbbreviationConflict
		}
		if source.role == "user" && message.Content != req.Query {
			return zero, types.ErrAbbreviationConflict
		}
	}
	return owner, nil
}

func abbreviationSnapshot(req *types.QARequest) types.AbbreviationRequestSnapshot {
	mode := "knowledge"
	agentID := ""
	agentTenantID := req.Session.TenantID
	if req.CustomAgent != nil {
		mode, agentID, agentTenantID = "agent", req.CustomAgent.ID, req.CustomAgent.TenantID
	}
	attachments := make([]string, 0, len(req.Attachments))
	for _, item := range req.Attachments {
		attachments = append(attachments, item.ID)
	}
	return types.AbbreviationRequestSnapshot{
		AgentID: agentID, Mode: mode, AgentTenantID: agentTenantID,
		KnowledgeBaseIDs: slices.Clone(req.KnowledgeBaseIDs), KnowledgeIDs: slices.Clone(req.KnowledgeIDs),
		MCPServiceIDs: slices.Clone(req.MCPServiceIDs), SkillNames: slices.Clone(req.SkillNames),
		TagScopes: slices.Clone(req.TagScopes), AttachmentIDs: attachments,
		WebSearchEnabled: req.WebSearchEnabled, LocalBrowserEnabled: req.LocalBrowserEnabled,
	}
}

func (s *sessionService) authorizeAbbreviationResume(ctx context.Context, req *types.QARequest, saved types.AbbreviationRequestSnapshot) error {
	current := abbreviationSnapshot(req)
	if saved.AgentID != current.AgentID || saved.Mode != current.Mode || saved.AgentTenantID != current.AgentTenantID ||
		saved.WebSearchEnabled != current.WebSearchEnabled || saved.LocalBrowserEnabled != current.LocalBrowserEnabled ||
		!slices.Equal(saved.KnowledgeBaseIDs, current.KnowledgeBaseIDs) || !slices.Equal(saved.KnowledgeIDs, current.KnowledgeIDs) ||
		!slices.Equal(saved.MCPServiceIDs, current.MCPServiceIDs) || !slices.Equal(saved.SkillNames, current.SkillNames) ||
		!slices.Equal(saved.AttachmentIDs, current.AttachmentIDs) || len(saved.TagScopes) != len(current.TagScopes) {
		return types.ErrAbbreviationConflict
	}
	for i, scope := range saved.TagScopes {
		if scope.KnowledgeBaseID != current.TagScopes[i].KnowledgeBaseID || !slices.Equal(scope.TagIDs, current.TagScopes[i].TagIDs) {
			return types.ErrAbbreviationConflict
		}
	}
	if len(saved.KnowledgeBaseIDs) != 0 || len(saved.KnowledgeIDs) != 0 || len(saved.TagScopes) != 0 {
		copyReq := *req
		copyReq.TagScopes = slices.Clone(req.TagScopes)
		kbIDs, knowledgeIDs, err := s.resolveKnowledgeBases(ctx, &copyReq)
		if err != nil || !slices.Equal(kbIDs, saved.KnowledgeBaseIDs) || !slices.Equal(knowledgeIDs, saved.KnowledgeIDs) ||
			!sameAbbreviationTagScopes(copyReq.TagScopes, req.TagScopes) {
			return types.ErrAbbreviationConflict
		}
	}
	return nil
}

func sameAbbreviationTagScopes(left, right []types.TagScope) bool {
	return slices.EqualFunc(left, right, func(a, b types.TagScope) bool {
		return a.KnowledgeBaseID == b.KnowledgeBaseID && slices.Equal(a.TagIDs, b.TagIDs)
	})
}

// abbreviationEffectiveQuery returns the validated effective query carried by
// a sealed ready turn. The raw request query stays untouched so persisted
// user messages keep their original text.
func abbreviationEffectiveQuery(ctx context.Context, raw string) string {
	if r, ok := abbreviation.ResolutionFromContext(ctx); ok && r.EffectiveQuery != "" {
		return r.EffectiveQuery
	}
	return raw
}

func (s *sessionService) PrepareAbbreviationTurn(ctx context.Context, req *types.QARequest, bus *event.EventBus) (context.Context, bool, error) {
	if s.abbreviationPreparer == nil {
		return ctx, false, types.ErrAbbreviationNotReady
	}
	owner, err := s.abbreviationOwner(ctx, req)
	if err != nil {
		return ctx, false, err
	}
	binding := types.AbbreviationBinding{Owner: owner, UserMessageID: req.UserMessageID,
		AssistantMessageID: req.AssistantMessageID, RawQuery: req.Query}
	if _, present := abbreviation.ResolutionFromContext(ctx); present {
		return ctx, false, abbreviation.RequireTurn(ctx, binding)
	}
	if req.ClarificationRequestID != "" {
		if s.abbreviationStore == nil || req.ClarificationVersion == nil {
			return ctx, false, types.ErrAbbreviationConflict
		}
		saved, err := s.abbreviationStore.Get(ctx, owner, req.ClarificationRequestID)
		if err != nil {
			return ctx, false, err
		}
		if err := s.authorizeAbbreviationResume(ctx, req, saved.Payload.Snapshot); err != nil {
			return ctx, false, err
		}
	}
	input := AbbreviationPrepareInput{Binding: binding, ContinuationID: req.ClarificationRequestID,
		ExpectedVersion: req.ClarificationVersion, Snapshot: abbreviationSnapshot(req), ModelID: req.SummaryModelID}
	input.ResolveModelID = func(ctx context.Context) (string, error) {
		kbIDs, knowledgeIDs, err := s.resolveKnowledgeBases(ctx, req)
		if err != nil {
			return "", err
		}
		return s.resolveChatModelID(ctx, req, kbIDs, knowledgeIDs)
	}
	r, err := s.abbreviationPreparer.Prepare(ctx, input)
	if err != nil {
		return ctx, false, err
	}
	if bus == nil {
		return ctx, false, types.ErrAbbreviationNotReady
	}
	if err := bus.Emit(ctx, event.Event{Type: event.EventAbbreviationResolution, SessionID: owner.SessionID,
		Data: event.AbbreviationResolutionData{AbbreviationPublicState: r.PublicState(), Origin: "runtime"}}); err != nil {
		return ctx, false, err
	}
	logger.Infof(ctx, "abbreviation gate status=%s candidate_count=%d unknown_count=%d", r.Status, len(r.Terms), len(r.UnknownTerms))
	if r.Status == types.AbbreviationStatusNeedsDefinition || r.Status == types.AbbreviationStatusBlockedError {
		return ctx, true, emitAbbreviationTerminal(ctx, bus, req, r)
	}
	if r.Status != types.AbbreviationStatusReady {
		return ctx, false, types.ErrAbbreviationNotReady
	}
	bound, err := abbreviation.BindTurn(ctx, binding, r)
	return bound, false, err
}

func EmitAbbreviationClarification(ctx context.Context, bus *event.EventBus, req *types.QARequest, r types.AbbreviationResolution) error {
	if r.Status != types.AbbreviationStatusNeedsDefinition || bus == nil || req == nil || req.Session == nil {
		return types.ErrAbbreviationNotReady
	}
	return emitAbbreviationTerminal(ctx, bus, req, r)
}

func emitAbbreviationTerminal(ctx context.Context, bus *event.EventBus, req *types.QARequest, r types.AbbreviationResolution) error {
	english := types.LanguageNameFromContext(ctx) == "English"
	answer := "Vui lòng cho biết nghĩa đầy đủ của " + strings.Join(r.UnknownTerms, ", ") + ". Ví dụ định dạng: ABC = Tên đầy đủ tương ứng."
	if english {
		answer = "Please provide the full meaning of " + strings.Join(r.UnknownTerms, ", ") + ". Example format: ABC = Corresponding full name."
	}
	if r.Status == types.AbbreviationStatusBlockedError {
		answer = "Không thể phân giải từ viết tắt lúc này. Vui lòng thử lại hoặc chia nhỏ câu hỏi."
		if english {
			answer = "Abbreviation resolution is unavailable. Please retry or split the question."
		}
		if err := bus.Emit(ctx, event.Event{Type: event.EventError, SessionID: req.Session.ID,
			Data: event.ErrorData{Error: answer, Stage: "abbreviation_resolution", SessionID: req.Session.ID}}); err != nil {
			return err
		}
	}
	if err := bus.Emit(ctx, event.Event{Type: event.EventAgentFinalAnswer, SessionID: req.Session.ID,
		RequestID: r.RequestID, Data: event.AgentFinalAnswerData{Content: answer, Done: true}}); err != nil {
		return err
	}
	return bus.Emit(ctx, event.Event{Type: event.EventAgentComplete, SessionID: req.Session.ID,
		RequestID: r.RequestID, Data: event.AgentCompleteData{SessionID: req.Session.ID,
			MessageID: req.AssistantMessageID, RequestID: r.RequestID, FinalAnswer: answer}})
}

func (s *sessionService) claimAbbreviationExecution(ctx context.Context, req *types.QARequest) error {
	if s.abbreviationStore == nil {
		return types.ErrAbbreviationNotReady
	}
	owner, err := s.abbreviationOwner(ctx, req)
	if err != nil {
		return err
	}
	binding := types.AbbreviationBinding{Owner: owner, UserMessageID: req.UserMessageID,
		AssistantMessageID: req.AssistantMessageID, RawQuery: req.Query}
	if err := abbreviation.RequireTurn(ctx, binding); err != nil {
		return err
	}
	r, _ := abbreviation.ResolutionFromContext(ctx)
	row, err := s.abbreviationStore.Get(ctx, owner, r.RequestID)
	if err != nil {
		return err
	}
	if row.State != types.AbbreviationTurnReady || row.Version != r.Version || row.RootUserMessageID != r.RootUserMessageID {
		return types.ErrAbbreviationConflict
	}
	row.State = types.AbbreviationTurnRunning
	row.ExecutingMessageID = req.AssistantMessageID
	ok, err := s.abbreviationStore.CompareAndSwap(ctx, owner, row.Version, row)
	if err != nil {
		return err
	}
	if !ok {
		return types.ErrAbbreviationConflict
	}
	return nil
}

func (s *sessionService) watchAbbreviationCompletion(ctx context.Context, req *types.QARequest, bus *event.EventBus) {
	if bus == nil || s.abbreviationStore == nil {
		return
	}
	r, ok := abbreviation.ResolutionFromContext(ctx)
	if !ok {
		return
	}
	requestID := r.RequestID
	var once sync.Once
	finish := func(state string) {
		once.Do(func() {
			writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			scope, err := s.abbreviationOwner(writeCtx, req)
			if err != nil {
				return
			}
			row, err := s.abbreviationStore.Get(writeCtx, scope, requestID)
			if err != nil || row.State != types.AbbreviationTurnRunning || row.ExecutingMessageID != req.AssistantMessageID {
				return
			}
			row.State = state
			if state == types.AbbreviationTurnBlockedError {
				row.ErrorCode = "execution_interrupted"
			}
			_, _ = s.abbreviationStore.CompareAndSwap(writeCtx, scope, row.Version, row)
		})
	}
	bus.On(event.EventAgentFinalAnswer, func(_ context.Context, evt event.Event) error {
		if evt.SessionID == req.Session.ID {
			if data, ok := evt.Data.(event.AgentFinalAnswerData); ok && data.Done {
				finish(types.AbbreviationTurnCompleted)
			}
		}
		return nil
	})
	bus.On(event.EventAgentComplete, func(_ context.Context, evt event.Event) error {
		if evt.SessionID == req.Session.ID {
			finish(types.AbbreviationTurnCompleted)
		}
		return nil
	})
	if ctx.Done() != nil {
		go func() { <-ctx.Done(); finish(types.AbbreviationTurnBlockedError) }()
	}
}

func (s *sessionService) RetryAbbreviationSuggestions(ctx context.Context, sessionID, requestID string) error {
	if s.abbreviationCoordinator == nil || sessionID == "" || requestID == "" {
		return types.ErrAbbreviationNotReady
	}
	tenantID, ok := types.SessionTenantIDFromContext(ctx)
	principal, principalOK := types.PrincipalFromContext(ctx)
	if !ok || !principalOK || s.sessionRepo == nil {
		return types.ErrAbbreviationConflict
	}
	owner := types.AbbreviationOwner{TenantID: tenantID, SessionID: sessionID,
		OwnerID: types.SessionOwnerIDFromContext(ctx), PrincipalID: principal.StorageID()}
	session, err := s.sessionRepo.Get(ctx, owner.TenantID, owner.OwnerID, sessionID)
	if err != nil {
		return err
	}
	if session == nil || session.ID != sessionID || session.TenantID != owner.TenantID || session.UserID != owner.OwnerID {
		return types.ErrAbbreviationNotFound
	}
	return s.abbreviationCoordinator.RetrySuggestions(ctx, owner, requestID)
}
