package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// Bounds of the passages injected when no document is named (rule 2 of
// BuildOpenDocumentPrompt): one budget shared by every document, a few
// documents and passages each, and the opening lines of a document whose
// card is not made yet.
const (
	relevantPassagesRunes  = 6000
	relevantPassagesDocs   = 4
	relevantPassagesPerDoc = 6
	relevantHitRunes       = 600
	relevantNeighbourRunes = 240
	documentHeadLines      = 10
	documentHeadLineRunes  = 200
)

const relevantPassagesInstruction = "Each <relevant_passages> block holds, for one document not given whole this turn, the passages " +
	"that match the question by keyword (with the passage before and after), and, for a document whose card is still being made, its opening lines. " +
	"[i] is a paragraph index of a working document (as rewrite_paragraphs takes) or a chunk/line number of a source. " +
	"Answer from these passages and the cards when they suffice; otherwise search with find_in_documents or read a part with read_document_outline document=vbN from=<i>. " +
	"Quote the passages, never the card."

// profileHasContent reports whether a profile can stand for its document
// on a card (see documentCard).
func profileHasContent(p *types.DocumentProfile) bool {
	return p != nil && (p.Gist != "" || p.Subject != "" || len(p.Sections) > 0)
}

// relevantPassages renders the <relevant_passages> blocks of docs for
// query. readCtx carries the tenant.
func relevantPassages(ctx, readCtx context.Context, src DocumentWorkspaceSource, sessionID string,
	docs []*types.DocumentWorkspace, profiles map[string]*types.DocumentProfile, query string,
) string {
	var loaded []*sessionDocUnits
	var units []searchUnit
	for _, d := range docs {
		if d.IsSource() && d.TextStatus != types.DocumentSourceTextReady {
			continue
		}
		du, err := loadDocumentUnits(readCtx, src, sessionID, d, len(loaded))
		if err != nil {
			logger.Warnf(ctx, "[DocumentWorkspace] passages unavailable for session=%s document=%s: %v", sessionID, d.ID, err)
			continue
		}
		loaded = append(loaded, du)
		units = append(units, du.units...)
	}
	if len(loaded) == 0 {
		return ""
	}

	q := parseSearchQuery(query)
	hits := rankSearchUnits(q, units)
	picked := map[int][]searchHit{}
	var docOrder []int
	shown := map[int]bool{}
	used := 0
	for _, h := range hits {
		if _, ok := picked[h.Doc]; !ok && len(docOrder) >= relevantPassagesDocs {
			continue
		}
		if len(picked[h.Doc]) >= relevantPassagesPerDoc {
			continue
		}
		cost := 0
		for _, pos := range []int{h.Prev, h.Pos, h.Next} {
			if pos >= 0 && !shown[pos] {
				cost += utf8.RuneCountInString(passageLine(q, units, pos, h.Pos)) + 1
			}
		}
		cost += utf8.RuneCountInString(units[h.Pos].Header) // the tag, roughly
		if used+cost > relevantPassagesRunes {
			continue // a shorter passage may still fit
		}
		used += cost
		if _, ok := picked[h.Doc]; !ok {
			docOrder = append(docOrder, h.Doc)
		}
		picked[h.Doc] = append(picked[h.Doc], h)
		for _, pos := range []int{h.Prev, h.Pos, h.Next} {
			if pos >= 0 {
				shown[pos] = true
			}
		}
	}
	// then the documents whose card cannot say what they are yet
	inOrder := map[int]bool{}
	for _, di := range docOrder {
		inOrder[di] = true
	}
	for di, du := range loaded {
		if !inOrder[di] && !profileHasContent(profiles[du.ws.ID]) {
			docOrder = append(docOrder, di)
		}
	}

	var sb strings.Builder
	for n, di := range docOrder {
		du := loaded[di]
		var head string
		if !profileHasContent(profiles[du.ws.ID]) {
			head = documentHead(du)
		}
		var body strings.Builder
		if hs := picked[di]; len(hs) > 0 {
			var positions []int
			hitPos := map[int]bool{}
			seen := map[int]bool{}
			for _, h := range hs {
				hitPos[h.Pos] = true
				for _, pos := range []int{h.Prev, h.Pos, h.Next} {
					if pos >= 0 && !seen[pos] {
						seen[pos] = true
						positions = append(positions, pos)
					}
				}
			}
			sort.Ints(positions)
			for i, pos := range positions {
				if i > 0 && pos != positions[i-1]+1 {
					body.WriteString("…\n")
				}
				anchor := pos
				if !hitPos[pos] {
					anchor = pos - 1 // after a hit
					if hitPos[pos+1] {
						anchor = pos + 1 // before one
					}
				}
				body.WriteString(passageLine(q, units, pos, anchor) + "\n")
			}
		}
		if head == "" && body.Len() == 0 {
			continue
		}
		role := "văn bản làm việc"
		if du.ws.IsSource() {
			role = "tài liệu nguồn"
		}
		fmt.Fprintf(&sb, "\n\n<relevant_passages handle=%q name=%q role=%q unit=%q>\n", du.ws.Handle(), du.ws.FileName, role, unitLabel(du.unit))
		if n == 0 {
			sb.WriteString("<instruction>" + relevantPassagesInstruction + "</instruction>\n")
		}
		if head != "" {
			sb.WriteString("<head>Đầu văn bản (hồ sơ chưa lập xong):\n" + escapeOpenDocument(head) + "</head>\n")
		}
		if body.Len() > 0 {
			sb.WriteString(escapeOpenDocument(body.String()))
		}
		sb.WriteString("</relevant_passages>\n")
	}
	return sb.String()
}

// passageLine renders unit pos: in full up to relevantHitRunes when it is
// a hit (pos == anchor; a long chunk is cut to its matching lines), else as
// a neighbour of the hit at anchor: the end of the unit before it, the
// start of the unit after it.
func passageLine(q *searchQuery, units []searchUnit, pos, anchor int) string {
	u := units[pos]
	var text string
	switch {
	case pos == anchor:
		text = passageExcerpt(q, u.Text, relevantHitRunes)
	case pos < anchor:
		text = tailRunes(flatText(u.Text), relevantNeighbourRunes)
	default:
		text = clipRunes(flatText(u.Text), relevantNeighbourRunes)
	}
	tag := ""
	if u.Header != "" && pos == anchor {
		tag = "(" + clipRunes(flatText(u.Header), 60) + ") "
	}
	return fmt.Sprintf("[%d] %s%s", u.Index, tag, text)
}

func tailRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return "…" + string(r[len(r)-n:])
}

// documentHead is the first documentHeadLines non-empty lines of a
// document, numbered by their unit.
func documentHead(du *sessionDocUnits) string {
	var b strings.Builder
	lines := 0
	for _, u := range du.units {
		for _, l := range strings.Split(u.Text, "\n") {
			if l = strings.TrimSpace(l); l == "" {
				continue
			}
			fmt.Fprintf(&b, "[%d] %s\n", u.Index, clipRunes(flatText(l), documentHeadLineRunes))
			if lines++; lines >= documentHeadLines {
				return b.String()
			}
		}
	}
	return b.String()
}
