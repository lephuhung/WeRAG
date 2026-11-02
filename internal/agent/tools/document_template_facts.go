package tools

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Two checks of a template draft against what the user wrote, beside
// draftViolations (what the draft invents):
//
//   - spokenValue: whether a name, title or agency of the draft is the
//     user's. The model decorates them ("Phó Giám đốc Công an tỉnh" for the
//     "Phó Giám đốc" the user wrote, "CÔNG AN TỈNH ……" with a placeholder for
//     what it does not know): placeholders are dropped before the check, and
//     a title or agency that is not verbatim keeps its longest leading part
//     the user wrote when the rest is words found elsewhere in the
//     conversation — the rest is dropped, nothing is ever added.
//   - draftOmissions: the figures the user settled (amounts, dates,
//     percentages, counts) the draft leaves out, asked for once more.

// templatePlaceholderRe matches what stands for an unknown value: an
// ellipsis ("……", "..."), a bracketed placeholder ("[...]", "[tên]"), a
// run of underscores.
var templatePlaceholderRe = regexp.MustCompile(`\[[^\]]*\]|…+|\.{2,}|_{2,}`)

// stripPlaceholders removes the placeholders of s; partial reports that
// there were some (the value is incomplete).
func stripPlaceholders(s string) (string, bool) {
	t := templatePlaceholderRe.ReplaceAllString(s, " ")
	t = strings.Trim(strings.Join(strings.Fields(t), " "), " .;:,-–")
	return t, templatePlaceholderRe.MatchString(s)
}

// saidWords reports whether text holds the words of s as whole words
// (folded; "an" is not found in "thanh").
func saidWords(text, s string) bool {
	s = strings.Trim(fold(s), " .;:,")
	if s == "" {
		return false
	}
	t := fold(text)
	for i := 0; ; {
		j := strings.Index(t[i:], s)
		if j < 0 {
			return false
		}
		start, end := i+j, i+j+len(s)
		if wordEdge(t, start, true) && wordEdge(t, end, false) {
			return true
		}
		i = start + 1
		for i < len(t) && !isRuneStart(t[i]) {
			i++
		}
	}
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }

// wordEdge: the byte offset i of t is a word boundary (before: the rune
// before i is no letter or digit; after: the rune at i is none).
func wordEdge(t string, i int, before bool) bool {
	if before {
		if i == 0 {
			return true
		}
		last, _ := utf8.DecodeLastRuneInString(t[:i])
		return !unicode.IsLetter(last) && !unicode.IsDigit(last)
	}
	if i >= len(t) {
		return true
	}
	r, _ := utf8.DecodeRuneInString(t[i:])
	return !unicode.IsLetter(r) && !unicode.IsDigit(r)
}

// spokenValue is the part of the draft's value v the user wrote, "" when
// none: v without its placeholders when userText holds it (said). For a
// title or an agency, scope holds the sentences that speak of its role
// (the signer, the issuing agency, the user themself): there a shorter
// value is accepted, the longest leading part of v the user wrote
// (headSaid), when the words after it are found in wider (the user's and
// the conversation's words): "Phó Giám đốc Công an tỉnh" → "Phó Giám đốc"
// when "Phó Giám đốc," was written about the signer and "Công an tỉnh"
// somewhere. scope nil: no shorter value (a person's name is never cut:
// "Nguyễn Văn" of "Nguyễn Văn Anh" is not the user's "Nguyễn Văn An").
// partial: v held placeholders.
func spokenValue(userText, wider, v string, scope []string) (string, bool) {
	s, partial := stripPlaceholders(v)
	if s == "" {
		return "", partial
	}
	if said(userText, s) {
		return s, partial
	}
	if len(scope) == 0 {
		return "", partial
	}
	words := strings.Fields(s)
	for n := len(words) - 1; n >= 2; n-- {
		h := strings.TrimRight(strings.Join(words[:n], " "), " ,;:-–")
		if !headSaid(scope, h) {
			continue
		}
		// the longest head the user wrote: its rest must be words of the
		// conversation, else the value is the model's own
		if saidWords(wider, strings.Join(words[n:], " ")) {
			return h, partial
		}
		return "", partial
	}
	return "", partial
}

