package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/docformat"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// The document router picks which documents (and sections) a turn that
// names none is about, so the prompt carries those (rule 3 of
// BuildOpenDocumentPrompt) instead of passages of every document.
//
//   - Tier 1, no model: the question is scored against each document's card
//     fields (số ký hiệu, subject, gist, topics, entities, section titles,
//     file name) and, more lightly, against its text. A document scoring at
//     least routeFloor and routeMargin times the next one wins, with its
//     section when one title matches clearly.
//   - Tier 2, one thinking-off model call (≤ routeModelTimeout): only when
//     tier 1 is not clear and the question is not a plain code or number
//     lookup (the passages find those). It reads the question, the last
//     turns and the cards; its answer is taken at confidence ≥
//     routeMinConfidence.
//   - Tier 3 (ask the user) comes with the scope chip.
//
// A decision is stored as the session's router scope. An @-mention or a
// selection overrides any scope for its turn and clears a router scope; a
// scope the user set wins over the router. When tier 1 is not clear and a
// router scope is stored, the stored one is kept rather than asking the
// model again.
const (
	routeFloor          = 2.0
	routeMargin         = 2.0
	routeSectionFloor   = 1.5
	routeUnitWeight     = 1.0
	routeModelTimeout   = 10 * time.Second
	routeMinConfidence  = 0.6
	routeTurnRunes      = 300
	routeHistoryEntries = 6 // the last three exchanges
)

// DocumentRouteTurn is one earlier user or assistant message.
type DocumentRouteTurn struct {
	Role    string
	Content string
}

// DocumentRouteInput is what the router decides from.
type DocumentRouteInput struct {
	Query   string
	History []DocumentRouteTurn
	// Model is the tier-2 router (thinking off); nil skips tier 2.
	Model docformat.Completer
}

