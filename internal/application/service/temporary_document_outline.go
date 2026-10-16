package service

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/infrastructure/chunker"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

// A chat attachment too large for the prompt is cut by its outline, the way
// PageIndex does it: the document's headings are listed as a tree with ids,
// the answering model picks the sections the question needs in one short
// call, and those sections go into the prompt with the outline in front of
// them. No LLM builds the outline. Attachments are mostly local documents
// (báo cáo, kế hoạch, tờ trình, biên bản, biểu mẫu), so the headings come
// from the text itself: Word heading styles, numbering (I. / A. / 1. /
// 1.1.), legal parts (Chương, Mục, Điều), Phụ lục and Mẫu số, and short
// upper-case or bold lines. A document without such structure, or when the
// pick fails, gets the keyword selection as before.

const (
	// attachmentOutlineMinSections: with fewer sections an outline says
	// nothing the keyword selection does not already find.
	attachmentOutlineMinSections = 3
	// attachmentOutlineMaxSections: deeper heading levels join the outline
	// only while it stays this short.
	attachmentOutlineMaxSections = 60
	// attachmentOutlineMaxOwnShare: when one section's own text is more
	// than this share of the document, the headings do not divide it and
	// the outline is not used.
	attachmentOutlineMaxOwnShare = 0.6
	// attachmentOutlinePickerRunes caps the outline the picker reads.
	attachmentOutlinePickerRunes = 24000
	// attachmentOutlinePromptRunes caps the outline shown to the answering
	// model; a longer one is cut and says so.
	attachmentOutlinePromptRunes = 6000
	// attachmentOutlineTitleRunes clips one heading in the outline.
	attachmentOutlineTitleRunes = 90
	// attachmentPickMaxSections bounds how many sections one pick may name.
	attachmentPickMaxSections = 8
	attachmentPickTimeout     = 25 * time.Second
	// A wrong pick must not push out what the keyword selection finds:
	// this share of the budget and of the prompt parts is kept for the
	// chunks with the most question words.
	attachmentKeywordReservePercent = 30
	attachmentKeywordReserveParts   = 4
)

// attachmentSection is a heading of the outline with everything under it:
// Start/End are rune offsets of the section with its subsections in the
// document content, First/Last the chunks that overlap it (inclusive),
// Tokens its approximate size.
type attachmentSection struct {
	ID          int
	Depth       int
	Title       string
	Start, End  int
	OwnEnd      int // where the first subsection starts
	First, Last int
	Tokens      int
}

// Heading kinds. Phụ lục and forms always open a top-level section; the
// other kinds take levels in the order they first appear in the document,
// as Word numbering does; upper-case and bold lines, the weakest signal,
// come below every numbered kind.
const (
	kindTop    = "top"
	kindUpper  = "upper"
	kindBold   = "bold"
	headingMax = 160 // runes; longer lines are body text
)

