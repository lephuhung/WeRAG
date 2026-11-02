package vbpl

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Phrase amendments change words in place instead of quoting a new
// provision: "Sửa đổi cụm từ “500 triệu đồng” thành “01 tỷ đồng” tại Điều
// 3, Điều 4, khoản 1 Điều 8 Nghị định số 68/2026/NĐ-CP", "Thay thế cụm từ
// “cấp huyện” bằng cụm từ “cấp xã” tại khoản 2 Điều 34", "Bãi bỏ cụm từ
// “cấp huyện,” tại khoản 1 Điều 11". Each named provision becomes one Amend
// with From/To set (To empty for a removal).

var (
	// phraseLeadRe opens a replacement or removal: the verb, then "cụm từ" /
	// "từ" / "từ ngữ" right before the first quoted phrase.
	phraseLeadRe   = regexp.MustCompile(`(?i)(?:sửa đổi|thay thế|thay đổi|thay|bãi bỏ)\s+(?:các\s+)?(?:cụm\s+từ|từ\s+ngữ|từ)\s*:?\s*[“"]`)
	phraseRemoveRe = regexp.MustCompile(`(?i)bãi bỏ\s+(?:các\s+)?(?:cụm\s+từ|từ\s+ngữ|từ)\s*:?\s*[“"]`)
	// phraseMoreRe opens a clause that goes on with the verb before it:
	// "…; cụm từ “C” bằng cụm từ “D” tại …".
	phraseMoreRe = regexp.MustCompile(`(?i)^(?:và\s+)?(?:các\s+)?(?:cụm\s+từ|từ\s+ngữ|từ)\s*:?\s*[“"]`)
	// phraseVerbRe starts a new clause after "," or "và": another verb.
	phraseVerbRe = regexp.MustCompile(`(?i)^(?:và\s+)?(?:bãi bỏ|sửa đổi|thay thế|thay đổi|thay|bổ sung)\s`)
	// phrasePairRe reads one “X” [tại …] (bằng|thành) [cụm từ|từ] “Y”.
	phrasePairRe = regexp.MustCompile(`[“"]([^”"]{1,200})[”"]\s*(tại\s+[^“”"]*?\s+)?(?:bằng|thành)\s+(?:cụm\s+từ\s+|từ\s+ngữ\s+|từ\s+)?[“"]([^”"]{0,200})[”"]`)
	// phraseOnlyRe reads a removed “X”.
	phraseOnlyRe = regexp.MustCompile(`[“"]([^”"]{1,200})[”"]`)
	// phraseProvRe reads the provisions after tại / trong / quy định tại. A
	// bare number (or, after điểm, a letter) after "," or "và" continues the
	// label just before it; provMatches checks both and that the match does
	// not run on into a word.
	// Only the keywords ignore case: a capital after "," ("…, Điều 4") is
	// not a bare điểm letter.
	phraseProvRe = regexp.MustCompile(`((?i:điểm|khoản|điều))\s+(\d+[a-zđ]?|[a-zđ]\d*)|(?:,|\svà)\s*(\d+[a-zđ]?)|(?:,|\svà)\s*([a-zđ]\d*)`)
	// phraseBareNextRe is what may follow a bare label: the list goes on or
	// ends ("Điều 3, 4 Luật …"), not a quantity ("Điều 3, 15 ngày").
	phraseBareNextRe = regexp.MustCompile(`(?i)^(?:\s*[,;.)]|\s*$|\s+(?:và|khoản|điểm|điều|của|tại|bộ luật|luật|pháp lệnh|nghị định|nghị quyết|thông tư|quyết định)(?:\s|$))`)
	// phraseListMoreRe is a segment after ";" that only goes on with the
	// provision list: labels and connectors, then at most a document name
	// ("; khoản 6 Điều 5; Điều 74 Nghị định số 53/2024/NĐ-CP"), not "; Điều 5
	// được sửa đổi như sau".
	phraseListMoreRe = regexp.MustCompile(`(?i)^\s*(?:(?:các|tên)\s+)?(?:điểm|khoản|điều)\s+[0-9a-zđ]+(?:(?:\s*,\s*|\s+và\s+|\s+)(?:(?:các|tên)\s+)?(?:(?:điểm|khoản|điều)\s+)?[0-9a-zđ]+)*(?:\s+(?:của\s+)?(?:` + "Bộ luật|Luật|Pháp lệnh|Nghị định|Nghị quyết|Thông tư liên tịch|Thông tư|Quyết định" + `)\s[^;()]*)?\s*[.]?\s*$`)
	// phraseBracketRe is a bracketed remark in a provision list.
	phraseBracketRe = regexp.MustCompile(`\([^()]*\)`)
	// phraseOtherDocRe, inside a title before its number, means the number
	// is another document's ("Nghị định này, Nghị định số 12/2020/NĐ-CP",
	// "Luật Đất đai, được hướng dẫn tại Nghị định số …").
	phraseOtherDocRe = regexp.MustCompile(`(?i)(?:^|\s)(?:` + "Bộ luật|Luật|Pháp lệnh|Nghị định|Nghị quyết|Thông tư liên tịch|Thông tư|Quyết định" + `|này|được|tại|theo)(?:\s|,|$)`)
	// phraseDocTypeRe is the document type a title starts with.
	phraseDocTypeRe = regexp.MustCompile(`(?i)^(?:Bộ luật|Luật|Pháp lệnh|Nghị định|Nghị quyết|Thông tư liên tịch|Thông tư|Quyết định)\s`)
	// phraseSpacedNumberRe joins "36/2024/TT- BGTVT".
	phraseSpacedNumberRe = regexp.MustCompile(`(\d+/\d{4}/[A-ZĐ]+)-\s+([A-ZĐ])`)
	// phraseExceptRe marks an exception ("trừ …"): such a list is not read.
	phraseExceptRe = regexp.MustCompile(`(?i)(?:^|[\s,(])(?:ngoại trừ|trừ|không áp dụng)(?:[\s,:]|$)`)
	fileNumberRe   = regexp.MustCompile(`\b(\d+)-(\d{4})-([A-ZĐ])`)
	phraseNumberRe = regexp.MustCompile(`\d+/\d{4}/[A-ZĐa-zđ0-9\-]+`)
	// phraseDocRe reads the document named right after the provisions,
	// with or without a title before its number: "Luật Xây dựng số
	// 50/2014/QH13", "Nghị định số 68/2026/NĐ-CP", "Luật Quản lý thuế".
	phraseDocRe = regexp.MustCompile(`(?i)^[\s,]*(?:của\s+|tại\s+)?((?:Bộ luật|Luật|Pháp lệnh|Nghị định|Nghị quyết|Thông tư liên tịch|Thông tư|Quyết định)\s[^,;:(“"]*)`)
	// phraseDocNumberRe is tried first: the title may hold commas ("Luật
	// Phòng, chống ma túy số 73/2021/QH14").
	phraseDocNumberRe = regexp.MustCompile(`(?i)^[\s,]*(?:của\s+|tại\s+)?((?:Bộ luật|Luật|Pháp lệnh|Nghị định|Nghị quyết|Thông tư liên tịch|Thông tư|Quyết định)\s[^;:(“"]{0,160}?\d+/\d{4}/[A-ZĐa-zđ0-9\-]+)`)
	phraseDocEnd      = regexp.MustCompile(`(?i)\s+(?:ngày\s+\d|được sửa đổi|đã được|như sau|và\s)`)
	// phraseDocStartRe is a document title in a provision list: the labels
	// before it are that document's ("Điều 3 Luật Doanh nghiệp, khoản 1
	// Điều 4 Luật Đầu tư"). Titles are capitalised, so it keeps case.
	// A bare type ("của Thông tư, Điều 1") is the document itself, no start.
	phraseDocStartRe = regexp.MustCompile(`(?:^|\s)(?:Bộ luật|Luật|Pháp lệnh|Nghị định|Nghị quyết|Thông tư liên tịch|Thông tư|Quyết định)\s`)
	// phraseProseRe marks prose inside a provision list: the labels after
	// it are not targets ("khoản 1 Điều 3 trong trường hợp quy định tại
	// khoản 2 Điều 9" names only khoản 1 Điều 3). List items that are no
	// provision of a Điều ("tên Chương II", "câu dẫn Điều 10", "Mẫu số 3
	// Phụ lục II ban hành kèm theo …") are not prose.
	phraseProseRe    = regexp.MustCompile(`(?i)(?:^|[\s,;])(?:trong trường hợp|trường hợp|quy định|nếu|khi|được|đối với|để|thì|theo|văn bản|áp dụng|thực hiện)(?:[\s,;.]|$)`)
	phraseAttachedRe = regexp.MustCompile(`(?i)(?:ban hành\s+)?kèm theo`)
	// phraseFileExtRe is the file extension a knowledge title kept.
	phraseFileExtRe = regexp.MustCompile(`(?i)\.+(?:html?|pdf|docx?)$`)
	// phraseAndDocRe is "và" before a second document.
	phraseAndDocRe = regexp.MustCompile(`(?i)\svà\s+(?:các\s+)?(?:Bộ luật|Luật|Pháp lệnh|Nghị định|Nghị quyết|Thông tư liên tịch|Thông tư|Quyết định)(?:\s|$)`)
	// phraseVerbDocRe is a document with its số hiệu right after the verb.
	phraseVerbDocRe = regexp.MustCompile(`(?i)^(?:sửa đổi|bổ sung|thay thế|bãi bỏ)(?:\s*,\s*(?:sửa đổi|bổ sung|thay thế|bãi bỏ))*\s+((?:Bộ luật|Luật|Pháp lệnh|Nghị định|Nghị quyết|Thông tư liên tịch|Thông tư|Quyết định)\s[^:;(“",]{0,120}?\d+/\d{4}/[A-ZĐa-zđ0-9\-]+)`)
	// phraseNumberKeyRe is the serial and year of a số hiệu.
	phraseNumberKeyRe = regexp.MustCompile(`\d+/\d{4}`)
	// phraseGroupSepRe joins the provisions of the next document to the
	// one just named: ", khoản 2 Điều 5 Nghị định số …", "; Điều 5 …".
	phraseGroupSepRe = regexp.MustCompile(`(?i)^\s*(?:[,;]\s*(?:và\s+)?|và\s+)(?:(?:các|tên)\s+)?(?:điểm|khoản|điều)\s+[0-9a-zđ]`)
	// phraseListGoesOnRe: the list goes on after the labels read.
	phraseListGoesOnRe = regexp.MustCompile(`(?i)^\s*(?:[,;]|và\s)`)
	// phraseOtherItemsRe: list items that are not provisions of a Điều
	// (phụ lục, mục, chương, phần), skipped up to the document after them.
	phraseOtherItemsRe = regexp.MustCompile(`(?i)^(?:\s*(?:,|;|và)?\s*(?:các\s+)?(?:phụ lục|mục|chương|phần)\s+[0-9IVXLCDM]+[a-zđ]?\b)+\s*(?:,\s*)?(?:tại|của|thuộc)?`)
	// phraseDocDateRe: a document's date and subject follow its name.
	phraseDocDateRe = regexp.MustCompile(`(?i)^\s*,?\s*ngày\s+\d`)
	// phraseNextRunRe is the next run of provisions after a document's
	// subject: a separator, then a label.
	phraseNextRunRe = regexp.MustCompile(`(?i)(?:[,;]\s*(?:và\s+)?|\svà\s+)(?:(?:các|tên)\s+)?(?:điểm|khoản|điều)\s+[0-9a-zđ]`)
	// phraseHistoryLeadRe opens a document's amendment history right after
	// its name: "đã được sửa đổi, bổ sung …".
	phraseHistoryLeadRe = regexp.MustCompile(`(?i)^\s*,?\s*(?:đã được|được)\s+(?:sửa đổi|bổ sung)`)
	// phraseAmendedByRe opens what amended a document already.
	phraseAmendedByRe = regexp.MustCompile(`(?i)(?:đã được|được)\s+(?:sửa đổi|bổ sung|thay thế)`)
	// phraseCollectiveRe is a heading amending several documents of a
	// kind ("… một số điều của các luật có liên quan").
	phraseCollectiveRe = regexp.MustCompile(`(?i)\bcác\s+(?:bộ luật|luật|pháp lệnh|nghị định|nghị quyết|thông tư|quyết định|văn bản)(?:[\s,.;]|$)`)
	// phraseSemiRunRe is the next run after a ";".
	phraseSemiRunRe = regexp.MustCompile(`(?i);\s*(?:và\s+)?(?:(?:các|tên)\s+)?(?:điểm|khoản|điều)\s+[0-9a-zđ]`)
	// phraseHistoryRe in a document's subject means it tells the
	// document's own amendments, not the next run of the list.
	phraseHistoryRe = regexp.MustCompile(`(?i)sửa đổi|bổ sung|đã được|được hướng dẫn|thay thế|bãi bỏ`)
	// phraseClauseLineRe / phraseItemLineRe start a khoản / điểm paragraph.
	phraseClauseLineRe = regexp.MustCompile(`^\d+[a-zđ]?\.\s`)
	phraseItemLineRe   = regexp.MustCompile(`^[a-zđ]\)\s`)
	// phraseQuotedRe is a quoted phrase.
	phraseQuotedRe = regexp.MustCompile(`“[^”]*”|"[^"]*"`)
	// phraseThisDocRe names the amending document itself.
	phraseThisDocRe = regexp.MustCompile(`(?i)(?:Bộ luật|Luật|Pháp lệnh|Nghị định|Nghị quyết|Thông tư liên tịch|Thông tư|Quyết định)\s+này`)
	// phraseProvLabelRe is a provision label inside a would-be title.
	phraseProvLabelRe = regexp.MustCompile(`(?i)(?:^|\s)(?:điểm|khoản|điều)\s+\d`)
)

