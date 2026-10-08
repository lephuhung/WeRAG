package types

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"strings"
)

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
		DocumentID:    truncateRunes(strings.TrimSpace(s.DocumentID), 64),
		Document:      truncateRunes(strings.TrimSpace(s.Document), 300),
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
	if n.Document != "" {
		sb.WriteString("<document>" + escapeDocumentSelection(n.Document) + "</document>\n")
	}
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
	for _, tag := range []string{"</text>", "</paragraph_hint>", "</document>", "</document_selection>"} {
		s = strings.ReplaceAll(s, tag, strings.ReplaceAll(strings.ReplaceAll(tag, "<", "&lt;"), ">", "&gt;"))
	}
	return s
}

// MessageDocumentSelection is the database form of a DocumentSelection
// persisted on the user message, so chat history can show which passage a
// question referred to. It has the same JSON shape as DocumentSelection.
type MessageDocumentSelection DocumentSelection

// NewMessageDocumentSelection converts a (normalized) selection for storage;
// nil stays nil.
func NewMessageDocumentSelection(s *DocumentSelection) *MessageDocumentSelection {
	if s == nil {
		return nil
	}
	stored := MessageDocumentSelection(*s)
	return &stored
}

// Value implements the driver.Valuer interface for database serialization.
func (s MessageDocumentSelection) Value() (driver.Value, error) {
	return json.Marshal(s)
}

// Scan implements the sql.Scanner interface for database deserialization.
func (s *MessageDocumentSelection) Scan(value any) error {
	var b []byte
	switch v := value.(type) {
	case nil:
		*s = MessageDocumentSelection{}
		return nil
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		return errors.New("types: cannot scan document selection from unsupported type")
	}
	if len(b) == 0 {
		*s = MessageDocumentSelection{}
		return nil
	}
	return json.Unmarshal(b, s)
}

// documentSelectionContextKey carries the turn's document selection to the
// agent's tools (rewrite_paragraphs edits only the selected passage).
const documentSelectionContextKey ContextKey = "DocumentSelection"

// WithDocumentSelection returns ctx carrying the normalized selection; a nil
// or empty selection leaves ctx unchanged.
func WithDocumentSelection(ctx context.Context, sel *DocumentSelection) context.Context {
	n := sel.Normalized()
	if n == nil {
		return ctx
	}
	return context.WithValue(ctx, documentSelectionContextKey, n)
}

// DocumentSelectionFromContext returns the selection of the current turn, or
// nil when the user highlighted nothing.
func DocumentSelectionFromContext(ctx context.Context) *DocumentSelection {
	if ctx == nil {
		return nil
	}
	sel, _ := ctx.Value(documentSelectionContextKey).(*DocumentSelection)
	return sel
}

// mentionedDocumentsContextKey carries the workspace IDs of the documents the
// user named (@) in the current turn to the agent's document tools.
const mentionedDocumentsContextKey ContextKey = "MentionedDocuments"

// WithMentionedDocuments returns ctx carrying the workspace IDs the user named
// in this turn; an empty list leaves ctx unchanged.
func WithMentionedDocuments(ctx context.Context, ids []string) context.Context {
	if len(ids) == 0 {
		return ctx
	}
	return context.WithValue(ctx, mentionedDocumentsContextKey, append([]string(nil), ids...))
}

// MentionedDocumentsFromContext returns the workspace IDs the user named in
// the current turn, in mention order.
func MentionedDocumentsFromContext(ctx context.Context) []string {
	if ctx == nil {
		return nil
	}
	ids, _ := ctx.Value(mentionedDocumentsContextKey).([]string)
	return ids
}
