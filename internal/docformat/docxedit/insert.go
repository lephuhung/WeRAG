package docxedit

import (
	"errors"
	"fmt"
)

// NewParagraph describes a paragraph to insert.
type NewParagraph struct {
	Text string // "\t" → w:tab, "\n" → w:br
	// InheritFrom copies that paragraph's w:pPr and its first text run's
	// w:rPr. Default: the anchor paragraph, or the first non-table
	// paragraph when inserting at -1. The copied w:pPr never includes
	// sectPr, rPr, pPrChange, pageBreakBefore or framePr; numPr (list
	// numbering) is kept only when InheritFrom is set explicitly or
	// KeepNumbering is true, so a paragraph added after a list item is not
	// itself numbered by default.
	InheritFrom   *int
	KeepNumbering bool
	Para          *ParaProps // overrides on top of the inherited w:pPr
	Run           *RunProps  // overrides on top of the inherited w:rPr
}

// insertion is a planned paragraph insertion.
type insertion struct {
	at int // byte offset of the new w:p
	sp []splice
}

// planInsertParagraphAfter builds the new paragraph and where it goes.
func (d *Document) planInsertParagraphAfter(index int, np NewParagraph, a Author) (insertion, error) {
	var at int
	var anchor *para
	src := -1
	switch {
	case index == -1:
		at = d.bodyStart()
		for i, p := range d.paras {
			if !p.inTable {
				src = i
				break
			}
		}
		if src < 0 && len(d.paras) > 0 {
			src = 0
		}
	default:
		p, err := d.para(index)
		if err != nil {
			return insertion{}, err
		}
		at, src, anchor = p.el.end, index, p
	}
	if np.InheritFrom != nil {
		if _, err := d.para(*np.InheritFrom); err != nil {
			return insertion{}, fmt.Errorf("docxedit: InheritFrom: %w", err)
		}
		src = *np.InheritFrom
	}
	if np.Para != nil {
		if err := validateParaProps(np.Para); err != nil {
			return insertion{}, err
		}
	}
	if r := np.Run; r != nil {
		if err := validateRunProps(*r); err != nil {
			return insertion{}, err
		}
	}

	var pPrKids, rPrKids []kid
	if src >= 0 {
		sp := d.paras[src]
		pPrKids = withoutKids(d.kids(d.w.child(sp.el, "pPr")),
			"rPr", "sectPr", "pPrChange", "pageBreakBefore", "framePr")
		if np.InheritFrom == nil && !np.KeepNumbering {
			pPrKids = removeKid(pPrKids, "numPr")
		}
		for _, r := range sp.runs {
			if r.hasText() {
				rPrKids = removeKid(d.kids(r.rPr), "rPrChange")
				break
			}
		}
	}
	if np.Para != nil {
		pPrKids = d.applyParaProps(pPrKids, *np.Para)
	}
	if np.Run != nil {
		rPrKids = d.applyRunProps(rPrKids, *np.Run)
	}
	rPrKids = sortKids(rPrKids, rPrOrder)

	// a section break on the anchor moves to the new paragraph (Word does
	// this on Enter), so the inserted paragraph stays in the same section
	var sps []splice
	if anchor != nil {
		if sect := d.w.child(d.w.child(anchor.el, "pPr"), "sectPr"); sect != nil {
			sps = append(sps, splice{sect.start, sect.end, ""})
			pPrKids = append(pPrKids, kid{local: "sectPr", xml: d.raw(sect)})
		}
	}
	// the paragraph mark is an insertion, and carries the run formatting
	mark := kid{local: "rPr", xml: "<" + d.q("rPr") + "><" + d.q("ins") + d.revAttrs(a) + "/>" +
		joinKids(rPrKids) + "</" + d.q("rPr") + ">"}
	pPrKids = sortKids(append(pPrKids, mark), pPrOrder)
	xml := "<" + d.q("p") + "><" + d.q("pPr") + ">" + joinKids(pPrKids) + "</" + d.q("pPr") + ">"
	if np.Text != "" {
		runRPr := ""
		if len(rPrKids) > 0 {
			runRPr = "<" + d.q("rPr") + ">" + joinKids(rPrKids) + "</" + d.q("rPr") + ">"
		}
		xml += d.insXML(np.Text, runRPr, a)
	}
	xml += "</" + d.q("p") + ">"
	return insertion{at: at, sp: append(sps, splice{at, at, xml})}, nil
}

// bodyStart is where a paragraph goes to become the first block of the body.
func (d *Document) bodyStart() int {
	body := d.w.child(d.root, "body")
	for _, c := range d.w.blockChildren(body) {
		if d.w.is(c, "p") || d.w.is(c, "tbl") || d.w.is(c, "sectPr") {
			return c.start
		}
	}
	return d.endTagStart(body)
}

// InsertParagraphAfter inserts a new paragraph right after paragraph index
// (index -1: before the first block of the body), as a tracked insertion:
// the new paragraph mark carries w:pPr/w:rPr/w:ins and its text run is
// wrapped in w:ins. Inside a table cell or text box it stays in that
// container. When the anchor paragraph contains text boxes, their
// paragraphs come first in docformat order, so the new index is then
// larger than index+1. Returns the new paragraph's index (InspectDocx
// order) in the updated document. A w:sectPr on the anchor moves to the new
// paragraph so it stays in the anchor's section (in a Batch this conflicts
// with other edits of that w:pPr or section).
func (d *Document) InsertParagraphAfter(index int, p NewParagraph, a Author) (int, error) {
	ins, err := d.planInsertParagraphAfter(index, p, a)
	if err != nil {
		return -1, err
	}
	// earlier splices (the moved section break) shift the new paragraph
	at := ins.at
	for _, s := range ins.sp {
		if s.end <= ins.at && s.start != ins.at {
			at += len(s.text) - (s.end - s.start)
		}
	}
	if err := d.apply(ins.sp); err != nil {
		return -1, err
	}
	for i, pp := range d.paras {
		if pp.el.start == at {
			return i, nil
		}
	}
	return -1, errors.New("docxedit: internal error: inserted paragraph not found")
}
