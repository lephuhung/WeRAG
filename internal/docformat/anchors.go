package docformat

import (
	"regexp"
	"sort"
	"strings"
	"sync"
)

// Anchors ("dòng mốc") are the few body lines the judgment model sees in
// place of the whole nội dung: the opening, the level-1 headings, the
// closing sentence, the last lines, and per type the lines its skill asks
// about (Điều, "QUYẾT ĐỊNH:", thời gian/địa điểm of a biên bản …).
//
// They are declared in the rule JSON ("anchors"): _base.json holds the
// default list, a type file adds its own, and a type anchor with the same
// id replaces the base one in place. Kinds:
//
//   - "regex" (default): paragraphs whose fold()ed text — upper case,
//     diacritics stripped, Đ→D — matches Regex, at most Max of them; when
//     more match, the first Max-1 and the last are kept, so the last Điều
//     stays visible.
//   - "head": the first Max body paragraphs.
//   - "tail": the last Max body paragraphs.
//
// Only paragraphs labelled noi_dung are scanned. Regex anchors are applied
// first, in list order, then head and tail; a paragraph matched by several
// anchors is listed once, under the first, with the other regex anchors'
// descriptions added (the last Điều that also ends in "./.").

// Anchor kinds.
const (
	AnchorRegex = "regex"
	AnchorHead  = "head"
	AnchorTail  = "tail"
)

// anchorTextRunes caps an anchor line.
const anchorTextRunes = 160

// Anchor is one anchor as written in the rule JSON.
type Anchor struct {
	ID    string `json:"id"`
	Kind  string `json:"kind,omitempty"`
	Regex string `json:"regex,omitempty"`
	Max   int    `json:"max,omitempty"`
	Desc  string `json:"desc,omitempty"`
}

// AnchorLine is one body line picked by an anchor.
type AnchorLine struct {
	Kind string `json:"kind"` // the anchor id
	Para int    `json:"para"`
	Text string `json:"text"`
	// Desc is the anchor's description, shown to the judgment model.
	Desc string `json:"desc,omitempty"`
	// IDs are every anchor that picked the line (Kind first): the
	// judgment rules (judgment.go) look lines up by anchor id.
	IDs []string `json:"ids,omitempty"`
	// Full is the untruncated text when Text was cut ("…"), so the
	// judgment rules read what the brief had to leave out.
	Full string `json:"-"`
	// SizePt, Bold and Italic are the line's measured cỡ chữ and kiểu
	// chữ, shown in the judgment brief.
	SizePt *float64 `json:"size_pt,omitempty"`
	Bold   bool     `json:"bold,omitempty"`
	Italic bool     `json:"italic,omitempty"`
}

// mergeAnchors returns base with each spec anchor of the same id replacing
// the base one in place, followed by the spec's new anchors.
func mergeAnchors(base, spec []Anchor) []Anchor {
	own := map[string]Anchor{}
	for _, a := range spec {
		own[a.ID] = a
	}
	out := make([]Anchor, 0, len(base)+len(spec))
	for _, a := range base {
		if o, ok := own[a.ID]; ok {
			out = append(out, o)
			delete(own, a.ID)
		} else {
			out = append(out, a)
		}
	}
	for _, a := range spec {
		if _, ok := own[a.ID]; ok {
			out = append(out, a)
			delete(own, a.ID)
		}
	}
	return out
}

var anchorRes sync.Map // pattern → *regexp.Regexp (nil when invalid)

func anchorRegexp(pattern string) *regexp.Regexp {
	if v, ok := anchorRes.Load(pattern); ok {
		return v.(*regexp.Regexp)
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		re = nil
	}
	anchorRes.Store(pattern, re)
	return re
}

// bodyLine is one non-empty noi_dung paragraph (or side of a tab-split one).
type bodyLine struct {
	para int
	text string
	data bool // a cell of a data table: never an opening or last line
	// the measured cỡ chữ and kiểu chữ (of its side, for a tab-split line)
	size         *float64
	bold, italic bool
}