// phraseBareMax bounds a bare continuation number ("Điều 3, 4"): no Điều
// or khoản runs to 1000, so "Điều 3, 2026" is a year, not Điều 2026.
const phraseBareMax = 1000

// phraseAmends reads the phrase amendments of one paragraph, sentence by
// sentence. Provisions in an appendix, mục or table (no Điều) are skipped;
// so is a phrase naming no Điều at all ("tại Thông tư này").
func (b *builder) phraseAmends(text string) []Amend {
	return parsePhraseAmends(text, b.enclosingAmendedDocument)
}

// parsePhraseAmends reads text sentence by sentence; enclosing names the
// amended document when a sentence names none.
func parsePhraseAmends(text string, enclosing func() string) []Amend {
	text = phraseSpacedNumberRe.ReplaceAllString(text, "$1-$2")
	var out []Amend
	for _, s := range splitSentences(text) {
		out = append(out, phraseSentence(s, enclosing)...)
	}
	return out
}

// PhraseAmendsInChunk reads the phrase amendments of a stored chunk, for
// chunks ingested before the converter read them: its unquoted paragraphs
// are read as at ingest, the amended document coming from the clause or
// item lead-in above them in the chunk ("5. Sửa đổi, bổ sung một số điều
// của Luật Nhà ở số 27/2023/QH15 như sau:"), the heading path (innermost
// first) or else the document title. A lead-in in an earlier chunk is not
// seen, so the converter can still name a document this misses. number is
// the chunk's own document's số hiệu: a title naming only that one names
// the amended document by its name alone.
func PhraseAmendsInChunk(content string, headingPath []string, title, number string) []Amend {
	// knowledge titles come from file names: "Nghị định số 140-2021-NĐ-CP",
	// "… số 06-2026-QH16.html"
	title = fileNumberRe.ReplaceAllString(phraseFileExtRe.ReplaceAllString(strings.TrimSpace(title), ""), "$1/$2/$3")
	titleDoc := func() string {
		if !amendVerbRe.MatchString(title) {
			return ""
		}
		d := amendedDocument(title)
		if d == "" || namesSeveral(d) {
			return ""
		}
		// "… của Luật Thi đua, khen thưởng số 06/2026/QH16": that số hiệu is
		// the amending law's own, the amended one is named by title
		if n := phraseNumberRe.FindStringIndex(d); n != nil {
			if numberKey(d[n[0]:n[1]]) == numberKey(number) && number != "" {
				d = strings.TrimRight(strings.TrimSuffix(strings.TrimSpace(d[:n[0]]), "số"), " ,")
			} else {
				d = d[:n[1]]
			}
		}
		if strings.TrimSpace(phraseDocTypeRe.ReplaceAllString(d+" ", "")) == "" {
			return ""
		}
		return d
	}
	enclosing := func() string {
		for i := len(headingPath) - 1; i >= 0; i-- {
			if d := headingDocument(headingPath[i]); d != "" {
				return d
			}
			// "Điều 2. Sửa đổi, bổ sung một số điều của các luật có liên
			// quan": which law is in the lead-ins, not the title
			if i == len(headingPath)-1 && amendVerbRe.MatchString(headingPath[i]) && phraseCollectiveRe.MatchString(headingPath[i]) {
				return ""
			}
		}
		return titleDoc()
	}
	var clauseDoc, itemDoc string
	inChunk := func() string {
		if itemDoc != "" {
			return itemDoc
		}
		if clauseDoc != "" {
			return clauseDoc
		}
		return enclosing()
	}
	var out []Amend
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			clauseDoc, itemDoc = "", ""
		}
		if line == "" || strings.HasPrefix(line, ">") || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "|") {
			continue
		}
		switch {
		case phraseClauseLineRe.MatchString(line):
			clauseDoc, itemDoc = leadOrHeading(leadInDocument(line), enclosing(), content, line), ""
		case phraseItemLineRe.MatchString(line):
			itemDoc = leadOrHeading(leadInDocument(line), enclosing(), content, line)
		}
		for _, a := range parsePhraseAmends(line, inChunk) {
			dup := false
			for _, have := range out {
				if have == a {
					dup = true
					break
				}
			}
			if !dup {
				out = append(out, a)
			}
		}
	}
	return out
}

