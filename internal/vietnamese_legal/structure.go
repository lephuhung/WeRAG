// Package vietnamese_legal holds the Vietnamese legal-document domain logic
// ported from AIRAG (lephuhung/AIRAG, branch feat/langgraph-v2). It is a leaf
// package: it must not import WeKnora application, service or repository
// packages so the domain layer stays isolated and cheap to rebase upstream.
//
// structure.go is the Go port of AIRAG's services/parsing/heading_path.py:
// it recovers the Phần/Chương/Mục/Điều hierarchy and the Khoản/Điểm markers
// from plain parsed text — Docling-style parsers do not recognise "Điều N."
// as a structural heading, but the structure is still present in the text.
//
// Positions returned by this package are RUNE offsets, matching the
// chunker's Chunk.Start/End convention (Python str offsets map 1:1).
package vietnamese_legal

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Heading levels; smaller number = higher level. A new heading at level L
// clears the carried state of every level >= L.
const (
	LevelPhan   = 1 // Phần
	LevelChuong = 2 // Chương
	LevelMuc    = 3 // Mục
	LevelDieu   = 4 // Điều
)

var headingLevels = map[string]int{
	"phần":   LevelPhan,
	"chương": LevelChuong,
	"mục":    LevelMuc,
	"điều":   LevelDieu,
}

// A single structural heading line inside parsed markdown/plain text. The
// pattern tolerates the messy inputs seen in the corpus: optional markdown
// '#' prefix (inconsistent depth), bold markers ("**Điều 12.** ..."),
// "Chương III" roman numerals, "Phần 1."/"PHẦN THỨ NHẤT", "Mục 2".
// Roman numerals must be UPPERCASE — under (?i) a lowercase 'c' in
// "Mục c khoản 2..." would otherwise match [IVXLCDM].
var headingRe = regexp.MustCompile(
	`(?im)^[ \t]*(?:#{1,6}[ \t]*)?(?:[*_]{1,3}[ \t]*)?` +
		`(?P<kw>Phần|Chương|Mục|Điều)[ \t]+` +
		`(?P<num>thứ[ \t]+[\p{L}\p{N}_]+|(?-i:[IVXLCDM]+)\b|\d+[a-zA-Z]?)` +
		`(?P<rest>[^\n]*)`,
)

// "Điều" only counts as a heading when the number is followed by "." or ":"
// ("Điều 17. Tiêu đề"; a closing bold marker may precede the colon:
// "**Điều 17**. ..."). This blocks body lines that happen to open with a
// reference ("Điều 5 và Điều 6 Nghị định này..."). A bare "Điều N" at end of
// line (OCR dropped the dot) is rescued by the next-line lookahead in
// dieuRestOK.
var dieuRestRe = regexp.MustCompile(`^[ \t]*[*_]{0,3}[ \t]*[.:]`)

// A "rest" tail that looks like a CONTINUED structural reference rather than
// a real title ("Chương III Nghị định này", "khoản 2 Điều 7") — a keyword
// followed by a number. Distinct from genuine titles that contain the keyword
// without a number ("Chương IX ĐIỀU KHOẢN THI HÀNH").
var refContRe = regexp.MustCompile(
	`(?i)^(?:Phần|Chương|Mục|Điều|khoản|điểm)[ \t]+(?:thứ[ \t]+[\p{L}\p{N}_]+|(?-i:[IVXLCDM]+)\b|\d)`,
)

// Markdown emphasis markers (bold/italic) — stripped from titles and gates.
var emphRe = regexp.MustCompile(`[*_]+`)

var wsRe = regexp.MustCompile(`\s+`)

// A chunk merging many short articles (appendix form tables) — cap the
// number of Điều components so metadata does not grow unboundedly.
const maxDieuPerChunk = 8

// Heading is one accepted structural heading: rune offset, level and the
// normalized title.
type Heading struct {
	Start int // rune offset into the source text
	Level int
	Title string
}

// byteHeading is the internal byte-offset form produced by the regex scan.
type byteHeading struct {
	start int // byte offset
	level int
	title string
}

// dieuComponentRe extracts the article number from a heading_path component:
// "Điều 17. Hồ sơ..." / "Điều 5a: ...".
var dieuComponentRe = regexp.MustCompile(`(?i)^\s*Điều\s+(\d+[a-zA-Z]?)\b`)

