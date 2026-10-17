package service

import (
	"context"
	"regexp"
	"strings"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// The Word add-in's chat-driven document templates: a turn carrying a
// document_template_request is answered here, before any gate or agent, with
// the prepare_document_template result (tools.PrepareDocumentTemplate): no
// document ops, no other tool. The workspace's agency profile gives the
// header; the user's drafting unit and position come from memory, asked in
// the chat when memory has none and remembered once the user says them.

// Memory topics of the drafting context: a statement kept under the same
// topic replaces the previous one.
const (
	templateUnitTopic     = "Phòng ban soạn thảo văn bản"
	templatePositionTopic = "Chức vụ công tác"
)

var (
	templateUnitRe     = regexp.MustCompile(`:\s*([^;]+?)\s*(?:;\s*viết tắt:\s*(\S+?)\.?)?\s*$`)
	templatePositionRe = regexp.MustCompile(`:\s*(.+?)\.?\s*$`)
)

// memoryTopicReader reads a statement kept under a fixed topic
// (memory.Service.TopicItem).
type memoryTopicReader interface {
	TopicItem(ctx context.Context, topic string) *types.MemoryItem
}

// SetAgencyProfiles gives the template flow the workspaces' agency profiles.
func (s *sessionService) SetAgencyProfiles(repo interfaces.TenantAgencyProfileRepository) {
	s.agencyProfiles = repo
}

// SetTemplateStyles gives the template flow the workspaces' font sizes.
func (s *sessionService) SetTemplateStyles(repo interfaces.TenantTemplateStyleRepository) {
	s.templateStyles = repo
}

// templateFontSizes resolves the font sizes a ready template is built with.
func (s *sessionService) templateFontSizes(ctx context.Context, tenantID uint64) types.TemplateFontSizes {
	tenant := types.TemplateFontSizes{}
	if s.templateStyles != nil {
		st, err := s.templateStyles.Get(ctx, tenantID)
		if err != nil {
			logger.Warnf(ctx, "document template: font sizes of tenant %d: %v", tenantID, err)
		} else if st != nil {
			if f, err := types.ParseTemplateFontSizes(st.FontSizes); err == nil {
				tenant = f
			}
		}
	}
	return types.ResolveTemplateFontSizes(types.PlatformTemplateFontSizes(ctx, s.systemSettings), tenant)
}

// documentTemplateTurn answers a template request turn; false when the turn
// carries none. agentMode says which stream the turn runs on (see
// broadQueryClarification).
func (s *sessionService) documentTemplateTurn(ctx context.Context, req *types.QARequest, bus *event.EventBus, agentMode bool) (bool, error) {
	tr := req.DocumentTemplate.Normalized()
	if tr == nil || req.Session == nil || bus == nil {
		return false, nil
	}
	answer := func(text string) error {
		return emitTemplateAnswer(ctx, bus, req, agentMode, text, nil)
	}
	if s.documentWorkspaces == nil || !s.documentWorkspaces.DocumentsEnabled() {
		return true, answer("Máy chủ chưa bật soạn thảo tài liệu nên chưa tạo được văn bản mới.")
	}
	tenantID, sessionID := req.Session.TenantID, req.Session.ID
	// the document must be one of this session's, in this workspace: the
	// IDs the client sent are not trusted alone
	if ws, err := s.documentWorkspaces.Get(ctx, tenantID, sessionID, tr.DocumentID); err != nil || ws == nil {
		logger.Warnf(ctx, "document template: document %s is not in session %s: %v", tr.DocumentID, sessionID, err)
		return true, answer("Không tìm thấy tài liệu Word này trong phiên trò chuyện; hãy mở lại tài liệu rồi thử lại.")
	}
	state := tools.LoadDocumentTemplateState(ctx, sessionID, tr.RequestID)
	if state == nil || state.TemplateID != tr.TemplateID || state.DocumentID != tr.DocumentID {
		state = &tools.DocumentTemplateState{TemplateID: tr.TemplateID, DocumentID: tr.DocumentID}
	}
	state.Inputs = append(state.Inputs, strings.TrimSpace(req.Query))

	var profile *types.TenantAgencyProfile
	if s.agencyProfiles != nil {
		p, err := s.agencyProfiles.Get(ctx, tenantID)
		if err != nil {
			logger.Warnf(ctx, "document template: agency profile of tenant %d: %v", tenantID, err)
		}
		profile = p
	}
	modelID, err := s.resolveChatModelID(ctx, req, nil, nil)
	if err != nil || modelID == "" {
		logger.Warnf(ctx, "document template: no chat model: %v", err)
		return true, answer("Chưa cấu hình mô hình trả lời nên chưa soạn được văn bản.")
	}
	model, err := s.modelService.GetChatModel(ctx, modelID)
	if err != nil {
		logger.Warnf(ctx, "document template: chat model %s: %v", modelID, err)
		return true, answer("Chưa soạn được văn bản lúc này; bạn gửi lại yêu cầu giúp.")
	}
	in := tools.DocumentTemplateInput{
		Request: tr, Profile: profile, User: s.templateUser(ctx), Inputs: state.Inputs,
		Today: types.PromptNow(), Model: model, Asked: state.Asked,
	}
	// the user's work context (profile preferences)
	if user, _ := ctx.Value(types.UserContextKey).(*types.User); user != nil {
		if u := user.Preferences.WorkUnit; u != nil {
			in.WorkUnit = strings.TrimSpace(*u)
		}
		if id := user.Preferences.ProvinceID; id != nil {
			if p, ok := types.ProvinceByID(*id); ok {
				in.Province = p.Short
			}
		}
	}
	out, err := tools.PrepareDocumentTemplate(ctx, in)
	if err != nil {
		logger.Warnf(ctx, "document template: %v", err)
		// the message stays part of the request: the next try reads it
		tools.SaveDocumentTemplateState(ctx, sessionID, tr.RequestID, state)
		return true, answer("Chưa soạn được văn bản lúc này (mô hình không trả lời đúng định dạng); bạn gửi lại giúp.")
	}
	state.Asked = out.Asked
	tools.SaveDocumentTemplateState(ctx, sessionID, tr.RequestID, state)
	if out.Learned != nil {
		s.rememberTemplateUser(ctx, req, *out.Learned)
	}
	logger.Infof(ctx, "document template %s (%s): ready=%v", tr.RequestID, tr.TemplateID, out.Ready)
	if out.Ready && out.Data != nil {
		out.Data["font_sizes"] = s.templateFontSizes(ctx, tenantID)
	}
	return true, emitTemplateAnswer(ctx, bus, req, agentMode, out.Answer, out.Data)
}

// emitTemplateAnswer ends the turn with the answer and, when there is one,
// the structured result.
func emitTemplateAnswer(ctx context.Context, bus *event.EventBus, req *types.QARequest, agentMode bool, answer string, data map[string]interface{}) error {
	if data == nil {
		// no result: an answer alone, with no tool card
		sessionID := req.Session.ID
		if err := bus.Emit(ctx, event.Event{Type: event.EventAgentFinalAnswer, SessionID: sessionID,
			Data: event.AgentFinalAnswerData{Content: answer, Done: true}}); err != nil {
			return err
		}
		if !agentMode {
			return nil
		}
		return bus.Emit(ctx, event.Event{Type: event.EventAgentComplete, SessionID: sessionID,
			Data: event.AgentCompleteData{SessionID: sessionID, MessageID: req.AssistantMessageID, FinalAnswer: answer}})
	}
	if agentMode {
		return emitPipelineCard(ctx, bus, req, types.DocumentTemplateToolName, "doctemplate-", answer, data)
	}
	return emitPipelineCardNormal(ctx, bus, req, types.DocumentTemplateToolName, "doctemplate-", answer, data)
}

// templateUser reads the drafting context kept in memory.
func (s *sessionService) templateUser(ctx context.Context) tools.TemplateUserContext {
	var u tools.TemplateUserContext
	reader, ok := s.memoryService.(memoryTopicReader)
	if !ok || s.memoryService == nil {
		return u
	}
	if it := reader.TopicItem(ctx, templateUnitTopic); it != nil {
		if m := templateUnitRe.FindStringSubmatch(it.Content); m != nil {
			u.Unit, u.UnitAbbr = strings.TrimSpace(m[1]), strings.TrimSpace(m[2])
		}
	}
	if it := reader.TopicItem(ctx, templatePositionTopic); it != nil {
		if m := templatePositionRe.FindStringSubmatch(it.Content); m != nil {
			u.Position = strings.TrimSpace(m[1])
		}
	}
	return u
}

// rememberTemplateUser keeps what the user said of their drafting unit and
// position (memory off: nothing is kept, and the next request asks again).
func (s *sessionService) rememberTemplateUser(ctx context.Context, req *types.QARequest, u tools.TemplateUserContext) {
	if s.memoryService == nil {
		return
	}
	remember := func(topic, content string) {
		if _, err := s.memoryService.Remember(ctx, types.MemoryItem{
			Kind: types.MemoryKindProfile, Topic: topic, Content: content, Importance: 4,
			Origin: types.MemoryOriginExplicit, SourceSessionID: req.Session.ID,
		}); err != nil {
			logger.Infof(ctx, "document template: not remembered (%s): %v", topic, err)
		}
	}
	if u.Unit != "" && u.UnitAbbr != "" {
		remember(templateUnitTopic, "Phòng/bộ phận soạn thảo văn bản: "+u.Unit+"; viết tắt: "+u.UnitAbbr)
	}
	if u.Position != "" {
		remember(templatePositionTopic, "Chức vụ: "+u.Position)
	}
}
