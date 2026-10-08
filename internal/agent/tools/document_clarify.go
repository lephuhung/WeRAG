package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// The scope clarification gate: before the document text of a turn is
// built, a generic request ("góp ý giúp", "tóm tắt") about a long document
// that names no part of it is answered with a question instead of running
// the agent, which would otherwise read the document page after page. No
// model is called. It asks when all hold:
//
//  1. a document the turn is about is long: a target of at least
//     clarifyTargetRunes runes or clarifyTargetParagraphs non-empty
//     paragraphs, a source of at least clarifySourceRunes, or together at
//     least clarifyTotalRunes;
//  2. the request has no anchor: no @-mention or selection, no code from
//     the question found in the documents, no section reference (Điều 3,
//     Chương II, Phụ lục 1) or section title, no document type or số ký
//     hiệu naming a document, and no scope the user set;
//  3. the request is generic (genericScopeRequest).
//
// It never asks when the task needs no text (format, spelling), when the
// user asks for the whole document (toàn bộ, cả văn bản, đầy đủ), when the
// user already answered for that document and task in this session (see
// MarkScopeClarificationAnswered), or when the agent turns it off.
const (
	clarifyTargetRunes      = openDocumentPromptRunes
	clarifyTargetParagraphs = 150
	clarifySourceRunes      = 16000
	clarifyTotalRunes       = 24000
	clarifyMaxWords         = 12
	clarifyRunesPerPage     = 2000 // an A4 page of Times New Roman 14
	clarifyPayloadDocs      = 10
	clarifyPayloadSections  = 40
	clarifySectionRunes     = 80
)

// DocumentScopeClarificationType is the display_type (and tool name) of
// the clarification payload the chat renders as a card.
const DocumentScopeClarificationType = "document_scope_clarification"

// Tasks offered by the clarification card. "part" stores a scope of task
// lookup with the sections ticked; "other" stores none.
const (
	ScopeClarifyTaskFormat   = "format"
	ScopeClarifyTaskSpelling = "spelling"
	ScopeClarifyTaskSummary  = "summary"
	ScopeClarifyTaskPart     = "part"
	ScopeClarifyTaskCompare  = "compare"
	ScopeClarifyTaskOther    = "other"
)

// ScopeClarificationInput is what the gate decides from; ctx carries the
// turn's @-mentions, selection and applied scope.
type ScopeClarificationInput struct {
	// Query is the request as the user wrote it.
	Query string
	// Disabled is the agent's ask_scope_for_long_documents turned off.
	Disabled bool
}

// ScopeClarification is the gate's question: the payload of the card and
// the assistant's sentence (Message).
type ScopeClarification struct {
	DisplayType string                       `json:"display_type"`
	Query       string                       `json:"query"`
	Documents   []ScopeClarificationDocument `json:"documents"`
	Tasks       []ScopeClarificationTask     `json:"tasks"`
	Suggested   ScopeClarificationSuggestion `json:"suggested"`
	long        []ScopeClarificationDocument // the long ones, for Message
}

// ScopeClarificationDocument is one document the card can scope to.
type ScopeClarificationDocument struct {
	ID         string                      `json:"id"`
	Handle     string                      `json:"handle"`
	FileName   string                      `json:"file_name"`
	Role       string                      `json:"role"`
	Unit       string                      `json:"unit"`
	Paragraphs int                         `json:"paragraphs"`
	Runes      int                         `json:"runes"`
	Long       bool                        `json:"long"`
	Sections   []ScopeClarificationSection `json:"sections"`
}

// ScopeClarificationSection is one section of a document, as on its card.
type ScopeClarificationSection struct {
	Title string `json:"title"`
	From  int    `json:"from"`
	To    int    `json:"to"`
}

// ScopeClarificationTask is one task chip.
type ScopeClarificationTask struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// ScopeClarificationSuggestion is the router's best guess the card starts
// from.
type ScopeClarificationSuggestion struct {
	DocumentID string                      `json:"document_id"`
	Task       string                      `json:"task,omitempty"`
	Sections   []ScopeClarificationSection `json:"sections"`
}