// ApplyDocumentScope decides the document scope of a turn (see above) and
// returns ctx carrying it for BuildOpenDocumentPrompt, with the scope (nil
// when none applies). ctx carries the turn's @-mentions and selection.
func ApplyDocumentScope(ctx context.Context, src DocumentWorkspaceSource, tenantID uint64, sessionID string, in DocumentRouteInput) (context.Context, *types.DocumentScope) {
	if src == nil || tenantID == 0 || strings.TrimSpace(sessionID) == "" {
		return ctx, nil
	}
	docs, err := src.List(ctx, tenantID, sessionID)
	if err != nil || len(docs) == 0 {
		return ctx, nil
	}
	stored := liveScope(SessionDocumentScope(ctx, sessionID), docs)
	if len(namedDocuments(ctx)) > 0 {
		if stored != nil && stored.SetBy == types.DocumentScopeSetByRouter {
			ClearSessionDocumentScope(ctx, sessionID)
		}
		return ctx, nil
	}
	if stored != nil && stored.SetBy == types.DocumentScopeSetByUser {
		return types.WithDocumentScope(ctx, stored), stored
	}
	if len(docs) < 2 || strings.TrimSpace(in.Query) == "" {
		return ctx, nil
	}
	if allDocumentsQuery(in.Query, docs) {
		// "các văn bản", "cả hai", a comparison naming none: every document
		// (rule 2), and a router scope from an earlier question goes
		if stored != nil {
			ClearSessionDocumentScope(ctx, sessionID)
			logger.Infof(ctx, "[DocumentRoute] session=%s question is about every document; router scope %v cleared", sessionID, stored.DocumentIDs)
		}
		return ctx, nil
	}

	readCtx := context.WithValue(ctx, types.TenantIDContextKey, tenantID)
	var loaded []*sessionDocUnits
	for _, d := range docs {
		if d.IsSource() && d.TextStatus != types.DocumentSourceTextReady {
			continue
		}
		du, err := loadDocumentUnits(readCtx, src, sessionID, d, len(loaded))
		if err != nil {
			logger.Warnf(ctx, "[DocumentRoute] session=%s document=%s unreadable: %v", sessionID, d.ID, err)
			continue
		}
		loaded = append(loaded, du)
	}
	ctx = withTurnUnits(ctx, loaded)
	if len(loaded) < 2 {
		return ctx, nil
	}
	if missing, all := unknownStrongCodes(parseSearchQuery(in.Query), loaded); all {
		// "PA04" in no document: no document is about it, and neither the
		// stored scope nor the model can say otherwise
		if stored != nil {
			ClearSessionDocumentScope(ctx, sessionID)
		}
		logger.Infof(ctx, "[DocumentRoute] session=%s codes %v occur in no document; no scope", sessionID, missing)
		return ctx, nil
	}

	scope, scores, topID, topScore := routeByKeywords(in.Query, loaded)
	tier := 1
	if scope == nil {
		if stored != nil {
			// a follow-up that matches nothing ("còn gì nữa") stays in the
			// stored scope, and so does one whose best document is in it;
			// a question pointing elsewhere drops it
			if topScore == 0 || stored.Includes(topID) {
				logger.Infof(ctx, "[DocumentRoute] session=%s tier 1 unclear %v; keeping the router scope %v", sessionID, scores, stored.DocumentIDs)
				return types.WithDocumentScope(ctx, stored), stored
			}
			ClearSessionDocumentScope(ctx, sessionID)
			logger.Infof(ctx, "[DocumentRoute] session=%s tier 1 points away from the router scope %v (%v); dropped", sessionID, stored.DocumentIDs, scores)
		}
		if in.Model == nil || plainLookup(in.Query) {
			logger.Infof(ctx, "[DocumentRoute] session=%s tier 1 unclear %v; no scope", sessionID, scores)
			return ctx, nil
		}
		tier = 2
		started := time.Now()
		var err error
		scope, err = routeByModel(ctx, in, loaded)
		logger.Infof(ctx, "[DocumentRoute] session=%s tier 2 took %s", sessionID, time.Since(started).Round(time.Millisecond))
		if err != nil || scope == nil {
			logger.Infof(ctx, "[DocumentRoute] session=%s tier 2 gave no scope (%v); tier 1 scores %v", sessionID, err, scores)
			return ctx, nil
		}
	}
	scope.SetBy, scope.Query, scope.At = types.DocumentScopeSetByRouter, in.Query, time.Now()
	if err := scope.Normalize(); err != nil {
		logger.Warnf(ctx, "[DocumentRoute] session=%s dropped an invalid scope: %v", sessionID, err)
		return ctx, nil
	}
	SetSessionDocumentScope(ctx, sessionID, scope)
	logger.Infof(ctx, "[DocumentRoute] session=%s tier %d scope documents=%v sections=%d task=%q (tier 1 scores %v)",
		sessionID, tier, scope.DocumentIDs, len(scope.Sections), scope.Task, scores)
	return types.WithDocumentScope(ctx, scope), scope
}

// cardText is what tier 1 matches a question against for one document.
func cardText(du *sessionDocUnits) (text, header string) {
	var parts []string
	name := du.ws.FileName
	if i := strings.LastIndex(name, "."); i > 0 {
		name = name[:i]
	}
	parts = append(parts, strings.NewReplacer("-", " ", "_", " ").Replace(name))
	if p := du.profile; p != nil {
		parts = append(parts, p.DocumentNumber, p.DocType, p.Issuer, p.Subject, p.Gist)
		parts = append(parts, p.Topics...)
		if e := p.Entities; e != nil {
			parts = append(parts, e.Units...)
			parts = append(parts, e.CitedNumbers...)
			parts = append(parts, e.Figures...)
		}
	}
	titles := make([]string, 0, len(du.sections))
	for _, s := range du.sections {
		titles = append(titles, s.Title)
	}
	return strings.Join(parts, " \n "), strings.Join(titles, " \n ")
}