// leadInDocument is the one document a clause or item lead-in amends
// ("Sửa đổi, bổ sung một số điều của Nghị định số 175/2024/NĐ-CP … như
// sau:"); "" for a line that itself changes phrases or names several.
func leadInDocument(line string) string {
	if phraseLeadRe.MatchString(line) || !amendVerbRe.MatchString(line) {
		return ""
	}
	d := amendedDocument(ownLabelRe.ReplaceAllString(line, ""))
	// "… của Thông tư như sau:" names the document itself, no title
	d = strings.TrimSpace(strings.TrimSuffix(d, " như sau"))
	if namesSeveral(d) || strings.TrimSpace(phraseDocTypeRe.ReplaceAllString(d+" ", "")) == "" {
		return ""
	}
	return d
}

// namesSeveral reports a name holding two documents: a second số hiệu, or
// "và" before a document type ("Nghị định số 68/2026/NĐ-CP và Nghị định
// 320/2025/NĐ-CP"), not the "và" of a title ("Luật Quy hoạch đô thị và
// nông thôn", "… giữa NHNN và TCTD").
func namesSeveral(d string) bool {
	return len(phraseNumberRe.FindAllString(d, 2)) > 1 || phraseAndDocRe.MatchString(d)
}

// numberKey is the serial and year of a số hiệu ("48/2013"), so a typo in
// its code ("48/2013/TTBNNPTNT") does not hide it.
func numberKey(s string) string {
	return phraseNumberKeyRe.FindString(s)
}

