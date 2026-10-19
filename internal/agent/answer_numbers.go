package agent

import (
	"context"
	"regexp"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/common"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/tracing/langfuse"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// Invented document numbers are never shown (user decision, 2026-10-19:
// "tra thẳng trong DB — nếu không có thì không đưa ra"). For an agent with
// the check on, every số hiệu in the model's answer must be in the legal
// catalogue or in what this turn's tools returned (a tenant's own
// "123/QĐ-UBND" is cited from its KB) or in the user's question; any other
// is replaced before it reaches the client — the stream is filtered, and
// the stored answer gets the same text. Numbers in code-appended blocks are
// not touched (they never pass through here).

// NumberCatalogFunc reports whether the legal catalogue holds a number,
// given its vietnamese_legal.CatalogNumberKey.
type NumberCatalogFunc func(ctx context.Context, key string) (bool, error)

// SetNumberCatalog turns the answer number check on, looking numbers up
// with fn; nil turns it off.
func (e *AgentEngine) SetNumberCatalog(fn NumberCatalogFunc) {
	e.numberCatalog = fn
}

// RemovedNumberNote replaces a removed number.
const RemovedNumberNote = "(số hiệu không có trong cơ sở dữ liệu nên đã lược bỏ)"

// docTypeWords are the document-type words a removed number takes with it
// ("Nghị định 132/2023/NĐ-CP" → "một nghị định (…)"), longest first.
var docTypeWords = []string{
	"thông tư liên tịch", "nghị quyết", "nghị định", "thông tư", "quyết định", "bộ luật",
	"pháp lệnh", "chỉ thị", "công văn", "văn bản", "luật",
}

var (
	docTypeAlt = func() string {
		parts := make([]string, len(docTypeWords))
		for i, w := range docTypeWords {
			parts[i] = regexp.QuoteMeta(w)
		}
		return strings.Join(parts, "|")
	}()
	// a doc-type word, optionally "số", then spaces, ending the text
	docTypeBeforeRe = regexp.MustCompile(`(?i)(?:^|[^\p{L}\p{N}])((?:` + docTypeAlt + `)(?:\s+số)?\s+)$`)
	// the same while streaming: the spaces may not have arrived yet
	docTypeTailRe = regexp.MustCompile(`(?i)(?:^|[^\p{L}\p{N}])((?:(?:` + docTypeAlt + `)(?:\s+số)?|số)\s*)$`)
	// "số " alone before a number
	soBeforeRe = regexp.MustCompile(`(?i)(?:^|[^\p{L}\p{N}])(số\s+)$`)
	// what a number may still grow into while streaming
	docTypePrefixes = func() []string {
		var out []string
		for _, w := range docTypeWords {
			out = append(out, w+" số")
		}
		return append(append(out, docTypeWords...), "số")
	}()
)

// numberCheck decides, once per number and turn, whether a number may be
// shown. It is shared by the streaming filter and the cleaning of the
// stored answer, so both give the same text.
type numberCheck struct {
	catalog NumberCatalogFunc
	state   *types.AgentState
	query   string

	mu            sync.Mutex
	catalogued    map[string]bool // catalogue answers, by key
	evidence      map[string]bool
	evidenceSteps int
	reported      map[string]bool
}

type numberCheckKey struct{}

// withNumberCheck puts the turn's number check in ctx for the round's
// emitters; ctx unchanged when the check is off.
func (e *AgentEngine) withNumberCheck(ctx context.Context, state *types.AgentState, query string) context.Context {
	if e.numberCatalog == nil || state == nil {
		return ctx
	}
	if e.numbers == nil || e.numbers.state != state {
		e.numbers = &numberCheck{
			catalog: e.numberCatalog, state: state, query: query,
			catalogued: map[string]bool{}, reported: map[string]bool{},
		}
	}
	return context.WithValue(ctx, numberCheckKey{}, e.numbers)
}

func numberCheckFromContext(ctx context.Context) *numberCheck {
	c, _ := ctx.Value(numberCheckKey{}).(*numberCheck)
	return c
}

// numberKeys are the keys of the số hiệu written in text.
func numberKeys(text string, into map[string]bool) {
	for _, s := range vietnamese_legal.CitedNumberSpans(text) {
		into[vietnamese_legal.CatalogNumberKey(text[s[0]:s[1]])] = true
	}
}

// refreshEvidence rebuilds the numbers this turn's tools returned (every
// successful result's text, read_file excepted: a pack's own commentary is
// not evidence) and the user's question.
func (c *numberCheck) refreshEvidence() {
	if c.evidence != nil && c.evidenceSteps == len(c.state.RoundSteps) {
		return
	}
	ev := map[string]bool{}
	numberKeys(c.query, ev)
	for _, step := range c.state.RoundSteps {
		for _, tc := range step.ToolCalls {
			if tc.Result == nil || !tc.Result.Success || tc.ExecutionName() == "read_file" {
				continue
			}
			numberKeys(tc.Result.Output, ev)
			for _, v := range tc.Result.Data {
				collectDataNumbers(v, ev, 0)
			}
		}
	}
	c.evidence, c.evidenceSteps = ev, len(c.state.RoundSteps)
}

// collectDataNumbers adds the numbers of the strings of a tool's Data.
func collectDataNumbers(v interface{}, into map[string]bool, depth int) {
	if depth > 6 {
		return
	}
	switch x := v.(type) {
	case string:
		numberKeys(x, into)
	case []interface{}:
		for _, e := range x {
			collectDataNumbers(e, into, depth+1)
		}
	case map[string]interface{}:
		for _, e := range x {
			collectDataNumbers(e, into, depth+1)
		}
	case []map[string]interface{}:
		for _, e := range x {
			collectDataNumbers(e, into, depth+1)
		}
	}
}

// keep reports whether number may be shown. A lookup error counts as not
// found (the user rule is never to show an unverified number) and is
// logged as such.
func (c *numberCheck) keep(ctx context.Context, number string) bool {
	key := vietnamese_legal.CatalogNumberKey(number)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.refreshEvidence()
	if c.evidence[key] {
		return true
	}
	if found, ok := c.catalogued[key]; ok {
		return found
	}
	found, err := c.catalog(ctx, key)
	reason := "not_found"
	if err != nil {
		found, reason = false, "lookup_failed"
		logger.Warnf(ctx, "[Agent][AnswerNumbers] catalogue lookup of %s failed: %v", number, err)
	}
	c.catalogued[key] = found
	if !found {
		if !c.reported[key] {
			c.reported[key] = true
			reportRemovedNumber(ctx, number, reason)
		}
	}
	return found
}

// reportRemovedNumber logs a removal for review of false removals.
func reportRemovedNumber(ctx context.Context, number, reason string) {
	logger.Warnf(ctx, "[Agent][AnswerNumbers] removed %s from the answer: %s", number, reason)
	common.PipelineWarn(ctx, "Agent", "answer_number_removed", map[string]interface{}{
		"number": number, "reason": reason,
	})
	_, span := langfuse.GetManager().StartSpan(ctx, langfuse.SpanOptions{
		Name:  "agent.answer_number_removed",
		Input: map[string]interface{}{"number": number, "reason": reason},
	})
	span.Finish(map[string]interface{}{"replacement": RemovedNumberNote}, nil, nil)
}

// rewrite replaces in text every number the check does not keep, with the
// doc-type word before it when there is one. before is the text that came
// just before (streaming), used only to tell a sentence start.
func (c *numberCheck) rewrite(ctx context.Context, before, text string) string {
	spans := vietnamese_legal.CitedNumberSpans(text)
	if len(spans) == 0 {
		return text
	}
	var b strings.Builder
	last := 0
	for _, s := range spans {
		if c.keep(ctx, text[s[0]:s[1]]) {
			continue
		}
		start, replacement := removalOf(before, text, last, s[0])
		b.WriteString(text[last:start])
		b.WriteString(replacement)
		last = s[1]
	}
	b.WriteString(text[last:])
	return b.String()
}

// removalOf is where the removed span starts (the number, or the doc-type
// word and "số" before it, not before floor) and what replaces it.
func removalOf(context, text string, floor, numStart int) (int, string) {
	before := text[floor:numStart]
	if m := docTypeBeforeRe.FindStringSubmatchIndex(before); m != nil {
		start := floor + m[2]
		word := strings.ToLower(strings.TrimSpace(text[start:numStart]))
		word = strings.TrimSpace(strings.TrimSuffix(word, "số"))
		article := "một"
		if sentenceStart(context + text[:start]) {
			article = "Một"
		}
		return start, article + " " + strings.Join(strings.Fields(word), " ") + " " + RemovedNumberNote
	}
	if m := soBeforeRe.FindStringSubmatchIndex(before); m != nil {
		return floor + m[2], RemovedNumberNote
	}
	return numStart, RemovedNumberNote
}

// sentenceStart: nothing but markup since a line break or a sentence end.
func sentenceStart(before string) bool {
	t := strings.TrimRight(before, " \t*_#>-")
	return t == "" || strings.HasSuffix(t, "\n") || strings.HasSuffix(t, ".") ||
		strings.HasSuffix(t, "!") || strings.HasSuffix(t, "?") || strings.HasSuffix(t, ":")
}

// cleanAnswerNumbers is rewrite for a whole answer when the check is on.
func cleanAnswerNumbers(ctx context.Context, text string) string {
	c := numberCheckFromContext(ctx)
	if c == nil || text == "" {
		return text
	}
	return c.rewrite(ctx, "", text)
}

// numberGuard filters a streamed answer: it holds back only what could
// still become a number or the doc-type words before one, and emits the
// rest rewritten. The cut points never split a number or a doc-type word
// from its number, so the streamed text equals cleanAnswerNumbers of the
// whole answer.
type numberGuard struct {
	ctx     context.Context
	check   *numberCheck
	emit    func(string)
	pending string
	before  string // the end of the text already emitted, as written
}

// newNumberGuard returns a guard writing to emit, nil when the check is off.
func newNumberGuard(ctx context.Context, emit func(string)) *numberGuard {
	c := numberCheckFromContext(ctx)
	if c == nil {
		return nil
	}
	return &numberGuard{ctx: ctx, check: c, emit: emit}
}

// Write adds streamed text and emits what can no longer change.
func (g *numberGuard) Write(text string) {
	if text == "" {
		return
	}
	g.pending += text
	cut := safeCut(g.pending)
	if cut == 0 {
		return
	}
	region := g.pending[:cut]
	g.pending = g.pending[cut:]
	g.emitRegion(region)
}

// emitRegion rewrites and emits one region and remembers its end.
func (g *numberGuard) emitRegion(region string) {
	if out := g.check.rewrite(g.ctx, g.before, region); out != "" {
		g.emit(out)
	}
	g.before += region
	if len(g.before) > 64 {
		cut := len(g.before) - 64
		for cut < len(g.before) && !utf8.RuneStart(g.before[cut]) {
			cut++
		}
		g.before = g.before[cut:]
	}
}

// Flush emits what is still held back (end of the stream).
func (g *numberGuard) Flush() {
	if g.pending == "" {
		return
	}
	region := g.pending
	g.pending = ""
	g.emitRegion(region)
}

// isNumberRune is a rune a số hiệu is written with.
func isNumberRune(r rune) bool {
	return r < utf8.RuneSelf && (r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r == '/' || r == '-') ||
		r == 'Đ' || r == 'đ' || r == '–' || r == '—' || r == '‐'
}

// safeCut is the length of text's prefix that can be emitted: before an
// incomplete rune, a trailing run that holds a digit (a number may be
// growing), a trailing doc-type word (with "số") waiting for its number,
// or a trailing fragment that may grow into such a word.
func safeCut(text string) int {
	hold := len(text)
	// an incomplete UTF-8 sequence
	if i := lastRuneStart(text); i < len(text) && !utf8.FullRuneInString(text[i:]) {
		hold = i
	}
	end := hold
	// a trailing run of number runes holding a digit
	j := hold
	digit := false
	for j > 0 {
		r, size := utf8.DecodeLastRuneInString(text[:j])
		if !isNumberRune(r) {
			break
		}
		if r >= '0' && r <= '9' {
			digit = true
		}
		j -= size
	}
	if digit {
		hold = j
	}
	// a doc-type word (and "số") waiting for a number
	if m := docTypeTailRe.FindStringSubmatchIndex(text[:hold]); m != nil {
		hold = m[2]
	}
	// a fragment that may become one ("Nghị đ", "Thông tư s"); only at the
	// very end: before a number already started, a doc-type word is whole
	if hold < end {
		return hold
	}
	head := text[:hold]
	for _, w := range docTypePrefixes {
		for k := len(w) - 1; k > 0; k-- {
			if !utf8.RuneStart(w[k]) || len(head) < k {
				continue
			}
			tail := head[len(head)-k:]
			if !utf8.ValidString(tail) || !strings.EqualFold(tail, w[:k]) {
				continue
			}
			if p := len(head) - k; p == 0 || !isWordRuneBefore(head[:p]) {
				if p < hold {
					hold = p
				}
				break
			}
		}
	}
	return hold
}

func lastRuneStart(s string) int {
	i := len(s) - 1
	for i > 0 && !utf8.RuneStart(s[i]) {
		i--
	}
	if i < 0 {
		return 0
	}
	return i
}

func isWordRuneBefore(s string) bool {
	r, _ := utf8.DecodeLastRuneInString(s)
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}
