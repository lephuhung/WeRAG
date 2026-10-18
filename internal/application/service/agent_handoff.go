package service

import (
	"context"
	"strings"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// agentHandoffEarlierMessages is how many earlier user messages may name the
// document type when the request itself does not ("tạo file Word giúp tôi").
const agentHandoffEarlierMessages = 10

// agentHandoff answers a web chat turn of another agent that asks for a
// Word file with the switch card (tools.AgentHandoffCard): only "Soạn thảo
// văn bản" writes .docx, and the click drafts it from this conversation.
// No model runs; true is returned. Left alone: the document assistant
// itself, a template request, a session that already holds documents (the
// Word add-in always does; a web session with one is pinned to the document
// assistant), servers without documents, and channels that cannot show the
// card (IM, MCP, embed).
func (s *sessionService) agentHandoff(ctx context.Context, req *types.QARequest, bus *event.EventBus, agentMode bool) (bool, error) {
	if req == nil || req.Session == nil || bus == nil || req.Channel != types.ChannelWeb || req.DocumentTemplate != nil {
		return false, nil
	}
	if req.CustomAgent != nil && req.CustomAgent.ID == types.BuiltinDocumentAssistantID {
		return false, nil
	}
	// a turn with a file or an image works on it ("soạn email từ file Word này")
	if len(req.Attachments) > 0 || len(req.ImageURLs) > 0 {
		return false, nil
	}
	if s.documentWorkspaces == nil || !s.documentWorkspaces.DocumentsEnabled() || !tools.WantsWordFile(req.Query) {
		return false, nil
	}
	if docs, err := s.documentWorkspaces.List(ctx, req.Session.TenantID, req.Session.ID); err != nil || len(docs) > 0 {
		return false, nil
	}
	card := &tools.AgentHandoffCard{TemplateID: tools.HandoffTemplateID(req.Query, s.earlierUserMessages(ctx, req))}
	logger.Infof(ctx, "Agent hand-off: a Word file asked of %s, template=%q", agentIDOf(req), card.TemplateID)
	if agentMode {
		return true, emitPipelineCard(ctx, bus, req, tools.AgentHandoffType, "handoff-", card.Message(), card.Data())
	}
	return true, emitPipelineCardNormal(ctx, bus, req, tools.AgentHandoffType, "handoff-", card.Message(), card.Data())
}

// earlierUserMessages returns the session's earlier user messages, latest
// first (not the turn's own).
func (s *sessionService) earlierUserMessages(ctx context.Context, req *types.QARequest) []string {
	if s.messageRepo == nil {
		return nil
	}
	msgs, err := s.messageRepo.GetRecentMessagesBySession(ctx, req.Session.ID, agentHandoffEarlierMessages*2+2)
	if err != nil {
		logger.Warnf(ctx, "Agent hand-off: history of %s: %v", req.Session.ID, err)
		return nil
	}
	var out []string
	for i := len(msgs) - 1; i >= 0 && len(out) < agentHandoffEarlierMessages; i-- {
		m := msgs[i]
		if m == nil || m.Role != "user" || m.ID == req.UserMessageID || strings.TrimSpace(m.Content) == "" {
			continue
		}
		out = append(out, m.Content)
	}
	return out
}

func agentIDOf(req *types.QARequest) string {
	if req.CustomAgent == nil {
		return "(no agent)"
	}
	return req.CustomAgent.ID
}

// The conversation a hand-off drafts from: the latest messages within a
// budget, each cut to a length that keeps a whole drafted plan or report
// (its head, where the body starts); a long chat keeps its end, where the
// content was settled.
const (
	templateConversationMessages = 40
	templateConversationMsgRunes = 12000
	templateConversationRunes    = 16000
)

// templateConversation returns the session's conversation before this turn,
// oldest first: what the user and the other agent wrote, without the cards
// the gates answered with (the hand-off card itself, choice cards) and
// without template-flow turns.
func (s *sessionService) templateConversation(ctx context.Context, req *types.QARequest) []tools.TemplateTurn {
	if s.messageRepo == nil || req.Session == nil {
		return nil
	}
	msgs, err := s.messageRepo.GetRecentMessagesBySession(ctx, req.Session.ID, templateConversationMessages)
	if err != nil {
		logger.Warnf(ctx, "document template: conversation of %s: %v", req.Session.ID, err)
		return nil
	}
	var out []tools.TemplateTurn
	budget := templateConversationRunes
	for i := len(msgs) - 1; i >= 0 && budget > 0; i-- {
		m := msgs[i]
		if m == nil || m.ID == req.UserMessageID || m.ID == req.AssistantMessageID ||
			(m.Role != "user" && m.Role != "assistant") || choiceCardHistoryNote(m) != "" || templateFlowMessage(m) {
			continue
		}
		text := strings.TrimSpace(agentHistoryThinkTagRegex.ReplaceAllString(m.Content, ""))
		if text == "" {
			continue
		}
		r := []rune(text)
		if len(r) > templateConversationMsgRunes {
			r = r[:templateConversationMsgRunes]
		}
		if len(r) > budget {
			r = r[len(r)-budget:]
		}
		budget -= len(r)
		out = append(out, tools.TemplateTurn{Role: m.Role, Text: string(r)})
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// templateFlowMessage tells whether an assistant message is a turn of the
// template flow (its questions are not content to draft from).
func templateFlowMessage(m *types.Message) bool {
	for _, step := range m.AgentSteps {
		for _, tc := range step.ToolCalls {
			if tc.Name == types.DocumentTemplateToolName {
				return true
			}
		}
	}
	return false
}