// leadOrHeading picks the document of a lead-in line: the lead-in's, unless
// it and the enclosing heading name different số hiệu and only the
// heading's appears elsewhere in the chunk — the lead-in has a typo then
// ("Thông tư 48/2018/TT-BNNPTNT" under "Điều 2. Sửa đổi … Thông tư số
// 48/2013/TT-BNNPTNT", the chunk citing 48/2013 further on).
func leadOrHeading(lead, heading, content, leadLine string) string {
	lk, hk := numberKey(lead), numberKey(heading)
	if lk == "" || hk == "" || lk == hk {
		return lead
	}
	// only the lines that change phrases count: the heading line and other
	// lead-ins name the heading's document for their own reasons
	// ("… sửa đổi, bổ sung một số điều của Thông tư số 02/2018/TT-BYT")
	var rest strings.Builder
	for _, l := range strings.Split(content, "\n") {
		if l = strings.TrimSpace(l); l != leadLine && !strings.HasPrefix(l, "#") && !strings.HasPrefix(l, ">") && phraseLeadRe.MatchString(l) {
			rest.WriteString(l + "\n")
		}
	}
	if strings.Contains(rest.String(), hk) && !strings.Contains(rest.String(), lk) {
		return heading
	}
	return lead
}

// phraseItem is one phrase of a clause: rest[start:end] is the phrase
// (or pair), mid the text between a pair's two phrases.
type phraseItem struct {
	from, to, mid string
	start, end    int
}

