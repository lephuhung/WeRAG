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

const sessionDocumentsInstruction = "The conversation holds these documents, one card each: handle and file name, role, then when known the document type, số ký hiệu, issuer and date, a one-line gist, key points, and its sections as \"title [from–to]\". " +
	"Section ranges are paragraph indexes of a working document and chunk (or line) indexes of a source: read a section with read_document_outline document=vbN from=<from>. " +
	"Use the cards to tell which document and which section a question is about; a card says what a document contains, not its exact wording — quote the text, never the card. " +
	"\"(hồ sơ đang được lập)\" means the card is still being made; \"(đã sửa sau lần đọc)\" means the document was edited after its card was made, so its text wins over the card. " +
	"A working document (văn bản làm việc) is a Word file open in an editor tab, the only kind you may check or edit; " +
	"the text of the one the user named (or of the conversation's only document) follows in an <open_document> block. " +
	"Otherwise no document is given whole: the passages matching the question follow in <relevant_passages> blocks, " +
	"and find_in_documents searches every document for the rest. " +
	"A source (tài liệu nguồn) is a file the user uploaded at chat: read-only, never edited or format-checked; search it with " +
	"find_in_documents or read it with read_document_outline document=vbN, and cite it by file name or số ký hiệu when you use it. " +
	"When the user asks about several or all of them (\"hai văn bản này\", \"các văn bản\"), answer for every document, naming each. " +
	"Pass a document's handle (vb1, vb2, …) as the \"document\" argument of the document tools. " +
	"Edit only a document the user named with @ (or selected text in) in this request; when the user asks for an edit " +
	"without naming the document, do not edit — ask which document, and tell them to type @ in the chat box to pick it. " +
	"Reading and comparing any of them needs no @."

type attachedSourcesKey struct{}

// WithAttachedSources records the source documents (workspace IDs) whose
// upload is attached to this turn's message: their parsed text is in the
// message's attachment block this turn, which the index says.
func WithAttachedSources(ctx context.Context, ids []string) context.Context {
	if len(ids) == 0 {
		return ctx
	}
	return context.WithValue(ctx, attachedSourcesKey{}, append([]string(nil), ids...))
}

func attachedSources(ctx context.Context) map[string]bool {
	ids, _ := ctx.Value(attachedSourcesKey{}).([]string)
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out
}

