package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/docformat"
	"github.com/Tencent/WeKnora/internal/docformat/docxedit"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

var checkSpellingTool = BaseTool{
	name: ToolCheckSpelling,
	description: `Review Vietnamese spelling in the Word document open in this conversation's editor: misspelled words, typos, wrong or missing diacritics (dấu), punctuation and obvious grammar slips. The findings are listed and, by default, underlined in red in the editor; the text itself is NEVER changed.

## When to Use

The user asks to check spelling, typos or diacritics ("kiểm tra chính tả", "soát lỗi chính tả", "lỗi đánh máy", "sai dấu"). It does not judge layout (use check_document_format) and does not rewrite anything: to correct a finding the user highlights it and asks for a rewrite (rewrite_paragraphs).

## Input

- scope: "selection" checks only the highlighted passage, "document" the document (default: the selection when the user highlighted one, else the document).
- from, limit: paragraph window for scope=document (default from 0, 60 paragraphs, at most 150); the result says where to continue.
- mark: false to only list the findings without underlining them (default true).

## Output

A numbered list "Đoạn [12]: “sai” → “đúng” (lý do)". Present it to the user; say the passages were underlined in red and that nothing was rewritten.`,
	schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    ` + documentParamSchema + `,
    "scope": {"type": "string", "enum": ["document", "selection"], "description": "selection: only the highlighted passage; document: the paragraph window. Default: selection when present, else document"},
    "from": {"type": "integer", "minimum": 0, "description": "First paragraph index for scope=document (default 0)"},
    "limit": {"type": "integer", "minimum": 1, "maximum": 150, "description": "Paragraphs to review for scope=document (default 60, max 150)"},
    "mark": {"type": "boolean", "description": "Underline the findings in the editor (default true)"}
  }
}`),
}

const (
	spellDefaultLimit   = 60
	spellMaxLimit       = 150
	spellBatchSize      = 20
	spellMaxFindings    = 60
	spellParagraphRunes = 2000
	strayCharReason     = "ký tự lạ trong từ"
	// spellMaxTokens caps one batch's reply: a batch of spellBatchSize
	// paragraphs has a few findings. Under the 8192 of the shared labeling
	// completer a reply once ran away for 90 s (Qwen3.6, 2026-10-10).
	spellMaxTokens = 2048
	// spellRetryEchoRunes is how much of a malformed reply the retry quotes.
	spellRetryEchoRunes = 1500
	// spellSplitMin: a batch larger than this that still fails is reviewed
	// again in two halves.
	spellSplitMin = 5
	// spellConcurrency is how many batches run at once.
	spellConcurrency = 3
)

// DefaultSpellcheckPrompt is the system prompt used when the spellcheck_review
// template is not configured. It must stay identical to the content of
// config/prompt_templates/spellcheck_review.yaml (a test checks it).
const DefaultSpellcheckPrompt = `You proofread Vietnamese administrative documents. You receive numbered paragraphs ("[12] text").

Report ONLY genuine errors:
- misspelled words and typos;
- missing or wrong diacritics that change the word (e.g. "đề nghi" for "đề nghị", "triễn khai" for "triển khai");
- doubled words ("các các");
- punctuation errors (wrong or missing punctuation, a space before a comma or period, a missing space after one).

Do NOT report:
- tone-mark placement variants, which are both accepted (hoà/hòa, thuỷ/thủy, oà/òa, khoẻ/khỏe);
- y/i spelling variants (lí/lý, kĩ/kỹ, mĩ/mỹ);
- headings or lines written in all capitals;
- proper nouns (names of people, places, agencies);
- document numbers and codes (số ký hiệu such as 12/SNV-VP), abbreviations, numbers, dates or legal references;
- style preferences or administrative wording you would phrase differently.

Answer with ONE JSON object and nothing else:
{"findings":[{"paragraph":12,"wrong":"exact erroneous text copied from that paragraph","correct":"the corrected text","reason":"short reason in Vietnamese"}]}