// routeByKeywords is tier 1. scores lists each document's total (handle →
// score) for the log; topID and topScore are the best document and its
// total, whether or not it won.
//
// A document the question names — by its số ký hiệu, or by its type ("kế
// hoạch …") when it is the only document of that type — is the scope
// outright; two documents named so leave tier 1 unclear. Otherwise the
// card scores lead: the passage scores add at most as much as the best
// card score (routeFloor while no card matches), and a passage winner the
// cards disagree with leaves tier 1 unclear. A body that merely repeats a
// word of the question ("xây dựng kế hoạch …" in a công văn) thus cannot
// outvote the cards.
func routeByKeywords(query string, loaded []*sessionDocUnits) (scope *types.DocumentScope, scores map[string]float64, topID string, topScore float64) {
	q := parseSearchQuery(query)
	scores = map[string]float64{}
	if q.empty() {
		return nil, scores, "", 0
	}
	if _, all := unknownStrongCodes(q, loaded); all {
		return nil, scores, "", 0
	}
	cards := make([]searchUnit, len(loaded))
	var units []searchUnit
	for i, du := range loaded {
		text, header := cardText(du)
		cards[i] = searchUnit{Doc: i, Index: i, Text: text, Header: header}
		units = append(units, du.units...)
	}
	card := make([]float64, len(loaded))
	for _, h := range rankSearchUnits(q, cards) {
		card[h.Doc] += h.Score
	}
	// the text: the best passage of each document, and a quarter of the
	// next two
	text := make([]float64, len(loaded))
	taken := make([]int, len(loaded))
	for _, h := range rankSearchUnits(q, units) {
		switch taken[h.Doc] {
		case 0:
			text[h.Doc] += routeUnitWeight * h.Score
		case 1, 2:
			text[h.Doc] += routeUnitWeight * 0.25 * h.Score
		}
		taken[h.Doc]++
	}
	bestCard := 0.0
	for _, c := range card {
		bestCard = max(bestCard, c)
	}
	textCap := bestCard
	if textCap == 0 {
		textCap = routeFloor
	}
	total := make([]float64, len(loaded))
	for i := range loaded {
		total[i] = card[i] + min(text[i], textCap)
		scores[loaded[i].ws.Handle()] = float64(int(total[i]*100)) / 100
	}
	best := argmax(total)
	topID, topScore = loaded[best].ws.ID, total[best]

	pick := func(i int) *types.DocumentScope {
		du := loaded[i]
		s := &types.DocumentScope{DocumentIDs: []string{du.ws.ID}, Task: taskFromQuery(query)}
		if sec := bestSection(q, du.sections); sec != nil {
			s.Sections = []types.DocumentScopeSection{{DocumentID: du.ws.ID, From: sec.From, To: sec.To, Title: sec.Title}}
		}
		return s
	}
	switch named := namedByQuery(query, loaded); len(named) {
	case 1:
		topID, topScore = loaded[named[0]].ws.ID, max(topScore, routeFloor)
		return pick(named[0]), scores, topID, topScore
	case 0:
	default:
		return nil, scores, topID, topScore
	}

	second := 0.0
	for i, t := range total {
		if i != best {
			second = max(second, t)
		}
	}
	if total[best] < routeFloor || total[best] < routeMargin*second {
		return nil, scores, topID, topScore
	}
	if bestCard > 0 && argmax(card) != best {
		return nil, scores, topID, topScore // the cards point elsewhere
	}
	return pick(best), scores, topID, topScore
}

// unknownStrongCodes lists the strong codes of q (số ký hiệu, references,
// unit codes, figures) found in no unit or card of loaded; all is true
// when q has such codes and none of them is found anywhere.
func unknownStrongCodes(q *searchQuery, loaded []*sessionDocUnits) (missing []string, all bool) {
	if !q.strong() {
		return nil, false
	}
	var texts []string
	for _, du := range loaded {
		text, header := cardText(du)
		texts = append(texts, " "+foldSearch(text+" \n "+header)+" ")
		for _, u := range du.units {
			texts = append(texts, " "+foldSearch(u.Header+" \n "+u.Text)+" ")
		}
	}
	missing, _ = q.missingStrongCodes(texts)
	return missing, len(missing) == strongCodeCount(q)
}

func strongCodeCount(q *searchQuery) int {
	n := 0
	for _, c := range q.codes {
		if c.weight >= searchStrongCode {
			n++
		}
	}
	return n
}

func argmax(v []float64) int {
	best := 0
	for i := range v {
		if v[i] > v[best] {
			best = i
		}
	}
	return best
}