// phraseSentence reads one sentence clause by clause: each clause has its
// own verb ("Bãi bỏ cụm từ “A”, sửa đổi cụm từ “B” thành “C” tại …") and
// its own provisions — nothing is carried over a ";" or to the next verb,
// a missed amendment being better than a wrong one.
func phraseSentence(text string, enclosing func() string) []Amend {
	// "(đã được sửa đổi, bổ sung bởi …; …)" says where a provision comes
	// from: it must not cut the clause list. An exception in brackets stays
	// for readPhraseTail to see.
	text = stripRemarks(text)
	type clause struct {
		rest   string
		remove bool
		run    int
	}
	var clauses []clause
	var runs []string // the text of each run: a verb and the clauses going on with it
	remove, known := false, false
	for _, cl := range splitClauses(text) {
		var rest string
		switch lead := phraseLeadRe.FindStringIndex(cl); {
		case lead != nil:
			remove, known = phraseRemoveRe.MatchString(cl[lead[0]:]), true
			rest = cl[lead[1]-len(quoteRuneAt(cl, lead[1])):]
			runs = append(runs, "")
		case known && phraseMoreRe.MatchString(strings.TrimSpace(cl)):
			t := strings.TrimSpace(cl)
			i := strings.IndexAny(t, "“\"")
			rest = t[i:]
		default:
			known = false
			continue
		}
		runs[len(runs)-1] += cl + ";"
		clauses = append(clauses, clause{rest: rest, remove: remove, run: len(runs) - 1})
	}
	var out []Amend
	for _, c := range clauses {
		run := runs[c.run]
		// a clause naming no document takes the enclosing one, else the one
		// document its run names ("Thay thế cụm từ … tại Điều 21; cụm từ …
		// tại khoản 7 Điều 22 Thông tư số 49/2021/TT-BGDĐT"); nothing is
		// carried to or from another verb
		named := func() string {
			if d := enclosing(); d != "" {
				return d
			}
			return sentenceDocument(run)
		}
		out = append(out, phraseClause(c.rest, c.remove, named)...)
	}
	return out
}

func phraseClause(rest string, remove bool, enclosing func() string) []Amend {
	var items []phraseItem
	if remove {
		for _, m := range phraseOnlyRe.FindAllStringSubmatchIndex(rest, -1) {
			items = append(items, phraseItem{from: rest[m[2]:m[3]], start: m[0], end: m[1]})
		}
	} else {
		for _, m := range phrasePairRe.FindAllStringSubmatchIndex(rest, -1) {
			it := phraseItem{from: rest[m[2]:m[3]], to: rest[m[6]:m[7]], start: m[0], end: m[1]}
			if m[4] >= 0 {
				it.mid = rest[m[4]:m[5]]
			}
			items = append(items, it)
		}
	}
	// Each phrase takes the provisions after it, up to the next phrase;
	// phrases followed by none share the next list ("“A”, “B” và “C” tại
	// khoản 6 Điều 5"). A pair with the provisions inside ("“A” tại khoản 2
	// Điều 5 bằng cụm từ “B”") keeps those.
	var out []Amend
	var pending []phraseItem
	for i, it := range items {
		tail := rest[it.end:]
		if i+1 < len(items) {
			tail = rest[it.end:items[i+1].start]
		}
		pending = append(pending, it)
		groups, ok := readPhraseTail(tail)
		if len(groups) == 0 && ok && it.mid != "" {
			groups, ok = readPhraseTail(it.mid)
		}
		if !ok {
			// the provisions are in the amending document itself
			return out
		}
		if len(groups) == 0 {
			continue
		}
		for _, g := range groups {
			doc := g.doc
			if doc == "" {
				doc = phraseSpacedNumberRe.ReplaceAllString(enclosing(), "$1-$2")
			}
			for _, p := range pending {
				from := strings.TrimSpace(p.from)
				if from == "" {
					continue
				}
				for _, pr := range g.provs {
					out = append(out, Amend{Document: doc, Article: pr.article, Khoan: pr.khoan, Diem: pr.diem,
						From: from, To: strings.TrimSpace(p.to), Phrase: true})
				}
			}
		}
		pending = nil
	}
	return out
}

// hasException reports a "trừ" / "ngoại trừ" / "không áp dụng" in tail,
// not the "trừ" of "khấu trừ", "giảm trừ" or "bù trừ".
func hasException(tail string) bool {
	for _, m := range phraseExceptRe.FindAllStringIndex(tail, -1) {
		before := strings.ToLower(strings.TrimRight(tail[:m[0]+1], " ,("))
		if !strings.HasSuffix(before, "khấu") && !strings.HasSuffix(before, "giảm") && !strings.HasSuffix(before, "bù") {
			return true
		}
	}
	return false
}

// phraseGroup is a run of provisions and the document named after them
// ("" = none: the enclosing one).
type phraseGroup struct {
	provs []phraseProv
	doc   string
}