// BuildOpenDocumentPrompt renders the session's documents for the agent's
// user turn: a <session_documents> index of cards when there are several
// or any is a source, then text, by these rules:
//
//  1. A target the user designated in this turn (@-mention, the
//     selection's document, see namedDocuments), or the only document of
//     the session, is injected as an <open_document> block: its text up to
//     openDocumentPromptRunes — with a selection, the window around the
//     selected paragraphs plus the document's opening lines.
//  2. Otherwise (several documents, or sources next to a tab, nothing
//     named) no document is injected whole: the passages of every document
//     that match the question (see searchDocuments) follow as
//     <relevant_passages> blocks under one shared budget, and a document
//     without a card yet also shows its opening lines, so "do đơn vị nào
//     ban hành" is answered from them. find_in_documents and
//     read_document_outline read the rest. Naming only sources with @
//     narrows the passages to them.
//
// A source is never injected whole. It returns "" when the session has no
// document or nothing can be rendered.
func BuildOpenDocumentPrompt(ctx context.Context, src DocumentWorkspaceSource, tenantID uint64, sessionID, query string) string {
	if src == nil || tenantID == 0 || strings.TrimSpace(sessionID) == "" {
		return ""
	}
	docs, err := src.List(ctx, tenantID, sessionID)
	if err != nil || len(docs) == 0 {
		return ""
	}
	targets := targetDocuments(docs)
	var active *types.DocumentWorkspace
	if len(targets) > 0 {
		active, _ = src.GetBySession(ctx, tenantID, sessionID)
	}
	named := namedDocuments(ctx)
	var chosen []*types.DocumentWorkspace
	for _, d := range targets {
		if named[d.ID] {
			chosen = append(chosen, d)
		}
	}
	if len(chosen) == 0 && len(docs) == 1 && len(targets) == 1 {
		chosen = targets // the only document
	}

	var sb strings.Builder
	indexed := len(docs) > 1 || len(targets) < len(docs)
	profiles := make(map[string]*types.DocumentProfile, len(docs))
	for _, d := range docs {
		profiles[d.ID] = SessionDocumentProfile(ctx, d.ID)
	}
	if indexed {
		attached := attachedSources(ctx)
		sb.WriteString("\n\n<session_documents>\n")
		sb.WriteString("<instruction>" + sessionDocumentsInstruction + "</instruction>\n")
		for _, d := range docs {
			var line strings.Builder
			line.WriteString("- " + DocumentLabel(d))
			if d.IsSource() {
				line.WriteString(" (" + sourceIndexNote(d, attached[d.ID]) + ")")
			} else {
				line.WriteString(" (văn bản làm việc")
				if active != nil && d.ID == active.ID {
					line.WriteString(", tab đang xem")
				}
				line.WriteString(")")
			}
			if named[d.ID] {
				line.WriteString(" (người dùng gọi đích danh trong yêu cầu này)")
			}
			profile := profiles[d.ID]
			if profile != nil && !profile.Describes(d) && profile.TextHash != "" && d.IsTarget() {
				// edited since its card: refresh now rather than at the mark
				PromoteDocumentProfileRefresh(d.ID)
			}
			sb.WriteString(escapeOpenDocument(documentCard(line.String(), d, profile)))
		}
		sb.WriteString("</session_documents>\n")
	}

	readCtx := context.WithValue(ctx, types.TenantIDContextKey, tenantID)
	rendered := 0
	if len(chosen) > 0 {
		budget, matchBudget := openDocumentPromptRunes, openDocumentMatchRunes
		if n := len(chosen); n > 1 {
			budget = max(openDocumentPromptRunes/n, openDocumentMinRunes)
			matchBudget = openDocumentMatchRunes / n
		}
		sel := types.DocumentSelectionFromContext(ctx)
		for _, d := range chosen {
			_, layout, ws, err := readWorkspaceLayout(readCtx, src, sessionID, d)
			if err != nil {
				logger.Warnf(ctx, "[DocumentWorkspace] open document text unavailable for session=%s document=%s: %v", sessionID, d.ID, err)
				continue
			}
			var window *paragraphWindow
			if sel != nil && sel.DocumentID == d.ID {
				window = selectionWindow(layout, sel)
			}
			if block := renderOpenDocument(ws, layout, query, budget, matchBudget, window); block != "" {
				sb.WriteString(block)
				rendered++
			}
		}
	} else if block := relevantPassages(ctx, readCtx, src, sessionID, passageDocuments(docs, named), profiles, query); block != "" {
		sb.WriteString(block)
		rendered++
	}
	if rendered == 0 && !indexed {
		return ""
	}
	return sb.String()
}

// passageDocuments are the documents rule 2 searches: the sources the user
// named with @ when they named any (a source is never injected whole, so
// naming one narrows the passages to it), else every document.
func passageDocuments(docs []*types.DocumentWorkspace, named map[string]bool) []*types.DocumentWorkspace {
	var out []*types.DocumentWorkspace
	for _, d := range docs {
		if named[d.ID] {
			out = append(out, d)
		}
	}
	if len(out) == 0 {
		return docs
	}
	return out
}

// Card bounds: a card stays near documentCardRunes so ten documents cost
// ~6k runes a turn.
const (
	documentCardRunes        = 600
	documentCardSections     = 12
	documentCardSectionsCut  = 10
	documentCardKeyPoints    = 3
	documentCardSectionRunes = 60
)

