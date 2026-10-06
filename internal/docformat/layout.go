package docformat

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Layout extraction: the effective paragraph/run formatting of a .docx that
// the NĐ30 checks need — alignment, font, size, emphasis, indentation,
// spacing, tab stops, table-column zone and section page setup.

const (
	twipsPerMM = 1440.0 / 25.4
	// maxPartBytes caps how much of one zip entry is decompressed; real
	// document.xml parts are a few MB, a larger one is a zip bomb.
	maxPartBytes = 64 << 20
)

// Zones a text fragment can sit in.
const (
	ZoneLeft  = "left"
	ZoneRight = "right"
	ZoneFull  = "full"
	ZoneSplit = "split" // one paragraph, text on both sides of a tab stop
)

// Run is one w:r with its effective formatting.
type Run struct {
	Text      string   `json:"text"`
	FontName  *string  `json:"font_name"`
	SizePt    *float64 `json:"size_pt"`
	Bold      *bool    `json:"bold"`
	Italic    *bool    `json:"italic"`
	Underline *bool    `json:"underline"`
	Caps      *bool    `json:"caps"`
}

// SideFormat is the formatting of the dominant run on one side of a
// tab-split paragraph. Present is false when that side has no text run.
type SideFormat struct {
	Present   bool
	FontName  *string
	SizePt    *float64
	Bold      *bool
	Italic    *bool
	Underline *bool
	Caps      *bool
}

// Para is one paragraph with resolved formatting and position.
type Para struct {
	Index             int        `json:"index"`
	Text              string     `json:"text"`
	Source            string     `json:"source"` // body | table_cell | textbox
	StyleID           *string    `json:"style_id,omitempty"`
	StyleName         *string    `json:"style_name"`
	Alignment         string     `json:"alignment"`
	FontName          *string    `json:"font_name"`
	SizePt            *float64   `json:"size_pt"`
	Bold              *bool      `json:"bold"`
	Italic            *bool      `json:"italic"`
	Underline         *bool      `json:"underline"`
	Caps              *bool      `json:"caps"`
	TextIsUpper       bool       `json:"text_is_upper"`
	IndentLeftMM      *float64   `json:"indent_left_mm"`
	IndentRightMM     *float64   `json:"indent_right_mm,omitempty"`
	IndentFirstLineMM *float64   `json:"indent_first_line_mm"`
	IndentHangingMM   *float64   `json:"indent_hanging_mm,omitempty"`
	SpaceBeforePt     *float64   `json:"space_before_pt"`
	SpaceAfterPt      *float64   `json:"space_after_pt"`
	LineSpacingPt     *float64   `json:"line_spacing_pt"`
	LineSpacingMult   *float64   `json:"line_spacing_multiple"`
	PageBreakBefore   bool       `json:"page_break_before"`
	InTable           bool       `json:"in_table"`
	Zone              string     `json:"zone"`
	LeftText          string     `json:"left_text,omitempty"`
	RightText         string     `json:"right_text,omitempty"`
	LeftProps         SideFormat `json:"-"`
	RightProps        SideFormat `json:"-"`
	CellLeftMM        *float64   `json:"-"`
	CellRightMM       *float64   `json:"-"`
	TableIndex        *int       `json:"table_index"`
	TableLayout       string     `json:"table_layout,omitempty"` // columns | data
	Row               *int       `json:"row"`
	Col               *int       `json:"col"`
	Section           int        `json:"section"`
	Runs              []Run      `json:"-"`
}

// Section is one w:sectPr page setup.
type Section struct {
	Index          int      `json:"index"`
	PageWidthMM    *float64 `json:"page_width_mm,omitempty"`
	PageHeightMM   *float64 `json:"page_height_mm,omitempty"`
	Orientation    *string  `json:"orientation,omitempty"`
	MarginTopMM    *float64 `json:"margin_top_mm,omitempty"`
	MarginRightMM  *float64 `json:"margin_right_mm,omitempty"`
	MarginBottomMM *float64 `json:"margin_bottom_mm,omitempty"`
	MarginLeftMM   *float64 `json:"margin_left_mm,omitempty"`
	HeaderMM       *float64 `json:"header_mm,omitempty"`
	FooterMM       *float64 `json:"footer_mm,omitempty"`
	GutterMM       *float64 `json:"gutter_mm,omitempty"`
}

