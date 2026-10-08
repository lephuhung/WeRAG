package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/docformat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// A profile is made by at most profileMaxCalls thinking-off calls, each
// carrying at most profileCallRunes of section headings and opening
// paragraphs (~8k tokens at ~3.5 runes a token for Vietnamese). A longer
// document is summarised in groups of sections, then merged.
const (
	profileCallRunes   = 28000
	profileMaxCalls    = 3
	profileCallTimeout = 2 * time.Minute
	// profileHeadLines opens every call with the document's first lines, so
	// a source without NĐ30 labels still shows its số ký hiệu and issuer.
	profileHeadLines = 10
)

// profileBlockShapes are the per-section excerpts tried in turn until the
// document fits profileMaxCalls calls: opening paragraphs, runes each.
var profileBlockShapes = []struct{ paras, runes int }{{3, 400}, {2, 250}, {1, 150}, {0, 0}}

// ProfileIdentityFunc reconciles the số hiệu and type a model wrote with
// the document header (docText) and the text the model saw (sourceText):
// the knowledge-base profile's applyLegalIdentity. nil uses a built-in
// header parse.
type ProfileIdentityFunc func(profile *types.KnowledgeProfile, docText, sourceText string) *types.KnowledgeProfile

// profileInput is what a profile is made from, read without a model.
type profileInput struct {
	fileName string
	role     string
	unit     string
	// text is the plain text the hash is taken of: a target's paragraphs
	// in order (empty ones included, so a shift of indexes changes it), a
	// source's stored text.
	text     string
	lines    []profileLine
	sections []types.DocumentProfileSection
	// ident holds the header fields read from the NĐ30 labels.
	ident types.DocumentProfile
}

func (in *profileInput) hash() string {
	sum := sha256.Sum256([]byte(in.unit + "\n" + in.text))
	return hex.EncodeToString(sum[:])
}

// targetProfileInput reads a target's paragraphs and their NĐ30 labels from
// the positional heuristic (no model) for the header fields and sections.
func targetProfileInput(ws *types.DocumentWorkspace, layout *docformat.Layout) *profileInput {
	in := &profileInput{fileName: ws.FileName, role: types.DocumentWorkspaceRoleTarget, unit: types.DocumentProfileUnitParagraph}
	if layout == nil {
		return in
	}
	seg := docformat.Segment(layout)
	texts := make([]string, len(layout.Paragraphs))
	for i, p := range layout.Paragraphs {
		texts[i] = strings.TrimSpace(strings.NewReplacer("\t", " ", "\n", " ").Replace(p.Text))
		if texts[i] == "" {
			continue
		}
		in.lines = append(in.lines, profileLine{Index: i, Text: texts[i], Label: seg.ParaLabels[i]})
	}
	in.text = strings.Join(texts, "\n")
	in.sections = detectProfileSections(in.lines)
	join := func(key string) string {
		c := seg.Comp(key)
		return strings.Join(strings.Fields(strings.ReplaceAll(c.Text, "\n", " ")), " ")
	}
	in.ident.Issuer = headerNoiseFree(draftMarkRe.ReplaceAllString(join("co_quan_ban_hanh"), " "))
	in.ident.Date = profileDate(join("dia_danh_ngay_thang"))
	in.ident.Subject = headerNoiseFree(profileSubject(seg.Comp("trich_yeu").Texts))
	return in
}

