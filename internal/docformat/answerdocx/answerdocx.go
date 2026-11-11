// Package answerdocx writes one chat answer as a .docx: a short header (the
// question, when it was answered, "Trả lời bởi WeRAG"), the answer's
// Markdown as Word paragraphs, lists and tables (see MarkdownBlocks), and a
// "Tài liệu tham khảo" list of the answer's sources. Nothing is generated:
// the document is the stored message as the chat shows it.
package answerdocx

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/Tencent/WeKnora/internal/docformat/docxwrite"
)

// Reference is one source of the answer.
type Reference struct {
	// Title is the document's title (or file name).
	Title string
	// DocumentNumber is the số hiệu of a legal document ("" when unknown).
	DocumentNumber string
	// Source names where the document lives: the knowledge base.
	Source string
	// FileName is shown when it differs from Title.
	FileName string
	// URL of a web source.
	URL string
}

// Input is what Build writes.
type Input struct {
	Question string
	// AnsweredAt is the answer's time, already formatted
	// ("14h05 - 16/10/2026").
	AnsweredAt string
	// Content is the answer's Markdown.
	Content    string
	References []Reference
}

// ReferencesHeading opens the sources section.
const ReferencesHeading = "Tài liệu tham khảo"

// Byline closes the header line.
const Byline = "Trả lời bởi WeRAG"

// metaGrey is the colour of the header labels.
const metaGrey = "595959"

// Build writes the answer document.
func Build(in Input) ([]byte, error) {
	var blocks []docxwrite.Block
	if q := strings.TrimSpace(in.Question); q != "" {
		blocks = append(blocks, docxwrite.Paragraph{Style: docxwrite.StyleMeta, Runs: []docxwrite.Run{
			{Text: "Câu hỏi: ", Bold: true, Color: metaGrey},
			{Text: q},
		}})
	}
	meta := Byline
	if in.AnsweredAt != "" {
		meta = in.AnsweredAt + " · " + Byline
	}
	blocks = append(blocks,
		docxwrite.Paragraph{Style: docxwrite.StyleMeta, Runs: []docxwrite.Run{{Text: meta, Italic: true}}},
		docxwrite.Paragraph{Style: docxwrite.StyleRule},
	)
	blocks = append(blocks, MarkdownBlocks(in.Content)...)
	if refs := referenceParagraphs(in.References); len(refs) > 0 {
		blocks = append(blocks, docxwrite.Paragraph{Style: docxwrite.StyleHeading3, Align: "left",
			Runs: []docxwrite.Run{{Text: ReferencesHeading}}})
		blocks = append(blocks, refs...)
	}
	return docxwrite.Build(docxwrite.Document{Title: titleOf(in), Blocks: blocks})
}

// referencesListKey is the numbering key of the sources list; the answer's
// own lists count up from 1.
const referencesListKey = -1

func referenceParagraphs(refs []Reference) []docxwrite.Block {
	var out []docxwrite.Block
	for _, r := range refs {
		title := strings.TrimSpace(r.Title)
		if title == "" {
			title = strings.TrimSpace(r.FileName)
		}
		if title == "" {
			title = strings.TrimSpace(r.URL)
		}
		if title == "" {
			continue
		}
		runs := []docxwrite.Run{{Text: title, Bold: true}}
		if n := strings.TrimSpace(r.DocumentNumber); n != "" && !strings.Contains(title, n) {
			runs = append(runs, docxwrite.Run{Text: " — Số hiệu: " + n})
		}
		if f := strings.TrimSpace(r.FileName); f != "" && f != title {
			runs = append(runs, docxwrite.Run{Text: " — Tệp: " + f})
		}
		if s := strings.TrimSpace(r.Source); s != "" {
			runs = append(runs, docxwrite.Run{Text: " — " + s, Italic: true})
		}
		if u := strings.TrimSpace(r.URL); u != "" && u != title {
			runs = append(runs, docxwrite.Run{Text: " — " + u})
		}
		out = append(out, docxwrite.Paragraph{Align: "left", Runs: runs,
			List: &docxwrite.ListItem{Ordered: true, List: referencesListKey}})
	}
	return out
}

// titleOf is the package title: the question, cut to a readable length.
func titleOf(in Input) string {
	q := strings.Join(strings.Fields(in.Question), " ")
	if q == "" {
		return Byline
	}
	if utf8.RuneCountInString(q) > 120 {
		q = string([]rune(q)[:120]) + "…"
	}
	return q
}

// fileNameMaxRunes bounds the question part of the file name.
const fileNameMaxRunes = 60

// FileName is the download name: "Trả lời - <question>.docx", the question
// cut at a word near 60 characters, without the characters Windows refuses
// in file names.
func FileName(question string) string {
	q := strings.Map(func(r rune) rune {
		switch {
		case r < 0x20 || r == 0x7f:
			return ' '
		case strings.ContainsRune(`\/:*?"<>|`, r):
			return ' '
		}
		return r
	}, question)
	q = strings.Join(strings.Fields(q), " ")
	if utf8.RuneCountInString(q) > fileNameMaxRunes {
		cut := []rune(q)[:fileNameMaxRunes]
		for i := len(cut) - 1; i > fileNameMaxRunes/2; i-- {
			if cut[i] == ' ' {
				cut = cut[:i]
				break
			}
		}
		q = string(cut)
	}
	q = strings.TrimRight(q, " .")
	if q == "" {
		return "Trả lời WeRAG.docx"
	}
	return "Trả lời - " + q + ".docx"
}

// ASCIIFileName is name without Vietnamese diacritics (đ → d) and with
// anything else outside printable ASCII dropped: the filename= fallback
// of Content-Disposition.
func ASCIIFileName(name string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(name) {
		switch {
		case r == 'đ':
			b.WriteByte('d')
		case r == 'Đ':
			b.WriteByte('D')
		case unicode.Is(unicode.Mn, r):
		case r == '"' || r == '\\':
			b.WriteByte('_')
		case r >= 0x20 && r < 0x7f:
			b.WriteRune(r)
		}
	}
	out := strings.Join(strings.Fields(b.String()), " ")
	if out == "" || out == ".docx" {
		return "WeRAG.docx"
	}
	return out
}