// Layout is the parsed document.
type Layout struct {
	Sections   []*Section
	Paragraphs []*Para // flat, document order
	NTables    int
	HasHeaders bool
	HasFooters bool
	Errors     []string
}

// ---------------------------------------------------------------------------
// value helpers

func ptr[T any](v T) *T { return &v }

// round2 rounds like Python's round(x, 2): the correctly rounded decimal of
// the binary value.
func round2(x float64) float64 {
	v, _ := strconv.ParseFloat(strconv.FormatFloat(x, 'f', 2, 64), 64)
	return v
}

func pyInt(s string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	return n, err == nil
}

func twipsToMM(s string, ok bool) *float64 {
	if !ok {
		return nil
	}
	n, good := pyInt(s)
	if !good {
		return nil
	}
	return ptr(round2(float64(n) / twipsPerMM))
}

func halfPoints(s string, ok bool) *float64 {
	if !ok {
		return nil
	}
	n, good := pyInt(s)
	if !good {
		return nil
	}
	return ptr(float64(n) / 2.0)
}

// boolProp reads a tri-state toggle (w:b, w:i, ...): absent → nil handled by
// the caller, no w:val → on, w:val=0/false/off → off.
func boolProp(el *node) bool {
	v, ok := el.wattr("val")
	if !ok {
		return true
	}
	return v != "0" && v != "false" && v != "off"
}

// props is a sparse property bag (docDefaults / style / direct formatting)
// merged in precedence order; absent keys inherit.
type props map[string]any

func merge(base, overlay props) props {
	out := make(props, len(base)+len(overlay))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range overlay {
		if v != nil {
			out[k] = v
		}
	}
	return out
}

func (p props) str(k string) *string {
	if v, ok := p[k].(string); ok {
		return &v
	}
	return nil
}

func (p props) num(k string) *float64 {
	if v, ok := p[k].(float64); ok {
		return &v
	}
	return nil
}

func (p props) flag(k string) *bool {
	if v, ok := p[k].(bool); ok {
		return &v
	}
	return nil
}

type tabStop struct {
	posMM *float64
	val   string
}

func setDefault(p props, k string, v any) {
	if _, ok := p[k]; !ok {
		p[k] = v
	}
}

func rprProps(rpr *node) props {
	out := props{}
	if rpr == nil {
		return out
	}
	if fonts := rpr.child("rFonts"); fonts != nil {
		for _, key := range []string{"ascii", "hAnsi", "eastAsia", "cs",
			"asciiTheme", "hAnsiTheme", "eastAsiaTheme", "cstheme"} {
			if v, ok := fonts.wattr(key); ok && v != "" {
				setDefault(out, "font_"+key, v)
			}
		}
	}
	if sz := rpr.child("sz"); sz != nil {
		if v := halfPoints(sz.wattr("val")); v != nil {
			out["size_pt"] = *v
		}
	}
	for _, t := range []struct{ tag, key string }{
		{"b", "bold"}, {"i", "italic"}, {"caps", "caps"},
	} {
		if el := rpr.child(t.tag); el != nil {
			out[t.key] = boolProp(el)
		}
	}
	if u := rpr.child("u"); u != nil {
		v, _ := u.wattr("val")
		out["underline"] = v != "none"
	}
	return out
}