Rules: "wrong" must be copied exactly from the paragraph (a word or a short phrase, not the whole paragraph); "correct" must differ from "wrong"; one finding per error; {"findings":[]} when there is no error.`

// CheckSpellingTool reviews spelling in the session's workspace document
// with a chat model and marks the findings through the editor plugin.
type CheckSpellingTool struct {
	BaseTool
	workspace DocumentWorkspaceSource
	model     chat.Chat
	prompt    string
	sessionID string
}

// NewCheckSpellingTool builds the tool for one session; model is the
// spellcheck model (spellcheck_model_id, else the agent's).
func NewCheckSpellingTool(workspace DocumentWorkspaceSource, model chat.Chat, sessionID string) *CheckSpellingTool {
	return &CheckSpellingTool{BaseTool: checkSpellingTool, workspace: workspace, model: model,
		prompt: DefaultSpellcheckPrompt, sessionID: sessionID}
}

// WithPrompt replaces the system prompt (the spellcheck_review template).
func (t *CheckSpellingTool) WithPrompt(prompt string) *CheckSpellingTool {
	if strings.TrimSpace(prompt) != "" {
		t.prompt = prompt
	}
	return t
}

type checkSpellingInput struct {
	Scope string `json:"scope"`
	From  int    `json:"from"`
	Limit int    `json:"limit"`
	Mark  *bool  `json:"mark"`
	// Document is the target document handle (see resolveDocument).
	Document string `json:"document"`
}

// SpellingFinding is one validated spelling error.
type SpellingFinding struct {
	Paragraph int    `json:"paragraph"`
	Wrong     string `json:"wrong"`
	Correct   string `json:"correct"`
	Reason    string `json:"reason"`
	// Unmarked: the text could not be located exactly enough to underline.
	Unmarked bool `json:"unmarked,omitempty"`
	// raw is the paragraph's own text of the error (it can differ from Wrong
	// by no-break / zero-width spaces or composition); occurrence is which
	// instance of raw in the paragraph it is (1-based).
	raw        string
	occurrence int
}

// spellPara is a paragraph under review; Part is set when only the
// highlighted part of it is in scope.
type spellPara struct {
	Index int
	Text  string
	Part  string
}

func (t *CheckSpellingTool) Execute(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
	var in checkSpellingInput
	if len(args) > 0 {
		if err := json.Unmarshal(args, &in); err != nil {
			return &types.ToolResult{Success: false, Error: "invalid arguments: " + err.Error()}, nil
		}
	}
	sel := types.DocumentSelectionFromContext(ctx)
	scope := in.Scope
	switch scope {
	case "":
		scope = "document"
		if sel != nil {
			scope = "selection"
		}
	case "document", "selection":
	default:
		return &types.ToolResult{Success: false, Error: "scope must be document or selection"}, nil
	}
	if scope == "selection" && sel == nil {
		return &types.ToolResult{Success: false, Error: "Người dùng chưa bôi đen đoạn nào: dùng scope=document hoặc nhờ người dùng bôi đen đoạn cần kiểm tra."}, nil
	}
	if t.model == nil {
		return &types.ToolResult{Success: false, Error: "Chưa cấu hình mô hình kiểm tra chính tả."}, nil
	}
	if in.From < 0 {
		in.From = 0
	}
	if in.Limit <= 0 {
		in.Limit = spellDefaultLimit
	}
	if in.Limit > spellMaxLimit {
		in.Limit = spellMaxLimit
	}
	mark := in.Mark == nil || *in.Mark

	var (
		content []byte
		ws      *types.DocumentWorkspace
		seq     int
		err     error
	)
	target, err := resolveTargetDocument(ctx, t.workspace, t.sessionID, in.Document, mark)
	if err != nil {
		return &types.ToolResult{Success: false, Error: err.Error()}, nil
	}
	if sel != nil && !selectionIn(sel, target) {
		// the highlighted passage is in another tab
		if in.Scope == "selection" {
			return &types.ToolResult{Success: false, Error: errSelectionElsewhere(sel, target)}, nil
		}
		sel, scope = nil, "document"
	}
	if mark {
		content, ws, seq, err = snapshotDocument(ctx, t.workspace, t.sessionID, target.ID, "kiểm tra chính tả")
	} else {
		content, ws, err = readListedDocument(ctx, t.workspace, t.sessionID, target)
	}
	if err != nil {
		return &types.ToolResult{Success: false, Error: err.Error()}, nil
	}
	doc, err := docxedit.Open(content)
	if err != nil {
		return &types.ToolResult{Success: false, Error: "không đọc được tài liệu: " + err.Error()}, nil
	}
	paras := doc.Paragraphs()

	var scoped []spellPara
	next := -1 // first paragraph after the window, -1: none left
	if scope == "selection" {
		scoped = selectedParagraphs(paras, sel)
		if len(scoped) == 0 {
			return &types.ToolResult{Success: false, Error: "Không tìm thấy đoạn đã bôi đen trong tài liệu hiện tại; có thể tài liệu vừa thay đổi — hãy bôi đen lại."}, nil
		}
	} else {
		end := min(in.From+in.Limit, len(paras))
		for i := in.From; i < end; i++ {
			if strings.TrimSpace(paras[i].Text) != "" {
				scoped = append(scoped, spellPara{Index: i, Text: paras[i].Text})
			}
		}
		if end < len(paras) {
			next = end
		}
	}

	findings, batches, failedBatches := t.review(ctx, scoped)
	if batches > 0 && failedBatches == batches && len(findings) == 0 {
		return &types.ToolResult{Success: false, Error: "Mô hình kiểm tra chính tả không trả lời được (hoặc trả lời sai định dạng). Hãy thử lại sau."}, nil
	}
	truncated := len(findings) > spellMaxFindings
	if truncated {
		findings = findings[:spellMaxFindings]
	}

	var ops []DocumentOp
	if mark {
		ops = spellingMarkOps(newVirtualDoc(paras), findings)
	}

	var out strings.Builder
	where := fmt.Sprintf("%d đoạn", len(scoped))
	if scope == "selection" {
		where = "phần đã bôi đen"
	}
	if len(findings) == 0 {
		fmt.Fprintf(&out, "Không phát hiện lỗi chính tả trong %s của %s.\n", where, ws.FileName)
	} else {
		fmt.Fprintf(&out, "Phát hiện %d lỗi chính tả trong %s của %s:\n", len(findings), where, ws.FileName)
		for i, f := range findings {
			fmt.Fprintf(&out, "%d. Đoạn [%d]: “%s” → “%s” (%s)", i+1, f.Paragraph, f.Wrong, f.Correct, f.Reason)
			if mark && f.Unmarked {
				out.WriteString(" (không gạch chân được)")
			}
			out.WriteString("\n")
		}
	}
	if truncated {
		fmt.Fprintf(&out, "(Chỉ liệt kê %d lỗi đầu tiên.)\n", spellMaxFindings)
	}
	if failedBatches > 0 {
		fmt.Fprintf(&out, "Lưu ý: %d/%d lượt kiểm tra không có kết quả, có thể còn sót lỗi.\n", failedBatches, batches)
	}
	if next >= 0 {
		fmt.Fprintf(&out, "Còn các đoạn sau: gọi lại với from=%d.\n", next)
	}
	if mark && len(findings) > 0 {
		out.WriteString("\nCác chỗ trên sẽ được gạch chân đỏ trong trình soạn thảo; nội dung không bị sửa (Ctrl+Z để bỏ gạch chân).\n")
	} else if len(findings) > 0 {
		out.WriteString("\nNội dung không bị sửa.\n")
	}

	data := map[string]interface{}{}
	if mark {
		data = opsData(ops, seq, ws)
	}
	if findings == nil {
		findings = []SpellingFinding{}
	}
	data["file_name"] = ws.FileName
	data["scope"] = scope
	data["found"] = len(findings)
	data["findings"] = findings
	data["marked"] = mark
	data["failed_batches"] = failedBatches
	if next >= 0 {
		data["next_from"] = next
	}
	return &types.ToolResult{Success: true, Output: out.String(), Data: data}, nil
}

// review runs the stray-character scan and the model over the paragraphs,
// in batches (spellConcurrency at a time, findings kept in batch order); a
// failed batch is logged and skipped. Findings are deduped.
func (t *CheckSpellingTool) review(ctx context.Context, scoped []spellPara) (findings []SpellingFinding, batches, failed int) {
	findings = strayCharFindings(scoped)
	var chunks [][]spellPara
	for start := 0; start < len(scoped); start += spellBatchSize {
		chunks = append(chunks, scoped[start:min(start+spellBatchSize, len(scoped))])
	}
	results := make([][]SpellingFinding, len(chunks))
	errs := make([]error, len(chunks))
	sem := make(chan struct{}, spellConcurrency)
	var wg sync.WaitGroup
	for i, batch := range chunks {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, batch []spellPara) {
			defer func() { <-sem; wg.Done() }()
			results[i], errs[i] = t.reviewBatchOrHalves(ctx, batch)
		}(i, batch)
	}
	wg.Wait()
	for i := range chunks {
		batches++
		if errs[i] != nil {
			failed++
			logger.Warnf(ctx, "check_spelling: batch %d failed: %v", i+1, errs[i])
			continue
		}
		findings = append(findings, results[i]...)
	}
	return dedupeFindings(findings), batches, failed
}

// reviewBatchOrHalves is reviewBatch; a larger batch that still fails is
// reviewed again as two halves (a long reply is where the model derails).
func (t *CheckSpellingTool) reviewBatchOrHalves(ctx context.Context, batch []spellPara) ([]SpellingFinding, error) {
	got, err := t.reviewBatch(ctx, batch)
	if err == nil || len(batch) <= spellSplitMin || ctx.Err() != nil {
		return got, err
	}
	mid := len(batch) / 2
	a, errA := t.reviewBatch(ctx, batch[:mid])
	b, errB := t.reviewBatch(ctx, batch[mid:])
	if errA != nil && errB != nil {
		return nil, err
	}
	return append(a, b...), nil
}

// spellingMarkOps underlines each finding in red where it is; a finding
// that could not be located exactly is flagged Unmarked instead.
func spellingMarkOps(vdoc *virtualDoc, findings []SpellingFinding) []DocumentOp {
	var ops []DocumentOp
	for i, f := range findings {
		if f.raw == "" {
			findings[i].Unmarked = true
			continue
		}
		op := DocumentOp{Op: OpMark, Anchor: vdoc.anchor(f.Paragraph), Text: f.raw, Style: "underline"}
		if f.occurrence > 1 {
			op.TextOccurrence = f.occurrence
		}
		ops = append(ops, op)
	}
	return ops
}

// documentSpelling reviews the first spellMaxLimit non-empty paragraphs of
// a document (the format check's companion pass, see
// CheckDocumentFormatTool.WithSpelling). checked is how many were reviewed,
// ok false when every batch failed.
func (t *CheckSpellingTool) documentSpelling(ctx context.Context, paras []docxedit.Paragraph) (findings []SpellingFinding, checked int, ok bool) {
	var scoped []spellPara
	for i, p := range paras {
		if len(scoped) >= spellMaxLimit {
			break
		}
		if strings.TrimSpace(p.Text) != "" {
			scoped = append(scoped, spellPara{Index: i, Text: p.Text})
		}
	}
	key := spellPassKey(t, scoped)
	if cached, hit := spellPasses.get(key); hit {
		return cached, len(scoped), true
	}
	findings, batches, failed := t.review(ctx, scoped)
	if len(findings) > spellMaxFindings {
		findings = findings[:spellMaxFindings]
	}
	if failed == 0 {
		// a complete pass only: a failed batch may have hidden mistakes
		spellPasses.put(key, findings)
	}
	return findings, len(scoped), batches == 0 || failed < batches || len(findings) > 0
}

// spellPassTTL keeps a document's spelling pass as long as a format result.
const spellPassTTL = formatCheckCacheTTL

// spellPassMax bounds the cache; past it the oldest half is dropped.
const spellPassMax = 500

// spellPassCache keeps the format check's spelling passes by the reviewed
// texts, model and prompt: a repeated check of an unchanged text (Word
// re-saved the file, the marks were applied) costs no model call.
type spellPassCache struct {
	mu      sync.Mutex
	entries map[string]spellPassEntry
}

type spellPassEntry struct {
	findings []SpellingFinding
	at       time.Time
}

var spellPasses = &spellPassCache{entries: map[string]spellPassEntry{}}

func spellPassKey(t *CheckSpellingTool, scoped []spellPara) string {
	h := sha256.New()
	model := ""
	if t.model != nil {
		model = t.model.GetModelName()
	}
	fmt.Fprintf(h, "%s\x00%s\x00", model, t.prompt)
	for _, p := range scoped {
		fmt.Fprintf(h, "%d\x00%s\x00", p.Index, p.Text)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (c *spellPassCache) get(key string) ([]SpellingFinding, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || time.Since(e.at) > spellPassTTL {
		return nil, false
	}
	// callers flag Unmarked on their copy
	return append([]SpellingFinding(nil), e.findings...), true
}

func (c *spellPassCache) put(key string, findings []SpellingFinding) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= spellPassMax {
		oldest := make([]time.Time, 0, len(c.entries))
		for _, e := range c.entries {
			oldest = append(oldest, e.at)
		}
		sort.Slice(oldest, func(i, j int) bool { return oldest[i].Before(oldest[j]) })
		cut := oldest[len(oldest)/2]
		for k, e := range c.entries {
			if e.at.Before(cut) {
				delete(c.entries, k)
			}
		}
	}
	c.entries[key] = spellPassEntry{findings: append([]SpellingFinding(nil), findings...), at: time.Now()}
}

func (c *spellPassCache) reset() {
	c.mu.Lock()
	c.entries = map[string]spellPassEntry{}
	c.mu.Unlock()
}

// selectedParagraphs are the paragraphs the selection covers; a paragraph
// only partly selected keeps the selected part.
func selectedParagraphs(paras []docxedit.Paragraph, sel *types.DocumentSelection) []spellPara {
	s := foldForOverlap(sel.Text)
	var out []spellPara
	for _, p := range paras {
		t := foldForOverlap(p.Text)
		if t == "" {
			continue
		}
		switch {
		case strings.Contains(s, t):
			out = append(out, spellPara{Index: p.Index, Text: p.Text})
		case strings.Contains(t, s):
			out = append(out, spellPara{Index: p.Index, Text: p.Text, Part: sel.Text})
		}
	}
	if len(out) == 0 { // a selection across paragraphs that matches none whole
		for _, line := range strings.Split(sel.Text, "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			for _, p := range paras {
				if strings.TrimSpace(p.Text) != "" && strings.Contains(foldForOverlap(p.Text), foldForOverlap(line)) {
					out = append(out, spellPara{Index: p.Index, Text: p.Text, Part: line})
					break
				}
			}
		}
	}
	return out
}

// strayCharRe mirrors docformat's text.stray_chars rule: a symbol wedged
// between two letters ("Chính p`hủ"). Models tend to silently "fix" such
// typos instead of reporting them, so they are found in code.
var strayCharRe = regexp.MustCompile("\\p{L}[`~^\\\\|]\\p{L}")