// docTypesByNameLength lists the document types longest name first, so
// "thông tư liên tịch" is read before "thông tư".
var docTypesByNameLength = func() []*vietnamese_legal.DocTypeDef {
	out := make([]*vietnamese_legal.DocTypeDef, 0, len(vietnamese_legal.DocTypes))
	for i := range vietnamese_legal.DocTypes {
		out = append(out, &vietnamese_legal.DocTypes[i])
	}
	sort.SliceStable(out, func(a, b int) bool { return len(out[a].Name) > len(out[b].Name) })
	return out
}()

// queryDocTypes returns the slugs of the document types the question names.
func queryDocTypes(query string) map[string]bool {
	f := " " + foldSearch(query) + " "
	out := map[string]bool{}
	for _, d := range docTypesByNameLength {
		name := foldSearch(d.Name)
		for containsCode(f, name) {
			out[d.Slug] = true
			f = strings.Replace(f, name, strings.Repeat(" ", len(name)), 1)
		}
	}
	return out
}

// documentTypeSlug is a document's type: its card's, else read from its
// first lines.
func documentTypeSlug(du *sessionDocUnits) string {
	if p := du.profile; p != nil && p.DocTypeCode != "" {
		return p.DocTypeCode
	}
	var head []string
	for i, u := range du.units {
		if i >= 15 {
			break
		}
		head = append(head, u.Text)
	}
	return vietnamese_legal.DetectDocType(strings.Join(head, "\n")).Slug
}

// namedByQuery returns the documents the question names: by số ký hiệu,
// by a strong code only that document holds, or by a type only that
// document has.
func namedByQuery(query string, loaded []*sessionDocUnits) []int {
	folded := " " + foldSearch(query) + " "
	named := map[int]bool{}
	for i, du := range loaded {
		if p := du.profile; p != nil && p.DocumentNumber != "" && containsCode(folded, foldSearch(p.DocumentNumber)) {
			named[i] = true
		}
	}
	// a strong code (PA05, a số ký hiệu, a figure) found in one document
	// only names that document
	if q := parseSearchQuery(query); q.strong() {
		var holders []int
		for i, du := range loaded {
			text, header := cardText(du)
			texts := []string{" " + foldSearch(text+" \n "+header) + " "}
			for _, u := range du.units {
				texts = append(texts, " "+foldSearch(u.Header+" \n "+u.Text)+" ")
			}
			if missing, _ := q.missingStrongCodes(texts); len(missing) < strongCodeCount(q) {
				holders = append(holders, i)
			}
		}
		if len(holders) == 1 {
			named[holders[0]] = true
		}
	}
	if types := queryDocTypes(query); len(types) > 0 {
		byType := map[string][]int{}
		for i, du := range loaded {
			if slug := documentTypeSlug(du); slug != "" {
				byType[slug] = append(byType[slug], i)
			}
		}
		for slug := range types {
			if docs := byType[slug]; len(docs) == 1 {
				named[docs[0]] = true
			}
		}
	}
	out := make([]int, 0, len(named))
	for i := range named {
		out = append(out, i)
	}
	sort.Ints(out)
	return out
}

var (
	allDocumentsRe = regexp.MustCompile(`(?i)các (văn bản|tài liệu)|những (văn bản|tài liệu)|tất cả|cả (hai|ba|bốn)\b|` +
		`\b(hai|ba|bốn|2|3|4) (văn bản|tài liệu)|mọi (văn bản|tài liệu)|từng (văn bản|tài liệu)`)
	compareRe = regexp.MustCompile(`(?i)so sánh|đối chiếu`)
	handleRe  = regexp.MustCompile(`(?i)\bvb\d+\b`)
)

// allDocumentsQuery reports a question about every document: "các văn
// bản", "tất cả", "cả hai", "hai văn bản", "mọi/từng tài liệu", or a
// comparison that names no document (no handle, no file name).
func allDocumentsQuery(query string, docs []*types.DocumentWorkspace) bool {
	if allDocumentsRe.MatchString(query) {
		return true
	}
	if !compareRe.MatchString(query) || handleRe.MatchString(query) {
		return false
	}
	lower := strings.ToLower(query)
	for _, d := range docs {
		name := strings.ToLower(d.FileName)
		if i := strings.LastIndex(name, "."); i > 0 {
			name = name[:i]
		}
		if utf8.RuneCountInString(name) >= 3 && strings.Contains(lower, name) {
			return false
		}
	}
	return true
}