func pprProps(ppr *node) props {
	out := props{}
	if ppr == nil {
		return out
	}
	if jc := ppr.child("jc"); jc != nil {
		if v, ok := jc.wattr("val"); ok && v != "" {
			switch v {
			case "both":
				v = "justify"
			}
			out["alignment"] = v
		}
	}
	if ind := ppr.child("ind"); ind != nil {
		for _, a := range []struct{ attr, key string }{
			{"left", "indent_left_mm"}, {"start", "indent_left_mm"},
			{"right", "indent_right_mm"}, {"end", "indent_right_mm"},
			{"firstLine", "indent_first_line_mm"}, {"hanging", "indent_hanging_mm"},
		} {
			if mm := twipsToMM(ind.wattr(a.attr)); mm != nil {
				setDefault(out, a.key, *mm)
			}
		}
	}
	if sp := ppr.child("spacing"); sp != nil {
		for _, a := range []struct{ attr, key string }{
			{"before", "space_before_pt"}, {"after", "space_after_pt"},
		} {
			if v, ok := sp.wattr(a.attr); ok {
				if n, good := pyInt(v); good {
					setDefault(out, a.key, float64(n)/20.0)
				}
			}
		}
		if line, ok := sp.wattr("line"); ok {
			if lv, good := pyInt(line); good {
				rule, _ := sp.wattr("lineRule")
				if rule == "exact" || rule == "atLeast" {
					out["line_spacing_pt"] = float64(lv) / 20.0
				} else {
					out["line_spacing_multiple"] = round2(float64(lv) / 240.0)
				}
			}
		}
	}
	if tabs := ppr.child("tabs"); tabs != nil {
		var stops []tabStop
		for _, t := range tabs.childrenNamed("tab") {
			v, _ := t.wattr("val")
			stops = append(stops, tabStop{posMM: twipsToMM(t.wattr("pos")), val: v})
		}
		out["tab_stops"] = stops
	}
	if pb := ppr.child("pageBreakBefore"); pb != nil {
		out["page_break_before"] = boolProp(pb)
	}
	if ps := ppr.child("pStyle"); ps != nil {
		if v, ok := ps.wattr("val"); ok && v != "" {
			out["style_id"] = v
		}
	}
	return out
}

type style struct {
	name    string
	basedOn string
	ppr     props
	rpr     props
}

func parseStyles(data []byte) (docRPr, docPPr props, styles map[string]*style) {
	docRPr, docPPr, styles = props{}, props{}, map[string]*style{}
	root, err := parseXML(data)
	if err != nil {
		return
	}
	if dd := root.child("docDefaults"); dd != nil {
		if r := dd.child("rPrDefault"); r != nil {
			docRPr = rprProps(r.child("rPr"))
		}
		if p := dd.child("pPrDefault"); p != nil {
			docPPr = pprProps(p.child("pPr"))
		}
	}
	for _, st := range root.childrenNamed("style") {
		sid, ok := st.wattr("styleId")
		if !ok || sid == "" {
			continue
		}
		s := &style{name: sid, ppr: pprProps(st.child("pPr")), rpr: rprProps(st.child("rPr"))}
		if n := st.child("name"); n != nil {
			s.name, _ = n.wattr("val")
		}
		if b := st.child("basedOn"); b != nil {
			s.basedOn, _ = b.wattr("val")
		}
		styles[sid] = s
	}
	return
}

// resolveStyle merges pPr/rPr along the basedOn chain, ancestors first.
func resolveStyle(styles map[string]*style, id string) (props, props, *string) {
	ppr, rpr := props{}, props{}
	var chain []string
	seen := map[string]bool{}
	for cur := id; cur != "" && styles[cur] != nil && !seen[cur]; cur = styles[cur].basedOn {
		seen[cur] = true
		chain = append(chain, cur)
	}
	for i := len(chain) - 1; i >= 0; i-- {
		st := styles[chain[i]]
		for k, v := range st.ppr {
			ppr[k] = v
		}
		for k, v := range st.rpr {
			rpr[k] = v
		}
	}
	if len(chain) == 0 {
		return ppr, rpr, nil
	}
	name := styles[chain[0]].name
	return ppr, rpr, &name
}

