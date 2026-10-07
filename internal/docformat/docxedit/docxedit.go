// Package docxedit writes edits back into a .docx as Word tracked changes
// (revisions) that a reviewer can accept or reject one by one in Word or
// ONLYOFFICE.
//
// The package never re-serializes the XML tree: word/document.xml is
// tokenized with encoding/xml (RawToken + InputOffset) to find the byte
// range of every element of interest, and each mutating call applies byte
// splices to the raw part, then re-tokenizes it. Namespace prefixes,
// comments, processing instructions, unknown extensions and formatting of
// untouched markup are therefore preserved exactly. Every other zip entry
// is copied byte-for-byte (zip.Writer.Copy), in the original order.
//
// # Paragraph enumeration
//
// Paragraphs are numbered exactly like docformat.InspectDocx: body
// paragraphs and tables in order (content controls and w:customXml
// unwrapped), table cells row by row (nested tables recursively), and after
// each paragraph the paragraphs of every w:txbxContent below it. Sections
// follow docformat Layout.Sections order (a w:pPr/w:sectPr when its paragraph
// is enumerated, the body's w:sectPr where it occurs).
//
// Known parity gaps with docformat (paragraph count and order always match;
// only Paragraph.Text can differ):
//   - docformat appends the text of runs nested in hyperlinks / w:ins /
//     smart tags after the paragraph's direct runs, and only their w:t (no
//     tabs/breaks); docxedit keeps document order and includes tabs/breaks.
//   - docformat includes text-box text in the anchoring paragraph's text
//     (twice when mc:AlternateContent carries both Choice and Fallback);
//     docxedit does not descend into drawings, so the anchor paragraph's
//     Text excludes it. Text-box paragraphs themselves are enumerated
//     (twice, like docformat, when Choice and Fallback both exist); editing
//     one copy leaves the other copy unchanged.
//
// # Visible text
//
// Text is the concatenation, in document order, of w:t, w:tab ("\t") and
// w:br ("\n") that are direct children of visible runs: direct w:r children
// of the paragraph and runs nested in w:hyperlink, w:ins, w:moveTo,
// w:smartTag, w:sdt, w:customXml, w:fldSimple... Runs inside w:del and
// w:moveFrom are not visible.
//
// # Preserved run content
//
// ReplaceText and ReplaceSubstring delete only text-like run children (w:t,
// w:tab, plain w:br, w:cr, w:sym, hyphens, w:lastRenderedPageBreak). Other
// run children — drawings, pictures, objects, field characters and field
// instructions, footnote/endnote/comment references — are kept OUTSIDE the
// deletion: the run is split into kept runs and w:del-wrapped runs that
// share the original w:rPr. ReplaceText also keeps page/column breaks.
// A field's result text is deleted; its field codes stay, and ReplaceText
// never places the new text inside a field result. Adjacent deleted runs
// (siblings separated only by bookmarks / proofErr / comment ranges) share
// one w:del. When the anchor run sits in a w:hyperlink the inserted run is
// placed inside the same hyperlink (it keeps the link).
//
// # Revision ids
//
// w:id values continue above the largest numeric w:id found in
// word/document.xml and in every other word/*.xml part (comments, notes,
// headers...), bookmarks included.
package docxedit

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Author identifies who made the tracked change.
type Author struct {
	Name string
	Date time.Time // zero → time.Now().UTC()
}

// Paragraph is one paragraph in docformat.InspectDocx order.
type Paragraph struct {
	Index   int    // same index as docformat.InspectDocx(...).Paragraphs
	Text    string // visible text, tabs as "\t", breaks as "\n"
	InTable bool
	Empty   bool
}

// RunProps are character properties to set; nil leaves a property unchanged.
type RunProps struct {
	Font                          *string  // w:rFonts ascii/hAnsi/cs/eastAsia
	SizePt                        *float64 // w:sz and w:szCs
	Bold, Italic, Caps, Underline *bool
}

// LineSpacing sets exactly one of the three rules.
type LineSpacing struct {
	Multiple  *float64
	ExactPt   *float64
	AtLeastPt *float64
}

// ParaProps are paragraph properties to set; nil leaves a property unchanged.
type ParaProps struct {
	Alignment                                           *string // left | center | right | both
	IndentLeftPt, IndentRightPt, FirstLinePt, HangingPt *float64
	SpaceBeforePt, SpaceAfterPt                         *float64
	LineSpacing                                         *LineSpacing
}

