package types

import "strings"

// Limits for the editor selection injected into a chat turn.
const (
	DocumentSelectionMaxRunes     = 8000
	documentSelectionMaxHintRunes = 2000
)

// documentSelectionInstruction tells the model what the block means.
const documentSelectionInstruction = "Người dùng đã bôi đen đoạn sau trong tài liệu đang mở; yêu cầu của họ áp dụng cho đoạn này."

// Normalized returns a trimmed, length-capped copy of the selection, or nil
// when there is no selected text.
func (s *DocumentSelection) Normalized() *DocumentSelection {
	if s == nil {
		return nil
	}
	text := truncateRunes(strings.TrimSpace(s.Text), DocumentSelectionMaxRunes)
	if text == "" {
		return nil
	}
	return &DocumentSelection{
		Text:          text,
		ParagraphHint: truncateRunes(strings.TrimSpace(s.ParagraphHint), documentSelectionMaxHintRunes),
	}
}

// BuildPrompt renders the selection as a prompt block appended to the user
// turn, after any attachments. It returns "" when nothing is selected.
func (s *DocumentSelection) BuildPrompt() string {
	n := s.Normalized()
	if n == nil {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n\n<document_selection>\n")
	sb.WriteString("<instruction>" + documentSelectionInstruction + "</instruction>\n")
	sb.WriteString("<text>\n")
	sb.WriteString(escapeDocumentSelection(n.Text))
	sb.WriteString("\n</text>\n")
	if n.ParagraphHint != "" && n.ParagraphHint != n.Text {
		sb.WriteString("<paragraph_hint>\n")
		sb.WriteString(escapeDocumentSelection(n.ParagraphHint))
		sb.WriteString("\n</paragraph_hint>\n")
	}
	sb.WriteString("</document_selection>\n")
	return sb.String()
}

// escapeDocumentSelection neutralises closing tags so selected text cannot
// end the block early.
func escapeDocumentSelection(s string) string {
	for _, tag := range []string{"</text>", "</paragraph_hint>", "</document_selection>"} {
		s = strings.ReplaceAll(s, tag, strings.ReplaceAll(strings.ReplaceAll(tag, "<", "&lt;"), ">", "&gt;"))
	}
	return s
}