func themeFonts(files map[string]*zip.File) map[string]string {
	out := map[string]string{}
	data, err := readPart(files, "word/theme/theme1.xml")
	if err != nil {
		return out
	}
	root, err := parseXML(data)
	if err != nil {
		return out
	}
	var scheme *node
	root.walk(func(n *node) {
		if scheme == nil && n != root && n.space == nsA && n.local == "fontScheme" {
			scheme = n
		}
	})
	if scheme == nil {
		return out
	}
	for _, w := range []struct{ tag, key string }{{"majorFont", "major"}, {"minorFont", "minor"}} {
		for _, el := range scheme.children {
			if el.space != nsA || el.local != w.tag {
				continue
			}
			for _, l := range el.children {
				if l.space == nsA && l.local == "latin" {
					if tf, ok := l.plainAttr("typeface"); ok && tf != "" {
						out[w.key] = tf
					}
					break
				}
			}
			break
		}
	}
	return out
}

var themeTokenRe = regexp.MustCompile(`(?i)^(major|minor)(HAnsi|Ascii|Bidi|EastAsia)$`)

// effectiveFont picks the latin font: explicit ascii/hAnsi, else a theme
// token resolved through the theme, else eastAsia.
func effectiveFont(rpr props, theme map[string]string) *string {
	for _, k := range []string{"font_ascii", "font_hAnsi"} {
		if v := rpr.str(k); v != nil && *v != "" {
			return v
		}
	}
	for _, k := range []string{"font_asciiTheme", "font_hAnsiTheme"} {
		if v := rpr.str(k); v != nil && *v != "" {
			if m := themeTokenRe.FindStringSubmatch(*v); m != nil {
				if f, ok := theme[strings.ToLower(m[1])]; ok {
					return &f
				}
			}
			s := "theme:" + *v
			return &s
		}
	}
	return rpr.str("font_eastAsia")
}

func parseSection(sp *node, index int) *Section {
	sec := &Section{Index: index}
	if pg := sp.child("pgSz"); pg != nil {
		sec.PageWidthMM = twipsToMM(pg.wattr("w"))
		sec.PageHeightMM = twipsToMM(pg.wattr("h"))
		if o, ok := pg.wattr("orient"); ok {
			sec.Orientation = &o
		} else if sec.PageWidthMM != nil && sec.PageHeightMM != nil &&
			*sec.PageWidthMM != 0 && *sec.PageHeightMM != 0 {
			o := "portrait"
			if *sec.PageWidthMM > *sec.PageHeightMM {
				o = "landscape"
			}
			sec.Orientation = &o
		}
	}
	if m := sp.child("pgMar"); m != nil {
		sec.MarginTopMM = twipsToMM(m.wattr("top"))
		sec.MarginRightMM = twipsToMM(m.wattr("right"))
		sec.MarginBottomMM = twipsToMM(m.wattr("bottom"))
		sec.MarginLeftMM = twipsToMM(m.wattr("left"))
		sec.HeaderMM = twipsToMM(m.wattr("header"))
		sec.FooterMM = twipsToMM(m.wattr("footer"))
		sec.GutterMM = twipsToMM(m.wattr("gutter"))
	}
	return sec
}

// ---------------------------------------------------------------------------
// builder

type builder struct {
	docRPr, docPPr props
	styles         map[string]*style
	theme          map[string]string
	paras          []*Para
	sections       []*Section
	sectionIdx     int
	nTables        int
	// every sectPr in document order: the body's final sectPr only comes
	// after all paragraphs, so it is pre-scanned to know page width and
	// margins while deriving zones.
	allSections []*Section
	tableLayout []string
}

func (b *builder) currentSection() *Section {
	if len(b.allSections) > 0 {
		i := b.sectionIdx
		if i > len(b.allSections)-1 {
			i = len(b.allSections) - 1
		}
		return b.allSections[i]
	}
	if len(b.sections) > 0 {
		return b.sections[len(b.sections)-1]
	}
	return nil
}

