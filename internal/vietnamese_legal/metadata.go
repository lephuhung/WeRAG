// metadata.go is the Go port of AIRAG's legal document-identity logic:
// the header-region số hiệu recovery (document_type_classifier.py::
// _recover_doc_number) and the deterministic reference parser used by
// resolve_candidates Stage 0 (agent/doc_resolver.py::_extract_by_regex).
//
// This is the deterministic side of document resolution — it never calls
// an LLM and never decides routing; it only extracts, normalizes and
// produces lookup candidates.
package vietnamese_legal

import (
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// DocTypeKeywords maps a Vietnamese document-type keyword to its canonical
// slug. Ordered longest-match first — "thông tư liên tịch" must be tried
// before "thông tư".
var DocTypeKeywords = []struct {
	Keyword string
	Slug    string
}{
	{"thông tư liên tịch", "thong_tu_lien_tich"},
	{"bộ luật", "bo_luat"},
	{"nghị quyết", "nghi_quyet"},
	{"nghị định", "nghi_dinh"},
	{"quyết định", "quyet_dinh"},
	{"pháp lệnh", "phap_lenh"},
	{"thông tư", "thong_tu"},
	{"chỉ thị", "chi_thi"},
	{"luật", "luat"},
}

// IssuingAgency maps an issuing-agency name to its document-number code and
// the số hiệu suffixes that agency uses — ordered longest name first.
var IssuingAgency = []struct {
	Name     string
	Code     string
	Suffixes []string
}{
	{"bộ giáo dục và đào tạo", "BGDĐT", []string{"TT-BGDĐT"}},
	{"bộ nông nghiệp và phát triển nông thôn", "BNNPTNT", []string{"TT-BNNPTNT"}},
	{"bộ thông tin và truyền thông", "BTTTT", []string{"TT-BTTTT"}},
	{"bộ lao động thương binh và xã hội", "BLĐTBXH", []string{"TT-BLĐTBXH"}},
	{"bộ tài nguyên và môi trường", "BTNMT", []string{"TT-BTNMT"}},
	{"bộ kế hoạch và đầu tư", "BKHĐT", []string{"TT-BKHĐT"}},
	{"bộ khoa học và công nghệ", "BKHCN", []string{"TT-BKHCN"}},
	{"bộ văn hóa thể thao và du lịch", "BVHTTDL", []string{"TT-BVHTTDL"}},
	{"bộ văn hóa thể thao", "BVHTTDL", []string{"TT-BVHTTDL"}},
	{"bộ giao thông vận tải", "BGTVT", []string{"TT-BGTVT"}},
	{"ngân hàng nhà nước", "NHNN", []string{"TT-NHNN"}},
	{"bộ công thương", "BCT", []string{"TT-BCT", "VBHN-BCT"}},
	{"bộ công an", "BCA", []string{"TT-BCA"}},
	{"bộ tài chính", "BTC", []string{"TT-BTC"}},
	{"bộ tư pháp", "BTP", []string{"TT-BTP"}},
	{"bộ quốc phòng", "BQP", []string{"TT-BQP"}},
	{"bộ y tế", "BYT", []string{"TT-BYT"}},
	{"bộ nội vụ", "BNV", []string{"TT-BNV"}},
	{"bộ xây dựng", "BXD", []string{"TT-BXD"}},
	{"bộ ngoại giao", "BNG", []string{"TT-BNG"}},
	{"ủy ban thường vụ quốc hội", "UBTVQH15", []string{"UBTVQH15", "NQ-UBTVQH15"}},
	{"thủ tướng chính phủ", "TTg", []string{"QĐ-TTg"}},
	{"thủ tướng", "TTg", []string{"QĐ-TTg"}},
	{"chính phủ", "CP", []string{"NĐ-CP", "NQ-CP"}},
	{"quốc hội", "QH15", []string{"QH15", "QH14"}},
	{"chủ tịch nước", "CTN", []string{"L-CTN"}},
	{"viện kiểm sát nhân dân tối cao", "VKSNDTC", []string{"QĐ-VKSNDTC"}},
	{"tòa án nhân dân tối cao", "TANDTC", []string{"QĐ-TANDTC"}},
	{"hội đồng nhân dân", "HĐND", []string{"NQ-HĐND"}},
	{"ủy ban nhân dân", "UBND", []string{"QĐ-UBND"}},
}

// TypeDefaultSuffix infers the số hiệu suffix from the document type when no
// agency is known.
var TypeDefaultSuffix = map[string][]string{
	"luat":               {"QH15", "QH14"},
	"nghi_dinh":          {"NĐ-CP"},
	"nghi_quyet":         {"NQ-CP", "NQ-HĐND"},
	"thong_tu":           {"TT"},
	"thong_tu_lien_tich": {"TTLT"},
	"quyet_dinh":         {"QĐ-TTg", "QĐ-UBND"},
	"phap_lenh":          {"UBTVQH15"},
}

// legalStopwords are excluded from title-keyword extraction.
var legalStopwords = map[string]bool{
	"của": true, "về": true, "và": true, "các": true, "theo": true,
	"trong": true, "đến": true, "từ": true, "tới": true, "là": true,
	"có": true, "được": true, "này": true, "đó": true, "cho": true,
	"tôi": true, "xem": true, "tìm": true, "tra": true, "cứu": true,
	"hỏi": true, "văn": true, "bản": true, "tài": true, "liệu": true,
	"nội": true, "dung": true, "số": true, "năm": true, "ngày": true,
	"tháng": true, "ban": true, "hành": true, "quy": true, "định": true,
	"do": true, "bởi": true, "với": true, "một": true, "hai": true,
	"hay": true, "hoặc": true, "đây": true,
}

// actionPatterns strip leading action phrases that are not part of the
// document reference ("tóm tắt", "tra cứu", ...).
var actionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^(?:tóm\s*tắt|tra\s*cứu|tìm|xem|liệt\s*kê|tổng\s*hợp|nội\s*dung)\s+`),
	regexp.MustCompile(`(?i)^(?:cho\s+tôi\s+xem|hiển\s+thị|lấy)\s+`),
}

// sectionPatterns capture section references (Điều X, Chương Y, Khoản N…).
var sectionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(?:điều|chương|khoản|mục|phụ\s*lục)\s+[\dIVXivx]+(?:\.\d+)*`),
}