// readPhraseTail reads the provisions of a tail, each run with the
// document named right after it ("tại Điều 3 Luật Doanh nghiệp, khoản 1
// Điều 4 Luật Đầu tư" is two runs); a tail with an exception gives none.
// ok is false when the first run's document is the amending one itself
// ("Nghị định này"). A run after a named document that names none of its
// own is left out: it may be that document's or the enclosing one's.
func readPhraseTail(tail string) (groups []phraseGroup, ok bool) {
	// "tại khoản 2 Điều 9, trừ điểm a" or "(trừ điểm a)": what is excepted
	// cannot be told apart reliably, so the amendment is left out — read
	// before the brackets go, so both spellings agree
	if hasException(tail) {
		return nil, true
	}
	// "khoản 2 Điều 9 (được sửa đổi bởi khoản 1 Điều 1 Nghị định số …)":
	// a bracket says where the provision comes from, it is not a target
	s := phraseBracketRe.ReplaceAllString(tail, " ")
	for {
		provs, end := readPhraseTargets(s)
		if len(provs) == 0 {
			return groups, true
		}
		after := s[end:]
		// "Điều 95 và các Phụ lục I, Phụ lục II tại Thông tư số 14/2026/TT-BCA":
		// parts that are no Điều go on with the list up to the document
		if m := phraseOtherItemsRe.FindStringIndex(after); m != nil {
			after = after[m[1]:]
			end += m[1]
		}
		name, nameEnd := phraseDocName(after)
		if strings.HasSuffix(strings.ToLower(name), " này") {
			return groups, len(groups) > 0
		}
		if name == "" {
			if len(groups) > 0 {
				return groups, true
			}
			// the list goes on past what was read ("…, Mẫu số 3 Phụ lục II ban
			// hành kèm theo Nghị định số 102/2018/NĐ-CP") to the document of
			// the whole list; after prose, whose provisions those are cannot
			// be told
			if phraseListGoesOnRe.MatchString(after) {
				if i := docStart(after); i >= 0 {
					if isProse(after[:i]) {
						return nil, true
					}
					if name, _ := phraseDocName(after[i:]); name != "" && !strings.HasSuffix(strings.ToLower(name), " này") {
						return []phraseGroup{{provs: provs, doc: sentenceCase(name)}}, true
					}
					return nil, true
				}
			}
			return []phraseGroup{{provs: provs}}, true
		}
		groups = append(groups, phraseGroup{provs: provs, doc: sentenceCase(name)})
		rest := after[nameEnd:]
		if !phraseGroupSepRe.MatchString(rest) {
			// "Nghị định số 24/2014/NĐ-CP ngày 04 tháng 4 năm 2014 của Chính
			// phủ quy định …; Khoản 5 Điều 7 Nghị định số 37/2014/NĐ-CP": the
			// next run comes after the document's date and subject; after
			// its amendment history ("… số 08/2017/QH14 đã được sửa đổi, bổ
			// sung một số điều theo Luật số 35/2018/QH14; điểm b …"), whose
			// labels are not targets, only after the next ";"
			if !phraseDocDateRe.MatchString(rest) && !phraseHistoryLeadRe.MatchString(rest) {
				return groups, true
			}
			m := phraseNextRunRe.FindStringIndex(rest)
			if m != nil && phraseHistoryRe.MatchString(rest[:m[0]]) {
				m = phraseSemiRunRe.FindStringIndex(rest)
			}
			if m == nil {
				return groups, true
			}
			rest = rest[m[0]:]
		}
		s = trimGroupSep(rest)
	}
}

// docStart is the byte offset of the first document named in s, -1 for
// none. "tên Nghị định" is the title of the document and "của Thông tư và
// …" the document itself, neither one named.
func docStart(s string) int {
	for _, i := range phraseDocStartRe.FindAllStringIndex(s, -1) {
		next := strings.ToLower(strings.TrimSpace(s[i[1]:]))
		if strings.HasSuffix(strings.ToLower(s[:i[0]+1]), "tên ") || strings.HasPrefix(next, "và ") || strings.HasPrefix(next, "hoặc ") {
			continue
		}
		return i[0]
	}
	return -1
}

// isProse reports prose in the text between two list items.
func isProse(gap string) bool {
	return phraseProseRe.MatchString(phraseAttachedRe.ReplaceAllString(gap, " "))
}

// phraseDocName reads the document named at the start of s and the byte
// offset just after it ("" when s names none).
func phraseDocName(s string) (name string, end int) {
	var start int
	if m := phraseDocNumberRe.FindStringSubmatchIndex(s); m != nil &&
		!phraseOtherDocRe.MatchString(phraseDocTypeRe.ReplaceAllString(s[m[2]:m[3]], "")) &&
		!phraseProvLabelRe.MatchString(s[m[2]:m[3]]) {
		start, name = m[2], s[m[2]:m[3]]
	} else if m := phraseDocRe.FindStringSubmatchIndex(s); m != nil {
		start, name = m[2], s[m[2]:m[3]]
	} else {
		return "", 0
	}
	if n := phraseNumberRe.FindStringIndex(name); n != nil {
		name = name[:n[1]]
	} else if e := phraseDocEnd.FindStringIndex(name); e != nil {
		name = name[:e[0]]
	}
	name = strings.TrimRight(strings.TrimSpace(name), " ,.")
	return name, start + len(name)
}

// trimGroupSep drops the separator before the next run's first label.
func trimGroupSep(s string) string {
	s = strings.TrimLeft(s, " ")
	s = strings.TrimLeft(s, ",;")
	s = strings.TrimLeft(s, " ")
	if low := strings.ToLower(s); strings.HasPrefix(low, "và ") {
		s = s[len("và "):]
	}
	return s
}

