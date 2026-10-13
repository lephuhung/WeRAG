package agent

import (
	"context"
	"strings"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// The wiki is a support tool: its pages are generated summaries and index
// listings, not the authoritative text. When a turn drew on wiki pages, the
// answer ends with a short reminder to check the source document. This is
// code-appended, like the amendment history, so it does not depend on the
// model following a prompt rule.

// wikiReadTools are the tools whose successful results put wiki content in
// front of the model. Write/maintenance tools do not count.
var wikiReadTools = map[string]bool{
	"wiki_search":    true,
	"wiki_read_page": true,
}

// usedWikiPages reports whether any successful tool call in the turn read
// wiki pages.
func usedWikiPages(steps []types.AgentStep) bool {
	for _, step := range steps {
		for _, call := range step.ToolCalls {
			if call.Result == nil || !call.Result.Success {
				continue
			}
			if wikiReadTools[call.Name] {
				return true
			}
		}
	}
	return false
}

// wikiCaveatBlock is the reminder appended to an answer that used the wiki.
func wikiCaveatBlock(locale string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(locale)), "vi") {
		return "\n\n---\n_Lưu ý: một phần câu trả lời dựa trên trang wiki tổng hợp tự động, chỉ mang tính hỗ trợ tra cứu. Vui lòng đối chiếu với văn bản gốc trước khi áp dụng._\n"
	}
	return "\n\n---\n_Note: part of this answer draws on automatically generated wiki pages, which are a lookup aid only. Please verify against the source document before relying on it._\n"
}

// appendWikiCaveat streams the reminder after the answer, on the answer's
// own event so the stored message carries it too.
func (e *AgentEngine) appendWikiCaveat(ctx context.Context, state *types.AgentState, sessionID, answerID string) {
	if e.eventBus == nil || answerID == "" || state == nil || state.FinalAnswer == "" {
		return
	}
	if !usedWikiPages(state.RoundSteps) {
		return
	}
	block := wikiCaveatBlock(types.LanguageFromContextOrDefault(ctx))
	logger.Infof(ctx, "[Agent] Appending wiki caveat")
	_ = e.eventBus.Emit(ctx, event.Event{
		ID:        answerID,
		Type:      event.EventAgentFinalAnswer,
		SessionID: sessionID,
		Data:      event.AgentFinalAnswerData{Content: block},
	})
	state.FinalAnswer += block
}