// A genuine số hiệu carries digits: "24/2018/QH14", "53/2022/NĐ-CP", labelled
// by "Số:" / "Luật số:" near the top of the header.
var docNumLabelledRe = regexp.MustCompile(
	`(?i)(?:^|\n)\s*(?:luật\s+số|số)\s*[:：]\s*(\d{1,4}\s*/\s*\d{2,4}\s*/\s*[\p{L}\p{N}_Đ\-]+)`,
)

// docNumBareRe finds a bare số hiệu. Python uses (?<!\d) — Go has no
// lookbehind, so the digit-boundary check is done by hand in
// RecoverDocumentNumber.
var docNumBareRe = regexp.MustCompile(
	`(\d{1,4}\s*/\s*\d{2,4}\s*/\s*[A-Za-zĐ][\p{L}\p{N}_Đ\-]*)`,
)

var preambleCutRe = regexp.MustCompile(
	`(?i)căn\s+cứ|quốc\s+hội\s+ban\s+hành|ban\s+hành\s+luật`,
)

// RecoverDocumentNumber extracts a document's own số hiệu from its header
// markdown — best-effort, no LLM.
//
// CRITICAL: the number must come from the header region BEFORE the
// "Căn cứ …" preamble. Every Vietnamese law's preamble cites OTHER
// documents ("Căn cứ … theo Nghị quyết số 203/2025/QH15;"), so scanning the
// whole text would grab a citation's number instead of this document's own.
// The real số hiệu always sits above the preamble (next to the issuing
// agency), so the search region is cut at the first preamble marker.
func RecoverDocumentNumber(markdownText string) string {
	if markdownText == "" {
		return ""
	}
	region := markdownText
	if len(region) > 1500 {
		region = region[:1500]
	}
	if cut := preambleCutRe.FindStringIndex(region); cut != nil {
		region = region[:cut[0]]
	}
	if m := docNumLabelledRe.FindStringSubmatch(region); m != nil {
		return stripWS(m[1])
	}
	// Bare form only in the very top block (header), to avoid stray matches.
	top := region
	if len(top) > 500 {
		top = top[:500]
	}
	for _, m := range docNumBareRe.FindAllStringIndex(top, -1) {
		// Digit-boundary check replacing Python's (?<!\d): the byte before
		// the match must not be a digit.
		if m[0] > 0 {
			r, _ := utf8.DecodeLastRuneInString(top[:m[0]])
			if r >= '0' && r <= '9' {
				continue
			}
		}
		return stripWS(top[m[0]:m[1]])
	}
	return ""
}

func stripWS(s string) string {
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, s)
}

// vnLocation is the calendar location for "current year" — document numbers
// are issued/queried in the Vietnamese local year, not the host's UTC clock.
var vnLocation = func() *time.Location {
	l, err := time.LoadLocation("Asia/Ho_Chi_Minh")
	if err != nil {
		return time.FixedZone("ICT", 7*3600)
	}
	return l
}()