func strayCharFindings(paras []spellPara) []SpellingFinding {
	var out []SpellingFinding
	for _, p := range paras {
		text := p.Text
		if p.Part != "" {
			text = p.Part
		}
		for _, loc := range strayCharRe.FindAllStringIndex(text, -1) {
			start, end := loc[0], loc[1]
			for start > 0 {
				r, n := utf8.DecodeLastRuneInString(text[:start])
				if !unicode.IsLetter(r) && !strings.ContainsRune("`~^\\|", r) {
					break
				}
				start -= n
			}
			for end < len(text) {
				r, n := utf8.DecodeRuneInString(text[end:])
				if !unicode.IsLetter(r) && !strings.ContainsRune("`~^\\|", r) {
					break
				}
				end += n
			}
			wrong := text[start:end]
			correct := strings.Map(func(r rune) rune {
				if strings.ContainsRune("`~^\\|", r) {
					return -1
				}
				return r
			}, wrong)
			f := SpellingFinding{Paragraph: p.Index, Wrong: wrong, Correct: correct, Reason: strayCharReason}
			f.raw, f.occurrence, _ = locateInParagraph(p.Text, wrong, p.Part)
			out = append(out, f)
		}
	}
	return out
}

// reviewBatch asks the model about one batch and returns the validated
// findings; a reply that is not the JSON asked for is retried once.
func (t *CheckSpellingTool) reviewBatch(ctx context.Context, batch []spellPara) ([]SpellingFinding, error) {
	var b strings.Builder
	b.WriteString("Đoạn cần kiểm tra:\n")
	for _, p := range batch {
		text := p.Text
		if p.Part != "" {
			text = p.Part
		}
		fmt.Fprintf(&b, "[%d] %s\n", p.Index, clipRunes(strings.TrimSpace(text), spellParagraphRunes))
	}
	llm := spellCompleter{t.model}
	msgs := []docformat.Message{{Role: "system", Content: t.prompt}, {Role: "user", Content: b.String()}}
	text, err := llm.Complete(ctx, msgs)
	if err != nil {
		return nil, err
	}
	raw, perr := parseSpellReply(text)
	if perr != nil {
		// quote only the head of a malformed reply: a runaway one would
		// make the retry as long as the failure
		msgs = append(msgs, docformat.Message{Role: "assistant", Content: clipRunes(text, spellRetryEchoRunes)},
			docformat.Message{Role: "user", Content: `Trả lời lại CHỈ bằng một JSON object hợp lệ dạng {"findings":[...]}.`})
		if text, err = llm.Complete(ctx, msgs); err != nil {
			return nil, err
		}
		if raw, perr = parseSpellReply(text); perr != nil {
			return nil, perr
		}
	}
	return validateFindings(raw, batch), nil
}