// stripRemarks blanks the bracketed remarks of text outside quoted
// phrases, except those holding an exception ("(trừ điểm a)").
func stripRemarks(text string) string {
	var b strings.Builder
	inQuote := false
	for i := 0; i < len(text); {
		r, n := utf8.DecodeRuneInString(text[i:])
		switch r {
		case '“':
			inQuote = true
		case '”':
			inQuote = false
		case '"':
			inQuote = !inQuote
		case '(':
			if !inQuote {
				if m := phraseBracketRe.FindStringIndex(text[i:]); m != nil && m[0] == 0 && !hasException(text[i:i+m[1]]) &&
					!strings.ContainsAny(text[i:i+m[1]], "“”\"") {
					b.WriteByte(' ')
					i += m[1]
					continue
				}
			}
		}
		b.WriteString(text[i : i+n])
		i += n
	}
	return b.String()
}

// headingDocument is the document a heading amends: after của / tại /
// vào (amendedDocument), else right after the verb when it has a số hiệu
// ("Điều 1. Sửa đổi, bổ sung Thông tư số 41/2011/TT-BCT ngày …").
func headingDocument(h string) string {
	h = ownLabelRe.ReplaceAllString(h, "")
	if d := amendedDocument(h); d != "" {
		return d
	}
	if m := phraseVerbDocRe.FindStringSubmatch(h); m != nil {
		return sentenceCase(m[1])
	}
	return ""
}

// sentenceDocument is the one document a sentence names by số hiệu,
// outside its quoted phrases ("Thông tư số 49/2021/TT-BGDĐT"); "" when it
// names none, several, or the amending document itself ("… này").
func sentenceDocument(text string) string {
	plain := phraseQuotedRe.ReplaceAllString(text, " ")
	if phraseThisDocRe.MatchString(plain) {
		return ""
	}
	// the numbers after a document's amendment history ("… Quyết định số
	// 33/2003/QĐ-BQP đã được sửa đổi, bổ sung tại … Thông tư số
	// 139/2011/TT-BQP") are of its amenders, not other targets
	if first := phraseNumberRe.FindStringIndex(plain); first != nil {
		if h := phraseAmendedByRe.FindStringIndex(plain[first[1]:]); h != nil {
			plain = plain[:first[1]+h[0]]
		}
	}
	var number string
	var at int
	for _, m := range phraseNumberRe.FindAllStringIndex(plain, -1) {
		n := plain[m[0]:m[1]]
		if number != "" && n != number {
			return ""
		}
		if number == "" {
			number, at = n, m[0]
		}
	}
	if number == "" {
		return ""
	}
	// the title right before the number: "Luật Nhà ở số 27/2023/QH15"
	before := plain[:at]
	if len(before) > 160 {
		before = before[len(before)-160:]
	}
	starts := phraseDocStartRe.FindAllStringIndex(before, -1)
	if len(starts) == 0 {
		return ""
	}
	name := strings.Join(strings.Fields(before[starts[len(starts)-1][0]:]+number), " ")
	if !strings.Contains(name, " ") || phraseProvLabelRe.MatchString(name) || strings.ContainsAny(name, ",;") {
		return ""
	}
	return sentenceCase(name)
}

// nextSegment is text up to its next ";" outside quoted phrases.
func nextSegment(text string) string {
	inQuote := false
	for i, r := range text {
		switch r {
		case '“':
			inQuote = true
		case '”':
			inQuote = false
		case '"':
			inQuote = !inQuote
		case ';':
			if !inQuote {
				return text[:i]
			}
		}
	}
	return text
}

// splitClauses cuts a sentence, outside quoted phrases, at ";" — unless a
// provision label follows, going on with the list — and before another
// verb after "," or "và".
func splitClauses(text string) []string {
	var out []string
	inQuote := false
	start := 0
	for i, r := range text {
		switch r {
		case '“':
			inQuote = true
		case '”':
			inQuote = false
		case '"':
			inQuote = !inQuote
		case ';':
			// "tại khoản 2 Điều 4; khoản 6 Điều 5; Điều 74" is one list
			if !inQuote && !phraseListMoreRe.MatchString(phraseBracketRe.ReplaceAllString(nextSegment(text[i+1:]), " ")) {
				out = append(out, text[start:i])
				start = i + 1
			}
		case ',', ' ':
			// "đã được sửa đổi, bổ sung" is one verb, of a document's history
			if !inQuote && i > start && phraseVerbRe.MatchString(strings.TrimLeft(text[i+1:], " ")) &&
				(r == ',' || strings.HasPrefix(text[i+1:], "và ")) &&
				!strings.HasSuffix(strings.ToLower(strings.TrimRight(text[start:i], " ")), "sửa đổi") {
				out = append(out, text[start:i])
				start = i + 1
			}
		}
	}
	return append(out, text[start:])
}

// splitSentences cuts text at ". " followed by a capital letter, outside
// quoted phrases.
func splitSentences(text string) []string {
	var out []string
	inQuote := false
	start := 0
	rs := []rune(text)
	pos := 0 // byte offset of rs[i]
	for i, r := range rs {
		switch r {
		case '“':
			inQuote = true
		case '”':
			inQuote = false
		case '"':
			inQuote = !inQuote
		case '.':
			if !inQuote && i+2 < len(rs) && rs[i+1] == ' ' && unicode.IsUpper(rs[i+2]) {
				out = append(out, text[start:pos+1])
				start = pos + 2
			}
		}
		pos += utf8.RuneLen(r)
	}
	return append(out, text[start:])
}

// quoteRuneAt is the opening quote ending at byte offset end.
func quoteRuneAt(s string, end int) string {
	if strings.HasSuffix(s[:end], "“") {
		return "“"
	}
	return `"`
}

type phraseProv struct{ article, khoan, diem string }

