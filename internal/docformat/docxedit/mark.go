package docxedit

import (
	"encoding/xml"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Mark is review formatting applied to a passage ("this is wrong"); nil
// leaves a property unchanged.
type Mark struct {
	Underline      *string // single | wave | double | ... | none (ST_Underline)
	UnderlineColor *string // hex RRGGBB without '#', or "auto"
	Color          *string // text colour hex RRGGBB, or "auto"
	Highlight      *string // yellow, red, green, cyan, magenta, lightGray, ... or none
}

var (
	underlineVals = set("single", "words", "double", "thick", "dotted", "dottedHeavy", "dash",
		"dashedHeavy", "dashLong", "dashLongHeavy", "dotDash", "dashDotHeavy", "dotDotDash",
		"dashDotDotHeavy", "wave", "wavyHeavy", "wavyDouble", "none")
	highlightVals = set("black", "blue", "cyan", "green", "magenta", "red", "yellow", "white",
		"darkBlue", "darkCyan", "darkGreen", "darkMagenta", "darkRed", "darkYellow",
		"darkGray", "lightGray", "none")
)

func set(vals ...string) map[string]bool {
	m := map[string]bool{}
	for _, v := range vals {
		m[v] = true
	}
	return m
}

// hexColor validates RRGGBB (an optional leading '#' is dropped) or "auto".
func hexColor(s string) (string, error) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if s == "auto" {
		return s, nil
	}
	if len(s) != 6 {
		return "", fmt.Errorf("docxedit: colour %q is not RRGGBB hex", s)
	}
	if _, err := strconv.ParseUint(s, 16, 32); err != nil {
		return "", fmt.Errorf("docxedit: colour %q is not RRGGBB hex", s)
	}
	return strings.ToUpper(s), nil
}

// normalize validates m and canonicalizes its colours.
func (m Mark) normalize() (Mark, error) {
	if m.Underline == nil && m.UnderlineColor == nil && m.Color == nil && m.Highlight == nil {
		return m, errors.New("docxedit: empty mark")
	}
	if m.Underline != nil && !underlineVals[*m.Underline] {
		return m, fmt.Errorf("docxedit: unsupported underline %q", *m.Underline)
	}
	if m.Highlight != nil && !highlightVals[*m.Highlight] {
		return m, fmt.Errorf("docxedit: unsupported highlight %q", *m.Highlight)
	}
	for _, c := range []**string{&m.UnderlineColor, &m.Color} {
		if *c != nil {
			v, err := hexColor(**c)
			if err != nil {
				return m, err
			}
			*c = &v
		}
	}
	return m, nil
}

func (d *Document) applyMark(ks []kid, m Mark) []kid {
	if m.Color != nil {
		v := *m.Color
		ks = d.editLeaf(ks, "color", func(at []xml.Attr) []xml.Attr {
			// theme colours override w:val
			return d.attrSet(d.attrDel(at, "themeColor", "themeTint", "themeShade"), "val", v)
		})
	}
	if m.Highlight != nil {
		v := *m.Highlight
		ks = d.editLeaf(ks, "highlight", func(at []xml.Attr) []xml.Attr { return d.attrSet(at, "val", v) })
	}
	if m.Underline != nil || m.UnderlineColor != nil {
		ks = d.editLeaf(ks, "u", func(at []xml.Attr) []xml.Attr {
			if m.Underline != nil {
				at = d.attrSet(at, "val", *m.Underline)
			} else if v, ok := d.attrGet(at, "val"); !ok || v == "" || v == "none" {
				at = d.attrSet(at, "val", "single") // a colour needs a line
			}
			if m.UnderlineColor != nil {
				at = d.attrSet(d.attrDel(at, "themeColor", "themeTint", "themeShade"), "color", *m.UnderlineColor)
			}
			return at
		})
	}
	return ks
}

// changeWithFreshID re-emits an existing w:rPrChange under a new w:id (for
// the extra copies created when a run is split).
func (d *Document) changeWithFreshID(c *elem) string {
	attrs := d.attrSet(append([]xml.Attr(nil), c.attrs...), "id", strconv.Itoa(d.nextID))
	d.nextID++
	open := "<" + qname(c) + renderAttrs(attrs)
	if c.selfClosing {
		return open + "/>"
	}
	return open + ">" + string(d.xml[c.startEnd:c.end])
}

// restyle computes r's w:rPr after mod, recording a w:rPrChange with the
// previous properties (an existing w:rPrChange is kept, under a fresh id
// when fresh is set). changed is false when mod alters nothing; the
// returned markup is then the original properties (fresh id applied).
func (d *Document) restyle(r *run, mod func([]kid) []kid, a Author, fresh bool) (rPr string, changed bool) {
	ks := d.kids(r.rPr)
	old := removeKid(ks, "rPrChange")
	next := sortKids(mod(append([]kid(nil), old...)), rPrOrder)
	existing := d.w.child(r.rPr, "rPrChange")
	open, close := "<"+d.q("rPr")+">", "</"+d.q("rPr")+">"
	if r.rPr != nil {
		open, close = d.openTag(r.rPr), closeTag(r.rPr)
	}
	if joinKids(next) == joinKids(sortKids(old, rPrOrder)) {
		return d.keptRPr(r, fresh), false
	}
	var change string
	switch {
	case existing != nil && fresh:
		change = d.changeWithFreshID(existing)
	case existing != nil:
		change = d.raw(existing)
	default:
		change = "<" + d.q("rPrChange") + d.revAttrs(a) + "><" + d.q("rPr") + ">" + joinKids(old) +
			"</" + d.q("rPr") + "></" + d.q("rPrChange") + ">"
	}
	return open + joinKids(next) + change + close, true
}

