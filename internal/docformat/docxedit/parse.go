package docxedit

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strings"
)

const nsW = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"

// elem is one element of word/document.xml with its byte range. Names keep
// the raw prefix (xml.Decoder.RawToken), so nothing is re-namespaced.
type elem struct {
	prefix, local string
	attrs         []xml.Attr // raw: Name.Space is the prefix
	start         int        // offset of '<'
	startEnd      int        // offset just past the start tag
	end           int        // offset just past the end tag (== startEnd when self-closing)
	selfClosing   bool
	parent        *elem
	children      []*elem
	text          string // decoded character data directly inside
}

// parseTree tokenizes data recording byte offsets of every element.
func parseTree(data []byte) (*elem, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	// lenient like docformat.parseXML: an undefined entity (&nbsp;) is kept
	// as literal text instead of failing, so whatever the checker reads can
	// be edited
	dec.Strict = false
	var root *elem
	var stack []*elem
	for {
		off := int(dec.InputOffset())
		tok, err := dec.RawToken()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			e := &elem{prefix: t.Name.Space, local: t.Name.Local,
				attrs: append([]xml.Attr(nil), t.Attr...), start: off,
				startEnd: int(dec.InputOffset())}
			if len(stack) > 0 {
				p := stack[len(stack)-1]
				e.parent = p
				p.children = append(p.children, e)
			} else if root == nil {
				root = e
			}
			stack = append(stack, e)
		case xml.EndElement:
			if len(stack) == 0 {
				return nil, errors.New("docxedit: unbalanced end element")
			}
			e := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			e.end = int(dec.InputOffset())
			e.selfClosing = e.end == e.startEnd
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].text += string(t)
			}
		}
	}
	if root == nil {
		return nil, io.ErrUnexpectedEOF
	}
	if len(stack) != 0 {
		return nil, io.ErrUnexpectedEOF
	}
	return root, nil
}

// wordPrefix finds the prefix bound to the WordprocessingML namespace on the
// root element ("" when it is the default namespace).
func wordPrefix(root *elem) (string, bool) {
	for _, a := range root.attrs {
		if a.Value != nsW {
			continue
		}
		if a.Name.Space == "xmlns" {
			return a.Name.Local, true
		}
		if a.Name.Space == "" && a.Name.Local == "xmlns" {
			return "", true
		}
	}
	return "", false
}

// walker carries the Word prefix for element tests.
type walker struct{ wp string }

func (w walker) is(e *elem, local string) bool {
	return e != nil && e.prefix == w.wp && e.local == local
}

func (w walker) child(e *elem, local string) *elem {
	if e == nil {
		return nil
	}
	for _, c := range e.children {
		if w.is(c, local) {
			return c
		}
	}
	return nil
}

func (w walker) childrenNamed(e *elem, local string) []*elem {
	var out []*elem
	if e == nil {
		return out
	}
	for _, c := range e.children {
		if w.is(c, local) {
			out = append(out, c)
		}
	}
	return out
}

func (w walker) descendants(e *elem, local string) []*elem {
	var out []*elem
	var rec func(*elem)
	rec = func(x *elem) {
		if w.is(x, local) {
			out = append(out, x)
		}
		for _, c := range x.children {
			rec(c)
		}
	}
	rec(e)
	return out
}

func (w walker) attr(e *elem, local string) (string, bool) {
	for _, a := range e.attrs {
		if a.Name.Space == w.wp && a.Name.Local == local {
			return a.Value, true
		}
	}
	return "", false
}

// blockChildren mirrors docformat.blockChildren: unwrap w:sdt/w:sdtContent
// and w:customXml.
func (w walker) blockChildren(parent *elem) []*elem {
	var out []*elem
	for _, c := range parent.children {
		switch {
		case w.is(c, "sdt"):
			if inner := w.child(c, "sdtContent"); inner != nil {
				out = append(out, w.blockChildren(inner)...)
			}
		case w.is(c, "customXml"):
			out = append(out, w.blockChildren(c)...)
		default:
			out = append(out, c)
		}
	}
	return out
}

// segment kinds of a run child.
const (
	segText   = iota // contributes to visible text (t, tab, br)
	segSilent        // deletable, no visible text (cr, lastRenderedPageBreak, ...)
	segKeep          // structural: drawings, fields, refs — never deleted by ReplaceText
	segProps         // w:rPr
)

type seg struct {
	el   *elem
	kind int
	text string
	// hardBreak marks a page/column w:br: text "\n" for parity, but
	// ReplaceText keeps it.
	hardBreak bool
}