var (
	// \b is ASCII-only in Go: after a Vietnamese letter ("số") it never
	// matches, so those alternatives end in (\s|$) instead.
	outlineTopRe     = regexp.MustCompile(`(?i)^(phụ\s+lục\b|mẫu\s+số(\s|$)|biểu\s+(mẫu|số)(\s|$)|bm\s*-?\s*\d+\b)`)
	outlinePartRe    = regexp.MustCompile(`(?i)^phần\s+(thứ\s+\S+|[ivxlc]+|\d+)\b`)
	outlineChapterRe = regexp.MustCompile(`(?i)^chương\s+([ivxlc]+|\d+)\b`)
	outlineSectionRe = regexp.MustCompile(`(?i)^mục\s+(\d+|[ivxlc]+)\b`)
	outlineArticleRe = regexp.MustCompile(`^Điều\s+\d+[a-zđ]?\s*[.:]`)
	outlineRomanRe   = regexp.MustCompile(`^[IVXLC]{1,6}\s*[.)\-–]\s*\S`)
	outlineAlphaRe   = regexp.MustCompile(`^[A-ZĐ]\s*[.)]\s+\S`)
	outlineDottedRe  = regexp.MustCompile(`^(\d{1,2}(\.\d{1,2})+)\.?\s+\p{L}`)
	outlineMeasureRe = regexp.MustCompile(`(?i)^\d+([.,]\d+)*\s*(mm|cm|m|km|kg|g|ha|m2|m²|%|đồng|triệu|tỷ)(\P{L}|$)`)
	outlineNumberRe  = regexp.MustCompile(`^\d{1,2}\s*[.)]\s*\p{L}`)
	// a signer's name in bold ("Trần Minh Quang"): headings are written in
	// sentence case, names capitalise every word
	personNameRe    = regexp.MustCompile(`^(\p{Lu}\p{Ll}*\s+){1,4}\p{Lu}\p{Ll}*$`)
	markdownHeadRe  = regexp.MustCompile(`^(#{1,6})\s+(.+)$`)
	markdownMarksRe = regexp.MustCompile(`^#{1,6}\s*|\*\*|__`)
	// the quốc hiệu, tiêu ngữ, addressees and signature block of an
	// administrative document are upper-case or bold but not headings
	outlineNoiseRe = regexp.MustCompile(`(?i)^(cộng\s+hòa\s+xã\s+hội|độc\s+lập\s*[-–]|trang\s+\d+|ghi\s+chú\s*:?$|nơi\s+nhận|kính\s+gửi|số\s*:|(tm|kt|tl|tuq|q)\.\s|(phó\s+)?(giám\s+đốc|chủ\s+tịch|trưởng\s+(phòng|ban)|chánh\s+văn\s+phòng|thủ\s+trưởng)\b)`)
)

type outlineHeading struct {
	kind  string
	title string
	start int // rune offset of the line
}

// headingKind classifies one line; "" is body text.
func headingKind(raw string) (kind, title string) {
	line := strings.TrimSpace(raw)
	if line == "" || strings.HasPrefix(line, "|") {
		return "", ""
	}
	if m := markdownHeadRe.FindStringSubmatch(line); m != nil {
		title = strings.TrimSpace(markdownMarksRe.ReplaceAllString(m[2], ""))
		if title == "" || outlineNoiseRe.MatchString(title) {
			return "", ""
		}
		if outlineTopRe.MatchString(title) {
			return kindTop, title
		}
		return fmt.Sprintf("md%d", len(m[1])), title
	}
	bold := strings.HasPrefix(line, "**") && strings.HasSuffix(line, "**") && len(line) > 4
	title = strings.TrimSpace(markdownMarksRe.ReplaceAllString(line, ""))
	n := utf8.RuneCountInString(title)
	if n < 2 || outlineNoiseRe.MatchString(title) {
		return "", ""
	}
	// a Điều heading may run long ("Điều 1. Ban hành kèm theo …")
	if outlineArticleRe.MatchString(title) {
		return "dieu", title
	}
	if n > headingMax {
		return "", ""
	}
	switch {
	case outlineTopRe.MatchString(title):
		return kindTop, title
	case outlinePartRe.MatchString(title):
		return "phan", title
	case outlineChapterRe.MatchString(title):
		return "chuong", title
	case outlineSectionRe.MatchString(title):
		return "muc", title
	}
	// numbered lines are headings only when short: "1. Giao Sở Tài chính
	// chủ trì, phối hợp …" is a paragraph of a list
	if n <= 110 {
		switch {
		case outlineRomanRe.MatchString(title):
			return "roman", title
		case outlineAlphaRe.MatchString(title):
			return "alpha", title
		case outlineDottedRe.MatchString(title) && !outlineMeasureRe.MatchString(title):
			depth := strings.Count(outlineDottedRe.FindStringSubmatch(title)[1], ".") + 1
			return fmt.Sprintf("dot%d", depth), title
		case outlineNumberRe.MatchString(title):
			return "num", title
		}
	}
	if n <= 120 && !strings.HasSuffix(title, ":") {
		upper := n >= 6 && strings.ToUpper(title) == title && strings.ToLower(title) != title
		switch {
		case upper:
			return kindUpper, title
		case bold && !personNameRe.MatchString(title):
			return kindBold, title
		}
	}
	return "", ""
}