// documentCard renders a document's <session_documents> entry: its index
// line, then from its profile the identity (type, số ký hiệu, issuer,
// date), the gist, for a ready profile three key points, and the section
// list "title [from–to]". Without a profile the line says one is being
// made. A card longer than documentCardRunes loses its key points, then
// section titles are shortened and the list cut.
func documentCard(line string, d *types.DocumentWorkspace, p *types.DocumentProfile) string {
	hasContent := profileHasContent(p)
	if !hasContent {
		if p == nil || p.InProgress() {
			if !(d.IsSource() && d.TextStatus == types.DocumentSourceTextFailed) {
				line += " (hồ sơ đang được lập)"
			}
		}
		return line + "\n"
	}
	var ident []string
	if p.DocType != "" {
		ident = append(ident, p.DocType)
	}
	if p.DocumentNumber != "" {
		ident = append(ident, "số "+p.DocumentNumber)
	}
	if p.Issuer != "" {
		ident = append(ident, p.Issuer)
	}
	if p.Date != "" {
		ident = append(ident, "ngày "+p.Date)
	}
	gist := p.Gist
	if gist == "" {
		gist = p.Subject
	}
	var points []string
	if p.Status == types.DocumentProfileReady {
		points = p.KeyPoints[:min(len(p.KeyPoints), documentCardKeyPoints)]
	}
	stale := !p.Describes(d) || p.InProgress()
	render := func(points []string, titleRunes, maxSections int, gistRunes int) string {
		var b strings.Builder
		b.WriteString(line)
		if stale {
			b.WriteString(" (đã sửa sau lần đọc)")
		}
		b.WriteString("\n")
		if len(ident) > 0 {
			b.WriteString("  " + strings.Join(ident, " · ") + "\n")
		}
		if gist != "" {
			b.WriteString("  Nội dung: " + clipRunes(gist, gistRunes) + "\n")
		}
		if len(points) > 0 {
			b.WriteString("  Ý chính: " + strings.Join(points, "; ") + "\n")
		}
		if n := len(p.Sections); n > 0 {
			shown := p.Sections
			if n > maxSections {
				shown = p.Sections[:min(documentCardSectionsCut, maxSections)]
			}
			parts := make([]string, 0, len(shown)+1)
			for _, s := range shown {
				parts = append(parts, fmt.Sprintf("%s [%d–%d]", clipRunes(s.Title, titleRunes), s.From, s.To))
			}
			if len(shown) < n {
				parts = append(parts, fmt.Sprintf("… (%d mục)", n))
			}
			b.WriteString("  Mục (" + unitLabel(p.Unit) + "): " + strings.Join(parts, "; ") + "\n")
		}
		return b.String()
	}
	card := render(points, documentCardSectionRunes, documentCardSections, 200)
	for _, try := range []func() string{
		func() string { return render(nil, documentCardSectionRunes, documentCardSections, 200) },
		func() string { return render(nil, 30, documentCardSections, 160) },
		func() string { return render(nil, 30, 6, 120) },
		func() string { return render(nil, 24, 3, 100) },
	} {
		if utf8.RuneCountInString(card) <= documentCardRunes {
			break
		}
		card = try()
	}
	return card
}