// Data is the payload as tool-result data (JSON field names).
func (c *ScopeClarification) Data() map[string]interface{} {
	raw, err := json.Marshal(c)
	if err != nil {
		return map[string]interface{}{"display_type": DocumentScopeClarificationType}
	}
	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]interface{}{"display_type": DocumentScopeClarificationType}
	}
	return out
}

// Message is the assistant's answer: which document, how long, and what
// the user can choose.
func (c *ScopeClarification) Message() string {
	describe := func(d ScopeClarificationDocument) string {
		pages := max(1, (d.Runes+clarifyRunesPerPage-1)/clarifyRunesPerPage)
		if d.Role == types.DocumentWorkspaceRoleSource {
			return fmt.Sprintf("%s · %s (khoảng %d trang)", d.Handle, d.FileName, pages)
		}
		return fmt.Sprintf("%s · %s (%d đoạn, khoảng %d trang)", d.Handle, d.FileName, d.Paragraphs, pages)
	}
	long := c.long
	if len(long) == 0 {
		long = c.Documents
	}
	var head string
	if len(long) == 1 {
		head = "Văn bản " + describe(long[0]) + " khá dài, đọc hết một lượt sẽ chậm và dễ sót."
	} else {
		parts := make([]string, 0, len(long))
		for _, d := range long {
			parts = append(parts, describe(d))
		}
		head = "Các tài liệu " + strings.Join(parts, ", ") + " khá dài, đọc hết một lượt sẽ chậm và dễ sót."
	}
	labels := make([]string, 0, len(c.Tasks))
	for _, t := range c.Tasks {
		if t.Key != ScopeClarifyTaskOther {
			labels = append(labels, strings.ToLower(t.Label))
		}
	}
	ask := " Bạn muốn tôi " + strings.Join(labels, ", ") + " hay làm việc khác?"
	if len(c.Documents) > 1 {
		ask = " Bạn chọn tài liệu và việc cần làm (" + strings.Join(labels, ", ") + " hay việc khác) ở thẻ bên dưới."
	}
	return head + ask
}

// DocumentScopeClarification runs the gate for a turn; nil means go on
// with the agent.
func DocumentScopeClarification(ctx context.Context, src DocumentWorkspaceSource, tenantID uint64, sessionID string, in ScopeClarificationInput) *ScopeClarification {
	query := strings.TrimSpace(in.Query)
	if in.Disabled || src == nil || tenantID == 0 || strings.TrimSpace(sessionID) == "" || query == "" {
		return nil
	}
	if clearTaskRequest(query) || wholeDocumentRequest(query) || !genericScopeRequest(query) {
		return nil
	}
	if len(namedDocuments(ctx)) > 0 {
		return nil
	}
	docs, err := src.List(ctx, tenantID, sessionID)
	if err != nil || len(docs) == 0 {
		return nil
	}
	if stored := liveScope(SessionDocumentScope(ctx, sessionID), docs); stored != nil && stored.SetBy == types.DocumentScopeSetByUser {
		return nil
	}
	applied := liveScope(types.DocumentScopeFromContext(ctx), docs)
	if applied != nil && (applied.SetBy == types.DocumentScopeSetByUser || len(applied.Sections) > 0) {
		return nil // the scope already narrows the text
	}
	inPlay := docs
	if applied != nil {
		inPlay = nil
		for _, d := range docs {
			if applied.Includes(d.ID) {
				inPlay = append(inPlay, d)
			}
		}
	}

	readCtx := context.WithValue(ctx, types.TenantIDContextKey, tenantID)
	var loaded []*sessionDocUnits
	for _, d := range inPlay {
		if d.IsSource() && d.TextStatus != types.DocumentSourceTextReady {
			continue
		}
		du, err := loadDocumentUnits(readCtx, src, sessionID, d, len(loaded))
		if err != nil {
			logger.Warnf(ctx, "[DocumentClarify] session=%s document=%s unreadable: %v", sessionID, d.ID, err)
			continue
		}
		loaded = append(loaded, du)
	}
	sizes, longIdx := measureDocuments(loaded)
	if len(longIdx) == 0 {
		return nil
	}
	if anchoredRequest(query, loaded) {
		return nil
	}
	task := taskFromQuery(query)
	for _, i := range longIdx {
		if scopeClarificationAnswered(ctx, sessionID, loaded[i].ws.ID, task) {
			logger.Infof(ctx, "[DocumentClarify] session=%s %s already answered for task %q; not asking", sessionID, loaded[i].ws.Handle(), task)
			return nil
		}
	}

	out := &ScopeClarification{DisplayType: DocumentScopeClarificationType, Query: query}
	isLong := map[int]bool{}
	for _, i := range longIdx {
		isLong[i] = true
	}
	hasSource := false
	for i, du := range loaded {
		if len(out.Documents) >= clarifyPayloadDocs {
			break
		}
		cd := ScopeClarificationDocument{ID: du.ws.ID, Handle: du.ws.Handle(), FileName: du.ws.FileName,
			Role: types.DocumentWorkspaceRoleTarget, Unit: du.unit, Runes: sizes[i].runes, Paragraphs: sizes[i].paragraphs,
			Long: isLong[i], Sections: clarificationSections(du.sections)}
		if du.ws.IsSource() {
			cd.Role = types.DocumentWorkspaceRoleSource
			hasSource = true
		}
		out.Documents = append(out.Documents, cd)
		if cd.Long {
			out.long = append(out.long, cd)
		}
	}
	out.Tasks = clarificationTasks(len(loaded) > 1 || hasSource)
	out.Suggested = suggestScope(ctx, src, tenantID, sessionID, query, loaded, longIdx, applied)
	logger.Infof(ctx, "[DocumentClarify] session=%s asking for a scope: %d long of %d documents, suggested %s",
		sessionID, len(longIdx), len(loaded), out.Suggested.DocumentID)
	return out
}