// contentBox is (left margin, content width) of the current section, mm.
func (b *builder) contentBox() (float64, float64) {
	sec := b.currentSection()
	if sec != nil && sec.PageWidthMM != nil && *sec.PageWidthMM != 0 &&
		sec.MarginLeftMM != nil && sec.MarginRightMM != nil {
		return *sec.MarginLeftMM, *sec.PageWidthMM - *sec.MarginLeftMM - *sec.MarginRightMM
	}
	return 30.0, 160.0
}

type cellExtent struct{ left, right float64 }

func (b *builder) runFromProps(text string, rp props) Run {
	return Run{
		Text: text, FontName: effectiveFont(rp, b.theme), SizePt: rp.num("size_pt"),
		Bold: rp.flag("bold"), Italic: rp.flag("italic"),
		Underline: rp.flag("underline"), Caps: rp.flag("caps"),
	}
}

func (b *builder) addParagraph(p *node, source string, cell *cellExtent, table, row, col *int) *Para {
	pprEl := p.child("pPr")
	pp := pprProps(pprEl)
	var styleID string
	if v := pp.str("style_id"); v != nil {
		styleID = *v
	}
	stPPr, stRPr, styleName := resolveStyle(b.styles, styleID)
	effPPr := merge(merge(b.docPPr, stPPr), pp)
	paraRPr := merge(b.docRPr, stRPr)

	// an inline sectPr ends the current section
	paraSection := b.sectionIdx
	if pprEl != nil {
		if sp := pprEl.child("sectPr"); sp != nil {
			b.sections = append(b.sections, parseSection(sp, b.sectionIdx))
			b.sectionIdx++
		}
	}

	var runs []Run
	direct := map[*node]bool{}
	for _, r := range p.childrenNamed("r") {
		direct[r] = true
		var sb strings.Builder
		r.walk(func(n *node) {
			switch {
			case n.is("t"):
				sb.WriteString(n.text)
			case n.is("tab"):
				sb.WriteByte('\t')
			case n.is("br"):
				sb.WriteByte('\n')
			}
		})
		runs = append(runs, b.runFromProps(sb.String(), merge(paraRPr, rprProps(r.child("rPr")))))
	}
	// runs nested in hyperlinks / smartTags / sdt
	for _, r := range p.descendants("r") {
		if direct[r] {
			continue
		}
		var sb strings.Builder
		for _, t := range r.descendants("t") {
			sb.WriteString(t.text)
		}
		if sb.Len() > 0 {
			runs = append(runs, b.runFromProps(sb.String(), merge(paraRPr, rprProps(r.child("rPr")))))
		}
	}

	var tb strings.Builder
	for _, r := range runs {
		tb.WriteString(r.Text)
	}
	text := tb.String()
	primary := primaryRun(runs)

	para := &Para{
		Index: len(b.paras), Text: text, Source: source,
		StyleID: pp.str("style_id"), StyleName: styleName,
		Alignment:         "left",
		TextIsUpper:       isUpperText(text),
		IndentLeftMM:      effPPr.num("indent_left_mm"),
		IndentRightMM:     effPPr.num("indent_right_mm"),
		IndentFirstLineMM: effPPr.num("indent_first_line_mm"),
		IndentHangingMM:   effPPr.num("indent_hanging_mm"),
		SpaceBeforePt:     effPPr.num("space_before_pt"),
		SpaceAfterPt:      effPPr.num("space_after_pt"),
		LineSpacingPt:     effPPr.num("line_spacing_pt"),
		LineSpacingMult:   effPPr.num("line_spacing_multiple"),
		Section:           paraSection,
		Runs:              runs,
	}
	if a := effPPr.str("alignment"); a != nil && *a != "" {
		para.Alignment = *a
	}
	if f := effPPr.flag("page_break_before"); f != nil {
		para.PageBreakBefore = *f
	}
	if primary != nil {
		para.FontName, para.SizePt = primary.FontName, primary.SizePt
		para.Bold, para.Italic = primary.Bold, primary.Italic
		para.Underline, para.Caps = primary.Underline, primary.Caps
	} else {
		para.FontName = effectiveFont(paraRPr, b.theme)
		para.SizePt = paraRPr.num("size_pt")
	}
	para.TableIndex, para.Row, para.Col = table, row, col
	if cell != nil {
		para.InTable = true
		para.TableLayout = "columns"
		if n := len(b.tableLayout); n > 0 {
			para.TableLayout = b.tableLayout[n-1]
		}
		para.CellLeftMM, para.CellRightMM = ptr(cell.left), ptr(cell.right)
	}
	var tabs []tabStop
	if v, ok := effPPr["tab_stops"].([]tabStop); ok {
		tabs = v
	}
	b.deriveZone(para, tabs)
	b.paras = append(b.paras, para)
	return para
}