// structuralWords end the part of a name that is no name yet ("Sở", "Phòng
// …", "Công an", "UBND tỉnh"): a value cut there is not one the user wrote.
var structuralWords = map[string]bool{
	"sở": true, "phòng": true, "ban": true, "cục": true, "tỉnh": true, "huyện": true, "xã": true,
	"phường": true, "quận": true, "thị": true, "trấn": true, "thành": true, "phố": true, "an": true, "ubnd": true,
	"hđnd": true, "ủy": true, "uỷ": true, "đội": true, "viện": true, "trường": true, "bộ": true, "tâm": true,
	"chi": true, "tổng": true, "văn": true, "đoàn": true, "hội": true, "khoa": true, "đảng": true, "trung": true,
	"công": true, "nhân": true, "dân": true, "của": true, "và": true,
}

// clauseEndRe: what may follow a value the user ended there (a comma, the
// end of a sentence or of the message).
var clauseEndRe = regexp.MustCompile(`^\s*([,.;:!?)\n]|$)`)

// headSaid: one of the scope's texts holds h as whole words, not right
// after "Phó" / "Quyền" (heldBy), ending a clause there; h does not end in
// a structural word.
func headSaid(scope []string, h string) bool {
	f := strings.Fields(fold(h))
	if len(f) == 0 || structuralWords[strings.Trim(f[len(f)-1], ".,;:")] {
		return false
	}
	h = strings.Trim(fold(h), " .;:,")
	for _, text := range scope {
		t := fold(text)
		for i := 0; i < len(t); {
			j := strings.Index(t[i:], h)
			if j < 0 {
				break
			}
			start, end := i+j, i+j+len(h)
			if wordEdge(t, start, true) && wordEdge(t, end, false) && !heldBy(t[:start], h) && clauseEndRe.MatchString(t[end:]) {
				return true
			}
			i = start + 1
			for i < len(t) && !isRuneStart(t[i]) {
				i++
			}
		}
	}
	return false
}

// deputyRe: a title right before a match makes it another one ("Phó Giám
// đốc" is no "Giám đốc", "Quyền Trưởng phòng" no "Trưởng phòng").
var deputyRe = regexp.MustCompile(`(^|[^\p{L}])(phó|quyền|q\.)\s*$`)

// heldBy: the folded text before a match of s ends in a deputy's title.
func heldBy(before, s string) bool {
	return deputyRe.MatchString(before)
}

// scopeSentences are the sentences of text keep accepts (the ones
// speaking of a role).
func scopeSentences(text string, keep func(sentence string) bool) []string {
	var out []string
	for _, s := range sentences(text) {
		if keep(s) {
			out = append(out, s)
		}
	}
	return out
}

// agencyTalkRe marks a sentence about the issuing agency or the user's own
// unit ("cơ quan ban hành: …", "đơn vị tôi …"), not one naming a place
// ("tại hội trường Công an tỉnh").
var agencyTalkRe = regexp.MustCompile(`cơ quan|ban hành|chủ quản|đơn vị (của )?(tôi|mình)|địa danh|gửi từ|văn bản của`)

// A settled fact is a figure the user wrote in a statement: an amount
// ("50 triệu đồng"), a date ("25/10/2026", "ngày 5 tháng 3"), a
// percentage ("15%") or a count ("3 lớp", "40 học viên").
type settledFact struct {
	// Text is the fact as the user wrote it (told to the model).
	Text string
	kind string
	// key groups the facts a later statement revises: a count's noun, the
	// words before an amount or a percentage; dates have none.
	key string
	// nums are the numbers the draft must hold (a date's day, month,
	// year); value is an amount in đồng, else 0.
	nums  []string
	value float64
}