type documentSize struct{ runes, paragraphs int }

// measureDocuments returns each document's size and the indexes of the
// long ones (all of them when only the total is long).
func measureDocuments(loaded []*sessionDocUnits) ([]documentSize, []int) {
	sizes := make([]documentSize, len(loaded))
	var long []int
	total := 0
	for i, du := range loaded {
		for _, u := range du.units {
			sizes[i].runes += utf8.RuneCountInString(u.Text)
		}
		sizes[i].paragraphs = len(du.units)
		total += sizes[i].runes
		if du.ws.IsSource() {
			if sizes[i].runes >= clarifySourceRunes {
				long = append(long, i)
			}
		} else if sizes[i].runes >= clarifyTargetRunes || sizes[i].paragraphs >= clarifyTargetParagraphs {
			long = append(long, i)
		}
	}
	if len(long) == 0 && total >= clarifyTotalRunes {
		for i := range loaded {
			long = append(long, i)
		}
	}
	return sizes, long
}

func clarificationSections(sections []types.DocumentProfileSection) []ScopeClarificationSection {
	out := make([]ScopeClarificationSection, 0, min(len(sections), clarifyPayloadSections))
	for _, s := range sections {
		if len(out) >= clarifyPayloadSections {
			break
		}
		out = append(out, ScopeClarificationSection{Title: clipRunes(strings.TrimSpace(s.Title), clarifySectionRunes), From: s.From, To: s.To})
	}
	return out
}

func clarificationTasks(withCompare bool) []ScopeClarificationTask {
	out := []ScopeClarificationTask{
		{ScopeClarifyTaskFormat, "Kiểm tra thể thức"},
		{ScopeClarifyTaskSpelling, "Kiểm tra chính tả"},
		{ScopeClarifyTaskSummary, "Tóm tắt"},
		{ScopeClarifyTaskPart, "Xem một phần"},
	}
	if withCompare {
		out = append(out, ScopeClarificationTask{ScopeClarifyTaskCompare, "Đối chiếu với nguồn"})
	}
	return append(out, ScopeClarificationTask{ScopeClarifyTaskOther, "Việc khác"})
}