func bodyLines(l *Layout, seg *Segmentation) []bodyLine {
	comp, ok := seg.Components["noi_dung"]
	if !ok || !comp.Found {
		return nil
	}
	var out []bodyLine
	for i, idx := range comp.Paras {
		if idx < 0 || idx >= len(l.Paragraphs) {
			continue
		}
		p := l.Paragraphs[idx]
		text := p.Text
		size, bold, italic := p.SizePt, isTrue(p.Bold), isTrue(p.Italic)
		if p.Zone == ZoneSplit && i < len(comp.Zones) {
			side := SideFormat{}
			switch comp.Zones[i] {
			case ZoneLeft:
				text, side = p.LeftText, p.LeftProps
			case ZoneRight:
				text, side = p.RightText, p.RightProps
			}
			if side.Present {
				size, bold, italic = side.SizePt, isTrue(side.Bold), isTrue(side.Italic)
			}
		}
		text = strings.Join(strings.Fields(text), " ")
		if text == "" {
			continue
		}
		out = append(out, bodyLine{para: idx, text: text, data: p.TableLayout == "data",
			size: size, bold: bold, italic: italic})
	}
	return out
}

// ExtractAnchors picks the anchor lines of the body and returns them in
// document order, with the number of body paragraphs.
func ExtractAnchors(l *Layout, seg *Segmentation, anchors []Anchor) ([]AnchorLine, int) {
	lines := bodyLines(l, seg)
	if len(lines) == 0 {
		return nil, 0
	}
	taken := make([]*Anchor, len(lines))
	also := make([][]string, len(lines)) // later anchors that matched a taken line
	ids := make([][]string, len(lines))  // every anchor id that picked a line
	folded := make([]string, len(lines))
	for i, bl := range lines {
		folded[i] = fold(bl.text)
	}
	for ai := range anchors {
		a := &anchors[ai]
		if a.Kind != "" && a.Kind != AnchorRegex {
			continue
		}
		re := anchorRegexp(a.Regex)
		if re == nil || a.Regex == "" {
			continue
		}
		var hits []int
		for i := range lines {
			if re.MatchString(folded[i]) {
				hits = append(hits, i)
			}
		}
		if a.Max > 0 && len(hits) > a.Max {
			// keep the first Max-1 and the last one
			hits = append(hits[:a.Max-1:a.Max-1], hits[len(hits)-1])
		}
		for _, i := range hits {
			ids[i] = append(ids[i], a.ID)
			if taken[i] == nil {
				taken[i] = a
			} else if a.Desc != "" {
				also[i] = append(also[i], a.Desc)
			}
		}
	}
	for ai := range anchors {
		a := &anchors[ai]
		n := a.Max
		if n <= 0 {
			n = 1
		}
		// head and tail count the body's own paragraphs, not the cells of
		// a data table ("12" is no closing line)
		switch a.Kind {
		case AnchorHead:
			for i, k := 0, 0; i < len(lines) && k < n; i++ {
				if lines[i].data {
					continue
				}
				k++
				ids[i] = append(ids[i], a.ID)
				if taken[i] == nil {
					taken[i] = a
				}
			}
		case AnchorTail:
			for i, k := len(lines)-1, 0; i >= 0 && k < n; i-- {
				if lines[i].data {
					continue
				}
				k++
				ids[i] = append(ids[i], a.ID)
				if taken[i] == nil {
					taken[i] = a
				}
			}
		}
	}
	var out []AnchorLine
	for i, a := range taken {
		if a == nil {
			continue
		}
		text, full := lines[i].text, ""
		if runeLen(text) > anchorTextRunes {
			text, full = truncateRunes(text, anchorTextRunes)+"…", text
		}
		desc := strings.Join(append([]string{a.Desc}, also[i]...), "; ")
		lineIDs := []string{a.ID}
		for _, id := range ids[i] {
			if id != a.ID {
				lineIDs = append(lineIDs, id)
			}
		}
		out = append(out, AnchorLine{Kind: a.ID, Para: lines[i].para, Text: text, Desc: strings.Trim(desc, "; "),
			IDs: lineIDs, Full: full, SizePt: lines[i].size, Bold: lines[i].bold, Italic: lines[i].italic})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Para < out[j].Para })
	return out, len(lines)
}