// SectionProps are page-setup values to set; nil leaves a value unchanged.
type SectionProps struct {
	PageWidthPt, PageHeightPt                                *float64
	MarginTopPt, MarginBottomPt, MarginLeftPt, MarginRightPt *float64
	HeaderPt, FooterPt, GutterPt                             *float64
	Orientation                                              *string // portrait | landscape
}

const documentPart = "word/document.xml"

// Document is an opened .docx being edited.
type Document struct {
	zr     *zip.Reader
	xml    []byte // current word/document.xml
	w      walker
	root   *elem
	paras  []*para
	sects  []*elem
	nextID int
	dirty  bool // an edit changed document.xml since Open
}

// Open parses .docx bytes.
func Open(content []byte) (*Document, error) {
	zr, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return nil, fmt.Errorf("docxedit: not a .docx/.zip package: %w", err)
	}
	d := &Document{zr: zr}
	var found bool
	maxID := -1
	for _, f := range zr.File {
		if f.Name == documentPart {
			if d.xml, err = readEntry(f); err != nil {
				return nil, err
			}
			found = true
			continue
		}
		if strings.HasPrefix(f.Name, "word/") && strings.HasSuffix(f.Name, ".xml") {
			// revision ids should be unique across the package's parts
			data, err := readEntry(f)
			if err != nil {
				return nil, err
			}
			for _, m := range idAttrRe.FindAllSubmatch(data, -1) {
				if n, err := strconv.Atoi(string(m[1])); err == nil && n > maxID {
					maxID = n
				}
			}
		}
	}
	if !found {
		return nil, errors.New("docxedit: word/document.xml missing")
	}
	if err := d.reload(d.xml); err != nil {
		return nil, err
	}
	var scan func(*elem)
	scan = func(e *elem) {
		if v, ok := d.w.attr(e, "id"); ok {
			if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > maxID {
				maxID = n
			}
		}
		for _, c := range e.children {
			scan(c)
		}
	}
	scan(d.root)
	d.nextID = maxID + 1
	return d, nil
}

var idAttrRe = regexp.MustCompile(`[A-Za-z_][\w.-]*:id="\s*(\d+)\s*"`)

const maxPartBytes = 64 << 20

func readEntry(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, maxPartBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxPartBytes {
		return nil, fmt.Errorf("docxedit: %s exceeds %d MiB", f.Name, maxPartBytes>>20)
	}
	return data, nil
}

// reload re-tokenizes data and replaces the document state on success.
func (d *Document) reload(data []byte) error {
	root, err := parseTree(data)
	if err != nil {
		return fmt.Errorf("docxedit: document.xml: %w", err)
	}
	wp, ok := wordPrefix(root)
	if !ok {
		return errors.New("docxedit: WordprocessingML namespace not declared on the root element")
	}
	w := walker{wp: wp}
	paras, sects, err := w.enumerate(root)
	if err != nil {
		return err
	}
	d.xml, d.w, d.root, d.paras, d.sects = data, w, root, paras, sects
	return nil
}

// Paragraphs lists every paragraph in docformat order.
func (d *Document) Paragraphs() []Paragraph {
	out := make([]Paragraph, len(d.paras))
	for i, p := range d.paras {
		out[i] = Paragraph{Index: i, Text: p.text, InTable: p.inTable,
			Empty: strings.TrimSpace(p.text) == ""}
	}
	return out
}

// FindParagraph returns the index of the first paragraph whose normalized
// text (canonically composed, whitespace collapsed, lower-cased, diacritics
// kept) contains the normalized needle; -1, false when none does. When
// several match, a paragraph whose text equals the needle wins (ambiguous
// only if several are equal); otherwise the first match is returned with
// ambiguous = true.
func (d *Document) FindParagraph(needle string) (index int, ambiguous bool) {
	n := normalizeText(needle)
	if n == "" {
		return -1, false
	}
	var hits, exact []int
	for i, p := range d.paras {
		t := normalizeText(p.text)
		if strings.Contains(t, n) {
			hits = append(hits, i)
			if t == n {
				exact = append(exact, i)
			}
		}
	}
	switch {
	case len(hits) == 0:
		return -1, false
	case len(hits) == 1:
		return hits[0], false
	case len(exact) > 0:
		return exact[0], len(exact) > 1
	default:
		return hits[0], true
	}
}