// sourceProfileInput reads a source's stored text: its chunks (sections by
// their context headers, else by heading lines) or its lines.
func sourceProfileInput(ws *types.DocumentWorkspace, text *types.DocumentWorkspaceText) *profileInput {
	in := &profileInput{fileName: ws.FileName, role: types.DocumentWorkspaceRoleSource, unit: types.DocumentProfileUnitLine}
	if text == nil {
		return in
	}
	parts, chunked := sourceParts(text)
	var all []string
	headers := make([]string, len(parts))
	for i, p := range parts {
		headers[i] = p.header
		all = append(all, p.text)
		for _, line := range strings.Split(p.text, "\n") {
			if line = strings.TrimSpace(line); line != "" {
				in.lines = append(in.lines, profileLine{Index: i, Text: line})
			}
		}
	}
	in.text = strings.Join(all, "\n")
	if chunked {
		in.unit = types.DocumentProfileUnitChunk
		in.sections = sectionsFromHeaders(headers)
	}
	if len(in.sections) == 0 {
		in.sections = detectProfileSections(in.lines)
	}
	return in
}

var (
	profileDateRe   = regexp.MustCompile(`(?i)ngày\s*(\d{1,2})\s*tháng\s*(\d{1,2})\s*năm\s*(\d{4})`)
	profileMonthRe  = regexp.MustCompile(`(?i)tháng\s*(\d{1,2})\s*năm\s*(\d{4})`)
	profileSubjVVRe = regexp.MustCompile(`(?i)^v/v\.?\s*`)
)

var (
	// draftMarkRe is a "DỰ THẢO" mark; draftMarksRe two or more in a row
	// (a watermark or a stamp read into the header, often glued:
	// "DỰ THẢODỰ THẢO DỰ THẢO").
	draftMarkRe  = regexp.MustCompile(`(?i)dự\s*thảo`)
	draftMarksRe = regexp.MustCompile(`(?i)(dự\s*thảo[\s.,;:]*){2,}`)
)

// headerNoiseFree cleans a header field read without a model: repeated
// "DỰ THẢO" marks go, and a word repeated three times or more in a row is
// kept once. (The issuer drops every "DỰ THẢO" before this: no issuer is
// called that; a trích yếu may be "góp ý dự thảo …".)
func headerNoiseFree(s string) string {
	s = draftMarksRe.ReplaceAllString(s, " ")
	words := strings.Fields(s)
	out := words[:0]
	for i := 0; i < len(words); {
		j := i
		for j < len(words) && strings.EqualFold(words[j], words[i]) {
			j++
		}
		if j-i >= 3 {
			out = append(out, words[i])
		} else {
			out = append(out, words[i:j]...)
		}
		i = j
	}
	return strings.Join(out, " ")
}

// profileDate turns "Hà Nội, ngày 5 tháng 3 năm 2024" into "05/03/2024".
// A line without a full date (a template with the day left blank: "Thành
// phố Huế, ngày   tháng 06 năm 2026") keeps only its "tháng 06 năm 2026",
// without the place; "" when it has no month and year either.
func profileDate(line string) string {
	m := profileDateRe.FindStringSubmatch(line)
	if m == nil {
		if i := strings.Index(line, ","); i >= 0 {
			line = line[i+1:]
		}
		mm := profileMonthRe.FindStringSubmatch(line)
		if mm == nil {
			return ""
		}
		mo, _ := strconv.Atoi(mm[1])
		return fmt.Sprintf("tháng %02d năm %s", mo, mm[2])
	}
	d, _ := strconv.Atoi(m[1])
	mo, _ := strconv.Atoi(m[2])
	return fmt.Sprintf("%02d/%02d/%s", d, mo, m[3])
}

// profileSubject is the trích yếu without its type line ("QUYẾT ĐỊNH") or
// "V/v" prefix.
func profileSubject(lines []string) string {
	var out []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" || vietnamese_legal.DocTypeByName(l) != nil {
			continue
		}
		out = append(out, profileSubjVVRe.ReplaceAllString(l, ""))
	}
	return strings.Join(out, " ")
}

// unitLabel names the indexes of a section range for the model and the
// card ("đoạn" for paragraphs, "chunk" or "dòng" for a source).
func unitLabel(unit string) string {
	switch unit {
	case types.DocumentProfileUnitChunk:
		return "chunk"
	case types.DocumentProfileUnitLine:
		return "dòng"
	}
	return "đoạn"
}

