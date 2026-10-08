package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/types"
)

// ToolFindInDocuments searches the documents of a document-assistant
// session (targets and sources, vb1…vbN). It is not search_document_section,
// which resolves a citation in a knowledge-base document (dN handles).
const ToolFindInDocuments = "find_in_documents"

// Bounds of one find_in_documents call.
const (
	findDefaultLimit  = 8
	findMaxLimit      = 20
	findOutputRunes   = 4000
	findHitRunes      = 420
	findNeighborRunes = 160
)

// sessionDocUnits is one session document cut into search units.
type sessionDocUnits struct {
	ws      *types.DocumentWorkspace
	unit    string // types.DocumentProfileUnit*
	units   []searchUnit
	profile *types.DocumentProfile
	// sections are the profile's when it describes this version, else the
	// detected ones.
	sections []types.DocumentProfileSection
}

// loadDocumentUnits reads d's units: a target's non-empty paragraphs from
// the layout cache (with their NĐ30 labels), a source's stored chunks (or
// lines). Each unit's header is its chunk heading or the title of the
// section holding it. ctx must carry the tenant.
func loadDocumentUnits(ctx context.Context, src DocumentWorkspaceSource, sessionID string, d *types.DocumentWorkspace, doc int) (*sessionDocUnits, error) {
	if du := turnUnits(ctx, d, doc); du != nil {
		return du, nil
	}
	out := &sessionDocUnits{ws: d, profile: SessionDocumentProfile(ctx, d.ID)}
	var detected []types.DocumentProfileSection
	if d.IsSource() {
		reader, ok := src.(sourceTextReader)
		tenantID, hasTenant := types.TenantIDFromContext(ctx)
		if !ok || !hasTenant {
			return nil, errors.New("không đọc được nội dung tài liệu nguồn " + DocumentLabel(d))
		}
		text, row, err := reader.SourceText(ctx, tenantID, sessionID, d.ID)
		if err != nil {
			return nil, err
		}
		if row != nil {
			out.ws = row
		}
		parts, chunked := sourceParts(text)
		out.unit = types.DocumentProfileUnitLine
		if chunked {
			out.unit = types.DocumentProfileUnitChunk
		}
		for i, p := range parts {
			if p.text == "" {
				continue
			}
			out.units = append(out.units, searchUnit{Doc: doc, Index: i, Text: p.text, Header: p.header})
		}
		detected = sourceProfileInput(out.ws, text).sections
	} else {
		cached, ws, err := openWorkspaceDoc(ctx, src, sessionID, d.ID, d)
		if err != nil {
			return nil, err
		}
		out.ws, out.unit = ws, types.DocumentProfileUnitParagraph
		lines, sections := workspaceDocs.outlineOf(cached, ws)
		for _, l := range lines {
			out.units = append(out.units, searchUnit{Doc: doc, Index: l.Index, Text: l.Text, Label: l.Label})
		}
		detected = sections
	}
	out.sections = detected
	if p := out.profile; p != nil && len(p.Sections) > 0 && p.Describes(out.ws) && (p.Unit == "" || p.Unit == out.unit) {
		out.sections = p.Sections
	}
	for i := range out.units {
		if out.units[i].Header == "" {
			out.units[i].Header = sectionTitleAt(out.sections, out.units[i].Index)
		}
	}
	return out, nil
}

type turnUnitsKey struct{}

// withTurnUnits keeps the units the router loaded for the rest of the
// turn (the prompt builder reads the same documents right after).
func withTurnUnits(ctx context.Context, loaded []*sessionDocUnits) context.Context {
	if len(loaded) == 0 {
		return ctx
	}
	m := make(map[string]*sessionDocUnits, len(loaded))
	for _, du := range loaded {
		m[du.ws.ID] = du
	}
	return context.WithValue(ctx, turnUnitsKey{}, m)
}