// detectOutlineHeadings returns the heading lines of content in order.
// An upper-case or bold line right after a heading (blank lines between at
// most) continues its title: "Chương II" + "QUY ĐỊNH CHUNG", or a title
// written over several lines.
func detectOutlineHeadings(content string) []outlineHeading {
	var out []outlineHeading
	offset := 0
	afterHeading := false
	for _, line := range strings.SplitAfter(content, "\n") {
		n := utf8.RuneCountInString(line)
		if strings.TrimSpace(line) == "" {
			offset += n
			continue
		}
		kind, title := headingKind(line)
		switch {
		case kind == "":
			afterHeading = false
		case afterHeading && (kind == kindUpper || kind == kindBold):
			last := &out[len(out)-1]
			last.title += " " + title
		default:
			out = append(out, outlineHeading{kind: kind, title: title, start: offset})
			afterHeading = true
		}
		offset += n
	}
	return out
}

func hasStrongHeading(headings []outlineHeading) bool {
	for _, h := range headings {
		if h.kind != kindUpper && h.kind != kindBold {
			return true
		}
	}
	return false
}

// headingLevels gives each kind its depth: top-level kinds 0, then the
// legal parts (Phần > Chương > Mục > Điều), then the other numbered kinds in
// order of first appearance (a dotted "1.1." always below its "1."), then
// upper-case and bold lines.
// The second value says whether upper-case and bold lines head the lists.
func headingLevels(headings []outlineHeading) (map[string]int, bool) {
	levels := map[string]int{kindTop: 0}
	var order []string
	seen := map[string]bool{kindTop: true, kindUpper: true, kindBold: true}
	// the legal parts nest in a fixed order, whichever appears first
	for _, kind := range []string{"phan", "chuong", "muc", "dieu"} {
		for _, h := range headings {
			if h.kind == kind {
				seen[kind] = true
				order = append(order, kind)
				break
			}
		}
	}
	for _, h := range headings {
		if !seen[h.kind] {
			seen[h.kind] = true
			order = append(order, h.kind)
		}
	}
	// "num" sits above "dot2" and "dot2" above "dot3" wherever they first
	// appear: the numbered kinds keep their slots, sorted by depth
	var slots []int
	var numbered []string
	for i, kind := range order {
		if dottedRank(kind) > 0 {
			slots = append(slots, i)
			numbered = append(numbered, kind)
		}
	}
	sort.Slice(numbered, func(i, j int) bool { return dottedRank(numbered[i]) < dottedRank(numbered[j]) })
	for i, slot := range slots {
		order[slot] = numbered[i]
	}
	// upper-case and bold lines go below the numbered kinds, unless they
	// head the lists of one: a list that restarts at 1 right after such a
	// line (a collection of forms, each numbered from 1) belongs to it
	weakAt := len(order)
	for i, kind := range order {
		if weakLinesParent(headings, kind) {
			weakAt = i
			break
		}
	}
	next := 1
	for i, kind := range order {
		if i == weakAt {
			levels[kindUpper], levels[kindBold] = next, next+1
			next += 2
		}
		levels[kind] = next
		next++
	}
	if weakAt == len(order) {
		levels[kindUpper], levels[kindBold] = next, next+1
	}
	return levels, weakAt < len(order)
}

var listStartRe = regexp.MustCompile(`^(1|I|A)\s*[.)]`)

// weakLinesParent: the lists of kind restart (at 1, I. or A.) at least
// twice, and mostly right after an upper-case or bold line.
func weakLinesParent(headings []outlineHeading, kind string) bool {
	restarts, afterWeak := 0, 0
	seen := false
	for i, h := range headings {
		if h.kind != kind {
			continue
		}
		if seen && listStartRe.MatchString(h.title) {
			restarts++
			if i > 0 && (headings[i-1].kind == kindUpper || headings[i-1].kind == kindBold) {
				afterWeak++
			}
		}
		seen = true
	}
	return restarts >= 2 && afterWeak*2 >= restarts
}

