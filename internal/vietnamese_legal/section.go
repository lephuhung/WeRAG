// section.go parses a section reference as a user or an agent writes it
// ("điểm a khoản 2 Điều 17", "Chương II") and matches it against the
// per-chunk LegalMetadata the chunker tier records, so a lookup resolves
// "Điều 17" exactly instead of regex-matching body text.
package vietnamese_legal

import (
	"errors"
	"regexp"
	"strings"
)

// SectionRef is a parsed section reference. Labels are lower-cased to match
// the LegalMetadata conventions (article_nos "17"/"5a", diem_labels "a").
type SectionRef struct {
	Article string // Điều number, "" when none
	Khoan   string // Khoản number, "" when none
	Diem    string // Điểm letter, "" when none
	// Heading is a lower-cased "chương ii" / "mục 1" / "phần i" lead that is
	// matched against heading_path components, "" when none.
	Heading string
	// Multi is set when the reference names more than one Điều, Khoản or
	// Điểm ("khoản 2 và 3"); such a reference is never silently reduced to
	// one target.
	Multi bool
}

// Section-part patterns. RE2 \b is ASCII-only and never fires next to "đ",
// so the keyword is anchored on a preceding non-letter (or the start) and
// the label on a following non-alphanumeric (or the end) instead.
var (
	sectionDieuRe  = regexp.MustCompile(`(?i)(?:^|[^\p{L}])điều\s+(\d+[a-zđ]?)(?:[^\p{L}\p{N}]|$)`)
	sectionKhoanRe = regexp.MustCompile(`(?i)(?:^|[^\p{L}])khoản\s+(\d{1,2})(?:[^\p{L}\p{N}]|$)`)
	sectionDiemRe  = regexp.MustCompile(`(?i)(?:^|[^\p{L}])điểm\s+([a-zđ])(?:[^\p{L}\p{N}]|$)`)
	sectionHeadRe  = regexp.MustCompile(`(?i)(?:^|[^\p{L}])(chương|mục|phần)\s+([ivx]+|\d+)(?:[^\p{L}\p{N}]|$)`)
	// Counting variants without the surrounding anchors: the anchored
	// patterns consume the separator after a label, which would hide a
	// second reference that immediately follows ("Mục 2 Chương III").
	sectionDieuCountRe  = regexp.MustCompile(`(?i)điều\s+\d+`)
	sectionKhoanCountRe = regexp.MustCompile(`(?i)khoản\s+\d+`)
	sectionDiemCountRe  = regexp.MustCompile(`(?i)điểm\s+[a-zđ](?:[^\p{L}]|$)`)
	sectionHeadCountRe  = regexp.MustCompile(`(?i)(chương|mục|phần)\s+([ivx]+|\d+)`)
)

// ErrNoSectionRef is returned when the text names no Điều/Khoản/Điểm/Chương/Mục/Phần.
var ErrNoSectionRef = errors.New("no section reference found")

// ParseSectionRef parses a free-form section reference, independent of word
// order and punctuation ("Điều 17 khoản 2", "khoản 2, Điều 17", "điểm a
// khoản 2 Điều 17").
func ParseSectionRef(text string) (SectionRef, error) {
	var ref SectionRef
	text = strings.TrimSpace(text)
	if text == "" {
		return ref, ErrNoSectionRef
	}
	if m := sectionDieuRe.FindStringSubmatch(text); m != nil {
		ref.Article = strings.ToLower(m[1])
	}
	if m := sectionKhoanRe.FindStringSubmatch(text); m != nil {
		ref.Khoan = strings.TrimLeft(m[1], "0")
	}
	if m := sectionDiemRe.FindStringSubmatch(text); m != nil {
		ref.Diem = strings.ToLower(m[1])
	}
	if m := sectionHeadRe.FindStringSubmatch(text); m != nil {
		ref.Heading = strings.ToLower(m[1]) + " " + strings.ToLower(m[2])
	}
	for _, re := range []*regexp.Regexp{sectionDieuCountRe, sectionKhoanCountRe, sectionDiemCountRe, sectionHeadCountRe} {
		if len(re.FindAllStringIndex(text, -1)) > 1 {
			ref.Multi = true
		}
	}
	if ref.Article == "" && ref.Khoan == "" && ref.Diem == "" && ref.Heading == "" {
		return ref, ErrNoSectionRef
	}
	return ref, nil
}

