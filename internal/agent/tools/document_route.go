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
	if len(loaded) < 2 {
		return ctx, nil
	}

	scope, scores := routeByKeywords(in.Query, loaded)
	tier := 1
	if scope == nil {
		if stored != nil {
			logger.Infof(ctx, "[DocumentRoute] session=%s tier 1 unclear %v; keeping the router scope %v", sessionID, scores, stored.DocumentIDs)
			return types.WithDocumentScope(ctx, stored), stored
		}
		if in.Model == nil || plainLookup(in.Query) {
			logger.Infof(ctx, "[DocumentRoute] session=%s tier 1 unclear %v; no scope", sessionID, scores)
			return ctx, nil
		}
		tier = 2
		var err error
		scope, err = routeByModel(ctx, in, loaded)
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
// score) for the log.
func routeByKeywords(query string, loaded []*sessionDocUnits) (*types.DocumentScope, map[string]float64) {
	q := parseSearchQuery(query)
	scores := map[string]float64{}
	if q.empty() {
		return nil, scores
	}
	cards := make([]searchUnit, len(loaded))
	var units []searchUnit
	for i, du := range loaded {
		text, header := cardText(du)
		cards[i] = searchUnit{Doc: i, Index: i, Text: text, Header: header}
		units = append(units, du.units...)
	}
	total := make([]float64, len(loaded))
	for _, h := range rankSearchUnits(q, cards) {
		total[h.Doc] += h.Score
	}
	// the text: the best passage of each document, and a quarter of the
	// next two
	taken := make([]int, len(loaded))
	for _, h := range rankSearchUnits(q, units) {
		switch taken[h.Doc] {
		case 0:
			total[h.Doc] += routeUnitWeight * h.Score
		case 1, 2:
			total[h.Doc] += routeUnitWeight * 0.25 * h.Score
		}
		taken[h.Doc]++
	}
	order := make([]int, len(loaded))
	for i := range order {
		order[i] = i
		scores[loaded[i].ws.Handle()] = float64(int(total[i]*100)) / 100
	}
	sort.SliceStable(order, func(a, b int) bool { return total[order[a]] > total[order[b]] })
	best := order[0]
	second := 0.0
	if len(order) > 1 {
		second = total[order[1]]
	}
	if total[best] < routeFloor || total[best] < routeMargin*second {
		return nil, scores
	}
	du := loaded[best]
	scope := &types.DocumentScope{DocumentIDs: []string{du.ws.ID}, Task: taskFromQuery(query)}
	if s := bestSection(q, du.sections); s != nil {
		scope.Sections = []types.DocumentScopeSection{{DocumentID: du.ws.ID, From: s.From, To: s.To, Title: s.Title}}
	}
	return scope, scores
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
