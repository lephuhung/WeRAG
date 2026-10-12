package vbpl

import (
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// AttachedFile is a file vbpl.vn lists in documentRelatedList with
// relatedType APPENDIX ("Thông-tư-148-2026-TT-BTC.Phụ lục II.Nội dung …").
type AttachedFile struct {
	Title    string `json:"title"`
	FileName string `json:"file_name"`
}

var (
	// appendixMentionRe finds "Phụ lục", "Phụ lục II", "phụ lục số 03",
	// "Phụ biểu 1" (file titles also write "Phụ-lục").
	appendixMentionRe = regexp.MustCompile(`(?i:phụ[\s_-]*(?:lục|biểu))(?:[\s_-]+(?:(?i:số)\s+)?([IVXLC]+|\d+[a-zA-Z]?)\b((?:\s*(?:,|và|-|–)\s*(?:[IVXLC]+|\d+[a-zA-Z]?)\b)*))?`)
	appendixLabelRe   = regexp.MustCompile(`[IVXLC]+|\d+[a-zA-Z]?`)
	// externalAppendixRe: the phụ lục belongs to another document ("Phụ lục
	// II ban hành kèm theo Thông tư số 38/2015/TT-BTC"), read after the mention.
	externalAppendixRe = regexp.MustCompile(`^[^.;\n]{0,60}?(?:kèm theo|của|tại)\s+(?:Bộ luật|Luật|Pháp lệnh|Nghị định|Nghị quyết|Thông tư liên tịch|Thông tư|Quyết định)\s+(?:số\s+)?\d`)
	// replaceVerbRe: this document attaches a new phụ lục for another
	// ("Thay thế Phụ lục III … kèm theo Thông tư số 49/2015/TT-BGTVT").
	replaceVerbRe = regexp.MustCompile(`(?i)(?:thay thế|sửa đổi|bổ sung)[^.;\n]{0,40}$`)
	romanValues   = map[byte]int{'I': 1, 'V': 5, 'X': 10, 'L': 50, 'C': 100}
)

// minInlineAppendixRunes: a phụ lục whose page text is shorter is only a
// heading — its content is in the attached file.
const minInlineAppendixRunes = 300

// maxLinkedFiles bounds the files linked from one chunk.
const maxLinkedFiles = 4

// LinkAttachedAppendices returns, per chunk, the attached phụ lục files the
// chunk's text refers to whose content the page itself does not carry. A
// numbered mention ("Phụ lục II") links the file of that number; when no
// file title carries a number, or the mention has none, every file is
// linked (the page then holds no phụ lục of its own).
func LinkAttachedAppendices(chunks []Chunk, files []AttachedFile) [][]vietnamese_legal.AppendixFile {
	out := make([][]vietnamese_legal.AppendixFile, len(chunks))
	if len(files) == 0 {
		return out
	}
	byLabel := map[string][]vietnamese_legal.AppendixFile{}
	var all []vietnamese_legal.AppendixFile
	for _, f := range files {
		if strings.TrimSpace(f.FileName) == "" {
			continue
		}
		title := norm.NFC.String(strings.TrimSpace(f.Title))
		if title == "" {
			// vbpl titles are the file name without extension
			title = norm.NFC.String(strings.TrimSpace(strings.TrimSuffix(f.FileName, path.Ext(f.FileName))))
		}
		af := vietnamese_legal.AppendixFile{Title: title, FileName: f.FileName}
		if m := appendixMentionRe.FindStringSubmatch(title); m != nil && m[1] != "" {
			af.Label = m[1]
			key := appendixKey(m[1])
			byLabel[key] = append(byLabel[key], af)
		}
		all = append(all, af)
	}
	if len(all) == 0 {
		return out
	}

	// Phụ lục whose content is in the page text.
	inlineRunes := map[string]int{}
	anyInline := false
	for _, c := range chunks {
		if c.Appendix == "" {
			continue
		}
		n := utf8.RuneCountInString(c.Content)
		inlineRunes[chunkAppendixKey(c)] += n
	}
	for _, n := range inlineRunes {
		if n >= minInlineAppendixRunes {
			anyInline = true
		}
	}

	for i, c := range chunks {
		own := chunkAppendixKey(c)
		var linked []vietnamese_legal.AppendixFile
		add := func(fs []vietnamese_legal.AppendixFile) {
			for _, f := range fs {
				if len(linked) == maxLinkedFiles {
					return
				}
				dup := false
				for _, l := range linked {
					dup = dup || l.FileName == f.FileName
				}
				if !dup {
					linked = append(linked, f)
				}
			}
		}
		for _, idx := range appendixMentionRe.FindAllStringSubmatchIndex(c.Content, -1) {
			if externalAppendixRe.MatchString(c.Content[idx[1]:]) &&
				!replaceVerbRe.MatchString(c.Content[max(0, idx[0]-120):idx[0]]) {
				continue
			}
			m := submatches(c.Content, idx)
			if m[1] == "" {
				if !anyInline {
					add(all)
				}
				continue
			}
			// "Phụ lục I, II và III" names three.
			for _, label := range append([]string{m[1]}, appendixLabelRe.FindAllString(m[2], -1)...) {
				key := appendixKey(label)
				if inlineRunes[key] >= minInlineAppendixRunes {
					continue
				}
				switch {
				case len(byLabel[key]) > 0:
					add(byLabel[key])
				case len(byLabel) == 0 && !anyInline:
					add(all)
				}
			}
		}
		// A phụ lục whose page text is only its heading links its file.
		if own != "" && own != "?" && inlineRunes[own] < minInlineAppendixRunes {
			add(byLabel[own])
		}
		out[i] = linked
	}
	return out
}

// chunkAppendixKey is the number of the phụ lục a chunk sits in, "" when
// none or unnumbered.
func chunkAppendixKey(c Chunk) string {
	if c.Appendix == "" {
		return ""
	}
	if m := appendixMentionRe.FindStringSubmatch(c.Appendix); m != nil && m[1] != "" {
		return appendixKey(m[1])
	}
	return "?"
}

// appendixKey compares Roman and Arabic numbering: "II", "2", "02" → "2".
func appendixKey(label string) string {
	label = strings.TrimSpace(label)
	if n, ok := roman(label); ok {
		return strconv.Itoa(n)
	}
	digits := strings.TrimLeft(label, "0")
	if digits == "" {
		digits = "0"
	}
	return strings.ToLower(digits)
}

func roman(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	total := 0
	for i := 0; i < len(s); i++ {
		v, ok := romanValues[s[i]]
		if !ok {
			return 0, false
		}
		if i+1 < len(s) && romanValues[s[i+1]] > v {
			total -= v
		} else {
			total += v
		}
	}
	return total, true
}

func submatches(s string, idx []int) []string {
	out := make([]string, len(idx)/2)
	for i := range out {
		if idx[2*i] >= 0 {
			out[i] = s[idx[2*i]:idx[2*i+1]]
		}
	}
	return out
}