// sectionBlock renders section n (1-based) for the model: its heading and
// range, its first paras lines (runes each) and the sub-headings inside.
func (in *profileInput) sectionBlock(n int, s types.DocumentProfileSection, paras, runes int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[%d] %s (%s %d–%d)\n", n, s.Title, unitLabel(in.unit), s.From, s.To)
	taken := 0
	var subs []string
	for _, l := range in.lines {
		if l.Index < s.From || l.Index > s.To || headerLabels[l.Label] {
			continue
		}
		if l.Index == s.From && strings.HasPrefix(s.Title, l.Text) {
			continue // the heading itself
		}
		if headingLevel(l.Text) != headingNone && len(subs) < 8 {
			subs = append(subs, clipRunes(l.Text, 80))
		}
		if taken < paras {
			b.WriteString(clipRunes(l.Text, runes) + "\n")
			taken++
		}
	}
	if len(subs) > 1 {
		b.WriteString("Gồm: " + strings.Join(subs, "; ") + "\n")
	}
	return b.String()
}

// headBlock is the document's opening lines, given to every call.
func (in *profileInput) headBlock() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Tệp: %s (%s)\n", in.fileName, map[bool]string{true: "tài liệu nguồn", false: "văn bản làm việc"}[in.role == types.DocumentWorkspaceRoleSource])
	if in.ident.Issuer != "" || in.ident.Date != "" || in.ident.Subject != "" {
		fmt.Fprintf(&b, "Đọc tự động từ phần đầu: cơ quan ban hành %q, ngày %q, trích yếu %q\n", in.ident.Issuer, in.ident.Date, in.ident.Subject)
	}
	b.WriteString("Đầu văn bản:\n")
	for i, l := range in.lines {
		if i >= profileHeadLines {
			break
		}
		b.WriteString(clipRunes(l.Text, 200) + "\n")
	}
	return b.String()
}

// profileGroups packs the section blocks into at most profileMaxCalls
// groups of at most profileCallRunes, trying shorter excerpts until they
// fit; sections that still do not fit are left out of the calls (they
// keep their title and range, without a summary). Each group lists the
// 1-based section numbers it holds.
func (in *profileInput) profileGroups() (groups [][]int, blocks []string) {
	head := utf8.RuneCountInString(in.headBlock())
	room := profileCallRunes - head - 1500 // instructions
	for _, shape := range profileBlockShapes {
		blocks = make([]string, len(in.sections))
		for i, s := range in.sections {
			blocks[i] = in.sectionBlock(i+1, s, shape.paras, shape.runes)
		}
		groups = packBlocks(blocks, room)
		if len(groups) <= profileMaxCalls {
			return groups, blocks
		}
	}
	return groups[:profileMaxCalls], blocks
}

// packBlocks groups consecutive blocks greedily under room runes each.
func packBlocks(blocks []string, room int) [][]int {
	var groups [][]int
	var cur []int
	used := 0
	for i, b := range blocks {
		n := utf8.RuneCountInString(b)
		if len(cur) > 0 && used+n > room {
			groups = append(groups, cur)
			cur, used = nil, 0
		}
		cur = append(cur, i+1)
		used += n
	}
	if len(cur) > 0 {
		groups = append(groups, cur)
	}
	return groups
}

