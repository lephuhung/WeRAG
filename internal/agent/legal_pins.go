package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

// legalPinNote tells the model which document a bare reference of the
// question stands for, as the user picked it earlier in the conversation
// (types.LegalReferencePin); "" when the turn has no pin.
func legalPinNote(pins []types.LegalReferencePin) string {
	if len(pins) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("[Ghi chú của hệ thống cho lượt này — không nhắc lại với người dùng]")
	for _, p := range pins {
		doc := strings.TrimSpace(p.DocType + " " + p.DocumentNumber)
		if t := strings.TrimSpace(p.Title); t != "" {
			doc += " (" + t + ")"
		}
		fmt.Fprintf(&b, "\nỞ câu trước trong cuộc trò chuyện này, người dùng đã chọn “%s” là %s. Hiểu “%s” trong câu "+
			"hỏi là văn bản đó, tra cứu theo số hiệu đầy đủ và nêu rõ số hiệu này trong câu trả lời.",
			p.Reference, doc, p.Reference)
	}
	return b.String()
}

// applyLegalPins adds the pin note to the current user message (the last
// message), only in this turn's message slice, like the scope brief.
func (e *AgentEngine) applyLegalPins(ctx context.Context, messages []chat.Message) {
	note := legalPinNote(types.LegalReferencePinsFromContext(ctx))
	if note == "" {
		return
	}
	if appendTurnNote(messages, note) {
		logger.Infof(ctx, "[Agent] Legal reference pin added (%d chars)", len(note))
	}
}

// appendTurnNote adds a per-turn system note to the current user message
// (the last message): to its text, or to its last text part when the
// message is multi-part (the note is never dropped for an image turn).
// false when the last message is not the user's.
func appendTurnNote(messages []chat.Message, note string) bool {
	if len(messages) == 0 || note == "" {
		return false
	}
	last := &messages[len(messages)-1]
	if last.Role != "user" {
		return false
	}
	if len(last.MultiContent) == 0 {
		last.Content += "\n\n" + note
		return true
	}
	parts := append([]chat.MessageContentPart(nil), last.MultiContent...)
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i].Type == "text" {
			parts[i].Text += "\n\n" + note
			last.MultiContent = parts
			return true
		}
	}
	last.MultiContent = append(parts, chat.MessageContentPart{Type: "text", Text: note})
	return true
}
