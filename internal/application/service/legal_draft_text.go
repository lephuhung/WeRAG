package service

import (
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/docformat"
	"github.com/Tencent/WeKnora/internal/docformat/docxedit"
	"github.com/Tencent/WeKnora/internal/docformat/docxwrite"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// Text handling of the OCR drafts: OCR Markdown → editor paragraphs, the
// edited paragraphs → structure report, and → vbpl-style HTML.

var (
	ocrPageMarkerRe   = regexp.MustCompile(`(?m)^[ \t]*<!--\s*page\s+(\d+)\s*-->[ \t]*$`)
	ocrPageLineRe     = regexp.MustCompile(`^<!--\s*page\s+(\d+)\s*-->$`)
	ocrCommentRe      = regexp.MustCompile(`<!--.*?-->`)
	ocrImageRe        = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	ocrMDHeadingRe    = regexp.MustCompile(`^#{1,6}[ \t]*`)
	ocrTableSepRe     = regexp.MustCompile(`^\|?[ \t]*:?-{2,}:?[ \t]*(\|[ \t]*:?-{2,}:?[ \t]*)*\|?$`)
	ocrCellBreakRe    = regexp.MustCompile(`(?i)</t[dh]>\s*<t[dh][^>]*>`)
	ocrBreakTagRe     = regexp.MustCompile(`(?i)<br\s*/?>`)
	ocrTagRe          = regexp.MustCompile(`</?[a-zA-Z][^>]*>`)
	ocrEscapeRe       = regexp.MustCompile(`\\([\\` + "`" + `*_{}\[\]()#+\-.!|>~])`)
	ocrBoldRe         = regexp.MustCompile(`\*\*|__`)
	ocrItalicRe       = regexp.MustCompile(`(^|[\s(])\*([^*\n]+)\*`)
	draftPageTextRe   = regexp.MustCompile(`^\[Trang \d+\]$`)
	draftWarnTextRe   = regexp.MustCompile(`^\[OCR hỏng ở trang (\d+)\b`)
	draftLabelOnlyRe  = regexp.MustCompile(`(?i)^(?:phần|chương|mục)\s+[\p{L}\p{N}]+\s*[.:]?$`)
	draftHeadingNumRe = regexp.MustCompile(`(?i)^(?:phần|chương|mục|điều)\s+((?:thứ\s+)?[\p{L}\p{N}]+)`)
	draftClauseRe     = regexp.MustCompile(`^\d+\.`)
	draftItemRe       = regexp.MustCompile(`^[a-zđ]{1,2}\)`)
	draftClosingRe    = regexp.MustCompile(`^(?:Nơi nhận|NƠI NHẬN|TM\.|KT\.|TL\.)`)
)

// ---------------------------------------------------------------------------
// OCR Markdown → editor paragraphs

// LegalDraftParagraphsFromOCR turns OCR Markdown into the paragraphs of the
// draft's .docx: "<!-- page N -->" becomes a "[Trang N]" PageMarker, the
// Phần/Chương/Mục/Điều headings the legal parser recognises Heading1–4
// (with the title line under a bare "Chương II"), Markdown marks and HTML
// tags are dropped, table rows become text cells joined by " | ", and the
// short upper-case lines of the header are centred in bold. Right after the
// marker of each page in degenerate (its OCR still broken after the second
// pass) comes a red PageWarning line asking the reviewer to type the page in.
func LegalDraftParagraphsFromOCR(markdown string, degenerate []int) []docxwrite.Paragraph {
	type line struct {
		text    string
		marker  bool
		warning bool
	}
	broken := map[string]bool{}
	for _, n := range degenerate {
		broken[strconv.Itoa(n)] = true
	}
	var lines []line
	for _, raw := range strings.Split(strings.ReplaceAll(markdown, "\r", ""), "\n") {
		t := strings.TrimSpace(raw)
		if m := ocrPageLineRe.FindStringSubmatch(t); m != nil {
			lines = append(lines, line{text: "[Trang " + m[1] + "]", marker: true})
			if broken[m[1]] {
				lines = append(lines, line{text: LegalDraftPageWarningText(m[1]), warning: true})
			}
			continue
		}
		if t = cleanOCRLine(t); t != "" {
			lines = append(lines, line{text: t})
		}
	}
	var texts []string
	var idx []int
	for i, l := range lines {
		if !l.marker && !l.warning {
			texts = append(texts, l.text)
			idx = append(idx, i)
		}
	}
	levels := headingLevelsOfLines(texts)
	styles := map[int]string{}
	firstHeading := len(lines)
	for k, level := range levels {
		i := idx[k]
		styles[i] = headingStyleOf(level)
		if i < firstHeading {
			firstHeading = i
		}
		// "Chương II" alone: its title is the next text line
		if level < vietnamese_legal.LevelDieu && draftLabelOnlyRe.MatchString(texts[k]) && k+1 < len(texts) {
			if _, isHeading := levels[k+1]; !isHeading && looksLikeTitle(texts[k+1]) {
				styles[idx[k+1]] = headingStyleOf(level)
			}
		}
	}
	paras := make([]docxwrite.Paragraph, 0, len(lines))
	for i, l := range lines {
		p := docxwrite.Paragraph{Text: l.text}
		switch {
		case l.marker:
			p.Style = docxwrite.StylePageMarker
		case l.warning:
			p.Style = docxwrite.StylePageWarning
		case styles[i] != "":
			p.Style = styles[i]
		case i < firstHeading && isUpperLine(l.text) && utf8.RuneCountInString(l.text) <= 120:
			p.Bold, p.Center = true, true
		}
		paras = append(paras, p)
	}
	return paras
}

func cleanOCRLine(t string) string {
	t = ocrCommentRe.ReplaceAllString(t, " ")
	t = ocrImageRe.ReplaceAllString(t, " ")
	if strings.Contains(strings.ToLower(t), "<t") {
		t = ocrCellBreakRe.ReplaceAllString(t, " | ")
	}
	t = ocrBreakTagRe.ReplaceAllString(t, " ")
	t = ocrTagRe.ReplaceAllString(t, " ")
	t = html.UnescapeString(t)
	t = strings.TrimSpace(t)
	if ocrTableSepRe.MatchString(t) {
		return ""
	}
	if strings.HasPrefix(t, "|") {
		t = strings.Trim(t, "| \t")
		cells := strings.Split(t, "|")
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		t = strings.Join(cells, " | ")
	}
	t = ocrMDHeadingRe.ReplaceAllString(t, "")
	t = ocrEscapeRe.ReplaceAllString(t, "$1")
	t = ocrBoldRe.ReplaceAllString(t, "")
	t = ocrItalicRe.ReplaceAllString(t, "$1$2")
	return strings.Join(strings.Fields(t), " ")
}

func headingStyleOf(level int) string {
	switch level {
	case vietnamese_legal.LevelPhan:
		return docxwrite.StyleHeading1
	case vietnamese_legal.LevelChuong:
		return docxwrite.StyleHeading2
	case vietnamese_legal.LevelMuc:
		return docxwrite.StyleHeading3
	}
	return docxwrite.StyleHeading4
}

// headingLevelsOfLines runs the legal parser's heading recognition over the
// lines (one per line) and returns line index → level.
func headingLevelsOfLines(lines []string) map[int]int {
	out := map[int]int{}
	for _, h := range headingsOfLines(lines) {
		out[h.line] = h.level
	}
	return out
}

type lineHeading struct {
	line  int
	level int
	title string
}

func headingsOfLines(lines []string) []lineHeading {
	joined, starts := joinLines(lines)
	var out []lineHeading
	for _, h := range vietnamese_legal.FindHeadings(joined) {
		out = append(out, lineHeading{line: lineAt(starts, h.Start), level: h.Level, title: h.Title})
	}
	return out
}

// joinLines joins lines with "\n" and returns each line's rune offset.
func joinLines(lines []string) (string, []int) {
	var b strings.Builder
	starts := make([]int, len(lines))
	off := 0
	for i, l := range lines {
		if i > 0 {
			b.WriteByte('\n')
			off++
		}
		starts[i] = off
		b.WriteString(l)
		off += utf8.RuneCountInString(l)
	}
	return b.String(), starts
}

// lineAt maps a rune offset to the line holding it.
func lineAt(starts []int, offset int) int {
	i := sort.Search(len(starts), func(i int) bool { return starts[i] > offset })
	if i == 0 {
		return 0
	}
	return i - 1
}

// isUpperLine: letters present and none lower case.
func isUpperLine(t string) bool {
	letters := 0
	for _, r := range t {
		if unicode.IsLetter(r) {
			letters++
			if unicode.IsLower(r) {
				return false
			}
		}
	}
	return letters > 0
}

// looksLikeTitle: the title line under a bare "Chương II" (upper case, short).
func looksLikeTitle(t string) bool {
	return isUpperLine(t) && utf8.RuneCountInString(t) <= 250
}

// LegalDraftPageWarningText is the line flagging a page whose OCR is broken.
func LegalDraftPageWarningText(page string) string {
	return "[OCR hỏng ở trang " + page + " — nhập tay từ bản PDF]"
}

// BuildLegalDraftDocx writes the draft's OCR text as the editor's .docx,
// flagging the pages its OCR report lists as still degenerate.
func BuildLegalDraftDocx(d *types.LegalDocumentDraft) ([]byte, error) {
	var report LegalOCRReport
	if len(d.OCRReport) > 0 {
		_ = json.Unmarshal(d.OCRReport, &report)
	}
	title := strings.TrimSpace(d.DocumentNumber + " " + d.Title)
	return docxwrite.Build(docxwrite.Document{Title: title,
		Paragraphs: LegalDraftParagraphsFromOCR(d.OCRMarkdown, report.DegeneratePages)})
}

// ---------------------------------------------------------------------------
// Edited document → paragraphs

// LegalDraftParagraph is one paragraph of the edited draft.
type LegalDraftParagraph struct {
	Index int
	// Text is the paragraph on one line (breaks and tabs as spaces), trimmed.
	Text string
	// Marker: a "[Trang N]" page marker (style PageMarker or that text).
	Marker bool
	// WarningPage is the page a "[OCR hỏng ở trang N …]" line flags (style
	// PageWarning or that text), 0 for any other paragraph.
	WarningPage int
}

// ReadLegalDraftParagraphs reads the paragraphs of a draft .docx.
func ReadLegalDraftParagraphs(data []byte) ([]LegalDraftParagraph, error) {
	doc, err := docxedit.Open(data)
	if err != nil {
		return nil, err
	}
	styles := map[int]string{}
	for _, p := range docformat.InspectDocx(data).Paragraphs {
		if p.StyleID != nil {
			styles[p.Index] = *p.StyleID
		}
		if p.StyleName != nil && styles[p.Index] == "" {
			styles[p.Index] = *p.StyleName
		}
	}
	paras := doc.Paragraphs()
	out := make([]LegalDraftParagraph, 0, len(paras))
	for _, p := range paras {
		// one line; inner spaces kept (a double space is an OCR slip to report)
		text := strings.TrimSpace(strings.NewReplacer("\n", " ", "\t", " ", "\u00a0", " ").Replace(p.Text))
		para := LegalDraftParagraph{
			Index: p.Index, Text: text,
			Marker: styles[p.Index] == docxwrite.StylePageMarker || draftPageTextRe.MatchString(text),
		}
		if m := draftWarnTextRe.FindStringSubmatch(text); m != nil {
			para.WarningPage, _ = strconv.Atoi(m[1])
		} else if styles[para.Index] == docxwrite.StylePageWarning && text != "" {
			para.WarningPage = -1 // a warning line whose text was edited
		}
		out = append(out, para)
	}
	return out, nil
}

// draftLines are the paragraphs that carry text (no page marker, not
// empty) and their heading levels.
type draftLines struct {
	paras    []LegalDraftParagraph // text paragraphs, document order
	headings []lineHeading         // line = index into paras
	levelOf  map[int]int           // line → level
	joined   string
	starts   []int
}

func newDraftLines(all []LegalDraftParagraph) *draftLines {
	dl := &draftLines{levelOf: map[int]int{}}
	var texts []string
	for _, p := range all {
		if p.Marker || p.WarningPage != 0 || p.Text == "" {
			continue
		}
		dl.paras = append(dl.paras, p)
		texts = append(texts, p.Text)
	}
	dl.joined, dl.starts = joinLines(texts)
	for _, h := range vietnamese_legal.FindHeadings(dl.joined) {
		lh := lineHeading{line: lineAt(dl.starts, h.Start), level: h.Level, title: h.Title}
		dl.headings = append(dl.headings, lh)
		dl.levelOf[lh.line] = lh.level
	}
	return dl
}

// ---------------------------------------------------------------------------
// Structure report

const (
	legalDraftMinArticles     = vietnamese_legal.MinDieuHeadings
	legalDraftMaxPositions    = 10
	legalDraftMaxOCRSuspects  = 50
	legalDraftSeverityError   = "error"
	legalDraftSeverityWarning = "warning"
	legalDraftSeverityInfo    = "info"
)

var headingKinds = map[int]string{
	vietnamese_legal.LevelPhan: "phan", vietnamese_legal.LevelChuong: "chuong",
	vietnamese_legal.LevelMuc: "muc", vietnamese_legal.LevelDieu: "dieu",
}

// ocrSuspects are OCR slips worth a look: a lost Đ in NĐ/QĐ of a số hiệu,
// the Latin Eth (Ð) read for Đ, "BQ" for "BỘ" at a line start.
var ocrSuspects = []struct {
	re  *regexp.Regexp
	msg string
}{
	{regexp.MustCompile(`/(?:ND|QD)\b|\b(?:ND|QD)-[A-Za-z]+\b`), "\"%s\": có thể OCR đã mất chữ Đ (NĐ, QĐ)"},
	{regexp.MustCompile(`\x{00D0}\p{L}*`), "\"%s\": chữ Ð (Latin Eth) thay cho Đ"},
	{regexp.MustCompile(`^BQ\s`), "\"%s\": có thể là \"BỘ\" đọc sai"},
}

// AnalyzeLegalDraft builds the structure report of a draft's paragraphs:
// the Phần/Chương/Mục/Điều tree as the legal parser reads it, the articles,
// the khoản/điểm the parser accepted and rejected, the page markers left
// and the OCR slips it can spot. Fewer than three Điều blocks approval;
// everything else is for the reviewer to judge.
func AnalyzeLegalDraft(all []LegalDraftParagraph) *types.LegalDraftStructure {
	r := &types.LegalDraftStructure{
		CheckedAt: time.Now(), Paragraphs: len(all),
		Headings: []types.LegalDraftHeading{}, Articles: []types.LegalDraftArticle{}, Warnings: []types.LegalDraftWarning{},
	}
	var markers, warnings, warnPages []int
	for _, p := range all {
		switch {
		case p.Marker:
			markers = append(markers, p.Index)
		case p.WarningPage != 0:
			warnings = append(warnings, p.Index)
			if p.WarningPage > 0 {
				warnPages = append(warnPages, p.WarningPage)
			}
		}
	}
	dl := newDraftLines(all)
	warn := func(code, severity, msg string, paragraph int, more []int) {
		w := types.LegalDraftWarning{Code: code, Severity: severity, Message: msg, Paragraph: paragraph,
			Blocking: severity == legalDraftSeverityError}
		if len(more) > 1 {
			w.Paragraphs = more
		}
		r.Warnings = append(r.Warnings, w)
		if w.Blocking {
			r.Blocking++
		}
	}

	// headings tree
	open := map[int]int{} // level → index in r.Headings
	for _, h := range dl.headings {
		parent := -1
		for lvl := h.level - 1; lvl >= 1; lvl-- {
			if i, ok := open[lvl]; ok {
				parent = i
				break
			}
		}
		number := ""
		if m := draftHeadingNumRe.FindStringSubmatch(h.title); m != nil {
			number = strings.TrimRight(m[1], ".:")
		}
		para := dl.paras[h.line].Index
		r.Headings = append(r.Headings, types.LegalDraftHeading{
			Level: h.level, Kind: headingKinds[h.level], Number: number, Title: h.title, Paragraph: para, Parent: parent,
		})
		open[h.level] = len(r.Headings) - 1
		for lvl := h.level + 1; lvl <= vietnamese_legal.LevelDieu; lvl++ {
			delete(open, lvl)
		}
		if h.level == vietnamese_legal.LevelDieu {
			r.Articles = append(r.Articles, types.LegalDraftArticle{Number: number, Title: h.title, Paragraph: para})
			if strings.Contains(dl.paras[h.line].Text, "  ") {
				warn("ocr_suspect", legalDraftSeverityWarning, fmt.Sprintf("\"%s\": khoảng trắng kép trong tiêu đề Điều", h.title), para, nil)
			}
			continue
		}
		// a Phần/Chương/Mục needs a title: on its line or the next one
		if draftLabelOnlyRe.MatchString(dl.paras[h.line].Text) {
			next := h.line + 1
			if next >= len(dl.paras) || dl.levelOf[next] != 0 || !looksLikeTitle(dl.paras[next].Text) {
				warn("heading_without_title", legalDraftSeverityWarning,
					fmt.Sprintf("%s chưa có tên (tên viết hoa ở cùng dòng hoặc dòng ngay sau)", h.title), para, nil)
			}
		}
	}

	// articles: count and numbering
	if len(r.Articles) < legalDraftMinArticles {
		warn("too_few_articles", legalDraftSeverityError, fmt.Sprintf(
			"Mới nhận ra %d Điều (cần ít nhất %d): mỗi Điều phải bắt đầu một đoạn bằng \"Điều N.\" kèm tên Điều",
			len(r.Articles), legalDraftMinArticles), -1, nil)
	}
	prev := 0
	for i, a := range r.Articles {
		n, err := strconv.Atoi(a.Number)
		if err != nil {
			continue // "5a": an inserted article, not numbered in sequence
		}
		switch {
		case i == 0 && n != 1:
			warn("article_gap", legalDraftSeverityWarning, fmt.Sprintf("Điều đầu tiên là Điều %d, không phải Điều 1", n), a.Paragraph, nil)
		case i > 0 && n == prev:
			warn("article_duplicate", legalDraftSeverityWarning, fmt.Sprintf("Điều %d xuất hiện hai lần", n), a.Paragraph, nil)
		case i > 0 && n > prev+1:
			missing := fmt.Sprintf("Điều %d", prev+1)
			if n > prev+2 {
				missing = fmt.Sprintf("Điều %d–%d", prev+1, n-1)
			}
			warn("article_gap", legalDraftSeverityWarning, fmt.Sprintf("Thiếu %s giữa Điều %d và Điều %d", missing, prev, n), a.Paragraph, nil)
		case i > 0 && n < prev:
			warn("article_order", legalDraftSeverityWarning, fmt.Sprintf(
				"Điều %d đứng sau Điều %d (đánh số lại: văn bản kèm theo, hay số Điều đọc sai?)", n, prev), a.Paragraph, nil)
		}
		prev = n
	}

	// khoản / điểm
	sub := vietnamese_legal.ParseSubdivisions(dl.joined)
	r.Subdivisions = types.LegalDraftSubdivisions{
		KhoanCandidates: sub.Stats.KhoanCandidates, KhoanAccepted: sub.Stats.KhoanAccepted,
		DiemCandidates: sub.Stats.DiemCandidates, DiemAccepted: sub.Stats.DiemAccepted, Rejected: sub.Stats.AmbiguousRejected,
	}
	if len(sub.Rejected) > 0 {
		var positions []int
		var labels []string
		for _, rej := range sub.Rejected {
			if len(positions) >= legalDraftMaxPositions {
				break
			}
			positions = append(positions, dl.paras[lineAt(dl.starts, rej.Start)].Index)
			label := rej.Label + "."
			if rej.Kind == "diem" {
				label = rej.Label + ")"
			}
			if rej.ArticleNo != "" {
				label += " (Điều " + rej.ArticleNo + ")"
			}
			labels = append(labels, label)
		}
		warn("subdivision_rejected", legalDraftSeverityWarning, fmt.Sprintf(
			"%d khoản/điểm không xếp được vào Điều (sai thứ tự hoặc nằm ngoài Điều): %s", len(sub.Rejected),
			strings.Join(labels, ", ")), positions[0], positions)
	}

	// page markers left from the OCR
	r.PageMarkers = len(markers)
	if len(markers) > 0 {
		shown := markers
		if len(shown) > legalDraftMaxPositions {
			shown = shown[:legalDraftMaxPositions]
		}
		warn("page_marker", legalDraftSeverityInfo, fmt.Sprintf(
			"Còn %d dấu [Trang N] của bản OCR (bỏ qua khi duyệt; nên xóa nếu nó cắt ngang một đoạn)", len(markers)),
			shown[0], shown)
	}

	// pages whose OCR stayed broken: the reviewer types them in from the PDF
	// and then deletes the red line
	if len(warnings) > 0 {
		pages := "?"
		if len(warnPages) > 0 {
			var ps []string
			for _, n := range warnPages {
				ps = append(ps, strconv.Itoa(n))
			}
			pages = strings.Join(ps, ", ")
		}
		warn("ocr_degenerate", legalDraftSeverityError, fmt.Sprintf(
			"OCR hỏng ở trang %s: nhập lại nội dung các trang này từ bản PDF rồi xóa dòng cảnh báo màu đỏ", pages),
			warnings[0], warnings)
	}
	repeats := 0
	for _, p := range dl.paras {
		if repeats < legalDraftMaxOCRSuspects && HasOCRRepetition(p.Text) {
			warn("ocr_repetition", legalDraftSeverityWarning,
				"Một từ lặp lại liên tiếp nhiều lần (OCR lặp vòng): đối chiếu với bản PDF", p.Index, nil)
			repeats++
		}
	}

	// OCR slips
	suspects := 0
	for _, p := range dl.paras {
		for _, s := range ocrSuspects {
			if suspects >= legalDraftMaxOCRSuspects {
				break
			}
			if loc := s.re.FindStringIndex(p.Text); loc != nil {
				warn("ocr_suspect", legalDraftSeverityWarning, fmt.Sprintf(s.msg, wordAround(p.Text, loc[0], loc[1])), p.Index, nil)
				suspects++
			}
		}
	}
	r.Ready = r.Blocking == 0
	return r
}

// wordAround widens text[start:end] to the whitespace-delimited token
// holding it ("/ND" → "14/2023/ND-CP"), without trailing punctuation.
func wordAround(text string, start, end int) string {
	for start > 0 && text[start-1] != ' ' {
		start--
	}
	for end < len(text) && text[end] != ' ' {
		end++
	}
	return strings.TrimRight(text[start:end], ".,;:)")
}

// ---------------------------------------------------------------------------
// Approved draft → vbpl-style HTML

// BuildLegalDraftHTML renders the reviewed paragraphs as a vbpl.vn page:
// flat <p> blocks with the prov-* classes internal/vbpl reads. Before the
// first structural heading (header, title, căn cứ) and after the closing
// line ("Nơi nhận", "TM.", "KT.", "TL.") paragraphs are plain; Phần /
// Chương / Mục headings and the title line under a bare label get
// prov-part / prov-chapter / prov-section, "Điều N." prov-article,
// "1." prov-clause, "a)" prov-item, anything else prov-content. Page
// markers are dropped.
func BuildLegalDraftHTML(all []LegalDraftParagraph) []byte {
	dl := newDraftLines(all)
	var b strings.Builder
	b.WriteString("<!DOCTYPE html>\n<html><head><meta charset=\"utf-8\"></head><body>\n")
	const (
		preamble = iota
		body
		closing
	)
	phase := preamble
	seenArticle := false
	titleClass := "" // a bare "Chương II" waits for its title line
	for i, p := range dl.paras {
		level := dl.levelOf[i]
		if phase == preamble && level > 0 {
			phase = body
		}
		if phase == body && seenArticle && level == 0 && draftClosingRe.MatchString(p.Text) {
			phase = closing
		}
		class := ""
		if phase == body {
			switch {
			case level == vietnamese_legal.LevelPhan:
				class = "prov-part"
			case level == vietnamese_legal.LevelChuong:
				class = "prov-chapter"
			case level == vietnamese_legal.LevelMuc:
				class = "prov-section"
			case level == vietnamese_legal.LevelDieu:
				class = "prov-article"
				seenArticle = true
			case titleClass != "" && looksLikeTitle(p.Text):
				class = titleClass
			case seenArticle && draftClauseRe.MatchString(p.Text):
				class = "prov-clause"
			case seenArticle && draftItemRe.MatchString(p.Text):
				class = "prov-item"
			default:
				class = "prov-content"
			}
			titleClass = ""
			if level > 0 && level < vietnamese_legal.LevelDieu && draftLabelOnlyRe.MatchString(p.Text) {
				titleClass = class
			}
		}
		if class == "" {
			b.WriteString("<p>")
		} else {
			b.WriteString(`<p class="` + class + `">`)
		}
		b.WriteString(html.EscapeString(p.Text))
		b.WriteString("</p>\n")
	}
	b.WriteString("</body></html>\n")
	return []byte(b.String())
}