func dottedRank(kind string) int {
	if kind == "num" {
		return 1
	}
	if strings.HasPrefix(kind, "dot") {
		n, _ := strconv.Atoi(strings.TrimPrefix(kind, "dot"))
		return n
	}
	return 0
}

// buildAttachmentSections builds the outline of content and maps its
// sections to the chunks. Levels are added from the top while the outline
// stays within attachmentOutlineMaxSections. Text before the first heading
// is "Mở đầu". Nil when the document has no usable structure: fewer than
// attachmentOutlineMinSections sections, or one section holding most of
// the text.
func buildAttachmentSections(content string, chunks []types.TemporaryDocumentChunk) []attachmentSection {
	headings := detectOutlineHeadings(content)
	// the title block ("BÁO CÁO" + trích yếu) is upper-case or bold lines
	// before the first numbered or legal heading: it names Mở đầu, it does
	// not open sections — unless such lines head the lists (forms), when
	// the first one is the first form's title
	intro := "Mở đầu"
	_, weakParents := headingLevels(headings)
	for len(headings) > 0 && !weakParents && (headings[0].kind == kindUpper || headings[0].kind == kindBold) && hasStrongHeading(headings) {
		if intro == "Mở đầu" {
			intro += ": " + headings[0].title
		}
		headings = headings[1:]
	}
	if len(headings) == 0 {
		return nil
	}
	levels, _ := headingLevels(headings)
	countAt := map[int]int{}
	for _, h := range headings {
		countAt[levels[h.kind]]++
	}
	maxLevel, total := -1, 0
	for lv := 0; lv <= len(levels)+1; lv++ {
		if countAt[lv] == 0 {
			continue
		}
		if total > 0 && total+countAt[lv] > attachmentOutlineMaxSections {
			break
		}
		total += countAt[lv]
		maxLevel = lv
	}

	runes := []rune(content)
	type node struct {
		title        string
		level, start int
		leaf         bool // Mở đầu ends where the first heading starts
	}
	var nodes []node
	if first := headings[0].start; strings.TrimSpace(string(runes[:first])) != "" {
		nodes = append(nodes, node{title: intro, level: 0, start: 0, leaf: true})
	}
	for _, h := range headings {
		if lv := levels[h.kind]; lv <= maxLevel {
			nodes = append(nodes, node{title: h.title, level: lv, start: h.start})
		}
	}
	if len(nodes) < attachmentOutlineMinSections {
		return nil
	}

	// depth in the tree: how many open headings of a lower level enclose it
	sections := make([]attachmentSection, len(nodes))
	var stack []int
	lang := chunker.DetectLanguage(content)
	maxOwn := 0
	for i, n := range nodes {
		for len(stack) > 0 && nodes[stack[len(stack)-1]].level >= n.level {
			stack = stack[:len(stack)-1]
		}
		ownEnd := len(runes)
		if i+1 < len(nodes) {
			ownEnd = nodes[i+1].start
		}
		end := ownEnd
		if !n.leaf {
			end = len(runes)
			for j := i + 1; j < len(nodes); j++ {
				if nodes[j].level <= n.level {
					end = nodes[j].start
					break
				}
			}
		}
		if own := ownEnd - n.start; own > maxOwn {
			maxOwn = own
		}
		sections[i] = attachmentSection{
			ID: i + 1, Depth: len(stack), Title: n.title, Start: n.start, End: end, OwnEnd: ownEnd,
			First: -1, Last: -1,
			Tokens: chunker.ApproxTokenCount(string(runes[n.start:end]), lang),
		}
		if !n.leaf {
			stack = append(stack, i)
		}
	}
	if float64(maxOwn) > attachmentOutlineMaxOwnShare*float64(len(runes)) {
		return nil
	}
	for i := range sections {
		for c, chunk := range chunks {
			if chunk.Start < sections[i].End && chunk.End > sections[i].Start {
				if sections[i].First < 0 {
					sections[i].First = c
				}
				sections[i].Last = c
			}
		}
	}
	return sections
}