type run struct {
	el   *elem
	rPr  *elem
	segs []seg
	// insParent is the w:ins that is the run's direct parent, if any;
	// insOuter is the outermost w:ins / w:moveTo ancestor inside the paragraph.
	insParent, insOuter *elem
	// inField marks a run inside a field result (between a complex field's
	// begin and end, or inside w:fldSimple).
	inField bool
}

func (r *run) hasText() bool {
	for _, s := range r.segs {
		if s.kind == segText {
			return true
		}
	}
	return false
}

type para struct {
	el      *elem
	inTable bool
	runs    []*run
	text    string
}

func (w walker) classify(c *elem) seg {
	s := seg{el: c, kind: segKeep}
	if c.prefix != w.wp {
		return s
	}
	switch c.local {
	case "rPr":
		s.kind = segProps
	case "t":
		s.kind, s.text = segText, c.text
	case "tab":
		s.kind, s.text = segText, "\t"
	case "br":
		s.kind, s.text = segText, "\n"
		if t, ok := w.attr(c, "type"); ok && (t == "page" || t == "column") {
			s.hardBreak = true
		}
	case "cr", "lastRenderedPageBreak", "noBreakHyphen", "softHyphen", "sym":
		s.kind = segSilent
	}
	return s
}

// collectRuns returns the visible runs of a paragraph in document order:
// direct runs and runs nested in hyperlinks, w:ins, w:moveTo, smartTags,
// content controls, simple fields... Runs in w:del / w:moveFrom are not
// visible text. Nested paragraphs (text boxes) are never reached because
// runs are not descended into.
func (w walker) collectRuns(p *elem) []*run {
	var out []*run
	depth := 0 // complex-field nesting
	var rec func(e *elem, insOuter *elem, fldSimple bool)
	rec = func(e *elem, insOuter *elem, fldSimple bool) {
		for _, c := range e.children {
			if c.prefix != w.wp {
				continue
			}
			switch c.local {
			case "r":
				r := &run{el: c, insOuter: insOuter, inField: fldSimple || depth > 0}
				if w.is(e, "ins") {
					r.insParent = e
				}
				for _, rc := range c.children {
					if w.is(rc, "fldChar") {
						switch t, _ := w.attr(rc, "fldCharType"); t {
						case "begin":
							depth++
						case "end":
							depth = max(depth-1, 0)
						}
					}
					s := w.classify(rc)
					if s.kind == segProps {
						if r.rPr == nil {
							r.rPr = rc
						}
						continue
					}
					r.segs = append(r.segs, s)
				}
				out = append(out, r)
			case "pPr", "del", "moveFrom", "p", "tbl":
			case "ins", "moveTo":
				o := insOuter
				if o == nil {
					o = c
				}
				rec(c, o, fldSimple)
			case "fldSimple":
				rec(c, insOuter, true)
			default:
				rec(c, insOuter, fldSimple)
			}
		}
	}
	rec(p, nil, false)
	return out
}

func (w walker) newPara(p *elem, inTable bool) *para {
	pp := &para{el: p, inTable: inTable, runs: w.collectRuns(p)}
	var sb strings.Builder
	for _, r := range pp.runs {
		for _, s := range r.segs {
			sb.WriteString(s.text)
		}
	}
	pp.text = sb.String()
	return pp
}

// enumerate replicates docformat.InspectDocx's paragraph and section order.
func (w walker) enumerate(root *elem) ([]*para, []*elem, error) {
	body := w.child(root, "body")
	if body == nil {
		return nil, nil, errors.New("docxedit: w:body missing")
	}
	var paras []*para
	var sects []*elem
	add := func(p *elem, inTable bool) {
		paras = append(paras, w.newPara(p, inTable))
		if sp := w.child(w.child(p, "pPr"), "sectPr"); sp != nil {
			sects = append(sects, sp)
		}
	}
	addWithBoxes := func(p *elem, inTable bool) {
		add(p, inTable)
		for _, tx := range w.descendants(p, "txbxContent") {
			for _, tp := range w.childrenNamed(tx, "p") {
				add(tp, inTable)
			}
		}
	}
	var addTable func(tbl *elem)
	addTable = func(tbl *elem) {
		for _, tr := range w.childrenNamed(tbl, "tr") {
			for _, tc := range w.childrenNamed(tr, "tc") {
				for _, c := range w.blockChildren(tc) {
					switch {
					case w.is(c, "p"):
						addWithBoxes(c, true)
					case w.is(c, "tbl"):
						addTable(c)
					}
				}
			}
		}
	}
	for _, c := range w.blockChildren(body) {
		switch {
		case w.is(c, "p"):
			addWithBoxes(c, false)
		case w.is(c, "tbl"):
			addTable(c)
		case w.is(c, "sectPr"):
			sects = append(sects, c)
		}
	}
	return paras, sects, nil
}
