package vbpl

import (
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// vbpl.vn pages link the documents they cite: <a href="/van-ban/chi-tiet/
// 136042">43/2019/QH14</a> (older pages use an absolute URL on another
// host). The link names the cited document exactly, by its vbpl id = the
// catalogue id, so it is kept on the chunk as metadata.legal.citations —
// a gold label for citations and for the amendment graph.

// Link is one citation link of the page, in page order.
type Link struct {
	VBPLID string `json:"vbpl_id"`
	Text   string `json:"text"`
	// before is the cleaned text right before the link in its paragraph,
	// used to find the link's own occurrence in the Markdown (the same
	// number may also be written without a link).
	before string
}

var vbplLinkRe = regexp.MustCompile(`/van-ban/chi-tiet/([0-9A-Za-z][0-9A-Za-z-]*)`)

// linkContextRunes is how much of the preceding text locates a link.
const linkContextRunes = 30

// collectLinks lists the citation links under root, skipping empty ones.
func collectLinks(root *html.Node) []Link {
	var out []Link
	var visit func(n *html.Node, container *html.Node)
	visit = func(n *html.Node, container *html.Node) {
		if n.Type == html.ElementNode {
			switch n.DataAtom {
			case atom.Head, atom.Style, atom.Script, atom.Noscript, atom.Template:
				return
			case atom.P, atom.Li, atom.Td, atom.Th, atom.Pre, atom.Div,
				atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
				container = n
			case atom.A:
				if m := vbplLinkRe.FindStringSubmatch(attr(n, "href")); m != nil {
					var b strings.Builder
					writeText(&b, n)
					if t := cleanText(b.String()); t != "" {
						out = append(out, Link{VBPLID: m[1], Text: t, before: textBefore(container, n)})
					}
				}
				return
			}
		}
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			visit(ch, container)
		}
	}
	visit(root, nil)
	return out
}

// textBefore is the cleaned text of container up to node a, its last
// linkContextRunes runes, with a trailing space kept.
func textBefore(container, a *html.Node) string {
	if container == nil {
		return ""
	}
	var b strings.Builder
	var visit func(n *html.Node) bool // false once a is reached
	visit = func(n *html.Node) bool {
		if n == a {
			return false
		}
		if !isAncestor(n, a) {
			writeText(&b, n)
			return true
		}
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			if !visit(ch) {
				return false
			}
		}
		return true
	}
	for ch := container.FirstChild; ch != nil; ch = ch.NextSibling {
		if !visit(ch) {
			break
		}
	}
	raw := b.String()
	t := cleanText(raw)
	if t == "" {
		return ""
	}
	if r, _ := utf8.DecodeLastRuneInString(raw); r == ' ' || r == '\n' || r == '\t' || r == ' ' {
		t += " "
	}
	if r := []rune(t); len(r) > linkContextRunes {
		t = string(r[len(r)-linkContextRunes:])
	}
	return t
}

func isAncestor(n, x *html.Node) bool {
	for p := x; p != nil; p = p.Parent {
		if p == n {
			return true
		}
	}
	return false
}

// CitationLinks places the document's citation links in its chunks:
// for each chunk, the links whose text lies inside it, with rune offsets
// in the chunk content, in text order. A link is found by its text with
// the words before it, first after the previous link so that a number
// cited twice keeps its order, else anywhere (pages put some links out of
// reading order); failing that by its text alone after the previous link.
// A link whose text is not found is dropped.
func CitationLinks(d *Document, chunks []Chunk) [][]vietnamese_legal.CitationLink {
	out := make([][]vietnamese_legal.CitationLink, len(chunks))
	if d == nil || len(d.Links) == 0 {
		return out
	}
	md := d.Markdown
	cursor := 0 // byte offset in md after the last link found with its context
	used := map[int]bool{}
	find := func(from int, needle string) int {
		for from <= len(md) {
			i := strings.Index(md[from:], needle)
			if i < 0 {
				return -1
			}
			if !used[from+i] {
				return from + i
			}
			from += i + 1
		}
		return -1
	}
	for _, l := range d.Links {
		at := -1
		if l.before != "" {
			if i := find(cursor, l.before+l.Text); i >= 0 {
				at = i + len(l.before)
				cursor = at + len(l.Text)
			} else if i := find(0, l.before+l.Text); i >= 0 {
				at = i + len(l.before)
			}
		}
		if at < 0 {
			at = find(cursor, l.Text)
		}
		if at < 0 || used[at] {
			continue
		}
		used[at] = true
		s := utf8.RuneCountInString(md[:at])
		e := s + utf8.RuneCountInString(l.Text)
		for ci, c := range chunks {
			if s >= c.Start && e <= c.End {
				out[ci] = append(out[ci], vietnamese_legal.CitationLink{
					Start: s - c.Start, End: e - c.Start, VBPLID: l.VBPLID, Text: l.Text,
				})
				break
			}
		}
	}
	for _, cs := range out {
		sort.Slice(cs, func(i, j int) bool { return cs[i].Start < cs[j].Start })
	}
	return out
}