// primaryRun is the run carrying the most visible text.
func primaryRun(runs []Run) *Run {
	var best *Run
	bestLen := 0
	for i := range runs {
		if n := utf8.RuneCountInString(strings.TrimSpace(runs[i].Text)); n > bestLen {
			best, bestLen = &runs[i], n
		}
	}
	return best
}

func (b *builder) deriveZone(p *Para, tabs []tabStop) {
	_, contentW := b.contentBox()
	if p.TableIndex != nil && p.CellLeftMM != nil {
		if p.TableLayout != "columns" {
			// data table or one full-width cell: body content, not columns
			p.Zone = ZoneFull
			return
		}
		right := *p.CellLeftMM
		if p.CellRightMM != nil && *p.CellRightMM != 0 {
			right = *p.CellRightMM
		}
		if (*p.CellLeftMM+right)/2.0 < contentW/2.0 {
			p.Zone = ZoneLeft
		} else {
			p.Zone = ZoneRight
		}
		return
	}
	if strings.Contains(p.Text, "\t") {
		rightish := false
		for _, t := range tabs {
			if t.posMM != nil && *t.posMM != 0 && *t.posMM >= contentW*0.4 {
				rightish = true
			}
		}
		left, right, _ := strings.Cut(p.Text, "\t")
		if (rightish || len(tabs) == 0) && strings.TrimSpace(left) != "" && strings.TrimSpace(right) != "" {
			p.Zone = ZoneSplit
			p.LeftText = strings.TrimSpace(left)
			if i := strings.LastIndex(right, "\t"); i >= 0 {
				p.RightText = strings.TrimSpace(right[i+1:])
			} else {
				p.RightText = strings.TrimSpace(right)
			}
			p.LeftProps, p.RightProps = sideProps(p.Runs)
			return
		}
		if strings.TrimSpace(left) == "" && strings.TrimSpace(right) != "" {
			// leading tabs push the text right ("\t\t\tCHỦ TỊCH"): estimate
			// where it starts from the tab stops used
			lead := len(p.Text) - len(strings.TrimLeft(p.Text, "\t "))
			nTabs := strings.Count(p.Text[:lead], "\t")
			var stops []float64
			for _, t := range tabs {
				if t.posMM != nil && *t.posMM != 0 && t.val != "clear" {
					stops = append(stops, *t.posMM)
				}
			}
			sort.Float64s(stops)
			var x float64
			if len(stops) >= nTabs {
				idx := nTabs - 1
				if idx < 0 { // Python stops[-1]
					idx = len(stops) - 1
				}
				if idx >= 0 {
					x = stops[idx]
				}
			} else {
				x = float64(nTabs) * 12.7
			}
			if p.IndentLeftMM != nil {
				x += *p.IndentLeftMM
			}
			if x >= contentW*0.4 {
				p.Zone = ZoneRight
				return
			}
		}
	}
	// a block pushed into the right half by a large left indent (khối ký
	// placed with an indent instead of a table)
	ind := 0.0
	if p.IndentLeftMM != nil {
		ind = *p.IndentLeftMM
	}
	if ind >= contentW*0.4 && p.Alignment != "right" {
		p.Zone = ZoneRight
		return
	}
	p.Zone = ZoneFull
}