type rawFinding struct {
	Paragraph json.RawMessage `json:"paragraph"`
	Wrong     string          `json:"wrong"`
	Correct   string          `json:"correct"`
	Reason    string          `json:"reason"`
}

var jsonFenceRe = regexp.MustCompile("^```(?:json)?\\s*|\\s*```$")

// parseSpellReply accepts {"findings":[...]} or a bare array, tolerating
// code fences and surrounding prose.
func parseSpellReply(text string) ([]rawFinding, error) {
	t := jsonFenceRe.ReplaceAllString(strings.TrimSpace(text), "")
	try := func(s string) ([]rawFinding, bool) {
		var obj struct {
			Findings []rawFinding `json:"findings"`
		}
		if json.Unmarshal([]byte(s), &obj) == nil && obj.Findings != nil {
			return obj.Findings, true
		}
		var arr []rawFinding
		if json.Unmarshal([]byte(s), &arr) == nil {
			return arr, true
		}
		var empty map[string]json.RawMessage
		if json.Unmarshal([]byte(s), &empty) == nil {
			if _, ok := empty["findings"]; ok {
				return nil, true // "findings": null
			}
		}
		return nil, false
	}
	if out, ok := try(t); ok {
		return out, nil
	}
	for _, pair := range [][2]string{{"{", "}"}, {"[", "]"}} {
		if s, e := strings.Index(t, pair[0]), strings.LastIndex(t, pair[1]); s >= 0 && e > s {
			if out, ok := try(t[s : e+1]); ok {
				return out, nil
			}
		}
	}
	return nil, errors.New("spellcheck reply is not the requested JSON")
}