var (
	// "25/10/2026", "25/10", "25-10-2026", "ngày 25 tháng 10 năm 2026"
	factDateRe   = regexp.MustCompile(`(?i)(\d{1,2})/(\d{1,2})(?:/(\d{4}))?|(\d{1,2})-(\d{1,2})-(\d{4})|ngày\s+(\d{1,2})\s+tháng\s+(\d{1,2})(?:\s+năm\s+(\d{4}))?`)
	factAmountRe = regexp.MustCompile(`(?i)(\d+(?:[.,]\d+)*)\s*(tỷ|tỉ|triệu|nghìn|ngàn|đồng|vnđ|vnd)`)
	factPctRe    = regexp.MustCompile(`(\d+(?:[.,]\d+)?)\s*(%|phần trăm)`)
	factCountRe  = regexp.MustCompile(`(\d+)\s+(\p{L}+)(?:\s+(\p{L}+))?`)
	// a number of a reference, not a fact of the content: "Nghị định 30",
	// "NĐ 30", "Điều 5", "số 12/2020/NĐ-CP", "Mẫu 1.1"
	factRefBeforeRe = regexp.MustCompile(`(?i)(nghị định|nđ|thông tư|tt|quyết định|qđ|luật|điều|khoản|điểm|mục|chương|phụ lục|mẫu|số|công văn|cv|kế hoạch|kh|chỉ thị|ct|nghị quyết|nq|tờ trình|ttr|báo cáo|bc)\s*$`)
	factDocNumberRe = regexp.MustCompile(`\d+/\d{4}/[\p{L}Đđ-]+|\d+/[\p{Lu}Đ][\p{L}Đđ-]*`)
	// a sentence ends at ".", "!", "?", ";" before a space (not inside
	// "50.000.000"), or at a line break
	factSentenceEndRe = regexp.MustCompile(`[.!?;]+(\s|$)|\n`)
	factYearRe        = regexp.MustCompile(`^(19|20)\d\d$`)
	// a number right after an hour is its minutes ("8 giờ 30")
	factAfterTimeRe = regexp.MustCompile(`(?i)(\d\s*(giờ|h)|giờ)\s*$`)
	// a day/month without a year only after a word of time ("từ 25/10",
	// "hạn 20/10"): "3/4 số cán bộ" is a fraction
	factDateLeadRe = regexp.MustCompile(`(?i)(ngày|từ|đến|tới|trước|sau|hạn|hết|đầu|cuối)\s*$`)
)

// factNotCounts are the words after a number that make no count: the
// request itself ("2 văn bản", "1 bản") and links of a range ("25 đến").
var factTimeWords = map[string]bool{"giờ": true, "h": true, "phút": true, "giây": true}

var factNotCounts = map[string]bool{
	"văn bản": true, "bản": true, "mẫu": true, "file": true, "tệp": true, "trang": true, "kế hoạch": true, "công văn": true,
	"tờ trình": true, "đến": true, "tới": true, "và": true, "hoặc": true, "hay": true, "đến ngày": true,
}

// sentences splits a message into its sentences.
func sentences(msg string) []string {
	var out []string
	last := 0
	for _, m := range factSentenceEndRe.FindAllStringIndex(msg, -1) {
		out = append(out, msg[last:m[1]])
		last = m[1]
	}
	return append(out, msg[last:])
}

var factScales = map[string]float64{
	"tỷ": 1e9, "tỉ": 1e9, "triệu": 1e6, "nghìn": 1e3, "ngàn": 1e3, "đồng": 1, "vnđ": 1, "vnd": 1, "đ": 1,
}

// parseAmount reads "50", "1,5", "50.000.000" as a number.
func parseAmount(n string) float64 {
	switch {
	case strings.Count(n, ".") > 1 || strings.Count(n, ",") > 1 ||
		(len(n) > 4 && (n[len(n)-4] == '.' || n[len(n)-4] == ',')):
		// thousands separators
		n = strings.NewReplacer(".", "", ",", "").Replace(n)
	default:
		n = strings.ReplaceAll(n, ",", ".")
	}
	f, _ := strconv.ParseFloat(n, 64)
	return f
}