const profileSystemPrompt = `Bạn lập hồ sơ (thẻ tóm tắt) cho một văn bản tiếng Việt để trợ lý soạn thảo biết văn bản nói gì và tra đúng mục. Bạn chỉ thấy tiêu đề các mục và vài đoạn đầu mỗi mục.

Trả về DUY NHẤT một đối tượng JSON:
{
  "document_number": "số ký hiệu của chính văn bản, ví dụ 45/KH-UBND; rỗng nếu không có",
  "issuer": "cơ quan ban hành",
  "date": "ngày ban hành dạng dd/mm/yyyy, rỗng nếu không rõ",
  "doc_type": "tên loại văn bản, ví dụ Quyết định, Kế hoạch, Báo cáo, Công văn",
  "subject": "trích yếu: văn bản về việc gì",
  "gist": "một câu nêu nội dung chính",
  "key_points": ["3-5 ý chính, mỗi ý một câu ngắn có số liệu nếu có"],
  "sections": [{"n": 1, "summary": "một câu tóm tắt mục [1]"}],
  "topics": ["3-5 từ khóa chủ đề"],
  "entities": {"units": ["cơ quan, đơn vị được nêu"], "cited_numbers": ["số ký hiệu văn bản khác được viện dẫn"], "dates": ["mốc thời gian"], "figures": ["số liệu quan trọng kèm đơn vị và mục chứa nó"]},
  "typical_questions": ["2-3 câu hỏi người dùng có thể hỏi về văn bản này"]
}

Quy tắc: chỉ dùng thông tin có trong văn bản, không bịa số ký hiệu hay số liệu; "n" là số trong [ ] của mục; mỗi mục được đưa ra có một tóm tắt; với tài liệu nguồn có bảng số liệu, ghi trong "figures" bảng đó nói về gì và nằm ở mục nào. Viết tiếng Việt.`

// profileUserMessage is one call's input: the head and the group's blocks.
func (in *profileInput) profileUserMessage(group []int, blocks []string, part, parts int) string {
	var b strings.Builder
	b.WriteString(in.headBlock())
	if parts > 1 {
		fmt.Fprintf(&b, "\nVăn bản dài: đây là phần %d/%d (các mục [%d]–[%d] trong %d mục). Tóm tắt các mục của phần này; các trường chung viết theo những gì phần này cho thấy.\n",
			part, parts, group[0], group[len(group)-1], len(in.sections))
	}
	fmt.Fprintf(&b, "\nCác mục (%s là chỉ số %s):\n", unitLabel(in.unit), unitLabel(in.unit))
	for _, n := range group {
		b.WriteString(blocks[n-1])
		b.WriteString("\n")
	}
	return b.String()
}

// profileReply is a model answer, parsed leniently.
type profileReply struct {
	DocumentNumber   string
	Issuer           string
	Date             string
	DocType          string
	Subject          string
	Gist             string
	KeyPoints        []string
	Summaries        map[int]string
	Topics           []string
	Entities         types.DocumentProfileEntities
	TypicalQuestions []string
}

var profileFenceRe = regexp.MustCompile("^```(?:json)?\\s*|\\s*```$")

// parseProfileReply extracts the JSON object of a reply, tolerating code
// fences, surrounding prose, a string where a list was asked and sections
// given as a map ("1": "…") or a list of {n|id|index, summary}.
func parseProfileReply(text string) (*profileReply, error) {
	t := profileFenceRe.ReplaceAllString(strings.TrimSpace(text), "")
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(t), &raw); err != nil {
		start, end := strings.Index(t, "{"), strings.LastIndex(t, "}")
		if start < 0 || end <= start {
			return nil, errors.New("no JSON object in model reply")
		}
		if err := json.Unmarshal([]byte(t[start:end+1]), &raw); err != nil {
			return nil, fmt.Errorf("model reply is not valid JSON: %w", err)
		}
	}
	str := func(key string) string {
		var s string
		if v, ok := raw[key]; ok && json.Unmarshal(v, &s) == nil {
			return strings.TrimSpace(s)
		}
		return ""
	}
	r := &profileReply{
		DocumentNumber: str("document_number"), Issuer: str("issuer"), Date: str("date"),
		DocType: str("doc_type"), Subject: str("subject"), Gist: str("gist"),
		KeyPoints: jsonStrings(raw["key_points"]), Topics: jsonStrings(raw["topics"]),
		TypicalQuestions: jsonStrings(raw["typical_questions"]), Summaries: map[int]string{},
	}
	var ent map[string]json.RawMessage
	if v, ok := raw["entities"]; ok && json.Unmarshal(v, &ent) == nil {
		r.Entities = types.DocumentProfileEntities{
			Units: jsonStrings(ent["units"]), CitedNumbers: jsonStrings(ent["cited_numbers"]),
			Dates: jsonStrings(ent["dates"]), Figures: jsonStrings(ent["figures"]),
		}
	}
	if v, ok := raw["sections"]; ok {
		var list []map[string]any
		var byKey map[string]any
		switch {
		case json.Unmarshal(v, &list) == nil:
			for i, x := range list {
				n := i + 1
				for _, k := range []string{"n", "id", "index", "section"} {
					if num, ok := anyInt(x[k]); ok {
						n = num
						break
					}
				}
				if s, _ := x["summary"].(string); strings.TrimSpace(s) != "" {
					r.Summaries[n] = strings.TrimSpace(s)
				}
			}
		case json.Unmarshal(v, &byKey) == nil:
			for k, x := range byKey {
				n, err := strconv.Atoi(strings.Trim(k, "[] "))
				if s, _ := x.(string); err == nil && strings.TrimSpace(s) != "" {
					r.Summaries[n] = strings.TrimSpace(s)
				}
			}
		}
	}
	return r, nil
}