// sourceIndexNote describes a source in the <session_documents> index: its
// role, type, size and how to read it (or that its text is attached to this
// message).
func sourceIndexNote(d *types.DocumentWorkspace, attachedNow bool) string {
	note := "tài liệu nguồn, chỉ tra cứu"
	if t := strings.TrimSpace(d.FileType); t != "" {
		note += ", " + t
	}
	if d.FileSize > 0 {
		note += ", " + humanSize(d.FileSize)
	}
	switch {
	case attachedNow:
		note += "; nội dung đính kèm trong tin nhắn này"
	case d.TextStatus == types.DocumentSourceTextProcessing:
		note += "; đang đọc nội dung, chưa tra cứu được"
	case d.TextStatus == types.DocumentSourceTextFailed:
		note += "; không đọc được nội dung"
	default:
		note += "; tra cứu bằng read_document_outline document=" + d.Handle()
	}
	return note
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// paragraphWindow is the part of a document injected around a selection:
// paragraphs From..To (inclusive) after the Head paragraphs (the opening
// lines, when they are not in the window).
type paragraphWindow struct {
	From, To int
	Head     []int
}

// Selection window bounds: the selected paragraphs ± selectionWindowParas,
// after the first selectionHeadParas non-empty paragraphs.
const (
	selectionWindowParas = 15
	selectionHeadParas   = 6
)

// selectionWindow locates the selected paragraphs in layout (as
// check_spelling does: a paragraph wholly inside the selection, else the
// one holding it) and returns the window around them; nil when the
// selection is not found (the text is then injected from the start).
func selectionWindow(layout *docformat.Layout, sel *types.DocumentSelection) *paragraphWindow {
	s := foldForOverlap(sel.Text)
	if s == "" || layout == nil {
		return nil
	}
	first, last := -1, -1
	mark := func(i int) {
		if first < 0 || i < first {
			first = i
		}
		if i > last {
			last = i
		}
	}
	hint := foldForOverlap(sel.ParagraphHint)
	partial := -1
	for i, p := range layout.Paragraphs {
		t := foldForOverlap(p.Text)
		if t == "" {
			continue
		}
		switch {
		case strings.Contains(s, t) && utf8.RuneCountInString(t) >= 8 || s == t:
			mark(i)
		case strings.Contains(t, s):
			// a passage inside one paragraph: the paragraph hint tells
			// which of several holding it
			if partial < 0 || (hint != "" && strings.Contains(t, hint)) {
				partial = i
			}
		}
	}
	if first < 0 {
		if partial < 0 {
			return nil
		}
		first, last = partial, partial
	}
	w := &paragraphWindow{From: max(0, first-selectionWindowParas), To: min(len(layout.Paragraphs)-1, last+selectionWindowParas)}
	for i, p := range layout.Paragraphs {
		if len(w.Head) >= selectionHeadParas || i >= w.From {
			break
		}
		if strings.TrimSpace(p.Text) != "" {
			w.Head = append(w.Head, i)
		}
	}
	return w
}

// renderOpenDocument renders one document's text as an <open_document>
// block: the paragraphs up to budget runes (with a window, its head
// paragraphs then the window), then up to matchBudget runes of the other
// paragraphs naming a code from query.
func renderOpenDocument(ws *types.DocumentWorkspace, layout *docformat.Layout, query string, budget, matchBudget int, window *paragraphWindow) string {
	if len(layout.Paragraphs) == 0 {
		return ""
	}

	flat := strings.NewReplacer("\t", " ", "\n", " ")
	var body strings.Builder
	used, cutAt := 0, -1
	shown := map[int]bool{}
	order := make([]int, 0, len(layout.Paragraphs))
	from := 0
	if window != nil {
		order = append(order, window.Head...)
		from = window.From
	}
	for i := from; i < len(layout.Paragraphs); i++ {
		if window != nil && i > window.To {
			break
		}
		order = append(order, i)
	}
	prev := -1
	for _, i := range order {
		text := strings.TrimSpace(layout.Paragraphs[i].Text)
		if text == "" {
			continue
		}
		line := fmt.Sprintf("[%d] %s\n", i, flat.Replace(text))
		if prev >= 0 && i > prev+1 && window != nil && i == window.From {
			line = "…\n" + line
		}
		n := utf8.RuneCountInString(line)
		if used+n > budget {
			cutAt = i
			break
		}
		body.WriteString(line)
		used += n
		shown[i] = true
		prev = i
	}
	// outside the text shown, keep the paragraphs that name a code from
	// the question, with their neighbours (the next one often goes on
	// without the code)
	var matched strings.Builder
	if codes := queryCodes(query); (cutAt >= 0 || window != nil) && len(codes) > 0 {
		keep := map[int]bool{}
		var picked []int
		for i := range layout.Paragraphs {
			if shown[i] || !containsAnyFold(layout.Paragraphs[i].Text, codes) {
				continue
			}
			for j := i - 1; j <= i+1; j++ {
				if j >= 0 && j < len(layout.Paragraphs) && !shown[j] && !keep[j] {
					keep[j] = true
					picked = append(picked, j)
				}
			}
		}
		extra := 0
		for _, i := range picked {
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
	if window != nil {
		fmt.Fprintf(&sb, "<window>The text below is the document's opening lines and paragraphs %d–%d around the passage the user highlighted, not the whole document.</window>\n", window.From, window.To)
	}
	sb.WriteString("<text>\n")
	sb.WriteString(escapeOpenDocument(body.String()))
	sb.WriteString("</text>\n")
	if matched.Len() > 0 {
		sb.WriteString("<matching_paragraphs>Other paragraphs that mention a code from the question:\n")
		sb.WriteString(escapeOpenDocument(matched.String()))
		sb.WriteString("</matching_paragraphs>\n")
	}
	switch {
	case cutAt >= 0:
		fmt.Fprintf(&sb, "<truncated>The text above stops before paragraph %d of %d; read the rest with read_document_outline document=%s from=%d.</truncated>\n",
			cutAt, len(layout.Paragraphs), ws.Handle(), cutAt)
	case window != nil:
		fmt.Fprintf(&sb, "<truncated>The document has %d paragraphs; read others with read_document_outline document=%s from=<index>, or search it with find_in_documents.</truncated>\n",
			len(layout.Paragraphs), ws.Handle())
	}
	sb.WriteString("</open_document>\n")
	return sb.String()
}

// escapeOpenDocument neutralises closing tags so document text cannot end
// the block early.
func escapeOpenDocument(s string) string {
	for _, tag := range []string{"</text>", "</matching_paragraphs>", "</open_document>", "</session_documents>", "</relevant_passages>"} {
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
