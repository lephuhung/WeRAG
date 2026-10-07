package docxedit

import (
	"encoding/xml"
	"math"
	"sort"
	"strconv"
	"strings"
)

// OOXML child sequences (ECMA-376 Part 1, CT_PPr / CT_RPr / CT_SectPr).
// Word rejects documents whose property children are out of order.
var (
	pPrOrder = []string{"pStyle", "keepNext", "keepLines", "pageBreakBefore", "framePr",
		"widowControl", "numPr", "suppressLineNumbers", "pBdr", "shd", "tabs",
		"suppressAutoHyphens", "kinsoku", "wordWrap", "overflowPunct", "topLinePunct",
		"autoSpaceDE", "autoSpaceDN", "bidi", "adjustRightInd", "snapToGrid", "spacing",
		"ind", "contextualSpacing", "mirrorIndents", "suppressOverlap", "jc",
		"textDirection", "textAlignment", "textboxTightWrap", "outlineLvl", "divId",
		"cnfStyle", "rPr", "sectPr", "pPrChange"}
	rPrOrder = []string{"rStyle", "rFonts", "b", "bCs", "i", "iCs", "caps", "smallCaps",
		"strike", "dstrike", "outline", "shadow", "emboss", "imprint", "noProof",
		"snapToGrid", "vanish", "webHidden", "color", "spacing", "w", "kern", "position",
		"sz", "szCs", "highlight", "u", "effect", "bdr", "shd", "fitText", "vertAlign",
		"rtl", "cs", "em", "lang", "eastAsianLayout", "specVanish", "oMath", "rPrChange"}
	sectPrOrder = []string{"headerReference", "footerReference", "footnotePr", "endnotePr",
		"type", "pgSz", "pgMar", "paperSrc", "pgBorders", "lnNumType", "pgNumType", "cols",
		"formProt", "vAlign", "noEndnote", "titlePg", "textDirection", "bidi", "rtlGutter",
		"docGrid", "printerSettings", "sectPrChange"}
)

// kid is one child element of a property container, as raw markup.
type kid struct {
	local string     // local name when in the w: namespace, else ""
	attrs []xml.Attr // raw attributes (for leaf edits)
	xml   string
}

func (d *Document) kids(e *elem) []kid {
	var out []kid
	if e == nil {
		return out
	}
	for _, c := range e.children {
		k := kid{attrs: c.attrs, xml: string(d.xml[c.start:c.end])}
		if c.prefix == d.w.wp {
			k.local = c.local
		}
		out = append(out, k)
	}
	return out
}

func findKid(ks []kid, local string) int {
	for i, k := range ks {
		if k.local == local {
			return i
		}
	}
	return -1
}

func removeKid(ks []kid, local string) []kid {
	out := ks[:0:0]
	for _, k := range ks {
		if k.local != local {
			out = append(out, k)
		}
	}
	return out
}

func withoutKids(ks []kid, locals ...string) []kid {
	for _, l := range locals {
		ks = removeKid(ks, l)
	}
	return ks
}

func joinKids(ks []kid) string {
	var sb strings.Builder
	for _, k := range ks {
		sb.WriteString(k.xml)
	}
	return sb.String()
}

// putKid replaces the child named local or appends it; sortKids fixes order.
func putKid(ks []kid, k kid) []kid {
	if i := findKid(ks, k.local); i >= 0 {
		ks[i] = k
		return ks
	}
	return append(ks, k)
}

// sortKids orders children by the schema sequence. Unknown children keep
// their position right after the preceding known sibling.
func sortKids(ks []kid, order []string) []kid {
	rank := map[string]int{}
	for i, n := range order {
		rank[n] = i
	}
	type ranked struct {
		k kid
		r int
	}
	rs := make([]ranked, len(ks))
	prev := -1
	for i, k := range ks {
		r, ok := rank[k.local]
		if !ok || k.local == "" {
			r = prev
		}
		rs[i] = ranked{k, r}
		prev = r
	}
	sort.SliceStable(rs, func(a, b int) bool { return rs[a].r < rs[b].r })
	out := make([]kid, len(rs))
	for i := range rs {
		out[i] = rs[i].k
	}
	return out
}