// turnUnits returns d's units loaded earlier in this turn when they still
// describe the same version, renumbered as document doc; nil otherwise.
func turnUnits(ctx context.Context, d *types.DocumentWorkspace, doc int) *sessionDocUnits {
	m, _ := ctx.Value(turnUnitsKey{}).(map[string]*sessionDocUnits)
	du := m[d.ID]
	if du == nil || du.ws.Revision != d.Revision || du.ws.SaveCount != d.SaveCount || du.ws.Role != d.Role || du.ws.TextStatus != d.TextStatus {
		return nil
	}
	out := *du
	out.units = make([]searchUnit, len(du.units))
	for i, u := range du.units {
		u.Doc = doc
		out.units[i] = u
	}
	return &out
}

// sectionTitleAt is the title of the last section whose range holds index.
func sectionTitleAt(sections []types.DocumentProfileSection, index int) string {
	title := ""
	for _, s := range sections {
		if s.From <= index && index <= s.To {
			title = s.Title
		}
	}
	return title
}

// matchSections picks the sections ref names: a reference ("Điều 5",
// "Chương II") matches the titles that start with it, other text the
// titles that contain it (both compared without diacritics).
func matchSections(sections []types.DocumentProfileSection, ref string) []types.DocumentProfileSection {
	f := foldSearch(strings.TrimSpace(ref))
	if f == "" {
		return nil
	}
	var out []types.DocumentProfileSection
	if m := searchReferenceRe.FindString(f); m != "" && strings.HasPrefix(f, m) {
		for _, s := range sections {
			if t := foldSearch(s.Title); strings.HasPrefix(t, m) && !isWordRune(firstRune(t[len(m):])) {
				out = append(out, s)
			}
		}
		return out
	}
	for _, s := range sections {
		if strings.Contains(foldSearch(s.Title), f) {
			out = append(out, s)
		}
	}
	return out
}

func firstRune(s string) rune {
	r, _ := utf8.DecodeRuneInString(s)
	return r
}

// inSections keeps the units inside the sections' ranges.
func inSections(units []searchUnit, sections []types.DocumentProfileSection) []searchUnit {
	var out []searchUnit
	for _, u := range units {
		for _, s := range sections {
			if s.From <= u.Index && u.Index <= s.To {
				out = append(out, u)
				break
			}
		}
	}
	return out
}

