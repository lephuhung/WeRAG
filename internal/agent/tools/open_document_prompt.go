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

// openDocumentMinRunes is the least text each document gets when several
// documents are injected in one turn (the budget above is split among them).
const openDocumentMinRunes = 8000

const openDocumentInstruction = "This is the current text of a Word document open in the editor next to this conversation. " +
	"Answer questions about the document from this text. Names, unit codes and abbreviations that appear in it " +
	"(for example a department code such as PA05) take their meaning from the document; do not ask the user to explain them. " +
	"Paragraph numbers in [ ] are the indexes rewrite_paragraphs takes."

const sessionDocumentsInstruction = "The conversation holds these Word documents, one editor tab each; the text of each follows in its own <open_document> block. " +
	"When the user asks about several or all of them (\"hai văn bản này\", \"các văn bản\"), answer for every document, naming each. " +
	"Pass a document's handle (vb1, vb2, …) as the \"document\" argument of the document tools. " +
	"Edit only a document the user named with @ (or selected text in) in this request; when the user asks for an edit " +
	"without naming the document, do not edit — ask which document, and tell them to type @ in the chat box to pick it. " +
	"Reading and comparing any of them needs no @."

// BuildOpenDocumentPrompt renders the session's editable documents for the
// agent's user turn: a <session_documents> index when there are several,
// then the current text of the documents the user designated in this turn
// (@-mentions and the selection's document, see namedDocuments) — or of the
// active tab when none — as <open_document> blocks, so a question about a
// document is answered without a tool call. It returns "" when the session
// has no document or none can be read.
func BuildOpenDocumentPrompt(ctx context.Context, src DocumentWorkspaceSource, tenantID uint64, sessionID, query string) string {
	if src == nil || tenantID == 0 || strings.TrimSpace(sessionID) == "" {
		return ""
	}
	docs, err := src.List(ctx, tenantID, sessionID)
	if err != nil || len(docs) == 0 {
		return ""
	}
	active, _ := src.GetBySession(ctx, tenantID, sessionID)
	named := namedDocuments(ctx)
	var chosen []*types.DocumentWorkspace
	for _, d := range docs {
		if named[d.ID] {
			chosen = append(chosen, d)
		}
	}
	if len(chosen) == 0 {
		// Nothing named: carry every open document, the viewed tab first, so a
		// question about "hai văn bản này" is answered for each of them. The
		// per-document budget below shrinks with the count (at most four).
		if active == nil {
			active = docs[len(docs)-1]
		}
		chosen = append(chosen, active)
		for _, d := range docs {
			if d.ID != active.ID {
				chosen = append(chosen, d)
			}
		}
	}

	var sb strings.Builder
	if len(docs) > 1 {
		sb.WriteString("\n\n<session_documents>\n")
		sb.WriteString("<instruction>" + sessionDocumentsInstruction + "</instruction>\n")
		for _, d := range docs {
			sb.WriteString("- " + escapeOpenDocument(DocumentLabel(d)))
			if active != nil && d.ID == active.ID {
				sb.WriteString(" (tab đang xem)")
			}
			if named[d.ID] {
				sb.WriteString(" (người dùng gọi đích danh trong yêu cầu này)")
			}
			sb.WriteString("\n")
		}
		sb.WriteString("</session_documents>\n")
	}

	budget, matchBudget := openDocumentPromptRunes, openDocumentMatchRunes
	if n := len(chosen); n > 1 {
		budget = max(openDocumentPromptRunes/n, openDocumentMinRunes)
		matchBudget = openDocumentMatchRunes / n
	}
	readCtx := context.WithValue(ctx, types.TenantIDContextKey, tenantID)
	rendered := 0
	for _, d := range chosen {
		_, layout, ws, err := readWorkspaceLayout(readCtx, src, sessionID, d)
		if err != nil {
			logger.Warnf(ctx, "[DocumentWorkspace] open document text unavailable for session=%s document=%s: %v", sessionID, d.ID, err)
			continue
		}
		if block := renderOpenDocument(ws, layout, query, budget, matchBudget); block != "" {
			sb.WriteString(block)
			rendered++
		}
	}
	if rendered == 0 && len(docs) == 1 {
		return ""
	}
	return sb.String()
}

// renderOpenDocument renders one document's text as an <open_document>
// block: the paragraphs up to budget runes, then up to matchBudget runes of
// later paragraphs naming a code from query.
func renderOpenDocument(ws *types.DocumentWorkspace, layout *docformat.Layout, query string, budget, matchBudget int) string {
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
		if used+n > budget {
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
			if extra+n > matchBudget {
				break
			}
			matched.WriteString(line)
			extra += n
		}
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "\n\n<open_document handle=%q name=%q revision=\"%d\">\n", ws.Handle(), ws.FileName, ws.Revision)
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
		fmt.Fprintf(&sb, "<truncated>The text above stops before paragraph %d of %d; read the rest with read_document_outline document=%s from=%d.</truncated>\n",
			cutAt, len(layout.Paragraphs), ws.Handle(), cutAt)
	}
	sb.WriteString("</open_document>\n")
	return sb.String()
}

// escapeOpenDocument neutralises closing tags so document text cannot end
// the block early.
func escapeOpenDocument(s string) string {
	for _, tag := range []string{"</text>", "</matching_paragraphs>", "</open_document>", "</session_documents>"} {
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