func trimZeros(n string) string {
	if t := strings.TrimLeft(n, "0"); t != "" {
		return t
	}
	return "0"
}

// wordsBefore are the last two words of s (the key of an amount: "kinh
// phí").
func wordsBefore(s string) string {
	f := strings.FieldsFunc(fold(s), func(r rune) bool { return !unicode.IsLetter(r) })
	if len(f) > 2 {
		f = f[len(f)-2:]
	}
	return strings.Join(f, " ")
}

// sentenceFacts are the facts one statement holds.
func sentenceFacts(s string) []settledFact {
	var out []settledFact
	taken := make([]bool, len(s))
	take := func(a, b int) bool {
		for i := a; i < b; i++ {
			if taken[i] {
				return false
			}
		}
		for i := a; i < b; i++ {
			taken[i] = true
		}
		return true
	}
	// numbers that belong to a reference are no facts
	for _, m := range factDocNumberRe.FindAllStringIndex(s, -1) {
		take(m[0], m[1])
	}
	for _, m := range factDateRe.FindAllStringSubmatchIndex(s, -1) {
		// the day, month and year of whichever form matched
		var g []string
		for i := 2; i+1 < len(m); i += 2 {
			if m[i] >= 0 {
				g = append(g, s[m[i]:m[i+1]])
			}
		}
		d, mo, y := g[0], g[1], ""
		if len(g) > 2 {
			y = g[2]
		}
		if y == "" && m[2] >= 0 && !factDateLeadRe.MatchString(s[:m[0]]) {
			continue
		}
		di, _ := strconv.Atoi(d)
		mi, _ := strconv.Atoi(mo)
		if di < 1 || di > 31 || mi < 1 || mi > 12 || factRefBeforeRe.MatchString(s[:m[0]]) || !take(m[0], m[1]) {
			continue
		}
		nums := []string{trimZeros(d), trimZeros(mo)}
		if y != "" {
			nums = append(nums, y)
		}
		out = append(out, settledFact{Text: strings.TrimSpace(s[m[0]:m[1]]), kind: "date", nums: nums})
	}
	for _, m := range factPctRe.FindAllStringSubmatchIndex(s, -1) {
		if !take(m[0], m[1]) {
			continue
		}
		n := s[m[2]:m[3]]
		out = append(out, settledFact{Text: s[m[0]:m[1]], kind: "percent", key: wordsBefore(s[:m[0]]), nums: []string{n}})
	}
	for _, m := range factAmountRe.FindAllStringSubmatchIndex(s, -1) {
		if factRefBeforeRe.MatchString(s[:m[0]]) || !take(m[0], m[1]) {
			continue
		}
		n, scale := s[m[2]:m[3]], strings.ToLower(s[m[4]:m[5]])
		end := m[1]
		if scale != "đồng" && strings.HasPrefix(strings.ToLower(strings.TrimSpace(s[end:])), "đồng") {
			end = strings.Index(strings.ToLower(s[end:]), "đồng") + end + len("đồng")
			take(m[1], end)
		}
		out = append(out, settledFact{Text: s[m[0]:end], kind: "amount", key: wordsBefore(s[:m[0]]),
			nums: []string{n}, value: parseAmount(n) * factScales[scale]})
	}
	for _, m := range factCountRe.FindAllStringSubmatchIndex(s, -1) {
		n := s[m[2]:m[3]]
		noun := strings.ToLower(s[m[4]:m[5]])
		before := s[:m[2]]
		// "NĐ30", "PV01": a code; "3/4 số", "1.5 lần": part of a figure
		if last, _ := utf8.DecodeLastRuneInString(before); before != "" &&
			(unicode.IsLetter(last) || unicode.IsDigit(last) || strings.ContainsRune("/.,:", last)) {
			continue
		}
		// a time ("8 giờ 30", "14h30"), a phone or another long number
		if factTimeWords[noun] || factAfterTimeRe.MatchString(before) || len(n) >= 4 {
			continue
		}
		two := noun
		if m[6] >= 0 {
			two += " " + strings.ToLower(s[m[6]:m[7]])
		}
		if factNotCounts[noun] || factNotCounts[two] || factRefBeforeRe.MatchString(before) || factYearRe.MatchString(n) ||
			trimZeros(n) == "0" || trimZeros(n) == "1" || !take(m[2], m[3]) {
			continue
		}
		// the noun keys the siblings ("40 học viên" … "35 học viên")
		out = append(out, settledFact{Text: strings.TrimSpace(s[m[0]:m[1]]), kind: "count", key: noun, nums: []string{n}})
	}
	return out
}

