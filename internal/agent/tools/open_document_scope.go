package tools

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

const scopeInstruction = "The line above is the part of the documents this conversation is about (chosen by the user, or by the router from the question). " +
	"Answer within it; its sections follow in full in <document_sections> blocks. When the user asks about something outside it, " +
	"use find_in_documents or read_document_outline on the other documents."

// renderScope writes the text of rule 3: the scoped sections of each
// scoped document in full (openDocumentPromptRunes shared), a scoped
// target without sections whole when it is the only scoped document, and
// the matching passages of the other scoped documents. It returns the
// blocks written.
func renderScope(sb *strings.Builder, ctx, readCtx context.Context, src DocumentWorkspaceSource, sessionID string,
	docs []*types.DocumentWorkspace, profiles map[string]*types.DocumentProfile, scope *types.DocumentScope, query string,
) int {
	byID := map[string]*types.DocumentWorkspace{}
	for _, d := range docs {
		byID[d.ID] = d
	}
	var withSections, without []*types.DocumentWorkspace
	for _, id := range scope.DocumentIDs {
		d := byID[id]
		if d == nil {
			continue
		}
		if len(scope.SectionsOf(id)) > 0 {
			withSections = append(withSections, d)
		} else {
			without = append(without, d)
		}
	}
	if len(withSections) == 0 && len(without) == 1 && without[0].IsTarget() {
		return renderFullDocuments(sb, ctx, readCtx, src, sessionID, without, query)
	}
	rendered := 0
	if n := len(withSections); n > 0 {
		budget := openDocumentPromptRunes / n
		for _, d := range withSections {
			du, err := loadDocumentUnits(readCtx, src, sessionID, d, 0)
			if err != nil {
				logger.Warnf(ctx, "[DocumentWorkspace] scoped sections unavailable for session=%s document=%s: %v", sessionID, d.ID, err)
				continue
			}
			if block := renderScopedSections(du, scope.SectionsOf(d.ID), budget); block != "" {
				sb.WriteString(block)
				rendered++
			}
		}
	}
	if len(without) > 0 {
		if block := relevantPassages(ctx, readCtx, src, sessionID, without, profiles, query); block != "" {
			sb.WriteString(block)
			rendered++
		}
	}
	return rendered
}

// renderScopedSections renders the units of du inside sections, in order,
// under their section titles, within budget runes.
func renderScopedSections(du *sessionDocUnits, sections []types.DocumentScopeSection, budget int) string {
	var body strings.Builder
	used, cut := 0, -1
	written := map[int]bool{}
outer:
	for _, s := range sections {
		heading := fmt.Sprintf("### %s [%d–%d]\n", strings.TrimSpace(s.Title), s.From, s.To)
		headed := false
		for _, u := range du.units {
			if u.Index < s.From || u.Index > s.To || written[u.Index] {
				continue
			}
			line := fmt.Sprintf("[%d] %s\n", u.Index, flatText(u.Text))
			if !headed {
				line = heading + line
			}
			n := utf8.RuneCountInString(line)
			if used > 0 && used+n > budget {
				cut = u.Index
				break outer
			}
			body.WriteString(line)
			headed = true
			written[u.Index] = true
			used += n
		}
	}
	if body.Len() == 0 {
		return ""
	}
	role := "văn bản làm việc"
	if du.ws.IsSource() {
		role = "tài liệu nguồn"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "\n\n<document_sections handle=%q name=%q role=%q unit=%q>\n", du.ws.Handle(), du.ws.FileName, role, unitLabel(du.unit))
	sb.WriteString(escapeOpenDocument(body.String()))
	if cut >= 0 {
		fmt.Fprintf(&sb, "<truncated>The sections above stop before %s %d; read on with read_document_outline document=%s from=%d.</truncated>\n",
			unitLabel(du.unit), cut, du.ws.Handle(), cut)
	}
	sb.WriteString("</document_sections>\n")
	return sb.String()
}