// ExtractArticleNos pulls the Điều numbers out of a chunk's heading_path
// components (["Chương II", "Điều 17. ..."] → ["17"]). Powers the article_nos
// metadata so section lookup can match "Điều 17" exactly — without regex
// over the heading_path string (avoids "Điều 3" matching "Điều 30").
func ExtractArticleNos(components []string) []string {
	out := make([]string, 0, len(components))
	for _, comp := range components {
		if m := dieuComponentRe.FindStringSubmatch(comp); m != nil {
			no := strings.ToLower(m[1])
			if !contains(out, no) {
				out = append(out, no)
			}
		}
	}
	return out
}

// ExtractArticleNosString is the string form of ExtractArticleNos for a
// ">"-joined heading_path ("Chương II > Điều 17. ... > Điều 18. ...").
func ExtractArticleNosString(headingPath string) []string {
	var components []string
	for _, c := range strings.Split(headingPath, ">") {
		components = append(components, strings.TrimSpace(c))
	}
	return ExtractArticleNos(components)
}

func contains(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

// FindHeadings lists the accepted Phần/Chương/Mục/Điều headings in text
// order. Used by DeriveHeadingPaths (per-chunk paths) and by the legal
// chunker (structural boundaries). Positions are rune offsets.
func FindHeadings(text string) []Heading {
	raw := iterHeadings(text)
	out := make([]Heading, 0, len(raw))
	for _, h := range raw {
		out = append(out, Heading{
			Start: utf8.RuneCountInString(text[:h.start]),
			Level: h.level,
			Title: h.title,
		})
	}
	return out
}

// HasLegalStructure reports whether the text carries a Vietnamese legal
// structure — at least MinDieuHeadings accepted "Điều N." headings. Documents
// below the threshold (công văn, tờ trình, ...) should use a generic
// splitter. Mirrors AIRAG LegalDocumentChunker.MIN_DIEU_HEADINGS.
const MinDieuHeadings = 3

func HasLegalStructure(text string) bool {
	dieu := 0
	for _, h := range iterHeadings(text) {
		if h.level == LevelDieu {
			dieu++
		}
	}
	return dieu >= MinDieuHeadings
}

// upperRestOK gates Phần/Chương/Mục headings: the line tail must look like a
// TITLE, not a reference. Accepts: empty ("Mục 1"), punctuation ("Phần 1.
// ..."), uppercase ("Chương II NHỮNG QUY ĐỊNH CHUNG"). Rejects: lowercase
// ("Chương V của Luật này..." — a line-wrapped citation) and a continued
// structural reference ("Mục 2 Chương III Nghị định này").
func upperRestOK(rest string) bool {
	rest = strings.TrimSpace(emphRe.ReplaceAllString(rest, ""))
	if rest == "" {
		return true
	}
	first, _ := utf8.DecodeRuneInString(rest)
	if strings.ContainsRune(".:-–—", first) {
		return true
	}
	if refContRe.MatchString(rest) {
		return false
	}
	return !unicode.IsLower(first)
}

// dieuRestOK gates Điều headings: either a [.:] after the number, or a bare
// "Điều N" at end of line whose NEXT line opens with an uppercase letter
// (a title split by OCR / a dropped dot).
func dieuRestOK(rest, text string, matchEnd int) bool {
	if dieuRestRe.MatchString(rest) {
		return true
	}
	if strings.TrimSpace(emphRe.ReplaceAllString(rest, "")) != "" {
		return false // text after the number but no [.:] → reference
	}
	// rest is empty → look at the next line: "Phạm vi điều chỉnh" (upper) is a
	// title; "và khoản 2..." (lower) / end of text is a wrapped reference.
	lines := strings.Split(text[matchEnd:], "\n")
	for _, line := range lines[1:] {
		s := strings.TrimSpace(emphRe.ReplaceAllString(line, ""))
		if s == "" {
			continue
		}
		first, _ := utf8.DecodeRuneInString(s)
		return unicode.IsUpper(first) && !refContRe.MatchString(s)
	}
	return false
}

func iterHeadings(text string) []byteHeading {
	var out []byteHeading
	for _, m := range headingRe.FindAllStringSubmatchIndex(text, -1) {
		kw := strings.ToLower(text[m[2]:m[3]])
		level, ok := headingLevels[kw]
		if !ok {
			continue
		}
		rest := ""
		if m[6] >= 0 {
			rest = text[m[6]:m[7]]
		}
		if level == LevelDieu {
			if !dieuRestOK(rest, text, m[1]) {
				continue
			}
		} else if !upperRestOK(rest) {
			continue
		}
		title := emphRe.ReplaceAllString(text[m[2]:m[3]]+" "+text[m[4]:m[5]]+rest, " ")
		title = wsRe.ReplaceAllString(title, " ")
		title = strings.TrimSpace(title)
		title = strings.TrimRight(title, " .:")
		if title != "" {
			out = append(out, byteHeading{start: m[0], level: level, title: title})
		}
	}
	return out
}

// DeriveHeadingPaths returns the heading_path for EACH chunk (same order as
// chunkTexts). Chunks must be in document order — the Phần/Chương/Mục/Điều
// state carries forward across chunks that contain no header of their own.
//
// Convention: upper levels (Phần > Chương > Mục) come from carried state,
// plus EVERY "Điều N." appearing in the chunk (a merged chunk lists all of
// them so lookup hits any); a chunk with no header inherits the open Điều
// from the previous chunk.
func DeriveHeadingPaths(chunkTexts []string) [][]string {
	state := map[int]string{}
	paths := make([][]string, 0, len(chunkTexts))
	for _, text := range chunkTexts {
		headings := iterHeadings(text)
		dieuAtStart := state[LevelDieu]
		// Content BEFORE the chunk's first header still belongs to the open
		// Điều of the previous chunk — keep it in the path so a lookup for
		// that Điều still hits this chunk.
		startsMidArticle := len(headings) > 0 && dieuAtStart != "" &&
			strings.TrimSpace(text[:headings[0].start]) != ""

		var dieuInChunk []string
		for _, h := range headings {
			for lv := range state {
				if lv >= h.level {
					delete(state, lv)
				}
			}
			state[h.level] = h.title
			if h.level == LevelDieu {
				dieuInChunk = append(dieuInChunk, h.title)
			}
		}

		var upper []string
		for _, lv := range []int{LevelPhan, LevelChuong, LevelMuc} {
			if t, ok := state[lv]; ok {
				upper = append(upper, t)
			}
		}
		var dieuComps []string
		if dieuAtStart != "" && (len(headings) == 0 || startsMidArticle) {
			dieuComps = append(dieuComps, dieuAtStart)
		}
		dieuComps = append(dieuComps, dieuInChunk...)
		// dedup preserving order (the carried-open Điều may equal the first
		// header of this chunk)
		seen := map[string]bool{}
		deduped := dieuComps[:0]
		for _, d := range dieuComps {
			if !seen[d] {
				seen[d] = true
				deduped = append(deduped, d)
			}
		}
		if len(deduped) > maxDieuPerChunk {
			deduped = deduped[:maxDieuPerChunk]
		}
		paths = append(paths, append(upper, deduped...))
	}
	return paths
}

// Optional bullet prefix ("- a)", "* 1.") — parsers render khoản/điểm of
// legal documents as markdown list items; OCR/plain text keeps the raw
// marker at line start.
const bulletPrefix = `(?:[-*•][ \t]+)?`

// Optional markdown heading prefix ("## 1.", "## a)") — parsers sometimes
// render a bold khoản/điểm as a markdown heading ("## a) 3 (được bãi bỏ)").
const headingPrefix = `(?:#{1,6}[ \t]*)?`

// gluedDiemRe rescues "N. x)" / "N. x" line ends: the superscript footnote
// marker of consolidated documents glued onto the Điểm marker — N is a
// footnote number, the letter is the real Điểm. NEVER a Khoản (a real
// khoản never opens with a letter marker).
var gluedDiemRe = regexp.MustCompile(
	`(?m)^[ \t]{0,3}` + bulletPrefix + `\d{1,2}\.[ \t]+([a-zđ])(?:[ \t]*\)|[ \t]*$)`,
)

// khoanCandidateRe is the syntactic Khoản candidate ("N. text"). The Python
// original carries a negative lookahead rejecting glued footnote tails
// ("N. x)" / "N. x$"); Go/RE2 has no lookahead, so the check is applied
// manually on the captured first body character in subdivisionEvents.
var khoanCandidateRe = regexp.MustCompile(
	`(?m)^[ \t]{0,3}` + headingPrefix + bulletPrefix + `(\d{1,2})\.[ \t]+(\S)`,
)

// gluedTailRe matches the remainder of a line when a glued footnote tail
// follows the marker letter: optional spaces then ')' or end-of-line.
var gluedTailRe = regexp.MustCompile(`^[ \t]*(?:\)|$)`)

// diemCandidateRe is the syntactic Điểm candidate ("a) text").
var diemCandidateRe = regexp.MustCompile(
	`(?m)^[ \t]{0,3}` + headingPrefix + bulletPrefix + `([a-zđ])\)[ \t]+\S`,
)

// diemOrder is the accepted ordering of Điểm letters (Vietnamese collation,
// includes đ, skips letters not used as list markers).
const diemOrder = "abcdđeghiklmnopqrstuvxy"

// khoanMax bounds the accepted khoản number.
const khoanMax = 30

// Subdivision is one accepted Khoản/Điểm marker, bound to its parent Điều
// (and parent Khoản for điểm). Start is a rune offset.
type Subdivision struct {
	Kind        string // "khoan" | "diem"
	Label       string
	Start       int // rune offset
	ArticleNo   string
	ParentKhoan string // "" when none
}

// byteSubdivision is the internal byte-offset form.
type byteSubdivision struct {
	kind        string
	label       string
	start       int // byte offset
	articleNo   string
	parentKhoan string
}

// SubdivisionMetadata is the per-chunk Khoản/Điểm metadata.
type SubdivisionMetadata struct {
	KhoanNos        []string
	DiemLabels      []string
	SubdivisionRefs []string
}

// LegalMetadata is the persisted per-chunk legal metadata. Field names and
// JSON keys mirror AIRAG's chunk metadata so downstream retrieval code can
// resolve "Điều 17" / "khoản 2 Điều 17" exactly instead of regex-matching
// body text ("Điều 3" must not hit "Điều 30").
type LegalMetadata struct {
	// HeadingPath is the structural breadcrumb active at this chunk
	// (["Chương II ...", "Điều 17. ..."]); a merged chunk lists every Điều.
	HeadingPath []string `json:"heading_path,omitempty"`
	// ArticleNos lists the bare Điều numbers derived from HeadingPath
	// (["17", "18"]) — lowercase to keep "5a"-style suffixes comparable.
	ArticleNos []string `json:"article_nos,omitempty"`
	// KhoanNos lists the khoản numbers open or introduced in this chunk.
	KhoanNos []string `json:"khoan_nos,omitempty"`
	// DiemLabels lists the điểm letters open or introduced in this chunk.
	DiemLabels []string `json:"diem_labels,omitempty"`
	// SubdivisionRefs are compact lookup keys: "khoan:2", "khoan:2/diem:a".
	SubdivisionRefs []string `json:"subdivision_refs,omitempty"`
	// SchemaVersion lets future migrations distinguish metadata shapes.
	SchemaVersion int `json:"subdivision_schema_version,omitempty"`
}

// SubdivisionParseStats records accept/reject counts for diagnostics.
type SubdivisionParseStats struct {
	KhoanCandidates   int
	KhoanAccepted     int
	DiemCandidates    int
	DiemAccepted      int
	AmbiguousRejected int
}

// SubdivisionParseResult bundles the accepted markers plus stats.
type SubdivisionParseResult struct {
	Subdivisions []Subdivision
	Stats        SubdivisionParseStats
}

// subdivisionEvent is a structural event at a byte position: kind 0 =
// heading, 1 = khoản candidate, 2 = điểm candidate.
type subdivisionEvent struct {
	start int
	kind  int
	head  byteHeading
	label string
}

func subdivisionEvents(text string) []subdivisionEvent {
	var events []subdivisionEvent
	for _, h := range iterHeadings(text) {
		events = append(events, subdivisionEvent{start: h.start, kind: 0, head: h})
	}
	// Khoản candidates: the Python lookahead (?!glued_tail) is applied by
	// hand — the char after "N. " must not open a glued footnote tail.
	for _, m := range khoanCandidateRe.FindAllStringSubmatchIndex(text, -1) {
		// m[4]:m[5] is the first non-space char of the khoản body. If it is
		// [a-zđ] AND the rest of the line is spaces + ')' or spaces + EOL,
		// this is a glued footnote tail, not a khoản.
		bodyStart := m[4]
		r, size := utf8.DecodeRuneInString(text[bodyStart:])
		if (r >= 'a' && r <= 'z') || r == 'đ' {
			lineEnd := strings.IndexByte(text[bodyStart:], '\n')
			var rest string
			if lineEnd < 0 {
				rest = text[bodyStart+size:]
			} else {
				rest = text[bodyStart+size : bodyStart+lineEnd]
			}
			if gluedTailRe.MatchString(rest) {
				continue // glued footnote tail, not a khoản
			}
		}
		events = append(events, subdivisionEvent{
			start: m[0], kind: 1, label: text[m[2]:m[3]],
		})
	}
	for _, m := range diemCandidateRe.FindAllStringSubmatchIndex(text, -1) {
		events = append(events, subdivisionEvent{
			start: m[0], kind: 2, label: text[m[2]:m[3]],
		})
	}
	for _, m := range gluedDiemRe.FindAllStringSubmatchIndex(text, -1) {
		events = append(events, subdivisionEvent{
			start: m[0], kind: 2, label: text[m[2]:m[3]],
		})
	}
	// Stable sort by (start, kind): a heading at a position always precedes
	// a marker candidate at the same position.
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].start != events[j].start {
			return events[i].start < events[j].start
		}
		return events[i].kind < events[j].kind
	})
	return events
}