// ---------------------------------------------------------------------------
// leaf element editing

func (d *Document) attrSet(attrs []xml.Attr, local, val string) []xml.Attr {
	for i, a := range attrs {
		if a.Name.Space == d.w.wp && a.Name.Local == local {
			attrs[i].Value = val
			return attrs
		}
	}
	return append(attrs, xml.Attr{Name: xml.Name{Space: d.w.wp, Local: local}, Value: val})
}

func (d *Document) attrDel(attrs []xml.Attr, locals ...string) []xml.Attr {
	out := attrs[:0:0]
outer:
	for _, a := range attrs {
		for _, l := range locals {
			if a.Name.Space == d.w.wp && a.Name.Local == l {
				continue outer
			}
		}
		out = append(out, a)
	}
	return out
}

func (d *Document) attrGet(attrs []xml.Attr, local string) (string, bool) {
	for _, a := range attrs {
		if a.Name.Space == d.w.wp && a.Name.Local == local {
			return a.Value, true
		}
	}
	return "", false
}

func renderAttrs(attrs []xml.Attr) string {
	var sb strings.Builder
	for _, a := range attrs {
		sb.WriteByte(' ')
		if a.Name.Space != "" {
			sb.WriteString(a.Name.Space)
			sb.WriteByte(':')
		}
		sb.WriteString(a.Name.Local)
		sb.WriteString(`="`)
		sb.WriteString(escAttr(a.Value))
		sb.WriteByte('"')
	}
	return sb.String()
}

func (d *Document) leaf(local string, attrs []xml.Attr) kid {
	return kid{local: local, attrs: attrs, xml: "<" + d.q(local) + renderAttrs(attrs) + "/>"}
}

// editLeaf rewrites (or creates) the empty child element local with fn
// applied to a copy of its attributes.
func (d *Document) editLeaf(ks []kid, local string, fn func([]xml.Attr) []xml.Attr) []kid {
	var attrs []xml.Attr
	if i := findKid(ks, local); i >= 0 {
		attrs = append([]xml.Attr(nil), ks[i].attrs...)
	}
	return putKid(ks, d.leaf(local, fn(attrs)))
}

func (d *Document) toggle(ks []kid, local string, on bool) []kid {
	var attrs []xml.Attr
	if !on {
		attrs = []xml.Attr{{Name: xml.Name{Space: d.w.wp, Local: "val"}, Value: "0"}}
	}
	return putKid(ks, d.leaf(local, attrs))
}

func twips(pt float64) string   { return strconv.Itoa(int(math.Round(pt * 20))) }
func halfPts(pt float64) string { return strconv.Itoa(int(math.Round(pt * 2))) }

func escText(s string) string {
	var sb strings.Builder
	for _, r := range s {
		switch r {
		case '&':
			sb.WriteString("&amp;")
		case '<':
			sb.WriteString("&lt;")
		case '>':
			sb.WriteString("&gt;")
		default:
			if validXMLChar(r) {
				sb.WriteRune(r)
			}
		}
	}
	return sb.String()
}

func escAttr(s string) string {
	var sb strings.Builder
	for _, r := range s {
		switch r {
		case '&':
			sb.WriteString("&amp;")
		case '<':
			sb.WriteString("&lt;")
		case '>':
			sb.WriteString("&gt;")
		case '"':
			sb.WriteString("&quot;")
		case '\t':
			sb.WriteString("&#x9;")
		case '\n':
			sb.WriteString("&#xA;")
		case '\r':
			sb.WriteString("&#xD;")
		default:
			if validXMLChar(r) {
				sb.WriteRune(r)
			}
		}
	}
	return sb.String()
}

func validXMLChar(r rune) bool {
	return r == 0x9 || r == 0xA || r == 0xD ||
		(r >= 0x20 && r <= 0xD7FF) || (r >= 0xE000 && r <= 0xFFFD) ||
		(r >= 0x10000 && r <= 0x10FFFF)
}