// renderAttachmentOutline lists the sections as a tree, indented by depth:
// "[S3] II. KẾT QUẢ THỰC HIỆN (~2400 token)" then "  [S4] 1. Công tác văn
// thư (~900 token)". A section's size includes its subsections. The ids
// carry an S so the picker does not take "Điều 6" for section 6.
// maxRunes > 0 cuts the list; the second value says whether it was cut.
func renderAttachmentOutline(sections []attachmentSection, maxRunes int) (string, bool) {
	var b strings.Builder
	for _, s := range sections {
		line := fmt.Sprintf("%s[S%d] %s (~%d token)\n", strings.Repeat("  ", s.Depth), s.ID, clipOutlineTitle(s.Title), s.Tokens)
		if maxRunes > 0 && utf8.RuneCountInString(b.String())+utf8.RuneCountInString(line) > maxRunes {
			return strings.TrimRight(b.String(), "\n"), true
		}
		b.WriteString(line)
	}
	return strings.TrimRight(b.String(), "\n"), false
}

func clipOutlineTitle(s string) string {
	r := []rune(s)
	if len(r) <= attachmentOutlineTitleRunes {
		return s
	}
	return strings.TrimSpace(string(r[:attachmentOutlineTitleRunes])) + "…"
}

// attachmentPick is the picker's reply: the section ids to read, or Whole
// when the question is about the document as a whole (summary, overview).
type attachmentPick struct {
	Sections []int
	Whole    bool
}

const attachmentPickerPrompt = `Bạn chọn các phần của một tài liệu đính kèm cần đọc để trả lời câu hỏi.
Bạn nhận mục lục của tài liệu: mỗi phần có mã [S…], tiêu đề và độ dài. Mã phần không phải số Điều.
Chọn ít phần nhất đủ để trả lời, tối đa %d phần, phần quan trọng nhất trước.
Nếu câu hỏi hỏi về toàn bộ tài liệu (tóm tắt, nội dung chính, bố cục), đặt "whole": true.
Nếu không phần nào liên quan, trả về danh sách rỗng.
Chỉ trả lời một đối tượng JSON, không giải thích: {"sections": ["S3", "S12"], "whole": false}`

// pickAttachmentSections asks modelID which sections of the outline the
// question needs. Ids not in the outline are dropped.
func (s *temporaryDocumentService) pickAttachmentSections(
	ctx context.Context, modelID, fileName, query, outline string, sectionCount int,
) (attachmentPick, error) {
	var pick attachmentPick
	chatModel, err := s.modelService.GetChatModel(ctx, modelID)
	if err != nil {
		return pick, err
	}
	ctx, cancel := context.WithTimeout(ctx, attachmentPickTimeout)
	defer cancel()
	thinking := false
	resp, err := chatModel.Chat(ctx, []chat.Message{
		{Role: "system", Content: fmt.Sprintf(attachmentPickerPrompt, attachmentPickMaxSections)},
		{Role: "user", Content: fmt.Sprintf("Tài liệu: %s\n\nMục lục:\n%s\n\nCâu hỏi: %s", fileName, outline, query)},
	}, &chat.ChatOptions{Temperature: 0, MaxTokens: 200, Thinking: &thinking})
	if err != nil {
		return pick, err
	}
	if resp == nil {
		return pick, fmt.Errorf("empty pick reply")
	}
	return parseAttachmentPick(resp.Content, sectionCount)
}

func parseAttachmentPick(reply string, sectionCount int) (attachmentPick, error) {
	var pick attachmentPick
	start, end := strings.Index(reply, "{"), strings.LastIndex(reply, "}")
	if start < 0 || end <= start {
		return pick, fmt.Errorf("no JSON in pick reply %q", reply)
	}
	var raw struct {
		Sections []json.RawMessage `json:"sections"`
		Whole    bool              `json:"whole"`
	}
	if err := json.Unmarshal([]byte(reply[start:end+1]), &raw); err != nil {
		return pick, fmt.Errorf("bad pick reply %q: %w", reply, err)
	}
	pick.Whole = raw.Whole
	seen := map[int]bool{}
	for _, item := range raw.Sections {
		// "S12", "s12" or a bare 12
		id, err := strconv.Atoi(strings.TrimLeft(strings.Trim(string(item), `" `), "Ss"))
		if err != nil || id < 1 || id > sectionCount || seen[id] || len(pick.Sections) >= attachmentPickMaxSections {
			continue
		}
		seen[id] = true
		pick.Sections = append(pick.Sections, id)
	}
	return pick, nil
}