// bestSection is the section whose title matches q clearly (at least
// routeSectionFloor, twice the next title), or nil.
func bestSection(q *searchQuery, sections []types.DocumentProfileSection) *types.DocumentProfileSection {
	if len(sections) == 0 {
		return nil
	}
	titles := make([]searchUnit, len(sections))
	for i, s := range sections {
		titles[i] = searchUnit{Index: i, Text: s.Title}
	}
	hits := rankSearchUnits(q, titles)
	if len(hits) == 0 || hits[0].Score < routeSectionFloor || (len(hits) > 1 && hits[0].Score < 2*hits[1].Score) {
		return nil
	}
	return &sections[hits[0].Index]
}

var routeTaskWords = []struct {
	task string
	re   *regexp.Regexp
}{
	{types.DocumentScopeTaskFormat, regexp.MustCompile(`(?i)thể thức|định dạng|nđ\s*30|nghị định 30`)},
	{types.DocumentScopeTaskSpelling, regexp.MustCompile(`(?i)chính tả|lỗi đánh máy|sai dấu`)},
	{types.DocumentScopeTaskCompare, regexp.MustCompile(`(?i)đối chiếu|so sánh|so với|khớp với|cập nhật .* theo`)},
	{types.DocumentScopeTaskSummary, regexp.MustCompile(`(?i)tóm tắt|tóm lược|nội dung chính`)},
	{types.DocumentScopeTaskEdit, regexp.MustCompile(`(?i)\bsửa\b|viết lại|chèn|bổ sung|chuẩn hóa`)},
}

// taskFromQuery reads the task a question names, "" when none.
func taskFromQuery(query string) string {
	for _, w := range routeTaskWords {
		if w.re.MatchString(query) {
			return w.task
		}
	}
	return ""
}

// plainLookup reports a question that is mostly a code or a number to
// find: the passages locate it without a model.
func plainLookup(query string) bool {
	q := parseSearchQuery(query)
	return q.strong() && len(q.terms) <= 3
}

const routeSystemPrompt = `Bạn là bộ định tuyến của trợ lý soạn thảo văn bản. Cuộc hội thoại có nhiều tài liệu (vb1, vb2, …), mỗi tài liệu một thẻ: tên tệp, vai trò, số ký hiệu, nội dung, các mục "tiêu đề [từ–đến]".
Đọc câu hỏi mới nhất (và các lượt gần đây để hiểu ngữ cảnh), cho biết câu hỏi nói về tài liệu nào và mục nào.
Chỉ trả về một đối tượng JSON:
{"documents":[{"handle":"vb2","sections":["tiêu đề mục đúng như trên thẻ"]}],"task":"","confidence":0.0}
- documents: các tài liệu câu hỏi nói tới (một hoặc vài); sections để trống khi câu hỏi nói về cả tài liệu hoặc không rõ mục.
- task: một trong "format" (thể thức), "spelling" (chính tả), "summary" (tóm tắt), "lookup" (tra cứu nội dung), "compare" (đối chiếu giữa các tài liệu), "edit" (sửa, viết lại), hoặc "" khi không rõ.
- confidence: từ 0 đến 1, mức chắc chắn của bạn. Câu hỏi chung chung, không chỉ rõ tài liệu nào thì để thấp (dưới 0.5).`

type routeReply struct {
	Documents []struct {
		Handle   string   `json:"handle"`
		Sections []string `json:"sections"`
	} `json:"documents"`
	Task       string  `json:"task"`
	Confidence float64 `json:"confidence"`
}