// keptRPr is r's original w:rPr markup; with fresh, an existing w:rPrChange
// gets a new id so a split copy does not duplicate it.
func (d *Document) keptRPr(r *run, fresh bool) string {
	if r.rPr == nil {
		return ""
	}
	c := d.w.child(r.rPr, "rPrChange")
	if c == nil || !fresh {
		return d.raw(r.rPr)
	}
	return string(d.xml[r.rPr.start:c.start]) + d.changeWithFreshID(c) + string(d.xml[c.end:r.rPr.end])
}

// restyleSplices applies mod to every text run of paragraph index.
func (d *Document) restyleSplices(index int, mod func([]kid) []kid, a Author) ([]splice, error) {
	pp, err := d.para(index)
	if err != nil {
		return nil, err
	}
	var sp []splice
	for _, r := range pp.runs {
		if !r.hasText() {
			continue
		}
		rPr, changed := d.restyle(r, mod, a, false)
		if !changed {
			continue
		}
		if r.rPr != nil {
			sp = append(sp, splice{r.rPr.start, r.rPr.end, rPr})
		} else {
			sp = append(sp, splice{r.el.startEnd, r.el.startEnd, rPr})
		}
	}
	return sp, nil
}

func (d *Document) planMarkParagraph(index int, m Mark, a Author) ([]splice, error) {
	m, err := m.normalize()
	if err != nil {
		return nil, err
	}
	return d.restyleSplices(index, func(ks []kid) []kid { return d.applyMark(ks, m) }, a)
}

// findText locates old in the paragraph text, retrying with old composed.
func findText(p *para, old string, index int) (int, string, error) {
	if old == "" {
		return 0, "", errors.New("docxedit: empty search text")
	}
	s := strings.Index(p.text, old)
	if s < 0 {
		old = nfcLatin(old)
		s = strings.Index(p.text, old)
	}
	if s < 0 {
		return 0, "", fmt.Errorf("docxedit: %q not found in paragraph %d", old, index)
	}
	return s, old, nil
}

func (d *Document) planMarkSubstring(index int, old string, m Mark, a Author) ([]splice, error) {
	m, err := m.normalize()
	if err != nil {
		return nil, err
	}
	p, err := d.para(index)
	if err != nil {
		return nil, err
	}
	s, old, err := findText(p, old, index)
	if err != nil {
		return nil, err
	}
	e := s + len(old)
	mod := func(ks []kid) []kid { return d.applyMark(ks, m) }
	const fragMark = fragDel // reuse: "inside the marked range"

	var sp []splice
	pos := 0
	for _, r := range p.runs {
		var frags []frag
		touched := false
		for _, sg := range r.segs {
			n := len(sg.text)
			lo, hi := max(s, pos), min(e, pos+n)
			switch {
			case n > 0 && lo < hi && d.w.is(sg.el, "t"):
				touched = true
				if pre := sg.text[:lo-pos]; pre != "" {
					frags = append(frags, frag{fragKeep, d.tXML(pre)})
				}
				frags = append(frags, frag{fragMark, d.tXML(sg.text[lo-pos : hi-pos])})
				if post := sg.text[hi-pos:]; post != "" {
					frags = append(frags, frag{fragKeep, d.tXML(post)})
				}
			case n > 0 && lo < hi:
				touched = true
				frags = append(frags, frag{fragMark, d.raw(sg.el)})
			case n == 0 && s < pos && pos < e:
				frags = append(frags, frag{fragMark, d.raw(sg.el)})
			default:
				frags = append(frags, frag{fragKeep, d.raw(sg.el)})
			}
			pos += n
		}
		if !touched {
			continue
		}
		open, close := d.openTag(r.el), closeTag(r.el)
		var sb strings.Builder
		first, anyChange := true, false
		for i := 0; i < len(frags); {
			kind := frags[i].kind
			var body strings.Builder
			for ; i < len(frags) && frags[i].kind == kind; i++ {
				body.WriteString(frags[i].xml)
			}
			var rPr string
			if kind == fragMark {
				var changed bool
				rPr, changed = d.restyle(r, mod, a, !first)
				anyChange = anyChange || changed
			} else {
				rPr = d.keptRPr(r, !first)
			}
			sb.WriteString(open + rPr + body.String() + close)
			first = false
		}
		if anyChange {
			sp = append(sp, splice{r.el.start, r.el.end, sb.String()})
		}
		if pos >= e {
			break
		}
	}
	return sp, nil
}

// MarkSubstring applies m to the first occurrence of old in the paragraph's
// visible text (retrying with old composed to NFC), splitting runs at the
// boundaries, as a tracked formatting change: every marked run gets a
// w:rPrChange with its previous properties, so rejecting the change removes
// the mark. Errors when old is not found or m is empty/invalid; marking text
// that already carries m records nothing.
func (d *Document) MarkSubstring(index int, old string, m Mark, a Author) error {
	sp, err := d.planMarkSubstring(index, old, m, a)
	if err != nil {
		return err
	}
	return d.apply(sp)
}

// MarkParagraph applies m to every text run of the paragraph as a tracked
// formatting change, like MarkSubstring.
func (d *Document) MarkParagraph(index int, m Mark, a Author) error {
	sp, err := d.planMarkParagraph(index, m, a)
	if err != nil {
		return err
	}
	return d.apply(sp)
}