// validateFindings keeps findings whose paragraph is in the batch, whose
// wrong text occurs in it (inside the selected part when only part is in
// scope) and whose correction differs.
func validateFindings(raw []rawFinding, batch []spellPara) []SpellingFinding {
	byIndex := map[int]spellPara{}
	for _, p := range batch {
		byIndex[p.Index] = p
	}
	var out []SpellingFinding
	for _, r := range raw {
		idx, err := strconv.Atoi(strings.Trim(strings.TrimSpace(string(r.Paragraph)), `"[]`))
		if err != nil {
			continue
		}
		p, ok := byIndex[idx]
		wrong, correct := strings.TrimSpace(r.Wrong), strings.TrimSpace(r.Correct)
		if !ok || wrong == "" || normalizeDocText(wrong) == normalizeDocText(correct) {
			continue
		}
		raw, occ, found := locateInParagraph(p.Text, wrong, p.Part)
		if !found {
			continue
		}
		if utf8.RuneCountInString(wrong) > 200 {
			continue // a whole sentence is not a spelling finding
		}
		reason := strings.TrimSpace(r.Reason)
		if reason == "" {
			reason = "lỗi chính tả"
		}
		out = append(out, SpellingFinding{Paragraph: idx, Wrong: wrong, Correct: correct, Reason: reason, raw: raw, occurrence: occ})
	}
	return out
}