var findInDocumentsTool = BaseTool{
	name: ToolFindInDocuments,
	description: `Search the text of this conversation's documents (the ones in <session_documents>: working documents and sources, vb1, vb2, …) for passages matching a query. Matching is by words, with and without diacritics, and favours exact codes and numbers: số ký hiệu (45/KH-UBND), Điều/khoản/điểm, unit codes (PA05), figures (1.250 tỷ, 15%).

## When to Use

- Any question about what a document says when the passage is not in the prompt (a long document, a source, a document not named in this message).
- Cross-checking figures, names or dates between documents: for each paragraph you check, search the other documents with the codes, figures and names taken from THAT paragraph, not only with the user's words.
- To find where a topic is before reading it in full with read_document_outline.

## Input

- query: the words, codes or figures to look for.
- document: a handle (vb2); empty searches every document of the conversation.
- section: limit the search to sections of the document's card, by title or reference ("Điều 5", "Chương II", "Phụ lục").
- limit: how many passages to return (default 8, max 20).

## Output

Per document: the matching passages as ` + "`[index] text`" + ` with the passage before and after, the NĐ30 component and section when known, and the number of matches. The index is a paragraph index for a working document (as rewrite_paragraphs takes) and a chunk or line number for a source; read around a result with read_document_outline document=vbN from=<index>. When an answer uses a source, cite it by file name or số ký hiệu.`,
	schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": {
      "type": "string",
      "description": "Words, codes or figures to look for"
    },
    "document": {
      "type": "string",
      "description": "Handle of one document from <session_documents> (vb1, vb2, …); empty = every document"
    },
    "section": {
      "type": "string",
      "description": "Only search these sections of the document: a section title or a reference such as \"Điều 5\""
    },
    "limit": {
      "type": "integer",
      "minimum": 1,
      "maximum": 20,
      "description": "Maximum number of passages (default 8, max 20)"
    }
  },
  "required": ["query"]
}`),
}

type findInDocumentsInput struct {
	Query    string `json:"query"`
	Document string `json:"document"`
	Section  string `json:"section"`
	Limit    int    `json:"limit"`
}

// FindInDocumentsTool searches the session's documents (find_in_documents).
type FindInDocumentsTool struct {
	BaseTool
	workspace DocumentWorkspaceSource
	sessionID string
}

// NewFindInDocumentsTool builds the tool for one session.
func NewFindInDocumentsTool(workspace DocumentWorkspaceSource, sessionID string) *FindInDocumentsTool {
	return &FindInDocumentsTool{BaseTool: findInDocumentsTool, workspace: workspace, sessionID: sessionID}
}

// FindHit is one passage find_in_documents returns (its Data).
type FindHit struct {
	Index     int     `json:"index"`
	Score     float64 `json:"score"`
	Text      string  `json:"text"`
	Label     string  `json:"label,omitempty"`
	Section   string  `json:"section,omitempty"`
	PrevIndex *int    `json:"prev_index,omitempty"`
	NextIndex *int    `json:"next_index,omitempty"`
}

// FindDocumentHits are the passages of one document.
type FindDocumentHits struct {
	Handle   string    `json:"handle"`
	FileName string    `json:"file_name"`
	Role     string    `json:"role"`
	Unit     string    `json:"unit"`
	HitCount int       `json:"hit_count"`
	Hits     []FindHit `json:"hits"`
}

func (t *FindInDocumentsTool) Execute(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
	var in findInDocumentsInput
	if len(args) > 0 {
		if err := json.Unmarshal(args, &in); err != nil {
			return &types.ToolResult{Success: false, Error: "invalid arguments: " + err.Error()}, nil
		}
	}
	in.Query = strings.TrimSpace(in.Query)
	if in.Query == "" {
		return &types.ToolResult{Success: false, Error: "query is required"}, nil
	}
	if in.Limit <= 0 {
		in.Limit = findDefaultLimit
	}
	in.Limit = min(in.Limit, findMaxLimit)
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok || t.workspace == nil {
		return &types.ToolResult{Success: false, Error: errNoWorkspace}, nil
	}

	var docs []*types.DocumentWorkspace
	if strings.TrimSpace(in.Document) != "" {
		d, err := resolveDocument(ctx, t.workspace, t.sessionID, in.Document, false)
		if err != nil {
			return &types.ToolResult{Success: false, Error: err.Error()}, nil
		}
		docs = []*types.DocumentWorkspace{d}
	} else {
		all, err := t.workspace.List(ctx, tenantID, t.sessionID)
		if err != nil || len(all) == 0 {
			return &types.ToolResult{Success: false, Error: errNoWorkspace}, nil
		}
		docs = all
	}

	var notes []string
	var loaded []*sessionDocUnits
	var units []searchUnit
	for _, d := range docs {
		if d.IsSource() && d.TextStatus != types.DocumentSourceTextReady {
			notes = append(notes, DocumentLabel(d)+": chưa đọc được nội dung, chưa tìm được")
			continue
		}
		du, err := loadDocumentUnits(ctx, t.workspace, t.sessionID, d, len(loaded))
		if err != nil {
			notes = append(notes, DocumentLabel(d)+": "+err.Error())
			continue
		}
		if in.Section != "" {
			secs := matchSections(du.sections, in.Section)
			if len(secs) == 0 {
				notes = append(notes, fmt.Sprintf("%s: không có mục %q", DocumentLabel(du.ws), in.Section))
				continue
			}
			du.units = inSections(du.units, secs)
		}
		loaded = append(loaded, du)
		units = append(units, du.units...)
	}
	searched := make([]string, 0, len(loaded))
	for _, du := range loaded {
		searched = append(searched, du.ws.Handle())
	}

	q := parseSearchQuery(in.Query)
	hits := rankSearchUnits(q, units)
	perDoc := make([]int, len(loaded))
	for _, h := range hits {
		perDoc[h.Doc]++
	}

	// pick hits best first while the output stays under the cap
	var b strings.Builder
	header := fmt.Sprintf("Tìm %q trong %s: %d đoạn khớp.\n", in.Query, strings.Join(searched, ", "), len(hits))
	note := "Số trong [ ] là chỉ số đoạn của văn bản làm việc (như rewrite_paragraphs nhận) hoặc số chunk/dòng của tài liệu nguồn; " +
		"đọc quanh một kết quả bằng read_document_outline document=<handle> from=<chỉ số>. Khi dùng nội dung tài liệu nguồn, dẫn nguồn bằng tên tệp hoặc số ký hiệu.\n"
	used := utf8.RuneCountInString(header) + utf8.RuneCountInString(note) + 200*len(loaded)
	var chosen []searchHit
	picked := map[int]bool{}
	for _, h := range hits {
		if len(chosen) >= in.Limit {
			break
		}
		n := utf8.RuneCountInString(t.renderHit(q, units, h, picked, nil)) + 2*findNeighborRunes
		if len(chosen) > 0 && used+n > findOutputRunes {
			break
		}
		chosen = append(chosen, h)
		picked[h.Pos] = true
		used += n
	}

	b.WriteString(header)
	for _, n := range notes {
		b.WriteString("(" + n + ")\n")
	}
	data := map[string]interface{}{
		"display_type": "document_search",
		"query":        in.Query,
		"searched":     strings.Join(searched, ", "),
		"hit_count":    len(hits),
		"shown":        len(chosen),
	}
	if len(hits) == 0 {
		b.WriteString("Không có đoạn nào khớp. Thử từ khóa khác (số ký hiệu, tên đơn vị, con số, cụm từ trong văn bản) hoặc đọc theo mục trên thẻ bằng read_document_outline.\n")
		data["documents"] = []FindDocumentHits{}
		return &types.ToolResult{Success: true, Output: b.String(), Data: data}, nil
	}
	b.WriteString(note)

	// group by document, documents in the order of their best hit,
	// passages in document order
	order := []int{}
	byDoc := map[int][]searchHit{}
	for _, h := range chosen {
		if _, ok := byDoc[h.Doc]; !ok {
			order = append(order, h.Doc)
		}
		byDoc[h.Doc] = append(byDoc[h.Doc], h)
	}
	var docsData []FindDocumentHits
	for _, di := range order {
		du := loaded[di]
		dh := byDoc[di]
		sort.Slice(dh, func(a, c int) bool { return dh[a].Pos < dh[c].Pos })
		fmt.Fprintf(&b, "\n## %s (%s) — %d kết quả", DocumentLabel(du.ws), documentRoleNote(du), perDoc[di])
		if len(dh) < perDoc[di] {
			fmt.Fprintf(&b, ", hiện %d", len(dh))
		}
		b.WriteString("\n")
		entry := FindDocumentHits{Handle: du.ws.Handle(), FileName: du.ws.FileName, Role: types.DocumentRoleOf(du.ws), Unit: du.unit, HitCount: perDoc[di]}
		for _, h := range dh {
			hit := FindHit{Index: h.Index, Score: math.Round(h.Score*100) / 100, Label: units[h.Pos].Label, Section: units[h.Pos].Header}
			b.WriteString(t.renderHit(q, units, h, picked, &hit))
			entry.Hits = append(entry.Hits, hit)
		}
		docsData = append(docsData, entry)
	}
	data["documents"] = docsData
	return &types.ToolResult{Success: true, Output: b.String(), Data: data}, nil
}

// documentRoleNote is the role and unit of a document in the output.
func documentRoleNote(du *sessionDocUnits) string {
	role := "văn bản làm việc"
	if du.ws.IsSource() {
		role = "tài liệu nguồn"
	}
	if p := du.profile; p != nil && p.DocumentNumber != "" {
		role += ", số " + p.DocumentNumber
	}
	return role + ", đơn vị: " + unitLabel(du.unit)
}

// renderHit renders one passage with its neighbours (those not picked as
// passages themselves); hit, when given, receives the text shown.
func (t *FindInDocumentsTool) renderHit(q *searchQuery, units []searchUnit, h searchHit, picked map[int]bool, hit *FindHit) string {
	u := units[h.Pos]
	var b strings.Builder
	text := passageExcerpt(q, u.Text, findHitRunes)
	var tags []string
	if u.Label != "" {
		tags = append(tags, u.Label)
	}
	if u.Header != "" {
		tags = append(tags, clipRunes(flatText(u.Header), 80))
	}
	tag := ""
	if len(tags) > 0 {
		tag = "(" + strings.Join(tags, " · ") + ") "
	}
	fmt.Fprintf(&b, "[%d] %s%s\n", u.Index, tag, text)
	if hit != nil {
		hit.Text = text
	}
	for _, nb := range []struct {
		pos   int
		arrow string
		next  bool
	}{{h.Prev, "↑", false}, {h.Next, "↓", true}} {
		if nb.pos < 0 {
			continue
		}
		if hit != nil {
			idx := units[nb.pos].Index
			if nb.next {
				hit.NextIndex = &idx
			} else {
				hit.PrevIndex = &idx
			}
		}
		if picked[nb.pos] {
			continue
		}
		n := units[nb.pos]
		s := flatText(n.Text)
		if !nb.next && utf8.RuneCountInString(s) > findNeighborRunes {
			r := []rune(s) // the end of the passage before leads into the hit
			s = "…" + string(r[len(r)-findNeighborRunes:])
		} else {
			s = clipRunes(s, findNeighborRunes)
		}
		fmt.Fprintf(&b, "   %s [%d] %s\n", nb.arrow, n.Index, s)
	}
	return b.String()
}

func flatText(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// passageExcerpt shortens a long unit (a source chunk) to its lines that
// match q best, in their order, within runes.
func passageExcerpt(q *searchQuery, text string, runes int) string {
	flat := flatText(text)
	if utf8.RuneCountInString(flat) <= runes {
		return flat
	}
	var lines []searchUnit
	for _, l := range strings.FieldsFunc(text, func(r rune) bool { return r == '\n' }) {
		for _, s := range splitSentences(l) {
			if s = strings.TrimSpace(s); s != "" {
				lines = append(lines, searchUnit{Index: len(lines), Text: s})
			}
		}
	}
	hits := rankSearchUnits(q, lines)
	if len(hits) == 0 {
		return clipRunes(flat, runes)
	}
	keep := map[int]bool{}
	used := 0
	for _, h := range hits {
		n := utf8.RuneCountInString(lines[h.Pos].Text) + 2
		if used > 0 && used+n > runes {
			continue
		}
		keep[h.Pos] = true
		used += n
	}
	var parts []string
	last := -2
	for i, l := range lines {
		if !keep[i] {
			continue
		}
		s := clipRunes(flatText(l.Text), runes)
		if last >= 0 && i != last+1 {
			s = "… " + s
		}
		parts = append(parts, s)
		last = i
	}
	out := strings.Join(parts, " ")
	if !keep[0] {
		out = "… " + out
	}
	return out
}

// splitSentences cuts a long line at sentence ends.
func splitSentences(s string) []string {
	if utf8.RuneCountInString(s) <= 300 {
		return []string{s}
	}
	var out []string
	start := 0
	for i := 0; i+1 < len(s); i++ {
		if (s[i] == '.' || s[i] == ';' || s[i] == '!' || s[i] == '?') && s[i+1] == ' ' {
			out = append(out, s[start:i+1])
			start = i + 2
		}
	}
	return append(out, s[start:])
}