// readPhraseTargets lists the provisions of "tại điểm a và c khoản 2, các
// khoản 3, 4 Điều 25, khoản 2 Điều 16, Điều 19": điểm collected before a
// khoản belong to it, khoản collected before a Điều belong to it, and a
// bare label right after one continues that list; the khoản before "các
// Điều 12, 13 và 14" belong to each of them. Reading stops at a document
// (its title or number: "Điều 14 Thông tư số 09/2015/TT-BGTVT") and at
// prose between two labels. end is the byte offset just after the last
// provision read.
func readPhraseTargets(s string) (out []phraseProv, end int) {
	if i := phraseNumberRe.FindStringIndex(s); i != nil {
		s = s[:i[0]]
	}
	if i := docStart(s); i >= 0 {
		s = s[:i]
	}
	var khoans []phraseKhoan
	var diems []string
	// carry: the khoản of "khoản 1 các Điều 12, 13", for the bare Điều after
	var carry []phraseKhoan
	seen := map[phraseProv]bool{}
	add := func(p phraseProv) {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	article := func(n string, ks []phraseKhoan) {
		for _, k := range ks {
			if len(k.diems) == 0 {
				add(phraseProv{article: n, khoan: k.n})
			}
			for _, d := range k.diems {
				add(phraseProv{article: n, khoan: k.n, diem: d})
			}
		}
	}
	for i, m := range provMatches(s) {
		if i > 0 && isProse(s[end:m.start]) {
			break
		}
		switch m.kw {
		case "điểm":
			diems = append(diems, strings.ToLower(m.label))
		case "khoản":
			khoans = append(khoans, phraseKhoan{n: m.label, diems: diems})
			diems = nil
		case "điều":
			ks := khoans
			if len(ks) == 0 {
				ks = []phraseKhoan{{diems: diems}}
			}
			switch {
			case m.bare && carry != nil && len(khoans) == 0 && len(diems) == 0:
				ks = carry
			case m.plural:
				carry = ks
			default:
				carry = nil
			}
			article(m.label, ks)
			khoans, diems = nil, nil
		}
		end = m.end
	}
	return out, end
}

type phraseKhoan struct {
	n     string
	diems []string
}

type provMatch struct {
	kw, label  string
	start, end int
	// bare: a label without its keyword ("Điều 3, 4"); plural: the keyword
	// after "các" ("các Điều 12, 13")
	bare, plural bool
}

// provMatches returns the provision labels of s: "Điều 3," but not "điều
// của"; a bare label only right after a label of its kind (a number below
// phraseBareMax after a khoản or Điều, a letter after a điểm) and followed
// by the list going on or ending, so "…, 15 ngày" or "…, 2026" is not one.
// A rejected match does not hide the text after its first rune (",
// khoản 6" is not điểm k).
func provMatches(s string) []provMatch {
	var out []provMatch
	last, lastEnd, lastPlural := "", -1, false
	for pos := 0; pos < len(s); {
		m := phraseProvRe.FindStringSubmatchIndex(s[pos:])
		if m == nil {
			break
		}
		for i := range m {
			if m[i] >= 0 {
				m[i] += pos
			}
		}
		next := m[1]
		accepted := false
		if r, _ := utf8.DecodeRuneInString(s[m[1]:]); m[1] >= len(s) || !(unicode.IsLetter(r) || unicode.IsDigit(r)) {
			bare := lastEnd == m[0] && phraseBareNextRe.MatchString(s[m[1]:])
			switch {
			case m[2] >= 0 && strings.HasSuffix(strings.ToLower(s[:m[0]]), "tên "):
				// "tên Điều 31": the title, not the provisions — not read
				last = ""
			case m[2] >= 0:
				last = strings.ToLower(s[m[2]:m[3]])
				lastPlural = strings.HasSuffix(strings.ToLower(s[:m[0]]), "các ")
				out = append(out, provMatch{kw: last, label: s[m[4]:m[5]], start: m[0], end: m[1], plural: lastPlural})
				accepted = true
			case bare && m[6] >= 0 && (last == "khoản" || last == "điều") && bareNumberOK(s[m[6]:m[7]]):
				out = append(out, provMatch{kw: last, label: s[m[6]:m[7]], start: m[0], end: m[1], bare: true, plural: lastPlural})
				accepted = true
			case bare && m[8] >= 0 && last == "điểm":
				out = append(out, provMatch{kw: last, label: s[m[8]:m[9]], start: m[0], end: m[1], bare: true, plural: lastPlural})
				accepted = true
			}
		}
		if accepted {
			lastEnd = m[1]
		} else {
			_, n := utf8.DecodeRuneInString(s[m[0]:])
			next = m[0] + n
		}
		pos = next
	}
	return out
}

// bareNumberOK: a bare continuation number below phraseBareMax.
func bareNumberOK(label string) bool {
	n := 0
	for _, r := range label {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
		if n >= phraseBareMax {
			return false
		}
	}
	return true
}

// enclosingAmendedDocument is the document the open Điều / khoản amends,
// else the one the title names.
func (b *builder) enclosingAmendedDocument() string {
	for _, i := range []int{b.item, b.clause, b.article} {
		if i >= 0 {
			if d := amendedDocument(ownLabelRe.ReplaceAllString(b.doc.Blocks[i].Text, "")); d != "" {
				return d
			}
		}
	}
	return b.titleAmendedDocument()
}