// jsonStrings reads a list of strings, or one string as a one-item list.
func jsonStrings(v json.RawMessage) []string {
	if len(v) == 0 {
		return nil
	}
	var list []any
	if json.Unmarshal(v, &list) == nil {
		var out []string
		for _, x := range list {
			switch s := x.(type) {
			case string:
				out = append(out, s)
			case float64:
				out = append(out, strconv.FormatFloat(s, 'f', -1, 64))
			}
		}
		return out
	}
	var s string
	if json.Unmarshal(v, &s) == nil && strings.TrimSpace(s) != "" {
		return []string{s}
	}
	return nil
}

func anyInt(v any) (int, bool) {
	switch x := v.(type) {
	case float64:
		return int(x), true
	case string:
		n, err := strconv.Atoi(strings.Trim(x, "[] "))
		return n, err == nil
	}
	return 0, false
}

// mergeProfileReplies merges the answers of the calls of one document:
// identity fields and the gist from the first answer that has them, lists
// interleaved (each call contributes before any gives a second item) and
// deduplicated, section summaries by number.
func mergeProfileReplies(replies []*profileReply) *profileReply {
	out := &profileReply{Summaries: map[int]string{}}
	first := func(dst *string, v string) {
		if *dst == "" {
			*dst = v
		}
	}
	interleave := func(get func(*profileReply) []string) []string {
		var all []string
		for k := 0; ; k++ {
			more := false
			for _, r := range replies {
				if l := get(r); k < len(l) {
					all = append(all, l[k])
					more = true
				}
			}
			if !more {
				return all
			}
		}
	}
	for _, r := range replies {
		first(&out.DocumentNumber, r.DocumentNumber)
		first(&out.Issuer, r.Issuer)
		first(&out.Date, r.Date)
		first(&out.DocType, r.DocType)
		first(&out.Subject, r.Subject)
		first(&out.Gist, r.Gist)
		for n, s := range r.Summaries {
			if _, ok := out.Summaries[n]; !ok {
				out.Summaries[n] = s
			}
		}
	}
	out.KeyPoints = interleave(func(r *profileReply) []string { return r.KeyPoints })
	out.Topics = interleave(func(r *profileReply) []string { return r.Topics })
	out.TypicalQuestions = interleave(func(r *profileReply) []string { return r.TypicalQuestions })
	out.Entities = types.DocumentProfileEntities{
		Units:        interleave(func(r *profileReply) []string { return r.Entities.Units }),
		CitedNumbers: interleave(func(r *profileReply) []string { return r.Entities.CitedNumbers }),
		Dates:        interleave(func(r *profileReply) []string { return r.Entities.Dates }),
		Figures:      interleave(func(r *profileReply) []string { return r.Entities.Figures }),
	}
	return out
}