// settledFacts are the figures the user settled in their messages, oldest
// first: the earlier conversation's user turns, then this request's
// messages. They are only asked for again (draftOmissions), so the rule
// errs on not asking: only the user's words count (what the other agent
// proposed is no fact until the user writes it); a question ("… 40 hay 50
// học viên?") and a sentence offering a choice ("hoặc", "hay") settle
// nothing; a figure after "không phải", "thay vì", "chứ không phải" is
// retracted ("không phải 50 triệu mà là 60 triệu": 60 stays); and a later
// sentence giving a sibling (an amount after an amount, a percentage
// after a percentage, a date after dates, a count of the same noun)
// leaves the earlier ones unsettled, since it may revise them ("dự kiến 50
// triệu" … "chốt 60 triệu"; "từ 25/10 đến 27/10" … "bắt đầu 26/10"). Two
// amounts, percentages or counts of one noun in one sentence are
// ambiguous and not asked for; dates of one sentence (a range) are.
func settledFacts(conversation []TemplateTurn, inputs []string) []settledFact {
	var msgs []string
	for _, t := range conversation {
		if t.Role == "user" {
			msgs = append(msgs, t.Text)
		}
	}
	msgs = append(msgs, inputs...)
	var facts []settledFact
	for _, msg := range msgs {
		msg = templatePromptRe.ReplaceAllString(msg, " ")
		for _, sent := range sentences(msg) {
			if strings.HasSuffix(strings.TrimSpace(sent), "?") {
				continue
			}
			found := sentenceFacts(dropRetracted(sent))
			if len(found) == 0 {
				continue
			}
			// the earlier siblings are no longer settled
			kept := facts[:0]
			for _, old := range facts {
				revised := false
				for _, f := range found {
					revised = revised || siblingFacts(old, f)
				}
				if !revised {
					kept = append(kept, old)
				}
			}
			facts = kept
			if factChoiceRe.MatchString(fold(sent)) {
				continue
			}
			for i, f := range found {
				ambiguous := false
				for j, g := range found {
					ambiguous = ambiguous || (i != j && f.kind != "date" && siblingFacts(f, g))
				}
				dup := false
				for _, old := range facts {
					dup = dup || fold(old.Text) == fold(f.Text)
				}
				if !ambiguous && !dup {
					facts = append(facts, f)
				}
			}
		}
	}
	return facts
}

// siblingFacts: b may revise a (the same kind; a count of the same noun).
func siblingFacts(a, b settledFact) bool {
	if a.kind != b.kind {
		return false
	}
	if a.kind == "count" {
		return a.key == b.key
	}
	return true
}

// factChoiceRe marks a sentence offering a choice of figures.
var factChoiceRe = regexp.MustCompile(`(^|[^\p{L}])(hoặc|hay)([^\p{L}]|$)`)

// factRetractRe: a figure the user takes back, up to what replaces it
// ("không phải 50 triệu mà là 60 triệu", "60 triệu chứ không phải 50",
// "35 thay vì 40 học viên").
var factRetractRe = regexp.MustCompile(`(?i)(?:không phải là|không phải|thay vì|thay cho|không còn)\s+(.+?)(?:\s+(?:mà là|mà|chứ|nay là|thành)\s+|[,;]|$)`)