// ParseSubdivisions parses Khoản/Điểm markers inside text with a conservative
// state machine.
//
// State is only active inside an accepted Điều heading; any new structural
// heading (including Phần/Chương/Mục) resets it. A Điều's first khoản must be
// 1, then strictly increase (gaps allowed — OCR can drop a line); equal or
// decreasing candidates are rejected. Điểm requires an open khoản, must
// start at 'a', then advance along diemOrder. Every rejected syntactic
// candidate counts into AmbiguousRejected.
func ParseSubdivisions(text string) SubdivisionParseResult {
	var article *string
	var lastKVal int
	var lastKLabel string
	hasK := false
	lastDIdx := -1
	hasD := false
	var subs []Subdivision
	var st SubdivisionParseStats

	for _, ev := range subdivisionEvents(text) {
		switch ev.kind {
		case 0:
			if ev.head.level == LevelDieu {
				if m := dieuComponentRe.FindStringSubmatch(ev.head.title); m != nil {
					a := strings.ToLower(m[1])
					article = &a
				} else {
					article = nil
				}
			} else {
				article = nil
			}
			hasK, hasD = false, false
			lastKVal, lastDIdx = 0, -1
			lastKLabel = ""
		case 1:
			st.KhoanCandidates++
			val := atoiSafe(ev.label)
			ok := article != nil && val >= 1 && val <= khoanMax &&
				((hasK && val > lastKVal) || (!hasK && val == 1))
			if ok {
				st.KhoanAccepted++
				hasK, hasD = true, false
				lastKVal, lastKLabel, lastDIdx = val, ev.label, -1
				subs = append(subs, Subdivision{
					Kind: "khoan", Label: ev.label,
					Start:     utf8.RuneCountInString(text[:ev.start]),
					ArticleNo: *article,
				})
			} else {
				st.AmbiguousRejected++
			}
		default:
			st.DiemCandidates++
			idx := strings.IndexRune(diemOrder, []rune(ev.label)[0])
			ok := article != nil && hasK && idx >= 0 &&
				((hasD && idx > lastDIdx) || (!hasD && idx == 0))
			if ok {
				st.DiemAccepted++
				hasD = true
				lastDIdx = idx
				subs = append(subs, Subdivision{
					Kind: "diem", Label: ev.label,
					Start:     utf8.RuneCountInString(text[:ev.start]),
					ArticleNo: *article, ParentKhoan: lastKLabel,
				})
			} else {
				st.AmbiguousRejected++
			}
		}
	}
	return SubdivisionParseResult{Subdivisions: subs, Stats: st}
}