// Bytes returns the edited .docx. Entries other than word/document.xml are
// copied raw (same compressed bytes, CRC and header), in the original order.
func (d *Document) Bytes() ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if c := d.zr.Comment; c != "" {
		if err := zw.SetComment(c); err != nil {
			return nil, err
		}
	}
	for _, f := range d.zr.File {
		if f.Name != documentPart {
			if err := zw.Copy(f); err != nil {
				return nil, fmt.Errorf("docxedit: copy %s: %w", f.Name, err)
			}
			continue
		}
		h := f.FileHeader
		h.Method = zip.Deflate
		h.CRC32, h.CompressedSize, h.CompressedSize64 = 0, 0, 0
		h.UncompressedSize, h.UncompressedSize64 = 0, 0
		h.Flags &^= 0x1 // never encrypted
		h.Extra = nil   // drop stale zip64 / size extras
		w, err := zw.CreateHeader(&h)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(d.xml); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ---------------------------------------------------------------------------
// splicing

type splice struct {
	start, end int
	text       string
}

// apply rewrites document.xml with non-overlapping splices and re-tokenizes.
func (d *Document) apply(sp []splice) error {
	if len(sp) == 0 {
		return nil
	}
	// descending offsets; at one offset the replacement goes first so an
	// insertion there ends up in front of it
	sort.SliceStable(sp, func(i, j int) bool {
		if sp[i].start != sp[j].start {
			return sp[i].start > sp[j].start
		}
		return sp[i].end > sp[j].end
	})
	for i := 1; i < len(sp); i++ {
		if overlaps(sp[i], sp[i-1]) {
			return errors.New("docxedit: internal error: overlapping edits")
		}
	}
	out := append([]byte(nil), d.xml...)
	for _, s := range sp {
		out = append(out[:s.start], append([]byte(s.text), out[s.end:]...)...)
	}
	if err := d.reload(out); err != nil {
		return err
	}
	d.dirty = true
	return nil
}

// Dirty reports whether any edit changed document.xml since Open. An edit
// asking for values the document already has records nothing, so a
// document whose edits were all such no-ops stays clean.
func (d *Document) Dirty() bool { return d.dirty }

// overlaps reports whether two splices conflict: their ranges intersect, or
// both are insertions at the same offset (their order would be undefined).
// An insertion at the start or end of a replaced range is fine: it lands
// before / after the replacement.
func overlaps(a, b splice) bool {
	if a.start == a.end && b.start == b.end {
		return a.start == b.start
	}
	return a.start < b.end && b.start < a.end
}

func (d *Document) para(index int) (*para, error) {
	if index < 0 || index >= len(d.paras) {
		return nil, fmt.Errorf("docxedit: paragraph index %d out of range [0,%d)", index, len(d.paras))
	}
	return d.paras[index], nil
}

// q is the qualified name of a w: element.
func (d *Document) q(local string) string {
	if d.w.wp == "" {
		return local
	}
	return d.w.wp + ":" + local
}

func qname(e *elem) string {
	if e.prefix == "" {
		return e.local
	}
	return e.prefix + ":" + e.local
}

func (d *Document) raw(e *elem) string { return string(d.xml[e.start:e.end]) }

// openTag is e's start tag, turned into a non-empty one if self-closing.
func (d *Document) openTag(e *elem) string {
	if !e.selfClosing {
		return string(d.xml[e.start:e.startEnd])
	}
	s := strings.TrimRight(string(d.xml[e.start:e.end]), " \t\r\n")
	s = strings.TrimSuffix(s, "/>")
	return strings.TrimRight(s, " \t\r\n") + ">"
}

func closeTag(e *elem) string { return "</" + qname(e) + ">" }

// endTagStart is the offset where e's end tag begins.
func (d *Document) endTagStart(e *elem) int {
	if e.selfClosing {
		return e.end
	}
	return e.start + bytes.LastIndex(d.xml[e.start:e.end], []byte("</"))
}

// revAttrs allocates a revision id and renders w:id/w:author/w:date.
func (d *Document) revAttrs(a Author) string {
	id := d.nextID
	d.nextID++
	date := a.Date
	if date.IsZero() {
		date = time.Now()
	}
	return fmt.Sprintf(` %s="%d" %s="%s" %s="%s"`, d.q("id"), id,
		d.q("author"), escAttr(a.Name), d.q("date"), date.UTC().Format("2006-01-02T15:04:05Z"))
}
