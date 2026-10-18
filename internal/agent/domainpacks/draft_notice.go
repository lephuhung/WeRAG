package domainpacks

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// DraftNoticeMarker opens the code-appended draft notice (doc §4 layer d,
// "nhãn dự thảo"). Like the footer it is cut when an answer is replayed as
// history (StripFooter).
const DraftNoticeMarker = "\n\n---\n**Lưu ý về dự thảo**\n"

// draftLabels mark a paragraph as already saying the text is a draft.
var draftLabels = []string{"dự thảo", "dự án luật"}

// genericDraftAliases are refused as aliases: they name no draft in
// particular, so every answer mentioning them would get a notice.
var genericDraftAliases = map[string]bool{
	"luật mới": true, "dự thảo": true, "dự án luật": true, "dự án": true, "luật": true,
	"nghị định": true, "thông tư": true, "quy định mới": true, "văn bản mới": true,
	"dự thảo luật": true, "luật sửa đổi": true,
}

// minAliasRunes is the shortest alias accepted.
const minAliasRunes = 4

// normText is how aliases and answers are compared: NFC, lower case.
func normText(s string) string {
	return strings.ToLower(norm.NFC.String(s))
}

// Names are the phrases that refer to the draft: its title and aliases.
func (d DraftDocument) Names() []string {
	names := []string{d.Title}
	return append(names, d.Aliases...)
}

// noticeLine is the one line the notice gives a draft.
func (d DraftDocument) noticeLine() string {
	title := strings.TrimSpace(d.Title)
	if d.Date.IsZero() || d.VersionUnknown() {
		return fmt.Sprintf("%s là dự án luật (chưa rõ lần dự thảo), chưa được thông qua; "+
			"không áp dụng như quy định hiện hành.", title)
	}
	return fmt.Sprintf("%s là dự thảo (%s, ngày %s), chưa có hiệu lực; không áp dụng như quy định hiện hành.",
		title, strings.TrimSpace(d.Version), d.Date.VN())
}

// DraftNotice is the block appended after a model-written answer that
// mentions one of the packs' drafts in a paragraph not saying "dự thảo" /
// "dự án luật": one line per such draft, the model's text untouched. ""
// when every mention is labelled. modelText is the model's own text;
// code-appended blocks in it are ignored.
func (s *Source) DraftNotice(modelText string) string {
	paragraphs := splitParagraphs(StripFooter(modelText))
	seen := map[string]bool{}
	var lines []string
	for _, pack := range s.Packs() {
		for _, d := range pack.Manifest.Metadata.DraftDocuments {
			key := normText(strings.TrimSpace(d.Title))
			if seen[key] {
				continue
			}
			if draftUnlabelled(d, paragraphs) {
				seen[key] = true
				lines = append(lines, "- "+d.noticeLine())
			}
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return DraftNoticeMarker + strings.Join(lines, "\n") + "\n"
}

// draftUnlabelled reports whether some paragraph names d without a label.
func draftUnlabelled(d DraftDocument, paragraphs []string) bool {
	for _, p := range paragraphs {
		text := normText(p)
		if hasDraftLabel(text) {
			continue
		}
		for _, name := range d.Names() {
			if containsPhrase(text, normText(strings.TrimSpace(name))) {
				return true
			}
		}
	}
	return false
}

func hasDraftLabel(text string) bool {
	for _, label := range draftLabels {
		if strings.Contains(text, label) {
			return true
		}
	}
	return false
}

// containsPhrase finds phrase in text on word boundaries (the runes around
// it are not letters or digits).
func containsPhrase(text, phrase string) bool {
	if phrase == "" {
		return false
	}
	for start := 0; start < len(text); {
		i := strings.Index(text[start:], phrase)
		if i < 0 {
			return false
		}
		i += start
		end := i + len(phrase)
		before, after := lastRune(text[:i]), firstRune(text[end:])
		if !isWordRune(before) && !isWordRune(after) {
			return true
		}
		start = i + 1
	}
	return false
}

func isWordRune(r rune) bool { return r != 0 && (unicode.IsLetter(r) || unicode.IsDigit(r)) }

func lastRune(s string) rune {
	if s == "" {
		return 0
	}
	r, _ := utf8.DecodeLastRuneInString(s)
	return r
}

func firstRune(s string) rune {
	if s == "" {
		return 0
	}
	r, _ := utf8.DecodeRuneInString(s)
	return r
}

var (
	headingLine  = regexp.MustCompile(`^\s{0,3}#{1,6}\s`)
	listItemLine = regexp.MustCompile(`^\s*(?:[-*+]|\d+[.)])\s`)
	tableRowLine = regexp.MustCompile(`^\s*\|`)
)

// splitParagraphs splits Markdown into paragraphs: a blank line, a list
// item, a table row or a heading starts a new one. A heading belongs to
// the paragraph that follows it ("### 2. Doanh nghiệp một người là gì?"
// then a labelled paragraph is one labelled unit).
func splitParagraphs(text string) []string {
	var out []string
	var cur []string
	headingOnly := false
	flush := func() {
		if len(cur) > 0 {
			out = append(out, strings.Join(cur, "\n"))
		}
		cur, headingOnly = nil, false
	}
	for _, line := range strings.Split(text, "\n") {
		blank := strings.TrimSpace(line) == ""
		switch {
		case blank:
			if !headingOnly {
				flush()
			}
		case headingLine.MatchString(line):
			if !headingOnly {
				flush()
			}
			cur = append(cur, line)
			headingOnly = true
		case listItemLine.MatchString(line) || tableRowLine.MatchString(line):
			if !headingOnly {
				flush()
			}
			cur = append(cur, line)
			headingOnly = false
		default:
			cur = append(cur, line)
			headingOnly = false
		}
	}
	flush()
	return out
}