// FindSubdivisions is the convenience form returning only accepted markers.
func FindSubdivisions(text string) []Subdivision {
	return ParseSubdivisions(text).Subdivisions
}

func atoiSafe(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return -1
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// DeriveSubdivisionMetadata infers per-chunk Khoản/Điểm metadata,
// sequence-aware. The Điều/Khoản/Điểm state carries across chunk boundaries:
// a continuation chunk with no marker still inherits the ref open at its
// first character. headingPaths (optional) supply the Điều context for older
// chunks that no longer repeat the heading — when the derived Điều changes,
// the Khoản/Điểm state resets.
func DeriveSubdivisionMetadata(chunkTexts []string, headingPaths [][]string) []SubdivisionMetadata {
	var article string
	hasArticle := false
	curKVal, curDIdx := 0, -1
	curKLabel, curDLabel := "", ""
	hasK, hasD := false, false

	out := make([]SubdivisionMetadata, 0, len(chunkTexts))
	for i, raw := range chunkTexts {
		text := raw
		headings := iterHeadings(text)
		hasDieu := false
		for _, h := range headings {
			if h.level == LevelDieu {
				hasDieu = true
				break
			}
		}
		if !hasDieu && headingPaths != nil && i < len(headingPaths) {
			nos := ExtractArticleNos(headingPaths[i])
			if len(nos) > 1 {
				hasArticle, hasK, hasD = false, false, false
				article, curKLabel, curDLabel = "", "", ""
				curKVal, curDIdx = 0, -1
			} else if len(nos) == 1 && (!hasArticle || nos[0] != article) {
				hasArticle = true
				article = nos[0]
				hasK, hasD = false, false
				curKLabel, curDLabel = "", ""
				curKVal, curDIdx = 0, -1
			}
		}

		var khoans, diems, refs []string
		add := func(k, d string) {
			if k == "" {
				return
			}
			if !contains(khoans, k) {
				khoans = append(khoans, k)
			}
			ref := "khoan:" + k
			if !contains(refs, ref) {
				refs = append(refs, ref)
			}
			if d != "" {
				if !contains(diems, d) {
					diems = append(diems, d)
				}
				ref = "khoan:" + k + "/diem:" + d
				if !contains(refs, ref) {
					refs = append(refs, ref)
				}
			}
		}
		inherit := func(upto int) {
			if curKLabel != "" && strings.TrimSpace(text[:upto]) != "" {
				add(curKLabel, curDLabel)
			}
		}

		for _, ev := range subdivisionEvents(text) {
			switch ev.kind {
			case 0:
				inherit(ev.start)
				if ev.head.level == LevelDieu {
					if m := dieuComponentRe.FindStringSubmatch(ev.head.title); m != nil {
						hasArticle = true
						article = strings.ToLower(m[1])
					} else {
						hasArticle = false
						article = ""
					}
				} else {
					hasArticle = false
					article = ""
				}
				hasK, hasD = false, false
				curKLabel, curDLabel = "", ""
				curKVal, curDIdx = 0, -1
			case 1:
				val := atoiSafe(ev.label)
				if hasArticle && val >= 1 && val <= khoanMax &&
					((hasK && val > curKVal) || (!hasK && val == 1)) {
					inherit(ev.start)
					hasK, hasD = true, false
					curKVal, curKLabel, curDIdx = val, ev.label, -1
					curDLabel = ""
					add(ev.label, "")
				}
			default:
				idx := strings.IndexRune(diemOrder, []rune(ev.label)[0])
				if hasArticle && hasK && idx >= 0 &&
					((hasD && idx > curDIdx) || (!hasD && idx == 0)) {
					inherit(ev.start)
					hasD = true
					curDIdx, curDLabel = idx, ev.label
					add(curKLabel, ev.label)
				}
			}
		}
		if len(refs) == 0 {
			inherit(len(text))
		}
		out = append(out, SubdivisionMetadata{
			KhoanNos:        khoans,
			DiemLabels:      diems,
			SubdivisionRefs: refs,
		})
	}
	return out
}
