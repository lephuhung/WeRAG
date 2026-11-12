package answerdocx

import (
	"html"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Citation is one inline source tag of a stored answer, in the public form
// the agent pipeline writes (internal/modelcontext ExpandText):
// <kb doc="…" chunk_id="…" kb_id="…" /> or <web url="…" title="…" />.
type Citation struct {
	ChunkID string
	KBID    string
	Doc     string
	URL     string
	Title   string
}

var citationAttrRe = regexp.MustCompile(`([A-Za-z_]+)\s*=\s*"([^"]*)"`)

// Citations lists the <kb/> and <web/> tags of md outside code in order
// of first use, each chunk or URL once. Read before the tags are stripped.
func Citations(md string) []Citation {
	var tags []string
	replaceOutsideCode(md, citationTagRe, func(seg string, _, _ bool) string {
		tags = append(tags, citationTagRe.FindAllString(seg, -1)...)
		return seg
	})
	var out []Citation
	seen := map[string]bool{}
	for _, tag := range tags {
		isKB := strings.HasPrefix(tag, "<kb")
		if !isKB && !strings.HasPrefix(tag, "<web") {
			continue // </kb>, <ref …/>: no source of their own
		}
		attrs := map[string]string{}
		for _, m := range citationAttrRe.FindAllStringSubmatch(tag, -1) {
			attrs[strings.ToLower(m[1])] = strings.TrimSpace(html.UnescapeString(m[2]))
		}
		c := Citation{ChunkID: attrs["chunk_id"], KBID: attrs["kb_id"], Doc: attrs["doc"], URL: attrs["url"], Title: attrs["title"]}
		key := "kb:" + c.ChunkID + "\x00" + c.Doc
		if isKB {
			c.URL, c.Title = "", "" // a page link is for <web/> tags only
		} else {
			c.ChunkID, c.KBID, c.Doc = "", "", ""
			key = "web:" + c.URL
		}
		if key == "kb:\x00" || key == "web:" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, c)
	}
	return out
}

// fileExtRe is the extension of a document file name used as its title.
var fileExtRe = regexp.MustCompile(`(?i)\.(?:html?|pdf|docx?|txt|md|xlsx?|pptx?|rtf|odt)$`)

// cutNameRunes is where imported file names are cut (vbpl.vn titles run
// longer): a name exactly this long without its extension was cut.
const cutNameRunes = 180

// DisplayTitle turns a document's file name into a title: no extension,
// and a name cut at cutNameRunes loses its broken last word and ends in
// "…" ("…Quỹ Phát triển doanh nghiệ.html" → "…Quỹ Phát triển doanh…").
func DisplayTitle(name string) string {
	name = strings.TrimSpace(name)
	base := fileExtRe.ReplaceAllString(name, "")
	if base == name || utf8.RuneCountInString(base) != cutNameRunes {
		return strings.TrimSpace(base)
	}
	if strings.HasSuffix(base, " ") {
		base = strings.TrimRight(base, " ") // cut between words
	} else if i := strings.LastIndexByte(base, ' '); i > 0 {
		base = base[:i] // drop the broken word
	}
	return strings.TrimRight(base, " ,;:-–—") + "…"
}
