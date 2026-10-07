package docxedit

import (
	"encoding/xml"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// frag is one piece of a rewritten run.
type frag struct {
	kind int // fragKeep | fragDel | fragRaw
	xml  string
}

const (
	fragKeep = iota // run content kept as is
	fragDel         // run content wrapped in w:del
	fragRaw         // markup emitted between runs (the insertion)
)

// runEdit is the new content of one run, as fragments.
type runEdit struct {
	r     *run
	frags []frag
}

// neutralMarkup are range markers allowed both inside and between w:del.
var neutralMarkup = map[string]bool{
	"bookmarkStart": true, "bookmarkEnd": true, "proofErr": true,
	"commentRangeStart": true, "commentRangeEnd": true, "permStart": true, "permEnd": true,
}

// chainable reports whether b directly follows a (same parent, only range
// markers in between), so their deletions can share one w:del.
func (d *Document) chainable(a, b *elem) bool {
	if a.parent != b.parent {
		return false
	}
	sibs := a.parent.children
	i := 0
	for i < len(sibs) && sibs[i] != a {
		i++
	}
	for i++; i < len(sibs); i++ {
		if sibs[i] == b {
			return true
		}
		if sibs[i].prefix != d.w.wp || !neutralMarkup[sibs[i].local] {
			return false
		}
	}
	return false
}

// renderEdits turns run edits into splices. Each run is re-emitted as
// consecutive kept / deleted runs sharing its start tag and w:rPr; adjacent
// deleted runs (also across neighbouring edited runs) share one w:del.
func (d *Document) renderEdits(edits []runEdit, a Author) []splice {
	type item struct {
		kind int // fragKeep | fragDel | fragRaw | neutral (-1)
		xml  string
	}
	const neutral = -1
	var sp []splice
	for i := 0; i < len(edits); {
		j := i + 1
		for j < len(edits) && d.chainable(edits[j-1].r.el, edits[j].r.el) {
			j++
		}
		var items []item
		for k := i; k < j; k++ {
			r := edits[k].r
			if k > i {
				items = append(items, item{neutral, string(d.xml[edits[k-1].r.el.end:r.el.start])})
			}
			open, close := d.openTag(r.el), closeTag(r.el)
			// the first piece keeps the run's w:rPr verbatim (with any
			// w:rPrChange); later pieces drop the w:rPrChange so its w:id
			// is not duplicated
			rPr, rPrRest := "", d.inheritRPr(r)
			if r.rPr != nil {
				rPr = d.raw(r.rPr)
			}
			fr := edits[k].frags
			for x := 0; x < len(fr); {
				kind := fr[x].kind
				if kind == fragRaw {
					items = append(items, item{fragRaw, fr[x].xml})
					x++
					continue
				}
				var body strings.Builder
				for ; x < len(fr) && fr[x].kind == kind; x++ {
					body.WriteString(fr[x].xml)
				}
				items = append(items, item{kind, open + rPr + body.String() + close})
				rPr = rPrRest
			}
		}
		var sb, del, pending strings.Builder
		inDel := false
		closeDel := func() {
			if inDel {
				sb.WriteString("<" + d.q("del") + d.revAttrs(a) + ">" + del.String() + "</" + d.q("del") + ">")
				del.Reset()
				inDel = false
			}
			sb.WriteString(pending.String())
			pending.Reset()
		}
		for _, it := range items {
			switch it.kind {
			case neutral:
				if inDel {
					pending.WriteString(it.xml)
				} else {
					sb.WriteString(it.xml)
				}
			case fragDel:
				del.WriteString(pending.String())
				pending.Reset()
				del.WriteString(it.xml)
				inDel = true
			default:
				closeDel()
				sb.WriteString(it.xml)
			}
		}
		closeDel()
		sp = append(sp, splice{edits[i].r.el.start, edits[j-1].r.el.end, sb.String()})
		i = j
	}
	return sp
}

func (d *Document) tXML(text string) string {
	return "<" + d.q("t") + ` xml:space="preserve">` + escText(text) + "</" + d.q("t") + ">"
}

func (d *Document) delTextXML(text string) string {
	return "<" + d.q("delText") + ` xml:space="preserve">` + escText(text) + "</" + d.q("delText") + ">"
}

// delFrag renders a whole segment as deleted content.
func (d *Document) delFrag(s seg) frag {
	if d.w.is(s.el, "t") {
		return frag{fragDel, d.delTextXML(s.text)}
	}
	return frag{fragDel, d.raw(s.el)}
}

// insXML renders a tracked insertion of text; "\t" → w:tab, "\n" → w:br.
func (d *Document) insXML(text, rPr string, a Author) string {
	var body strings.Builder
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			body.WriteString(d.tXML(cur.String()))
			cur.Reset()
		}
	}
	for _, r := range text {
		switch r {
		case '\t':
			flush()
			body.WriteString("<" + d.q("tab") + "/>")
		case '\n':
			flush()
			body.WriteString("<" + d.q("br") + "/>")
		case '\r':
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return "<" + d.q("ins") + d.revAttrs(a) + "><" + d.q("r") + ">" + rPr + body.String() +
		"</" + d.q("r") + "></" + d.q("ins") + ">"
}

// inheritRPr is r's w:rPr markup without any w:rPrChange.
func (d *Document) inheritRPr(r *run) string {
	if r == nil || r.rPr == nil {
		return ""
	}
	if d.w.child(r.rPr, "rPrChange") == nil {
		return d.raw(r.rPr)
	}
	return d.openTag(r.rPr) + joinKids(removeKid(d.kids(r.rPr), "rPrChange")) + closeTag(r.rPr)
}

// appendToPara inserts markup at the end of the paragraph's content.
func (d *Document) appendToPara(p *elem, markup string) splice {
	if p.selfClosing {
		return splice{p.start, p.end, d.openTag(p) + markup + closeTag(p)}
	}
	at := d.endTagStart(p)
	return splice{at, at, markup}
}

// planReplaceText computes the splices of ReplaceText without applying them.
func (d *Document) planReplaceText(index int, newText string, a Author) ([]splice, error) {
	p, err := d.para(index)
	if err != nil {
		return nil, err
	}
	if newText == p.text {
		return nil, nil
	}
	var insRPr string
	var gotRPr bool
	var edits []runEdit
	for _, r := range p.runs {
		if !gotRPr && r.hasText() {
			insRPr, gotRPr = d.inheritRPr(r), true
		}
		var frags []frag
		del := false
		for _, s := range r.segs {
			if (s.kind == segText && !s.hardBreak) || s.kind == segSilent {
				frags = append(frags, d.delFrag(s))
				del = true
			} else {
				frags = append(frags, frag{fragKeep, d.raw(s.el)})
			}
		}
		if del {
			edits = append(edits, runEdit{r, frags})
		}
	}
	var sp []splice
	if newText != "" {
		ins := d.insXML(newText, insRPr, a)
		// anchor after the last deleted run that is not a field result
		// (text placed there would be overwritten on field update)
		anchor := -1
		for i := len(edits) - 1; i >= 0; i-- {
			if !edits[i].r.inField {
				anchor = i
				break
			}
		}
		switch {
		case anchor < 0:
			sp = append(sp, d.appendToPara(p.el, ins))
		case edits[anchor].r.insOuter != nil:
			o := edits[anchor].r.insOuter
			sp = append(sp, splice{o.end, o.end, ins})
		default:
			// right after the run's last deleted fragment
			fr := edits[anchor].frags
			k := len(fr)
			for k > 0 && fr[k-1].kind != fragDel {
				k--
			}
			out := append(append([]frag(nil), fr[:k]...), frag{fragRaw, ins})
			edits[anchor].frags = append(out, fr[k:]...)
		}
	}
	return append(sp, d.renderEdits(edits, a)...), nil
}

// planReplaceSubstring computes the splices of ReplaceSubstring without applying them.
func (d *Document) planReplaceSubstring(index int, old, new string, a Author) ([]splice, error) {
	p, err := d.para(index)
	if err != nil {
		return nil, err
	}
	if old == "" {
		return nil, errors.New("docxedit: empty search text")
	}
	s := strings.Index(p.text, old)
	if s < 0 {
		old = nfcLatin(old)
		s = strings.Index(p.text, old)
	}
	if s < 0 {
		return nil, fmt.Errorf("docxedit: %q not found in paragraph %d", old, index)
	}
	if old == new {
		return nil, nil
	}
	e := s + len(old)

	var edits []runEdit
	var startRun *run
	lastEdit, lastFrag := -1, -1
	pos := 0
	for _, r := range p.runs {
		var frags []frag
		touched := false
		for _, sg := range r.segs {
			n := len(sg.text)
			switch sg.kind {
			case segText:
				lo, hi := max(s, pos), min(e, pos+n)
				if n == 0 || lo >= hi {
					frags = append(frags, frag{fragKeep, d.raw(sg.el)})
					break
				}
				if startRun == nil {
					startRun = r
				}
				touched = true
				if d.w.is(sg.el, "t") {
					if pre := sg.text[:lo-pos]; pre != "" {
						frags = append(frags, frag{fragKeep, d.tXML(pre)})
					}
					frags = append(frags, frag{fragDel, d.delTextXML(sg.text[lo-pos : hi-pos])})
					lastFrag = len(frags) - 1
					if post := sg.text[hi-pos:]; post != "" {
						frags = append(frags, frag{fragKeep, d.tXML(post)})
					}
				} else {
					frags = append(frags, d.delFrag(sg))
					lastFrag = len(frags) - 1
				}
			case segSilent:
				if s < pos && pos < e {
					touched = true
					frags = append(frags, d.delFrag(sg))
					lastFrag = len(frags) - 1
				} else {
					frags = append(frags, frag{fragKeep, d.raw(sg.el)})
				}
			default:
				frags = append(frags, frag{fragKeep, d.raw(sg.el)})
			}
			pos += n
		}
		if touched {
			// a touched run always holds a deleted fragment, so lastFrag
			// indexes into this run's frags
			edits = append(edits, runEdit{r, frags})
			lastEdit = len(edits) - 1
		}
		if pos >= e && touched {
			break
		}
	}
	if len(edits) == 0 || lastEdit < 0 {
		return nil, errors.New("docxedit: internal error: substring not mapped to runs")
	}
	if new != "" {
		le := &edits[lastEdit]
		ins := d.insXML(new, d.inheritRPr(startRun), a)
		if r := le.r; r.insOuter != nil {
			if r.insParent == nil || r.insParent != r.insOuter {
				return nil, errors.New("docxedit: text sits in a nested tracked insertion; accept or reject it first")
			}
			attrs := d.attrSet(append([]xml.Attr(nil), r.insParent.attrs...), "id", strconv.Itoa(d.nextID))
			d.nextID++
			ins = closeTag(r.insParent) + ins + "<" + qname(r.insParent) + renderAttrs(attrs) + ">"
		}
		f := append([]frag(nil), le.frags[:lastFrag+1]...)
		f = append(f, frag{fragRaw, ins})
		le.frags = append(f, le.frags[lastFrag+1:]...)
	}
	return d.renderEdits(edits, a), nil
}

// planSetRunProps computes the splices of SetRunProps without applying them.
func (d *Document) planSetRunProps(index int, p RunProps, a Author) ([]splice, error) {
	pp, err := d.para(index)
	if err != nil {
		return nil, err
	}
	if p.Font != nil && strings.TrimSpace(*p.Font) == "" {
		return nil, errors.New("docxedit: empty font name")
	}
	if p.SizePt != nil && (*p.SizePt <= 0 || *p.SizePt > 1638) {
		return nil, fmt.Errorf("docxedit: font size %.1fpt out of range", *p.SizePt)
	}
	var sp []splice
	for _, r := range pp.runs {
		if !r.hasText() {
			continue
		}
		ks := d.kids(r.rPr)
		old := removeKid(ks, "rPrChange")
		next := d.applyRunProps(append([]kid(nil), old...), p)
		next = sortKids(next, rPrOrder)
		if joinKids(next) == joinKids(sortKids(old, rPrOrder)) {
			continue
		}
		var change kid
		if i := findKid(ks, "rPrChange"); i >= 0 {
			change = ks[i]
		} else {
			change = kid{local: "rPrChange", xml: "<" + d.q("rPrChange") + d.revAttrs(a) + "><" +
				d.q("rPr") + ">" + joinKids(old) + "</" + d.q("rPr") + "></" + d.q("rPrChange") + ">"}
		}
		next = append(next, change)
		if r.rPr != nil {
			sp = append(sp, splice{r.rPr.start, r.rPr.end,
				d.openTag(r.rPr) + joinKids(next) + closeTag(r.rPr)})
		} else {
			sp = append(sp, splice{r.el.startEnd, r.el.startEnd,
				"<" + d.q("rPr") + ">" + joinKids(next) + "</" + d.q("rPr") + ">"})
		}
	}
	return sp, nil
}

func (d *Document) applyRunProps(ks []kid, p RunProps) []kid {
	if p.Font != nil {
		f := *p.Font
		ks = d.editLeaf(ks, "rFonts", func(at []xml.Attr) []xml.Attr {
			at = d.attrDel(at, "asciiTheme", "hAnsiTheme", "eastAsiaTheme", "cstheme")
			for _, k := range []string{"ascii", "hAnsi", "cs", "eastAsia"} {
				at = d.attrSet(at, k, f)
			}
			return at
		})
	}
	if p.SizePt != nil {
		v := halfPts(*p.SizePt)
		for _, k := range []string{"sz", "szCs"} {
			ks = d.editLeaf(ks, k, func(at []xml.Attr) []xml.Attr { return d.attrSet(at, "val", v) })
		}
	}
	if p.Bold != nil {
		ks = d.toggle(ks, "b", *p.Bold)
		ks = d.toggle(ks, "bCs", *p.Bold)
	}
	if p.Italic != nil {
		ks = d.toggle(ks, "i", *p.Italic)
		ks = d.toggle(ks, "iCs", *p.Italic)
	}
	if p.Caps != nil {
		ks = d.toggle(ks, "caps", *p.Caps)
	}
	if p.Underline != nil {
		on := *p.Underline
		ks = d.editLeaf(ks, "u", func(at []xml.Attr) []xml.Attr {
			if on {
				if v, ok := d.attrGet(at, "val"); ok && v != "none" && v != "" {
					return at // already underlined in some style
				}
				return d.attrSet(at, "val", "single")
			}
			return d.attrSet(d.attrDel(at, "color", "themeColor", "themeTint", "themeShade"), "val", "none")
		})
	}
	return ks
}

// planSetParaProps computes the splices of SetParaProps without applying them.
func (d *Document) planSetParaProps(index int, p ParaProps, a Author) ([]splice, error) {
	pp, err := d.para(index)
	if err != nil {
		return nil, err
	}
	if err := validateParaProps(&p); err != nil {
		return nil, err
	}
	pPr := d.w.child(pp.el, "pPr")
	ks := d.kids(pPr)
	old := withoutKids(ks, "rPr", "sectPr", "pPrChange")
	next := d.applyParaProps(removeKid(ks, "pPrChange"), p)
	next = sortKids(next, pPrOrder)
	if joinKids(withoutKids(next, "rPr", "sectPr")) == joinKids(sortKids(old, pPrOrder)) {
		return nil, nil
	}
	var change kid
	if i := findKid(ks, "pPrChange"); i >= 0 {
		change = ks[i]
	} else {
		change = kid{local: "pPrChange", xml: "<" + d.q("pPrChange") + d.revAttrs(a) + "><" +
			d.q("pPr") + ">" + joinKids(old) + "</" + d.q("pPr") + "></" + d.q("pPrChange") + ">"}
	}
	next = append(next, change)
	var sp splice
	switch {
	case pPr != nil:
		sp = splice{pPr.start, pPr.end, d.openTag(pPr) + joinKids(next) + closeTag(pPr)}
	case pp.el.selfClosing:
		sp = splice{pp.el.start, pp.el.end, d.openTag(pp.el) + "<" + d.q("pPr") + ">" +
			joinKids(next) + "</" + d.q("pPr") + ">" + closeTag(pp.el)}
	default:
		sp = splice{pp.el.startEnd, pp.el.startEnd,
			"<" + d.q("pPr") + ">" + joinKids(next) + "</" + d.q("pPr") + ">"}
	}
	return []splice{sp}, nil
}

func validateParaProps(p *ParaProps) error {
	if p.Alignment != nil {
		switch *p.Alignment {
		case "left", "center", "right", "both":
		case "justify":
			v := "both"
			p.Alignment = &v
		default:
			return fmt.Errorf("docxedit: unsupported alignment %q", *p.Alignment)
		}
	}
	if p.FirstLinePt != nil && p.HangingPt != nil {
		return errors.New("docxedit: first-line and hanging indent are exclusive")
	}
	if ls := p.LineSpacing; ls != nil {
		n := 0
		for _, v := range []*float64{ls.Multiple, ls.ExactPt, ls.AtLeastPt} {
			if v != nil {
				n++
				if *v <= 0 {
					return errors.New("docxedit: line spacing must be positive")
				}
			}
		}
		if n != 1 {
			return errors.New("docxedit: set exactly one line spacing rule")
		}
	}
	return nil
}

func (d *Document) applyParaProps(ks []kid, p ParaProps) []kid {
	if p.Alignment != nil {
		v := *p.Alignment
		ks = d.editLeaf(ks, "jc", func(at []xml.Attr) []xml.Attr { return d.attrSet(at, "val", v) })
	}
	if p.IndentLeftPt != nil || p.IndentRightPt != nil || p.FirstLinePt != nil || p.HangingPt != nil {
		ks = d.editLeaf(ks, "ind", func(at []xml.Attr) []xml.Attr {
			if v := p.IndentLeftPt; v != nil {
				at = d.attrSet(d.attrDel(at, "start", "leftChars", "startChars"), "left", twips(*v))
			}
			if v := p.IndentRightPt; v != nil {
				at = d.attrSet(d.attrDel(at, "end", "rightChars", "endChars"), "right", twips(*v))
			}
			if v := p.FirstLinePt; v != nil {
				at = d.attrSet(d.attrDel(at, "hanging", "hangingChars", "firstLineChars"), "firstLine", twips(*v))
			}
			if v := p.HangingPt; v != nil {
				at = d.attrSet(d.attrDel(at, "firstLine", "firstLineChars", "hangingChars"), "hanging", twips(*v))
			}
			return at
		})
	}
	if p.SpaceBeforePt != nil || p.SpaceAfterPt != nil || p.LineSpacing != nil {
		ks = d.editLeaf(ks, "spacing", func(at []xml.Attr) []xml.Attr {
			if v := p.SpaceBeforePt; v != nil {
				at = d.attrSet(d.attrDel(at, "beforeLines", "beforeAutospacing"), "before", twips(*v))
			}
			if v := p.SpaceAfterPt; v != nil {
				at = d.attrSet(d.attrDel(at, "afterLines", "afterAutospacing"), "after", twips(*v))
			}
			if ls := p.LineSpacing; ls != nil {
				switch {
				case ls.Multiple != nil:
					at = d.attrSet(at, "line", strconv.Itoa(int(math.Round(*ls.Multiple*240))))
					at = d.attrSet(at, "lineRule", "auto")
				case ls.ExactPt != nil:
					at = d.attrSet(at, "line", twips(*ls.ExactPt))
					at = d.attrSet(at, "lineRule", "exact")
				case ls.AtLeastPt != nil:
					at = d.attrSet(at, "line", twips(*ls.AtLeastPt))
					at = d.attrSet(at, "lineRule", "atLeast")
				}
			}
			return at
		})
	}
	return ks
}

// planSetSectionProps computes the splices of SetSectionProps without applying them.
func (d *Document) planSetSectionProps(sectIndex int, p SectionProps, a Author) ([]splice, error) {
	if sectIndex < 0 || sectIndex >= len(d.sects) {
		return nil, fmt.Errorf("docxedit: section index %d out of range [0,%d)", sectIndex, len(d.sects))
	}
	if o := p.Orientation; o != nil && *o != "portrait" && *o != "landscape" {
		return nil, fmt.Errorf("docxedit: unsupported orientation %q", *o)
	}
	sp := d.sects[sectIndex]
	ks := d.kids(sp)
	refs := []string{"headerReference", "footerReference"}
	old := withoutKids(ks, append(refs, "sectPrChange")...)
	next := sortKids(d.applySectionProps(removeKid(ks, "sectPrChange"), p), sectPrOrder)
	if joinKids(withoutKids(next, refs...)) == joinKids(sortKids(old, sectPrOrder)) {
		return nil, nil
	}
	var change kid
	if i := findKid(ks, "sectPrChange"); i >= 0 {
		change = ks[i]
	} else {
		change = kid{local: "sectPrChange", xml: "<" + d.q("sectPrChange") + d.revAttrs(a) + "><" +
			d.q("sectPr") + ">" + joinKids(old) + "</" + d.q("sectPr") + "></" + d.q("sectPrChange") + ">"}
	}
	next = append(next, change)
	return []splice{{sp.start, sp.end, d.openTag(sp) + joinKids(next) + closeTag(sp)}}, nil
}

func (d *Document) applySectionProps(ks []kid, p SectionProps) []kid {
	if p.PageWidthPt != nil || p.PageHeightPt != nil || p.Orientation != nil {
		ks = d.editLeaf(ks, "pgSz", func(at []xml.Attr) []xml.Attr {
			if v := p.PageWidthPt; v != nil {
				at = d.attrSet(at, "w", twips(*v))
			}
			if v := p.PageHeightPt; v != nil {
				at = d.attrSet(at, "h", twips(*v))
			}
			if o := p.Orientation; o != nil {
				at = d.attrSet(at, "orient", *o)
				ws, okW := d.attrGet(at, "w")
				hs, okH := d.attrGet(at, "h")
				w, errW := strconv.Atoi(ws)
				h, errH := strconv.Atoi(hs)
				if p.PageWidthPt == nil && p.PageHeightPt == nil && okW && okH && errW == nil && errH == nil &&
					((*o == "landscape" && w < h) || (*o == "portrait" && w > h)) {
					at = d.attrSet(at, "w", hs)
					at = d.attrSet(at, "h", ws)
				}
			}
			return at
		})
	}
	margins := []struct {
		attr string
		v    *float64
		def  string
	}{
		{"top", p.MarginTopPt, "1440"}, {"right", p.MarginRightPt, "1440"},
		{"bottom", p.MarginBottomPt, "1440"}, {"left", p.MarginLeftPt, "1440"},
		{"header", p.HeaderPt, "720"}, {"footer", p.FooterPt, "720"}, {"gutter", p.GutterPt, "0"},
	}
	anyMargin := false
	for _, m := range margins {
		anyMargin = anyMargin || m.v != nil
	}
	if anyMargin {
		ks = d.editLeaf(ks, "pgMar", func(at []xml.Attr) []xml.Attr {
			for _, m := range margins {
				if m.v != nil {
					at = d.attrSet(at, m.attr, twips(*m.v))
				} else if _, ok := d.attrGet(at, m.attr); !ok {
					at = d.attrSet(at, m.attr, m.def) // all seven are required
				}
			}
			return at
		})
	}
	return ks
}

// ReplaceText replaces the whole visible text of paragraph index as a
// tracked change: text-like run content is wrapped in w:del (w:t become
// w:delText) and one w:ins run carrying newText is added after the last
// deleted run outside any field result (after the enclosing w:ins when that
// run is already a tracked insertion; at the end of the paragraph when there
// is no such run). The new
// run inherits the w:rPr of the first text run. Drawings, fields, note
// references and page/column breaks are kept outside the deletion.
func (d *Document) ReplaceText(index int, newText string, a Author) error {
	sp, err := d.planReplaceText(index, newText, a)
	if err != nil {
		return err
	}
	return d.apply(sp)
}

// ReplaceSubstring replaces the first occurrence of old in the paragraph's
// visible text with new, splitting runs at the boundaries; the old text is
// tracked as w:del and new as a w:ins run placed right after it, carrying
// the w:rPr of the run where old starts. If old ends inside a run that is
// itself a tracked insertion, that w:ins is closed and reopened around the
// new insertion (insertions cannot nest).
func (d *Document) ReplaceSubstring(index int, old, new string, a Author) error {
	sp, err := d.planReplaceSubstring(index, old, new, a)
	if err != nil {
		return err
	}
	return d.apply(sp)
}

// SetRunProps applies p to every text run of the paragraph as a tracked
// formatting change (w:rPrChange holding the previous run properties). An
// existing w:rPrChange is kept as is, so repeated calls record the original
// formatting rather than nesting. Runs without text and runs whose
// properties would not change are skipped.
func (d *Document) SetRunProps(index int, p RunProps, a Author) error {
	sp, err := d.planSetRunProps(index, p, a)
	if err != nil {
		return err
	}
	return d.apply(sp)
}

// SetParaProps applies p to the paragraph as a tracked formatting change:
// w:pPrChange holds the previous w:pPr children (without rPr, sectPr and
// pPrChange). An existing w:pPrChange is kept, so the original properties
// survive repeated calls.
func (d *Document) SetParaProps(index int, p ParaProps, a Author) error {
	sp, err := d.planSetParaProps(index, p, a)
	if err != nil {
		return err
	}
	return d.apply(sp)
}

// SetSectionProps edits the sectIndex-th w:sectPr (docformat
// Layout.Sections order) and records w:sectPrChange with the previous
// values (headers/footers references excluded, as the schema requires). An
// existing w:sectPrChange is kept. Changing only Orientation swaps the
// page width/height when they contradict it.
func (d *Document) SetSectionProps(sectIndex int, p SectionProps, a Author) error {
	sp, err := d.planSetSectionProps(sectIndex, p, a)
	if err != nil {
		return err
	}
	return d.apply(sp)
}