// GenerateNumberCandidates builds candidate số hiệu strings for an OR
// lookup. e.g. numberRaw="15", suffixes=["TT-BCA"], year="" →
// ["15/<currentYear>/TT-BCA", "15/<currentYear-1>/TT-BCA", "15/TT-BCA"].
func GenerateNumberCandidates(numberRaw string, suffixes []string, year string) []string {
	currentYear := time.Now().In(vnLocation).Year()
	var candidates []string
	for _, suffix := range suffixes {
		if year != "" {
			candidates = append(candidates, numberRaw+"/"+year+"/"+suffix)
		} else {
			candidates = append(candidates,
				numberRaw+"/"+itoa(currentYear)+"/"+suffix,
				numberRaw+"/"+itoa(currentYear-1)+"/"+suffix,
			)
		}
		candidates = append(candidates, numberRaw+"/"+suffix)
	}
	// Dedup preserving order.
	seen := map[string]bool{}
	out := candidates[:0]
	for _, c := range candidates {
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// ExtractTitleKeywords returns meaningful tokens from leftover reference
// text. Keeps 2-char tokens (e.g. "An" in "An ninh mạng"); noise function
// words are filtered by the stopword list instead of by length.
func ExtractTitleKeywords(text string) []string {
	var out []string
	for _, t := range strings.Fields(text) {
		if utf8.RuneCountInString(t) >= 2 && !legalStopwords[strings.ToLower(t)] {
			out = append(out, t)
		}
	}
	return out
}

// ParsedReference is the deterministic structured extraction of a document
// reference (Stage 0 of AIRAG resolve_candidates — regex only, no LLM).
type ParsedReference struct {
	DocTypeSlug         string
	DocumentNumber      string
	DocNumberCandidates []string
	TitleKeywords       []string
	IssuingAgencyText   string
	IssuingAgencyCode   string
	Year                string
	SectionReference    string
	NumberRaw           string // bare leading number — identity signal
	Confidence          string // "high" | "medium" | "low"
}

var (
	docNumFullRe    = regexp.MustCompile(`\b(\d+/\d{4}/[\p{L}\p{N}_\-]+)\b`)
	docNumShortRe   = regexp.MustCompile(`\b(\d+/[\p{L}\p{N}_\-]{3,})\b`)
	docNumSoRe      = regexp.MustCompile(`(?i)\bsố\s+(\d+(?:[./]\d+)*)\b`)
	yearRe          = regexp.MustCompile(`\b((?:19|20)\d{2})\b`)
	bareNumberRe    = regexp.MustCompile(`\b(\d{1,4})\b`)
	connectorWordRe = regexp.MustCompile(`(?i)\b(?:của|do|bởi)\b`)
	leadingDigitRe  = regexp.MustCompile(`^(\d+)`)
)

// ParseReference extracts structured document metadata from a free-form
// reference ("Nghị định 13 năm 2023", "Thông tư 15 của Bộ Công an") using
// regex only. Port of AIRAG doc_resolver._extract_by_regex.
func ParseReference(reference string) ParsedReference {
	text := strings.TrimSpace(reference)
	for _, pat := range actionPatterns {
		text = strings.TrimSpace(pat.ReplaceAllString(text, ""))
	}

	var res ParsedReference
	res.Confidence = "low"

	// Section reference (Điều X, Chương Y…) — extracted first, removed from
	// the working text.
	for _, spat := range sectionPatterns {
		if sm := spat.FindStringIndex(text); sm != nil {
			res.SectionReference = strings.TrimSpace(text[sm[0]:sm[1]])
			text = strings.TrimSpace(text[:sm[0]] + " " + text[sm[1]:])
			break
		}
	}

	// Explicit document number (53/2022/NĐ-CP, 23/TT-BCA, số 361).
	numberRaw := ""
	for _, dnp := range []*regexp.Regexp{docNumFullRe, docNumShortRe, docNumSoRe} {
		if dnm := dnp.FindStringSubmatchIndex(text); dnm != nil {
			res.DocumentNumber = text[dnm[2]:dnm[3]]
			text = strings.TrimSpace(text[:dnm[0]] + text[dnm[1]:])
			if m := leadingDigitRe.FindStringSubmatch(res.DocumentNumber); m != nil {
				numberRaw = m[1]
			}
			break
		}
	}

	// Year BEFORE the bare-number fallback: a standalone 4-digit year must
	// not be mistaken for a document number. Explicit numbers were consumed
	// above, so this only removes a true publication year.
	if ym := yearRe.FindStringIndex(text); ym != nil {
		res.Year = text[ym[0]:ym[1]]
		text = strings.TrimSpace(text[:ym[0]] + text[ym[1]:])
	}

	// Bare number after stripping the year (e.g. "TT 15 BCA").
	if numberRaw == "" {
		if bare := bareNumberRe.FindStringIndex(text); bare != nil {
			numberRaw = text[bare[0]:bare[1]]
			text = strings.TrimSpace(text[:bare[0]] + text[bare[1]:])
		}
	}

	// Document type — longest keyword match first.
	textLower := strings.ToLower(text)
	for _, dt := range DocTypeKeywords {
		if strings.Contains(textLower, dt.Keyword) {
			res.DocTypeSlug = dt.Slug
			text = removeFirstCI(text, dt.Keyword)
			break
		}
	}

	// Issuing agency — longest name first.
	var agencySuffixes []string
	textLower = strings.ToLower(text)
	for _, ag := range IssuingAgency {
		if strings.Contains(textLower, ag.Name) {
			res.IssuingAgencyText = titleCase(ag.Name)
			res.IssuingAgencyCode = ag.Code
			agencySuffixes = ag.Suffixes
			text = removeFirstCI(text, ag.Name)
			text = strings.TrimSpace(connectorWordRe.ReplaceAllString(text, ""))
			break
		}
	}

	text = strings.Join(strings.Fields(text), " ")

	// Candidate số hiệu generation.
	if numberRaw != "" && len(agencySuffixes) > 0 {
		res.DocNumberCandidates = GenerateNumberCandidates(numberRaw, agencySuffixes, res.Year)
	} else if numberRaw != "" && res.DocTypeSlug != "" {
		if sfx := TypeDefaultSuffix[res.DocTypeSlug]; len(sfx) > 0 {
			res.DocNumberCandidates = GenerateNumberCandidates(numberRaw, sfx, res.Year)
		}
	}
	// An explicit full number is always the highest-priority candidate.
	if res.DocumentNumber != "" && !contains(res.DocNumberCandidates, res.DocumentNumber) {
		res.DocNumberCandidates = append([]string{res.DocumentNumber}, res.DocNumberCandidates...)
	}

	res.NumberRaw = numberRaw
	res.TitleKeywords = ExtractTitleKeywords(text)

	// Confidence assessment.
	hasCandidates := len(res.DocNumberCandidates) > 0
	hasNum := res.DocumentNumber != ""
	hasType := res.DocTypeSlug != ""
	hasAgency := res.IssuingAgencyText != ""
	hasTitleKW := len(res.TitleKeywords) > 0
	switch {
	case hasNum && hasType:
		res.Confidence = "high"
	case hasCandidates && (hasAgency || hasType):
		res.Confidence = "high"
	case hasCandidates || (hasType && hasTitleKW):
		res.Confidence = "medium"
	default:
		res.Confidence = "low"
	}
	return res
}

// removeFirstCI deletes the first case-insensitive occurrence of needle
// from text (replacement for Python's re.sub(..., count=1, IGNORECASE)).
func removeFirstCI(text, needle string) string {
	idx := strings.Index(strings.ToLower(text), strings.ToLower(needle))
	if idx < 0 {
		return text
	}
	return strings.TrimSpace(text[:idx] + text[idx+len(needle):])
}

// titleCase upper-cases the first letter of each word — used for the agency
// display text.
func titleCase(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		r := []rune(w)
		if len(r) > 0 {
			r[0] = []rune(strings.ToUpper(string(r[0])))[0]
			words[i] = string(r)
		}
	}
	return strings.Join(words, " ")
}

// NumberTokenPresent reports whether bare appears in text as a standalone
// number ("53" matches "53/2022/NĐ-CP" but NOT "853", "153" or "2053").
// Python uses (?<!\d)bare(?!\d) — Go has no lookaround, so boundaries are
// checked by hand.
func NumberTokenPresent(text, bare string) bool {
	if text == "" || bare == "" {
		return false
	}
	start := 0
	for {
		idx := strings.Index(text[start:], bare)
		if idx < 0 {
			return false
		}
		abs := start + idx
		leftOK := abs == 0
		if !leftOK {
			r, _ := utf8.DecodeLastRuneInString(text[:abs])
			leftOK = !(r >= '0' && r <= '9')
		}
		end := abs + len(bare)
		rightOK := end >= len(text)
		if !rightOK {
			r, _ := utf8.DecodeRuneInString(text[end:])
			rightOK = !(r >= '0' && r <= '9')
		}
		if leftOK && rightOK {
			return true
		}
		start = abs + 1
	}
}