// dropRetracted blanks the retracted figures of a sentence.
func dropRetracted(s string) string {
	b := []byte(s)
	for _, m := range factRetractRe.FindAllStringSubmatchIndex(s, -1) {
		for i := m[2]; i < m[3]; i++ {
			b[i] = ' '
		}
	}
	return string(b)
}

// vietnameseCounts are the words a draft may write a small count with.
var vietnameseCounts = map[string]string{
	"2": "hai", "3": "ba", "4": "bốn", "5": "năm", "6": "sáu", "7": "bảy", "8": "tám", "9": "chín", "10": "mười",
}

// draftText is all the content text of a draft (not the signer).
func draftText(d *templateDraft) string {
	parts := []string{d.Subject, d.Addressee, d.RequestBasis}
	for _, p := range d.Paragraphs {
		parts = append(parts, p.Text)
	}
	parts = append(parts, d.Grounds...)
	parts = append(parts, d.Recipients...)
	for _, v := range d.Form {
		parts = append(parts, string(v))
	}
	return strings.Join(parts, "\n")
}

// factIn reports whether the draft text holds the fact: all its numbers
// (a date as "25/10/2026" or "ngày 25 tháng 10 năm 2026"), an amount also
// at another scale ("50 triệu" = "50.000.000"), a count also in words
// ("ba lớp").
func factIn(f settledFact, text string, nums map[string]bool, amounts map[float64]bool) bool {
	if f.kind == "amount" && f.value > 0 && amounts[f.value] {
		return true
	}
	all := true
	for _, n := range f.nums {
		if f.kind == "amount" || f.kind == "percent" {
			n = strings.NewReplacer(",", ".").Replace(n)
		}
		if !nums[trimZeros(n)] {
			all = false
		}
	}
	if all {
		return true
	}
	if f.kind == "count" {
		if w := vietnameseCounts[trimZeros(f.nums[0])]; w != "" {
			noun := strings.Fields(f.key)[0]
			return saidWords(text, w+" "+noun)
		}
	}
	return false
}

var (
	draftNumberRe = regexp.MustCompile(`\d+(?:[.,]\d+)?`)
	// a figure with thousands separators: "50.000.000"
	fullFigureRe = regexp.MustCompile(`\d{1,3}(?:[.,]\d{3})+`)
)

// draftOmissions are the settled facts the draft leaves out, as the user
// wrote them.
func draftOmissions(d *templateDraft, facts []settledFact) []string {
	if len(facts) == 0 {
		return nil
	}
	text := draftText(d)
	nums := map[string]bool{}
	for _, n := range digitsRe.FindAllString(text, -1) {
		nums[trimZeros(n)] = true
	}
	for _, n := range draftNumberRe.FindAllString(text, -1) {
		nums[trimZeros(strings.ReplaceAll(n, ",", "."))] = true
	}
	amounts := map[float64]bool{}
	for _, f := range sentenceFacts(text) {
		if f.kind == "amount" && f.value > 0 {
			amounts[f.value] = true
		}
	}
	// a figure written out in full ("50.000.000") is an amount in đồng
	for _, m := range fullFigureRe.FindAllString(text, -1) {
		amounts[parseAmount(m)] = true
	}
	var out []string
	for _, f := range facts {
		if !factIn(f, text, nums, amounts) {
			out = append(out, f.Text)
		}
	}
	return out
}

// onlySkipWords: the message holds nothing but a skip or a refusal and the
// words around one ("bỏ qua", "cứ tạo văn bản để tôi tự điền sau"): it
// adds no fact to the request.
func onlySkipWords(m string) bool {
	f := fold(m)
	t := keepBlankRe.ReplaceAllString(skipAsksRe.ReplaceAllString(templatePromptRe.ReplaceAllString(f, " "), " "), " ")
	for _, w := range strings.FieldsFunc(t, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if !emptyAskWords[w] {
			return false
		}
	}
	return true
}