// suggestScope is the card's starting point: the applied router scope's
// document, else the document a handle in the question names, else the
// tier-1 best document, else the tab being viewed, else the first long one.
func suggestScope(ctx context.Context, src DocumentWorkspaceSource, tenantID uint64, sessionID, query string,
	loaded []*sessionDocUnits, longIdx []int, applied *types.DocumentScope,
) ScopeClarificationSuggestion {
	s := ScopeClarificationSuggestion{Sections: []ScopeClarificationSection{}}
	switch taskFromQuery(query) {
	case types.DocumentScopeTaskSummary:
		s.Task = ScopeClarifyTaskSummary
	case types.DocumentScopeTaskCompare:
		s.Task = ScopeClarifyTaskCompare
	}
	has := func(id string) bool {
		for _, du := range loaded {
			if du.ws.ID == id {
				return true
			}
		}
		return false
	}
	if applied != nil && len(applied.DocumentIDs) > 0 && has(applied.DocumentIDs[0]) {
		s.DocumentID = applied.DocumentIDs[0]
		return s
	}
	for _, h := range handleRe.FindAllString(query, -1) {
		for _, du := range loaded {
			if strings.EqualFold(du.ws.Handle(), h) {
				s.DocumentID = du.ws.ID
				return s
			}
		}
	}
	if len(loaded) > 1 {
		if _, _, topID, topScore := routeByKeywords(query, loaded); topScore > 0 && has(topID) {
			s.DocumentID = topID
			return s
		}
	}
	if active, err := src.GetBySession(ctx, tenantID, sessionID); err == nil && active != nil && has(active.ID) {
		s.DocumentID = active.ID
		return s
	}
	s.DocumentID = loaded[longIdx[0]].ws.ID
	return s
}

// clearTaskRequest reports a task that needs no text read into the
// prompt: the format check and the spelling check run on the file.
func clearTaskRequest(query string) bool {
	switch taskFromQuery(query) {
	case types.DocumentScopeTaskFormat, types.DocumentScopeTaskSpelling:
		return true
	}
	return false
}

var wholeDocumentRe = regexp.MustCompile(`\b(toan bo|toan van|ca van ban|ca tai lieu|ca file|day du|het van ban|tu dau den cuoi|tung muc|tung phan)\b`)

// wholeDocumentRequest reports a request for the whole document: the
// agent then reads it section by section.
func wholeDocumentRequest(query string) bool {
	return wholeDocumentRe.MatchString(foldSearch(query))
}

// The generic-intent list, folded (no diacritics, lower case): a request
// made of these and of clarifyFillerWords only says "do something with the
// document" without naming what to look at.
var clarifyGenericPhrases = []string{
	"xem", "doc", "xem xet",
	"gop y", "kiem tra", "ra soat", "soat lai", "soat", "nhan xet", "danh gia", "phan tich",
	"tom tat", "tom luoc", "noi dung chinh", "y chinh", "the nao", "ra sao",
	"co van de gi", "co gi sai", "co sai sot gi", "co loi gi", "co gi can sua", "can sua gi",
	"review", "check", "feedback",
}

// clarifyFillerWords are the folded words a generic request may also hold:
// politeness, pronouns, "văn bản này", handles (vbN, matched apart).
var clarifyFillerWords = func() map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.Fields(`giup ho toi minh em anh chi ban cho voi nhe nha nhi di a ah hay vui long lam on xin
		van ban tai lieu file tep du thao nay do kia cai the nao gi khong la co duoc can muon mot chut lai qua
		thu ve cua noi dung sao vay ne dum gium`) {
		out[w] = true
	}
	return out
}()

var clarifyHandleWordRe = regexp.MustCompile(`^vb\d+$`)

// genericScopeRequest reports a short request (at most clarifyMaxWords
// words) holding a generic phrase and nothing more concrete: "góp ý giúp
// văn bản này", "tóm tắt nội dung chính", "văn bản này có vấn đề gì
// không". A request naming what to look at ("kiểm tra số liệu", "thời hạn
// là bao giờ", "ai ký") is not generic.
func genericScopeRequest(query string) bool {
	words := clarifyWords(foldSearch(query))
	if len(words) == 0 || len(words) > clarifyMaxWords {
		return false
	}
	text := " " + strings.Join(words, " ") + " "
	found := false
	for _, p := range clarifyGenericPhrases {
		needle := " " + p + " "
		for strings.Contains(text, needle) {
			text = strings.Replace(text, needle, " ", 1)
			found = true
		}
	}
	if !found {
		return false
	}
	for _, w := range strings.Fields(text) {
		if !clarifyFillerWords[w] && !clarifyHandleWordRe.MatchString(w) {
			return false
		}
	}
	return true
}

