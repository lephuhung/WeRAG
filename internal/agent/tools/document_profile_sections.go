package tools

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/types"
)

// profileLine is one line of a document as the section detection sees it:
// Index is the paragraph (target) or chunk/line (source) it belongs to, so
// several lines may share an index; Label is the paragraph's NĐ30
// component from the heuristic segmentation ("" when unknown).
type profileLine struct {
	Index int
	Text  string
	Label string
}

// Heading levels, coarsest first. A document is split at the coarsest
// level that occurs at least twice (a quy chế with Chương I–V is split by
// chương, not into forty Điều); a Phụ lục always starts its own section.
const (
	headingNone = iota
	headingPart
	headingChapter
	headingSection
	headingArticle
	headingRoman
	headingNumber
)

var (
	partHeadingRe    = regexp.MustCompile(`(?i)^phần\s+(thứ\s+\S+|[ivxlc]+|\d+)\b`)
	chapterHeadingRe = regexp.MustCompile(`(?i)^chương\s+([ivxlc]+|\d+)\b`)
	sectionHeadingRe = regexp.MustCompile(`(?i)^mục\s+(\d+|[ivxlc]+)\b`)
	articleHeadingRe = regexp.MustCompile(`^Điều\s+\d+[a-zđ]?\s*[.:]`)
	romanHeadingRe   = regexp.MustCompile(`^[IVXLC]{1,6}\s*[.)\-–]\s*\S`)
	numberHeadingRe  = regexp.MustCompile(`^\d{1,2}\s*[.)]\s*\p{L}`)
	appendixRe       = regexp.MustCompile(`(?i)^phụ\s+lục\b`)
)

// headingMaxRunes: longer lines are body text even when numbered ("1. Giao
// Sở Tài chính chủ trì…" is a heading only when short). Điều lines are
// headings at any length ("Điều 1. Ban hành kèm theo Quyết định này…").
const (
	headingMaxRunes      = 160
	numberedHeadingRunes = 110
	sectionTitleRunes    = 90
	// unheadedBlockLines splits a document without headings into blocks.
	unheadedBlockLines = 40
)

// headingLevel classifies a line as a heading of some level.
func headingLevel(text string) int {
	n := utf8.RuneCountInString(text)
	switch {
	case articleHeadingRe.MatchString(text):
		return headingArticle
	case n > headingMaxRunes:
		return headingNone
	case partHeadingRe.MatchString(text):
		return headingPart
	case chapterHeadingRe.MatchString(text):
		return headingChapter
	case sectionHeadingRe.MatchString(text):
		return headingSection
	case n > numberedHeadingRunes:
		return headingNone
	case romanHeadingRe.MatchString(text):
		return headingRoman
	case numberHeadingRe.MatchString(text):
		return headingNumber
	}
	return headingNone
}

// headerLabels are the NĐ30 components of the header and the tail: not
// content, so they belong to no section.
var headerLabels = map[string]bool{
	"quoc_hieu": true, "tieu_ngu": true, "co_quan_chu_quan": true, "co_quan_ban_hanh": true,
	"so_ky_hieu": true, "dia_danh_ngay_thang": true, "trich_yeu": true, "tham_quyen_ban_hanh": true,
	"do_mat": true, "do_khan": true, "header_left_other": true, "header_right_other": true, "header_other": true,
	"noi_nhan": true, "chuc_danh": true, "nguoi_ky": true, "signature": true,
}

// detectProfileSections splits a document's lines into sections: "Căn cứ"
// for the legal bases (NĐ30 label can_cu), then one section per heading of
// the coarsest level that occurs at least twice (Phần, Chương, Mục, Điều,
// "I.", "1."), each Phụ lục on its own. Text before the first heading is
// "Mở đầu". A document without headings is cut into blocks of
// unheadedBlockLines lines named by their first line. Header and signature
// lines (by label) belong to no section. From/To are inclusive indexes.
func detectProfileSections(lines []profileLine) []types.DocumentProfileSection {
	var body []profileLine
	for _, l := range lines {
		l.Text = strings.TrimSpace(strings.NewReplacer("\t", " ", "\n", " ").Replace(l.Text))
		if !hasWord(l.Text) || headerLabels[l.Label] {
			continue // rule lines ("_____") and the header and tail
		}
		body = append(body, l)
	}
	if len(body) == 0 {
		return nil
	}

	// the split level: the coarsest one with at least two headings
	counts := map[int]int{}
	for _, l := range body {
		if l.Label == "can_cu" {
			continue
		}
		counts[headingLevel(l.Text)]++
	}
	level := headingNone
	for lv := headingPart; lv <= headingNumber; lv++ {
		if counts[lv] >= 2 {
			level = lv
			break
		}
	}
	if level == headingNone {
		for lv := headingPart; lv <= headingArticle; lv++ {
			if counts[lv] == 1 {
				level = lv // one Chương / Điều still names the content
				break
			}
		}
	}

	var out []types.DocumentProfileSection
	open := func(title string, idx int) {
		if n := len(out); n > 0 && out[n-1].From == idx && out[n-1].To == idx {
			return // several headings in one chunk: the first names it
		}
		out = append(out, types.DocumentProfileSection{Title: clipRunes(title, sectionTitleRunes), From: idx, To: idx})
	}
	extend := func(idx int) {
		if n := len(out); n > 0 && idx > out[n-1].To {
			out[n-1].To = idx
		}
	}
	inBases := false
	for i, l := range body {
		if l.Label == "can_cu" {
			if !inBases {
				open("Căn cứ", l.Index)
				inBases = true
			}
			extend(l.Index)
			continue
		}
		afterBases := inBases
		inBases = false
		appendix := appendixRe.MatchString(l.Text) && utf8.RuneCountInString(l.Text) <= headingMaxRunes
		switch {
		case appendix || (level != headingNone && headingLevel(l.Text) == level):
			open(headingTitle(body, i), l.Index)
		case len(out) == 0 || afterBases:
			// content before the first heading (blockSections names
			// the untitled runs of a document without headings)
			title := ""
			if level != headingNone {
				title = "Mở đầu"
			}
			out = append(out, types.DocumentProfileSection{Title: title, From: l.Index, To: l.Index})
		}
		extend(l.Index)
	}

	if level == headingNone {
		out = blockSections(out, body)
	}
	return out
}