// normMap is a paragraph's text normalised like normalizeDocText, with the
// raw byte span each normalised rune came from.
type normMap struct {
	runes      []rune
	start, end []int
}

var zeroWidth = map[rune]bool{'\u200b': true, '\u200c': true, '\u200d': true, '\ufeff': true}

// buildNormMap normalises text cluster by cluster (a base letter with its
// combining marks is composed as one unit), so every normalised rune maps
// back to whole raw characters.
func buildNormMap(text string) normMap {
	var m normMap
	add := func(r rune, s, e int) {
		m.runes = append(m.runes, r)
		m.start = append(m.start, s)
		m.end = append(m.end, e)
	}
	pendingSpace := -1 // raw offset of a whitespace run not yet emitted
	for i := 0; i < len(text); {
		r, n := utf8.DecodeRuneInString(text[i:])
		switch {
		case zeroWidth[r]:
			i += n
			continue
		case r == '\u00a0' || r == '\u202f' || unicode.IsSpace(r):
			if pendingSpace < 0 {
				pendingSpace = i
			}
			i += n
			continue
		}
		if pendingSpace >= 0 {
			if len(m.runes) > 0 { // leading whitespace is trimmed
				add(' ', pendingSpace, i)
			}
			pendingSpace = -1
		}
		j := i + n
		for j < len(text) {
			r2, n2 := utf8.DecodeRuneInString(text[j:])
			if !unicode.Is(unicode.Mn, r2) {
				break
			}
			j += n2
		}
		for _, c := range docxedit.NFCLatin(text[i:j]) {
			add(c, i, j)
		}
		i = j
	}
	return m
}