// generateDocumentProfile makes the profile of in with llm (thinking off):
// one call, or one per group of sections for a long document, merged. The
// header fields read without a model win over the model's; the số hiệu and
// type go through identity. Metadata (hash, model, status) is the caller's.
func generateDocumentProfile(ctx context.Context, llm docformat.Completer, in *profileInput, identity ProfileIdentityFunc) (*types.DocumentProfile, error) {
	if len(in.sections) == 0 {
		return nil, errors.New("document has no text")
	}
	groups, blocks := in.profileGroups()
	var replies []*profileReply
	var seen strings.Builder
	var lastErr error
	for k, g := range groups {
		msg := in.profileUserMessage(g, blocks, k+1, len(groups))
		seen.WriteString(msg)
		callCtx, cancel := context.WithTimeout(ctx, profileCallTimeout)
		text, err := llm.Complete(callCtx, []docformat.Message{
			{Role: "system", Content: profileSystemPrompt},
			{Role: "user", Content: msg},
		})
		cancel()
		if err == nil {
			var r *profileReply
			if r, err = parseProfileReply(text); err == nil {
				replies = append(replies, r)
				continue
			}
		}
		lastErr = err
		if ctx.Err() != nil {
			break
		}
	}
	if len(replies) == 0 {
		if lastErr == nil {
			lastErr = errors.New("no model answer")
		}
		return nil, lastErr
	}
	m := mergeProfileReplies(replies)

	p := &types.DocumentProfile{
		DocumentNumber: m.DocumentNumber, Issuer: m.Issuer, Date: m.Date, DocType: m.DocType,
		Subject: m.Subject, Gist: m.Gist, KeyPoints: m.KeyPoints, Topics: m.Topics,
		TypicalQuestions: m.TypicalQuestions, Unit: in.unit,
	}
	ent := m.Entities
	p.Entities = &ent
	for i, s := range in.sections {
		s.Summary = m.Summaries[i+1]
		p.Sections = append(p.Sections, s)
	}
	if in.ident.Issuer != "" {
		p.Issuer = in.ident.Issuer
	}
	if in.ident.Date != "" {
		p.Date = in.ident.Date
	}
	if in.ident.Subject != "" {
		p.Subject = in.ident.Subject
	}
	if identity == nil {
		identity = defaultProfileIdentity
	}
	header := in.text
	if len(header) > 2500 {
		header = header[:2500]
	}
	kp := identity(&types.KnowledgeProfile{DocType: p.DocType, DocumentNumber: p.DocumentNumber, Gist: p.Gist}, header, seen.String())
	p.DocumentNumber, p.DocType = "", ""
	if kp != nil {
		p.DocumentNumber, p.DocType, p.DocTypeCode = kp.DocumentNumber, kp.DocType, kp.DocTypeCode
	}
	return p.Normalize(), nil
}

// defaultProfileIdentity is the header parse used without the service's
// applyLegalIdentity: the "Số:" line and the tên loại line win, a model's
// number is kept only when it occurs in the text it saw.
func defaultProfileIdentity(p *types.KnowledgeProfile, docText, sourceText string) *types.KnowledgeProfile {
	if n := vietnamese_legal.NormalizeOwnDocumentNumber(vietnamese_legal.RecoverDocumentNumber(docText)); n != "" {
		p.DocumentNumber = n
	} else if n := vietnamese_legal.NormalizeOwnDocumentNumber(p.DocumentNumber); n == "" ||
		!strings.Contains(strings.Join(strings.Fields(sourceText), ""), n) {
		p.DocumentNumber = ""
	}
	if d := vietnamese_legal.DocTypeBySlug(vietnamese_legal.DetectDocType(docText).Slug); d != nil {
		p.DocType = d.Name
	}
	return p.Normalize()
}
