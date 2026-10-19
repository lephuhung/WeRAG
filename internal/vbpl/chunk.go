package vbpl

import (
	"strings"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// Chunk is an exact slice [Start, End) of Document.Markdown (rune offsets),
// so Content == runes(Markdown)[Start:End] always holds.
type Chunk struct {
	Start   int    `json:"start"`
	End     int    `json:"end"`
	Content string `json:"-"`
	Kind    string `json:"kind"` // preamble | article | heading | appendix | closing
	// Path is the heading path: Phần/Chương/Mục titles, then the Điều
	// heading, e.g. ["Chương II. TIẾP NHẬN …", "Điều 14. Thẩm quyền …"].
	Path []string `json:"path,omitempty"`
	// Part is 1-based when the unit was split, 0 when it is whole.
	Part      int    `json:"part,omitempty"`
	ArticleNo string `json:"article_no,omitempty"`
	ArticleID string `json:"article_id,omitempty"`
	// KhoanNos / DiemLabels are the document's own khoản / điểm in the
	// chunk (quoted amending text excluded); Refs holds "khoan:K" and
	// "khoan:K/diem:D" keys, IDs maps those keys and "dieu" to vbpl ids.
	KhoanNos   []string          `json:"khoan_nos,omitempty"`
	DiemLabels []string          `json:"diem_labels,omitempty"`
	Refs       []string          `json:"refs,omitempty"`
	IDs        map[string]string `json:"ids,omitempty"`
	// Appendix is the title of the attached document / phụ lục, if any.
	Appendix string `json:"appendix,omitempty"`
	// Quoted is true when the chunk carries quoted (amending) text.
	Quoted bool `json:"quoted,omitempty"`
	// Amends lists what the chunk's quoted spans amend, in order.
	Amends []Amend `json:"amends,omitempty"`
}

// ContextHeader joins Path for the chunk's embedding/display prefix.
func (c Chunk) ContextHeader() string { return strings.Join(c.Path, " > ") }

const maxPathTitle = 160

// cut is the smallest cuttable range; level orders preferred cut points:
// 1 = a clause starts here, 2 = an item starts here, 3 = anything else.
type cut struct {
	start, end int
	level      int
}

// Plan cuts the document into chunks: one per Điều (or per heading / old
// style part holding its own text, per appendix, plus the preamble). A unit
// longer than maxRunes is split at Khoản, then Điểm, then paragraph, then
// table-row boundaries. Short units are never merged. The closing
// (signature, Nơi nhận) is its own chunk of kind "closing".
func Plan(d *Document, maxRunes int) []Chunk {
	if maxRunes <= 0 {
		maxRunes = 2500
	}
	runes := []rune(d.Markdown)
	blocks := d.Blocks
	var out []Chunk

	isBoundary := func(i int) bool {
		k := blocks[i].Kind
		if k.IsHeading() || k == KindArticle || k == KindAppendix {
			return true
		}
		if i == 0 {
			return true
		}
		prev := blocks[i-1].Kind
		// preamble/closing runs start where the kind changes
		return (k == KindPreamble || k == KindClosing) && prev != k
	}

	// Chunks must cover every block: the full text of a knowledge is
	// rebuilt from its chunks (read_document, summaries). A bare heading
	// line ("## Chương II. …") is prepended to the next unit's first chunk.
	lead := -1
	// flushLead emits the pending headings up to block i (exclusive) as a
	// chunk of their own.
	flushLead := func(i int) {
		out = append(out, Chunk{
			Start:   lead,
			End:     blocks[i-1].End,
			Content: string(runes[lead:blocks[i-1].End]),
			Kind:    chunkKind(blocks[i-1].Kind),
			Path:    headingPath(d, i-1),
		})
		lead = -1
	}
	for s := 0; s < len(blocks); {
		e := s + 1
		for e < len(blocks) && !isBoundary(e) {
			e++
		}
		head := blocks[s]
		if (head.Kind.IsHeading() || head.Kind == KindAppendix) && e == s+1 && e < len(blocks) &&
			head.End-head.Start <= maxRunes {
			if lead >= 0 && head.End-lead > maxRunes {
				flushLead(s) // a long run of bare headings (e.g. a table of contents)
			}
			if lead < 0 {
				lead = head.Start
			}
			s = e
			continue
		}
		unit := planUnit(d, runes, s, e, maxRunes)
		if lead >= 0 && len(unit) > 0 {
			if unit[0].End-lead <= maxRunes {
				unit[0].Start = lead
				unit[0].Content = string(runes[lead:unit[0].End])
				lead = -1
			} else {
				flushLead(s) // no room in front of the unit
			}
		}
		out = append(out, unit...)
		s = e
	}
	return out
}

func planUnit(d *Document, runes []rune, s, e, maxRunes int) []Chunk {
	blocks := d.Blocks
	head := blocks[s]
	base := Chunk{Kind: chunkKind(head.Kind)}
	base.Path = headingPath(d, s)
	if head.Kind == KindArticle {
		base.ArticleNo = head.Number
		base.ArticleID = head.ID
	}
	for p := s; p >= 0; p = blocks[p].Parent {
		if blocks[p].Kind == KindAppendix {
			base.Appendix = shortTitle(blocks[p].Text)
			break
		}
	}

	// Atoms: blocks, with oversized tables/paragraphs broken further.
	var atoms []cut
	for i := s; i < e; i++ {
		b := blocks[i]
		lvl := 3
		switch b.Kind {
		case KindClause:
			lvl = 1
		case KindItem:
			lvl = 2
		}
		if b.End-b.Start <= maxRunes {
			atoms = append(atoms, cut{b.Start, b.End, lvl})
			continue
		}
		if len(b.RowStarts) > 0 {
			// header stays with the first row unless the pair is too long
			for k, rs := range b.RowStarts {
				end := b.End
				if k+1 < len(b.RowStarts) {
					end = b.RowStarts[k+1] - 1 // drop the '\n'
				}
				switch {
				case k > 0:
					atoms = append(atoms, cut{rs, end, 3})
				case end-b.Start <= maxRunes:
					atoms = append(atoms, cut{b.Start, end, lvl})
				default:
					atoms = append(atoms, cut{b.Start, b.HeaderEnd, lvl}, cut{rs, end, 3})
				}
			}
			continue
		}
		atoms = append(atoms, cut{b.Start, b.End, lvl}) // windowed below
	}

	// an atom still longer than the limit (huge table row) is windowed
	var fitted []cut
	for _, a := range atoms {
		if a.end-a.start <= maxRunes {
			fitted = append(fitted, a)
			continue
		}
		// windows leave room for a short lead-in kept in front of them
		for k, w := range windows(runes, a.start, a.end, maxRunes-maxRunes/5) {
			lvl := 3
			if k == 0 {
				lvl = a.level
			}
			fitted = append(fitted, cut{w[0], w[1], lvl})
		}
	}
	atoms = fitted

	var groups [][2]int // atom index ranges
	packLevels(atoms, 0, len(atoms), 1, maxRunes, &groups)

	var res []Chunk

	for pi, g := range groups {
		c := base
		c.Start, c.End = atoms[g[0]].start, atoms[g[1]-1].end
		c.Content = string(runes[c.Start:c.End])
		if len(groups) > 1 {
			c.Part = pi + 1
		}
		c.addStructure(blocks, s, e)
		res = append(res, c)
	}
	return res
}

// addStructure records the khoản/điểm the chunk holds: blocks starting
// inside it, plus the clause/item still open at its start (a continuation
// piece of a split Điều belongs to the khoản it starts in).
func (c *Chunk) addStructure(blocks []Block, s, e int) {
	if blocks[s].Kind == KindArticle && blocks[s].ID != "" {
		c.setID("dieu", blocks[s].ID)
	}
	// the clause / item still open where the chunk starts comes first
	openClause, openItem := -1, -1
	for i := s; i < e && blocks[i].Start < c.Start; i++ {
		switch blocks[i].Kind {
		case KindClause:
			openClause, openItem = i, -1
		case KindItem:
			openItem = i
		}
	}
	if openClause >= 0 {
		c.addClause(blocks[openClause])
	}
	if openItem >= 0 {
		c.addItem(blocks, blocks[openItem])
	}
	var lastAmend *Amend
	for i := s; i < e && blocks[i].Start < c.End; i++ {
		b := blocks[i]
		if b.Kind == KindQuote && b.End > c.Start {
			c.Quoted = true
			if b.Amend != nil && b.Amend != lastAmend {
				c.addAmend(*b.Amend)
			}
			lastAmend = b.Amend
		}
		if b.Start < c.Start {
			continue
		}
		for _, a := range b.Phrases {
			c.addAmend(a)
		}
		switch b.Kind {
		case KindClause:
			c.addClause(b)
		case KindItem:
			c.addItem(blocks, b)
		}
	}
}

func (c *Chunk) addAmend(a Amend) {
	for _, have := range c.Amends {
		if have == a {
			return
		}
	}
	c.Amends = append(c.Amends, a)
}

func (c *Chunk) addClause(b Block) {
	if b.Number == "" {
		return
	}
	c.KhoanNos = appendNonEmpty(c.KhoanNos, b.Number)
	key := "khoan:" + b.Number
	c.Refs = appendNonEmpty(c.Refs, key)
	c.setID(key, b.ID)
}

func (c *Chunk) addItem(blocks []Block, b Block) {
	if b.Number == "" {
		return
	}
	c.DiemLabels = appendNonEmpty(c.DiemLabels, b.Number)
	if p := b.Parent; p >= 0 && blocks[p].Kind == KindClause && blocks[p].Number != "" {
		key := "khoan:" + blocks[p].Number + "/diem:" + b.Number
		c.Refs = appendNonEmpty(c.Refs, key)
		c.setID(key, b.ID)
	}
}

func (c *Chunk) setID(key, id string) {
	if id == "" {
		return
	}
	if c.IDs == nil {
		c.IDs = map[string]string{}
	}
	c.IDs[key] = id
}

// packLevels splits atoms[lo:hi] into groups no longer than max, cutting
// first at level-`level` atoms; a group still too long is split at the next
// level; at level 3 atoms are packed greedily.
func packLevels(atoms []cut, lo, hi, level, max int, out *[][2]int) {
	if lo >= hi {
		return
	}
	if atoms[hi-1].end-atoms[lo].start <= max {
		*out = append(*out, [2]int{lo, hi})
		return
	}
	if level > 3 {
		// cannot cut further: greedy by atom
		start := lo
		for i := lo + 1; i <= hi; i++ {
			if i == hi || atoms[i].end-atoms[start].start > max {
				*out = append(*out, [2]int{start, i})
				start = i
			}
		}
		return
	}
	// segments starting at atoms of this level (or lower number)
	var segs [][2]int
	start := lo
	for i := lo + 1; i < hi; i++ {
		if atoms[i].level <= level {
			segs = append(segs, [2]int{start, i})
			start = i
		}
	}
	segs = append(segs, [2]int{start, hi})
	if len(segs) == 1 {
		packLevels(atoms, lo, hi, level+1, max, out)
		return
	}
	// greedy pack consecutive segments; oversized segments recurse
	curLo, curHi := -1, -1
	flush := func() {
		if curLo >= 0 {
			*out = append(*out, [2]int{curLo, curHi})
			curLo, curHi = -1, -1
		}
	}
	for _, sg := range segs {
		size := atoms[sg[1]-1].end - atoms[sg[0]].start
		switch {
		case size > max:
			lo := sg[0]
			if curLo >= 0 && atoms[curHi-1].end-atoms[curLo].start < max/5 {
				// a short lead-in ("4. Điều 10 được sửa đổi như sau:") stays
				// with the first piece of the oversized segment it introduces
				lo, curLo, curHi = curLo, -1, -1
			}
			flush()
			packLevels(atoms, lo, sg[1], level+1, max, out)
		case curLo < 0:
			curLo, curHi = sg[0], sg[1]
		case atoms[sg[1]-1].end-atoms[curLo].start <= max:
			curHi = sg[1]
		default:
			flush()
			curLo, curHi = sg[0], sg[1]
		}
	}
	flush()
}

// windows cuts an over-long paragraph near sentence ends.
func windows(runes []rune, start, end, max int) [][2]int {
	var out [][2]int
	for start < end {
		stop := start + max
		if stop >= end {
			out = append(out, [2]int{start, end})
			break
		}
		cut := -1
		for k := stop; k > start+max/2; k-- {
			if (runes[k-1] == '.' || runes[k-1] == ';' || runes[k-1] == '\n') && k < end && runes[k] == ' ' {
				cut = k
				break
			}
		}
		if cut < 0 {
			for k := stop; k > start+max/2; k-- {
				if runes[k] == ' ' {
					cut = k
					break
				}
			}
		}
		if cut < 0 {
			cut = stop
		}
		out = append(out, [2]int{start, cut})
		start = cut
		for start < end && runes[start] == ' ' {
			start++
		}
	}
	return out
}

func chunkKind(k BlockKind) string {
	switch {
	case k == KindArticle:
		return "article"
	case k.IsHeading():
		return "heading"
	case k == KindAppendix:
		return "appendix"
	case k == KindPreamble:
		return "preamble"
	}
	return string(k)
}

// headingPath lists the enclosing headings of block i and the block itself
// when it is an article/heading/appendix.
func headingPath(d *Document, i int) []string {
	var rev []string
	b := d.Blocks[i]
	if b.Kind == KindArticle || b.Kind.IsHeading() || b.Kind == KindAppendix {
		rev = append(rev, shortTitle(b.Text))
	}
	for p := b.Parent; p >= 0; p = d.Blocks[p].Parent {
		if d.Blocks[p].Kind.IsHeading() || d.Blocks[p].Kind == KindAppendix {
			rev = append(rev, shortTitle(d.Blocks[p].Text))
		}
	}
	path := make([]string, len(rev))
	for k := range rev {
		path[k] = rev[len(rev)-1-k]
	}
	return path
}

func shortTitle(t string) string {
	if utf8.RuneCountInString(t) <= maxPathTitle {
		return t
	}
	r := []rune(t)[:maxPathTitle]
	if k := strings.LastIndexByte(string(r), ' '); k > maxPathTitle/2 {
		return string(r)[:k] + "…"
	}
	return string(r) + "…"
}

func appendNonEmpty(s []string, v string) []string {
	if v == "" {
		return s
	}
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}

// Legal converts the chunk into the metadata the retrieval side reads
// (section lookup, citations).
func (c Chunk) Legal() *vietnamese_legal.LegalMetadata {
	m := &vietnamese_legal.LegalMetadata{
		HeadingPath:     c.Path,
		KhoanNos:        c.KhoanNos,
		DiemLabels:      c.DiemLabels,
		SubdivisionRefs: c.Refs,
		SchemaVersion:   2,
		Source:          "vbpl",
		ProvisionIDs:    c.IDs,
		Appendix:        c.Appendix,
		Quoted:          c.Quoted,
	}
	for _, a := range c.Amends {
		m.Amends = append(m.Amends, vietnamese_legal.AmendRef{
			Document: a.Document, Article: a.Article, Khoan: a.Khoan, Diem: a.Diem,
			Phrase: a.Phrase, From: a.From, To: a.To,
		})
	}
	if c.ArticleNo != "" {
		m.ArticleNos = []string{strings.ToLower(c.ArticleNo)}
	}
	return m
}