// packPickedSections fills budget with the chunks of the picked sections,
// in pick order; a section larger than what is left gives its chunks with
// the most query terms first. Whole takes the opening chunk of every
// section. A share of the budget is kept for the chunks with the most
// question words, so a wrong pick still leaves the keyword hits in; what
// that share does not use goes back to the picked sections. The chunk
// indexes come back in document order.
func packPickedSections(
	chunks []types.TemporaryDocumentChunk, sections []attachmentSection, pick attachmentPick, query string, budget int,
) []int {
	taken := map[int]bool{}
	tokens := 0
	// a part is a run of adjacent chunks: the prompt shows it as one piece
	parts := 0
	take := func(i, limit, maxParts int) {
		if taken[i] {
			return
		}
		joins := taken[i-1] || taken[i+1]
		if !joins && parts >= maxParts {
			return
		}
		if tokens > 0 && tokens+chunks[i].TokenCount > limit {
			return
		}
		taken[i] = true
		tokens += chunks[i].TokenCount
		switch {
		case taken[i-1] && taken[i+1]:
			parts-- // i bridged two parts
		case !joins:
			parts++
		}
	}
	terms := temporaryDocumentQueryTerms(query)
	var picked []int
	if pick.Whole {
		for _, s := range sections {
			if s.First >= 0 {
				picked = append(picked, s.First)
			}
		}
	}
	for _, id := range pick.Sections {
		s := sections[id-1]
		if s.First < 0 {
			continue
		}
		idx := make([]int, 0, s.Last-s.First+1)
		for i := s.First; i <= s.Last; i++ {
			idx = append(idx, i)
		}
		sort.SliceStable(idx, func(a, b int) bool {
			return chunkTermScore(chunks[idx[a]], terms) > chunkTermScore(chunks[idx[b]], terms)
		})
		picked = append(picked, idx...)
	}
	var keyword []int
	for i := range chunks {
		if chunkTermScore(chunks[i], terms) > 0 {
			keyword = append(keyword, i)
		}
	}
	sort.SliceStable(keyword, func(a, b int) bool {
		return chunkTermScore(chunks[keyword[a]], terms) > chunkTermScore(chunks[keyword[b]], terms)
	})

	maxParts := temporaryDocumentMaxPromptParts
	pickLimit, pickParts := budget, maxParts
	if len(keyword) > 0 {
		pickLimit = budget * (100 - attachmentKeywordReservePercent) / 100
		pickParts = maxParts - attachmentKeywordReserveParts
	}
	for _, i := range picked {
		take(i, pickLimit, pickParts)
	}
	for _, i := range keyword {
		take(i, budget, maxParts)
	}
	for _, i := range picked {
		take(i, budget, maxParts)
	}
	out := make([]int, 0, len(taken))
	for i := range taken {
		out = append(out, i)
	}
	sort.Ints(out)
	return out
}

func chunkTermScore(chunk types.TemporaryDocumentChunk, terms []string) int {
	text := strings.ToLower(chunk.ContextHeader + "\n" + chunk.Content)
	score := 0
	for _, term := range terms {
		score += strings.Count(text, term) * (1 + len([]rune(term))/2)
	}
	return score
}

// joinTemporaryChunks renders chunks as the prompt shows them: each with
// its breadcrumb, separated by rules.
func joinTemporaryChunks(chunks []types.TemporaryDocumentChunk, indexes []int) string {
	var builder strings.Builder
	for _, i := range indexes {
		part := chunks[i]
		if builder.Len() > 0 {
			builder.WriteString("\n\n---\n\n")
		}
		if part.ContextHeader != "" {
			builder.WriteString(part.ContextHeader)
			builder.WriteString("\n\n")
		}
		builder.WriteString(strings.TrimSpace(part.Content))
	}
	return builder.String()
}
