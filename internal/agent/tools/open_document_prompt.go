package tools

import (
	"context"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/docformat"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// openDocumentPromptRunes caps the leading document text injected into one
// turn; a longer document is cut and the rest is read with
// read_document_outline. openDocumentMatchRunes is the extra room for later
// paragraphs that name a code from the question (see queryCodes), so "PA05
// làm gì" finds PA05 even past the cut. The block is sent on every turn of a
// document-assistant session (not stored in history): Vietnamese text runs
// about 3.5 runes per Qwen token, so the two caps cost at most ~7k prompt
// tokens a turn — measured 5.2k on a 502-paragraph quy chế.
const (
	openDocumentPromptRunes = 16000
	openDocumentMatchRunes  = 8000
)

const openDocumentInstruction = "This is the current text of the Word document open in the editor next to this conversation. " +
	"Answer questions about the document from this text. Names, unit codes and abbreviations that appear in it " +
	"(for example a department code such as PA05) take their meaning from the document; do not ask the user to explain them. " +
	"Paragraph numbers in [ ] are the indexes rewrite_paragraphs takes."

// BuildOpenDocumentPrompt renders the current text of the session's editable
// document as an <open_document> block for the agent's user turn, so a
// question about the document is answered from it without a tool call. It
// returns "" when the session has no document or the file cannot be read.
func BuildOpenDocumentPrompt(ctx context.Context, src DocumentWorkspaceSource, tenantID uint64, sessionID, query string) string {
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

	flat := strings.NewReplacer("\t", " ", "\n", " ")
	var body strings.Builder
	used, cutAt := 0, -1
	for i, p := range layout.Paragraphs {
		text := strings.TrimSpace(p.Text)
		if text == "" {
			continue
		}
		line := fmt.Sprintf("[%d] %s\n", i, flat.Replace(text))
		n := utf8.RuneCountInString(line)
		if used+n > openDocumentPromptRunes {
			cutAt = i
			break
		}
		body.WriteString(line)
		used += n
	}

	// past the cut, keep the paragraphs that name a code from the question,
	// with their neighbours (the next one often goes on without the code)
	var matched strings.Builder
	if codes := queryCodes(query); cutAt >= 0 && len(codes) > 0 {
		keep := map[int]bool{}
		var order []int
		for i := cutAt; i < len(layout.Paragraphs); i++ {
			if !containsAnyFold(layout.Paragraphs[i].Text, codes) {
				continue
			}
			for j := i - 1; j <= i+1; j++ {
				if j >= cutAt && j < len(layout.Paragraphs) && !keep[j] {
					keep[j] = true
					order = append(order, j)
				}
			}
		}
		extra := 0
		for _, i := range order {
			text := strings.TrimSpace(layout.Paragraphs[i].Text)
			if text == "" {
				continue
			}
			line := fmt.Sprintf("[%d] %s\n", i, flat.Replace(text))
			n := utf8.RuneCountInString(line)
			if extra+n > openDocumentMatchRunes {
				break
			}
			matched.WriteString(line)
			extra += n
		}
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "\n\n<open_document name=%q revision=\"%d\">\n", ws.FileName, ws.Revision)
	sb.WriteString("<instruction>" + openDocumentInstruction + "</instruction>\n")
	sb.WriteString("<text>\n")
	sb.WriteString(escapeOpenDocument(body.String()))
	sb.WriteString("</text>\n")
	if matched.Len() > 0 {
		sb.WriteString("<matching_paragraphs>Later paragraphs that mention a code from the question:\n")
		sb.WriteString(escapeOpenDocument(matched.String()))
		sb.WriteString("</matching_paragraphs>\n")
	}
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
	for _, tag := range []string{"</text>", "</matching_paragraphs>", "</open_document>"} {
		s = strings.ReplaceAll(s, tag, strings.ReplaceAll(strings.ReplaceAll(tag, "<", "&lt;"), ">", "&gt;"))
	}
	return s
}

// queryCodes picks the distinctive tokens of a question — codes with a digit
// or an upper-case abbreviation (PA05, NĐ30, 30/2020, CNTT) — that locate a
// passage better than ordinary words do.
func queryCodes(query string) []string {
	var out []string
	seen := map[string]bool{}
	for _, f := range strings.FieldsFunc(query, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune(",.;:!?()[]\"'“”«»", r)
	}) {
		n := utf8.RuneCountInString(f)
		hasDigit, upper, letters := false, 0, 0
		for _, r := range f {
			switch {
			case unicode.IsDigit(r):
				hasDigit = true
			case unicode.IsLetter(r):
				letters++
				if unicode.IsUpper(r) {
					upper++
				}
			}
		}
		code := (hasDigit && n >= 3) || (letters >= 2 && upper == letters)
		if key := strings.ToLower(f); code && !seen[key] {
			seen[key] = true
			out = append(out, f)
		}
	}
	return out
}

func containsAnyFold(text string, codes []string) bool {
	lower := strings.ToLower(text)
	for _, c := range codes {
		if strings.Contains(lower, strings.ToLower(c)) {
			return true
		}
	}
	return false
}