func (m normMap) indexFrom(needle []rune, from int) int {
	for i := from; i+len(needle) <= len(m.runes); i++ {
		match := true
		for k, r := range needle {
			if m.runes[i+k] != r {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// locateInParagraph finds wrong in the paragraph (inside part when only
// part is in scope) after normalisation, and returns the paragraph's raw
// text of it with its occurrence among the raw instances of that text in
// the paragraph. found is false when wrong is not there; raw is "" when it
// is there but cannot be mapped back exactly.
func locateInParagraph(text, wrong, part string) (raw string, occurrence int, found bool) {
	m := buildNormMap(text)
	needle := []rune(normalizeDocText(wrong))
	if len(needle) == 0 {
		return "", 0, false
	}
	lo, hi := 0, len(m.runes)
	if part != "" {
		if p := []rune(normalizeDocText(part)); len(p) > 0 {
			if at := m.indexFrom(p, 0); at >= 0 {
				lo, hi = at, at+len(p)
			}
		}
	}
	at := m.indexFrom(needle, lo)
	if at < 0 || at+len(needle) > hi {
		return "", 0, false
	}
	s, e := m.start[at], m.end[at+len(needle)-1]
	// a composed rune shared with a neighbour outside the match cannot be
	// split back into raw characters
	if (at > 0 && m.start[at-1] == s) || (at+len(needle) < len(m.runes) && m.end[at+len(needle)] == e) {
		return "", 0, true
	}
	raw = text[s:e]
	return raw, 1 + strings.Count(text[:s], raw), true
}

// dedupeFindings drops repeats (the same error reported twice, or by both
// the stray-character rule and the model) and orders by paragraph.
func dedupeFindings(in []SpellingFinding) []SpellingFinding {
	seen := map[string]bool{}
	var out []SpellingFinding
	for _, f := range in {
		key := strconv.Itoa(f.Paragraph) + "\x00" + normalizeDocText(f.Wrong)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, f)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Paragraph < out[j].Paragraph })
	return out
}

// spellCompleter runs one spelling batch: thinking off, near-greedy, JSON,
// and a reply capped at spellMaxTokens.
type spellCompleter struct{ model chat.Chat }

func (c spellCompleter) Complete(ctx context.Context, messages []docformat.Message) (string, error) {
	msgs := make([]chat.Message, 0, len(messages))
	for _, m := range messages {
		msgs = append(msgs, chat.Message{Role: m.Role, Content: m.Content})
	}
	thinking := false
	resp, err := c.model.Chat(ctx, msgs, &chat.ChatOptions{
		Temperature: 0.01,
		MaxTokens:   spellMaxTokens,
		Thinking:    &thinking,
		Format:      json.RawMessage(`{"type":"object"}`),
	})
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", errors.New("empty model response")
	}
	return resp.Content, nil
}