// clarifyWords splits folded text into words of letters and digits.
func clarifyWords(folded string) []string {
	return strings.FieldsFunc(folded, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// anchoredRequest reports a request that names a part of the documents: a
// code from it found in them, a section reference or a section title, or
// a document named by type or số ký hiệu.
func anchoredRequest(query string, loaded []*sessionDocUnits) bool {
	folded := foldSearch(query)
	if searchReferenceRe.MatchString(folded) {
		return true
	}
	var codes []string
	for _, c := range queryCodes(query) {
		if !handleRe.MatchString(c) {
			codes = append(codes, c)
		}
	}
	if len(codes) > 0 {
		for _, du := range loaded {
			for _, u := range du.units {
				if containsAnyFold(u.Text, codes) {
					return true
				}
			}
		}
	}
	q := parseSearchQuery(query)
	if !q.empty() {
		for _, du := range loaded {
			if bestSection(q, du.sections) != nil {
				return true
			}
		}
	}
	return len(namedByQuery(query, loaded)) > 0
}

// The documents and tasks the user answered a clarification for, per
// session: in Redis (werag:docclarify:<session>, as the scope) for a day,
// else in process memory.
const docClarifyKeyPrefix = "werag:docclarify:"

type storedClarified struct {
	pairs   map[string][]string
	expires time.Time
}

var (
	docClarified   sync.Map // session ID → storedClarified
	docClarifiedMu sync.Mutex
)

func loadClarified(ctx context.Context, sessionID string) map[string][]string {
	if formatChecks.rdb != nil {
		var pairs map[string][]string
		if formatChecks.redisGet(ctx, docClarifyKeyPrefix+sessionID, &pairs) && pairs != nil {
			return pairs
		}
		return map[string][]string{}
	}
	v, ok := docClarified.Load(sessionID)
	if !ok {
		return map[string][]string{}
	}
	st := v.(storedClarified)
	if time.Now().After(st.expires) {
		docClarified.Delete(sessionID)
		return map[string][]string{}
	}
	out := make(map[string][]string, len(st.pairs))
	for k, v := range st.pairs {
		out[k] = append([]string(nil), v...)
	}
	return out
}

// MarkScopeClarificationAnswered records that the user chose a scope of
// task for documentIDs (the clarification card, or the scope chip), so the
// gate does not ask again for those documents and task this session.
func MarkScopeClarificationAnswered(ctx context.Context, sessionID string, documentIDs []string, task string) {
	if sessionID == "" || len(documentIDs) == 0 {
		return
	}
	docClarifiedMu.Lock()
	defer docClarifiedMu.Unlock()
	pairs := loadClarified(ctx, sessionID)
	for _, id := range documentIDs {
		known := false
		for _, t := range pairs[id] {
			known = known || t == task
		}
		if !known {
			pairs[id] = append(pairs[id], task)
		}
	}
	if formatChecks.rdb == nil {
		docClarified.Store(sessionID, storedClarified{pairs: pairs, expires: time.Now().Add(docScopeTTL)})
		return
	}
	raw, err := json.Marshal(pairs)
	if err != nil {
		return
	}
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), formatCheckRedisTimeout)
	defer cancel()
	if err := formatChecks.rdb.Set(rctx, docClarifyKeyPrefix+sessionID, raw, docScopeTTL).Err(); err != nil {
		logger.Warnf(ctx, "[DocumentClarify] redis set %s: %v", sessionID, err)
	}
}

// scopeClarificationAnswered reports an answer recorded for documentID and
// task; a request without a task counts any answer for the document.
func scopeClarificationAnswered(ctx context.Context, sessionID, documentID, task string) bool {
	tasks := loadClarified(ctx, sessionID)[documentID]
	if task == "" {
		return len(tasks) > 0
	}
	for _, t := range tasks {
		if t == task {
			return true
		}
	}
	return false
}