// sideProps is the formatting of the dominant run left / right of the first
// tab of a split paragraph.
func sideProps(runs []Run) (SideFormat, SideFormat) {
	var best [2]*Run
	var bestLen [2]int
	side := 0
	for i := range runs {
		for k, part := range strings.Split(runs[i].Text, "\t") {
			if k > 0 {
				side = 1
			}
			if n := utf8.RuneCountInString(strings.TrimSpace(part)); n > bestLen[side] {
				best[side], bestLen[side] = &runs[i], n
			}
		}
	}
	conv := func(r *Run) SideFormat {
		if r == nil {
			return SideFormat{}
		}
		return SideFormat{Present: true, FontName: r.FontName, SizePt: r.SizePt,
			Bold: r.Bold, Italic: r.Italic, Underline: r.Underline, Caps: r.Caps}
	}
	return conv(best[0]), conv(best[1])
}

// blockChildren yields block-level children, unwrapping content controls
// (w:sdt/w:sdtContent) and w:customXml which templates use heavily.
func blockChildren(parent *node) []*node {
	var out []*node
	for _, c := range parent.children {
		switch {
		case c.is("sdt"):
			if inner := c.child("sdtContent"); inner != nil {
				out = append(out, blockChildren(inner)...)
			}
		case c.is("customXml"):
			out = append(out, blockChildren(c)...)
		default:
			out = append(out, c)
		}
	}
	return out
}

func cellHasText(tc *node) bool {
	for _, t := range tc.descendants("t") {
		if strings.TrimSpace(t.text) != "" {
			return true
		}
	}
	return false
}

func tableHasBorders(tbl *node) bool {
	pr := tbl.child("tblPr")
	if pr == nil {
		return false
	}
	if borders := pr.child("tblBorders"); borders != nil {
		for _, b := range borders.children {
			if v, ok := b.wattr("val"); ok && v != "nil" && v != "none" {
				return true
			}
		}
		return false
	}
	if st := pr.child("tblStyle"); st != nil {
		v, _ := st.wattr("val")
		return strings.Contains(strings.ToLower(v), "grid")
	}
	return false
}

// classifyTable: "columns" = a borderless layout table placing text side by
// side (header / signature + nơi nhận); "data" = a real table whose cells
// are body content.
func classifyTable(tbl *node, rows []*node) string {
	maxText, maxCells := 0, 0
	for _, tr := range rows {
		tcs := tr.childrenNamed("tc")
		if len(tcs) > maxCells {
			maxCells = len(tcs)
		}
		n := 0
		for _, tc := range tcs {
			if cellHasText(tc) {
				n++
			}
		}
		if n > maxText {
			maxText = n
		}
	}
	// one column → full-width box; 3+ filled columns → data grid. A 3-col
	// header (cơ quan | spacer | quốc hiệu) still has 2 filled cells.
	if maxCells < 2 || maxText > 2 {
		return "data"
	}
	if tableHasBorders(tbl) && len(rows) >= 3 {
		return "data"
	}
	return "columns"
}

