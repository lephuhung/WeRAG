package tools

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/docformat"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// openDocumentPromptRunes caps the document text injected into one turn; a
// longer document is cut and the rest is read with read_document_outline.
const openDocumentPromptRunes = 16000

const openDocumentInstruction = "This is the current text of the Word document open in the editor next to this conversation. " +
	"Answer questions about the document from this text. Names, unit codes and abbreviations that appear in it " +
	"(for example a department code such as PA05) take their meaning from the document; do not ask the user to explain them. " +
	"Paragraph numbers in [ ] are the indexes rewrite_paragraphs takes."

// BuildOpenDocumentPrompt renders the current text of the session's editable
// document as an <open_document> block for the agent's user turn, so a
// question about the document is answered from it without a tool call. It
// returns "" when the session has no document or the file cannot be read.
func BuildOpenDocumentPrompt(ctx context.Context, src DocumentWorkspaceSource, tenantID uint64, sessionID string) string {
	if src == nil || tenantID == 0 || strings.TrimSpace(sessionID) == "" {
		return ""
	}
	ws, err := src.GetBySession(ctx, tenantID, sessionID)
	if err != nil || ws == nil {
		return ""
	}
	content, ws, err := readWorkspaceDocument(context.WithValue(ctx, types.TenantIDContextKey, tenantID), src, sessionID)
	if err != nil {
		logger.Warnf(ctx, "[DocumentWorkspace] open document text unavailable for session=%s: %v", sessionID, err)
		return ""
	}
	layout := docformat.InspectDocx(content)
	if len(layout.Paragraphs) == 0 {
		return ""
	}

	var body strings.Builder
	used, cutAt := 0, -1
	for i, p := range layout.Paragraphs {
		text := strings.TrimSpace(p.Text)
		if text == "" {
			continue
		}
		line := fmt.Sprintf("[%d] %s\n", i, strings.NewReplacer("\t", " ", "\n", " ").Replace(text))
		n := utf8.RuneCountInString(line)
		if used+n > openDocumentPromptRunes {
			cutAt = i
			break
		}
		body.WriteString(line)
		used += n
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "\n\n<open_document name=%q revision=\"%d\">\n", ws.FileName, ws.Revision)
	sb.WriteString("<instruction>" + openDocumentInstruction + "</instruction>\n")
	sb.WriteString("<text>\n")
	sb.WriteString(escapeOpenDocument(body.String()))
	sb.WriteString("</text>\n")
	if cutAt >= 0 {
		fmt.Fprintf(&sb, "<truncated>The text above stops before paragraph %d of %d; read the rest with read_document_outline from=%d.</truncated>\n",
			cutAt, len(layout.Paragraphs), cutAt)
	}
	sb.WriteString("</open_document>\n")
	return sb.String()
}

// escapeOpenDocument neutralises closing tags so document text cannot end
// the block early.
func escapeOpenDocument(s string) string {
	for _, tag := range []string{"</text>", "</open_document>"} {
		s = strings.ReplaceAll(s, tag, strings.ReplaceAll(strings.ReplaceAll(tag, "<", "&lt;"), ">", "&gt;"))
	}
	return s
}