// routeByModel is tier 2.
func routeByModel(ctx context.Context, in DocumentRouteInput, loaded []*sessionDocUnits) (*types.DocumentScope, error) {
	var user strings.Builder
	user.WriteString("Câu hỏi: " + clipRunes(strings.TrimSpace(in.Query), 1000) + "\n")
	if hist := recentTurns(in.History); len(hist) > 0 {
		user.WriteString("\nCác lượt gần đây:\n")
		for _, t := range hist {
			fmt.Fprintf(&user, "%s: %s\n", t.Role, clipRunes(flatText(t.Content), routeTurnRunes))
		}
	}
	user.WriteString("\nTài liệu:\n")
	for _, du := range loaded {
		role := "văn bản làm việc"
		if du.ws.IsSource() {
			role = "tài liệu nguồn"
		}
		user.WriteString(documentCard("- "+DocumentLabel(du.ws)+" ("+role+")", du.ws, du.profile))
	}
	rctx, cancel := context.WithTimeout(ctx, routeModelTimeout)
	defer cancel()
	text, err := in.Model.Complete(rctx, []docformat.Message{
		{Role: "system", Content: routeSystemPrompt},
		{Role: "user", Content: user.String()},
	})
	if err != nil {
		return nil, err
	}
	reply, err := parseRouteReply(text)
	if err != nil {
		return nil, err
	}
	if reply.Confidence < routeMinConfidence {
		return nil, fmt.Errorf("confidence %.2f below %.2f", reply.Confidence, routeMinConfidence)
	}
	docs := make([]*types.DocumentWorkspace, len(loaded))
	for i, du := range loaded {
		docs[i] = du.ws
	}
	scope := &types.DocumentScope{}
	if types.ValidDocumentScopeTask(strings.ToLower(strings.TrimSpace(reply.Task))) {
		scope.Task = strings.ToLower(strings.TrimSpace(reply.Task))
	}
	for _, rd := range reply.Documents {
		d := matchDocument(docs, rd.Handle)
		if d == nil || scope.Includes(d.ID) {
			continue
		}
		scope.DocumentIDs = append(scope.DocumentIDs, d.ID)
		var du *sessionDocUnits
		for _, l := range loaded {
			if l.ws.ID == d.ID {
				du = l
			}
		}
		for _, title := range rd.Sections {
			if s := sectionByTitle(du.sections, title); s != nil && len(scope.Sections) < types.DocumentScopeMaxSections {
				scope.Sections = append(scope.Sections, types.DocumentScopeSection{DocumentID: d.ID, From: s.From, To: s.To, Title: s.Title})
			}
		}
	}
	if len(scope.DocumentIDs) == 0 {
		return nil, errors.New("the router named no document of the session")
	}
	return scope, nil
}

// recentTurns keeps the last routeHistoryEntries user and assistant
// messages with text.
func recentTurns(history []DocumentRouteTurn) []DocumentRouteTurn {
	var out []DocumentRouteTurn
	for i := len(history) - 1; i >= 0 && len(out) < routeHistoryEntries; i-- {
		t := history[i]
		if (t.Role == "user" || t.Role == "assistant") && strings.TrimSpace(t.Content) != "" {
			out = append(out, t)
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// sectionByTitle finds the section a title written by the model names:
// the same title (without diacritics and case), else a unique match of
// matchSections.
func sectionByTitle(sections []types.DocumentProfileSection, title string) *types.DocumentProfileSection {
	f := foldSearch(strings.TrimSpace(title))
	if f == "" {
		return nil
	}
	// the card may show a title cut with "…"
	f = strings.TrimSuffix(f, "…")
	for i := range sections {
		if t := foldSearch(sections[i].Title); t == f || (utf8.RuneCountInString(f) >= 12 && strings.HasPrefix(t, f)) {
			return &sections[i]
		}
	}
	if m := matchSections(sections, title); len(m) == 1 {
		for i := range sections {
			if sections[i] == m[0] {
				return &sections[i]
			}
		}
	}
	return nil
}

func parseRouteReply(text string) (*routeReply, error) {
	t := profileFenceRe.ReplaceAllString(strings.TrimSpace(text), "")
	var r routeReply
	if err := json.Unmarshal([]byte(t), &r); err != nil {
		start, end := strings.Index(t, "{"), strings.LastIndex(t, "}")
		if start < 0 || end <= start {
			return nil, errors.New("no JSON object in router reply")
		}
		if err := json.Unmarshal([]byte(t[start:end+1]), &r); err != nil {
			return nil, fmt.Errorf("router reply is not valid JSON: %w", err)
		}
	}
	return &r, nil
}