// blockSections cuts the content outside Căn cứ and the Phụ lục sections of
// a document without headings into blocks of unheadedBlockLines lines (or
// one "Nội dung" section when it is short).
func blockSections(found []types.DocumentProfileSection, body []profileLine) []types.DocumentProfileSection {
	var out []types.DocumentProfileSection
	var run []profileLine
	flush := func() {
		if len(run) == 0 {
			return
		}
		if len(run) <= unheadedBlockLines {
			out = append(out, types.DocumentProfileSection{Title: "Nội dung", From: run[0].Index, To: run[len(run)-1].Index})
		} else {
			for k := 0; k < len(run); k += unheadedBlockLines {
				end := min(k+unheadedBlockLines, len(run)) - 1
				out = append(out, types.DocumentProfileSection{
					Title: clipRunes(run[k].Text, 60), From: run[k].Index, To: run[end].Index,
				})
			}
		}
		run = nil
	}
	titled := map[int]types.DocumentProfileSection{}
	for _, s := range found {
		if s.Title != "" {
			titled[s.From] = s
		}
	}
	for i := 0; i < len(body); i++ {
		if s, ok := titled[body[i].Index]; ok {
			flush()
			out = append(out, s)
			for i+1 < len(body) && body[i+1].Index <= s.To {
				i++
			}
			continue
		}
		run = append(run, body[i])
	}
	flush()
	return out
}

func hasWord(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// bareHeadingRe is a heading that is only its label and number.
var bareHeadingRe = regexp.MustCompile(`(?i)^(phần|chương|mục|phụ\s+lục)(\s+[\p{L}\d]+)?\s*[.:]?$`)

// headingTitle names the section opened by body[i]: the heading line, plus
// the next line when the heading is only a number ("Chương II" over
// "QUY ĐỊNH CỤ THỂ", "PHỤ LỤC I" over its title).
func headingTitle(body []profileLine, i int) string {
	title := body[i].Text
	if bareHeadingRe.MatchString(title) && i+1 < len(body) {
		next := body[i+1].Text
		if utf8.RuneCountInString(next) <= sectionTitleRunes && headingLevel(next) == headingNone && body[i+1].Label != "can_cu" {
			title = strings.TrimRight(title, " .:") + ". " + next
		}
	}
	return title
}

// sectionsFromHeaders groups a chunked source by its chunks' context
// headers ("Chương I > Điều 3", or markdown "# A\n## B"): at the shallowest
// header depth whose value changes between chunks, each run of chunks
// sharing that value is a section. nil when the headers do not vary.
func sectionsFromHeaders(headers []string) []types.DocumentProfileSection {
	paths := make([][]string, len(headers))
	maxDepth := 0
	for i, h := range headers {
		for _, seg := range strings.FieldsFunc(h, func(r rune) bool { return r == '\n' || r == '>' }) {
			if seg = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(seg), "#")); seg != "" {
				paths[i] = append(paths[i], seg)
			}
		}
		maxDepth = max(maxDepth, len(paths[i]))
	}
	for depth := 0; depth < maxDepth; depth++ {
		distinct := map[string]bool{}
		for _, p := range paths {
			if depth < len(p) {
				distinct[p[depth]] = true
			}
		}
		if len(distinct) < 2 {
			continue
		}
		var out []types.DocumentProfileSection
		for i, p := range paths {
			key := ""
			if depth < len(p) {
				key = p[depth]
			}
			n := len(out)
			switch {
			case n > 0 && (key == "" || key == out[n-1].Title):
				out[n-1].To = i
			case key == "":
				out = append(out, types.DocumentProfileSection{Title: "Mở đầu", From: i, To: i})
			default:
				out = append(out, types.DocumentProfileSection{Title: clipRunes(key, sectionTitleRunes), From: i, To: i})
			}
		}
		return out
	}
	return nil
}