// IsEmpty reports whether the reference names nothing.
func (r SectionRef) IsEmpty() bool {
	return r.Article == "" && r.Khoan == "" && r.Diem == "" && r.Heading == ""
}

// String renders the reference in canonical Vietnamese order
// ("Điểm a Khoản 2 Điều 17", "Chương II").
func (r SectionRef) String() string {
	var parts []string
	if r.Diem != "" {
		parts = append(parts, "Điểm "+r.Diem)
	}
	if r.Khoan != "" {
		parts = append(parts, "Khoản "+r.Khoan)
	}
	if r.Article != "" {
		parts = append(parts, "Điều "+r.Article)
	}
	if r.Heading != "" {
		kw, label, _ := strings.Cut(r.Heading, " ")
		if kw == "chương" || kw == "phần" {
			label = strings.ToUpper(label)
		}
		parts = append(parts, capitalizeWord(kw)+" "+label)
	}
	return strings.Join(parts, " ")
}

// Matches reports whether a chunk carrying meta holds the referenced
// section. Every named part must hold:
//
//   - Điều: article_nos lists the number (a merged chunk lists every Điều).
//   - Khoản + Điểm: subdivision_refs holds the atomic "khoan:K/diem:D"
//     token — the parent/child relation is never inferred from the two
//     independent lists.
//   - Khoản alone: khoan_nos; Điểm alone: diem_labels.
//   - Chương/Mục/Phần: a heading_path component opens with the lead.
func (r SectionRef) Matches(meta *LegalMetadata) bool {
	if meta == nil || r.IsEmpty() {
		return false
	}
	if r.Article != "" && !containsFold(meta.ArticleNos, r.Article) {
		return false
	}
	switch {
	case r.Khoan != "" && r.Diem != "":
		if !containsFold(meta.SubdivisionRefs, "khoan:"+r.Khoan+"/diem:"+r.Diem) {
			return false
		}
	case r.Khoan != "":
		if !containsFold(meta.KhoanNos, r.Khoan) {
			return false
		}
	case r.Diem != "":
		if !containsFold(meta.DiemLabels, r.Diem) {
			return false
		}
	}
	if r.Heading != "" && !headingPathHas(meta.HeadingPath, r.Heading) {
		return false
	}
	return true
}

// ContentPattern is the fallback for documents indexed before the legal
// chunker recorded metadata: a line-anchored match on the Điều heading or,
// without a Điều, on the Chương/Mục/Phần heading. Khoản and Điểm cannot be
// resolved from body text and are ignored; nil when nothing can be matched.
func (r SectionRef) ContentPattern() *regexp.Regexp {
	switch {
	case r.Article != "":
		return regexp.MustCompile(`(?im)^\s*(?:#{1,6}\s*)?Điều\s+` + regexp.QuoteMeta(r.Article) + `(?:[^\p{L}\p{N}]|$)`)
	case r.Heading != "":
		kw, label, _ := strings.Cut(r.Heading, " ")
		return regexp.MustCompile(`(?im)^\s*(?:#{1,6}\s*)?` + regexp.QuoteMeta(kw) + `\s+` +
			regexp.QuoteMeta(label) + `(?:[^\p{L}\p{N}]|$)`)
	}
	return nil
}

func containsFold(xs []string, v string) bool {
	for _, x := range xs {
		if strings.EqualFold(strings.TrimSpace(x), v) {
			return true
		}
	}
	return false
}

// headingPathHas reports whether a heading_path component opens with lead
// ("chương ii") followed by a non-alphanumeric or the end, so "chương i"
// does not match "Chương II".
func headingPathHas(path []string, lead string) bool {
	for _, comp := range path {
		c := strings.ToLower(strings.TrimSpace(comp))
		if !strings.HasPrefix(c, lead) {
			continue
		}
		rest := c[len(lead):]
		if rest == "" {
			return true
		}
		if r := []rune(rest)[0]; !isLetterOrDigit(r) {
			return true
		}
	}
	return false
}

func isLetterOrDigit(r rune) bool {
	return (r >= '0' && r <= '9') || strings.ContainsRune("abcdefghijklmnopqrstuvwxyz", r) ||
		(r > 127 && strings.ToLower(string(r)) != strings.ToUpper(string(r)))
}