func (b *builder) addTable(tbl *node) {
	ti := b.nTables
	b.nTables++
	var grid []float64
	if g := tbl.child("tblGrid"); g != nil {
		for _, gc := range g.childrenNamed("gridCol") {
			w := 0.0
			if v := twipsToMM(gc.wattr("w")); v != nil {
				w = *v
			}
			grid = append(grid, w)
		}
	}
	rows := tbl.childrenNamed("tr")
	b.tableLayout = append(b.tableLayout, classifyTable(tbl, rows))
	defer func() { b.tableLayout = b.tableLayout[:len(b.tableLayout)-1] }()
	for ri, tr := range rows {
		x := 0.0
		ci := 0
		for _, tc := range tr.childrenNamed("tc") {
			span := 1
			tcPr := tc.child("tcPr")
			if gs := tcPr.child("gridSpan"); gs != nil {
				if v, ok := gs.wattr("val"); ok && v != "" {
					if n, good := pyInt(v); good {
						span = n
					}
				}
			}
			width := 0.0
			if ci < len(grid) {
				end := ci + span
				if end > len(grid) {
					end = len(grid)
				}
				for _, w := range grid[ci:end] {
					width += w
				}
			}
			if width == 0 {
				// no/empty tblGrid: fall back to the cell's own width
				if tcw := tcPr.child("tcW"); tcw != nil {
					if t, ok := tcw.wattr("type"); !ok || t == "dxa" {
						if v := twipsToMM(tcw.wattr("w")); v != nil {
							width = *v
						}
					}
				}
			}
			cell := &cellExtent{left: x, right: x + width}
			r, c := ri, ci
			for _, child := range blockChildren(tc) {
				switch {
				case child.is("p"):
					b.addParagraph(child, "table_cell", cell, ptr(ti), &r, &c)
					for _, tx := range child.descendants("txbxContent") {
						for _, tp := range tx.childrenNamed("p") {
							b.addParagraph(tp, "textbox", cell, ptr(ti), &r, &c)
						}
					}
				case child.is("tbl"):
					b.addTable(child) // nested table: rare, keep order
				}
			}
			x += width
			ci += span
		}
	}
}

func isUpperText(text string) bool {
	letters, lower := 0, false
	for _, r := range text {
		if unicode.IsLetter(r) {
			letters++
			if unicode.IsLower(r) {
				lower = true
			}
		}
	}
	return letters >= 2 && !lower
}

// ---------------------------------------------------------------------------
// entry point

func readPart(files map[string]*zip.File, name string) ([]byte, error) {
	f, ok := files[name]
	if !ok {
		return nil, fmt.Errorf("%s missing", name)
	}
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
		return nil, fmt.Errorf("%s exceeds %d MiB", name, maxPartBytes>>20)
	}
	return data, nil
}

var headerPartRe = regexp.MustCompile(`^word/header\d+\.xml$`)
var footerPartRe = regexp.MustCompile(`^word/footer\d+\.xml$`)

// InspectDocx parses .docx bytes into a Layout. Problems are reported in
// Layout.Errors; a layout without paragraphs and with errors is unusable.
func InspectDocx(content []byte) *Layout {
	layout := &Layout{}
	zr, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		layout.Errors = append(layout.Errors, "not a .docx/.zip package")
		return layout
	}
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[f.Name] = f
		if headerPartRe.MatchString(f.Name) {
			layout.HasHeaders = true
		}
		if footerPartRe.MatchString(f.Name) {
			layout.HasFooters = true
		}
	}
	b := &builder{theme: themeFonts(files), docRPr: props{}, docPPr: props{}, styles: map[string]*style{}}
	if data, err := readPart(files, "word/styles.xml"); err == nil {
		b.docRPr, b.docPPr, b.styles = parseStyles(data)
	}
	data, err := readPart(files, "word/document.xml")
	if err != nil {
		layout.Errors = append(layout.Errors, err.Error())
		return layout
	}
	root, err := parseXML(data)
	if err != nil {
		layout.Errors = append(layout.Errors, "document.xml parse error: "+err.Error())
		return layout
	}
	body := root.child("body")
	if body == nil {
		layout.Errors = append(layout.Errors, "w:body missing")
		return layout
	}
	for _, sp := range body.descendants("sectPr") {
		b.allSections = append(b.allSections, parseSection(sp, len(b.allSections)))
	}
	for _, child := range blockChildren(body) {
		switch {
		case child.is("p"):
			b.addParagraph(child, "body", nil, nil, nil, nil)
			for _, tx := range child.descendants("txbxContent") {
				for _, tp := range tx.childrenNamed("p") {
					b.addParagraph(tp, "textbox", nil, nil, nil, nil)
				}
			}
		case child.is("tbl"):
			b.addTable(child)
		case child.is("sectPr"):
			b.sections = append(b.sections, parseSection(child, b.sectionIdx))
			b.sectionIdx++
		}
	}
	layout.Paragraphs = b.paras
	layout.Sections = b.sections
	layout.NTables = b.nTables
	return layout
}
